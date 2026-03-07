package writer

import (
	"context"

	"github.com/xh63/netbird-events/pkg/events"
)

// EventWriter defines the common interface for all event writers
type EventWriter interface {
	// SendEvents sends a batch of events
	SendEvents(ctx context.Context, eventsList []events.Event) error

	// SendEvent sends a single event
	SendEvent(ctx context.Context, evt events.Event) error

	// Close closes the writer and flushes any pending events
	Close() error
}

// MultiWriter sends events to multiple writers simultaneously
type MultiWriter struct {
	writers []EventWriter
}

// NewMultiWriter creates a new MultiWriter that writes to all provided writers
func NewMultiWriter(writers ...EventWriter) *MultiWriter {
	return &MultiWriter{
		writers: writers,
	}
}

// SendEvents sends events to all writers
// Returns the first error encountered, but continues sending to remaining writers
func (mw *MultiWriter) SendEvents(ctx context.Context, eventsList []events.Event) error {
	var firstErr error

	for _, writer := range mw.writers {
		if err := writer.SendEvents(ctx, eventsList); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			// Continue sending to other writers even if one fails
		}
	}

	return firstErr
}

// SendEvent sends a single event to all writers
func (mw *MultiWriter) SendEvent(ctx context.Context, evt events.Event) error {
	return mw.SendEvents(ctx, []events.Event{evt})
}

// Close closes all writers
func (mw *MultiWriter) Close() error {
	var firstErr error

	for _, writer := range mw.writers {
		if err := writer.Close(); err != nil {
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	return firstErr
}
