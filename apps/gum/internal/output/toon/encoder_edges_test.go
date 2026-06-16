package toon_test

import (
	"math"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/output/toon"
)

// TestEncodeScalarTypes covers the int64/int32/uint64/uint32 branches in
// encodeScalar (previously 45.5%).
func TestEncodeScalarTypes(t *testing.T) {
	cases := []struct {
		label string
		input any
		want  string
	}{
		{"int", int(42), "42"},
		{"int64", int64(123), "123"},
		{"int32", int32(-7), "-7"},
		{"uint64", uint64(1 << 40), "1099511627776"},
		{"uint32", uint32(99), "99"},
		{"true", true, "true"},
		{"false", false, "false"},
		{"nil", nil, "null"},
		{"float-fractional", 1.5, "1.5"},
		{"float-whole", float64(7), "7"},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			got, err := toon.Encode(tc.input)
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			if strings.TrimSpace(string(got)) != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestEncodeNaNAndInf covers the encodeScalar error branches: NaN and Inf
// floats must surface an error rather than emit invalid TOON.
func TestEncodeNaNAndInf(t *testing.T) {
	cases := []any{math.NaN(), math.Inf(1), math.Inf(-1)}
	for _, tc := range cases {
		_, err := toon.Encode(tc)
		if err == nil {
			t.Errorf("expected error for %v, got nil", tc)
		}
	}
}

// TestEncodeStringEdges covers the encodeString empty-string and
// CSV-quoting branches (previously 80% — empty/keyword/special-char paths).
func TestEncodeStringEdges(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"", `""`},                 // empty string
		{"true", `"true"`},         // keyword needs quoting
		{"null", `"null"`},         // keyword needs quoting
		{`he"llo`, `"he""llo"`},    // interior quote → doubled
		{"a,b", `"a,b"`},           // comma forces CSV-quote
		{"plain", `plain`},         // bare
	}
	for _, tc := range cases {
		got, err := toon.Encode(tc.input)
		if err != nil {
			t.Fatalf("Encode %q: %v", tc.input, err)
		}
		if strings.TrimSpace(string(got)) != tc.want {
			t.Errorf("input %q: got %s, want %s", tc.input, got, tc.want)
		}
	}
}

