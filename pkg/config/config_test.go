package config

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
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

	if err.Error() != "postgres_url is required" {
		t.Errorf("Expected 'postgres_url is required' error, got: %v", err)
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

func TestGetLogger_Levels(t *testing.T) {
	tests := []struct {
		name     string
		logLevel string
		wantErr  bool
	}{
		{"debug level", "debug", false},
		{"info level", "info", false},
		{"warn level", "warn", false},
		{"error level", "error", false},
		{"default level", "invalid", false}, // Should default to info
		{"empty level", "", false},          // Should default to info
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{LogLevel: tt.logLevel}
			logger := cfg.GetLogger()

			if logger == nil {
				t.Error("GetLogger returned nil")
			}
		})
	}
}

func TestGetLogger_DebugLevel(t *testing.T) {
	cfg := &Config{LogLevel: "debug"}
	logger := cfg.GetLogger()

	if logger == nil {
		t.Fatal("Expected non-nil logger")
	}

	if !logger.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("Expected debug level to be enabled")
	}
}

func TestGetLogger_InfoLevel(t *testing.T) {
	cfg := &Config{LogLevel: "info"}
	logger := cfg.GetLogger()

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

func TestGetLogger_WarnLevel(t *testing.T) {
	cfg := &Config{LogLevel: "warn"}
	logger := cfg.GetLogger()

	if !logger.Enabled(context.Background(), slog.LevelWarn) {
		t.Error("Expected warn level to be enabled")
	}

	if logger.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("Expected info level to be disabled for warn logger")
	}
}

func TestGetLogger_ErrorLevel(t *testing.T) {
	cfg := &Config{LogLevel: "error"}
	logger := cfg.GetLogger()

	if !logger.Enabled(context.Background(), slog.LevelError) {
		t.Error("Expected error level to be enabled")
	}

	if logger.Enabled(context.Background(), slog.LevelWarn) {
		t.Error("Expected warn level to be disabled for error logger")
	}
}

func TestGetLogger_DefaultLevel(t *testing.T) {
	cfg := &Config{LogLevel: "invalid"}
	logger := cfg.GetLogger()

	// Should default to info
	if !logger.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("Expected info level (default) to be enabled for invalid log level")
	}

	if logger.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("Expected debug level to be disabled for default (info) logger")
	}
}

func TestGetLogger_EmptyLevel(t *testing.T) {
	cfg := &Config{LogLevel: ""}
	logger := cfg.GetLogger()

	// Should default to info
	if !logger.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("Expected info level (default) to be enabled for empty log level")
	}
}

func TestGetDB_Success(t *testing.T) {
	// This test requires a real database or mock
	// Skip if no database available
	t.Skip("Skipping database connection test - requires PostgreSQL")
}

func TestGetDB_InvalidConnectionString(t *testing.T) {
	cfg := &Config{LogLevel: "info"}
	logger := cfg.GetLogger()

	// Invalid connection string
	_, err := GetDB("invalid-connection-string", logger)
	if err == nil {
		t.Error("Expected error for invalid connection string, got nil")
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
	logger := cfg.GetLogger()
	if logger == nil {
		t.Error("GetLogger returned nil")
	}

	// Test logger can be used with context (just verify it doesn't panic)
	logger.InfoContext(ctx, "Test log message")
}
