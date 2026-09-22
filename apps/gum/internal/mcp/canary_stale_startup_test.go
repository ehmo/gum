// Bead gum-yh7p / docs/test-matrix.md row 180.
//
// Spec §13 line 3252 makes the startup shape of gum://status/canaries
// normative: "on server startup, before any passive cron run has completed,
// the resource MUST return rows with status = \"stale\" for every known plugin
// canary, including freshly-installed-but-never-run canaries." The same
// paragraph names TestCanaryStaleOnStartup as the proof.
//
// gum 2.0.x hardcoded `count: 0`, so an operator with three installed plugins
// saw an empty roster and could not tell "no canaries" from "canaries never
// ran". These tests pin the roster, not the status: every row stays stale
// until the §8.5 passive runner exists.

package mcp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/goleak"
)

// canaryRows splits a canaries body into its header lines and data rows.
func canaryRows(t *testing.T, body string) []string {
	t.Helper()
	parts := strings.SplitN(body, "\n\n", 2)
	if len(parts) != 2 {
		t.Fatalf("body has no blank line between header and rows:\n%s", body)
	}
	var rows []string
	for _, line := range strings.Split(parts[1], "\n") {
		if line != "" {
			rows = append(rows, line)
		}
	}
	return rows
}

// TestCanaryStaleOnStartup is the spec-named proof: an MCP server with
// installed plugin canaries reports every one of them as stale before any
// cron tick fires. installed_pending_restart counts as known: the spec says
// "including freshly-installed-but-never-run canaries", and gum://plugins'
// pending-restart filter does not carry over here.
func TestCanaryStaleOnStartup(t *testing.T) {
	defer goleak.VerifyNone(t)

	ctx, cs, profileDir, cleanup := connectResourceClient(t)
	defer cleanup()

	writePluginFilesAt(t, profileDir,
		map[string]any{"plugins": []map[string]any{
			{"name": "zeta", "status": "active"},
			{"name": "alpha", "status": "installed_pending_restart"},
			{"name": "mid", "status": "quarantined"},
		}},
		map[string]any{"plugins": []map[string]any{
			{"name": "zeta", "version": "2.0.0", "shape": "mcp_subprocess", "tos": "accepted", "risk": "read", "variant_count": 4},
			{"name": "alpha", "version": "1.0.0", "shape": "mcp_subprocess", "tos": "accepted", "risk": "read", "variant_count": 1},
			{"name": "mid", "version": "0.9.0", "shape": "mcp_subprocess", "tos": "accepted", "risk": "write", "variant_count": 2},
		}},
	)

	got, err := cs.ReadResource(ctx, &sdkmcp.ReadResourceParams{URI: "gum://status/canaries"})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	body := got.Contents[0].Text

	if !strings.Contains(body, "count: 3") {
		t.Errorf("body missing 'count: 3'; three plugins are installed:\n%s", body)
	}

	rows := canaryRows(t, body)
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3:\n%s", len(rows), body)
	}

	// Sorted by canary_id lexicographically (spec §13 line 3252).
	wantIDs := []string{"alpha", "mid", "zeta"}
	for i, row := range rows {
		fields := strings.Split(row, ",")
		if len(fields) != 7 {
			t.Fatalf("row %q has %d fields, want 7", row, len(fields))
		}
		if fields[0] != wantIDs[i] {
			t.Errorf("row %d canary_id=%q, want %q (rows must sort by canary_id)", i, fields[0], wantIDs[i])
		}
		if fields[3] != "stale" {
			t.Errorf("row %d status=%q, want stale before any cron tick", i, fields[3])
		}
		if fields[4] != "" {
			t.Errorf("row %d last_run_at=%q, want empty: a never-run canary has no timestamp", i, fields[4])
		}
		if fields[5] != "" {
			t.Errorf("row %d latency_ms=%q, want empty: spec omits latency for stale", i, fields[5])
		}
		if fields[6] != "" {
			t.Errorf("row %d error_code=%q, want empty: error_code is set only when status=fail", i, fields[6])
		}
	}
}

// TestCanaryRosterTracksInstallsBetweenReads proves the roster is regenerated
// per resources/read (spec §13 line 3252), not captured at server startup.
func TestCanaryRosterTracksInstallsBetweenReads(t *testing.T) {
	defer goleak.VerifyNone(t)

	ctx, cs, profileDir, cleanup := connectResourceClient(t)
	defer cleanup()

	read := func() string {
		t.Helper()
		got, err := cs.ReadResource(ctx, &sdkmcp.ReadResourceParams{URI: "gum://status/canaries"})
		if err != nil {
			t.Fatalf("ReadResource: %v", err)
		}
		return got.Contents[0].Text
	}

	if body := read(); !strings.Contains(body, "count: 0") {
		t.Fatalf("pre-install read must be empty; got:\n%s", body)
	}

	writePluginFilesAt(t, profileDir,
		map[string]any{"plugins": []map[string]any{{"name": "fresh", "status": "installed_pending_restart"}}},
		map[string]any{"plugins": []map[string]any{{"name": "fresh", "version": "0.1.0", "shape": "mcp_subprocess", "tos": "accepted", "risk": "read", "variant_count": 1}}},
	)

	body := read()
	if !strings.Contains(body, "count: 1") {
		t.Errorf("post-install read must show the fresh canary; got:\n%s", body)
	}
	if !strings.Contains(body, "fresh,,,stale,,,") {
		t.Errorf("fresh canary row missing or malformed; got:\n%s", body)
	}
}

// writePluginFilesAt seeds profileDir, creating it first. The sibling helper
// in handle_plugins_read_test.go assumes the directory already exists.
func writePluginFilesAt(t *testing.T, profileDir string, state, lock map[string]any) {
	t.Helper()
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatalf("MkdirAll %s: %v", profileDir, err)
	}
	writeJSONFile(t, filepath.Join(profileDir, "plugin-state.json"), state)
	writeJSONFile(t, filepath.Join(profileDir, "plugins.lock"), lock)
}
