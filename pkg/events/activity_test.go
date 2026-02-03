package events

import (
	"testing"
)

func TestEnrichActivityInfo(t *testing.T) {
	tests := []struct {
		name         string
		activity     int
		expectedName string
		expectedCode string
	}{
		{
			name:         "User logged in peer",
			activity:     49,
			expectedName: "User logged in peer",
			expectedCode: "user.peer.login",
		},
		{
			name:         "Dashboard login",
			activity:     51,
			expectedName: "Dashboard login",
			expectedCode: "dashboard.login",
		},
		{
			name:         "Peer added with setup key",
			activity:     1,
			expectedName: "Peer added",
			expectedCode: "peer.setupkey.add",
		},
		{
			name:         "User joined",
			activity:     2,
			expectedName: "User joined",
			expectedCode: "user.join",
		},
		{
			name:         "Policy created",
			activity:     9,
			expectedName: "Policy added",
			expectedCode: "policy.add",
		},
		{
			name:         "Network created",
			activity:     73,
			expectedName: "Network created",
			expectedCode: "network.create",
		},
		{
			name:         "Unknown activity",
			activity:     99998,
			expectedName: "UNKNOWN_ACTIVITY",
			expectedCode: "UNKNOWN_ACTIVITY",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &Event{
				Activity: tt.activity,
			}

			EnrichActivityInfo(event)

			if event.ActivityName != tt.expectedName {
				t.Errorf("ActivityName = %q, want %q", event.ActivityName, tt.expectedName)
			}

			if event.ActivityCode != tt.expectedCode {
				t.Errorf("ActivityCode = %q, want %q", event.ActivityCode, tt.expectedCode)
			}
		})
	}
}

func TestEnrichActivityInfo_AllKnownActivities(t *testing.T) {
	// Test a range of known activities to ensure mapping works
	knownActivities := []struct {
		activity int
		name     string
	}{
		{0, "Peer added"},
		{1, "Peer added"},
		{2, "User joined"},
		{10, "Policy updated"},
		{49, "User logged in peer"},
		{50, "Peer login expired"},
		{51, "Dashboard login"},
		{73, "Network created"},
		{74, "Network updated"},
		{75, "Network deleted"},
	}

	for _, tt := range knownActivities {
		event := &Event{Activity: tt.activity}
		EnrichActivityInfo(event)

		if event.ActivityName == "UNKNOWN_ACTIVITY" {
			t.Errorf("Activity %d should have known name %q, got UNKNOWN_ACTIVITY", tt.activity, tt.name)
		}

		if event.ActivityCode == "UNKNOWN_ACTIVITY" {
			t.Errorf("Activity %d should have known code, got UNKNOWN_ACTIVITY", tt.activity)
		}

		if event.ActivityName != tt.name {
			t.Errorf("Activity %d: got name %q, want %q", tt.activity, event.ActivityName, tt.name)
		}
	}
}

func TestEnrichActivityInfo_PreservesOtherFields(t *testing.T) {
	event := &Event{
		ID:             12345,
		Activity:       49,
		InitiatorID:    "00u123",
		TargetID:       "00u456",
		AccountID:      "acc123",
		Meta:           `{"key":"value"}`,
		InitiatorEmail: "user@example.com",
		TargetEmail:    "peer@example.com",
	}

	EnrichActivityInfo(event)

	// Verify activity fields are set
	if event.ActivityName != "User logged in peer" {
		t.Errorf("ActivityName = %q, want %q", event.ActivityName, "User logged in peer")
	}

	if event.ActivityCode != "user.peer.login" {
		t.Errorf("ActivityCode = %q, want %q", event.ActivityCode, "user.peer.login")
	}

	// Verify other fields unchanged
	if event.ID != 12345 {
		t.Errorf("ID changed: got %d, want 12345", event.ID)
	}

	if event.InitiatorID != "00u123" {
		t.Errorf("InitiatorID changed: got %q, want %q", event.InitiatorID, "00u123")
	}

	if event.Meta != `{"key":"value"}` {
		t.Errorf("Meta changed: got %q", event.Meta)
	}
}
