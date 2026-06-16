package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestHandlePollDefaultFactoryFallsThroughWhenNotInjected pins
// handlers.go:343-345 — `factory == nil → factory = s.defaultPollerFactory`.
// Existing poll tests inject a custom pollerFactory to control the
// outcome; this one leaves it nil so the default factory path is
// exercised. The operation_name is intentionally not routable so the
// HTTPFetcher fails fast (ErrUnroutable) and we end up at the generic
// errorResult arm rather than blocking on real HTTP.
func TestHandlePollDefaultFactoryFallsThroughWhenNotInjected(t *testing.T) {
	srv := NewServerWithCatalog(noopDispatcher{}, defaultCatalog())
	// Deliberately DO NOT set srv.pollerFactory — exercises default arm.

	raw, _ := json.Marshal(map[string]any{
		"operation_name": "operations/not-a-real-lro-anywhere-xyz",
	})
	req := &sdkmcp.CallToolRequest{
		Params: &sdkmcp.CallToolParamsRaw{Name: "gum.poll", Arguments: raw},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel so Poll returns immediately via ctx.Done()

	result, err := srv.handlePoll(ctx, req)
	if err != nil {
		t.Fatalf("handlePoll go err: %v", err)
	}
	if result == nil {
		t.Fatal("nil result; want a tool result")
	}
	// What matters is that handlePoll returned a result rather than
	// panicking on a nil factory. The default poller's HTTPFetcher
	// surfaces LRO_UNROUTABLE for unknown operation names, which also
	// covers handlers.go:362 (generic errorResult fall-through —
	// neither TimeoutError nor context.Canceled despite the pre-cancel,
	// because the Poller's nil routing-table lookup short-circuits
	// before the ctx-done check).
	if !result.IsError {
		t.Errorf("result.IsError=false; want true (default poller must surface failure for unroutable op)")
	}
	var body strings.Builder
	for _, c := range result.Content {
		if tc, ok := c.(*sdkmcp.TextContent); ok {
			body.WriteString(tc.Text)
		}
	}
	if body.Len() == 0 {
		t.Errorf("result text empty; want a non-empty envelope from default poller")
	}
}
