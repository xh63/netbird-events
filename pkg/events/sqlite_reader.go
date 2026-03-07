package events

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
)

// SQLiteEventReader implements ReaderInterface for reading events from NetBird's SQLite store.
// SQLite differences from PostgreSQL:
//   - Positional placeholders are ? instead of $1, $2, ...
//   - No schema prefix (no idp. on checkpoint table)
//   - CURRENT_TIMESTAMP instead of NOW()
//   - No schema-qualified custom tables (falls back to "none")
type SQLiteEventReader struct {
	db                  *sql.DB
	logger              *slog.Logger
	emailEnrichmentConf EmailEnrichmentConfig
	decryptor           *NetbirdDecryptor // nil if decryption is not configured
}

// NewSQLiteEventReader creates a new SQLiteEventReader
func NewSQLiteEventReader(db *sql.DB, logger *slog.Logger, emailConf EmailEnrichmentConfig) ReaderInterface {
	r := &SQLiteEventReader{
		db:                  db,
		logger:              logger,
		emailEnrichmentConf: emailConf,
	}
	key, err := emailConf.GetDecryptionKey()
	if err != nil {
		logger.Warn("Failed to load NetBird decryption key, email fields will not be decrypted", "error", err)
	} else if key != nil {
		d, err := NewNetbirdDecryptor(key)
		if err != nil {
			logger.Warn("Failed to initialise NetBird decryptor", "error", err)
		} else {
			r.decryptor = d
			logger.Info("NetBird AES-GCM decryptor initialised — email fields will be decrypted")
		}
	}
	return r
}

// buildEmailEnrichmentQuery builds the SELECT query for SQLite (no schema prefixes)
func (r *SQLiteEventReader) buildEmailEnrichmentQuery() string {
	baseQuery := `SELECT e.id, e.timestamp, e.activity, e.initiator_id, e.target_id, e.account_id, e.meta`

	source := r.emailEnrichmentConf.GetSource()

	switch source {
	case "netbird_users":
		return baseQuery + `,
		COALESCE(u1.email, u1.name, e.initiator_id) as initiator_email,
		COALESCE(u2.email, u2.name, e.target_id) as target_email
	FROM events e
	LEFT JOIN users u1 ON e.initiator_id = u1.id
	LEFT JOIN users u2 ON e.target_id = u2.id`

	case "auto":
		// SQLite NetBird store has users table; okta_users may not exist — try both via LEFT JOIN
		return baseQuery + `,
		COALESCE(
			netbird.email,
			netbird.name,
			e.initiator_id
		) as initiator_email,
		COALESCE(
			netbird_target.email,
			netbird_target.name,
			e.target_id
		) as target_email
	FROM events e
	LEFT JOIN users netbird ON e.initiator_id = netbird.id
	LEFT JOIN users netbird_target ON e.target_id = netbird_target.id`

	case "none", "custom", "idp_okta_users":
		// custom and idp_okta_users require schema-qualified tables not supported in SQLite;
		// fall back to no enrichment
		if source != "none" {
			r.logger.Warn("Email enrichment source not supported for SQLite, falling back to none", "source", source)
		}
		return baseQuery + `,
		e.initiator_id as initiator_email,
		e.target_id as target_email
	FROM events e`

	default:
		r.logger.Warn("Unknown email enrichment source, defaulting to none for SQLite", "source", source)
		return baseQuery + `,
		e.initiator_id as initiator_email,
		e.target_id as target_email
	FROM events e`
	}
}

