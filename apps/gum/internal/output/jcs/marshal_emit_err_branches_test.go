package jcs_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/output/jcs"
)

// TestMarshalEmitCanonicalErrSurfacesAsInvalidNumber pins the
// `emitCanonical err → return nil, err` arm of Marshal (jcs.go:51-53).
// json.Number("1e9999") is syntactically a valid JSON number so it
// passes both validateValue (Kind=String) AND normalizeToTree's
// json.Marshal/Decode round-trip — but canonicalNumber's ParseFloat
// overflows to +Inf with ErrRange, surfacing ErrJCSInvalidNumber.
//
// Coverage side-effect: also lifts canonicalNumber's
// `ParseFloat err != nil` branch (jcs.go:213-215) and emitCanonical's
// json.Number canonicalNumber-err branch (jcs.go:150-152). One input,
// three arms — picked over building an unexported-fn unit test because
// it exercises the full Marshal call chain end-to-end.
func TestMarshalEmitCanonicalErrSurfacesAsInvalidNumber(t *testing.T) {
	got, err := jcs.Marshal(json.Number("1e9999"))
	if err == nil {
		t.Fatalf("Marshal(json.Number(\"1e9999\"))=%q nil err; want ErrJCSInvalidNumber", got)
	}
	if got != nil {
		t.Errorf("got=%q; want nil on err", got)
	}
	if !errors.Is(err, jcs.ErrJCSInvalidNumber) {
		t.Errorf("err=%v; want errors.Is(err, ErrJCSInvalidNumber)", err)
	}
	if !strings.Contains(err.Error(), "1e9999") {
		t.Errorf("err=%q; want input number string in message for triage", err)
	}
}

// TestMarshalSliceElementErrPropagates pins emitCanonical's
// `[]any elem err → return err` arm (jcs.go:162-164). A slice
// containing a bogus json.Number survives validateValue +
// normalizeToTree, then fails inside the recursive emitCanonical
// call on the element. Without propagation the slice would emit a
// truncated "[" with no closing bracket and no err to the caller.
func TestMarshalSliceElementErrPropagates(t *testing.T) {
	_, err := jcs.Marshal([]any{json.Number("1e9999")})
	if err == nil {
		t.Fatal("Marshal([]any{json.Number(\"1e9999\")})=nil err; want recursive ErrJCSInvalidNumber")
	}
	if !errors.Is(err, jcs.ErrJCSInvalidNumber) {
		t.Errorf("err=%v; want errors.Is(err, ErrJCSInvalidNumber) propagated from slice elem", err)
	}
}

// TestMarshalMapValueErrPropagates pins emitCanonical's
// `map[string]any value err → return err` arm (jcs.go:180-182). A
// map whose value is a bogus json.Number fails recursively after the
// key has already been emitted; the err must surface to Marshal
// rather than producing partial output.
func TestMarshalMapValueErrPropagates(t *testing.T) {
	_, err := jcs.Marshal(map[string]any{"bad": json.Number("1e9999")})
	if err == nil {
		t.Fatal("Marshal(map[string]any{\"bad\":json.Number(\"1e9999\")})=nil err; want recursive ErrJCSInvalidNumber")
	}
	if !errors.Is(err, jcs.ErrJCSInvalidNumber) {
		t.Errorf("err=%v; want errors.Is(err, ErrJCSInvalidNumber) propagated from map value", err)
	}
}

// TestMarshalUnsupportedMapKeyTypeSurfacesError pins the
// `validateValue map key err → return err` arm (jcs.go:87-89). A map
// with a non-comparable-or-unsupported key kind triggers the per-key
// validateValue walk to reject before any encoding attempt. Using a
// chan-keyed map: keys are technically comparable (channels compare
// by identity) so the map is constructable, but Kind=Chan trips
// ErrJCSUnsupportedType in the key validation pass.
//
// Distinct from value-validation: value errs are pinned by existing
// tests; key errs require a non-string key type, which is exotic
// enough that the existing suite missed this branch.
func TestMarshalUnsupportedMapKeyTypeSurfacesError(t *testing.T) {
	ch := make(chan int)
	m := map[chan int]string{ch: "value"}

	got, err := jcs.Marshal(m)
	if err == nil {
		t.Fatalf("Marshal(map[chan int]string)=%q nil err; want ErrJCSUnsupportedType", got)
	}
	if got != nil {
		t.Errorf("got=%q; want nil on err", got)
	}
	if !errors.Is(err, jcs.ErrJCSUnsupportedType) {
		t.Errorf("err=%v; want errors.Is(err, ErrJCSUnsupportedType)", err)
	}
}
