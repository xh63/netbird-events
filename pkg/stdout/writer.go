package stdout

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/xh63/netbird-events/pkg/events"
)

// StdoutWriter writes events to stdout in JSON format
// When running under systemd, stdout is captured and stored in the journal
type StdoutWriter struct {
	logger *slog.Logger
}

// NewStdoutWriter creates a new StdoutWriter
func NewStdoutWriter(logger *slog.Logger) *StdoutWriter {
	return &StdoutWriter{
		logger: logger,
	}
}

// SendEvents writes events to stdout in JSON format
func (w *StdoutWriter) SendEvents(ctx context.Context, eventsList []events.Event) error {
	for _, event := range eventsList {
		line, err := eventToJSON(event)
		if err != nil {
			w.logger.Warn("Failed to convert event to JSON", "event_id", event.ID, "error", err)
			continue
		}
		fmt.Println(line)
	}
	return nil
}

// SendEvent writes a single event to stdout
func (w *StdoutWriter) SendEvent(ctx context.Context, event events.Event) error {
	return w.SendEvents(ctx, []events.Event{event})
}

// Close is a no-op for StdoutWriter
func (w *StdoutWriter) Close() error {
	return nil
}

// eventToJSON converts an event to JSON format with flattened meta fields
func eventToJSON(event events.Event) (string, error) {
	// Create a map to hold all fields
	data := make(map[string]interface{})

	// Add classification fields for log filtering and routing
	data["log_type"] = "application"
	data["event_source"] = "netbird"

	// Add event fields
	data["event_id"] = event.ID
	data["timestamp"] = event.Timestamp.Format("2006-01-02T15:04:05.000Z07:00")
	data["activity"] = event.Activity
	data["activity_name"] = event.ActivityName
	data["activity_code"] = event.ActivityCode
	data["account_id"] = event.AccountID

	// Add initiator fields if they exist
	if event.InitiatorID != "" {
		data["initiator_id"] = event.InitiatorID
	}
	if event.InitiatorEmail != "" {
		data["initiator_email"] = event.InitiatorEmail
	}

	// Add target fields if they exist
	if event.TargetID != "" {
		data["target_id"] = event.TargetID
	}
	if event.TargetEmail != "" {
		data["target_email"] = event.TargetEmail
	}

	// Flatten meta JSON into individual fields with meta_ prefix
	if event.Meta != "" {
		var metaData map[string]interface{}
		if err := json.Unmarshal([]byte(event.Meta), &metaData); err == nil {
			for key, value := range metaData {
				data["meta_"+key] = value
			}
		} else {
			// If meta is not valid JSON, add it as a single field
			data["meta_unparsable"] = event.Meta
		}
	}

	// Marshal to JSON
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("failed to marshal event to JSON: %w", err)
	}

	return string(jsonBytes), nil
}
