package gain

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/tiktoken-go/tokenizer"
)

// resetCodecCache resets the sync.Once-gated tokenizer cache so a test
// can override the codec/err pair, with t.Cleanup restoring the real
// initialized values so later tests still tokenize successfully.
func resetCodecCache(t *testing.T) {
	t.Helper()
	savedCodec, savedErr := cachedCodec, cachedCodecErr
	t.Cleanup(func() {
		codecOnce = sync.Once{}
		cachedCodec, cachedCodecErr = savedCodec, savedErr
		if cachedCodec == nil && cachedCodecErr == nil {
			_, _ = getCodec()
		} else {
			codecOnce.Do(func() {})
		}
	})
	codecOnce = sync.Once{}
	codecOnce.Do(func() {}) // burn the Once so getCodec returns cached vars as-is
	cachedCodec = nil
	cachedCodecErr = nil
}

// TestMeasureTokensCl100kInitErrorWrapsWithSentinel pins the
// `getCodec err != nil` arm: a non-nil cachedCodecErr MUST surface a
// "gain: init tokenizer" wrap. Without this, a one-time tokenizer
// init failure would propagate as a raw error from a downstream
// package, masking which component actually failed.
func TestMeasureTokensCl100kInitErrorWrapsWithSentinel(t *testing.T) {
	resetCodecCache(t)
	cachedCodecErr = errors.New("sentinel: tokenizer init failed")

	_, err := MeasureTokensCl100k([]byte("anything"))
	if err == nil {
		t.Fatal("want init error; got nil")
	}
	if !strings.Contains(err.Error(), "gain: init tokenizer") {
		t.Errorf("err=%v; want 'gain: init tokenizer' wrap", err)
	}
}

// TestMeasureTokensCl100kEncodeErrorWrapsWithSentinel pins the
// `codec.Encode err != nil` arm: an Encode failure MUST surface a
// "gain: tokenize" wrap (distinct from the init wrap above) so callers
// can distinguish "tokenizer never started" from "tokenizer choked on
// this input."
func TestMeasureTokensCl100kEncodeErrorWrapsWithSentinel(t *testing.T) {
	resetCodecCache(t)
	cachedCodec = encodeErrCodec{}

	_, err := MeasureTokensCl100k([]byte("anything"))
	if err == nil {
		t.Fatal("want encode error; got nil")
	}
	if !strings.Contains(err.Error(), "gain: tokenize") {
		t.Errorf("err=%v; want 'gain: tokenize' wrap", err)
	}
}

// encodeErrCodec satisfies tokenizer.Codec but always errors on Encode.
type encodeErrCodec struct{}

func (encodeErrCodec) GetName() string           { return "encodeErr" }
func (encodeErrCodec) Count(string) (int, error) { return 0, errors.New("count fail") }
func (encodeErrCodec) Encode(string) ([]uint, []string, error) {
	return nil, nil, errors.New("sentinel encode failure")
}
func (encodeErrCodec) Decode([]uint) (string, error) { return "", errors.New("decode fail") }

var _ tokenizer.Codec = encodeErrCodec{}
