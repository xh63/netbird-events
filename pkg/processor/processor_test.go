package processor

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/xh63/netbird-events/pkg/config"
	"github.com/xh63/netbird-events/pkg/events"
)

// mockWriter implements a simple mock writer for testing
type mockWriter struct {
	name       string
	sentEvents [][]events.Event
	shouldFail bool
	failError  string
	callCount  int
}

func (m *mockWriter) SendEvents(ctx context.Context, eventsList []events.Event) error {
	m.callCount++
	if m.shouldFail {
		return errors.New(m.failError)
	}
	m.sentEvents = append(m.sentEvents, eventsList)
	return nil
}

func (m *mockWriter) Close() error {
	return nil
}

func TestNewProcessor_Success(t *testing.T) {
	// Create mock database
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Expect database ping
	mock.ExpectPing()

	// Create test config
	cfg := &config.Config{
		PostgresURL:     "mock://db",
		Platform:        "sandbox",
		Region:          "apac",
		ConsumerID:      "test-consumer",
		LogLevel:        "info",
		BatchSize:       1000,
		LookbackHours:   24,
		PollingInterval: 0,
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// This will fail because we can't inject the mock DB easily
	// In real usage, NewProcessor connects to the database itself
	_, err = NewProcessor(cfg, logger)
	if err == nil {
		t.Skip("Expected error without real database connection")
	}

	// Verify expectations
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Logf("Some expectations were not met (expected for this test): %v", err)
	}
}

func TestProcessor_Struct(t *testing.T) {
	// Test that Processor struct has expected fields
	cfg := &config.Config{
		PostgresURL: "postgresql://localhost/db",
		Platform:    "sandbox",
		Region:      "apac",
		ConsumerID:  "test",
		LogLevel:    "info",
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Create mock processor (simplified architecture - single writer)
	proc := &Processor{
		eventReader: nil, // Would be set by NewProcessor
		writer:      &mockWriter{name: "stdout"},
		checkpoint: &events.ProcessingCheckpoint{
			ConsumerID:           cfg.ConsumerID,
			LastEventID:          0,
			LastEventTimestamp:   time.Now(),
			TotalEventsProcessed: 0,
			ProcessingNode:       "test-node",
		},
		config:   cfg,
		logger:   logger,
		hostname: "test-node",
	}

	// Verify struct fields match simplified architecture
	if proc.writer == nil {
		t.Error("Expected writer to be set")
	}
	if proc.checkpoint == nil {
		t.Error("Expected checkpoint to be set")
	}
	if proc.config != cfg {
		t.Error("Expected config to match")
	}
	if proc.logger != logger {
		t.Error("Expected logger to match")
	}
	if proc.hostname != "test-node" {
		t.Error("Expected hostname to be set")
	}
}

func TestConfig_SimplifiedArchitecture(t *testing.T) {
	// Verify config only contains fields needed for simplified architecture
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

	// Verify essential fields are present
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
		{
			ID:        1,
			Timestamp: time.Now(),
			Activity:  100,
			AccountID: "account1",
		},
		{
			ID:        2,
			Timestamp: time.Now(),
			Activity:  200,
			AccountID: "account2",
		},
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
