package events

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
)

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

// Config interface to avoid circular dependency
type EmailEnrichmentConfig interface {
	GetSource() string
	GetCustomSchema() string
	GetCustomTable() string
	IsEnabled() bool
}

// EventReader implements ReaderInterface for reading events from Postgres
type EventReader struct {
	db                  *sql.DB
	logger              *slog.Logger
	emailEnrichmentConf EmailEnrichmentConfig
}

// NewEventReader creates a new EventReader
func NewEventReader(db *sql.DB, logger *slog.Logger, emailConf EmailEnrichmentConfig) *EventReader {
	return &EventReader{
		db:                  db,
		logger:              logger,
		emailEnrichmentConf: emailConf,
	}
}

// buildEmailEnrichmentQuery builds the appropriate SELECT query based on email enrichment configuration
func (er *EventReader) buildEmailEnrichmentQuery() string {
	baseQuery := `SELECT e.id, e.timestamp, e.activity, e.initiator_id, e.target_id, e.account_id, e.meta`

	source := er.emailEnrichmentConf.GetSource()

	switch source {
	case "idp_okta_users":
		// Use custom idp.okta_users table (custom organization deployment)
		return baseQuery + `,
		COALESCE(u1.email, e.initiator_id) as initiator_email,
		COALESCE(u2.email, e.target_id) as target_email
	FROM events e
	LEFT JOIN idp.okta_users u1 ON e.initiator_id = u1.id
	LEFT JOIN idp.okta_users u2 ON e.target_id = u2.id`

	case "netbird_users":
		// Use standard NetBird users table
		return baseQuery + `,
		COALESCE(u1.email, u1.name, e.initiator_id) as initiator_email,
		COALESCE(u2.email, u2.name, e.target_id) as target_email
	FROM events e
	LEFT JOIN users u1 ON e.initiator_id = u1.id
	LEFT JOIN users u2 ON e.target_id = u2.id`

	case "custom":
		// Use custom table
		schema := er.emailEnrichmentConf.GetCustomSchema()
		table := er.emailEnrichmentConf.GetCustomTable()
		return baseQuery + fmt.Sprintf(`,
		COALESCE(u1.email, e.initiator_id) as initiator_email,
		COALESCE(u2.email, e.target_id) as target_email
	FROM events e
	LEFT JOIN %s.%s u1 ON e.initiator_id = u1.id
	LEFT JOIN %s.%s u2 ON e.target_id = u2.id`, schema, table, schema, table)

	case "auto":
		// Try idp.okta_users first, fallback to users table, then user_id
		return baseQuery + `,
		COALESCE(
			okta.email,
			netbird.email,
			netbird.name,
			e.initiator_id
		) as initiator_email,
		COALESCE(
			okta_target.email,
			netbird_target.email,
			netbird_target.name,
			e.target_id
		) as target_email
	FROM events e
	LEFT JOIN idp.okta_users okta ON e.initiator_id = okta.id
	LEFT JOIN idp.okta_users okta_target ON e.target_id = okta_target.id
	LEFT JOIN users netbird ON e.initiator_id = netbird.id
	LEFT JOIN users netbird_target ON e.target_id = netbird_target.id`

	case "none":
		// No email enrichment, just use user_id
		return baseQuery + `,
		e.initiator_id as initiator_email,
		e.target_id as target_email
	FROM events e`

	default:
		// Default to auto
		er.logger.Warn("Unknown email enrichment source, defaulting to auto", "source", source)
		// Create temporary config with auto source to avoid infinite recursion
		return baseQuery + `,
		COALESCE(
			okta.email,
			netbird.email,
			netbird.name,
			e.initiator_id
		) as initiator_email,
		COALESCE(
			okta_target.email,
			netbird_target.email,
			netbird_target.name,
			e.target_id
		) as target_email
	FROM events e
	LEFT JOIN idp.okta_users okta ON e.initiator_id = okta.id
	LEFT JOIN idp.okta_users okta_target ON e.target_id = okta_target.id
	LEFT JOIN users netbird ON e.initiator_id = netbird.id
	LEFT JOIN users netbird_target ON e.target_id = netbird_target.id`
	}
}

