package processor

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/xh63/netbird-events/pkg/config"
	"github.com/xh63/netbird-events/pkg/events"
	"github.com/xh63/netbird-events/pkg/metrics"
	"github.com/xh63/netbird-events/pkg/stdout"
)

// EventWriter interface for sending events to an output (allows mocking in tests)
type EventWriter interface {
	SendEvents(ctx context.Context, events []events.Event) error
}

// Processor orchestrates reading events from DB and sending to stdout (journal)
// Uses single checkpoint model (after migration 003)
type Processor struct {
	eventReader events.ReaderInterface
	writer      EventWriter                  // Single stdout writer
	checkpoint  *events.ProcessingCheckpoint // Single checkpoint
	config      *config.Config
	logFactory  config.LogFactory // creates typed loggers at runtime (e.g. "security", "audit")
	logger      *slog.Logger      // system logger, pre-created from logFactory
	hostname    string            // for processing_node tracking
}

// NewProcessor creates a new event processor.
// logFactory is used to create typed loggers; call logFactory.New("<type>") anywhere
// in the processor to emit logs with a specific log_type without changing signatures.
func NewProcessor(cfg *config.Config, logFactory config.LogFactory) (*Processor, error) {
	logger := logFactory.New("system")

	// Validate cluster + SQLite constraint
	if cfg.DatabaseDriver == "sqlite" && cfg.Cluster.Enabled {
		return nil, fmt.Errorf("cluster mode is not supported with SQLite; use PostgreSQL for HA")
	}

	// Create event reader based on configured driver
	var eventReader events.ReaderInterface
	switch cfg.DatabaseDriver {
	case "sqlite":
		db, err := config.GetSQLiteDB(cfg.SQLitePath, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to open SQLite database: %w", err)
		}
		eventReader = events.NewSQLiteEventReader(db, logger, &cfg.EmailEnrichment)
		logger.Info("Using SQLite event reader", "path", cfg.SQLitePath)
	default: // "postgres" or empty
		db, err := config.GetDB(cfg.PostgresURL, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to database: %w", err)
		}
		eventReader = events.NewPostgresEventReader(db, logger, &cfg.EmailEnrichment)
		logger.Info("Using PostgreSQL event reader")
	}

	// Create stdout writer (outputs JSON to journal)
	stdoutWriter := stdout.NewStdoutWriter(logger)
	logger.Info("Initialized stdout writer for journal output")

	// Get hostname for processing_node tracking
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
		logger.Warn("Failed to get hostname", "error", err)
	}

	return &Processor{
		eventReader: eventReader,
		writer:      stdoutWriter,
		checkpoint:  nil, // Will be loaded in Run()
		config:      cfg,
		logFactory:  logFactory,
		logger:      logger,
		hostname:    hostname,
	}, nil
}

// createLokiWriter creates a Loki writer with TLS support
// Run starts the event processor
func (p *Processor) Run(ctx context.Context) error {
	p.logger.Info("Starting events processor",
		"platform", p.config.Platform,
		"region", p.config.Region,
		"consumer_id", p.config.ConsumerID,
		"batch_size", p.config.BatchSize,
		"lookback_hours", p.config.LookbackHours,
		"polling_interval", p.config.PollingInterval,
		"processing_node", p.hostname,
	)

	// Load checkpoint from database (single checkpoint model)
	checkpoint, err := p.eventReader.GetCheckpoint(ctx, p.config.ConsumerID)
	if err != nil {
		return fmt.Errorf("failed to load checkpoint: %w", err)
	}

	if checkpoint == nil {
		// Initialize checkpoint
		p.checkpoint = &events.ProcessingCheckpoint{
			ConsumerID:           p.config.ConsumerID,
			LastEventID:          0,
			LastEventTimestamp:   time.Time{},
			TotalEventsProcessed: 0,
			ProcessingNode:       p.hostname,
		}
		p.logger.Info("No existing checkpoint found, starting fresh")
	} else {
		p.checkpoint = checkpoint
		p.logger.Info("Resuming from checkpoint",
			"last_event_id", checkpoint.LastEventID,
			"last_event_timestamp", checkpoint.LastEventTimestamp,
			"total_events_processed", checkpoint.TotalEventsProcessed,
		)
	}

	// Run once or continuously based on polling_interval
	if p.config.PollingInterval == 0 {
		// Run once and exit
		return p.processEvents(ctx)
	}

	// Run continuously with polling
	ticker := time.NewTicker(time.Duration(p.config.PollingInterval) * time.Second)
	defer ticker.Stop()

	// Process immediately on start
	if err := p.processEvents(ctx); err != nil {
		p.logger.Error("Error processing events", "error", err)
	}

	// Then poll at intervals
	for {
		select {
		case <-ctx.Done():
			p.logger.Info("Context cancelled, shutting down")
			return ctx.Err()
		case <-ticker.C:
			if err := p.processEvents(ctx); err != nil {
				p.logger.Error("Error processing events", "error", err)
			}
		}
	}
}

