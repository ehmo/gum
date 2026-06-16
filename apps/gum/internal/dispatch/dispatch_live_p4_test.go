//go:build live

// Package dispatch_test — Phase 4 live integration tests (G4.7).
// These tests require real Google credentials and are excluded from the default
// test run. To execute:
//
//	GUM_LIVE_TEST_ACCOUNT=user@example.com go test -tags=live ./internal/dispatch/
package dispatch_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/adapters"
	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
)

// gmailSendLiveCatalog returns a minimal catalog with gmail.users.messages.send
// wired to the typed-rest-sdk adapter.
func gmailSendLiveCatalog() *catalog.Catalog {
	return &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratedAt:          time.Now().UTC().Format(time.RFC3339),
		GeneratorVersion:     "test-live-p4",
		Ops: []catalog.Op{
			{
				OpID:             "gmail.users.messages.send",
				OpSchemaVersion:  1,
				Title:            "Send Gmail message",
				Summary:          "Send a Gmail message on behalf of the user.",
				Service:          "gmail",
				ServiceFamily:    "workspace",
				DefaultVariantID: "gmail.v1.rest.users.messages.send",
				Variants: []catalog.Variant{
					{
						VariantID:     "gmail.v1.rest.users.messages.send",
						Stability:     catalog.StabilityStable,
						InterfaceKind: catalog.InterfaceKindDiscoveryREST,
						BackendKind:   catalog.BackendKindTypedRestSDK,
						Preferred:     true,
						RiskClass:     catalog.RiskClassWrite,
						AuthStrategy:  catalog.AuthStrategyADC,
						Scopes:        []string{"https://mail.google.com/"},
						Binding: &catalog.Binding{
							BindingSchemaVersion: 1,
							AdapterKey:           "rest.typed-rest-sdk",
							OperationKey:         "gmail.users.messages.send",
							HTTP: &catalog.HTTPBinding{
								Method: "POST",
								Path:   "https://gmail.googleapis.com/gmail/v1/users/{userId}/messages/send",
							},
							GoPkg:  "google.golang.org/api/gmail/v1",
							GoCall: "Users.Messages.Send",
						},
					},
				},
			},
		},
	}
}

// TestGmailSendLiveCanary (G4.7) drives gum.write → gmail.users.messages.send
// with a test recipient, subject, and body.
//
// The test sends to the live account itself (self-send) to avoid spamming external addresses.
func TestGmailSendLiveCanary(t *testing.T) {
	defer goleak.VerifyNone(t)

	account := os.Getenv(liveAccountEnvVar)
	if account == "" {
		t.Skipf("skipping live Gmail send test: %s env var not set", liveAccountEnvVar)
	}

	cat := gmailSendLiveCatalog()
	ex := adapters.NewTypedRestSDK()
	disp := dispatch.NewDispatcher(cat, map[string]dispatch.Adapter{
		"rest.typed-rest-sdk": ex,
	})

	// Build a minimal RFC 2822 message as base64url for the Gmail API.
	// Subject identifies this as a gum test message.
	rawMsg := strings.Join([]string{
		"From: " + account,
		"To: " + account,
		"Subject: gum Phase4 live send canary",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"This is an automated test message sent by the gum Phase 4 live canary test.",
		"It is safe to delete.",
	}, "\r\n")

	// Gmail API /send expects raw as base64url-encoded RFC 2822 bytes.
	// We pass raw bytes as-is; the typed-rest-sdk adapter handles base64url encoding.
	inv := &dispatch.Invocation{
		OpID: "gmail.users.messages.send",
		Args: map[string]any{
			"userId": "me",
			"raw":    rawMsg,
		},
		Format:    "json",
		RequestID: "live-gmail-send-canary",
	}

	resp, err := disp.Dispatch(t.Context(), inv)
	if err != nil {
		t.Fatalf("Dispatch gmail.users.messages.send: %v", err)
	}
	if resp == nil || len(resp.Body) == 0 {
		t.Fatal("response body is empty")
	}

	// Structural check: Gmail send returns a Message resource with an "id".
	var result map[string]any
	if err := json.Unmarshal(resp.Body, &result); err != nil {
		t.Fatalf("response is not valid JSON: %v\nbody: %s", err, resp.Body)
	}
	if _, ok := result["id"]; !ok {
		t.Errorf("response has no 'id' key (send may have failed): %s", resp.Body)
	}
	t.Logf("sent message id: %v", result["id"])
}
