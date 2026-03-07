package config

import (
	"database/sql"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"

	_ "github.com/lib/pq" // PostgreSQL driver
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
	_ "modernc.org/sqlite" // SQLite driver (pure Go, no CGO)
)

// EmailEnrichmentConfig holds configuration for email enrichment
type EmailEnrichmentConfig struct {
	// Enable email enrichment (default: true)
	Enabled bool `mapstructure:"enabled"`

	// Source: "auto", "idp_okta_users", "netbird_users", "custom", "none"
	// Default: "none" — NetBird encrypts name/email in the users table, so joins yield
	// encrypted blobs rather than plain addresses. Enable and set a source only when
	// you have a separate, unencrypted users table to enrich against.
	Source string `mapstructure:"source"`

	// Custom table/schema (only used if source = "custom")
	CustomSchema string `mapstructure:"custom_schema"`
	CustomTable  string `mapstructure:"custom_table"`

	// NetbirdEncryptionKey is the base64-encoded AES-256 key NetBird uses to
	// encrypt sensitive database columns (email, name). Found at
	// server.store.encryptionKey in NetBird's config.yaml.
	// Option A: supply the key directly in this config.
	NetbirdEncryptionKey string `mapstructure:"netbird_encryption_key"`

	// NetbirdConfigPath is the filesystem path to NetBird's config.yaml.
	// Option B: eventsproc reads server.store.encryptionKey from that file
	// automatically. Takes priority over NetbirdEncryptionKey when both are set.
	NetbirdConfigPath string `mapstructure:"netbird_config_path"`
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

// GetDecryptionKey resolves the AES-256 key used to decrypt NetBird's encrypted
// database columns. NetbirdConfigPath (Option B) takes priority over
// NetbirdEncryptionKey (Option A). Returns nil if neither is configured,
// meaning no decryption will be attempted.
func (e *EmailEnrichmentConfig) GetDecryptionKey() ([]byte, error) {
	// Option B: read key from NetBird's config.yaml (single source of truth)
	if e.NetbirdConfigPath != "" {
		return readEncryptionKeyFromNetbirdConfig(e.NetbirdConfigPath)
	}
	// Option A: key supplied directly in eventsproc config
	if e.NetbirdEncryptionKey != "" {
		key, err := base64.StdEncoding.DecodeString(e.NetbirdEncryptionKey)
		if err != nil {
			return nil, fmt.Errorf("invalid netbird_encryption_key (must be base64): %w", err)
		}
		return key, nil
	}
	return nil, nil // no decryption configured
}

// netbirdConfigFile is the minimal subset of NetBird's config.yaml structure
// needed to extract the store encryption key.
type netbirdConfigFile struct {
	Server struct {
		Store struct {
			EncryptionKey string `yaml:"encryptionKey"`
		} `yaml:"store"`
	} `yaml:"server"`
}

// readEncryptionKeyFromNetbirdConfig parses a NetBird config.yaml file and
// returns the decoded AES-256 store encryption key.
func readEncryptionKeyFromNetbirdConfig(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read netbird config %q: %w", path, err)
	}
	var cfg netbirdConfigFile
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse netbird config %q: %w", path, err)
	}
	if cfg.Server.Store.EncryptionKey == "" {
		return nil, fmt.Errorf("server.store.encryptionKey not found in %q", path)
	}
	key, err := base64.StdEncoding.DecodeString(cfg.Server.Store.EncryptionKey)
	if err != nil {
		return nil, fmt.Errorf("invalid encryptionKey in %q (must be base64): %w", path, err)
	}
	return key, nil
}

// ClusterConfig holds configuration for HA cluster mode using a Redis distributed lock.
// When enabled, only the node that holds the Redis lock runs the processor.
// If the leader crashes, the lock expires after LockTTL seconds and another node takes over.
type ClusterConfig struct {
	// Enable cluster mode (default: false)
	Enabled bool `mapstructure:"enabled"`

	// RedisURL is the Redis address for the distributed lock.
	// Accepts redis:// URLs or plain host:port.
	// Example: "redis.example.com:6379"
	RedisURL string `mapstructure:"redis_url"`

	// LockTTL is the lock lease duration in seconds (default: 15).
	// If the leader crashes without releasing the lock, it expires after this duration.
	LockTTL int `mapstructure:"lock_ttl"`

	// LockRetryInterval is how often standby nodes poll for the lock in seconds (default: 5).
	LockRetryInterval int `mapstructure:"lock_retry_interval"`
}