// GetEvents fetches events from the database based on query options
func (er *EventReader) GetEvents(ctx context.Context, opts EventQueryOptions) ([]Event, error) {
	// Build the query with appropriate email enrichment
	query := er.buildEmailEnrichmentQuery()
	conditions := []string{}
	args := []any{}
	argPos := 1

	// Add filters
	if opts.AccountID != "" {
		conditions = append(conditions, fmt.Sprintf("e.account_id = $%d", argPos))
		args = append(args, opts.AccountID)
		argPos++
	}

	if opts.StartTime != nil {
		conditions = append(conditions, fmt.Sprintf("e.timestamp >= $%d", argPos))
		args = append(args, *opts.StartTime)
		argPos++
	}

	if opts.EndTime != nil {
		conditions = append(conditions, fmt.Sprintf("e.timestamp <= $%d", argPos))
		args = append(args, *opts.EndTime)
		argPos++
	}

	if opts.Activity != nil {
		conditions = append(conditions, fmt.Sprintf("e.activity = $%d", argPos))
		args = append(args, *opts.Activity)
		argPos++
	}

	if opts.MinEventID != nil {
		conditions = append(conditions, fmt.Sprintf("e.id > $%d", argPos))
		args = append(args, *opts.MinEventID)
		argPos++
	}

	// Add WHERE clause if there are conditions
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	// Add ORDER BY
	if opts.OrderAsc {
		query += " ORDER BY e.timestamp ASC"
	} else {
		query += " ORDER BY e.timestamp DESC"
	}

	// Add LIMIT and OFFSET
	if opts.Limit == 0 {
		opts.Limit = 1000 // default limit
	}
	query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", argPos, argPos+1)
	args = append(args, opts.Limit, opts.Offset)

	er.logger.Debug("Executing query", "query", query, "args", args)

	// Execute the query
	rows, err := er.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query events: %w", err)
	}
	defer rows.Close()

	// Parse results
	events := []Event{}
	for rows.Next() {
		var event Event
		var initiatorID, targetID, accountID, meta, initiatorEmail, targetEmail sql.NullString

		err := rows.Scan(
			&event.ID,
			&event.Timestamp,
			&event.Activity,
			&initiatorID,
			&targetID,
			&accountID,
			&meta,
			&initiatorEmail,
			&targetEmail,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan event: %w", err)
		}

		// Handle NULL values
		if initiatorID.Valid {
			event.InitiatorID = initiatorID.String
		}
		if targetID.Valid {
			event.TargetID = targetID.String
		}
		if accountID.Valid {
			event.AccountID = accountID.String
		}
		if meta.Valid {
			event.Meta = meta.String
		}
		if initiatorEmail.Valid {
			event.InitiatorEmail = initiatorEmail.String
		}
		if targetEmail.Valid {
			event.TargetEmail = targetEmail.String
		}

		// Enrich with activity name and code
		EnrichActivityInfo(&event)

		events = append(events, event)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	er.logger.Info("Fetched events", "count", len(events))
	return events, nil
}

