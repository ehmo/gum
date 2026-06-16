package jcs

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
)

// TestEmitCanonicalRejectsUnexpectedType pins emitCanonical's
// `default → ErrJCSUnsupportedType` arm (jcs.go:185-186). The
// canonical tree contract is closed: nil/bool/json.Number/string/
// []any/map[string]any. Marshal's validateValue pre-pass plus the
// json.Marshal/Decode round-trip means the public path never produces
// anything else — but callers that wire up emitCanonical via a future
// streaming API must still get a typed error rather than silently
// emitting nothing.
func TestEmitCanonicalRejectsUnexpectedType(t *testing.T) {
	var buf bytes.Buffer
	// int isn't in the canonical-tree type set; this is exactly the kind
	// of mistake a future refactor could introduce.
	err := emitCanonical(&buf, 42)
	if err == nil {
		t.Fatal("emitCanonical(int)=nil; want ErrJCSUnsupportedType")
	}
	if !errors.Is(err, ErrJCSUnsupportedType) {
		t.Errorf("err=%v; want errors.Is(err, ErrJCSUnsupportedType)", err)
	}
}

// TestCanonicalNumberRejectsNaNAndInf pins canonicalNumber's
// `math.IsNaN || math.IsInf → ErrJCSInvalidNumber` arm
// (jcs.go:216-218). Marshal's validateValue catches NaN/Inf via
// reflection before they ever reach canonicalNumber, but the JSON
// grammar technically permits Infinity/NaN tokens as json.Number
// strings via UseNumber decoding of a hand-crafted input — so the
// defense-in-depth check here MUST stay live.
func TestCanonicalNumberRejectsNaNAndInf(t *testing.T) {
	for _, tok := range []string{"NaN", "Inf", "+Inf", "-Inf", "Infinity"} {
		if _, err := canonicalNumber(json.Number(tok)); err == nil {
			t.Errorf("canonicalNumber(%q)=nil err; want ErrJCSInvalidNumber", tok)
		} else if !errors.Is(err, ErrJCSInvalidNumber) {
			t.Errorf("canonicalNumber(%q) err=%v; want errors.Is(err, ErrJCSInvalidNumber)", tok, err)
		}
	}
}
