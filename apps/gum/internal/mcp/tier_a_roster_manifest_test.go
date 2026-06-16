// Package mcp — gum-6yn acceptance tests.
//
// Two test names called out by docs/test-matrix.md and spec §4.1 must exist
// and pass:
//
//   - TestTierAConvenienceToolCount (spec §370): the registered convenience
//     tool count equals 18 — the hard cap.
//   - TestTierARosterManifest (test-matrix row 24): loading active plugins
//     before Server.Run does not grow tools/list; the v0.1.0 roster matches
//     docs/tier-a-roster.v1.json (9 meta + 18 convenience) exactly.
package mcp

import (
	"encoding/json"
	"os"
	"testing"
)

// TestTierAConvenienceToolCount asserts the MCP server registers exactly 18
// convenience tools (spec §4.1 cap). Per spec line 370 this gate prevents
// silent Tier A bloat from leaking past the 8k schema-token budget.
func TestTierAConvenienceToolCount(t *testing.T) {
	srv := NewServer(noopDispatcher{})
	got := len(srv.ConvenienceToolNames())
	if got != 18 {
		t.Errorf("ConvenienceToolNames length = %d; want 18 (spec §4.1 hard cap)", got)
	}
}

// TestTierARosterManifest asserts the v0.1.0 tool surface — 9 meta + 18
// convenience — registered by the MCP server matches docs/tier-a-roster.v1.json
// exactly, both in count and in name set, and that constructing a server
// (which loads the embedded catalog but does NOT start any plugin subprocess)
// does not grow tools/list past the documented roster.
//
// docs/test-matrix.md row 24 names this test and pins the roster source of
// truth to docs/tier-a-roster.v1.json.
func TestTierARosterManifest(t *testing.T) {
	const rosterPath = "../../docs/tier-a-roster.v1.json"
	data, err := os.ReadFile(rosterPath)
	if err != nil {
		t.Fatalf("docs/tier-a-roster.v1.json not found at %s: %v", rosterPath, err)
	}

	var roster struct {
		MetaTools        []string `json:"meta_tools"`
		ConvenienceTools []string `json:"convenience_tools"`
	}
	if err := json.Unmarshal(data, &roster); err != nil {
		t.Fatalf("tier-a-roster.v1.json: parse error: %v", err)
	}
	if got := len(roster.MetaTools); got != 9 {
		t.Fatalf("roster.meta_tools count = %d; want 9", got)
	}
	if got := len(roster.ConvenienceTools); got != 18 {
		t.Fatalf("roster.convenience_tools count = %d; want 18", got)
	}

	srv := NewServer(noopDispatcher{})
	regMeta := srv.MetaToolNames()
	regConv := srv.ConvenienceToolNames()

	if len(regMeta) != 9 {
		t.Errorf("server registered %d meta-tools; want 9", len(regMeta))
	}
	if len(regConv) != 18 {
		t.Errorf("server registered %d convenience tools; want 18", len(regConv))
	}
	if total := len(srv.AllToolNames()); total != 27 {
		t.Errorf("AllToolNames length = %d; want 27 (9 meta + 18 convenience)", total)
	}

	// Set equality vs. roster.
	assertSetsEqual(t, regMeta, roster.MetaTools, "meta_tools")
	assertSetsEqual(t, regConv, roster.ConvenienceTools, "convenience_tools")
}

// assertSetsEqual fails if got and want are not the same set (order-insensitive).
func assertSetsEqual(t *testing.T, got, want []string, label string) {
	t.Helper()
	gotSet := make(map[string]bool, len(got))
	for _, s := range got {
		gotSet[s] = true
	}
	wantSet := make(map[string]bool, len(want))
	for _, s := range want {
		wantSet[s] = true
	}
	for s := range gotSet {
		if !wantSet[s] {
			t.Errorf("%s: registered %q not in roster", label, s)
		}
	}
	for s := range wantSet {
		if !gotSet[s] {
			t.Errorf("%s: roster lists %q but server did not register it", label, s)
		}
	}
}
