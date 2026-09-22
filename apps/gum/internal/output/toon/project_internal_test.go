package toon

import (
	"encoding/json"
	"reflect"
	"testing"
)

// TestRecordArrayKey pins the precedence rule: a named key wins over a
// lone unnamed array, two unnamed arrays pick neither, and an object with
// no array-valued field has no record array.
func TestRecordArrayKey(t *testing.T) {
	cases := []struct {
		name string
		in   map[string]any
		want string
	}{
		{"named wins over unnamed", map[string]any{"results": []any{}, "unmatchedInputs": []any{}}, "results"},
		{"precedence order", map[string]any{"data": []any{}, "messages": []any{}}, "data"},
		{"lone unnamed array", map[string]any{"files": []any{}, "kind": "drive#fileList"}, "files"},
		{"two unnamed arrays", map[string]any{"left": []any{}, "right": []any{}}, ""},
		{"no array", map[string]any{"id": "x"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := RecordArrayKey(tc.in); got != tc.want {
				t.Errorf("RecordArrayKey = %q; want %q", got, tc.want)
			}
		})
	}
}

// TestDocumentFromRepresentable covers every shape §9.0 can carry.
func TestDocumentFromRepresentable(t *testing.T) {
	t.Run("array of records", func(t *testing.T) {
		doc, ok := DocumentFrom("op.list", "v1", []any{
			map[string]any{"a": 1},
			map[string]any{"b": 2},
		}, false)
		if !ok {
			t.Fatal("ok=false; want true")
		}
		if doc.Op != "op.list" || doc.Variant != "v1" || doc.FormatVersion != 1 {
			t.Errorf("headers = %q/%q/%d", doc.Op, doc.Variant, doc.FormatVersion)
		}
		if !reflect.DeepEqual(doc.Fields, []string{"a", "b"}) {
			t.Errorf("fields = %v; want the sorted union [a b]", doc.Fields)
		}
		if doc.Count != 2 {
			t.Errorf("count = %d; want 2", doc.Count)
		}
		want := [][]any{{1, nil}, {nil, 2}}
		if !reflect.DeepEqual(doc.Rows, want) {
			t.Errorf("rows = %v; want %v (a missing field is an empty cell)", doc.Rows, want)
		}
	})

	t.Run("empty object", func(t *testing.T) {
		doc, ok := DocumentFrom("op.get", "", map[string]any{}, false)
		if !ok {
			t.Fatal("ok=false; want true")
		}
		if doc.Count != 0 || len(doc.Rows) != 0 {
			t.Errorf("count = %d, rows = %v; want the count-0 sentinel", doc.Count, doc.Rows)
		}
	})

	t.Run("single resource is one row", func(t *testing.T) {
		doc, ok := DocumentFrom("op.get", "", map[string]any{"id": "x", "name": "y"}, false)
		if !ok {
			t.Fatal("ok=false; want true")
		}
		if !reflect.DeepEqual(doc.Fields, []string{"id", "name"}) {
			t.Errorf("fields = %v", doc.Fields)
		}
		if doc.Count != 1 || !reflect.DeepEqual(doc.Rows, [][]any{{"x", "y"}}) {
			t.Errorf("count = %d, rows = %v", doc.Count, doc.Rows)
		}
	})

	t.Run("single resource drops zero counts", func(t *testing.T) {
		doc, ok := DocumentFrom("op.get", "", map[string]any{"id": "x", "files_omitted_count": 0}, true)
		if !ok {
			t.Fatal("ok=false; want true")
		}
		if !reflect.DeepEqual(doc.Fields, []string{"id"}) {
			t.Errorf("fields = %v; want the zero count dropped", doc.Fields)
		}
	})

	t.Run("siblings ride the header sorted", func(t *testing.T) {
		doc, ok := DocumentFrom("op.list", "", map[string]any{
			"messages":           []any{map[string]any{"id": "a"}},
			"resultSizeEstimate": 1,
			"kind":               "gmail#listMessagesResponse",
		}, false)
		if !ok {
			t.Fatal("ok=false; want true")
		}
		if doc.RecordKey != "messages" {
			t.Errorf("records = %q; want messages", doc.RecordKey)
		}
		got := []string{doc.Extra[0].Key, doc.Extra[1].Key}
		if !reflect.DeepEqual(got, []string{"kind", "resultSizeEstimate"}) {
			t.Errorf("extra header order = %v; want sorted", got)
		}
	})

	t.Run("sibling zero count is dropped", func(t *testing.T) {
		doc, ok := DocumentFrom("op.list", "", map[string]any{
			"items":               []any{map[string]any{"id": "a"}},
			"items_omitted_count": 0,
		}, true)
		if !ok {
			t.Fatal("ok=false; want true")
		}
		if len(doc.Extra) != 0 {
			t.Errorf("extra = %v; want the zero count dropped", doc.Extra)
		}
	})

	t.Run("next page token aliases", func(t *testing.T) {
		for _, key := range []string{"next_page_token", "nextPageToken"} {
			doc, ok := DocumentFrom("op.list", "", map[string]any{
				"items": []any{map[string]any{"id": "a"}},
				key:     "tok",
			}, false)
			if !ok {
				t.Fatalf("%s: ok=false; want true", key)
			}
			if doc.NextPageToken != "tok" {
				t.Errorf("%s: next_page_token = %q; want tok", key, doc.NextPageToken)
			}
			if len(doc.Extra) != 0 {
				t.Errorf("%s: the token must not also ride an extra header: %v", key, doc.Extra)
			}
		}
	})
}

