package processor

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"gitlab.global.company.com/netbird/code/netbird-prov/eventsproc/pkg/config"
	"gitlab.global.company.com/netbird/code/netbird-prov/pkg/events"
	"gitlab.global.company.com/netbird/code/netbird-prov/pkg/stdout"
)

// EventWriter interface for sending events to an output (allows mocking in tests)
type EventWriter interface {
	SendEvents(ctx context.Context, events []events.Event) error
}

// Processor orchestrates reading events from DB and sending to stdout (journal)
// Uses single checkpoint model (after migration 003)
type Processor struct {
	eventReader *events.EventReader
	writer      EventWriter                  // Single stdout writer
	checkpoint  *events.ProcessingCheckpoint // Single checkpoint
	config      *config.Config
	logger      *slog.Logger
	hostname    string // for processing_node tracking
}

// NewProcessor creates a new event processor
func NewProcessor(cfg *config.Config, logger *slog.Logger) (*Processor, error) {
	// Connect to database using our standalone config package
	db, err := config.GetDB(cfg.PostgresURL, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Create event reader with email enrichment config
	eventReader := events.NewEventReader(db, logger, &cfg.EmailEnrichment)

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
		eventBatch, err := p.eventReader.GetEvents(ctx, opts)
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
			// Return error - checkpoint won't be updated, will retry next time
			return fmt.Errorf("failed to send events: %w", err)
		}

		p.logger.Debug("Sent events to stdout",
			"count", len(eventBatch),
			"duration_ms", time.Since(sendStart).Milliseconds(),
		)

		totalProcessed += len(eventBatch)

		// Update checkpoint
		lastEvent := eventBatch[len(eventBatch)-1]
		p.checkpoint.LastEventID = lastEvent.ID
		p.checkpoint.LastEventTimestamp = lastEvent.Timestamp
		p.checkpoint.TotalEventsProcessed += int64(len(eventBatch))
		p.checkpoint.ProcessingNode = p.hostname

		// Save checkpoint to database
		if err := p.eventReader.SaveCheckpoint(ctx, p.checkpoint); err != nil {
			return fmt.Errorf("failed to save checkpoint: %w", err)
		}

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
