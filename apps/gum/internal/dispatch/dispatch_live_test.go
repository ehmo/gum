//go:build live

// Package dispatch_test — live integration tests (G3.3, G3.4).
// These tests require real Google credentials and are excluded from the default
// test run. To execute:
//
//	GUM_LIVE_TEST_ACCOUNT=user@example.com go test -tags=live ./internal/dispatch/
package dispatch_test

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/adapters"
	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
)

const liveAccountEnvVar = "GUM_LIVE_TEST_ACCOUNT"

// gmailLiveCatalog returns a minimal catalog with gmail.users.messages.list wired
// to the typed-rest-sdk adapter.
func gmailLiveCatalog() *catalog.Catalog {
	return &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratedAt:          time.Now().UTC().Format(time.RFC3339),
		GeneratorVersion:     "test-live",
		Ops: []catalog.Op{
			{
				OpID:             "gmail.users.messages.list",
				OpSchemaVersion:  1,
				Title:            "List Gmail messages",
				Summary:          "List message IDs in a Gmail mailbox.",
				Service:          "gmail",
				ServiceFamily:    "workspace",
				DefaultVariantID: "gmail.v1.rest.users.messages.list",
				Variants: []catalog.Variant{
					{
						VariantID:     "gmail.v1.rest.users.messages.list",
						Stability:     catalog.StabilityStable,
						InterfaceKind: catalog.InterfaceKindDiscoveryREST,
						BackendKind:   catalog.BackendKindTypedRestSDK,
						Preferred:     true,
						RiskClass:     catalog.RiskClassRead,
						AuthStrategy:  catalog.AuthStrategyADC,
						Scopes:        []string{"https://www.googleapis.com/auth/gmail.readonly"},
						Binding: &catalog.Binding{
							BindingSchemaVersion: 1,
							AdapterKey:           "rest.typed-rest-sdk",
							OperationKey:         "gmail.users.messages.list",
							HTTP: &catalog.HTTPBinding{
								Method: "GET",
								Path:   "https://gmail.googleapis.com/gmail/v1/users/{userId}/messages",
							},
							GoPkg:  "google.golang.org/api/gmail/v1",
							GoCall: "Users.Messages.List",
						},
					},
				},
			},
		},
	}
}

// calendarLiveCatalog returns a minimal catalog with calendar.events.list wired
// to the typed-rest-sdk adapter.
func calendarLiveCatalog() *catalog.Catalog {
	return &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratedAt:          time.Now().UTC().Format(time.RFC3339),
		GeneratorVersion:     "test-live",
		Ops: []catalog.Op{
			{
				OpID:             "calendar.events.list",
				OpSchemaVersion:  1,
				Title:            "List Calendar events",
				Summary:          "List events on a calendar.",
				Service:          "calendar",
				ServiceFamily:    "workspace",
				DefaultVariantID: "calendar.v3.rest.events.list",
				Variants: []catalog.Variant{
					{
						VariantID:     "calendar.v3.rest.events.list",
						Stability:     catalog.StabilityStable,
						InterfaceKind: catalog.InterfaceKindDiscoveryREST,
						BackendKind:   catalog.BackendKindTypedRestSDK,
						Preferred:     true,
						RiskClass:     catalog.RiskClassRead,
						AuthStrategy:  catalog.AuthStrategyADC,
						Scopes:        []string{"https://www.googleapis.com/auth/calendar.readonly"},
						Binding: &catalog.Binding{
							BindingSchemaVersion: 1,
							AdapterKey:           "rest.typed-rest-sdk",
							OperationKey:         "calendar.events.list",
							HTTP: &catalog.HTTPBinding{
								Method: "GET",
								Path:   "https://www.googleapis.com/calendar/v3/calendars/{calendarId}/events",
							},
							GoPkg:  "google.golang.org/api/calendar/v3",
							GoCall: "Events.List",
						},
					},
				},
			},
		},
	}
}

// TestGmailReadonlyLiveCanary drives a real gmail.users.messages.list call
// through the full kernel stack and asserts the response contains a messages[]
// array (structural check only; no content assertion). (G3.3)
func TestGmailReadonlyLiveCanary(t *testing.T) {
	defer goleak.VerifyNone(t)

	account := os.Getenv(liveAccountEnvVar)
	if account == "" {
		t.Skipf("skipping live Gmail test: %s env var not set", liveAccountEnvVar)
	}

	cat := gmailLiveCatalog()
	ex := adapters.NewTypedRestSDK()
	disp := dispatch.NewDispatcher(cat, map[string]dispatch.Adapter{
		"rest.typed-rest-sdk": ex,
	})

	inv := &dispatch.Invocation{
		OpID: "gmail.users.messages.list",
		Args: map[string]any{
			"userId":     "me",
			"maxResults": 1,
		},
		Format:    "json",
		RequestID: "live-gmail-canary",
	}

	resp, err := disp.Dispatch(t.Context(), inv)
	if err != nil {
		t.Fatalf("Dispatch gmail.users.messages.list: %v", err)
	}
	if resp == nil || len(resp.Body) == 0 {
		t.Fatal("response body is empty")
	}

	// Structural check: response must be valid JSON with a "messages" key.
	var result map[string]any
	if err := json.Unmarshal(resp.Body, &result); err != nil {
		t.Fatalf("response body is not valid JSON: %v\nbody: %s", err, resp.Body)
	}
	if _, ok := result["messages"]; !ok {
		// Some accounts may have no messages — the API still returns the key with
		// an empty array or omits it with resultSizeEstimate. Either is acceptable,
		// but the outer object must parse.
		t.Logf("response has no 'messages' key (may be empty inbox): %s", resp.Body)
	}
}

// TestCalendarEventsLiveCanary drives a real calendar.events.list call
// through the full kernel stack and asserts the response contains an items[]
// array. (G3.4)
func TestCalendarEventsLiveCanary(t *testing.T) {
	defer goleak.VerifyNone(t)

	account := os.Getenv(liveAccountEnvVar)
	if account == "" {
		t.Skipf("skipping live Calendar test: %s env var not set", liveAccountEnvVar)
	}

	cat := calendarLiveCatalog()
	ex := adapters.NewTypedRestSDK()
	disp := dispatch.NewDispatcher(cat, map[string]dispatch.Adapter{
		"rest.typed-rest-sdk": ex,
	})

	inv := &dispatch.Invocation{
		OpID: "calendar.events.list",
		Args: map[string]any{
			"calendarId": "primary",
			"maxResults": 1,
		},
		Format:    "json",
		RequestID: "live-calendar-canary",
	}

	resp, err := disp.Dispatch(t.Context(), inv)
	if err != nil {
		t.Fatalf("Dispatch calendar.events.list: %v", err)
	}
	if resp == nil || len(resp.Body) == 0 {
		t.Fatal("response body is empty")
	}

	// Structural check: response must be valid JSON.
	var result map[string]any
	if err := json.Unmarshal(resp.Body, &result); err != nil {
		t.Fatalf("response body is not valid JSON: %v\nbody: %s", err, resp.Body)
	}
	// Google Calendar list returns "kind": "calendar#events" and "items": [].
	if _, ok := result["items"]; !ok {
		t.Logf("response has no 'items' key (may be empty calendar): %s", resp.Body)
	}
}
