package toon_test

import (
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/output/toon"
)

// TestEncodeMapNestedMapMarshalErrorPropagates pins encoder.go:217-219.
// When a map value is itself a map[string]any containing a Go type that
// json.Marshal can't encode (here: a chan int), encodeMap's nested-map
// arm MUST surface that err — silently dropping it would emit malformed
// TOON downstream.
func TestEncodeMapNestedMapMarshalErrorPropagates(t *testing.T) {
	t.Parallel()
	in := map[string]any{
		"outer": map[string]any{"inner": make(chan int)},
	}
	_, err := toon.Encode(in)
	if err == nil {
		t.Fatal("Encode(map containing chan-bearing map)=nil err; want json.Marshal err")
	}
	if !strings.Contains(err.Error(), "unsupported type") {
		t.Errorf("err=%q; want json 'unsupported type' surface", err)
	}
}

// TestEncodeMapNestedArrayMarshalErrorPropagates pins encoder.go:223-225.
// Same shape as above but with the value being []any. The arm covering
// nested arrays is distinct from the nested-map arm and needs its own
// propagation test.
func TestEncodeMapNestedArrayMarshalErrorPropagates(t *testing.T) {
	t.Parallel()
	in := map[string]any{
		"arr": []any{make(chan int)},
	}
	_, err := toon.Encode(in)
	if err == nil {
		t.Fatal("Encode(map with chan-bearing []any)=nil err; want json.Marshal err")
	}
	if !strings.Contains(err.Error(), "unsupported type") {
		t.Errorf("err=%q; want json 'unsupported type' surface", err)
	}
}

// TestEncodeMapScalarMarshalErrorPropagates pins encoder.go:229-231 (and
// encodeScalar's default json.Marshal fallback at 106-108). When a map
// value is a Go scalar type that lands in encodeScalar's default arm and
// json.Marshal can't encode it, encodeMap MUST surface that err.
func TestEncodeMapScalarMarshalErrorPropagates(t *testing.T) {
	t.Parallel()
	in := map[string]any{
		"ch": make(chan int),
	}
	_, err := toon.Encode(in)
	if err == nil {
		t.Fatal("Encode(map with chan scalar)=nil err; want encodeScalar fallback err")
	}
	if !strings.Contains(err.Error(), "unsupported type") {
		t.Errorf("err=%q; want json 'unsupported type' surface", err)
	}
}

// TestEncodeMapNilValueEmitsNull pins encoder.go:71-73 — encodeScalar's
// `v == nil → return "null", nil` arm. Reached through encodeMap's
// default case when a map contains a literal nil value alongside a
// non-empty value (the non-empty value prevents allSentinelEmpty from
// short-circuiting into the {} sentinel).
func TestEncodeMapNilValueEmitsNull(t *testing.T) {
	t.Parallel()
	in := map[string]any{
		"a": "keepalive", // prevents allSentinelEmpty short-circuit
		"b": nil,         // forces encodeScalar(nil) via default case
	}
	out, err := toon.Encode(in)
	if err != nil {
		t.Fatalf("Encode mixed-nil map: %v", err)
	}
	if !strings.Contains(string(out), "b=null") {
		t.Errorf("out=%q; want 'b=null' line", out)
	}
}
