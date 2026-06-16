package mcp

import (
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestJSONResultMarshalFailureSurfacesAsErrorEnvelope pins the
// `json.Marshal err → return errorResult("JSON_ENCODE_FAILED: ...")` arm
// of jsonResult. The MCP handlers funnel structured results through
// this helper; if the handler ever hands it an unmarshalable value
// (e.g. via a buggy adapter or a stray chan in a map), the helper MUST
// degrade to a labelled error envelope so the operator sees a clean
// failure rather than the SDK panicking on a missing Content entry.
func TestJSONResultMarshalFailureSurfacesAsErrorEnvelope(t *testing.T) {
	// chan int is the canonical "unsupported type" trigger for the
	// encoding/json package (json.UnsupportedTypeError).
	res := jsonResult(map[string]any{"bad": make(chan int)})
	if res == nil {
		t.Fatal("jsonResult returned nil")
	}
	if !res.IsError {
		t.Error("res.IsError=false; want true on marshal failure")
	}
	if len(res.Content) != 1 {
		t.Fatalf("len(Content)=%d; want 1", len(res.Content))
	}
	tc, ok := res.Content[0].(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("content type=%T; want *TextContent", res.Content[0])
	}
	if !strings.HasPrefix(tc.Text, "JSON_ENCODE_FAILED:") {
		t.Errorf("text=%q; want JSON_ENCODE_FAILED prefix", tc.Text)
	}
}

// TestJSONErrorResultMarshalFailureSurfacesAsErrorEnvelope pins the
// matching marshal-err arm of jsonErrorResult. Same risk profile as
// jsonResult but the helper is used specifically when the caller
// already knows the result represents an error — keeping the
// "JSON_ENCODE_FAILED:" prefix consistent matters because operator
// dashboards grep for that label across both helpers.
func TestJSONErrorResultMarshalFailureSurfacesAsErrorEnvelope(t *testing.T) {
	res := jsonErrorResult(map[string]any{"bad": make(chan int)})
	if res == nil {
		t.Fatal("jsonErrorResult returned nil")
	}
	if !res.IsError {
		t.Error("res.IsError=false; want true on marshal failure")
	}
	if len(res.Content) != 1 {
		t.Fatalf("len(Content)=%d; want 1", len(res.Content))
	}
	tc, ok := res.Content[0].(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("content type=%T; want *TextContent", res.Content[0])
	}
	if !strings.HasPrefix(tc.Text, "JSON_ENCODE_FAILED:") {
		t.Errorf("text=%q; want JSON_ENCODE_FAILED prefix", tc.Text)
	}
}
