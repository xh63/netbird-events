package config

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestLoadConfig_FromFile(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	// Write test config
	configContent := `
postgres_url: "postgresql://user:pass@localhost:5432/netbird"
platform: "preprod"
region: "emea"
consumer_id: "test-consumer"
log_level: "debug"
batch_size: 500
lookback_hours: 48
polling_interval: 30
`
	if err := os.WriteFile(configFile, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	// Load config
	cfg, err := LoadConfig(configFile)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Verify all fields
	if cfg.PostgresURL != "postgresql://user:pass@localhost:5432/netbird" {
		t.Errorf("Expected PostgresURL='postgresql://user:pass@localhost:5432/netbird', got '%s'", cfg.PostgresURL)
	}
	if cfg.Platform != "preprod" {
		t.Errorf("Expected Platform='preprod', got '%s'", cfg.Platform)
	}
	if cfg.Region != "emea" {
		t.Errorf("Expected Region='emea', got '%s'", cfg.Region)
	}
	if cfg.ConsumerID != "test-consumer" {
		t.Errorf("Expected ConsumerID='test-consumer', got '%s'", cfg.ConsumerID)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("Expected LogLevel='debug', got '%s'", cfg.LogLevel)
	}
	if cfg.BatchSize != 500 {
		t.Errorf("Expected BatchSize=500, got %d", cfg.BatchSize)
	}
	if cfg.LookbackHours != 48 {
		t.Errorf("Expected LookbackHours=48, got %d", cfg.LookbackHours)
	}
	if cfg.PollingInterval != 30 {
		t.Errorf("Expected PollingInterval=30, got %d", cfg.PollingInterval)
	}
}

func TestLoadConfig_FromEnvironment(t *testing.T) {
	// Create temp directory with minimal config
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	configContent := `
postgres_url: "postgresql://fromfile:pass@localhost:5432/netbird"
platform: "sandbox"
region: "apac"
`
	if err := os.WriteFile(configFile, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	// Set environment variables
	t.Setenv("EP_PLATFORM", "prod")
	t.Setenv("EP_REGION", "emea")
	t.Setenv("EP_BATCH_SIZE", "2000")
	t.Setenv("EP_LOG_LEVEL", "error")

	// Load config
	cfg, err := LoadConfig(configFile)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Environment should override file
	if cfg.Platform != "prod" {
		t.Errorf("Expected Platform='prod' (from env), got '%s'", cfg.Platform)
	}
	if cfg.Region != "emea" {
		t.Errorf("Expected Region='emea' (from env), got '%s'", cfg.Region)
	}
	if cfg.BatchSize != 2000 {
		t.Errorf("Expected BatchSize=2000 (from env), got %d", cfg.BatchSize)
	}
	if cfg.LogLevel != "error" {
		t.Errorf("Expected LogLevel='error' (from env), got '%s'", cfg.LogLevel)
	}
}

func TestLoadConfig_Defaults(t *testing.T) {
	// Create temp directory with minimal config (only required fields)
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	configContent := `
postgres_url: "postgresql://user:pass@localhost:5432/netbird"
`
	if err := os.WriteFile(configFile, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	// Load config
	cfg, err := LoadConfig(configFile)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Verify defaults
	if cfg.Platform != "sandbox" {
		t.Errorf("Expected default Platform='sandbox', got '%s'", cfg.Platform)
	}
	if cfg.Region != "apac" {
		t.Errorf("Expected default Region='apac', got '%s'", cfg.Region)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("Expected default LogLevel='info', got '%s'", cfg.LogLevel)
	}
	if cfg.BatchSize != 1000 {
		t.Errorf("Expected default BatchSize=1000, got %d", cfg.BatchSize)
	}
	if cfg.LookbackHours != 24 {
		t.Errorf("Expected default LookbackHours=24, got %d", cfg.LookbackHours)
	}
	if cfg.PollingInterval != 0 {
		t.Errorf("Expected default PollingInterval=0, got %d", cfg.PollingInterval)
	}
	if cfg.ConsumerID == "" {
		t.Error("Expected ConsumerID to be auto-generated, got empty string")
	}
}

func TestLoadConfig_AutoGenerateConsumerID(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	configContent := `
postgres_url: "postgresql://user:pass@localhost:5432/netbird"
platform: "sandbox"
region: "apac"
`
	if err := os.WriteFile(configFile, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	cfg, err := LoadConfig(configFile)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Should be auto-generated as "eventsproc-{platform}-{region}"
	expected := "eventsproc-sandbox-apac"
	if cfg.ConsumerID != expected {
		t.Errorf("Expected ConsumerID='%s', got '%s'", expected, cfg.ConsumerID)
	}
}

func TestLoadConfig_MissingFile(t *testing.T) {
	_, err := LoadConfig("/nonexistent/path/config.yaml")
	if err == nil {
		t.Error("Expected error for missing config file, got nil")
	}
}

func TestLoadConfig_NonExistentFile(t *testing.T) {
	// Non-existent files in directories that don't exist cause an error
	// This is expected behavior - file not found error
	cfg, err := LoadConfig("/non/existent/directory/config.yaml")
	if err == nil {
		t.Error("Expected error for non-existent file in non-existent directory")
	}

	if cfg != nil {
		t.Error("Expected nil config when file cannot be read")
	}
}

func TestLoadConfig_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	// Write invalid YAML
	invalidYAML := `
postgres_url: "test"
platform: [this is not valid yaml
`
	if err := os.WriteFile(configFile, []byte(invalidYAML), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	_, err := LoadConfig(configFile)
	if err == nil {
		t.Error("Expected error for invalid YAML, got nil")
	}
}

func TestLoadConfig_MissingRequiredFields(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	// Missing postgres_url
	configContent := `
platform: "sandbox"
region: "apac"
`
	if err := os.WriteFile(configFile, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	_, err := LoadConfig(configFile)
	if err == nil {
		t.Error("Expected error for missing postgres_url, got nil")
	}
}

func TestLoadConfig_MissingPostgresURL(t *testing.T) {
	_, err := LoadConfig("")
	if err == nil {
		t.Error("Expected error for missing postgres_url")
	}

	if err.Error() != "postgres_url is required when database_driver is not sqlite" {
		t.Errorf("Expected postgres_url required error, got: %v", err)
	}
}

func TestLoadConfig_EnvironmentOverridesFile(t *testing.T) {
	// Create config file
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	configContent := `
postgres_url: "postgresql://file:pass@localhost/filedb"
platform: "sandbox"
batch_size: 500
`

	err := os.WriteFile(configFile, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test config file: %v", err)
	}

	// Set environment variables that should override file
	t.Setenv("EP_PLATFORM", "prod")
	t.Setenv("EP_BATCH_SIZE", "2000")

	cfg, err := LoadConfig(configFile)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Platform should be from env
	if cfg.Platform != "prod" {
		t.Errorf("Expected platform 'prod' from env (not 'sandbox' from file), got %s", cfg.Platform)
	}

	// BatchSize should be from env
	if cfg.BatchSize != 2000 {
		t.Errorf("Expected batch_size 2000 from env (not 500 from file), got %d", cfg.BatchSize)
	}

	// postgres_url should still be from file (not overridden)
	if cfg.PostgresURL != "postgresql://file:pass@localhost/filedb" {
		t.Errorf("Expected postgres_url from file, got %s", cfg.PostgresURL)
	}
}

func TestNewLogFactory_Levels(t *testing.T) {
	tests := []struct {
		name     string
		logLevel string
	}{
		{"debug level", "debug"},
		{"info level", "info"},
		{"warn level", "warn"},
		{"error level", "error"},
		{"default level", "invalid"}, // Should default to info
		{"empty level", ""},          // Should default to info
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{LogLevel: tt.logLevel}
			logger := cfg.NewLogFactory().New("system")

			if logger == nil {
				t.Error("NewLogFactory().New() returned nil")
			}
		})
	}
}

func TestNewLogFactory_DebugLevel(t *testing.T) {
	cfg := &Config{LogLevel: "debug"}
	logger := cfg.NewLogFactory().New("system")

	if logger == nil {
		t.Fatal("Expected non-nil logger")
	}

	if !logger.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("Expected debug level to be enabled")
	}
}

func TestNewLogFactory_InfoLevel(t *testing.T) {
	cfg := &Config{LogLevel: "info"}
	logger := cfg.NewLogFactory().New("system")

	if logger == nil {
		t.Fatal("Expected non-nil logger")
	}

	if !logger.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("Expected info level to be enabled")
	}

	if logger.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("Expected debug level to be disabled for info logger")
	}
}

