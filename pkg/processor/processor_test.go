package processor

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xh63/netbird-events/pkg/config"
	"github.com/xh63/netbird-events/pkg/events"
	"github.com/xh63/netbird-events/pkg/metrics"
)

// ============================================================================
// Helpers
// ============================================================================

// makeTestProcessor builds a Processor directly, bypassing NewProcessor's DB
// dialling. Uses source="none" so GetEvents produces the simplest query
// (no JOINs), making mock expectations easier to write.
func makeTestProcessor(db *sql.DB, writer EventWriter, checkpoint *events.ProcessingCheckpoint, batchSize int) *Processor {
	cfg := &config.Config{
		ConsumerID:    "test-consumer",
		Platform:      "sandbox",
		Region:        "apac",
		LogLevel:      "info",
		BatchSize:     batchSize,
		LookbackHours: 0, // no lookback so fresh start has no WHERE clause
		EmailEnrichment: config.EmailEnrichmentConfig{
			Enabled: true,
			Source:  "none", // simplest query path — no JOINs
		},
	}
	logFactory := cfg.NewLogFactory()
	logger := logFactory.New("system")
	return &Processor{
		eventReader: events.NewPostgresEventReader(db, logger, &cfg.EmailEnrichment),
		writer:      writer,
		checkpoint:  checkpoint,
		config:      cfg,
		logFactory:  logFactory,
		logger:      logger,
		hostname:    "test-node",
	}
}

// freshCheckpoint returns a zero-value checkpoint for the test consumer.
func freshCheckpoint() *events.ProcessingCheckpoint {
	return &events.ProcessingCheckpoint{
		ConsumerID:           "test-consumer",
		LastEventID:          0,
		TotalEventsProcessed: 0,
		ProcessingNode:       "test-node",
	}
}

// eventCols are the 9 columns GetEvents scans in order.
var eventCols = []string{
	"id", "timestamp", "activity",
	"initiator_id", "target_id", "account_id", "meta",
	"initiator_email", "target_email",
}

// addEventRow appends one row to a sqlmock.Rows value.
func addEventRow(rows *sqlmock.Rows, id int64, ts time.Time, activity int) *sqlmock.Rows {
	return rows.AddRow(id, ts, activity, "user1", "peer1", "account1", "{}", "user1@example.com", "peer1@example.com")
}

// expectSaveCheckpoint sets up the sqlmock INSERT expectation for SaveCheckpoint.
// Uses AnyArg() for the timestamp to avoid brittle time comparisons.
func expectSaveCheckpoint(mock sqlmock.Sqlmock, consumerID string, lastEventID, totalProcessed int64, node string) {
	mock.ExpectExec("INSERT INTO idp.event_processing_checkpoint").
		WithArgs(consumerID, lastEventID, sqlmock.AnyArg(), totalProcessed, node).
		WillReturnResult(sqlmock.NewResult(1, 1))
}

// checkpointCols are the 7 columns GetCheckpoint scans (matches the SELECT in reader.go).
var checkpointCols = []string{
	"consumer_id", "last_event_id", "last_event_timestamp",
	"total_events_processed", "processing_node", "updated_at", "created_at",
}

// expectGetCheckpointEmpty makes GetCheckpoint return no rows → fresh start.
func expectGetCheckpointEmpty(mock sqlmock.Sqlmock) {
	mock.ExpectQuery("SELECT.*FROM idp.event_processing_checkpoint").
		WithArgs("test-consumer").
		WillReturnRows(sqlmock.NewRows(checkpointCols))
}

// expectGetCheckpointFound makes GetCheckpoint return an existing checkpoint row.
func expectGetCheckpointFound(mock sqlmock.Sqlmock, lastEventID, totalProcessed int64, node string) {
	ts := time.Now()
	mock.ExpectQuery("SELECT.*FROM idp.event_processing_checkpoint").
		WithArgs("test-consumer").
		WillReturnRows(sqlmock.NewRows(checkpointCols).
			AddRow("test-consumer", lastEventID, ts, totalProcessed, node, ts, ts))
}

