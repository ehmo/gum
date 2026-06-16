package adapters

import (
	"regexp"
	"testing"
)

// TestCanonicalJSONShapes pins the round-trip-as-key contract that
// shared_expression_fields hoisting depends on — two equivalent map
// values must canonicalize to the same string so they collapse into a
// single bucket. Non-marshalable types (e.g. chan) surface the marshal
// error untouched.
func TestCanonicalJSONShapes(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string
	}{
		{"nil", nil, "null"},
		{"scalar_string", "x", `"x"`},
		{"scalar_int", 42, "42"},
		{"empty_map", map[string]any{}, "{}"},
		{"single_key_map", map[string]any{"a": 1}, `{"a":1}`},
		{"empty_slice", []any{}, "[]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := canonicalJSON(tc.in)
			if err != nil {
				t.Fatalf("canonicalJSON: %v", err)
			}
			if got != tc.want {
				t.Errorf("got=%q; want %q", got, tc.want)
			}
		})
	}

	t.Run("marshal_error_surfaces", func(t *testing.T) {
		// json.Marshal returns UnsupportedTypeError for chan.
		_, err := canonicalJSON(make(chan int))
		if err == nil {
			t.Fatalf("expected marshal error for chan; got nil")
		}
	})
}

// TestNewBatchIDShape pins the §9.0.1 batch_id contract: an 8-char
// lowercase hex string. The parallel-envelope outer entry surfaces
// this verbatim, so a length or charset regression breaks downstream
// consumers parsing the envelope.
func TestNewBatchIDShape(t *testing.T) {
	pattern := regexp.MustCompile(`^[0-9a-f]{8}$`)
	for i := 0; i < 10; i++ {
		id := newBatchID()
		if !pattern.MatchString(id) {
			t.Errorf("id=%q does not match ^[0-9a-f]{8}$", id)
		}
	}
}

// TestStringFieldBranches pins the three observable outcomes — missing
// key returns ""; wrong-type returns ""; present-and-string returns
// the value. The parallel runner uses this as a defensive coercion
// over Risor-produced maps where types are not statically guaranteed.
func TestStringFieldBranches(t *testing.T) {
	m := map[string]any{
		"name":  "alice",
		"count": 42,
		"nilv":  nil,
	}
	if got := stringField(m, "name"); got != "alice" {
		t.Errorf("name=%q; want alice", got)
	}
	if got := stringField(m, "missing"); got != "" {
		t.Errorf("missing=%q; want empty", got)
	}
	if got := stringField(m, "count"); got != "" {
		t.Errorf("count=%q; want empty (wrong type)", got)
	}
	if got := stringField(m, "nilv"); got != "" {
		t.Errorf("nilv=%q; want empty (nil)", got)
	}
}

// TestToAnySliceShapes pins the empty-input and populated-input
// branches. Risor cannot iterate []map[string]any directly, so this
// helper is the bridge — a regression that left a nil result would
// turn a populated parallel envelope into an empty Risor iteration.
func TestToAnySliceShapes(t *testing.T) {
	if got := toAnySlice(nil); got == nil || len(got) != 0 {
		t.Errorf("nil input: got=%v; want empty non-nil slice", got)
	}
	in := []map[string]any{{"a": 1}, {"b": 2}}
	got := toAnySlice(in)
	if len(got) != 2 {
		t.Fatalf("len=%d; want 2", len(got))
	}
	if first, ok := got[0].(map[string]any); !ok || first["a"] != 1 {
		t.Errorf("got[0]=%v; want {a:1}", got[0])
	}
}