func TestNewLogFactory_WarnLevel(t *testing.T) {
	cfg := &Config{LogLevel: "warn"}
	logger := cfg.NewLogFactory().New("system")

	if !logger.Enabled(context.Background(), slog.LevelWarn) {
		t.Error("Expected warn level to be enabled")
	}

	if logger.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("Expected info level to be disabled for warn logger")
	}
}

func TestNewLogFactory_ErrorLevel(t *testing.T) {
	cfg := &Config{LogLevel: "error"}
	logger := cfg.NewLogFactory().New("system")

	if !logger.Enabled(context.Background(), slog.LevelError) {
		t.Error("Expected error level to be enabled")
	}

	if logger.Enabled(context.Background(), slog.LevelWarn) {
		t.Error("Expected warn level to be disabled for error logger")
	}
}

func TestNewLogFactory_DefaultLevel(t *testing.T) {
	cfg := &Config{LogLevel: "invalid"}
	logger := cfg.NewLogFactory().New("system")

	// Should default to info
	if !logger.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("Expected info level (default) to be enabled for invalid log level")
	}

	if logger.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("Expected debug level to be disabled for default (info) logger")
	}
}

func TestNewLogFactory_EmptyLevel(t *testing.T) {
	cfg := &Config{LogLevel: ""}
	logger := cfg.NewLogFactory().New("system")

	// Should default to info
	if !logger.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("Expected info level (default) to be enabled for empty log level")
	}
}

