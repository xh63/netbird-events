package events

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

// mockEmailEnrichmentConfig is a mock implementation for testing
type mockEmailEnrichmentConfig struct {
	enabled      bool
	source       string
	customSchema string
	customTable  string
}

func (m *mockEmailEnrichmentConfig) GetSource() string {
	if !m.enabled {
		return "none"
	}
	return m.source
}

func (m *mockEmailEnrichmentConfig) GetCustomSchema() string {
	return m.customSchema
}

func (m *mockEmailEnrichmentConfig) GetCustomTable() string {
	return m.customTable
}

func (m *mockEmailEnrichmentConfig) IsEnabled() bool {
	return m.enabled
}

// newMockEmailConfig creates a default mock email enrichment config for tests
func newMockEmailConfig() *mockEmailEnrichmentConfig {
	return &mockEmailEnrichmentConfig{
		enabled: true,
		source:  "idp_okta_users",
	}
}

func TestNewEventReader(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	emailConfig := newMockEmailConfig()
	reader := NewEventReader(db, logger, emailConfig)

	if reader == nil {
		t.Fatal("Expected non-nil EventReader")
	}
	if reader.db != db {
		t.Error("Expected db to be set correctly")
	}
	if reader.logger != logger {
		t.Error("Expected logger to be set correctly")
	}
}

