package writer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/xh63/netbird-events/pkg/events"
)

// MockWriter is a mock implementation of EventWriter for testing
type MockWriter struct {
	sendEventsCalled bool
	sendEventCalled  bool
	closeCalled      bool
	sendEventsErr    error
	sendEventErr     error
	closeErr         error
	receivedEvents   []events.Event
}

func (mw *MockWriter) SendEvents(ctx context.Context, eventsList []events.Event) error {
	mw.sendEventsCalled = true
	mw.receivedEvents = append(mw.receivedEvents, eventsList...)
	return mw.sendEventsErr
}

func (mw *MockWriter) SendEvent(ctx context.Context, evt events.Event) error {
	mw.sendEventCalled = true
	mw.receivedEvents = append(mw.receivedEvents, evt)
	return mw.sendEventErr
}

func (mw *MockWriter) Close() error {
	mw.closeCalled = true
	return mw.closeErr
}

func TestNewMultiWriter(t *testing.T) {
	mock1 := &MockWriter{}
	mock2 := &MockWriter{}

	mw := NewMultiWriter(mock1, mock2)

	if mw == nil {
		t.Fatal("Expected non-nil MultiWriter")
	}

	if len(mw.writers) != 2 {
		t.Errorf("Expected 2 writers, got %d", len(mw.writers))
	}
}

func TestMultiWriter_SendEvents_Success(t *testing.T) {
	mock1 := &MockWriter{}
	mock2 := &MockWriter{}
	mock3 := &MockWriter{}

	mw := NewMultiWriter(mock1, mock2, mock3)

	now := time.Now()
	testEvents := []events.Event{
		{
			ID:          1,
			Timestamp:   now,
			Activity:    1,
			InitiatorID: "user1",
			AccountID:   "account1",
		},
		{
			ID:          2,
			Timestamp:   now.Add(1 * time.Minute),
			Activity:    2,
			InitiatorID: "user2",
			AccountID:   "account2",
		},
	}

	err := mw.SendEvents(context.Background(), testEvents)
	if err != nil {
		t.Fatalf("SendEvents failed: %v", err)
	}

	// Verify all writers were called
	if !mock1.sendEventsCalled {
		t.Error("Expected mock1.SendEvents to be called")
	}
	if !mock2.sendEventsCalled {
		t.Error("Expected mock2.SendEvents to be called")
	}
	if !mock3.sendEventsCalled {
		t.Error("Expected mock3.SendEvents to be called")
	}

	// Verify all writers received the events
	if len(mock1.receivedEvents) != 2 {
		t.Errorf("Expected mock1 to receive 2 events, got %d", len(mock1.receivedEvents))
	}
	if len(mock2.receivedEvents) != 2 {
		t.Errorf("Expected mock2 to receive 2 events, got %d", len(mock2.receivedEvents))
	}
	if len(mock3.receivedEvents) != 2 {
		t.Errorf("Expected mock3 to receive 2 events, got %d", len(mock3.receivedEvents))
	}
}

func TestMultiWriter_SendEvents_OneWriterFails(t *testing.T) {
	mock1 := &MockWriter{}
	mock2 := &MockWriter{sendEventsErr: errors.New("mock2 error")}
	mock3 := &MockWriter{}

	mw := NewMultiWriter(mock1, mock2, mock3)

	now := time.Now()
	testEvents := []events.Event{
		{ID: 1, Timestamp: now, Activity: 1, AccountID: "test"},
	}

	err := mw.SendEvents(context.Background(), testEvents)

	// Should return the first error
	if err == nil {
		t.Error("Expected error from failing writer")
	}
	if err.Error() != "mock2 error" {
		t.Errorf("Expected 'mock2 error', got: %v", err)
	}

	// But all writers should still have been called
	if !mock1.sendEventsCalled {
		t.Error("Expected mock1.SendEvents to be called")
	}
	if !mock2.sendEventsCalled {
		t.Error("Expected mock2.SendEvents to be called")
	}
	if !mock3.sendEventsCalled {
		t.Error("Expected mock3.SendEvents to be called despite earlier error")
	}

	// Verify mock1 and mock3 still received the events
	if len(mock1.receivedEvents) != 1 {
		t.Errorf("Expected mock1 to receive 1 event, got %d", len(mock1.receivedEvents))
	}
	if len(mock3.receivedEvents) != 1 {
		t.Errorf("Expected mock3 to receive 1 event, got %d", len(mock3.receivedEvents))
	}
}

func TestMultiWriter_SendEvents_MultipleWritersFail(t *testing.T) {
	mock1 := &MockWriter{sendEventsErr: errors.New("mock1 error")}
	mock2 := &MockWriter{sendEventsErr: errors.New("mock2 error")}
	mock3 := &MockWriter{sendEventsErr: errors.New("mock3 error")}

	mw := NewMultiWriter(mock1, mock2, mock3)

	now := time.Now()
	testEvents := []events.Event{
		{ID: 1, Timestamp: now, Activity: 1, AccountID: "test"},
	}

	err := mw.SendEvents(context.Background(), testEvents)

	// Should return the first error encountered
	if err == nil {
		t.Error("Expected error from failing writers")
	}
	if err.Error() != "mock1 error" {
		t.Errorf("Expected first error 'mock1 error', got: %v", err)
	}

	// All writers should have been called
	if !mock1.sendEventsCalled {
		t.Error("Expected mock1.SendEvents to be called")
	}
	if !mock2.sendEventsCalled {
		t.Error("Expected mock2.SendEvents to be called")
	}
	if !mock3.sendEventsCalled {
		t.Error("Expected mock3.SendEvents to be called")
	}
}

