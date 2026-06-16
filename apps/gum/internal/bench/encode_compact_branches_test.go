package bench

import (
	"bytes"
	"testing"
)

// TestEncodeCompactInvalidJSONPassesThrough pins the
// `json.Unmarshal err → return jsonBody` arm: when a shaper hands
// encodeCompact a non-JSON payload (e.g. a pre-formatted TOON snippet),
// encodeCompact MUST return the bytes unchanged rather than corrupting
// the output by re-encoding empty {}.
func TestEncodeCompactInvalidJSONPassesThrough(t *testing.T) {
	in := []byte("not valid json @ all")
	out := encodeCompact(in)
	if !bytes.Equal(out, in) {
		t.Errorf("encodeCompact(invalid) = %q; want pass-through %q", out, in)
	}
}

// TestEncodeCompactSingleKeyArrayOfScalarsKeepsElements pins the
// `e not a map → flat[i] = e` arm of the inner loop: a single-key map
// wrapping an array of SCALARS (not objects) MUST keep the scalars
// intact (no flattenScalars), still go through toon.Encode, and produce
// a non-empty output.
func TestEncodeCompactSingleKeyArrayOfScalarsKeepsElements(t *testing.T) {
	in := []byte(`{"ids":[1,2,3]}`)
	out := encodeCompact(in)
	if len(out) == 0 {
		t.Error("encodeCompact returned empty bytes; want toon-encoded payload")
	}
	// Sanity: contains the scalar values somewhere.
	if !bytes.Contains(out, []byte("1")) || !bytes.Contains(out, []byte("3")) {
		t.Errorf("encodeCompact(%q) = %q; want output containing scalar ids", in, out)
	}
}
