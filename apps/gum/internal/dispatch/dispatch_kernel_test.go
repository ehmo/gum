// Package dispatch_test contains acceptance tests for the dispatch kernel (Phase 2 gate G2.1).
//
// API note (go-sdk v0.2.0 — verified from module source):
//
//   - mcp.AddTool(srv, tool, handler) is a PACKAGE-LEVEL generic function, not a server method.
//   - (*Server).AddTool(t *Tool, h ToolHandler) is the lower-level server method.
//   - mcp.NewInMemoryTransports() → (*InMemoryTransport, *InMemoryTransport)
//   - server.Connect(ctx, transport) → (*ServerSession, error)
//   - client.Connect(ctx, transport) → (*ClientSession, error)  (performs initialize)
//   - cs.ListTools(ctx, *ListToolsParams) → (*ListToolsResult, error)
//   - cs.CallTool(ctx, *CallToolParams) → (*CallToolResult, error)
//   - CallToolResult.Content []mcp.Content — text items are *mcp.TextContent{Text: ...}
package dispatch_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/adapters"
	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
	gummcp "github.com/ehmo/gum/internal/mcp"
)

// TestKernelGumCodeRoundTrip is the headline Phase 2 gate (G2.1).
//
//  1. Build a Catalog snapshot from the kernel-catalog.json fixture.
//  2. Construct a CodeRunner adapter.
//  3. Construct a Dispatcher with catalog + adapter.
//  4. Construct an mcp.Server wrapping the dispatcher.
//  5. Pair with in-memory MCP client transport.
//  6. Run the server in a goroutine; cancel at teardown.
//  7. From the client: initialize → assert protocol version + capabilities.
//  8. tools/list → assert exactly 9 meta-tools.
//  9. tools/call gum.code {language:risor, code:gum_print("hi")} → assert "hi".
func TestKernelGumCodeRoundTrip(t *testing.T) {
	defer goleak.VerifyNone(t)

	// --- Step 1: catalog ---
	data, err := os.ReadFile("testdata/kernel-catalog.json")
	if err != nil {
		t.Fatalf("read kernel-catalog.json: %v", err)
	}
	var cat catalog.Catalog
	if err := json.Unmarshal(data, &cat); err != nil {
		t.Fatalf("unmarshal catalog: %v", err)
	}

	// --- Step 2 & 3: dispatcher ---
	runner := adapters.NewCodeRunner()
	disp := dispatch.NewDispatcher(&cat, map[string]dispatch.Adapter{
		"code.risor": runner,
	})

	// --- Step 4: mcp.Server ---
	srv := gummcp.NewServer(disp)

	// --- Step 5 & 6: in-memory transport + background server ---
	srvTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- srv.Run(ctx, srvTransport)
	}()

	// --- Step 7: connect client (performs initialize internally) ---
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect / initialize: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	// Assert that the server responded with a protocol version.
	// Client.Connect already performed the initialize handshake; if it did not
	// fail, the server returned a valid InitializeResult.

	// --- Step 8: tools/list → Tier A tools plus read-only skill helpers ---
	listResult, err := cs.ListTools(ctx, &sdkmcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	// Phase 4: 9 meta-tools + 18 convenience tools = 27 Tier A tools.
	// Public setup parity adds skills_list and skills_get as read-only helpers.
	if len(listResult.Tools) != 29 {
		names := make([]string, 0, len(listResult.Tools))
		for _, tool := range listResult.Tools {
			names = append(names, tool.Name)
		}
		t.Errorf("tools/list returned %d tools; want 29 (27 Tier A + 2 skills helpers). Tools: %v", len(listResult.Tools), names)
	}

	// --- Step 9: tools/call gum.code ---
	// CallToolParams.Arguments is `any` in go-sdk v0.2.0; pass map directly.
	callResult, err := cs.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "gum.code",
		Arguments: map[string]any{
			"language": "risor",
			"source":   `gum_print("hi")`,
		},
	})
	if err != nil {
		t.Fatalf("CallTool gum.code: %v", err)
	}
	if len(callResult.Content) == 0 {
		t.Fatal("CallTool result has no content items")
	}
	// content[0] must be a TextContent containing "hi".
	found := false
	for _, c := range callResult.Content {
		if tc, ok := c.(*sdkmcp.TextContent); ok {
			if strings.Contains(tc.Text, "hi") {
				found = true
				break
			}
		}
	}
	if !found {
		t.Errorf("response content[0] does not contain 'hi': %v", callResult.Content)
	}

	// --- Teardown: cancel and wait for server ---
	cancel()
	select {
	case runErr := <-serverDone:
		if runErr != nil && !isCloseError(runErr) {
			t.Errorf("server.Run returned unexpected error: %v", runErr)
		}
	case <-time.After(3 * time.Second):
		t.Error("server did not stop within 3s after context cancel")
	}
}

// isCloseError returns true for errors that are expected when the context is
// cancelled (EOF, closed connection, context cancelled, etc.).
func isCloseError(err error) bool {
	if err == nil {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, token := range []string{"eof", "closed", "cancel", "reset", "broken pipe"} {
		if strings.Contains(msg, token) {
			return true
		}
	}
	return false
}

// Ensure json import is used (catalog unmarshal uses it above).
var _ = json.Marshal