func TestMultiWriter_SendEvents_EmptyList(t *testing.T) {
	mock1 := &MockWriter{}
	mock2 := &MockWriter{}

	mw := NewMultiWriter(mock1, mock2)

	err := mw.SendEvents(context.Background(), []events.Event{})
	if err != nil {
		t.Fatalf("SendEvents with empty list failed: %v", err)
	}

	// Writers should still be called with empty list
	if !mock1.sendEventsCalled {
		t.Error("Expected mock1.SendEvents to be called")
	}
	if !mock2.sendEventsCalled {
		t.Error("Expected mock2.SendEvents to be called")
	}
}

func TestMultiWriter_SendEvent(t *testing.T) {
	mock1 := &MockWriter{}
	mock2 := &MockWriter{}

	mw := NewMultiWriter(mock1, mock2)

	now := time.Now()
	testEvent := events.Event{
		ID:          1,
		Timestamp:   now,
		Activity:    1,
		InitiatorID: "user1",
		AccountID:   "account1",
	}

	err := mw.SendEvent(context.Background(), testEvent)
	if err != nil {
		t.Fatalf("SendEvent failed: %v", err)
	}

	// Verify all writers were called
	if !mock1.sendEventsCalled {
		t.Error("Expected mock1.SendEvents to be called")
	}
	if !mock2.sendEventsCalled {
		t.Error("Expected mock2.SendEvents to be called")
	}

	// Verify all writers received the event
	if len(mock1.receivedEvents) != 1 {
		t.Errorf("Expected mock1 to receive 1 event, got %d", len(mock1.receivedEvents))
	}
	if len(mock2.receivedEvents) != 1 {
		t.Errorf("Expected mock2 to receive 1 event, got %d", len(mock2.receivedEvents))
	}

	// Verify event data
	if mock1.receivedEvents[0].ID != 1 {
		t.Errorf("Expected event ID 1, got %d", mock1.receivedEvents[0].ID)
	}
}

func TestMultiWriter_Close_Success(t *testing.T) {
	mock1 := &MockWriter{}
	mock2 := &MockWriter{}
	mock3 := &MockWriter{}

	mw := NewMultiWriter(mock1, mock2, mock3)

	err := mw.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Verify all writers were closed
	if !mock1.closeCalled {
		t.Error("Expected mock1.Close to be called")
	}
	if !mock2.closeCalled {
		t.Error("Expected mock2.Close to be called")
	}
	if !mock3.closeCalled {
		t.Error("Expected mock3.Close to be called")
	}
}

func TestMultiWriter_Close_OneWriterFails(t *testing.T) {
	mock1 := &MockWriter{}
	mock2 := &MockWriter{closeErr: errors.New("close error")}
	mock3 := &MockWriter{}

	mw := NewMultiWriter(mock1, mock2, mock3)

	err := mw.Close()

	// Should return the error
	if err == nil {
		t.Error("Expected error from Close")
	}
	if err.Error() != "close error" {
		t.Errorf("Expected 'close error', got: %v", err)
	}

	// All writers should still have been closed
	if !mock1.closeCalled {
		t.Error("Expected mock1.Close to be called")
	}
	if !mock2.closeCalled {
		t.Error("Expected mock2.Close to be called")
	}
	if !mock3.closeCalled {
		t.Error("Expected mock3.Close to be called despite error")
	}
}

func TestMultiWriter_NoWriters(t *testing.T) {
	mw := NewMultiWriter()

	now := time.Now()
	testEvent := events.Event{
		ID:        1,
		Timestamp: now,
		Activity:  1,
		AccountID: "test",
	}

	// Should not error with no writers
	err := mw.SendEvent(context.Background(), testEvent)
	if err != nil {
		t.Errorf("SendEvent with no writers should not error, got: %v", err)
	}

	err = mw.Close()
	if err != nil {
		t.Errorf("Close with no writers should not error, got: %v", err)
	}
}

func TestMultiWriter_ContextCancellation(t *testing.T) {
	mock1 := &MockWriter{}
	mock2 := &MockWriter{}

	mw := NewMultiWriter(mock1, mock2)

	// Create cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	now := time.Now()
	testEvent := events.Event{
		ID:        1,
		Timestamp: now,
		Activity:  1,
		AccountID: "test",
	}

	// Context cancellation is passed to underlying writers
	// Our mock doesn't check context, so this will succeed
	err := mw.SendEvent(ctx, testEvent)
	if err != nil {
		t.Logf("SendEvent with cancelled context result: %v", err)
	}

	// Writers should still be called (context handling is writer-specific)
	if !mock1.sendEventsCalled {
		t.Error("Expected mock1.SendEvents to be called")
	}
	if !mock2.sendEventsCalled {
		t.Error("Expected mock2.SendEvents to be called")
	}
}
