package toon_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ehmo/gum/internal/output/toon"
	"github.com/ehmo/gum/internal/testutil/golden"
)

// TestTOONGoldenEncoding pins three representative TOON encodings under
// testdata/golden/toon/ via the shared golden.Bytes helper (gum-b22o.2). The
// goal is two-fold: (a) lock the wire-format byte-for-byte so a silent
// change in EncodeWithOptions is caught in CI, (b) demonstrate the golden
// framework on an existing producer. The fixtures themselves were generated
// once with `go test ./internal/output/toon/... -update`.
func TestTOONGoldenEncoding(t *testing.T) {
	cases := []struct {
		name  string
		input any
		opts  toon.EncoderOptions
	}{
		{
			name: "homogeneous-array-of-objects",
			input: []map[string]any{
				{"id": "a1", "subject": "hello"},
				{"id": "a2", "subject": "world"},
			},
		},
		{
			name:  "object-zero-counts-kept",
			input: map[string]any{"name": "x", "count": 0},
			opts:  toon.EncoderOptions{},
		},
		{
			name:  "object-zero-counts-omitted",
			input: map[string]any{"name": "x", "count": 0},
			opts:  toon.EncoderOptions{OmitZeroCounts: true},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, err := toon.EncodeWithOptions(tc.input, tc.opts)
			if err != nil {
				t.Fatalf("EncodeWithOptions: %v", err)
			}
			golden.Bytes(t, goldenPath(t, "toon", tc.name+".toon"), got)
		})
	}

}

// goldenPath returns an absolute path under testdata/golden/<bucket>/<name>
// anchored at this test file so tests are runnable from any cwd.
func goldenPath(t *testing.T, bucket, name string) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "testdata", "golden", bucket, name)
}
