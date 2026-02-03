package events

import (
	"time"
)

// Event represents a NetBird event from the events table
type Event struct {
	ID             int64     `json:"id"`
	Timestamp      time.Time `json:"timestamp"`
	Activity       int       `json:"activity"`
	ActivityName   string    `json:"activity_name"`   // Human-readable activity name (e.g., "User logged in peer")
	ActivityCode   string    `json:"activity_code"`   // Machine-readable code (e.g., "user.peer.login")
	InitiatorID    string    `json:"initiator_id"`
	TargetID       string    `json:"target_id"`
	AccountID      string    `json:"account_id"`
	Meta           string    `json:"meta"` // JSON string containing additional metadata
	InitiatorEmail string    `json:"initiator_email"`
	TargetEmail    string    `json:"target_email"`
}

// EventQueryOptions defines parameters for querying events
type EventQueryOptions struct {
	// Limit the number of events to fetch (default: 1000)
	Limit int

	// Offset for pagination
	Offset int

	// Filter by account ID
	AccountID string

	// Filter by time range
	StartTime *time.Time
	EndTime   *time.Time

	// Filter by activity type
	Activity *int

	// Filter by minimum event ID (for resuming from checkpoint)
	MinEventID *int64

	// Order by timestamp (default: DESC)
	OrderAsc bool
}

// ProcessingCheckpoint tracks the last processed event for a consumer/writer combination
type ProcessingCheckpoint struct {
	// ConsumerID uniquely identifies the consumer group (e.g., "eventsproc-sandbox-apac")
	ConsumerID string

	// WriterType identifies the output writer (e.g., "journal", "loki", "splunk")
	// Each writer tracks its own checkpoint independently
	WriterType string

	// LastEventID is the ID of the last successfully processed event
	LastEventID int64

	// LastEventTimestamp is the timestamp of the last processed event
	LastEventTimestamp time.Time

	// TotalEventsProcessed is the running count of events processed
	TotalEventsProcessed int64

	// ProcessingNode is the hostname of the node that last updated this checkpoint
	// Used for debugging/observability in HA deployments
	ProcessingNode string

	// UpdatedAt is when this checkpoint was last updated
	UpdatedAt time.Time

	// CreatedAt is when this checkpoint was first created
	CreatedAt time.Time
}
