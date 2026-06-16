package adapters

import (
	"reflect"
	"testing"

	"github.com/ehmo/gum/internal/dispatch"
)

// TestSuccessItemNilShaped pins the early-out: when shaped is nil the
// envelope must still carry _idx and _expression.op_id so downstream
// per-element error rendering has a stable key.
func TestSuccessItemNilShaped(t *testing.T) {
	got := successItem(3, "gum.list_messages", nil)
	if got["_idx"] != 3 {
		t.Errorf("_idx=%v; want 3", got["_idx"])
	}
	expr, ok := got["_expression"].(map[string]any)
	if !ok || expr["op_id"] != "gum.list_messages" {
		t.Errorf("_expression=%v; want {op_id: gum.list_messages}", got["_expression"])
	}
	if _, hasFormat := got["format"]; hasFormat {
		t.Errorf("nil shaped: format key leaked")
	}
}

// TestSuccessItemStructuredContentWins drives the StructuredContent
// branch: when set, the envelope uses it directly as `data` and never
// touches Body.
func TestSuccessItemStructuredContentWins(t *testing.T) {
	shaped := &dispatch.ShapedResponse{
		Format:            "json",
		StructuredContent: map[string]any{"k": "v"},
		Body:              []byte(`"ignored"`),
	}
	got := successItem(0, "op", shaped)
	if got["format"] != "json" {
		t.Errorf("format=%v; want json", got["format"])
	}
	want := map[string]any{"k": "v"}
	if !reflect.DeepEqual(got["data"], want) {
		t.Errorf("data=%v; want %v", got["data"], want)
	}
}

// TestSuccessItemTOONBodyVerbatim drives the format=="toon" Body
// branch: TOON text is opaque so we never parse it; the raw bytes are
// surfaced as `toon`.
func TestSuccessItemTOONBodyVerbatim(t *testing.T) {
	shaped := &dispatch.ShapedResponse{
		Format: "toon",
		Body:   []byte("op: x\ncount: 0\n"),
	}
	got := successItem(0, "op", shaped)
	if got["toon"] != "op: x\ncount: 0\n" {
		t.Errorf("toon=%q", got["toon"])
	}
	if _, hasData := got["data"]; hasData {
		t.Errorf("toon path leaked data field")
	}
}

// TestSuccessItemJSONBodyParsedIntoData drives the json-Body branch
// when StructuredContent is empty: the body is Unmarshalled into `data`.
func TestSuccessItemJSONBodyParsedIntoData(t *testing.T) {
	shaped := &dispatch.ShapedResponse{
		Format: "json",
		Body:   []byte(`{"answer":42}`),
	}
	got := successItem(0, "op", shaped)
	want := map[string]any{"answer": float64(42)}
	if !reflect.DeepEqual(got["data"], want) {
		t.Errorf("data=%v; want %v", got["data"], want)
	}
}

// TestSuccessItemBadJSONBodyFallsBackToString drives the json.Unmarshal
// error branch: when Body is not valid JSON the raw string is preserved
// so the caller still gets *something* instead of a silent drop.
func TestSuccessItemBadJSONBodyFallsBackToString(t *testing.T) {
	shaped := &dispatch.ShapedResponse{
		Format: "json",
		Body:   []byte("not-json"),
	}
	got := successItem(0, "op", shaped)
	if got["data"] != "not-json" {
		t.Errorf("data=%v; want raw string", got["data"])
	}
}