// TestNewLogFactory_SharedHandler verifies that two loggers from the same factory
// share the same underlying handler configuration: same level, same output, same format.
//
// Note: slog's .With() wraps the handler in a new object to attach the log_type attribute,
// so handler pointer equality is not meaningful here. What matters is that both loggers
// observe the same level filtering — they draw from the same configured handler.
func TestNewLogFactory_SharedHandler(t *testing.T) {
	cfg := &Config{LogLevel: "warn"}
	factory := cfg.NewLogFactory()

	loggerA := factory.New("system")
	loggerB := factory.New("security")

	ctx := context.Background()

	// Both loggers must filter at the same level (shared handler configuration)
	if loggerA.Enabled(ctx, slog.LevelInfo) != loggerB.Enabled(ctx, slog.LevelInfo) {
		t.Error("Expected loggerA and loggerB to agree on info level (shared handler config)")
	}
	if !loggerA.Enabled(ctx, slog.LevelWarn) || !loggerB.Enabled(ctx, slog.LevelWarn) {
		t.Error("Expected both loggers to have warn level enabled (LogLevel=warn)")
	}
	if loggerA.Enabled(ctx, slog.LevelInfo) || loggerB.Enabled(ctx, slog.LevelInfo) {
		t.Error("Expected both loggers to have info level disabled (LogLevel=warn)")
	}
}

// TestNewLogFactory_DifferentLogTypes verifies that New() produces independent loggers
// whose log_type attributes do not bleed into one another.
func TestNewLogFactory_DifferentLogTypes(t *testing.T) {
	cfg := &Config{LogLevel: "debug"}
	factory := cfg.NewLogFactory()

	systemLogger := factory.New("system")
	securityLogger := factory.New("security")

	// Both must be non-nil and distinct objects
	if systemLogger == nil || securityLogger == nil {
		t.Fatal("Expected non-nil loggers from factory")
	}
	if systemLogger == securityLogger {
		t.Error("Expected factory.New() to return distinct *slog.Logger instances")
	}
}

