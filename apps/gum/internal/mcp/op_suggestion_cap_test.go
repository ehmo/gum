// Package mcp — regression test for the OP_NOT_FOUND suggestion cap.
//
// Defect: spec.md §4.1 (docs/spec.md:349) caps the OP_NOT_FOUND envelope at
// "up to 3 BM25-fuzzy matches". internal/dispatch honours that cap on both
// CLI paths, but handleRiskTier asked the search index for 5, so the same
// error code carried a different suggestion count depending on which surface
// produced it.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestOpNotFoundSuggestionsCappedAtThree seeds more near-miss ops than the cap
// allows, so an uncapped or wrongly-capped handler overflows.
func TestOpNotFoundSuggestionsCappedAtThree(t *testing.T) {
	ops := []catalog.Op{}
	for i := 1; i <= 8; i++ {
		ops = append(ops, minimalReadOp(fmt.Sprintf("gmail.users.messages.list%d", i), catalog.RiskClassRead))
	}
	srv := NewServerWithCatalog(&captureDispatcher{}, minimalCatalog(ops...))

	req := makeReadRequest(map[string]any{"op_id": "gmail.users.messages.list"})
	res, err := srv.handleRead(context.Background(), req)
	if err != nil {
		t.Fatalf("handleRead returned a Go error: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatal("expected an error result for an unknown op_id")
	}
	tc, ok := res.Content[0].(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("content[0] is %T; want *sdkmcp.TextContent", res.Content[0])
	}

	var env map[string]any
	if err := json.Unmarshal([]byte(tc.Text), &env); err != nil {
		t.Fatalf("envelope is not JSON: %v; body=%s", err, tc.Text)
	}
	if code, _ := env["error_code"].(string); code != "OP_NOT_FOUND" {
		t.Fatalf("error_code=%q; want OP_NOT_FOUND; body=%s", code, tc.Text)
	}
	suggestions, ok := env["suggestions"].([]any)
	if !ok {
		t.Fatalf("suggestions is %T; want an array; body=%s", env["suggestions"], tc.Text)
	}
	if len(suggestions) == 0 {
		t.Fatal("suggestions is empty; the seeded near-miss ops should rank")
	}
	if len(suggestions) > dispatch.MaxOpSuggestions {
		t.Errorf("suggestions has %d entries; spec §4.1 caps OP_NOT_FOUND at %d (%v)",
			len(suggestions), dispatch.MaxOpSuggestions, suggestions)
	}
}
