// Package mcp_test contains acceptance tests for the MCP server presentation layer.
//
// API note (go-sdk v0.2.0):
//   - Package-level function: mcp.AddTool(srv, tool, handler) — NOT a method call.
//   - Server method: (*Server).AddTool(t *Tool, h ToolHandler) also exists (lower-level).
//   - Transport: mcp.NewInMemoryTransports() → (*InMemoryTransport, *InMemoryTransport)
//   - Server.Connect(ctx, transport) → (*ServerSession, error) — use for in-memory.
//   - Server.Run(ctx, transport) error — convenience wrapper (for stdio usage).
//   - Client.Connect(ctx, transport) → (*ClientSession, error) — does initialize.
//   - ClientSession.ListTools(ctx, *ListToolsParams) → (*ListToolsResult, error)
//   - ClientSession.CallTool(ctx, *CallToolParams) → (*CallToolResult, error)
//   - CallToolResult.Content is []mcp.Content; text content: *mcp.TextContent{Text: ...}
package mcp_test

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/dispatch"
	gummcp "github.com/ehmo/gum/internal/mcp"
)

func textContent(t *testing.T, res *sdkmcp.CallToolResult) string {
	t.Helper()
	if res == nil || len(res.Content) == 0 {
		return ""
	}
	text, ok := res.Content[0].(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("content[0] = %T, want *TextContent", res.Content[0])
	}
	return text.Text
}

// stubDispatcher is a no-op dispatcher used for structural tests that don't
// exercise actual dispatch.
type stubDispatcher struct{}

func (stubDispatcher) Dispatch(_ context.Context, _ *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
	panic("not implemented in stub")
}

// loadRosterNames reads docs/tier-a-roster.v1.json and returns the meta_tools list.
func loadRosterNames(t *testing.T) []string {
	t.Helper()
	// Path relative to module root; the test runs from the module root.
	data, err := os.ReadFile("../embedded/data/tier-a-roster.v1.json")
	if err != nil {
		t.Fatalf("loadRosterNames: %v", err)
	}
	var roster struct {
		MetaTools []string `json:"meta_tools"`
	}
	if err := json.Unmarshal(data, &roster); err != nil {
		t.Fatalf("loadRosterNames unmarshal: %v", err)
	}
	return roster.MetaTools
}

// TestMetaToolNamesReturnsExactly9 checks that MetaToolNames returns exactly
// the 9 tools from docs/tier-a-roster.v1.json.
func TestMetaToolNamesReturnsExactly9(t *testing.T) {
	defer goleak.VerifyNone(t)

	expected := loadRosterNames(t)
	srv := gummcp.NewServer(stubDispatcher{})
	got := srv.MetaToolNames()

	if len(got) != 9 {
		t.Errorf("MetaToolNames returned %d names; want 9. Got: %v", len(got), got)
	}

	expectedSet := map[string]bool{}
	for _, n := range expected {
		expectedSet[n] = true
	}
	gotSet := map[string]bool{}
	for _, n := range got {
		gotSet[n] = true
	}
	for name := range expectedSet {
		if !gotSet[name] {
			t.Errorf("missing meta-tool %q", name)
		}
	}
	for name := range gotSet {
		if !expectedSet[name] {
			t.Errorf("unexpected meta-tool %q", name)
		}
	}
}

func TestSkillsToolsHandleListAndGet(t *testing.T) {
	srv := gummcp.NewServer(stubDispatcher{})
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	serverDone := make(chan error, 1)
	go func() { serverDone <- srv.Run(ctx, serverTransport) }()
	t.Cleanup(func() {
		cancel()
		<-serverDone
	})
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	list, err := cs.CallTool(ctx, &sdkmcp.CallToolParams{Name: "skills_list", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("skills_list: %v", err)
	}
	if list.IsError || !strings.Contains(textContent(t, list), `"name":"core"`) {
		t.Fatalf("skills_list result=%#v text=%q", list, textContent(t, list))
	}

	got, err := cs.CallTool(ctx, &sdkmcp.CallToolParams{Name: "skills_get", Arguments: map[string]any{"name": "core", "max_bytes": 20}})
	if err != nil {
		t.Fatalf("skills_get: %v", err)
	}
	body := textContent(t, got)
	if got.IsError || !strings.Contains(body, `"truncated":true`) || !strings.Contains(body, "# gum core") {
		t.Fatalf("skills_get result=%#v text=%q", got, body)
	}
	if got.StructuredContent != nil {
		t.Fatalf("skills_get emitted StructuredContent=%#v; text-only skill bodies must stay in TextContent for client compatibility", got.StructuredContent)
	}

	bad, err := cs.CallTool(ctx, &sdkmcp.CallToolParams{Name: "skills_get", Arguments: map[string]any{"name": "Bad"}})
	if err != nil {
		t.Fatalf("bad skills_get: %v", err)
	}
	if !bad.IsError || !strings.Contains(textContent(t, bad), "INVALID_ARGS") {
		t.Fatalf("bad skills_get result=%#v text=%q", bad, textContent(t, bad))
	}
}

// TestMCPRegistryStructurallyMutable verifies that calling AddTool on the
// underlying go-sdk server after Run does NOT panic. This documents the
// spec.md §4.2 invariant: all registrations happen BEFORE Run, but the SDK
// itself must not enforce a hard post-Run lock (i.e. the registry stays
// structurally mutable even if gum never adds tools after Run).
func TestMCPRegistryStructurallyMutable(t *testing.T) {
	defer goleak.VerifyNone(t)

	sdkSrv := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "gum", Version: "0.1.0-test"}, nil)

	// Start the server with a context we can cancel.
	ctx, cancel := context.WithCancel(context.Background())
	srvTransport, _ := sdkmcp.NewInMemoryTransports()
	done := make(chan error, 1)
	go func() {
		done <- sdkSrv.Run(ctx, srvTransport)
	}()

	// Cancel to stop the server.
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not stop within 2s after context cancel")
	}

	// AddTool after Run must not panic.
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("AddTool after Run panicked: %v", r)
		}
	}()
	sdkSrv.AddTool(&sdkmcp.Tool{
		Name:        "post-run-tool",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		return &sdkmcp.CallToolResult{}, nil
	})
}

// TestServerRunSurvivesShutdown starts the server, waits for it to be ready,
// then closes the client session (equivalent to sending shutdown + exit), and
// asserts Run returns within 2 seconds without error.
func TestServerRunSurvivesShutdown(t *testing.T) {
	defer goleak.VerifyNone(t)

	srv := gummcp.NewServer(stubDispatcher{})
	srvTransport, clientTransport := sdkmcp.NewInMemoryTransports()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- srv.Run(ctx, srvTransport)
	}()

	// Connect a client; Client.Connect performs initialize/initialized.
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}

	// Close the client; this triggers graceful shutdown on the server side.
	if err := cs.Close(); err != nil {
		t.Logf("cs.Close: %v (may be benign)", err)
	}

	select {
	case runErr := <-done:
		if runErr != nil {
			t.Errorf("Server.Run returned error: %v", runErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Server.Run did not return within 2s after client shutdown")
	}
	_ = sort.Search // suppress import if unused
}
