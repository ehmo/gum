package mcp

import (
	"context"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestHandleCompleteNilRequestReturnsEmpty pins the
// `req == nil || req.Params == nil || req.Params.Ref == nil → return
// emptyCompleteResult()` guard. The MCP SDK can theoretically deliver
// a request whose Params/Ref were dropped by a misbehaving transport
// or a future protocol version with new optional fields; the handler
// MUST degrade to an empty completion rather than panic on the
// req.Params.Ref.Type switch. The spec contract (Values: [], never
// nil) is what clients rely on to render "no suggestions" without
// crashing on a JSON null.
func TestHandleCompleteNilRequestReturnsEmpty(t *testing.T) {
	s := &Server{}
	tests := []struct {
		name string
		req  *sdkmcp.CompleteRequest
	}{
		{"nil request", nil},
		{"nil params", &sdkmcp.CompleteRequest{Params: nil}},
		{"nil ref", &sdkmcp.CompleteRequest{Params: &sdkmcp.CompleteParams{Ref: nil}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, err := s.handleComplete(context.Background(), tc.req)
			if err != nil {
				t.Fatalf("handleComplete: %v", err)
			}
			if res == nil {
				t.Fatal("res is nil; want emptyCompleteResult")
			}
			if res.Completion.Values == nil {
				t.Error("Values is nil; spec requires [], never nil")
			}
			if len(res.Completion.Values) != 0 {
				t.Errorf("Values=%v; want []", res.Completion.Values)
			}
		})
	}
}

// TestHandleCompleteUnknownRefTypeReturnsEmpty pins the
// `default: return emptyCompleteResult()` arm of the ref-type switch.
// MCP defines ref/resource and ref/prompt; a future SDK could add new
// ref types (e.g. ref/tool) and dispatch them to handlers that don't
// exist yet. The handler MUST treat unknown types as "no completions"
// rather than panicking on a missing case, so old gum binaries stay
// useful when clients upgrade ahead of the server.
func TestHandleCompleteUnknownRefTypeReturnsEmpty(t *testing.T) {
	s := &Server{}
	req := &sdkmcp.CompleteRequest{
		Params: &sdkmcp.CompleteParams{
			Ref: &sdkmcp.CompleteReference{Type: "ref/unknown-future-type", URI: "irrelevant"},
		},
	}
	res, err := s.handleComplete(context.Background(), req)
	if err != nil {
		t.Fatalf("handleComplete: %v", err)
	}
	if res == nil {
		t.Fatal("res is nil; want emptyCompleteResult")
	}
	if len(res.Completion.Values) != 0 {
		t.Errorf("Values=%v; want [] for unknown ref type", res.Completion.Values)
	}
}
