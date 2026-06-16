package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestSmokeCLIVersion builds the binary and asserts `gum version` prints the
// configured version. This is the cheapest end-to-end check (compiles + boots).
func TestSmokeCLIVersion(t *testing.T) {
	bin := buildSmokeBinary(t)
	out, err := runWithTimeout(t, 5*time.Second, bin, "version")
	if err != nil {
		t.Fatalf("gum version: %v\noutput: %s", err, out)
	}
	if strings.TrimSpace(out) == "" {
		t.Errorf("gum version produced no output")
	}
}

// TestSmokeCLISearch verifies that `gum search` against the embedded catalog
// returns at least one real BM25 hit. This guards against the catalog being
// accidentally emptied or the BM25 index failing to build.
func TestSmokeCLISearch(t *testing.T) {
	bin := buildSmokeBinary(t)
	out, err := runWithTimeout(t, 5*time.Second, bin, "search", "gmail")
	if err != nil {
		t.Fatalf("gum search: %v\noutput: %s", err, out)
	}
	var payload struct {
		Results []map[string]any `json:"results"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("decode search output: %v\noutput: %s", err, out)
	}
	if len(payload.Results) == 0 {
		t.Errorf("gum search gmail returned no results; embedded catalog may be empty")
	}
}

// TestSmokeCLIDescribe verifies that describe returns a known op.
func TestSmokeCLIDescribe(t *testing.T) {
	bin := buildSmokeBinary(t)
	out, err := runWithTimeout(t, 5*time.Second, bin, "describe", "gmail.users.messages.list")
	if err != nil {
		t.Fatalf("gum describe: %v\noutput: %s", err, out)
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("decode describe output: %v\noutput: %s", err, out)
	}
	// gum-4gey.10: describe now wraps the catalog Op under "op" alongside
	// "example_args". Older callers reading op_id at the root need to descend.
	opNode, ok := envelope["op"].(map[string]any)
	if !ok {
		t.Fatalf("describe output missing \"op\" field; got: %s", out)
	}
	if opNode["op_id"] != "gmail.users.messages.list" {
		t.Errorf("describe op_id = %v, want gmail.users.messages.list", opNode["op_id"])
	}
	if _, ok := envelope["example_args"]; !ok {
		t.Errorf("describe output missing \"example_args\" key (gum-4gey.10); got: %s", out)
	}
}

// TestSmokeMCPToolsList boots the MCP stdio server, sends the initialize +
// tools/list handshake, and asserts that all 27 Tier A tools are advertised.
// This is the canonical "does the binary actually work" check.
func TestSmokeMCPToolsList(t *testing.T) {
	bin := buildSmokeBinary(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "mcp", "--stdio")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Start(); err != nil {
		t.Fatalf("start mcp: %v", err)
	}

	handshake := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"smoke","version":"0"}}}` + "\n" +
		`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}` + "\n"
	if _, err := stdin.Write([]byte(handshake)); err != nil {
		t.Fatal(err)
	}
	// Give the server a moment to respond before closing stdin.
	time.Sleep(500 * time.Millisecond)
	_ = stdin.Close()
	_ = cmd.Wait()

	body := stdout.String()
	// Count distinct tool names (each appears once in the tools/list response).
	expected := []string{
		"gum.search_apis", "gum.describe_op", "gum.read", "gum.write",
		"gum.destructive", "gum.code", "gum.poll", "gum.cache_stats", "gum.gain",
		"gmail_search", "gmail_send", "gmail_create_draft", "gmail_get_message",
		"drive_find", "drive_get_file", "drive_share",
		"calendar_upcoming", "calendar_create_event", "calendar_update_event",
		"docs_get", "docs_create", "sheets_read", "sheets_write", "slides_get",
		"tasks_list", "tasks_create", "flights_search",
	}
	for _, name := range expected {
		if !strings.Contains(body, `"name":"`+name+`"`) {
			t.Errorf("MCP tools/list missing %q\nbody=%s", name, body)
		}
	}
}

// TestSmokeMCPCallSearchAPIs verifies that calling gum.search_apis through MCP
// returns a real BM25 result body. It includes the reserved params._meta field
// because clients such as Claude Code attach progress tokens to tool calls.
func TestSmokeMCPCallSearchAPIs(t *testing.T) {
	bin := buildSmokeBinary(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "mcp", "--stdio")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Start(); err != nil {
		t.Fatalf("start mcp: %v", err)
	}

	handshake := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"smoke","version":"0"}}}` + "\n" +
		`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"gum.search_apis","arguments":{"query":"gmail messages","k":3},"_meta":{"progressToken":2}}}` + "\n"
	if _, err := stdin.Write([]byte(handshake)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	_ = stdin.Close()
	_ = cmd.Wait()

	body := stdout.String()
	if strings.Contains(body, `"code":-32602`) || strings.Contains(body, "invalid params") {
		t.Fatalf("MCP tools/call rejected reserved params._meta\nbody=%s", body)
	}
	if !strings.Contains(body, "gmail.users.messages") {
		t.Errorf("MCP gum.search_apis did not return a gmail result\nbody=%s", body)
	}
}

// buildSmokeBinary compiles cmd/gum into a temp dir and returns the binary path.
func buildSmokeBinary(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("smoke build skipped in -short mode")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "gum-smoke")
	root := findRepoRoot(t)
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/gum")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build cmd/gum: %v\n%s", err, out)
	}
	return bin
}

// runWithTimeout runs the binary with args under a context deadline. Returns
// combined stdout and the run error.
func runWithTimeout(t *testing.T, d time.Duration, bin string, args ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	out, err := cmd.Output()
	return string(out), err
}