// TestDocumentFromFallsBack covers every shape that sends the response to
// the JSON fallback.
func TestDocumentFromFallsBack(t *testing.T) {
	cases := []struct {
		name string
		in   any
	}{
		{"bare scalar", 42},
		{"nested single resource", map[string]any{"id": "x", "capabilities": map[string]any{"canEdit": true}}},
		{"unencodable field name in single resource", map[string]any{"a,b": 1}},
		{"record is not an object", []any{1, 2}},
		{"unencodable field name in a record", []any{map[string]any{"a\nb": 1}}},
		{"sibling is an object", map[string]any{"items": []any{map[string]any{"id": "a"}}, "meta": map[string]any{"x": 1}}},
		{"sibling collides with a reserved header", map[string]any{"items": []any{map[string]any{"id": "a"}}, "count": 5}},
		{"next page token is not a string", map[string]any{"items": []any{map[string]any{"id": "a"}}, "nextPageToken": 7}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := DocumentFrom("op", "", tc.in, false); ok {
				t.Error("ok=true; want the JSON fallback")
			}
		})
	}
}

// TestEncodeDocument pins both arms of the encoder entry point: bytes for
// a representable value, (nil, nil) for one the caller must send as JSON.
func TestEncodeDocument(t *testing.T) {
	out, err := EncodeDocument("op.list", "", []any{map[string]any{"id": "a"}}, false)
	if err != nil {
		t.Fatalf("EncodeDocument: %v", err)
	}
	doc, err := DecodeTOONDocument(out)
	if err != nil {
		t.Fatalf("EncodeDocument output does not decode: %v\n%s", err, out)
	}
	if doc.Op != "op.list" || doc.Count != 1 {
		t.Errorf("decoded op=%q count=%d", doc.Op, doc.Count)
	}

	out, err = EncodeDocument("op.get", "", 42, false)
	if err != nil || out != nil {
		t.Errorf("EncodeDocument(scalar) = %q, %v; want nil, nil", out, err)
	}
}

// TestValidFieldName pins the names the fields header cannot carry.
func TestValidFieldName(t *testing.T) {
	cases := map[string]bool{
		"id":    true,
		"a.b":   true,
		"":      false,
		" id":   false,
		"id ":   false,
		"a,b":   false,
		"a\nb":  false,
		"a\r\n": false,
	}
	for k, want := range cases {
		if got := validFieldName(k); got != want {
			t.Errorf("validFieldName(%q) = %v; want %v", k, got, want)
		}
	}
}

// TestIsZeroNumber pins what omit_zero_counts drops: numeric zeros only.
// A bool, a string and nil are never zero counts, and a value no numeric
// conversion accepts is not one either.
func TestIsZeroNumber(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want bool
	}{
		{"float zero", float64(0), true},
		{"int zero", 0, true},
		{"int64 zero", int64(0), true},
		{"int32 zero", int32(0), true},
		{"json.Number zero", json.Number("0"), true},
		{"non-zero", float64(3), false},
		{"false is not zero", false, false},
		{"empty string is not zero", "", false},
		{"nil is not zero", nil, false},
		{"malformed json.Number", json.Number("nope"), false},
		{"unconvertible", struct{}{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isZeroNumber(tc.in); got != tc.want {
				t.Errorf("isZeroNumber(%#v) = %v; want %v", tc.in, got, tc.want)
			}
		})
	}
}