func TestGetEvents_NoFilters(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	emailConfig := newMockEmailConfig()
	reader := NewEventReader(db, logger, emailConfig)

	// Mock the query
	now := time.Now()
	rows := sqlmock.NewRows([]string{"id", "timestamp", "activity", "initiator_id", "target_id", "account_id", "meta", "initiator_email", "target_email"}).
		AddRow(int64(1), now, 1, "initiator1", "target1", "account1", `{"key":"value"}`, "init1@example.com", "target1@example.com").
		AddRow(int64(2), now, 2, "initiator2", "target2", "account1", `{"key":"value2"}`, "init2@example.com", "target2@example.com")

	mock.ExpectQuery("SELECT e.id, e.timestamp, e.activity, e.initiator_id, e.target_id, e.account_id, e.meta").
		WillReturnRows(rows)

	opts := EventQueryOptions{
		Limit: 10,
	}

	events, err := reader.GetEvents(context.Background(), opts)
	if err != nil {
		t.Fatalf("GetEvents failed: %v", err)
	}

	if len(events) != 2 {
		t.Errorf("Expected 2 events, got %d", len(events))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

func TestGetEvents_WithAccountFilter(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	emailConfig := newMockEmailConfig()
	reader := NewEventReader(db, logger, emailConfig)

	now := time.Now()
	rows := sqlmock.NewRows([]string{"id", "timestamp", "activity", "initiator_id", "target_id", "account_id", "meta", "initiator_email", "target_email"}).
		AddRow(int64(1), now, 1, "initiator1", "target1", "account_1", `{"key":"value"}`, "init1@example.com", "target1@example.com")

	// Expect query with WHERE clause for account_id
	mock.ExpectQuery("SELECT e.id, e.timestamp, e.activity, e.initiator_id, e.target_id, e.account_id, e.meta").
		WithArgs("account_1", 10, 0).
		WillReturnRows(rows)

	opts := EventQueryOptions{
		AccountID: "account_1",
		Limit:     10,
	}

	events, err := reader.GetEvents(context.Background(), opts)
	if err != nil {
		t.Fatalf("GetEvents failed: %v", err)
	}

	if len(events) == 0 {
		t.Error("Expected at least one event")
	}

	for _, event := range events {
		if event.AccountID != "account_1" {
			t.Errorf("Expected account_id 'account_1', got '%s'", event.AccountID)
		}
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

func TestGetEvents_WithTimeFilter(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	emailConfig := newMockEmailConfig()
	reader := NewEventReader(db, logger, emailConfig)

	now := time.Now()
	startTime := now.Add(-1 * time.Hour)
	endTime := now.Add(1 * time.Hour)

	rows := sqlmock.NewRows([]string{"id", "timestamp", "activity", "initiator_id", "target_id", "account_id", "meta", "initiator_email", "target_email"}).
		AddRow(int64(1), now, 1, "initiator1", "target1", "account_1", `{"key":"value"}`, "initiator1@example.com", "target1@example.com")

	mock.ExpectQuery("SELECT e.id, e.timestamp, e.activity, e.initiator_id, e.target_id, e.account_id, e.meta").
		WithArgs(startTime, endTime, 10, 0).
		WillReturnRows(rows)

	opts := EventQueryOptions{
		StartTime: &startTime,
		EndTime:   &endTime,
		Limit:     10,
	}

	events, err := reader.GetEvents(context.Background(), opts)
	if err != nil {
		t.Fatalf("GetEvents failed: %v", err)
	}

	if len(events) < 1 {
		t.Error("Expected at least one event within time range")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

func TestGetEvents_WithActivityFilter(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	emailConfig := newMockEmailConfig()
	reader := NewEventReader(db, logger, emailConfig)

	now := time.Now()
	activity := 1

	rows := sqlmock.NewRows([]string{"id", "timestamp", "activity", "initiator_id", "target_id", "account_id", "meta", "initiator_email", "target_email"}).
		AddRow(int64(1), now, 1, "initiator1", "target1", "account_1", `{"key":"value"}`, "initiator1@example.com", "target1@example.com").
		AddRow(int64(2), now, 1, "initiator2", "target2", "account_1", `{"key":"value2"}`, "initiator2@example.com", "target2@example.com")

	mock.ExpectQuery("SELECT e.id, e.timestamp, e.activity, e.initiator_id, e.target_id, e.account_id, e.meta.*WHERE e.activity = \\$1").
		WithArgs(activity, 10, 0).
		WillReturnRows(rows)

	opts := EventQueryOptions{
		Activity: &activity,
		Limit:    10,
	}

	events, err := reader.GetEvents(context.Background(), opts)
	if err != nil {
		t.Fatalf("GetEvents failed: %v", err)
	}

	if len(events) == 0 {
		t.Error("Expected at least one event")
	}

	for _, event := range events {
		if event.Activity != 1 {
			t.Errorf("Expected activity 1, got %d", event.Activity)
		}
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

func TestGetEvents_WithMinEventID(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	emailConfig := newMockEmailConfig()
	reader := NewEventReader(db, logger, emailConfig)

	now := time.Now()
	minEventID := int64(5)

	rows := sqlmock.NewRows([]string{"id", "timestamp", "activity", "initiator_id", "target_id", "account_id", "meta", "initiator_email", "target_email"}).
		AddRow(int64(6), now, 1, "initiator1", "target1", "account_1", `{"key":"value"}`, "initiator1@example.com", "target1@example.com").
		AddRow(int64(7), now, 2, "initiator2", "target2", "account_1", `{"key":"value2"}`, "initiator2@example.com", "target2@example.com")

	mock.ExpectQuery("SELECT e.id, e.timestamp, e.activity, e.initiator_id, e.target_id, e.account_id, e.meta.*WHERE e.id > \\$1 ORDER BY e.timestamp ASC").
		WithArgs(minEventID, 100, 0).
		WillReturnRows(rows)

	opts := EventQueryOptions{
		MinEventID: &minEventID,
		Limit:      100,
		OrderAsc:   true,
	}

	events, err := reader.GetEvents(context.Background(), opts)
	if err != nil {
		t.Fatalf("GetEvents failed: %v", err)
	}

	for _, event := range events {
		if event.ID <= minEventID {
			t.Errorf("Expected event ID > %d, got %d", minEventID, event.ID)
		}
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

func TestGetEvents_WithLimitAndOffset(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	emailConfig := newMockEmailConfig()
	reader := NewEventReader(db, logger, emailConfig)

	now := time.Now()

	rows := sqlmock.NewRows([]string{"id", "timestamp", "activity", "initiator_id", "target_id", "account_id", "meta", "initiator_email", "target_email"}).
		AddRow(int64(6), now, 1, "initiator1", "target1", "account_1", `{"key":"value"}`, "initiator1@example.com", "target1@example.com").
		AddRow(int64(7), now, 2, "initiator2", "target2", "account_1", `{"key":"value2"}`, "initiator2@example.com", "target2@example.com")

	mock.ExpectQuery("SELECT e.id, e.timestamp, e.activity, e.initiator_id, e.target_id, e.account_id, e.meta").
		WithArgs(5, 5). // LIMIT 5 OFFSET 5
		WillReturnRows(rows)

	opts := EventQueryOptions{
		Limit:  5,
		Offset: 5,
	}

	events, err := reader.GetEvents(context.Background(), opts)
	if err != nil {
		t.Fatalf("GetEvents failed: %v", err)
	}

	if len(events) != 2 {
		t.Errorf("Expected 2 events, got %d", len(events))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

func TestGetEvents_DefaultLimit(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	emailConfig := newMockEmailConfig()
	reader := NewEventReader(db, logger, emailConfig)

	rows := sqlmock.NewRows([]string{"id", "timestamp", "activity", "initiator_id", "target_id", "account_id", "meta", "initiator_email", "target_email"})

	// When Limit is 0, it should default to 1000
	mock.ExpectQuery("SELECT e.id, e.timestamp, e.activity, e.initiator_id, e.target_id, e.account_id, e.meta").
		WithArgs(1000, 0). // Default LIMIT 1000 OFFSET 0
		WillReturnRows(rows)

	opts := EventQueryOptions{
		Limit: 0, // Should use default of 1000
	}

	_, err = reader.GetEvents(context.Background(), opts)
	if err != nil {
		t.Fatalf("GetEvents failed: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

func TestGetEvents_NullValues(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	emailConfig := newMockEmailConfig()
	reader := NewEventReader(db, logger, emailConfig)

	now := time.Now()
	rows := sqlmock.NewRows([]string{"id", "timestamp", "activity", "initiator_id", "target_id", "account_id", "meta", "initiator_email", "target_email"}).
		AddRow(int64(1), now, 1, nil, nil, nil, nil, nil, nil) // NULL values

	mock.ExpectQuery("SELECT e.id, e.timestamp, e.activity, e.initiator_id, e.target_id, e.account_id, e.meta").
		WillReturnRows(rows)

	opts := EventQueryOptions{
		Limit: 10,
	}

	events, err := reader.GetEvents(context.Background(), opts)
	if err != nil {
		t.Fatalf("GetEvents failed: %v", err)
	}

	if len(events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(events))
	}

	// NULL values should result in empty strings
	if events[0].InitiatorID != "" {
		t.Errorf("Expected empty InitiatorID for NULL, got '%s'", events[0].InitiatorID)
	}
	if events[0].TargetID != "" {
		t.Errorf("Expected empty TargetID for NULL, got '%s'", events[0].TargetID)
	}
	if events[0].AccountID != "" {
		t.Errorf("Expected empty AccountID for NULL, got '%s'", events[0].AccountID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

func TestGetEventCount(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	emailConfig := newMockEmailConfig()
	reader := NewEventReader(db, logger, emailConfig)

	rows := sqlmock.NewRows([]string{"count"}).AddRow(int64(15))

	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM events").
		WillReturnRows(rows)

	opts := EventQueryOptions{}
	count, err := reader.GetEventCount(context.Background(), opts)
	if err != nil {
		t.Fatalf("GetEventCount failed: %v", err)
	}

	if count != 15 {
		t.Errorf("Expected count 15, got %d", count)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

func TestGetEventCount_WithFilters(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	emailConfig := newMockEmailConfig()
	reader := NewEventReader(db, logger, emailConfig)

	rows := sqlmock.NewRows([]string{"count"}).AddRow(int64(5))

	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM events WHERE account_id = \\$1").
		WithArgs("account_1").
		WillReturnRows(rows)

	opts := EventQueryOptions{
		AccountID: "account_1",
	}
	count, err := reader.GetEventCount(context.Background(), opts)
	if err != nil {
		t.Fatalf("GetEventCount failed: %v", err)
	}

	if count != 5 {
		t.Errorf("Expected count 5, got %d", count)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

func TestGetCheckpoint_NotFound(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	emailConfig := newMockEmailConfig()
	reader := NewEventReader(db, logger, emailConfig)

	// sql.ErrNoRows indicates no checkpoint found (first run)
	mock.ExpectQuery("SELECT consumer_id, last_event_id, last_event_timestamp").
		WithArgs("test-consumer").
		WillReturnError(sql.ErrNoRows)

	checkpoint, err := reader.GetCheckpoint(context.Background(), "test-consumer")
	if err != nil {
		t.Fatalf("GetCheckpoint should not return error on first run: %v", err)
	}

	if checkpoint != nil {
		t.Error("Expected nil checkpoint for first run")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

func TestGetCheckpoint_Found(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	emailConfig := newMockEmailConfig()
	reader := NewEventReader(db, logger, emailConfig)

	now := time.Now()
	rows := sqlmock.NewRows([]string{"consumer_id", "last_event_id", "last_event_timestamp", "total_events_processed", "processing_node", "updated_at", "created_at"}).
		AddRow("test-consumer", int64(100), now, int64(100), "test-node", now, now)

	mock.ExpectQuery("SELECT consumer_id, last_event_id, last_event_timestamp").
		WithArgs("test-consumer").
		WillReturnRows(rows)

	checkpoint, err := reader.GetCheckpoint(context.Background(), "test-consumer")
	if err != nil {
		t.Fatalf("GetCheckpoint failed: %v", err)
	}

	if checkpoint == nil {
		t.Fatal("Expected non-nil checkpoint")
	}

	if checkpoint.ConsumerID != "test-consumer" {
		t.Errorf("Expected consumer_id 'test-consumer', got '%s'", checkpoint.ConsumerID)
	}
	if checkpoint.LastEventID != 100 {
		t.Errorf("Expected last_event_id 100, got %d", checkpoint.LastEventID)
	}
	if checkpoint.TotalEventsProcessed != 100 {
		t.Errorf("Expected total_events_processed 100, got %d", checkpoint.TotalEventsProcessed)
	}
	if checkpoint.ProcessingNode != "test-node" {
		t.Errorf("Expected processing_node 'test-node', got '%s'", checkpoint.ProcessingNode)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

func TestSaveCheckpoint_Insert(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	emailConfig := newMockEmailConfig()
	reader := NewEventReader(db, logger, emailConfig)

	checkpoint := &ProcessingCheckpoint{
		ConsumerID:             "test-consumer",
		LastEventID:            100,
		LastEventTimestamp:     time.Now(),
		TotalEventsProcessed:   100,
		ProcessingNode:         "test-node",
	}

	mock.ExpectExec("INSERT INTO idp.event_processing_checkpoint").
		WithArgs("test-consumer", int64(100), checkpoint.LastEventTimestamp, int64(100), "test-node").
		WillReturnResult(sqlmock.NewResult(1, 1))

	err = reader.SaveCheckpoint(context.Background(), checkpoint)
	if err != nil {
		t.Fatalf("SaveCheckpoint failed: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

func TestSaveCheckpoint_Update(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	emailConfig := newMockEmailConfig()
	reader := NewEventReader(db, logger, emailConfig)

	checkpoint := &ProcessingCheckpoint{
		ConsumerID:             "test-consumer",
		LastEventID:            200,
		LastEventTimestamp:     time.Now(),
		TotalEventsProcessed:   200,
		ProcessingNode:         "test-node-2",
	}

	// ON CONFLICT DO UPDATE
	mock.ExpectExec("INSERT INTO idp.event_processing_checkpoint").
		WithArgs("test-consumer", int64(200), checkpoint.LastEventTimestamp, int64(200), "test-node-2").
		WillReturnResult(sqlmock.NewResult(1, 1))

	err = reader.SaveCheckpoint(context.Background(), checkpoint)
	if err != nil {
		t.Fatalf("SaveCheckpoint failed: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

func TestSaveCheckpoint_Error(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	emailConfig := newMockEmailConfig()
	reader := NewEventReader(db, logger, emailConfig)

	checkpoint := &ProcessingCheckpoint{
		ConsumerID:             "test-consumer",
		LastEventID:            100,
		LastEventTimestamp:     time.Now(),
		TotalEventsProcessed:   100,
		ProcessingNode:         "test-node",
	}

	mock.ExpectExec("INSERT INTO idp.event_processing_checkpoint").
		WithArgs("test-consumer", int64(100), checkpoint.LastEventTimestamp, int64(100), "test-node").
		WillReturnError(sql.ErrConnDone)

	err = reader.SaveCheckpoint(context.Background(), checkpoint)
	if err == nil {
		t.Error("Expected error from SaveCheckpoint when database fails")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

func TestClose(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}

	emailConfig := newMockEmailConfig()
	reader := NewEventReader(db, logger, emailConfig)

	mock.ExpectClose()

	err = reader.Close()
	if err != nil {
		t.Errorf("Close failed: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

func TestGetEvents_QueryError(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	emailConfig := newMockEmailConfig()
	reader := NewEventReader(db, logger, emailConfig)

	mock.ExpectQuery("SELECT e.id, e.timestamp, e.activity, e.initiator_id, e.target_id, e.account_id, e.meta").
		WillReturnError(sql.ErrConnDone)

	opts := EventQueryOptions{
		Limit: 10,
	}

	_, err = reader.GetEvents(context.Background(), opts)
	if err == nil {
		t.Error("Expected error when query fails")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}
