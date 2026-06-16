package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// newPluginsReadReq is the minimal request shape handlePluginsRead reads:
// only Params.URI is consumed.
func newPluginsReadReq(uri string) *sdkmcp.ReadResourceRequest {
	return &sdkmcp.ReadResourceRequest{Params: &sdkmcp.ReadResourceParams{URI: uri}}
}

// writePluginFiles seeds the profile dir with plugin-state.json and
// plugins.lock so loadPluginInventoryRows has data to fold together.
func writePluginFiles(t *testing.T, dir string, state, lock map[string]any) {
	t.Helper()
	stateBytes, _ := json.Marshal(state)
	lockBytes, _ := json.Marshal(lock)
	if err := os.WriteFile(filepath.Join(dir, "plugin-state.json"), stateBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugins.lock"), lockBytes, 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestHandlePluginsReadEmptyProfile drives the empty-profileDir branch
// in loadPluginInventoryRows (XDG_DATA_HOME empty + HOME unset → "")
// AND the count:0 header path in handlePluginsRead. The handler must
// still return a valid TOON envelope, never an error.
func TestHandlePluginsReadEmptyProfile(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", "")
	if os.Getenv("USERPROFILE") != "" {
		t.Skip("Windows USERPROFILE drives UserHomeDir; not the empty-dir branch")
	}
	s := &Server{}
	res, err := s.handlePluginsRead(context.Background(), newPluginsReadReq("gum://plugins"))
	if err != nil {
		t.Fatalf("handlePluginsRead: %v", err)
	}
	if len(res.Contents) != 1 {
		t.Fatalf("Contents=%d; want 1", len(res.Contents))
	}
	if !strings.Contains(res.Contents[0].Text, "count: 0") {
		t.Errorf("missing count: 0 header in:\n%s", res.Contents[0].Text)
	}
}

// TestHandlePluginsReadHappyPath seeds plugin-state.json + plugins.lock
// with two plugins and asserts that the rendered TOON contains both
// row payloads. This drives the for-range body and csvField formatting.
func TestHandlePluginsReadHappyPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	profileDir := filepath.Join(dir, "gum", "default")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writePluginFiles(t, profileDir,
		map[string]any{
			"plugins": []map[string]any{
				{"name": "alpha", "status": "active"},
				{"name": "beta", "status": "active"},
			},
		},
		map[string]any{
			"plugins": []map[string]any{
				{"name": "alpha", "version": "1.0.0", "shape": "mcp-plugin", "tos": "accepted", "risk": "low", "variant_count": 3},
				{"name": "beta", "version": "0.5.1", "shape": "mcp-plugin", "tos": "accepted", "risk": "medium", "variant_count": 1},
			},
		},
	)

	s := &Server{}
	res, err := s.handlePluginsRead(context.Background(), newPluginsReadReq("gum://plugins"))
	if err != nil {
		t.Fatalf("handlePluginsRead: %v", err)
	}
	body := res.Contents[0].Text
	for _, want := range []string{"count: 2", "alpha,1.0.0,mcp-plugin,active,accepted,low,3", "beta,0.5.1,mcp-plugin,active,accepted,medium,1"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in output:\n%s", want, body)
		}
	}
}

// TestHandlePluginsReadFiltersInstalledPendingRestart asserts the spec
// §13 line 3148 filter: rows in installed_pending_restart status must
// not appear in the MCP inventory (gum plugin list still shows them).
func TestHandlePluginsReadFiltersInstalledPendingRestart(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	profileDir := filepath.Join(dir, "gum", "default")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writePluginFiles(t, profileDir,
		map[string]any{
			"plugins": []map[string]any{
				{"name": "visible", "status": "active"},
				{"name": "hidden", "status": "installed_pending_restart"},
			},
		},
		map[string]any{"plugins": []map[string]any{}},
	)

	s := &Server{}
	res, _ := s.handlePluginsRead(context.Background(), newPluginsReadReq("gum://plugins"))
	body := res.Contents[0].Text
	if !strings.Contains(body, "count: 1") {
		t.Errorf("want count:1 (hidden filtered); got:\n%s", body)
	}
	if strings.Contains(body, "hidden") {
		t.Errorf("installed_pending_restart row leaked:\n%s", body)
	}
}

// TestHandlePluginsReadQuarantinedStatus drives resolvePluginStatus's
// quarantined-wins precedence: a plugin with quarantined:true must be
// reported as "quarantined" regardless of its status field.
func TestHandlePluginsReadQuarantinedStatus(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	profileDir := filepath.Join(dir, "gum", "default")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writePluginFiles(t, profileDir,
		map[string]any{
			"plugins": []map[string]any{
				{"name": "broken", "quarantined": true, "status": "active"},
			},
		},
		map[string]any{"plugins": []map[string]any{}},
	)

	s := &Server{}
	res, _ := s.handlePluginsRead(context.Background(), newPluginsReadReq("gum://plugins"))
	body := res.Contents[0].Text
	if !strings.Contains(body, "broken,") || !strings.Contains(body, ",quarantined,") {
		t.Errorf("expected quarantined status for broken, got:\n%s", body)
	}
}
