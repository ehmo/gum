package main

// Spec §13 line 3234 names one test for the two halves of the pending-restart
// rule: a plugin awaiting a restart is hidden from the gum://plugins MCP view,
// because an LLM client cannot dispatch it, and visible to the operator on the
// CLI, because the operator is the one who restarts it. Asserting both in one
// function is what keeps the split honest; two separate tests could each pass
// while the two surfaces drifted onto different data.

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/dispatch"
	gummcp "github.com/ehmo/gum/internal/mcp"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// inventoryDispatcher satisfies the Dispatcher the MCP server needs to start.
// The plugins resource reads the profile's registry files and never dispatches,
// so a call here means the test drove the wrong code path.
type inventoryDispatcher struct{}

func (inventoryDispatcher) Dispatch(context.Context, *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
	panic("gum://plugins must not dispatch")
}

// seedPendingRestartProfile writes the §8.7 registry pair for a profile holding
// one active plugin and one waiting on a restart, and points both surfaces at
// it. It returns the profile dir.
func seedPendingRestartProfile(t *testing.T) string {
	t.Helper()

	home := t.TempDir()
	t.Setenv("XDG_DATA_HOME", home)
	profileDir := filepath.Join(home, "gum", "default")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatal(err)
	}

	write := func(name string, body map[string]any) {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(profileDir, name), encoded, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("plugin-state.json", map[string]any{"plugins": []map[string]any{
		{"name": "ready", "status": "active"},
		{"name": "waiting", "status": "installed_pending_restart"},
	}})
	write("plugins.lock", map[string]any{"plugins": []map[string]any{
		{"name": "ready", "version": "1.2.0", "shape": "mcp-plugin", "tos": "accepted", "risk": "low", "variant_count": 4},
		{"name": "waiting", "version": "0.9.0", "shape": "mcp-plugin", "tos": "accepted", "risk": "medium", "variant_count": 2},
	}})
	return profileDir
}

// readPluginsResource runs one resources/read over an in-memory transport pair,
// so the assertion covers the registered resource rather than the loader behind
// it.
func readPluginsResource(t *testing.T) string {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	srvTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	done := make(chan error, 1)
	go func() { done <- gummcp.NewServer(inventoryDispatcher{}).Run(ctx, srvTransport) }()

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "plugins-view-test", Version: "0.0.1"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() {
		_ = session.Close()
		<-done
	})

	res, err := session.ReadResource(ctx, &sdkmcp.ReadResourceParams{URI: "gum://plugins"})
	if err != nil {
		t.Fatalf("read gum://plugins: %v", err)
	}
	if len(res.Contents) != 1 {
		t.Fatalf("Contents=%d; want 1", len(res.Contents))
	}
	return res.Contents[0].Text
}

// pluginListJSON runs `gum plugin list --format=json` through the real root
// command and returns the decoded §12 line 2537 root.
func pluginListJSON(t *testing.T) map[string][]gummcp.PluginInventoryRow {
	t.Helper()

	var stdout bytes.Buffer
	cmd := newRootCmd()
	cmd.SetArgs([]string{"plugin", "list", "--format=json"})
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("plugin list --format=json: %v\noutput: %s", err, stdout.String())
	}

	var root map[string][]gummcp.PluginInventoryRow
	if err := json.Unmarshal(stdout.Bytes(), &root); err != nil {
		t.Fatalf("decode %q: %v", stdout.String(), err)
	}
	if _, ok := root["plugins"]; !ok {
		t.Fatalf("root has no \"plugins\" key: %s", stdout.String())
	}
	return root
}

func TestPluginsResourceFiltersPendingRestart(t *testing.T) {
	seedPendingRestartProfile(t)

	body := readPluginsResource(t)
	if strings.Contains(body, "waiting") {
		t.Errorf("gum://plugins lists the installed_pending_restart plugin; spec §13 filters it:\n%s", body)
	}
	if !strings.Contains(body, "ready,1.2.0,mcp-plugin,active,accepted,low,4") {
		t.Errorf("gum://plugins dropped the active row:\n%s", body)
	}

	rows := pluginListJSON(t)["plugins"]
	byName := make(map[string]gummcp.PluginInventoryRow, len(rows))
	for _, row := range rows {
		byName[row.Name] = row
	}

	waiting, ok := byName["waiting"]
	if !ok {
		t.Fatalf("gum plugin list --format=json dropped the installed_pending_restart plugin; the operator who has to restart it sees it nowhere else. rows=%+v", rows)
	}
	if waiting.Status != "installed_pending_restart" {
		t.Errorf("waiting.Status = %q; want installed_pending_restart", waiting.Status)
	}
	if _, ok := byName["ready"]; !ok {
		t.Errorf("gum plugin list --format=json dropped the active plugin; rows=%+v", rows)
	}
}

// TestPluginListJSONRowsMirrorTheResourceColumns pins the §12 line 2537 claim
// that the JSON rows "mirror gum://plugins columns": same seven values, same
// source, named instead of positional.
func TestPluginListJSONRowsMirrorTheResourceColumns(t *testing.T) {
	seedPendingRestartProfile(t)

	var ready gummcp.PluginInventoryRow
	for _, row := range pluginListJSON(t)["plugins"] {
		if row.Name == "ready" {
			ready = row
		}
	}

	want := gummcp.PluginInventoryRow{
		Name: "ready", Version: "1.2.0", Shape: "mcp-plugin", Status: "active",
		ToS: "accepted", Risk: "low", VariantCount: 4,
	}
	if ready != want {
		t.Errorf("row = %+v; want %+v", ready, want)
	}

	// The TOON row the resource renders is these same fields in §13 column
	// order, so the CSV line has to fall out of the JSON row.
	csv := "ready,1.2.0,mcp-plugin,active,accepted,low,4"
	if !strings.Contains(readPluginsResource(t), csv) {
		t.Errorf("resource does not carry %q built from the same fields", csv)
	}
}

// TestPluginListJSONEmptyProfileIsValidJSON covers the no-plugins case: the
// text listing prints nothing so pipes stay clean, but a script parsing the
// JSON root needs a document, not an empty file.
func TestPluginListJSONEmptyProfileIsValidJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_DATA_HOME", home)

	var stdout bytes.Buffer
	cmd := newRootCmd()
	cmd.SetArgs([]string{"plugin", "list", "--format=json"})
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("plugin list --format=json: %v", err)
	}
	if got := strings.TrimSpace(stdout.String()); got != `{"plugins":[]}` {
		t.Errorf("empty profile printed %q; want {\"plugins\":[]}", got)
	}
}

// TestPluginListRejectsUnknownFormat keeps the flag a closed enum. Silently
// falling back to text would let a script that asked for JSON parse a table.
func TestPluginListRejectsUnknownFormat(t *testing.T) {
	seedPendingRestartProfile(t)

	var stdout bytes.Buffer
	cmd := newRootCmd()
	cmd.SetArgs([]string{"plugin", "list", "--format=yaml"})
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("--format=yaml was accepted; output: %s", stdout.String())
	}
	if !strings.Contains(err.Error(), "unsupported --format") {
		t.Errorf("error = %v; want it to name the unsupported format", err)
	}
}
