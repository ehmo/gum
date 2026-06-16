// gum-0fx1: acceptance for spec §9.2 dispatch-path integration of
// ResolveProjectRootForRequest. A Tier A tool call against a multi-root MCP
// session without `_meta.gumRoot` MUST surface the PROJECT_ROOT_REQUIRED
// envelope inside the CallToolResult — not just via the unit-test helper.

package mcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ehmo/gum/internal/dispatch"
	gummcp "github.com/ehmo/gum/internal/mcp"
)

// sentinelDispatcher returns a stable sentinel error so callers can assert
// "the request reached dispatch" without dealing with a panicking stub.
type sentinelDispatcher struct{ msg string }

func (d sentinelDispatcher) Dispatch(_ context.Context, _ *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
	return nil, errors.New(d.msg)
}

// TestMultiRootSessionWithoutGumRootReturnsProjectRootRequired drives the
// real Tier A handler path (gum.code, which always exists in the meta-tool
// roster). With two file roots advertised by the client and no
// `_meta.gumRoot` provided, the §9.2 selection rule MUST fail the request
// before dispatch hits the catalog — surfaced as a tool error whose JSON body
// carries the spec §1421 envelope (`error_code: PROJECT_ROOT_REQUIRED`,
// `reason: missing_gumroot_in_multi_root_session`, and both negotiated roots
// in `negotiated_roots`).
func TestMultiRootSessionWithoutGumRootReturnsProjectRootRequired(t *testing.T) {
	srv := gummcp.NewServer(stubDispatcher{})
	srvTransport, clientTransport := sdkmcp.NewInMemoryTransports()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx, srvTransport) }()

	client := sdkmcp.NewClient(
		&sdkmcp.Implementation{Name: "gum-0fx1-multi", Version: "0.0.1"},
		&sdkmcp.ClientOptions{
			Capabilities: &sdkmcp.ClientCapabilities{
				RootsV2: &sdkmcp.RootCapabilities{},
			},
		},
	)
	client.AddRoots(
		&sdkmcp.Root{URI: "file:///tmp/0fx1-a", Name: "a"},
		&sdkmcp.Root{URI: "file:///tmp/0fx1-b", Name: "b"},
	)

	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	defer func() { _ = cs.Close() }()

	result, err := cs.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "gum.code",
		Arguments: map[string]any{"language": "risor", "source": "1"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !result.IsError {
		t.Fatalf("result.IsError=false; want true (spec §9.2 multi-root without gumRoot must fail)")
	}
	if len(result.Content) == 0 {
		t.Fatal("result.Content empty; want PROJECT_ROOT_REQUIRED envelope text")
	}
	tc, ok := result.Content[0].(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("result.Content[0] = %T; want *TextContent", result.Content[0])
	}
	var env map[string]any
	if err := json.Unmarshal([]byte(tc.Text), &env); err != nil {
		t.Fatalf("envelope unmarshal failed (text=%q): %v", tc.Text, err)
	}
	if env["error_code"] != "PROJECT_ROOT_REQUIRED" {
		t.Errorf("error_code=%v; want PROJECT_ROOT_REQUIRED", env["error_code"])
	}
	if env["reason"] != "missing_gumroot_in_multi_root_session" {
		t.Errorf("reason=%v; want missing_gumroot_in_multi_root_session", env["reason"])
	}
	roots, ok := env["negotiated_roots"].([]any)
	if !ok || len(roots) != 2 {
		t.Errorf("negotiated_roots=%v; want both file URIs", env["negotiated_roots"])
	}
	if _, ok := env["user_message"]; !ok {
		t.Error("envelope missing user_message")
	}
}

// TestSingleRootSessionDispatchesNormally confirms that the §9.2 wiring does
// not break the happy path: a single file root advertised by the client
// resolves to that root and dispatch proceeds. We use a sentinel-error
// dispatcher so reaching dispatch is observable without the panic-recovery
// noise that a panic-stub would create.
func TestSingleRootSessionDispatchesNormally(t *testing.T) {
	const sentinel = "gum-0fx1-reached-dispatch"
	srv := gummcp.NewServer(sentinelDispatcher{msg: sentinel})
	srvTransport, clientTransport := sdkmcp.NewInMemoryTransports()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx, srvTransport) }()

	client := sdkmcp.NewClient(
		&sdkmcp.Implementation{Name: "gum-0fx1-single", Version: "0.0.1"},
		&sdkmcp.ClientOptions{
			Capabilities: &sdkmcp.ClientCapabilities{
				RootsV2: &sdkmcp.RootCapabilities{},
			},
		},
	)
	client.AddRoots(&sdkmcp.Root{URI: "file:///tmp/0fx1-only", Name: "only"})

	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	defer func() { _ = cs.Close() }()

	callCtx, callCancel := context.WithTimeout(ctx, 2*time.Second)
	defer callCancel()
	result, err := cs.CallTool(callCtx, &sdkmcp.CallToolParams{
		Name:      "gum.code",
		Arguments: map[string]any{"language": "risor", "source": "1"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !result.IsError || len(result.Content) == 0 {
		t.Fatalf("expected IsError with content; got IsError=%v content=%v", result.IsError, result.Content)
	}
	tc, ok := result.Content[0].(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("content[0] = %T; want *TextContent", result.Content[0])
	}
	if containsSubstring(tc.Text, "PROJECT_ROOT_REQUIRED") {
		t.Fatalf("single-root happy path incorrectly returned PROJECT_ROOT_REQUIRED envelope: %s", tc.Text)
	}
	if !containsSubstring(tc.Text, sentinel) {
		t.Errorf("expected dispatcher-side sentinel %q in error; got %q", sentinel, tc.Text)
	}
}

// containsSubstring is a tiny strings.Contains replacement to avoid one more
// import for two call sites.
func containsSubstring(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
