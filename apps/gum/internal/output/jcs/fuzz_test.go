package jcs

import (
	"bytes"
	"encoding/json"
	"testing"
)

// FuzzJCSCanonical feeds the JCS canonicalizer arbitrary JSON-shaped inputs by
// first json.Unmarshaling to any (skipping invalid JSON) then calling Marshal.
// A "pass" is any panic-free return — errors are acceptable. Required by spec
// §15 release-pipeline step 2.
func FuzzJCSCanonical(f *testing.F) {
	seeds := [][]byte{
		[]byte(`{}`),
		[]byte(`[]`),
		[]byte(`null`),
		[]byte(`0`),
		[]byte(`""`),
		[]byte(`{"a":1,"b":2}`),
		[]byte(`{"b":2,"a":1}`), // out-of-order keys
		[]byte(`[1,2,3]`),
		[]byte(`{"nested":{"k":"v"}}`),
		// Unicode + escapes
		[]byte(`"é"`),
		[]byte(`"A"`),
		// Numbers — JCS has specific normalization rules
		[]byte(`1.0`),
		[]byte(`-0`),
		[]byte(`1e10`),
		[]byte(`0.1`),
		// Deep nesting
		[]byte(`{"a":{"b":{"c":[1,2,{"d":null}]}}}`),
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var v any
		if err := json.Unmarshal(data, &v); err != nil {
			return // Not valid JSON — nothing to canonicalize.
		}
		_, _ = Marshal(v)
	})
}

// FuzzJCSIdempotent strengthens FuzzJCSCanonical: it asserts canonicalization is
// IDEMPOTENT — re-canonicalizing a canonical form reproduces it byte-for-byte.
// This is the security property the args_hash / confirmation-token binding and
// the semantic cache keys depend on: two semantically-equal arg sets MUST
// produce identical canonical bytes, or a token bound to args A could verify for
// a different args B (and the cache could mis-key).
func FuzzJCSIdempotent(f *testing.F) {
	for _, seed := range []string{
		`{}`, `{"b":2,"a":1}`, `[1,2,3]`, `"x"`, `123`, `1.5`, `null`, `true`,
		`{"k":{"nested":[1,{"z":0}]}}`, `1e20`, `{"":""}`, `[]`, `-0`,
		`12345678901234567890`, `{"unicode":"日本語"}`, `" "`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var v any
		if err := json.Unmarshal(data, &v); err != nil {
			return
		}
		out1, err := Marshal(v)
		if err != nil {
			return // errors are acceptable; the contract is no panic + idempotence
		}
		var v2 any
		if err := json.Unmarshal(out1, &v2); err != nil {
			t.Fatalf("JCS output is not valid JSON: %q (%v)", out1, err)
		}
		out2, err := Marshal(v2)
		if err != nil {
			t.Fatalf("re-marshal of canonical form failed: %v (input %q)", err, out1)
		}
		if !bytes.Equal(out1, out2) {
			t.Fatalf("JCS not idempotent:\n  first:  %q\n  second: %q", out1, out2)
		}
	})
}