// GetEventCount returns the total count of events matching the query options
func (er *EventReader) GetEventCount(ctx context.Context, opts EventQueryOptions) (int64, error) {
	query := "SELECT COUNT(*) FROM events"
	conditions := []string{}
	args := []any{}
	argPos := 1

	// Add filters (same as GetEvents)
	if opts.AccountID != "" {
		conditions = append(conditions, fmt.Sprintf("account_id = $%d", argPos))
		args = append(args, opts.AccountID)
		argPos++
	}

	if opts.StartTime != nil {
		conditions = append(conditions, fmt.Sprintf("timestamp >= $%d", argPos))
		args = append(args, *opts.StartTime)
		argPos++
	}

	if opts.EndTime != nil {
		conditions = append(conditions, fmt.Sprintf("timestamp <= $%d", argPos))
		args = append(args, *opts.EndTime)
		argPos++
	}

	if opts.Activity != nil {
		conditions = append(conditions, fmt.Sprintf("activity = $%d", argPos))
		args = append(args, *opts.Activity)
		argPos++
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	var count int64
	err := er.db.QueryRowContext(ctx, query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count events: %w", err)
	}

	return count, nil
}


// GetWriterCheckpoint retrieves the checkpoint for a specific consumer/writer combination
// Deprecated: Use GetCheckpoint for single-checkpoint model (migration 003+)
func (er *EventReader) GetWriterCheckpoint(ctx context.Context, consumerID, writerType string) (*ProcessingCheckpoint, error) {
	query := `
		SELECT consumer_id, writer_type, last_event_id, last_event_timestamp,
		       total_events_processed, COALESCE(processing_node, ''), updated_at, created_at
		FROM idp.event_processing_checkpoint
		WHERE consumer_id = $1 AND writer_type = $2
	`

	var checkpoint ProcessingCheckpoint
	err := er.db.QueryRowContext(ctx, query, consumerID, writerType).Scan(
		&checkpoint.ConsumerID,
		&checkpoint.WriterType,
		&checkpoint.LastEventID,
		&checkpoint.LastEventTimestamp,
		&checkpoint.TotalEventsProcessed,
		&checkpoint.ProcessingNode,
		&checkpoint.UpdatedAt,
		&checkpoint.CreatedAt,
	)

	if err == sql.ErrNoRows {
		// No checkpoint found - this is the first run for this writer
		er.logger.Info("No checkpoint found for writer",
			"consumer_id", consumerID,
			"writer_type", writerType,
		)
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("failed to query writer checkpoint: %w", err)
	}

	er.logger.Info("Loaded writer checkpoint",
		"consumer_id", checkpoint.ConsumerID,
		"writer_type", checkpoint.WriterType,
		"last_event_id", checkpoint.LastEventID,
		"last_event_timestamp", checkpoint.LastEventTimestamp,
		"total_events_processed", checkpoint.TotalEventsProcessed,
		"processing_node", checkpoint.ProcessingNode,
	)

	return &checkpoint, nil
}

// SaveWriterCheckpoint saves or updates the checkpoint for a specific consumer/writer
// Deprecated: Use SaveCheckpoint for single-checkpoint model (migration 003+)
func (er *EventReader) SaveWriterCheckpoint(ctx context.Context, checkpoint *ProcessingCheckpoint) error {
	query := `
		INSERT INTO idp.event_processing_checkpoint
		(consumer_id, writer_type, last_event_id, last_event_timestamp, total_events_processed, processing_node, updated_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
		ON CONFLICT (consumer_id, writer_type)
		DO UPDATE SET
			last_event_id = EXCLUDED.last_event_id,
			last_event_timestamp = EXCLUDED.last_event_timestamp,
			total_events_processed = EXCLUDED.total_events_processed,
			processing_node = EXCLUDED.processing_node,
			updated_at = NOW()
	`

	_, err := er.db.ExecContext(ctx, query,
		checkpoint.ConsumerID,
		checkpoint.WriterType,
		checkpoint.LastEventID,
		checkpoint.LastEventTimestamp,
		checkpoint.TotalEventsProcessed,
		checkpoint.ProcessingNode,
	)

	if err != nil {
		return fmt.Errorf("failed to save writer checkpoint: %w", err)
	}

	er.logger.Debug("Saved writer checkpoint",
		"consumer_id", checkpoint.ConsumerID,
		"writer_type", checkpoint.WriterType,
		"last_event_id", checkpoint.LastEventID,
		"total_events_processed", checkpoint.TotalEventsProcessed,
		"processing_node", checkpoint.ProcessingNode,
	)

	return nil
}

// GetCheckpoint retrieves the checkpoint for a consumer (single checkpoint model)
// This is used after migration 003 which removes per-writer checkpoints
func (er *EventReader) GetCheckpoint(ctx context.Context, consumerID string) (*ProcessingCheckpoint, error) {
	query := `
		SELECT consumer_id, last_event_id, last_event_timestamp,
		       total_events_processed, COALESCE(processing_node, ''), updated_at, created_at
		FROM idp.event_processing_checkpoint
		WHERE consumer_id = $1
	`

	var checkpoint ProcessingCheckpoint
	err := er.db.QueryRowContext(ctx, query, consumerID).Scan(
		&checkpoint.ConsumerID,
		&checkpoint.LastEventID,
		&checkpoint.LastEventTimestamp,
		&checkpoint.TotalEventsProcessed,
		&checkpoint.ProcessingNode,
		&checkpoint.UpdatedAt,
		&checkpoint.CreatedAt,
	)

	if err == sql.ErrNoRows {
		// No checkpoint found - this is the first run
		er.logger.Info("No checkpoint found for consumer",
			"consumer_id", consumerID,
		)
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("failed to query checkpoint: %w", err)
	}

	er.logger.Info("Loaded checkpoint",
		"consumer_id", checkpoint.ConsumerID,
		"last_event_id", checkpoint.LastEventID,
		"last_event_timestamp", checkpoint.LastEventTimestamp,
		"total_events_processed", checkpoint.TotalEventsProcessed,
		"processing_node", checkpoint.ProcessingNode,
	)

	return &checkpoint, nil
}

// SaveCheckpoint saves or updates the checkpoint for a consumer (single checkpoint model)
// This is used after migration 003 which removes per-writer checkpoints
func (er *EventReader) SaveCheckpoint(ctx context.Context, checkpoint *ProcessingCheckpoint) error {
	query := `
		INSERT INTO idp.event_processing_checkpoint
		(consumer_id, last_event_id, last_event_timestamp, total_events_processed, processing_node, updated_at, created_at)
		VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
		ON CONFLICT (consumer_id)
		DO UPDATE SET
			last_event_id = EXCLUDED.last_event_id,
			last_event_timestamp = EXCLUDED.last_event_timestamp,
			total_events_processed = EXCLUDED.total_events_processed,
			processing_node = EXCLUDED.processing_node,
			updated_at = NOW()
	`

	_, err := er.db.ExecContext(ctx, query,
		checkpoint.ConsumerID,
		checkpoint.LastEventID,
		checkpoint.LastEventTimestamp,
		checkpoint.TotalEventsProcessed,
		checkpoint.ProcessingNode,
	)

	if err != nil {
		return fmt.Errorf("failed to save checkpoint: %w", err)
	}

	er.logger.Debug("Saved checkpoint",
		"consumer_id", checkpoint.ConsumerID,
		"last_event_id", checkpoint.LastEventID,
		"total_events_processed", checkpoint.TotalEventsProcessed,
		"processing_node", checkpoint.ProcessingNode,
	)

	return nil
}

// Close closes the database connection
func (er *EventReader) Close() error {
	return er.db.Close()
}
