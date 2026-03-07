package events

import "context"

// ReaderInterface defines the interface for reading events from the database
type ReaderInterface interface {
	// GetEvents fetches events from the database based on query options
	GetEvents(ctx context.Context, opts EventQueryOptions) ([]Event, error)

	// GetEventCount returns the total count of events matching the query options
	GetEventCount(ctx context.Context, opts EventQueryOptions) (int64, error)

	// GetCheckpoint retrieves the last processing checkpoint for a consumer (deprecated: use GetWriterCheckpoint)
	GetCheckpoint(ctx context.Context, consumerID string) (*ProcessingCheckpoint, error)

	// SaveCheckpoint saves or updates the processing checkpoint for a consumer (deprecated: use SaveWriterCheckpoint)
	SaveCheckpoint(ctx context.Context, checkpoint *ProcessingCheckpoint) error

	// GetWriterCheckpoint retrieves the checkpoint for a specific consumer/writer combination
	GetWriterCheckpoint(ctx context.Context, consumerID, writerType string) (*ProcessingCheckpoint, error)

	// SaveWriterCheckpoint saves or updates the checkpoint for a specific consumer/writer
	SaveWriterCheckpoint(ctx context.Context, checkpoint *ProcessingCheckpoint) error

	// Close closes the database connection
	Close() error
}

// EmailEnrichmentConfig is used by reader implementations to avoid circular dependency
type EmailEnrichmentConfig interface {
	GetSource() string
	GetCustomSchema() string
	GetCustomTable() string
	IsEnabled() bool
	// GetDecryptionKey returns the raw AES-256 key for decrypting NetBird's
	// encrypted database columns, or nil if decryption is not configured.
	GetDecryptionKey() ([]byte, error)
}
