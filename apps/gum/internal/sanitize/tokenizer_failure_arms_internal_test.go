package sanitize

import (
	"errors"
	"strings"
	"testing"

	"github.com/tiktoken-go/tokenizer"
)

// swapBudgetCodec installs a replacement tokenizer for the duration of one
// test and restores the real one afterwards.
func swapBudgetCodec(t *testing.T, fn func() (tokenizer.Codec, error)) {
	t.Helper()
	saved := budgetCodec
	t.Cleanup(func() { budgetCodec = saved })
	budgetCodec = fn
}

// encodeErrCodec satisfies tokenizer.Codec and fails every Encode.
type encodeErrCodec struct{}

func (encodeErrCodec) GetName() string           { return "encodeErr" }
func (encodeErrCodec) Count(string) (int, error) { return 0, errors.New("count fail") }
func (encodeErrCodec) Encode(string) ([]uint, []string, error) {
	return nil, nil, errors.New("sentinel encode failure")
}
func (encodeErrCodec) Decode([]uint) (string, error) { return "", errors.New("decode fail") }

var _ tokenizer.Codec = encodeErrCodec{}

// TestSanitizeSurfacesTokenizerInitFailure covers the rule 4/5 init arm.
// A build that cannot start the tokenizer MUST fail loudly rather than
// report a clean description, because no token budget was checked.
func TestSanitizeSurfacesTokenizerInitFailure(t *testing.T) {
	swapBudgetCodec(t, func() (tokenizer.Codec, error) {
		return nil, errors.New("sentinel init failure")
	})

	got, violations, err := Sanitize("Lists calendar events.", "convenience", "read")
	if err == nil {
		t.Fatal("Sanitize err=nil; want the tokenizer init failure")
	}
	if !strings.Contains(err.Error(), "SANITIZER_TOKENIZER_FAILED") {
		t.Errorf("err=%v; want the SANITIZER_TOKENIZER_FAILED code", err)
	}
	if got != "" || violations != nil {
		t.Errorf("Sanitize=(%q, %v); want empty results on failure", got, violations)
	}
}

// TestSanitizeSurfacesTokenizerEncodeFailure covers the rule 4/5 encode arm,
// which is distinct from the init arm above: the codec started but choked on
// this description.
func TestSanitizeSurfacesTokenizerEncodeFailure(t *testing.T) {
	swapBudgetCodec(t, func() (tokenizer.Codec, error) { return encodeErrCodec{}, nil })

	got, violations, err := Sanitize("Lists calendar events.", "meta", "read")
	if err == nil {
		t.Fatal("Sanitize err=nil; want the tokenizer encode failure")
	}
	if !strings.Contains(err.Error(), "SANITIZER_TOKENIZER_FAILED") {
		t.Errorf("err=%v; want the SANITIZER_TOKENIZER_FAILED code", err)
	}
	if !strings.Contains(err.Error(), "sentinel encode failure") {
		t.Errorf("err=%v; want the underlying encode error wrapped", err)
	}
	if got != "" || violations != nil {
		t.Errorf("Sanitize=(%q, %v); want empty results on failure", got, violations)
	}
}