// TestEncodeOmitZeroCountsDropsZeros covers the OmitZeroCounts branch in
// encodeMap (isZeroVal for non-float kinds previously 50%).
func TestEncodeOmitZeroCountsDropsZeros(t *testing.T) {
	v := map[string]any{
		"a": int64(0),
		"b": float64(0),
		"c": uint64(0),
		"d": int(1),
		"e": "keep",
	}
	got, err := toon.EncodeWithOptions(v, toon.EncoderOptions{OmitZeroCounts: true})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	out := string(got)
	for _, want := range []string{"d=1", "e=keep"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	for _, dropped := range []string{"a=", "b=", "c="} {
		if strings.Contains(out, dropped) {
			t.Errorf("unexpected %q in:\n%s", dropped, out)
		}
	}
}

// TestEncodeOmitZeroCountsAllZero covers the "all values skipped → {}"
// fallback in encodeMap.
func TestEncodeOmitZeroCountsAllZero(t *testing.T) {
	v := map[string]any{"a": 0, "b": 0}
	got, err := toon.EncodeWithOptions(v, toon.EncoderOptions{OmitZeroCounts: true})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if strings.TrimSpace(string(got)) != "{}" {
		t.Errorf("got %q, want {}", got)
	}
}

// TestEncodeEmptyMap covers the empty-map branch of encodeMap.
func TestEncodeEmptyMap(t *testing.T) {
	got, err := toon.Encode(map[string]any{})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if strings.TrimSpace(string(got)) != "{}" {
		t.Errorf("got %q, want {}", got)
	}
}

// TestEncodeEmptyArray covers the empty-array branch of encodeArray.
func TestEncodeEmptyArray(t *testing.T) {
	got, err := toon.Encode([]any{})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if strings.TrimSpace(string(got)) != "[]" {
		t.Errorf("got %q, want []", got)
	}
}

// TestEncodeHeterogeneousArrayFallsBackToJSON covers the homogeneousKeys
// nil-return path → "# heterogeneous" JSON fallback in encodeArray.
func TestEncodeHeterogeneousArrayFallsBackToJSON(t *testing.T) {
	v := []any{
		map[string]any{"a": 1},
		map[string]any{"b": 2}, // different key set → heterogeneous
	}
	got, err := toon.Encode(v)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !strings.HasPrefix(string(got), "# heterogeneous\n") {
		t.Errorf("expected heterogeneous prefix, got:\n%s", got)
	}
}

// TestEncodeArrayOfScalarsHeterogeneous covers homogeneousKeys returning
// nil when arr[0] is not a map.
func TestEncodeArrayOfScalarsHeterogeneous(t *testing.T) {
	v := []any{1, "two", true}
	got, err := toon.Encode(v)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !strings.HasPrefix(string(got), "# heterogeneous\n") {
		t.Errorf("expected heterogeneous prefix, got:\n%s", got)
	}
}

// TestEncodeMapWithNestedJSON covers the nested-map and nested-array
// inline JSON branches inside encodeMap (lines 215-226 in encoder.go).
func TestEncodeMapWithNestedJSON(t *testing.T) {
	v := map[string]any{
		"obj":  map[string]any{"x": 1},
		"arr":  []any{1, 2, 3},
		"name": "alice",
	}
	got, err := toon.Encode(v)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	for _, want := range []string{`obj={"x":1}`, `arr=[1,2,3]`, `name=alice`} {
		if !strings.Contains(string(got), want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// TestEncodeHomogeneousArrayWithNestedValue covers the nested-map and
// nested-array branches inside encodeCSVValue (lines 300-314).
func TestEncodeHomogeneousArrayWithNestedValue(t *testing.T) {
	v := []any{
		map[string]any{"id": 1, "tags": []any{"a", "b"}, "meta": map[string]any{"k": "v"}},
		map[string]any{"id": 2, "tags": []any{"c"}, "meta": map[string]any{"k": "w"}},
	}
	got, err := toon.Encode(v)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	out := string(got)
	if !strings.Contains(out, "id,meta,tags") && !strings.Contains(out, "id,tags,meta") {
		t.Errorf("expected CSV header with all three columns in:\n%s", out)
	}
}

// TestEncodeNullCellInHomogeneousArray covers the v==nil branch of
// encodeCSVValue (null cell → empty string).
func TestEncodeNullCellInHomogeneousArray(t *testing.T) {
	v := []any{
		map[string]any{"a": 1, "b": nil},
		map[string]any{"a": 2, "b": "x"},
	}
	got, err := toon.Encode(v)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	// Expect first data row to have empty b cell: "1,"
	lines := strings.Split(strings.TrimSpace(string(got)), "\n")
	if len(lines) < 3 {
		t.Fatalf("expected 3 lines (header + 2 data), got %d:\n%s", len(lines), got)
	}
	if !strings.Contains(lines[1], "1,") {
		t.Errorf("expected '1,' (empty b cell) in row 1, got %q", lines[1])
	}
}

// TestDecodeKeyValueRoundTrip covers Decode's key=value branch and
// decodeCSVCell's bare-string and numeric-parsing branches.
func TestDecodeKeyValueRoundTrip(t *testing.T) {
	input := "name=alice\nage=30\nactive=true\nnone=null\n"
	got, err := toon.Decode([]byte(input))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("Decode returned %T, want map", got)
	}
	if m["name"] != "alice" {
		t.Errorf("name=%v, want alice", m["name"])
	}
	if m["age"] != float64(30) {
		t.Errorf("age=%v, want 30", m["age"])
	}
	if m["active"] != true {
		t.Errorf("active=%v, want true", m["active"])
	}
	if m["none"] != nil {
		t.Errorf("none=%v, want nil", m["none"])
	}
}

// TestDecodeCSVTableRoundTrip covers decodeCSVTable + parseCSVRow + decodeCSVCell.
func TestDecodeCSVTableRoundTrip(t *testing.T) {
	input := "id,name\n1,alice\n2,bob\n"
	got, err := toon.Decode([]byte(input))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	arr, ok := got.([]any)
	if !ok {
		t.Fatalf("Decode returned %T, want []any", got)
	}
	if len(arr) != 2 {
		t.Fatalf("len=%d, want 2", len(arr))
	}
	row0 := arr[0].(map[string]any)
	if row0["name"] != "alice" {
		t.Errorf("row0.name=%v, want alice", row0["name"])
	}
}

// TestDecodeHeadersFormat covers decodeHeadersFormat (was 0%).
func TestDecodeHeadersFormat(t *testing.T) {
	input := "# headers: id,name\n1,alice\n2,bob\n"
	got, err := toon.Decode([]byte(input))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	arr, ok := got.([]any)
	if !ok {
		t.Fatalf("Decode returned %T, want []any", got)
	}
	if len(arr) != 2 {
		t.Fatalf("len=%d, want 2; got %v", len(arr), arr)
	}
	row0, ok := arr[0].(map[string]any)
	if !ok {
		t.Fatalf("row0 is %T", arr[0])
	}
	if row0["name"] != "alice" {
		t.Errorf("row0.name=%v, want alice", row0["name"])
	}
}

// TestDecodeHeadersFormatNoNewline covers the early-return branch when
// the input has no newline (idx < 0 → returns empty array).
func TestDecodeHeadersFormatNoNewline(t *testing.T) {
	input := "# headers: id,name"
	got, err := toon.Decode([]byte(input))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	arr, ok := got.([]any)
	if !ok {
		t.Fatalf("Decode returned %T, want []any", got)
	}
	if len(arr) != 0 {
		t.Errorf("len=%d, want 0", len(arr))
	}
}

// TestDecodeHeterogeneousArray covers the # heterogeneous branch of Decode.
func TestDecodeHeterogeneousArray(t *testing.T) {
	input := "# heterogeneous\n[1,\"two\",true]\n"
	got, err := toon.Decode([]byte(input))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	arr, ok := got.([]any)
	if !ok || len(arr) != 3 {
		t.Fatalf("got %v, want 3-element slice", got)
	}
}

// TestDecodeScalar covers the scalar branch of Decode (single value
// without "=" or ",").
func TestDecodeScalar(t *testing.T) {
	cases := []struct {
		input string
		want  any
	}{
		{"null", nil},
		{"true", true},
		{"false", false},
		{"42", float64(42)},
		{`"hello"`, "hello"},
		{"bare", "bare"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := toon.Decode([]byte(tc.input))
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// TestDecodeEmpty covers the empty-input branch.
func TestDecodeEmpty(t *testing.T) {
	got, err := toon.Decode([]byte(""))
	if err != nil {
		t.Fatalf("Decode empty: %v", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

// TestDecodeEmptySentinels covers the {} and [] sentinel branches.
func TestDecodeEmptySentinels(t *testing.T) {
	gotMap, err := toon.Decode([]byte("{}"))
	if err != nil {
		t.Fatalf("Decode {}: %v", err)
	}
	if m, ok := gotMap.(map[string]any); !ok || len(m) != 0 {
		t.Errorf("got %v, want empty map", gotMap)
	}
	gotArr, err := toon.Decode([]byte("[]"))
	if err != nil {
		t.Fatalf("Decode []: %v", err)
	}
	if a, ok := gotArr.([]any); !ok || len(a) != 0 {
		t.Errorf("got %v, want empty array", gotArr)
	}
}

// TestDecodeCSVCellJSONArray covers the "{ or [ → json.Unmarshal" branch
// of decodeCSVCell.
func TestDecodeCSVCellJSONArray(t *testing.T) {
	// A CSV row containing a JSON array inside a quoted cell.
	input := "id,tags\n1,\"[1,2,3]\"\n"
	got, err := toon.Decode([]byte(input))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	arr := got.([]any)
	row := arr[0].(map[string]any)
	tags, ok := row["tags"].([]any)
	if !ok || len(tags) != 3 {
		t.Errorf("tags=%v, want 3-element array", row["tags"])
	}
}

// TestDecodeKeyValueQuotedKey covers the unquoteString branch when a
// key=value pair has a quoted key.
func TestDecodeKeyValueQuotedKey(t *testing.T) {
	input := `"a,b"=value` + "\n"
	got, err := toon.Decode([]byte(input))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	m := got.(map[string]any)
	if m["a,b"] != "value" {
		t.Errorf("got %v, want {a,b: value}", m)
	}
}

// TestEncodeCSVValueWithNewlineInString covers the encodeCSVValue
// "contains newline → complex=true" branch by encoding a homogeneous
// table containing a multi-line string field.
func TestEncodeCSVValueWithNewlineInString(t *testing.T) {
	// Strings with newlines are quoted and embedded with literal newlines;
	// the encoder is expected to handle this without panicking.
	v := []any{
		map[string]any{"k": "line1\nline2"},
		map[string]any{"k": "ok"},
	}
	got, err := toon.Encode(v)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if len(got) == 0 {
		t.Error("empty output")
	}
}