// GetEvents fetches events from SQLite using ? placeholders
func (r *SQLiteEventReader) GetEvents(ctx context.Context, opts EventQueryOptions) ([]Event, error) {
	query := r.buildEmailEnrichmentQuery()
	conditions := []string{}
	args := []any{}

	if opts.AccountID != "" {
		conditions = append(conditions, "e.account_id = ?")
		args = append(args, opts.AccountID)
	}

	if opts.StartTime != nil {
		conditions = append(conditions, "e.timestamp >= ?")
		args = append(args, *opts.StartTime)
	}

	if opts.EndTime != nil {
		conditions = append(conditions, "e.timestamp <= ?")
		args = append(args, *opts.EndTime)
	}

	if opts.Activity != nil {
		conditions = append(conditions, "e.activity = ?")
		args = append(args, *opts.Activity)
	}

	if opts.MinEventID != nil {
		conditions = append(conditions, "e.id > ?")
		args = append(args, *opts.MinEventID)
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	if opts.OrderAsc {
		query += " ORDER BY e.timestamp ASC"
	} else {
		query += " ORDER BY e.timestamp DESC"
	}

	if opts.Limit == 0 {
		opts.Limit = 1000
	}
	query += " LIMIT ? OFFSET ?"
	args = append(args, opts.Limit, opts.Offset)

	r.logger.Debug("Executing SQLite query", "query", query, "args", args)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := []Event{}
	for rows.Next() {
		var event Event
		var initiatorID, targetID, accountID, meta, initiatorEmail, targetEmail sql.NullString

		if err := rows.Scan(
			&event.ID,
			&event.Timestamp,
			&event.Activity,
			&initiatorID,
			&targetID,
			&accountID,
			&meta,
			&initiatorEmail,
			&targetEmail,
		); err != nil {
			return nil, fmt.Errorf("failed to scan event: %w", err)
		}

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

		// Decrypt email fields if NetBird AES-GCM decryption is configured.
		// NetBird encrypts name/email columns before writing to the database;
		// without decryption, these fields contain base64 ciphertext blobs.
		if r.decryptor != nil {
			event.InitiatorEmail = r.decryptor.Decrypt(event.InitiatorEmail)
			event.TargetEmail = r.decryptor.Decrypt(event.TargetEmail)
		}

		EnrichActivityInfo(&event)
		result = append(result, event)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	r.logger.Info("Fetched events", "count", len(result))
	return result, nil
}

// GetEventCount returns the total count of events matching the query options
func (r *SQLiteEventReader) GetEventCount(ctx context.Context, opts EventQueryOptions) (int64, error) {
	query := "SELECT COUNT(*) FROM events"
	conditions := []string{}
	args := []any{}

	if opts.AccountID != "" {
		conditions = append(conditions, "account_id = ?")
		args = append(args, opts.AccountID)
	}
	if opts.StartTime != nil {
		conditions = append(conditions, "timestamp >= ?")
		args = append(args, *opts.StartTime)
	}
	if opts.EndTime != nil {
		conditions = append(conditions, "timestamp <= ?")
		args = append(args, *opts.EndTime)
	}
	if opts.Activity != nil {
		conditions = append(conditions, "activity = ?")
		args = append(args, *opts.Activity)
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	var count int64
	if err := r.db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("failed to count events: %w", err)
	}
	return count, nil
}

// GetWriterCheckpoint retrieves the checkpoint for a consumer/writer pair
func (r *SQLiteEventReader) GetWriterCheckpoint(ctx context.Context, consumerID, writerType string) (*ProcessingCheckpoint, error) {
	query := `
		SELECT consumer_id, writer_type, last_event_id, last_event_timestamp,
		       total_events_processed, COALESCE(processing_node, ''), updated_at, created_at
		FROM event_processing_checkpoint
		WHERE consumer_id = ? AND writer_type = ?
	`

	var checkpoint ProcessingCheckpoint
	err := r.db.QueryRowContext(ctx, query, consumerID, writerType).Scan(
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
		r.logger.Info("No checkpoint found for writer", "consumer_id", consumerID, "writer_type", writerType)
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query writer checkpoint: %w", err)
	}

	r.logger.Info("Loaded writer checkpoint",
		"consumer_id", checkpoint.ConsumerID,
		"writer_type", checkpoint.WriterType,
		"last_event_id", checkpoint.LastEventID,
	)
	return &checkpoint, nil
}

// SaveWriterCheckpoint saves or updates the checkpoint for a consumer/writer pair
func (r *SQLiteEventReader) SaveWriterCheckpoint(ctx context.Context, checkpoint *ProcessingCheckpoint) error {
	query := `
		INSERT INTO event_processing_checkpoint
		(consumer_id, writer_type, last_event_id, last_event_timestamp, total_events_processed, processing_node, updated_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT (consumer_id, writer_type)
		DO UPDATE SET
			last_event_id = excluded.last_event_id,
			last_event_timestamp = excluded.last_event_timestamp,
			total_events_processed = excluded.total_events_processed,
			processing_node = excluded.processing_node,
			updated_at = CURRENT_TIMESTAMP
	`

	_, err := r.db.ExecContext(ctx, query,
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

	r.logger.Debug("Saved writer checkpoint",
		"consumer_id", checkpoint.ConsumerID,
		"writer_type", checkpoint.WriterType,
		"last_event_id", checkpoint.LastEventID,
	)
	return nil
}

// GetCheckpoint retrieves the single checkpoint for a consumer
func (r *SQLiteEventReader) GetCheckpoint(ctx context.Context, consumerID string) (*ProcessingCheckpoint, error) {
	query := `
		SELECT consumer_id, last_event_id, last_event_timestamp,
		       total_events_processed, COALESCE(processing_node, ''), updated_at, created_at
		FROM event_processing_checkpoint
		WHERE consumer_id = ?
	`

	var checkpoint ProcessingCheckpoint
	err := r.db.QueryRowContext(ctx, query, consumerID).Scan(
		&checkpoint.ConsumerID,
		&checkpoint.LastEventID,
		&checkpoint.LastEventTimestamp,
		&checkpoint.TotalEventsProcessed,
		&checkpoint.ProcessingNode,
		&checkpoint.UpdatedAt,
		&checkpoint.CreatedAt,
	)

	if err == sql.ErrNoRows {
		r.logger.Info("No checkpoint found for consumer", "consumer_id", consumerID)
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query checkpoint: %w", err)
	}

	r.logger.Info("Loaded checkpoint",
		"consumer_id", checkpoint.ConsumerID,
		"last_event_id", checkpoint.LastEventID,
		"total_events_processed", checkpoint.TotalEventsProcessed,
	)
	return &checkpoint, nil
}

// SaveCheckpoint saves or updates the single checkpoint for a consumer
func (r *SQLiteEventReader) SaveCheckpoint(ctx context.Context, checkpoint *ProcessingCheckpoint) error {
	query := `
		INSERT INTO event_processing_checkpoint
		(consumer_id, last_event_id, last_event_timestamp, total_events_processed, processing_node, updated_at, created_at)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT (consumer_id)
		DO UPDATE SET
			last_event_id = excluded.last_event_id,
			last_event_timestamp = excluded.last_event_timestamp,
			total_events_processed = excluded.total_events_processed,
			processing_node = excluded.processing_node,
			updated_at = CURRENT_TIMESTAMP
	`

	_, err := r.db.ExecContext(ctx, query,
		checkpoint.ConsumerID,
		checkpoint.LastEventID,
		checkpoint.LastEventTimestamp,
		checkpoint.TotalEventsProcessed,
		checkpoint.ProcessingNode,
	)
	if err != nil {
		return fmt.Errorf("failed to save checkpoint: %w", err)
	}

	r.logger.Debug("Saved checkpoint",
		"consumer_id", checkpoint.ConsumerID,
		"last_event_id", checkpoint.LastEventID,
		"total_events_processed", checkpoint.TotalEventsProcessed,
	)
	return nil
}

// Close closes the database connection
func (r *SQLiteEventReader) Close() error {
	return r.db.Close()
}
