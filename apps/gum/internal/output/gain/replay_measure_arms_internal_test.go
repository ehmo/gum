package gain

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/tiktoken-go/tokenizer"
)

// errMeasureSentinel is the failure the fail-after-N codec returns.
var errMeasureSentinel = errors.New("sentinel: tokenizer gave up")

// failAfterNCodec counts Encode calls and fails from the nth on. It gives
// each MeasureTokensCl100k call site in processFixture its own reachable
// failure, which no real tokenizer input can produce: the cl100k codec
// encodes any byte string.
type failAfterNCodec struct {
	n     int
	calls atomic.Int32
}

func (c *failAfterNCodec) GetName() string { return "failAfterN" }

func (c *failAfterNCodec) Encode(s string) ([]uint, []string, error) {
	if int(c.calls.Add(1)) >= c.n {
		return nil, nil, errMeasureSentinel
	}
	return make([]uint, len(s)), nil, nil
}

func (c *failAfterNCodec) Count(s string) (int, error) { return len(s), nil }

func (c *failAfterNCodec) Decode([]uint) (string, error) {
	return "", errors.New("decode unsupported")
}

var _ tokenizer.Codec = (*failAfterNCodec)(nil)

// TestProcessFixtureMeasureFailureWraps pins every MeasureTokensCl100k
// error arm in processFixture (replay.go:203-204, 207-208, 211-212 and
// 262-263). Each call site must name which measurement failed, otherwise a
// tokenizer fault in a release replay reports only "tokenize" with no way
// to tell the raw body from the TOON or JSON re-encoding.
func TestProcessFixtureMeasureFailureWraps(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "response.json"), []byte(`{"items":[]}`), 0o644); err != nil {
		t.Fatalf("write response.json: %v", err)
	}

	shaper := Shaper(func(opID, format string, rawBody []byte) (ShapeResult, error) {
		return ShapeResult{Body: []byte("shaped"), OutputProfile: "test/one"}, nil
	})

	cases := []struct {
		name     string
		failAt   int
		shape    Shaper
		wantWrap string
	}{
		{"raw body", 1, nil, "measure tokens in:"},
		{"toon re-encoding", 2, nil, "measure tokens out toon:"},
		{"json re-encoding", 3, nil, "measure tokens out json:"},
		{"shaped body", 4, shaper, "measure tokens shaped:"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetCodecCache(t)
			cachedCodec = &failAfterNCodec{n: tc.failAt}

			_, err := processFixture(dir, "leaf", "toon", false, tc.shape)
			if err == nil {
				t.Fatalf("processFixture with a codec failing on call %d: nil err", tc.failAt)
			}
			if !strings.Contains(err.Error(), tc.wantWrap) {
				t.Errorf("err=%q; want a %q wrap", err, tc.wantWrap)
			}
			if !errors.Is(err, errMeasureSentinel) {
				t.Errorf("err=%v; want it to wrap the codec failure", err)
			}
		})
	}
}
