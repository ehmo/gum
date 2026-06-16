package toon_test

import (
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/output/toon"
)

// TestEncodeArrayCSVCellNestedMapMarshalError pins encodeCSVValue's
// nested-map json.Marshal err arm (encoder.go:303-305). When a CSV
// cell value is a map[string]any whose contents can't be marshalled
// (here: a chan int), the CSV encoder MUST surface that err — it
// can't emit a row with an unencodable cell and silently truncating
// would produce malformed CSV.
func TestEncodeArrayCSVCellNestedMapMarshalError(t *testing.T) {
	t.Parallel()
	// Homogeneous []any of maps so encodeArray takes the CSV-table path;
	// the cell is a nested map containing a chan that json.Marshal rejects.
	in := []any{
		map[string]any{"k": map[string]any{"inner": make(chan int)}},
	}
	_, err := toon.Encode(in)
	if err == nil {
		t.Fatal("Encode([{k: {inner: chan}}])=nil err; want json.Marshal err from encodeCSVValue")
	}
	if !strings.Contains(err.Error(), "unsupported type") {
		t.Errorf("err=%q; want 'unsupported type' surface", err)
	}
}

// TestEncodeArrayCSVCellNestedArrayMarshalError pins encodeCSVValue's
// nested-array json.Marshal err arm (encoder.go:310-312). Distinct from
// the map arm above — needs its own propagation test.
func TestEncodeArrayCSVCellNestedArrayMarshalError(t *testing.T) {
	t.Parallel()
	in := []any{
		map[string]any{"k": []any{make(chan int)}},
	}
	_, err := toon.Encode(in)
	if err == nil {
		t.Fatal("Encode([{k: [chan]}])=nil err; want json.Marshal err from encodeCSVValue")
	}
	if !strings.Contains(err.Error(), "unsupported type") {
		t.Errorf("err=%q; want 'unsupported type' surface", err)
	}
}

// TestEncodeArrayCSVCellScalarEncodeError pins encodeCSVValue's default
// arm — encodeScalar err (encoder.go:317-319). When a CSV cell value
// is a non-map non-array Go type that encodeScalar can't handle (chan
// int falls through to encodeScalar's json.Marshal fallback which
// rejects channel types), the err MUST propagate.
func TestEncodeArrayCSVCellScalarEncodeError(t *testing.T) {
	t.Parallel()
	in := []any{
		map[string]any{"k": make(chan int)},
	}
	_, err := toon.Encode(in)
	if err == nil {
		t.Fatal("Encode([{k: chan}])=nil err; want encodeScalar err from encodeCSVValue")
	}
	if !strings.Contains(err.Error(), "unsupported type") {
		t.Errorf("err=%q; want 'unsupported type' surface", err)
	}
}

// TestEncodeArrayHeterogeneousMarshalError pins encodeArray's
// heterogeneous-fallback json.Marshal err arm (encoder.go:338-340).
// Mixed-type arrays take the JSON-with-comment fallback; if the array
// contains an unencodable element (here: chan int alongside a string
// so homogeneousKeys returns nil), json.Marshal returns err.
func TestEncodeArrayHeterogeneousMarshalError(t *testing.T) {
	t.Parallel()
	// String + chan: heterogeneous (first is not a map), so encodeArray
	// falls into the json.Marshal heterogeneous arm with the chan,
	// triggering the err.
	in := []any{"first", make(chan int)}
	_, err := toon.Encode(in)
	if err == nil {
		t.Fatal("Encode([str, chan])=nil err; want heterogeneous-fallback json.Marshal err")
	}
	if !strings.Contains(err.Error(), "unsupported type") {
		t.Errorf("err=%q; want 'unsupported type' surface", err)
	}
}

// TestEncodeArrayHomogeneousCSVCellEncodeError pins encodeArray's
// data-row encodeCSVValue err propagation (encoder.go:368-370). The
// outer Encode→encodeArray happy path reaches the data-row loop and
// the chan cell triggers encodeCSVValue err which bubbles back through
// encodeArray's return.
func TestEncodeArrayHomogeneousCSVCellEncodeError(t *testing.T) {
	t.Parallel()
	// Two homogeneous rows so encodeArray takes the CSV path and the
	// second row's chan trips encodeCSVValue at the data-row level.
	in := []any{
		map[string]any{"k": "ok"},
		map[string]any{"k": make(chan int)},
	}
	_, err := toon.Encode(in)
	if err == nil {
		t.Fatal("Encode(homog CSV with chan in row 2)=nil err; want encodeCSVValue err propagation")
	}
	if !strings.Contains(err.Error(), "unsupported type") {
		t.Errorf("err=%q; want 'unsupported type' surface", err)
	}
}