// TestNewLogFactory_ImplementsInterface is a compile-time check that
// defaultLogFactory satisfies the LogFactory interface.
func TestNewLogFactory_ImplementsInterface(_ *testing.T) {
	var _ LogFactory = (*defaultLogFactory)(nil) // compile-time interface check
}

func TestGetDB_InvalidConnectionString(t *testing.T) {
	cfg := &Config{LogLevel: "info"}
	logger := cfg.NewLogFactory().New("system")

	// Invalid connection string
	_, err := GetDB("invalid-connection-string", logger)
	if err == nil {
		t.Error("Expected error for invalid connection string, got nil")
	}
}

// TestCheckpointTable_Insert tests inserting a checkpoint record using sqlmock
func TestCheckpointTable_Insert(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Test data matching idp.event_processing_checkpoint schema
	consumerID := "eventsproc-sandbox-apac"
	lastEventID := int64(12345)
	lastEventTimestamp := time.Now()
	totalEventsProcessed := int64(1000)
	processingNode := "node-01.example.com"

	// Expect INSERT query
	mock.ExpectExec(`INSERT INTO idp\.event_processing_checkpoint`).
		WithArgs(consumerID, lastEventID, lastEventTimestamp, totalEventsProcessed, processingNode).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Execute insert
	_, err = db.Exec(
		`INSERT INTO idp.event_processing_checkpoint
		(consumer_id, last_event_id, last_event_timestamp, total_events_processed, processing_node)
		VALUES ($1, $2, $3, $4, $5)`,
		consumerID, lastEventID, lastEventTimestamp, totalEventsProcessed, processingNode,
	)
	if err != nil {
		t.Errorf("Insert failed: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

// TestCheckpointTable_Select tests querying a checkpoint record using sqlmock
func TestCheckpointTable_Select(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Expected data
	consumerID := "eventsproc-prod-emea"
	lastEventID := int64(99999)
	lastEventTimestamp := time.Now()
	totalEventsProcessed := int64(50000)
	processingNode := "prod-node-02"
	updatedAt := time.Now()
	createdAt := time.Now().Add(-24 * time.Hour)

	// Create expected rows with all columns from the schema
	rows := sqlmock.NewRows([]string{
		"consumer_id", "last_event_id", "last_event_timestamp",
		"total_events_processed", "processing_node", "updated_at", "created_at",
	}).AddRow(
		consumerID, lastEventID, lastEventTimestamp,
		totalEventsProcessed, processingNode, updatedAt, createdAt,
	)

	// Expect SELECT query
	mock.ExpectQuery(`SELECT .+ FROM idp\.event_processing_checkpoint WHERE consumer_id = \$1`).
		WithArgs(consumerID).
		WillReturnRows(rows)

	// Execute query
	row := db.QueryRow(
		`SELECT consumer_id, last_event_id, last_event_timestamp,
		total_events_processed, processing_node, updated_at, created_at
		FROM idp.event_processing_checkpoint WHERE consumer_id = $1`,
		consumerID,
	)

	// Scan results
	var gotConsumerID, gotProcessingNode string
	var gotLastEventID, gotTotalEventsProcessed int64
	var gotLastEventTimestamp, gotUpdatedAt, gotCreatedAt time.Time

	err = row.Scan(
		&gotConsumerID, &gotLastEventID, &gotLastEventTimestamp,
		&gotTotalEventsProcessed, &gotProcessingNode, &gotUpdatedAt, &gotCreatedAt,
	)
	if err != nil {
		t.Errorf("Scan failed: %v", err)
	}

	// Verify all fields
	if gotConsumerID != consumerID {
		t.Errorf("consumer_id: expected %s, got %s", consumerID, gotConsumerID)
	}
	if gotLastEventID != lastEventID {
		t.Errorf("last_event_id: expected %d, got %d", lastEventID, gotLastEventID)
	}
	if gotTotalEventsProcessed != totalEventsProcessed {
		t.Errorf("total_events_processed: expected %d, got %d", totalEventsProcessed, gotTotalEventsProcessed)
	}
	if gotProcessingNode != processingNode {
		t.Errorf("processing_node: expected %s, got %s", processingNode, gotProcessingNode)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

// TestCheckpointTable_Update tests updating a checkpoint record using sqlmock
func TestCheckpointTable_Update(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	consumerID := "eventsproc-preprod-apac"
	newLastEventID := int64(55555)
	newLastEventTimestamp := time.Now()
	newTotalEventsProcessed := int64(2500)
	newProcessingNode := "preprod-node-01"

	// Expect UPDATE query
	mock.ExpectExec(`UPDATE idp\.event_processing_checkpoint SET`).
		WithArgs(newLastEventID, newLastEventTimestamp, newTotalEventsProcessed, newProcessingNode, consumerID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	// Execute update
	result, err := db.Exec(
		`UPDATE idp.event_processing_checkpoint SET
		last_event_id = $1, last_event_timestamp = $2,
		total_events_processed = $3, processing_node = $4,
		updated_at = NOW()
		WHERE consumer_id = $5`,
		newLastEventID, newLastEventTimestamp, newTotalEventsProcessed, newProcessingNode, consumerID,
	)
	if err != nil {
		t.Errorf("Update failed: %v", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		t.Errorf("RowsAffected failed: %v", err)
	}
	if rowsAffected != 1 {
		t.Errorf("Expected 1 row affected, got %d", rowsAffected)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

// TestCheckpointTable_Upsert tests upsert (INSERT ON CONFLICT) using sqlmock
func TestCheckpointTable_Upsert(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	consumerID := "eventsproc-sandbox-emea"
	lastEventID := int64(77777)
	lastEventTimestamp := time.Now()
	totalEventsProcessed := int64(3000)
	processingNode := "sandbox-node-03"

	// Expect UPSERT query (INSERT ... ON CONFLICT)
	mock.ExpectExec(`INSERT INTO idp\.event_processing_checkpoint .+ ON CONFLICT \(consumer_id\) DO UPDATE`).
		WithArgs(consumerID, lastEventID, lastEventTimestamp, totalEventsProcessed, processingNode).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Execute upsert
	_, err = db.Exec(
		`INSERT INTO idp.event_processing_checkpoint
		(consumer_id, last_event_id, last_event_timestamp, total_events_processed, processing_node)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (consumer_id) DO UPDATE SET
		last_event_id = EXCLUDED.last_event_id,
		last_event_timestamp = EXCLUDED.last_event_timestamp,
		total_events_processed = EXCLUDED.total_events_processed,
		processing_node = EXCLUDED.processing_node,
		updated_at = NOW()`,
		consumerID, lastEventID, lastEventTimestamp, totalEventsProcessed, processingNode,
	)
	if err != nil {
		t.Errorf("Upsert failed: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

// TestCheckpointTable_SelectNotFound tests querying non-existent checkpoint
func TestCheckpointTable_SelectNotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	consumerID := "nonexistent-consumer"

	// Return empty result set
	rows := sqlmock.NewRows([]string{
		"consumer_id", "last_event_id", "last_event_timestamp",
		"total_events_processed", "processing_node", "updated_at", "created_at",
	})

	mock.ExpectQuery(`SELECT .+ FROM idp\.event_processing_checkpoint WHERE consumer_id = \$1`).
		WithArgs(consumerID).
		WillReturnRows(rows)

	// Execute query
	row := db.QueryRow(
		`SELECT consumer_id, last_event_id, last_event_timestamp,
		total_events_processed, processing_node, updated_at, created_at
		FROM idp.event_processing_checkpoint WHERE consumer_id = $1`,
		consumerID,
	)

	var gotConsumerID string
	var gotLastEventID int64
	var gotLastEventTimestamp time.Time
	var gotTotalEventsProcessed int64
	var gotProcessingNode string
	var gotUpdatedAt, gotCreatedAt time.Time

	err = row.Scan(
		&gotConsumerID, &gotLastEventID, &gotLastEventTimestamp,
		&gotTotalEventsProcessed, &gotProcessingNode, &gotUpdatedAt, &gotCreatedAt,
	)

	// Should get sql.ErrNoRows
	if err == nil {
		t.Error("Expected error for non-existent record, got nil")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

// TestCheckpointTable_Delete tests deleting a checkpoint record
func TestCheckpointTable_Delete(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	consumerID := "eventsproc-old-consumer"

	mock.ExpectExec(`DELETE FROM idp\.event_processing_checkpoint WHERE consumer_id = \$1`).
		WithArgs(consumerID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	result, err := db.Exec(
		`DELETE FROM idp.event_processing_checkpoint WHERE consumer_id = $1`,
		consumerID,
	)
	if err != nil {
		t.Errorf("Delete failed: %v", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		t.Errorf("RowsAffected failed: %v", err)
	}
	if rowsAffected != 1 {
		t.Errorf("Expected 1 row deleted, got %d", rowsAffected)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

// TestCheckpointTable_NullableProcessingNode tests that processing_node can be NULL
func TestCheckpointTable_NullableProcessingNode(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	consumerID := "eventsproc-no-node"
	lastEventID := int64(11111)
	lastEventTimestamp := time.Now()
	totalEventsProcessed := int64(100)

	// Insert with NULL processing_node
	mock.ExpectExec(`INSERT INTO idp\.event_processing_checkpoint`).
		WithArgs(consumerID, lastEventID, lastEventTimestamp, totalEventsProcessed, nil).
		WillReturnResult(sqlmock.NewResult(1, 1))

	_, err = db.Exec(
		`INSERT INTO idp.event_processing_checkpoint
		(consumer_id, last_event_id, last_event_timestamp, total_events_processed, processing_node)
		VALUES ($1, $2, $3, $4, $5)`,
		consumerID, lastEventID, lastEventTimestamp, totalEventsProcessed, nil,
	)
	if err != nil {
		t.Errorf("Insert with NULL processing_node failed: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

func TestConfig_AllFieldsPresent(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	configContent := `
postgres_url: "postgresql://user:pass@localhost:5432/netbird"
platform: "prod"
region: "emea"
consumer_id: "eventsproc-prod-emea"
log_level: "info"
batch_size: 5000
lookback_hours: 1
polling_interval: 300
`
	if err := os.WriteFile(configFile, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	cfg, err := LoadConfig(configFile)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Verify production config
	if cfg.Platform != "prod" {
		t.Errorf("Expected Platform='prod', got '%s'", cfg.Platform)
	}
	if cfg.Region != "emea" {
		t.Errorf("Expected Region='emea', got '%s'", cfg.Region)
	}
	if cfg.BatchSize != 5000 {
		t.Errorf("Expected BatchSize=5000, got %d", cfg.BatchSize)
	}
	if cfg.LookbackHours != 1 {
		t.Errorf("Expected LookbackHours=1, got %d", cfg.LookbackHours)
	}
	if cfg.PollingInterval != 300 {
		t.Errorf("Expected PollingInterval=300, got %d", cfg.PollingInterval)
	}
}

func TestLoadConfig_WithContext(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	configContent := `
postgres_url: "postgresql://user:pass@localhost:5432/netbird"
`
	if err := os.WriteFile(configFile, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	// Test that config can be loaded with context
	cfg, err := LoadConfig(configFile)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Create context with logger
	ctx := context.Background()
	logger := cfg.NewLogFactory().New("system")
	if logger == nil {
		t.Error("NewLogFactory().New() returned nil")
	}

	// Test logger can be used with context (just verify it doesn't panic)
	logger.InfoContext(ctx, "Test log message")
}
