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

// TestES6FromShortestFallsBackOnMalformedInput pins es6FromShortest's two
// defensive arms. Neither is reachable through es6Number: NaN and Inf are
// rejected by canonicalNumber before the call, and every remaining float64
// makes strconv.FormatFloat(f, 'e', -1, 64) return "d.dddde±XX" with a decimal
// exponent. The arms exist so a toolchain that changed that format would
// degrade to Go's own shortest form rather than index past the end of a string
// or silently emit the wrong digits. This test states what that degradation is.
func TestES6FromShortestFallsBackOnMalformedInput(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		// No 'e' at all: strings.Cut does not split, so there is no exponent
		// to read and no way to place the decimal point.
		{"no exponent", "1.5", "1.5"},
		{"plain digits", "42", "42"},
		{"empty", "", ""},
		// An 'e' whose exponent is not a base-10 integer. strconv.Atoi fails
		// and n is unknown, so the rules cannot be applied.
		{"non-numeric exponent", "1.5eX", "1.5eX"},
		{"empty exponent", "1.5e", "1.5e"},
		{"exponent overflows int", "1.5e999999999999999999999", "1.5e999999999999999999999"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := es6FromShortest(tc.input); got != tc.want {
				t.Errorf("es6FromShortest(%q) = %q; want %q", tc.input, got, tc.want)
			}
		})
	}
}

// TestCanonicalNumberZeroTokens pins RFC 8785 §3.2.2.3's rule that negative
// zero serializes as "0". Two different arms implement it: the integer fast
// path formats the tokens "0" and "-0" through ParseInt, which folds the sign,
// and es6Number's own f == 0 arm catches every token that carries a decimal
// point or an exponent. json.Marshal of a Go float64 always emits the bare
// token, so only a hand-decoded json.Number reaches the second arm.
func TestCanonicalNumberZeroTokens(t *testing.T) {
	for _, tok := range []string{"0", "-0", "0.0", "-0.0", "0e0", "-0e10", "0.000e-5"} {
		got, err := canonicalNumber(json.Number(tok))
		if err != nil {
			t.Errorf("canonicalNumber(%q) error: %v", tok, err)
			continue
		}
		if got != "0" {
			t.Errorf("canonicalNumber(%q) = %q; want \"0\"", tok, got)
		}
	}
}
