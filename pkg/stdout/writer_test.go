package stdout

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/xh63/netbird-events/pkg/events"
)

// TestSendEvents_WritesToStdout confirms that events are written to stdout in JSON format.
func TestSendEvents_WritesToStdout(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil)) // Use a discard logger for the test
	writer := NewStdoutWriter(logger)

	// --- Capture stdout ---
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// --- Create and send test event ---
	testTime := time.Date(2026, 1, 28, 10, 30, 0, 0, time.UTC)
	testEvent := events.Event{
		ID:             123,
		Timestamp:      testTime,
		Activity:       42,
		ActivityName:   "Test Event",
		ActivityCode:   "test.event",
		AccountID:      "acc1",
		InitiatorID:    "user1",
		InitiatorEmail: "user1@example.com",
		TargetID:       "peer1",
		Meta:           `{"ip":"10.0.0.1","os":"linux"}`,
	}

	err := writer.SendEvents(context.Background(), []events.Event{testEvent})
	if err != nil {
		t.Fatalf("SendEvents failed: %v", err)
	}

	// --- Restore stdout and read captured output ---
	_ = w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	_ = r.Close()

	output := strings.TrimSpace(buf.String())

	// --- Assertions for JSON output ---
	// Parse JSON to validate structure
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("Output is not valid JSON: %v\nOutput: %s", err, output)
	}

	// Check classification fields
	if result["log_type"] != "application" {
		t.Errorf("Expected log_type='application', got %v", result["log_type"])
	}
	if result["event_source"] != "netbird" {
		t.Errorf("Expected event_source='netbird', got %v", result["event_source"])
	}

	// Check event fields
	if result["event_id"] != float64(123) {
		t.Errorf("Expected event_id=123, got %v", result["event_id"])
	}
	if result["activity_name"] != "Test Event" {
		t.Errorf("Expected activity_name='Test Event', got %v", result["activity_name"])
	}
	if result["initiator_email"] != "user1@example.com" {
		t.Errorf("Expected initiator_email='user1@example.com', got %v", result["initiator_email"])
	}

	// Check flattened meta fields
	if result["meta_os"] != "linux" {
		t.Errorf("Expected flattened meta field meta_os='linux', got %v", result["meta_os"])
	}
	if result["meta_ip"] != "10.0.0.1" {
		t.Errorf("Expected flattened meta field meta_ip='10.0.0.1', got %v", result["meta_ip"])
	}

	// Ensure the original meta field is not present (it should be flattened)
	if _, exists := result["meta"]; exists {
		t.Errorf("Expected 'meta' field to be flattened (not present as a field), but it was found")
	}
}

func TestSendEvent_Single(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	writer := NewStdoutWriter(logger)

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := writer.SendEvent(context.Background(), events.Event{ID: 456})
	if err != nil {
		t.Fatalf("SendEvent failed: %v", err)
	}

	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	_ = r.Close()

	output := strings.TrimSpace(buf.String())

	// Parse JSON output
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("Output is not valid JSON: %v\nOutput: %s", err, output)
	}

	// Check classification fields are present
	if result["log_type"] != "application" {
		t.Errorf("Expected log_type='application', got %v", result["log_type"])
	}
	if result["event_source"] != "netbird" {
		t.Errorf("Expected event_source='netbird', got %v", result["event_source"])
	}

	if result["event_id"] != float64(456) {
		t.Errorf("Expected event_id=456, got %v", result["event_id"])
	}
}

func TestClose(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	writer := NewStdoutWriter(logger)

	// Close should be a no-op and not error
	if err := writer.Close(); err != nil {
		t.Errorf("Close() returned an error: %v", err)
	}
}
