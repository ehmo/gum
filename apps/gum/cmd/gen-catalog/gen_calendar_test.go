package main_test

import (
	"os"
	"strings"
	"testing"

	gencatalog "github.com/ehmo/gum/cmd/gen-catalog"
	"github.com/ehmo/gum/internal/catalog"
)

// TestGenerateEmitsGmailMessagesGet verifies that GenerateFromDiscovery emits the
// gmail.users.messages.list op from the Gmail fixture.
func TestGenerateEmitsGmailMessagesGet(t *testing.T) {
	f, err := os.Open("testdata/gmail-discovery.json")
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer func() { _ = f.Close() }()

	cat, err := gencatalog.GenerateFromDiscovery(f)
	if err != nil {
		t.Fatalf("GenerateFromDiscovery(gmail): %v", err)
	}

	found := false
	for _, op := range cat.Ops {
		if op.OpID == "gmail.users.messages.list" {
			found = true
			// Must have at least one variant with a typed-rest-sdk binding.
			if len(op.Variants) == 0 {
				t.Error("gmail.users.messages.list: no variants emitted")
			}
			hasBinding := false
			for _, v := range op.Variants {
				if v.Binding != nil && v.Binding.HTTP != nil {
					hasBinding = true
					if !strings.Contains(v.Binding.HTTP.Path, "messages") {
						t.Errorf("gmail variant HTTP path %q does not contain 'messages'", v.Binding.HTTP.Path)
					}
				}
			}
			if !hasBinding {
				t.Error("gmail.users.messages.list: no variant has an HTTP binding")
			}
			break
		}
	}
	if !found {
		t.Error("generator did not emit gmail.users.messages.list op")
	}
}

// TestGenerateEmitsCalendarEventsList verifies that GenerateFromDiscovery emits
// the calendar.events.list op from the Calendar fixture.
func TestGenerateEmitsCalendarEventsList(t *testing.T) {
	f, err := os.Open("testdata/calendar-discovery.json")
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer func() { _ = f.Close() }()

	cat, err := gencatalog.GenerateFromDiscovery(f)
	if err != nil {
		// If the generator hasn't been extended to handle Calendar yet, fail clearly.
		t.Fatalf("GenerateFromDiscovery(calendar): %v — green team must extend generator to handle calendar.events.list", err)
	}

	if err := cat.Validate(); err != nil {
		t.Fatalf("Validate() after calendar generation: %v", err)
	}

	found := false
	for _, op := range cat.Ops {
		if op.OpID == "calendar.events.list" {
			found = true
			if len(op.Variants) == 0 {
				t.Error("calendar.events.list: no variants emitted")
			}
			for _, v := range op.Variants {
				if v.RiskClass != catalog.RiskClassRead {
					t.Errorf("calendar.events.list variant %q: risk_class=%q, want 'read'", v.VariantID, v.RiskClass)
				}
				if v.Binding != nil && v.Binding.HTTP != nil {
					if !strings.Contains(v.Binding.HTTP.Path, "events") {
						t.Errorf("calendar variant HTTP path %q does not contain 'events'", v.Binding.HTTP.Path)
					}
				}
			}
			break
		}
	}
	if !found {
		t.Error("generator did not emit calendar.events.list op")
	}
}

// TestGenerateRejectsGumOAuthEmission verifies that the generator never emits
// auth_strategy = "gum_oauth" for any variant. This is a hard invariant per
// bd memory gum-auth-strategy-v3.
func TestGenerateRejectsGumOAuthEmission(t *testing.T) {
	fixtures := []string{
		"testdata/gmail-discovery.json",
		"testdata/calendar-discovery.json",
	}

	for _, path := range fixtures {
		f, err := os.Open(path)
		if err != nil {
			t.Logf("skipping %s (not found): %v", path, err)
			continue
		}
		defer func() { _ = f.Close() }()

		cat, err := gencatalog.GenerateFromDiscovery(f)
		if err != nil {
			// Generation failure is fine here; we only check generated content.
			continue
		}

		for _, op := range cat.Ops {
			for _, v := range op.Variants {
				if v.AuthStrategy == catalog.AuthStrategyGUMOAuth {
					t.Errorf(
						"fixture %s: op %s variant %s emits auth_strategy=%q — "+
							"gum_oauth MUST NOT be emitted by the generator in v0.1.0",
						path, op.OpID, v.VariantID, v.AuthStrategy,
					)
				}
			}
		}
	}
}
