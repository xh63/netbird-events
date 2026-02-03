package events

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

// Mock is already defined in reader_test.go, but since these are separate test files,
// we need it here too for standalone running
type mockEmailEnrichmentConfigSetupToken struct {
	enabled      bool
	source       string
	customSchema string
	customTable  string
}

func (m *mockEmailEnrichmentConfigSetupToken) GetSource() string {
	if !m.enabled {
		return "none"
	}
	return m.source
}

func (m *mockEmailEnrichmentConfigSetupToken) GetCustomSchema() string {
	return m.customSchema
}

func (m *mockEmailEnrichmentConfigSetupToken) GetCustomTable() string {
	return m.customTable
}

func (m *mockEmailEnrichmentConfigSetupToken) IsEnabled() bool {
	return m.enabled
}

func newMockEmailConfigSetupToken() *mockEmailEnrichmentConfigSetupToken {
	return &mockEmailEnrichmentConfigSetupToken{
		enabled: true,
		source:  "idp_okta_users",
	}
}

// TestGetEvents_WithSetupToken tests that events with setup tokens (non-Okta IDs)
// are processed correctly with empty email fields
func TestGetEvents_WithSetupToken(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer func() { _ = db.Close() }()

	emailConfig := newMockEmailConfigSetupToken()
	reader := NewEventReader(db, logger, emailConfig)

	now := time.Now()

	// Mock rows with setup token (no email match)
	rows := sqlmock.NewRows([]string{"id", "timestamp", "activity", "initiator_id", "target_id", "account_id", "meta", "initiator_email", "target_email"}).
		AddRow(int64(1), now, 5, "setup_token_abc123", "", "account_1", `{"action":"peer_registered"}`, "not_okta_user", "not_okta_user") // Empty emails from COALESCE

	mock.ExpectQuery("SELECT e.id, e.timestamp, e.activity, e.initiator_id, e.target_id, e.account_id, e.meta").
		WillReturnRows(rows)

	opts := EventQueryOptions{
		Limit: 10,
	}

	events, err := reader.GetEvents(context.Background(), opts)
	if err != nil {
		t.Fatalf("GetEvents failed: %v", err)
	}

	if len(events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(events))
	}

	event := events[0]

	// Verify setup token ID is preserved
	if event.InitiatorID != "setup_token_abc123" {
		t.Errorf("Expected initiator_id 'setup_token_abc123', got '%s'", event.InitiatorID)
	}

	// Verify email fields are "not_okta_user" (not error)
	if event.InitiatorEmail != "not_okta_user" {
		t.Errorf("Expected initiator_email 'not_okta_user' for setup token, got '%s'", event.InitiatorEmail)
	}

	if event.TargetEmail != "not_okta_user" {
		t.Errorf("Expected target_email 'not_okta_user', got '%s'", event.TargetEmail)
	}

	// Verify event is still valid and processable
	if event.Activity != 5 {
		t.Errorf("Expected activity 5, got %d", event.Activity)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

// TestGetEvents_MixedUserTypes tests events from both Okta users and setup tokens
func TestGetEvents_MixedUserTypes(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer func() { _ = db.Close() }()

	emailConfig := newMockEmailConfigSetupToken()
	reader := NewEventReader(db, logger, emailConfig)

	now := time.Now()

	// Mock rows with mixed types
	rows := sqlmock.NewRows([]string{"id", "timestamp", "activity", "initiator_id", "target_id", "account_id", "meta", "initiator_email", "target_email"}).
		// Event 1: Okta user (has email)
		AddRow(int64(1), now, 1, "00u123abc", "00u456def", "account_1", `{}`, "john@example.com", "jane@example.com").
		// Event 2: Setup token (no email)
		AddRow(int64(2), now, 5, "setup_token_xyz", "", "account_1", `{}`, "not_okta_user", "not_okta_user").
		// Event 3: System event (NULL initiator, no email)
		AddRow(int64(3), now, 10, "not_okta_user", "not_okta_user", "account_1", `{}`, "not_okta_user", "not_okta_user")

	mock.ExpectQuery("SELECT e.id, e.timestamp, e.activity, e.initiator_id, e.target_id, e.account_id, e.meta").
		WillReturnRows(rows)

	opts := EventQueryOptions{
		Limit: 10,
	}

	events, err := reader.GetEvents(context.Background(), opts)
	if err != nil {
		t.Fatalf("GetEvents failed: %v", err)
	}

	if len(events) != 3 {
		t.Fatalf("Expected 3 events, got %d", len(events))
	}

	// Verify Event 1: Okta user with emails
	if events[0].InitiatorEmail != "john@example.com" {
		t.Errorf("Event 1: Expected initiator_email 'john@example.com', got '%s'", events[0].InitiatorEmail)
	}
	if events[0].TargetEmail != "jane@example.com" {
		t.Errorf("Event 1: Expected target_email 'jane@example.com', got '%s'", events[0].TargetEmail)
	}

	// Verify Event 2: Setup token with "not_okta_user" emails
	if events[1].InitiatorID != "setup_token_xyz" {
		t.Errorf("Event 2: Expected initiator_id 'setup_token_xyz', got '%s'", events[1].InitiatorID)
	}
	if events[1].InitiatorEmail != "not_okta_user" {
		t.Errorf("Event 2: Expected initiator_email 'not_okta_user', got '%s'", events[1].InitiatorEmail)
	}

	// Verify Event 3: System event with "not_okta_user" emails
	if events[2].InitiatorID != "not_okta_user" {
		t.Errorf("Event 3: Expected initiator_id 'not_okta_user', got '%s'", events[2].InitiatorID)
	}
	if events[2].InitiatorEmail != "not_okta_user" {
		t.Errorf("Event 3: Expected initiator_email 'not_okta_user', got '%s'", events[2].InitiatorEmail)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

// TestGetEvents_ServiceAccount tests events from service accounts (non-Okta)
func TestGetEvents_ServiceAccount(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer func() { _ = db.Close() }()

	emailConfig := newMockEmailConfigSetupToken()
	reader := NewEventReader(db, logger, emailConfig)

	now := time.Now()

	// Service account ID that doesn't exist in idp.okta_users
	rows := sqlmock.NewRows([]string{"id", "timestamp", "activity", "initiator_id", "target_id", "account_id", "meta", "initiator_email", "target_email"}).
		AddRow(int64(1), now, 3, "service_account_monitoring", "", "account_1", `{"service":"prometheus"}`, "not_okta_user", "not_okta_user")

	mock.ExpectQuery("SELECT e.id, e.timestamp, e.activity, e.initiator_id, e.target_id, e.account_id, e.meta").
		WillReturnRows(rows)

	opts := EventQueryOptions{
		Limit: 10,
	}

	events, err := reader.GetEvents(context.Background(), opts)
	if err != nil {
		t.Fatalf("GetEvents failed: %v", err)
	}

	if len(events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(events))
	}

	event := events[0]

	// Verify service account is preserved
	if event.InitiatorID != "service_account_monitoring" {
		t.Errorf("Expected initiator_id 'service_account_monitoring', got '%s'", event.InitiatorID)
	}

	// Verify "not_okta_user" email (graceful handling)
	if event.InitiatorEmail != "not_okta_user" {
		t.Errorf("Expected initiator_email 'not_okta_user' for service account, got '%s'", event.InitiatorEmail)
	}

	// Verify metadata is preserved
	if event.Meta != `{"service":"prometheus"}` {
		t.Errorf("Expected meta preserved, got '%s'", event.Meta)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}