// ============================================================================
// processEvents tests
// ============================================================================

// TestProcessEvents_FreshStart verifies the basic happy path:
// no checkpoint, two events returned, writer called once, checkpoint saved.
func TestProcessEvents_FreshStart(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	ts := time.Now().Add(-1 * time.Minute)

	// First SELECT: returns 2 events (less than batchSize=1000 → stops after this batch)
	rows := sqlmock.NewRows(eventCols)
	rows = addEventRow(rows, 1, ts, 1)
	rows = addEventRow(rows, 2, ts, 2)
	mock.ExpectQuery("SELECT.*FROM events").
		WithArgs(1000, 0).
		WillReturnRows(rows)

	// SaveCheckpoint after the batch: last_event_id=2, total=2
	expectSaveCheckpoint(mock, "test-consumer", 2, 2, "test-node")

	writer := &mockWriter{}
	proc := makeTestProcessor(db, writer, freshCheckpoint(), 1000)

	err = proc.processEvents(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 1, writer.callCount, "writer should be called once")
	assert.Equal(t, 2, len(writer.sentEvents[0]), "both events should be sent")
	assert.Equal(t, int64(2), proc.checkpoint.LastEventID)
	assert.Equal(t, int64(2), proc.checkpoint.TotalEventsProcessed)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestProcessEvents_EmptyBatch verifies that when there are no events,
// the writer is never called and no checkpoint is saved.
func TestProcessEvents_EmptyBatch(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// SELECT returns zero rows immediately
	mock.ExpectQuery("SELECT.*FROM events").
		WithArgs(1000, 0).
		WillReturnRows(sqlmock.NewRows(eventCols))

	writer := &mockWriter{}
	proc := makeTestProcessor(db, writer, freshCheckpoint(), 1000)

	err = proc.processEvents(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 0, writer.callCount, "writer should not be called when no events")
	assert.Equal(t, int64(0), proc.checkpoint.LastEventID, "checkpoint should not advance")
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestProcessEvents_WriterFails verifies that when the writer returns an error,
// processEvents returns that error and does NOT save the checkpoint.
func TestProcessEvents_WriterFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	ts := time.Now()
	rows := addEventRow(sqlmock.NewRows(eventCols), 1, ts, 1)
	mock.ExpectQuery("SELECT.*FROM events").
		WithArgs(1000, 0).
		WillReturnRows(rows)

	// No ExpectExec here — checkpoint must NOT be saved on writer failure

	writer := &mockWriter{shouldFail: true, failError: "stdout broken"}
	proc := makeTestProcessor(db, writer, freshCheckpoint(), 1000)

	err = proc.processEvents(context.Background())

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "stdout broken")
	assert.Equal(t, int64(0), proc.checkpoint.LastEventID, "checkpoint must not advance on failure")
	require.NoError(t, mock.ExpectationsWereMet(), "no checkpoint INSERT should have been called")
}

