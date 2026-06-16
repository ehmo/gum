package dispatch

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

// TestShapeResponseRawPassThrough verifies format="raw" returns the body bytes
// untransformed — spec §3.1 step 8: raw bypass is identity.
func TestShapeResponseRawPassThrough(t *testing.T) {
	d := &dispatcher{}
	body := []byte("arbitrary\x00bytes-not-json")
	inv := &Invocation{OpID: "x", Format: "raw"}
	rv := &ResolvedVariant{Variant: &catalog.Variant{}}
	resp := &Response{Body: body}

	out, err := d.shapeResponse(t.Context(), inv, rv, resp)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out.Format != "raw" {
		t.Errorf("Format=%q want raw", out.Format)
	}
	if string(out.Body) != string(body) {
		t.Errorf("Body mutated; raw must be identity")
	}
}

// TestShapeResponseTOONFormat verifies format="toon" produces TOON-encoded output.
// Acceptance: format='toon' applies toon profile.
func TestShapeResponseTOONFormat(t *testing.T) {
	d := &dispatcher{}
	body := []byte(`[{"a":1,"b":2}]`)
	inv := &Invocation{OpID: "x", Format: "toon"}
	rv := &ResolvedVariant{Variant: &catalog.Variant{}}
	resp := &Response{Body: body}

	out, err := d.shapeResponse(t.Context(), inv, rv, resp)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out.Format != "toon" {
		t.Errorf("Format=%q want toon", out.Format)
	}
	if len(out.Body) == 0 {
		t.Fatal("empty TOON body")
	}
	// TOON output for [{"a":1,"b":2}] should NOT start with `[` (that's JSON)
	if out.Body[0] == '[' {
		t.Errorf("body looks like JSON, not TOON: %q", out.Body)
	}
}

// TestShapeResponseJSONFormat verifies format="json" passes through valid JSON
// (or re-encodes deterministically without TOON transformation).
// Acceptance: format='json' applies field-mask profile (no fields here → identity).
func TestShapeResponseJSONFormat(t *testing.T) {
	d := &dispatcher{}
	body := []byte(`{"a":1,"b":2}`)
	inv := &Invocation{OpID: "x", Format: "json"}
	rv := &ResolvedVariant{Variant: &catalog.Variant{}}
	resp := &Response{Body: body}

	out, err := d.shapeResponse(t.Context(), inv, rv, resp)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out.Format != "json" {
		t.Errorf("Format=%q want json", out.Format)
	}
	// Should be valid JSON
	var got map[string]any
	if err := json.Unmarshal(out.Body, &got); err != nil {
		t.Fatalf("output not valid JSON: %v body=%q", err, out.Body)
	}
	if got["a"].(float64) != 1 || got["b"].(float64) != 2 {
		t.Errorf("payload corrupted: %v", got)
	}
}

// TestShapeResponseInvalidFormatStructured verifies an unknown format value
// is rejected before encoding with a structured INVALID_ARGS error carrying
// the offending field in Detail.
// Acceptance: unknown format returns INVALID_FORMAT (we use INVALID_ARGS per
// spec §1421; the canonical stable code, with `field=format` in detail).
func TestShapeResponseInvalidFormatStructured(t *testing.T) {
	d := &dispatcher{}
	inv := &Invocation{OpID: "x", Format: "xml"}
	rv := &ResolvedVariant{Variant: &catalog.Variant{}}
	resp := &Response{Body: []byte(`{}`)}

	_, err := d.shapeResponse(t.Context(), inv, rv, resp)
	if err == nil {
		t.Fatal("want error for unknown format, got nil")
	}
	if !IsStructuredError(err, ErrCodeInvalidArgs) {
		t.Fatalf("err code: got %v, want INVALID_ARGS", err)
	}
	var se *StructuredError
	if !errors.As(err, &se) {
		t.Fatal("err is not *StructuredError")
	}
	if got := se.Detail["field"]; got != "format" {
		t.Errorf("Detail[field]=%v, want format", got)
	}
	if got := se.Detail["value"]; got != "xml" {
		t.Errorf("Detail[value]=%v, want xml", got)
	}
}

// TestShapeResponseDefaultFormatIsTOON verifies that when Invocation.Format is
// empty the kernel falls back to TOON (token-efficient default per spec §3.1
// step 8 / §9).
func TestShapeResponseDefaultFormatIsTOON(t *testing.T) {
	d := &dispatcher{}
	body := []byte(`[{"a":1}]`)
	inv := &Invocation{OpID: "x", Format: ""}
	rv := &ResolvedVariant{Variant: &catalog.Variant{}}
	resp := &Response{Body: body}

	out, err := d.shapeResponse(t.Context(), inv, rv, resp)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out.Format != "toon" {
		t.Errorf("default Format=%q want toon", out.Format)
	}
}

// TestShapeResponseInvalidJSONNonRawErrors verifies a non-JSON body with a
// non-raw format produces an error (the profile applier cannot parse it). The
// raw bypass is the only path that tolerates arbitrary bytes.
func TestShapeResponseInvalidJSONNonRawErrors(t *testing.T) {
	d := &dispatcher{}
	inv := &Invocation{OpID: "x", Format: "toon"}
	rv := &ResolvedVariant{Variant: &catalog.Variant{}}
	resp := &Response{Body: []byte("not json at all")}

	_, err := d.shapeResponse(t.Context(), inv, rv, resp)
	if err == nil {
		t.Fatal("want error for invalid JSON in non-raw format, got nil")
	}
}
