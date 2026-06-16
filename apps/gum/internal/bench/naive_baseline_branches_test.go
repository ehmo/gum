package bench_test

import (
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/bench"
	"github.com/ehmo/gum/internal/catalog"
)

// TestNaiveInputSchemaNilOpRejected pins NaiveInputSchema's
// `op == nil → error` arm (naive_baseline.go:98-100). The function is
// part of the bench harness; a nil op must surface a typed error
// rather than panic.
func TestNaiveInputSchemaNilOpRejected(t *testing.T) {
	t.Parallel()
	_, err := bench.NaiveInputSchema(nil)
	if err == nil {
		t.Fatal("NaiveInputSchema(nil) err=nil; want nil-op error")
	}
	if !strings.Contains(err.Error(), "nil op") {
		t.Errorf("err=%q; want 'nil op'", err.Error())
	}
}

// TestNaiveToolsListJSONNilCatalogPropagates pins NaiveToolsListJSON's
// `NaiveToolsListPayload err → return err` arm
// (naive_baseline.go:232-234).
func TestNaiveToolsListJSONNilCatalogPropagates(t *testing.T) {
	t.Parallel()
	_, err := bench.NaiveToolsListJSON(nil)
	if err == nil {
		t.Fatal("NaiveToolsListJSON(nil) err=nil; want payload err")
	}
}

// TestNaiveInputSchemaSkipsEmptyAndDupParams pins three continue arms:
//   - empty required param name skipped (naive_baseline.go:105-106)
//   - empty optional param name skipped (naive_baseline.go:112-113)
//   - duplicate optional param name skipped (naive_baseline.go:115-116)
//
// A single op carrying all three malformed-entry shapes exercises all
// three continues in one call.
func TestNaiveInputSchemaSkipsEmptyAndDupParams(t *testing.T) {
	t.Parallel()
	op := &catalog.Op{
		OpID:  "test.op",
		Title: "t",
		ParamsRequired: [][]string{
			{}, // empty → name=="" → continue
			{"required_a", "string"},
		},
		ParamsOptional: [][]string{
			{},                       // empty → name=="" → continue
			{"required_a", "string"}, // dup of required → continue
		},
	}
	got, err := bench.NaiveInputSchema(op)
	if err != nil {
		t.Fatalf("NaiveInputSchema: %v", err)
	}
	if !strings.Contains(string(got), `"required_a"`) {
		t.Errorf("schema=%s; want required_a property", string(got))
	}
}