// TestProcessEvents_MultipleBatches verifies that when the first batch is full
// (exactly batchSize), a second query is issued for the next batch.
func TestProcessEvents_MultipleBatches(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	ts := time.Now().Add(-2 * time.Minute)
	batchSize := 2

	// First batch: 2 events (== batchSize → loop continues)
	firstBatch := sqlmock.NewRows(eventCols)
	firstBatch = addEventRow(firstBatch, 1, ts, 1)
	firstBatch = addEventRow(firstBatch, 2, ts, 2)
	mock.ExpectQuery("SELECT.*FROM events").
		WithArgs(batchSize, 0).
		WillReturnRows(firstBatch)
	expectSaveCheckpoint(mock, "test-consumer", 2, 2, "test-node")

	// Second batch: 1 event (< batchSize → loop stops)
	secondBatch := addEventRow(sqlmock.NewRows(eventCols), 3, ts, 3)
	mock.ExpectQuery("SELECT.*FROM events").
		WithArgs(int64(2), batchSize, 0). // WHERE e.id > 2
		WillReturnRows(secondBatch)
	expectSaveCheckpoint(mock, "test-consumer", 3, 3, "test-node")

	writer := &mockWriter{}
	proc := makeTestProcessor(db, writer, freshCheckpoint(), batchSize)

	err = proc.processEvents(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 2, writer.callCount, "writer should be called once per batch")
	assert.Equal(t, 2, len(writer.sentEvents[0]), "first batch has 2 events")
	assert.Equal(t, 1, len(writer.sentEvents[1]), "second batch has 1 event")
	assert.Equal(t, int64(3), proc.checkpoint.LastEventID)
	assert.Equal(t, int64(3), proc.checkpoint.TotalEventsProcessed)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestProcessEvents_ResumeFromCheckpoint verifies that when LastEventID > 0,
// the SELECT includes WHERE e.id > <lastEventID>.
func TestProcessEvents_ResumeFromCheckpoint(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	ts := time.Now()

	// Expect SELECT with the MinEventID arg first (WHERE e.id > 42)
	rows := addEventRow(sqlmock.NewRows(eventCols), 43, ts, 1)
	mock.ExpectQuery("SELECT.*FROM events").
		WithArgs(int64(42), 1000, 0).
		WillReturnRows(rows)
	expectSaveCheckpoint(mock, "test-consumer", 43, 101, "test-node")

	checkpoint := &events.ProcessingCheckpoint{
		ConsumerID:           "test-consumer",
		LastEventID:          42,
		TotalEventsProcessed: 100,
		ProcessingNode:       "test-node",
	}

	writer := &mockWriter{}
	proc := makeTestProcessor(db, writer, checkpoint, 1000)

	err = proc.processEvents(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 1, writer.callCount)
	assert.Equal(t, int64(43), proc.checkpoint.LastEventID, "checkpoint should advance to new event")
	assert.Equal(t, int64(101), proc.checkpoint.TotalEventsProcessed, "total should be 100+1")
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestProcessEvents_DBQueryFails verifies that a database error on GetEvents
// is propagated and no writer call is made.
func TestProcessEvents_DBQueryFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("SELECT.*FROM events").
		WillReturnError(errors.New("connection reset by peer"))

	writer := &mockWriter{}
	proc := makeTestProcessor(db, writer, freshCheckpoint(), 1000)

	err = proc.processEvents(context.Background())

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "connection reset by peer")
	assert.Equal(t, 0, writer.callCount)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestProcessEvents_MetricsUpdated verifies that after a successful run,
// the Prometheus metrics reflect the processed events.
func TestProcessEvents_MetricsUpdated(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	ts := time.Now().Add(-30 * time.Second)

	rows := sqlmock.NewRows(eventCols)
	rows = addEventRow(rows, 10, ts, 1)
	rows = addEventRow(rows, 11, ts, 2)
	rows = addEventRow(rows, 12, ts, 3)
	mock.ExpectQuery("SELECT.*FROM events").
		WithArgs(1000, 0).
		WillReturnRows(rows)
	expectSaveCheckpoint(mock, "test-consumer", 12, 3, "test-node")

	// Record metric values BEFORE the run (counters accumulate across tests)
	beforeProcessed := testutil.ToFloat64(metrics.EventsProcessed.WithLabelValues("false"))
	beforeErrors := testutil.ToFloat64(metrics.EventsProcessed.WithLabelValues("true"))

	writer := &mockWriter{}
	proc := makeTestProcessor(db, writer, freshCheckpoint(), 1000)

	err = proc.processEvents(context.Background())
	require.NoError(t, err)

	// Counter delta: 3 events processed successfully, 0 errors
	assert.Equal(t, float64(3), testutil.ToFloat64(metrics.EventsProcessed.WithLabelValues("false"))-beforeProcessed)
	assert.Equal(t, float64(0), testutil.ToFloat64(metrics.EventsProcessed.WithLabelValues("true"))-beforeErrors)

	// Gauge: last event ID set to 12
	assert.Equal(t, float64(12), testutil.ToFloat64(metrics.LastEventID))

	// Gauge: lag should be ~30s (event was 30s ago), allow generous range
	lag := testutil.ToFloat64(metrics.CheckpointLagSeconds)
	assert.GreaterOrEqual(t, lag, float64(25), "lag should be around 30s")
	assert.LessOrEqual(t, lag, float64(60), "lag should not be unreasonably large")

	require.NoError(t, mock.ExpectationsWereMet())
}

// TestProcessEvents_WriterFailsMetrics verifies that writer failures increment
// the error counter, not the success counter.
func TestProcessEvents_WriterFailsMetrics(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	ts := time.Now()
	rows := addEventRow(sqlmock.NewRows(eventCols), 1, ts, 1)
	mock.ExpectQuery("SELECT.*FROM events").
		WithArgs(1000, 0).
		WillReturnRows(rows)

	beforeErrors := testutil.ToFloat64(metrics.EventsProcessed.WithLabelValues("true"))
	beforeSuccess := testutil.ToFloat64(metrics.EventsProcessed.WithLabelValues("false"))

	writer := &mockWriter{shouldFail: true, failError: "pipe broken"}
	proc := makeTestProcessor(db, writer, freshCheckpoint(), 1000)

	err = proc.processEvents(context.Background())
	assert.Error(t, err)

	assert.Equal(t, float64(1), testutil.ToFloat64(metrics.EventsProcessed.WithLabelValues("true"))-beforeErrors)
	assert.Equal(t, float64(0), testutil.ToFloat64(metrics.EventsProcessed.WithLabelValues("false"))-beforeSuccess)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestProcessEvents_LookbackHours covers the branch where LookbackHours > 0
// and there is no prior checkpoint — a StartTime filter is added to the query.
func TestProcessEvents_LookbackHours(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	ts := time.Now().Add(-30 * time.Minute)

	// StartTime is the first positional arg when MinEventID is not set.
	rows := addEventRow(sqlmock.NewRows(eventCols), 1, ts, 1)
	mock.ExpectQuery("SELECT.*FROM events").
		WithArgs(sqlmock.AnyArg(), 1000, 0). // AnyArg = the computed startTime
		WillReturnRows(rows)
	expectSaveCheckpoint(mock, "test-consumer", 1, 1, "test-node")

	proc := makeTestProcessor(db, &mockWriter{}, freshCheckpoint(), 1000)
	proc.config.LookbackHours = 1 // trigger the lookback path

	err = proc.processEvents(context.Background())

	require.NoError(t, err)
	assert.Equal(t, int64(1), proc.checkpoint.LastEventID)
	require.NoError(t, mock.ExpectationsWereMet())
}

// ============================================================================
// Run and Close tests
// ============================================================================

// TestProcessor_Run_FreshStart verifies the full Run lifecycle when there is
// no existing checkpoint: loads from DB (no rows), processes events, exits.
func TestProcessor_Run_FreshStart(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	ts := time.Now().Add(-1 * time.Minute)

	expectGetCheckpointEmpty(mock)
	rows := addEventRow(sqlmock.NewRows(eventCols), 1, ts, 1)
	mock.ExpectQuery("SELECT.*FROM events").
		WithArgs(1000, 0).
		WillReturnRows(rows)
	expectSaveCheckpoint(mock, "test-consumer", 1, 1, "test-node")

	writer := &mockWriter{}
	proc := makeTestProcessor(db, writer, freshCheckpoint(), 1000)

	err = proc.Run(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 1, writer.callCount)
	assert.Equal(t, int64(1), proc.checkpoint.LastEventID)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestProcessor_Run_ResumeFromCheckpoint verifies that when GetCheckpoint
// returns an existing row, Run resumes from that event ID.
func TestProcessor_Run_ResumeFromCheckpoint(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	ts := time.Now().Add(-1 * time.Minute)

	expectGetCheckpointFound(mock, 50, 50, "old-node")
	rows := addEventRow(sqlmock.NewRows(eventCols), 51, ts, 1)
	mock.ExpectQuery("SELECT.*FROM events").
		WithArgs(int64(50), 1000, 0). // WHERE e.id > 50
		WillReturnRows(rows)
	expectSaveCheckpoint(mock, "test-consumer", 51, 51, "test-node")

	writer := &mockWriter{}
	proc := makeTestProcessor(db, writer, freshCheckpoint(), 1000)

	err = proc.Run(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 1, writer.callCount)
	assert.Equal(t, int64(51), proc.checkpoint.LastEventID)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestProcessor_Run_GetCheckpointFails verifies that a DB error during
// checkpoint loading causes Run to return a wrapped error immediately.
func TestProcessor_Run_GetCheckpointFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("SELECT.*FROM idp.event_processing_checkpoint").
		WithArgs("test-consumer").
		WillReturnError(errors.New("db timeout"))

	proc := makeTestProcessor(db, &mockWriter{}, freshCheckpoint(), 1000)

	err = proc.Run(context.Background())

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load checkpoint")
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestProcessor_Run_ContextCancelledWhilePolling verifies that when
// PollingInterval > 0, Run exits cleanly with context.Canceled.
func TestProcessor_Run_ContextCancelledWhilePolling(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Checkpoint load: fresh start
	expectGetCheckpointEmpty(mock)
	// First processEvents returns no events immediately
	mock.ExpectQuery("SELECT.*FROM events").
		WithArgs(1000, 0).
		WillReturnRows(sqlmock.NewRows(eventCols))

	proc := makeTestProcessor(db, &mockWriter{}, freshCheckpoint(), 1000)
	proc.config.PollingInterval = 300 // 5 min ticker — won't fire during test

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	err = proc.Run(ctx)

	assert.ErrorIs(t, err, context.Canceled)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestProcessor_Close verifies that Close closes the underlying DB connection.
func TestProcessor_Close(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	mock.ExpectClose()

	proc := makeTestProcessor(db, &mockWriter{}, freshCheckpoint(), 1000)

	err = proc.Close()

	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

// ============================================================================
// mockWriter — in-memory EventWriter for tests
// ============================================================================

type mockWriter struct {
	name       string
	shouldFail bool
	failError  string
	callCount  int
	sentEvents [][]events.Event
}

func (m *mockWriter) SendEvents(_ context.Context, evts []events.Event) error {
	m.callCount++
	if m.shouldFail {
		return errors.New(m.failError)
	}
	batch := make([]events.Event, len(evts))
	copy(batch, evts)
	m.sentEvents = append(m.sentEvents, batch)
	return nil
}

// ============================================================================
// Existing tests (unchanged)
// ============================================================================

func TestNewProcessor_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectPing()

	cfg := &config.Config{
		PostgresURL:     "mock://db",
		Platform:        "sandbox",
		Region:          "apac",
		ConsumerID:      "test-consumer",
		BatchSize:       1000,
		LookbackHours:   24,
		PollingInterval: 0,
	}

	_, err = NewProcessor(cfg, cfg.NewLogFactory())
	if err == nil {
		t.Skip("Expected error without real database connection")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Logf("Some expectations were not met (expected for this test): %v", err)
	}
}

func TestProcessor_Struct(t *testing.T) {
	cfg := &config.Config{
		PostgresURL: "postgresql://localhost/db",
		Platform:    "sandbox",
		Region:      "apac",
		ConsumerID:  "test",
		LogLevel:    "info",
	}

	logFactory := cfg.NewLogFactory()
	logger := logFactory.New("system")

	proc := &Processor{
		eventReader: nil,
		writer:      &mockWriter{name: "stdout"},
		checkpoint: &events.ProcessingCheckpoint{
			ConsumerID:           cfg.ConsumerID,
			LastEventID:          0,
			LastEventTimestamp:   time.Now(),
			TotalEventsProcessed: 0,
			ProcessingNode:       "test-node",
		},
		config:     cfg,
		logFactory: logFactory,
		logger:     logger,
		hostname:   "test-node",
	}

	if proc.writer == nil {
		t.Error("Expected writer to be set")
	}
	if proc.checkpoint == nil {
		t.Error("Expected checkpoint to be set")
	}
	if proc.config != cfg {
		t.Error("Expected config to match")
	}
	if proc.logFactory == nil {
		t.Error("Expected logFactory to be set")
	}
	if proc.logger != logger {
		t.Error("Expected logger to match")
	}
	if proc.hostname != "test-node" {
		t.Error("Expected hostname to be set")
	}
}

func TestConfig_SimplifiedArchitecture(t *testing.T) {
	cfg := &config.Config{
		PostgresURL:     "postgresql://user:pass@localhost:5432/netbird",
		Platform:        "prod",
		Region:          "emea",
		ConsumerID:      "eventsproc-prod-emea",
		LogLevel:        "info",
		BatchSize:       5000,
		LookbackHours:   1,
		PollingInterval: 300,
	}

	if cfg.PostgresURL != "postgresql://user:pass@localhost:5432/netbird" {
		t.Errorf("Expected PostgresURL to be set, got %s", cfg.PostgresURL)
	}
	if cfg.Platform != "prod" {
		t.Errorf("Expected Platform='prod', got %s", cfg.Platform)
	}
	if cfg.Region != "emea" {
		t.Errorf("Expected Region='emea', got %s", cfg.Region)
	}
	if cfg.ConsumerID != "eventsproc-prod-emea" {
		t.Errorf("Expected ConsumerID='eventsproc-prod-emea', got %s", cfg.ConsumerID)
	}
	if cfg.BatchSize != 5000 {
		t.Errorf("Expected BatchSize=5000, got %d", cfg.BatchSize)
	}
	if cfg.PollingInterval != 300 {
		t.Errorf("Expected PollingInterval=300, got %d", cfg.PollingInterval)
	}
}

func TestMockWriter_SendEvents(t *testing.T) {
	writer := &mockWriter{name: "test"}

	ctx := context.Background()
	testEvents := []events.Event{
		{ID: 1, Timestamp: time.Now(), Activity: 100, AccountID: "account1"},
		{ID: 2, Timestamp: time.Now(), Activity: 200, AccountID: "account2"},
	}

	err := writer.SendEvents(ctx, testEvents)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if len(writer.sentEvents) != 1 {
		t.Errorf("Expected 1 call to SendEvents, got %d", len(writer.sentEvents))
	}
	if len(writer.sentEvents[0]) != 2 {
		t.Errorf("Expected 2 events sent, got %d", len(writer.sentEvents[0]))
	}
	if writer.sentEvents[0][0].ID != 1 {
		t.Errorf("Expected first event ID=1, got %d", writer.sentEvents[0][0].ID)
	}
	if writer.sentEvents[0][1].ID != 2 {
		t.Errorf("Expected second event ID=2, got %d", writer.sentEvents[0][1].ID)
	}
	if writer.callCount != 1 {
		t.Errorf("Expected callCount=1, got %d", writer.callCount)
	}
}

func TestMockWriter_Failure(t *testing.T) {
	writer := &mockWriter{
		name:       "failing-writer",
		shouldFail: true,
		failError:  "connection refused",
	}

	ctx := context.Background()
	testEvents := []events.Event{
		{ID: 1, Timestamp: time.Now(), Activity: 100, AccountID: "account1"},
	}

	err := writer.SendEvents(ctx, testEvents)
	if err == nil {
		t.Error("Expected error from failing writer")
	}
	if err.Error() != "connection refused" {
		t.Errorf("Expected error 'connection refused', got '%s'", err.Error())
	}
	if writer.callCount != 1 {
		t.Errorf("Expected callCount=1 even on failure, got %d", writer.callCount)
	}
	if len(writer.sentEvents) != 0 {
		t.Errorf("Expected no events sent on failure, got %d", len(writer.sentEvents))
	}
}
