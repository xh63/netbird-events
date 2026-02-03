package events

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

// TestEmailEnrichment_AisOktaUsers tests email enrichment using idp.okta_users table
func TestEmailEnrichment_AisOktaUsers(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	emailConfig := &mockEmailEnrichmentConfig{
		enabled: true,
		source:  "idp_okta_users",
	}
	reader := NewEventReader(db, logger, emailConfig)

	now := time.Now()
	rows := sqlmock.NewRows([]string{"id", "timestamp", "activity", "initiator_id", "target_id", "account_id", "meta", "initiator_email", "target_email"}).
		AddRow(int64(1), now, 1, "00u123abc", "00u456def", "account_1", `{}`, "john@example.com", "jane@example.com")

	// Expect query with idp.okta_users JOIN
	mock.ExpectQuery("SELECT e.id, e.timestamp, e.activity, e.initiator_id, e.target_id, e.account_id, e.meta.*FROM events e.*LEFT JOIN idp.okta_users u1.*LEFT JOIN idp.okta_users u2").
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

	if events[0].InitiatorEmail != "john@example.com" {
		t.Errorf("Expected initiator_email 'john@example.com', got '%s'", events[0].InitiatorEmail)
	}
	if events[0].TargetEmail != "jane@example.com" {
		t.Errorf("Expected target_email 'jane@example.com', got '%s'", events[0].TargetEmail)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

// TestEmailEnrichment_NetbirdUsers tests email enrichment using standard users table
func TestEmailEnrichment_NetbirdUsers(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	emailConfig := &mockEmailEnrichmentConfig{
		enabled: true,
		source:  "netbird_users",
	}
	reader := NewEventReader(db, logger, emailConfig)

	now := time.Now()
	rows := sqlmock.NewRows([]string{"id", "timestamp", "activity", "initiator_id", "target_id", "account_id", "meta", "initiator_email", "target_email"}).
		AddRow(int64(1), now, 1, "00u123abc", "00u456def", "account_1", `{}`, "user1@netbird.io", "user2@netbird.io")

	// Expect query with users table JOIN
	mock.ExpectQuery("SELECT e.id, e.timestamp, e.activity, e.initiator_id, e.target_id, e.account_id, e.meta.*FROM events e.*LEFT JOIN users u1.*LEFT JOIN users u2").
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

	if events[0].InitiatorEmail != "user1@netbird.io" {
		t.Errorf("Expected initiator_email 'user1@netbird.io', got '%s'", events[0].InitiatorEmail)
	}
	if events[0].TargetEmail != "user2@netbird.io" {
		t.Errorf("Expected target_email 'user2@netbird.io', got '%s'", events[0].TargetEmail)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

// TestEmailEnrichment_CustomTable tests email enrichment using custom schema/table
func TestEmailEnrichment_CustomTable(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	emailConfig := &mockEmailEnrichmentConfig{
		enabled:      true,
		source:       "custom",
		customSchema: "auth",
		customTable:  "user_directory",
	}
	reader := NewEventReader(db, logger, emailConfig)

	now := time.Now()
	rows := sqlmock.NewRows([]string{"id", "timestamp", "activity", "initiator_id", "target_id", "account_id", "meta", "initiator_email", "target_email"}).
		AddRow(int64(1), now, 1, "00u123abc", "00u456def", "account_1", `{}`, "admin@custom.com", "user@custom.com")

	// Expect query with custom schema.table JOIN
	mock.ExpectQuery("SELECT e.id, e.timestamp, e.activity, e.initiator_id, e.target_id, e.account_id, e.meta.*FROM events e.*LEFT JOIN auth.user_directory u1.*LEFT JOIN auth.user_directory u2").
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

	if events[0].InitiatorEmail != "admin@custom.com" {
		t.Errorf("Expected initiator_email 'admin@custom.com', got '%s'", events[0].InitiatorEmail)
	}
	if events[0].TargetEmail != "user@custom.com" {
		t.Errorf("Expected target_email 'user@custom.com', got '%s'", events[0].TargetEmail)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

// TestEmailEnrichment_Auto tests auto-detection mode with fallback
func TestEmailEnrichment_Auto(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	emailConfig := &mockEmailEnrichmentConfig{
		enabled: true,
		source:  "auto",
	}
	reader := NewEventReader(db, logger, emailConfig)

	now := time.Now()
	rows := sqlmock.NewRows([]string{"id", "timestamp", "activity", "initiator_id", "target_id", "account_id", "meta", "initiator_email", "target_email"}).
		AddRow(int64(1), now, 1, "00u123abc", "00u456def", "account_1", `{}`, "from.okta@example.com", "from.netbird@example.com")

	// Expect query with COALESCE trying multiple sources
	mock.ExpectQuery("SELECT e.id, e.timestamp, e.activity, e.initiator_id, e.target_id, e.account_id, e.meta.*COALESCE.*okta.email.*netbird.email.*FROM events e.*LEFT JOIN idp.okta_users okta.*LEFT JOIN users netbird").
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

	// Verify email was enriched from auto-detection
	if events[0].InitiatorEmail == "" {
		t.Error("Expected non-empty initiator_email from auto-detection")
	}
	if events[0].TargetEmail == "" {
		t.Error("Expected non-empty target_email from auto-detection")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

// TestEmailEnrichment_None tests disabled email enrichment
func TestEmailEnrichment_None(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	emailConfig := &mockEmailEnrichmentConfig{
		enabled: true,
		source:  "none",
	}
	reader := NewEventReader(db, logger, emailConfig)

	now := time.Now()
	rows := sqlmock.NewRows([]string{"id", "timestamp", "activity", "initiator_id", "target_id", "account_id", "meta", "initiator_email", "target_email"}).
		AddRow(int64(1), now, 1, "00u123abc", "00u456def", "account_1", `{}`, "00u123abc", "00u456def")

	// Expect query WITHOUT any JOINs - just user_id
	mock.ExpectQuery("SELECT e.id, e.timestamp, e.activity, e.initiator_id, e.target_id, e.account_id, e.meta.*e.initiator_id as initiator_email.*e.target_id as target_email.*FROM events e").
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

	// When disabled, email fields should contain user_id
	if events[0].InitiatorEmail != "00u123abc" {
		t.Errorf("Expected initiator_email to be user_id '00u123abc', got '%s'", events[0].InitiatorEmail)
	}
	if events[0].TargetEmail != "00u456def" {
		t.Errorf("Expected target_email to be user_id '00u456def', got '%s'", events[0].TargetEmail)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

// TestEmailEnrichment_Disabled tests when email enrichment is completely disabled
func TestEmailEnrichment_Disabled(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	emailConfig := &mockEmailEnrichmentConfig{
		enabled: false,
		source:  "idp_okta_users", // Source is ignored when disabled
	}
	reader := NewEventReader(db, logger, emailConfig)

	now := time.Now()
	rows := sqlmock.NewRows([]string{"id", "timestamp", "activity", "initiator_id", "target_id", "account_id", "meta", "initiator_email", "target_email"}).
		AddRow(int64(1), now, 1, "00u123abc", "00u456def", "account_1", `{}`, "00u123abc", "00u456def")

	// When disabled, GetSource() returns "none", so no JOINs
	mock.ExpectQuery("SELECT e.id, e.timestamp, e.activity, e.initiator_id, e.target_id, e.account_id, e.meta.*e.initiator_id as initiator_email.*e.target_id as target_email.*FROM events e").
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

	// When disabled, email fields should contain user_id
	if events[0].InitiatorEmail != "00u123abc" {
		t.Errorf("Expected initiator_email to be user_id '00u123abc', got '%s'", events[0].InitiatorEmail)
	}
	if events[0].TargetEmail != "00u456def" {
		t.Errorf("Expected target_email to be user_id '00u456def', got '%s'", events[0].TargetEmail)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

// TestEmailEnrichment_UnknownSource tests fallback for unknown source
func TestEmailEnrichment_UnknownSource(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}
	defer db.Close()

	emailConfig := &mockEmailEnrichmentConfig{
		enabled: true,
		source:  "unknown_source", // Invalid source should fallback to auto
	}
	reader := NewEventReader(db, logger, emailConfig)

	now := time.Now()
	rows := sqlmock.NewRows([]string{"id", "timestamp", "activity", "initiator_id", "target_id", "account_id", "meta", "initiator_email", "target_email"}).
		AddRow(int64(1), now, 1, "00u123abc", "00u456def", "account_1", `{}`, "fallback@example.com", "fallback2@example.com")

	// Unknown source should fallback to auto mode (tries all sources)
	mock.ExpectQuery("SELECT e.id, e.timestamp, e.activity, e.initiator_id, e.target_id, e.account_id, e.meta.*COALESCE.*okta.email.*netbird.email").
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

	// Should get email from fallback (auto mode)
	if events[0].InitiatorEmail == "" {
		t.Error("Expected non-empty initiator_email from fallback to auto mode")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

// TestEmailEnrichmentConfig_InterfaceMethods tests the config interface methods
func TestEmailEnrichmentConfig_InterfaceMethods(t *testing.T) {
	tests := []struct {
		name           string
		enabled        bool
		source         string
		customSchema   string
		customTable    string
		expectedSource string
	}{
		{
			name:           "Enabled with ais_okta_users",
			enabled:        true,
			source:         "idp_okta_users",
			expectedSource: "idp_okta_users",
		},
		{
			name:           "Enabled with auto",
			enabled:        true,
			source:         "auto",
			expectedSource: "auto",
		},
		{
			name:           "Disabled returns none",
			enabled:        false,
			source:         "idp_okta_users",
			expectedSource: "none",
		},
		{
			name:           "Custom with schema and table",
			enabled:        true,
			source:         "custom",
			customSchema:   "auth",
			customTable:    "users",
			expectedSource: "custom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &mockEmailEnrichmentConfig{
				enabled:      tt.enabled,
				source:       tt.source,
				customSchema: tt.customSchema,
				customTable:  tt.customTable,
			}

			if config.GetSource() != tt.expectedSource {
				t.Errorf("GetSource() = %s, expected %s", config.GetSource(), tt.expectedSource)
			}

			if config.IsEnabled() != tt.enabled {
				t.Errorf("IsEnabled() = %v, expected %v", config.IsEnabled(), tt.enabled)
			}

			if config.GetCustomSchema() != tt.customSchema {
				t.Errorf("GetCustomSchema() = %s, expected %s", config.GetCustomSchema(), tt.customSchema)
			}

			if config.GetCustomTable() != tt.customTable {
				t.Errorf("GetCustomTable() = %s, expected %s", config.GetCustomTable(), tt.customTable)
			}
		})
	}
}
