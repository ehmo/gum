package toon_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/output/toon"
)

// testdataDir returns the absolute path to the testdata directory adjacent to
// this test file, which is required because tests may be run from any working
// directory.
func testdataDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "testdata")
}

// loadFixture reads a testdata file and returns its contents.
func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(testdataDir(t), name))
	if err != nil {
		t.Fatalf("loadFixture %s: %v", name, err)
	}
	return data
}

// normaliseJSON round-trips raw JSON through json.Unmarshal + json.Marshal to
// produce a canonical form for structural comparison.
func normaliseJSON(t *testing.T, data []byte) []byte {
	t.Helper()
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("normaliseJSON Unmarshal: %v (input: %s)", err, data)
	}
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("normaliseJSON Marshal: %v", err)
	}
	return out
}

// catchPanicToon wraps fn in a recover. Returns (message, true) if fn panicked.
func catchPanicToon(fn func()) (msg string, panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			msg = fmt.Sprintf("panic: %v", r)
			panicked = true
		}
	}()
	fn()
	return "", false
}

// TestToonRoundTrip runs four sub-tests, one per fixture pair in testdata/.
// For each pair:
//   - Encode JSON fixture → assert byte-for-byte equals .toon fixture.
//   - Decode .toon fixture → re-marshal as JSON → structural equality with .json fixture.
func TestToonRoundTrip(t *testing.T) {
	defer goleak.VerifyNone(t)

	tests := []struct {
		name        string
		opts        toon.EncoderOptions
		description string
	}{
		{
			name:        "list-with-nulls",
			opts:        toon.EncoderOptions{},
			description: "JSON array of homogeneous objects with null values",
		},
		{
			name:        "quoted-csv",
			opts:        toon.EncoderOptions{},
			description: "strings requiring CSV double-quoting (comma, double-quote, newline)",
		},
		{
			name:        "zero-omitted-count",
			opts:        toon.EncoderOptions{OmitZeroCounts: true},
			description: "object with zero-valued integer fields omitted when OmitZeroCounts=true",
		},
		{
			name:        "empty-body",
			opts:        toon.EncoderOptions{},
			description: "object with all-empty fields encodes as {} not <empty>",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			jsonData := loadFixture(t, tc.name+".json")
			wantToon := loadFixture(t, tc.name+".toon")

			// Parse JSON into a Go value.
			var v any
			if err := json.Unmarshal(jsonData, &v); err != nil {
				t.Fatalf("unmarshal fixture JSON: %v", err)
			}

			// --- Encode JSON → TOON, compare byte-for-byte ---
			var gotToon []byte
			var encErr error
			panicMsg, panicked := catchPanicToon(func() {
				gotToon, encErr = toon.EncodeWithOptions(v, tc.opts)
			})
			if panicked {
				t.Fatalf("EncodeWithOptions panicked: %s — green team must implement EncodeWithOptions", panicMsg)
			}
			if encErr != nil {
				t.Fatalf("EncodeWithOptions: %v", encErr)
			}
			if !bytes.Equal(gotToon, wantToon) {
				t.Errorf("TOON encode mismatch\ngot:\n%s\nwant:\n%s", gotToon, wantToon)
			}

			// --- Decode TOON → assert structural equality with original JSON ---
			var decoded any
			var decErr error
			panicMsg, panicked = catchPanicToon(func() {
				decoded, decErr = toon.Decode(wantToon)
			})
			if panicked {
				t.Fatalf("Decode panicked: %s — green team must implement Decode", panicMsg)
			}
			if decErr != nil {
				t.Fatalf("Decode: %v", decErr)
			}
			decodedJSON, err := json.Marshal(decoded)
			if err != nil {
				t.Fatalf("Marshal decoded value: %v", err)
			}
			// Normalize both sides for structural comparison.
			gotNorm := normaliseJSON(t, decodedJSON)
			wantNorm := normaliseJSON(t, jsonData)
			if !bytes.Equal(gotNorm, wantNorm) {
				t.Errorf("TOON decode→JSON mismatch\ngot:  %s\nwant: %s", gotNorm, wantNorm)
			}
		})
	}
}

// TestEncodeEmptyObjectSentinel specifically asserts that encoding an object
// whose fields are all empty/zero (default values) produces "{}\n" and NOT "<empty>".
func TestEncodeEmptyObjectSentinel(t *testing.T) {
	defer goleak.VerifyNone(t)

	// A map of only empty/nil values collapses to the {} sentinel.
	got, err := toon.Encode(map[string]any{"name": "", "value": ""})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if bytes.Contains(got, []byte("<empty>")) {
		t.Errorf("encoded output contains <empty> sentinel; want {}: %q", got)
	}
	if !bytes.Contains(got, []byte("{}")) {
		t.Errorf("all-empty map did not collapse to {}; got: %q", got)
	}

	// A zero-valued number is REAL data and must be preserved — it must NOT be
	// treated as sentinel-empty and collapsed to {} (silent data loss).
	got2, err := toon.Encode(map[string]any{"count": 0, "size": 0})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !bytes.Contains(got2, []byte("count=0")) || !bytes.Contains(got2, []byte("size=0")) {
		t.Errorf("zero-valued fields were dropped (collapsed to {}); got: %q", got2)
	}
}