// processEvents processes events and sends to stdout writer
func (p *Processor) processEvents(ctx context.Context) error {
	startTime := time.Now()
	defer func() {
		metrics.ProcessingDuration.Observe(time.Since(startTime).Seconds())
	}()
	p.logger.Info("Starting to process events")

	// Build query options
	opts := events.EventQueryOptions{
		Limit:    p.config.BatchSize,
		Offset:   0,
		OrderAsc: true, // Process oldest first
	}

	// Resume from checkpoint
	if p.checkpoint.LastEventID > 0 {
		opts.MinEventID = &p.checkpoint.LastEventID
		p.logger.Debug("Resuming from checkpoint",
			"last_event_id", p.checkpoint.LastEventID,
		)
	} else {
		// First run - use lookback hours if configured
		if p.config.LookbackHours > 0 {
			lookbackTime := time.Now().Add(-time.Duration(p.config.LookbackHours) * time.Hour)
			opts.StartTime = &lookbackTime
			p.logger.Info("First run, using lookback",
				"lookback_hours", p.config.LookbackHours,
			)
		}
	}

	totalProcessed := 0

	for {
		// Check context
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Fetch batch of events
		dbStart := time.Now()
		eventBatch, err := p.eventReader.GetEvents(ctx, opts)
		metrics.DBQueryDuration.WithLabelValues("get_events").Observe(time.Since(dbStart).Seconds())
		if err != nil {
			return fmt.Errorf("failed to fetch events: %w", err)
		}

		// If no more events, we're done
		if len(eventBatch) == 0 {
			break
		}

		// Send to stdout writer
		sendStart := time.Now()
		if err := p.writer.SendEvents(ctx, eventBatch); err != nil {
			metrics.EventsProcessed.WithLabelValues("true").Add(float64(len(eventBatch)))
			// Return error - checkpoint won't be updated, will retry next time
			return fmt.Errorf("failed to send events: %w", err)
		}

		p.logger.Debug("Sent events to stdout",
			"count", len(eventBatch),
			"duration_ms", time.Since(sendStart).Milliseconds(),
		)

		metrics.EventsProcessed.WithLabelValues("false").Add(float64(len(eventBatch)))
		metrics.BatchSize.Observe(float64(len(eventBatch)))
		totalProcessed += len(eventBatch)

		// Update checkpoint
		lastEvent := eventBatch[len(eventBatch)-1]
		p.checkpoint.LastEventID = lastEvent.ID
		p.checkpoint.LastEventTimestamp = lastEvent.Timestamp
		metrics.LastEventID.Set(float64(lastEvent.ID))
		metrics.CheckpointLagSeconds.Set(time.Since(lastEvent.Timestamp).Seconds())
		p.checkpoint.TotalEventsProcessed += int64(len(eventBatch))
		p.checkpoint.ProcessingNode = p.hostname

		// Save checkpoint to database
		dbStart = time.Now()
		if err := p.eventReader.SaveCheckpoint(ctx, p.checkpoint); err != nil {
			return fmt.Errorf("failed to save checkpoint: %w", err)
		}
		metrics.DBQueryDuration.WithLabelValues("save_checkpoint").Observe(time.Since(dbStart).Seconds())

		p.logger.Debug("Updated checkpoint",
			"last_event_id", p.checkpoint.LastEventID,
			"total_events", p.checkpoint.TotalEventsProcessed,
		)

		// If we got fewer events than batch size, we're done
		if len(eventBatch) < p.config.BatchSize {
			break
		}

		// Move to next batch - update MinEventID for next iteration
		opts.MinEventID = &p.checkpoint.LastEventID
		opts.Offset = 0 // Reset offset since we're using MinEventID
	}

	// Always record poll time regardless of whether events were found.
	// This is the primary liveness signal — use it to detect a stopped service.
	metrics.LastPollTime.SetToCurrentTime()

	duration := time.Since(startTime)
	if totalProcessed > 0 {
		p.logger.Info("Finished processing events",
			"events_processed", totalProcessed,
			"last_event_id", p.checkpoint.LastEventID,
			"duration_s", duration.Seconds(),
		)
	} else {
		p.logger.Debug("No events to process")
	}

	return nil
}

// Close cleans up resources
func (p *Processor) Close() error {
	return p.eventReader.Close()
}
