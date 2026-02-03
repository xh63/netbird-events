package config

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"

	_ "github.com/lib/pq" // PostgreSQL driver
	"github.com/spf13/viper"
)

// EmailEnrichmentConfig holds configuration for email enrichment
type EmailEnrichmentConfig struct {
	// Enable email enrichment (default: true)
	Enabled bool `mapstructure:"enabled"`

	// Source: "auto", "idp_okta_users", "netbird_users", "custom", "none"
	// Default: "auto" (try okta_users first, fallback to users table, then user_id)
	Source string `mapstructure:"source"`

	// Custom table/schema (only used if source = "custom")
	CustomSchema string `mapstructure:"custom_schema"`
	CustomTable  string `mapstructure:"custom_table"`
}

// GetSource returns the email enrichment source
func (e *EmailEnrichmentConfig) GetSource() string {
	if !e.Enabled {
		return "none"
	}
	return e.Source
}

// GetCustomSchema returns the custom schema name
func (e *EmailEnrichmentConfig) GetCustomSchema() string {
	return e.CustomSchema
}

// GetCustomTable returns the custom table name
func (e *EmailEnrichmentConfig) GetCustomTable() string {
	return e.CustomTable
}

// IsEnabled returns whether email enrichment is enabled
func (e *EmailEnrichmentConfig) IsEnabled() bool {
	return e.Enabled
}

// Config holds configuration for the events processor
type Config struct {
	// Database connection string
	PostgresURL string `mapstructure:"postgres_url"`

	// Platform (sandbox, preprod, prod)
	Platform string `mapstructure:"platform"`

	// Region (apac, emea)
	Region string `mapstructure:"region"`

	// Consumer ID for checkpoint tracking (auto-generated if empty)
	ConsumerID string `mapstructure:"consumer_id"`

	// Log level (debug, info, warn, error)
	LogLevel string `mapstructure:"log_level"`

	// Batch size for fetching events
	BatchSize int `mapstructure:"batch_size"`

	// How far back to process events (in hours, 0 = all events)
	LookbackHours int `mapstructure:"lookback_hours"`

	// Polling interval in seconds (0 = run once and exit)
	PollingInterval int `mapstructure:"polling_interval"`

	// Email enrichment configuration
	EmailEnrichment EmailEnrichmentConfig `mapstructure:"email_enrichment"`
}

// LoadConfig loads configuration from file and environment variables
// Environment variables have the EP_ prefix (e.g., EP_POSTGRES_URL)
func LoadConfig(configFile string) (*Config, error) {
	v := viper.New()

	// Set defaults
	v.SetDefault("batch_size", 1000)
	v.SetDefault("lookback_hours", 24)
	v.SetDefault("polling_interval", 0)
	v.SetDefault("log_level", "info")
	v.SetDefault("platform", "sandbox")
	v.SetDefault("region", "apac")

	// Email enrichment defaults
	v.SetDefault("email_enrichment.enabled", true)
	v.SetDefault("email_enrichment.source", "auto")

	// Load from config file if it exists
	if configFile != "" {
		v.SetConfigFile(configFile)
		if err := v.ReadInConfig(); err != nil {
			if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
				return nil, fmt.Errorf("failed to read config file: %w", err)
			}
		}
	}

	// Environment variables override config file
	v.SetEnvPrefix("EP") // Events Processor
	v.AutomaticEnv()

	// Bind specific environment variables (errors are intentionally ignored as BindEnv only fails for empty keys)
	_ = v.BindEnv("postgres_url")
	_ = v.BindEnv("platform")
	_ = v.BindEnv("region")
	_ = v.BindEnv("consumer_id")
	_ = v.BindEnv("log_level")
	_ = v.BindEnv("batch_size")
	_ = v.BindEnv("lookback_hours")
	_ = v.BindEnv("polling_interval")

	// Email enrichment environment variables
	_ = v.BindEnv("email_enrichment.enabled")
	_ = v.BindEnv("email_enrichment.source")
	_ = v.BindEnv("email_enrichment.custom_schema")
	_ = v.BindEnv("email_enrichment.custom_table")

	var config Config
	if err := v.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Validate required fields
	if config.PostgresURL == "" {
		return nil, fmt.Errorf("postgres_url is required")
	}

	// Auto-generate consumer_id if not provided
	if config.ConsumerID == "" {
		config.ConsumerID = fmt.Sprintf("eventsproc-%s-%s", config.Platform, config.Region)
	}

	return &config, nil
}

// GetLogger creates a logger based on the log level in config
// Output is JSON format with timestamp as the first field
func (c *Config) GetLogger() *slog.Logger {
	var level slog.Level
	switch c.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	// Use JSON handler for structured logging
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	})

	return slog.New(handler)
}

// GetDB creates a database connection
// Uses the same logic as logsproc for consistency
func GetDB(postgresURL string, logger *slog.Logger) (*sql.DB, error) {
	db, err := sql.Open("postgres", postgresURL)
	if err != nil {
		logger.Error("Error connecting to database", "error", err)
		return nil, err
	}

	// Test connection
	if err = db.Ping(); err != nil {
		logger.Error("Error pinging database", "error", err)
		return nil, err
	}

	logger.Debug("Connected to database")
	return db, nil
}
