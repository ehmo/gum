package dispatch

import (
	"context"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

// TestShapedResponseStructuredContentJSON verifies the shaped response carries
// the JSON-parsed payload in StructuredContent for json/toon formats so the MCP
// handler can hand a schema-conformant tree to clients.
// Spec §3.1 step 9 envelope: structuredContent + text content block.
func TestShapedResponseStructuredContentJSON(t *testing.T) {
	d := &dispatcher{}
	body := []byte(`{"messages":[{"id":"a"},{"id":"b"}]}`)
	inv := &Invocation{OpID: "x", Format: "json"}
	rv := &ResolvedVariant{Variant: &catalog.Variant{}}

	out, err := d.shapeResponse(context.Background(), inv, rv, &Response{Body: body})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out.StructuredContent == nil {
		t.Fatal("StructuredContent nil; want JSON tree for json format")
	}
	m, ok := out.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("StructuredContent=%T; want map[string]any", out.StructuredContent)
	}
	if _, ok := m["messages"]; !ok {
		t.Errorf("StructuredContent missing 'messages' key: %v", m)
	}
}

// TestShapedResponseStructuredContentTOON verifies the structuredContent is
// populated even when the wire format is TOON — clients that consume the
// outputSchema get the JSON shape regardless of the text encoding.
func TestShapedResponseStructuredContentTOON(t *testing.T) {
	d := &dispatcher{}
	body := []byte(`[{"id":1}]`)
	inv := &Invocation{OpID: "x", Format: "toon"}
	rv := &ResolvedVariant{Variant: &catalog.Variant{}}

	out, err := d.shapeResponse(context.Background(), inv, rv, &Response{Body: body})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out.Format != "toon" {
		t.Errorf("Format=%q want toon", out.Format)
	}
	if out.StructuredContent == nil {
		t.Fatal("StructuredContent nil; want JSON tree even when format=toon")
	}
	arr, ok := out.StructuredContent.([]any)
	if !ok || len(arr) != 1 {
		t.Fatalf("StructuredContent=%v; want 1-element array", out.StructuredContent)
	}
}

// TestShapedResponseStructuredContentNilForRaw verifies raw format does NOT
// populate StructuredContent (executor body is opaque bytes, not parsable).
func TestShapedResponseStructuredContentNilForRaw(t *testing.T) {
	d := &dispatcher{}
	inv := &Invocation{OpID: "x", Format: "raw"}
	rv := &ResolvedVariant{Variant: &catalog.Variant{}}
	out, err := d.shapeResponse(context.Background(), inv, rv, &Response{Body: []byte("opaque bytes")})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out.StructuredContent != nil {
		t.Errorf("raw bypass populated StructuredContent=%v; want nil", out.StructuredContent)
	}
}
