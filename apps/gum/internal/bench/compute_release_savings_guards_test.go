package bench_test

import (
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/bench"
)

// TestComputeReleaseSavingsRejectsEmptyNaiveJSON pins the
// `len(naiveToolsListJSON) == 0 → err` guard. The naive baseline tokens
// are the denominator of AggregateSavingsPct; passing an empty body
// would yield a meaningless 0/0 ratio downstream, so the function MUST
// fail loud rather than fall through.
func TestComputeReleaseSavingsRejectsEmptyNaiveJSON(t *testing.T) {
	_, err := bench.ComputeReleaseSavings(t.TempDir(), nil, []byte(`{"tools":[]}`))
	if err == nil {
		t.Fatal("want empty-naive guard err; got nil")
	}
	if !strings.Contains(err.Error(), "empty naiveToolsListJSON") {
		t.Errorf("err=%v; want 'empty naiveToolsListJSON' substr", err)
	}
}

// TestComputeReleaseSavingsRejectsEmptyGumJSON pins the
// `len(gumToolsListJSON) == 0 → err` guard. The gum baseline tokens
// are the numerator-adjusting term for AggregateSavingsPct; an empty
// body would short-circuit shaping-savings reporting to "100%" which
// is a misleading release-blog number.
func TestComputeReleaseSavingsRejectsEmptyGumJSON(t *testing.T) {
	_, err := bench.ComputeReleaseSavings(t.TempDir(), []byte(`{"tools":[]}`), nil)
	if err == nil {
		t.Fatal("want empty-gum guard err; got nil")
	}
	if !strings.Contains(err.Error(), "empty gumToolsListJSON") {
		t.Errorf("err=%v; want 'empty gumToolsListJSON' substr", err)
	}
}