// Config holds configuration for the events processor
type Config struct {
	// DatabaseDriver selects the backend: "postgres" (default) or "sqlite"
	DatabaseDriver string `mapstructure:"database_driver"`

	// Database connection string (required when DatabaseDriver = "postgres" or unset)
	PostgresURL string `mapstructure:"postgres_url"`

	// SQLitePath is the path to the NetBird SQLite store (default: /var/lib/netbird/store.db)
	// Only used when DatabaseDriver = "sqlite"
	SQLitePath string `mapstructure:"sqlite_path"`

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

	// MetricsPort is the port for the Prometheus /metrics endpoint (default: 2113)
	MetricsPort int `mapstructure:"metrics_port"`

	// Cluster configuration for HA mode
	Cluster ClusterConfig `mapstructure:"cluster"`
}

// LoadConfig loads configuration from file and environment variables
// Environment variables have the EP_ prefix (e.g., EP_POSTGRES_URL)
func LoadConfig(configFile string) (*Config, error) {
	v := viper.New()

	// Set defaults
	v.SetDefault("database_driver", "postgres")
	v.SetDefault("sqlite_path", "/var/lib/netbird/store.db")
	v.SetDefault("batch_size", 1000)
	v.SetDefault("lookback_hours", 24)
	v.SetDefault("polling_interval", 0)
	v.SetDefault("log_level", "info")
	v.SetDefault("platform", "sandbox")
	v.SetDefault("region", "apac")

	// Email enrichment defaults — off by default because NetBird encrypts the
	// users table; joining it yields encrypted blobs, not plain email addresses.
	v.SetDefault("email_enrichment.enabled", false)
	v.SetDefault("email_enrichment.source", "none")

	// Metrics defaults
	v.SetDefault("metrics_port", 2113)

	// Cluster defaults
	v.SetDefault("cluster.enabled", false)
	v.SetDefault("cluster.lock_ttl", 15)
	v.SetDefault("cluster.lock_retry_interval", 5)

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
	_ = v.BindEnv("database_driver")
	_ = v.BindEnv("sqlite_path")
	_ = v.BindEnv("postgres_url")
	_ = v.BindEnv("platform")
	_ = v.BindEnv("region")
	_ = v.BindEnv("consumer_id")
	_ = v.BindEnv("log_level")
	_ = v.BindEnv("batch_size")
	_ = v.BindEnv("lookback_hours")
	_ = v.BindEnv("polling_interval")
	_ = v.BindEnv("metrics_port")

	// Email enrichment environment variables
	_ = v.BindEnv("email_enrichment.enabled")
	_ = v.BindEnv("email_enrichment.source")
	_ = v.BindEnv("email_enrichment.custom_schema")
	_ = v.BindEnv("email_enrichment.custom_table")
	_ = v.BindEnv("email_enrichment.netbird_encryption_key")
	_ = v.BindEnv("email_enrichment.netbird_config_path")

	// Cluster environment variables
	_ = v.BindEnv("cluster.enabled")
	_ = v.BindEnv("cluster.redis_url")
	_ = v.BindEnv("cluster.lock_ttl")
	_ = v.BindEnv("cluster.lock_retry_interval")

	var config Config
	if err := v.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Validate required fields
	if config.DatabaseDriver != "sqlite" && config.PostgresURL == "" {
		return nil, fmt.Errorf("postgres_url is required when database_driver is not sqlite")
	}

	// Auto-generate consumer_id if not provided
	if config.ConsumerID == "" {
		config.ConsumerID = fmt.Sprintf("eventsproc-%s-%s", config.Platform, config.Region)
	}

	return &config, nil
}

// LogFactory creates typed loggers from a shared handler.
// Using an interface allows mocking in tests and decouples log creation from
// consumption: any component calls factory.New("security"), factory.New("audit"),
// etc. at runtime without requiring new constructor parameters per log type.
type LogFactory interface {
	New(logType string) *slog.Logger
}

// defaultLogFactory is the production implementation of LogFactory.
// All loggers it produces share the same slog.Handler — same output, same level,
// same format — only the log_type attribute differs.
type defaultLogFactory struct {
	handler slog.Handler
}

func (f *defaultLogFactory) New(logType string) *slog.Logger {
	return slog.New(f.handler).With("log_type", logType)
}

// NewLogFactory builds a LogFactory from the config's log level setting.
// The handler is created once and shared across all loggers the factory produces.
func (c *Config) NewLogFactory() LogFactory {
	var level slog.Level
	switch c.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	return &defaultLogFactory{handler: handler}
}

// GetDB creates a PostgreSQL database connection
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

// GetSQLiteDB opens a SQLite database at the given path
func GetSQLiteDB(path string, logger *slog.Logger) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		logger.Error("Error opening SQLite database", "path", path, "error", err)
		return nil, err
	}

	if err = db.Ping(); err != nil {
		logger.Error("Error pinging SQLite database", "path", path, "error", err)
		return nil, err
	}

	logger.Debug("Connected to SQLite database", "path", path)
	return db, nil
}
