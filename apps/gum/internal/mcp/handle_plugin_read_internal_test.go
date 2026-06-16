package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
)

func detailOfJSONRPCError(t *testing.T, err error) string {
	t.Helper()
	var je *jsonrpc.Error
	if !errors.As(err, &je) {
		t.Fatalf("err %v is not *jsonrpc.Error", err)
	}
	var env map[string]any
	if uerr := json.Unmarshal(je.Data, &env); uerr != nil {
		t.Fatalf("Data not JSON: %v", uerr)
	}
	s, _ := env["detail"].(string)
	return s
}

func newReadReq(uri string) *sdkmcp.ReadResourceRequest {
	r := &sdkmcp.ReadResourceRequest{}
	r.Params = &sdkmcp.ReadResourceParams{URI: uri}
	return r
}

// TestHandlePluginReadBadURI pins the parseTemplateParam rejection
// branch: an URI that doesn't carry the gum://plugin/ prefix (or
// carries a forbidden char) must surface RESOURCE_NOT_FOUND, NOT a
// crash on the empty-name path.
func TestHandlePluginReadBadURI(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	s := &Server{profile: "default"}
	cases := []string{
		"gum://plugin/", // empty tail
		"gum://plugin/has/slash",
		"unrelated://x",
	}
	for _, uri := range cases {
		_, err := s.handlePluginRead(context.Background(), newReadReq(uri))
		if err == nil {
			t.Errorf("uri=%q: want RESOURCE_NOT_FOUND error; got nil", uri)
			continue
		}
		if d := detailOfJSONRPCError(t, err); !strings.Contains(d, "grammar rejected") {
			t.Errorf("uri=%q: detail=%q; want 'grammar rejected' wrap", uri, d)
		}
	}
}

// TestHandlePluginReadUnknownPluginNotFound pins the loadPluginResourceRecord
// negative branch: a syntactically-valid name with no plugins.lock or
// plugin-state.json row returns "not installed" so MCP hosts get a
// stable 404-style error instead of an empty JSON object.
func TestHandlePluginReadUnknownPluginNotFound(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	if err := os.MkdirAll(filepath.Join(dataHome, "gum", "default"), 0o700); err != nil {
		t.Fatal(err)
	}
	s := &Server{profile: "default"}
	_, err := s.handlePluginRead(context.Background(), newReadReq("gum://plugin/ghost.plugin"))
	if err == nil {
		t.Fatal("want not-installed error; got nil")
	}
	if d := detailOfJSONRPCError(t, err); !strings.Contains(d, "not installed") {
		t.Errorf("detail=%q; want 'not installed' wrap", d)
	}
}

// TestHandlePluginReadHappyPath pins the JSON-shape contract: a known
// plugin (lock + state present) is returned as canonical JSON in a
// single text content block with mime=application/json.
func TestHandlePluginReadHappyPath(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	dir := filepath.Join(dataHome, "gum", "default")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugins.lock"),
		[]byte(`{"plugins":[{"name":"alpha.plugin","version":"1.0.0","description":"d"}]}`),
		0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin-state.json"),
		[]byte(`{"plugins":[{"name":"alpha.plugin","status":"active","installed_at":"2026-01-01T00:00:00Z"}]}`),
		0o600); err != nil {
		t.Fatal(err)
	}

	s := &Server{profile: "default"}
	res, err := s.handlePluginRead(context.Background(), newReadReq("gum://plugin/alpha.plugin"))
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if res == nil || len(res.Contents) != 1 {
		t.Fatalf("contents=%+v; want exactly 1", res)
	}
	c := res.Contents[0]
	if c.MIMEType != "application/json" {
		t.Errorf("mime=%q; want application/json", c.MIMEType)
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(c.Text), &rec); err != nil {
		t.Fatalf("not JSON: %v\nraw=%s", err, c.Text)
	}
	if rec["name"] != "alpha.plugin" {
		t.Errorf("name=%v; want alpha.plugin", rec["name"])
	}
	if rec["version"] != "1.0.0" {
		t.Errorf("version=%v; want 1.0.0", rec["version"])
	}
	if rec["status"] != "active" {
		t.Errorf("status=%v; want active", rec["status"])
	}
}
