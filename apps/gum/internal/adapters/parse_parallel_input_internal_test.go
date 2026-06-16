package adapters

import (
	"strings"
	"testing"
)

// TestParseParallelInputRejectsNonList pins the type-guard branch:
// the raw input MUST be []any. A scalar or map is rejected with an
// INVALID_ARGS error so Risor scripts see a clear "expected list"
// diagnostic instead of a panic on the type assertion.
func TestParseParallelInputRejectsNonList(t *testing.T) {
	cases := []any{
		"not a list",
		map[string]any{"op_id": "x"},
		42,
		nil,
	}
	for _, c := range cases {
		_, err := parseParallelInput(c)
		if err == nil {
			t.Errorf("input=%v: want INVALID_ARGS error; got nil", c)
			continue
		}
		if !strings.Contains(err.Error(), "expected list") {
			t.Errorf("input=%v: err=%v; want 'expected list'", c, err)
		}
	}
}

// TestParseParallelInputRejectsNonMapElement pins the element-type
// guard: each list entry must be a map. A string element surfaces a
// "not a map" error with the index for easier debugging.
func TestParseParallelInputRejectsNonMapElement(t *testing.T) {
	_, err := parseParallelInput([]any{"not a map"})
	if err == nil {
		t.Fatal("want error; got nil")
	}
	if !strings.Contains(err.Error(), "element 0 is not a map") {
		t.Errorf("err=%v; want 'element 0 is not a map'", err)
	}
}

// TestParseParallelInputRejectsMissingOpID pins the required-field
// guard: an element without op_id (and without the "op" alias) MUST
// fail rather than silently dispatching to an empty op.
func TestParseParallelInputRejectsMissingOpID(t *testing.T) {
	_, err := parseParallelInput([]any{map[string]any{"args": map[string]any{}}})
	if err == nil {
		t.Fatal("want error; got nil")
	}
	if !strings.Contains(err.Error(), "missing 'op_id'") {
		t.Errorf("err=%v; want missing-op_id diag", err)
	}
}

// TestParseParallelInputArgsMustBeMap pins the args type-guard: when
// the args key is present but not a map, dispatch would reject it
// downstream; surface the error here for a sharper diagnostic.
func TestParseParallelInputArgsMustBeMap(t *testing.T) {
	_, err := parseParallelInput([]any{map[string]any{"op_id": "x", "args": "not a map"}})
	if err == nil {
		t.Fatal("want error; got nil")
	}
	if !strings.Contains(err.Error(), "'args' is not a map") {
		t.Errorf("err=%v; want args-not-a-map diag", err)
	}
}

// TestParseParallelInputAcceptsOpAlias pins the "op" → op_id alias:
// callers can use either key. A regression that only honoured op_id
// would silently drop elements written against earlier Risor docs.
func TestParseParallelInputAcceptsOpAlias(t *testing.T) {
	out, err := parseParallelInput([]any{map[string]any{"op": "alias.op"}})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if len(out) != 1 || out[0].OpID != "alias.op" {
		t.Errorf("out=%+v; want one element with OpID=alias.op", out)
	}
	if out[0].Args == nil {
		t.Errorf("Args=nil; want empty map (caller may not nil-check)")
	}
}

// TestParseParallelInputHappyPath pins the success branch: all fields
// — op_id, variant_id, args — round-trip into the parallelElement.
func TestParseParallelInputHappyPath(t *testing.T) {
	out, err := parseParallelInput([]any{
		map[string]any{
			"op_id":      "gmail.users.messages.list",
			"variant_id": "v1",
			"args":       map[string]any{"userId": "me"},
		},
	})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if len(out) != 1 {
		t.Fatalf("len(out)=%d; want 1", len(out))
	}
	got := out[0]
	if got.OpID != "gmail.users.messages.list" {
		t.Errorf("OpID=%q; want gmail...list", got.OpID)
	}
	if got.VariantID != "v1" {
		t.Errorf("VariantID=%q; want v1", got.VariantID)
	}
	if got.Args["userId"] != "me" {
		t.Errorf("Args[userId]=%v; want me", got.Args["userId"])
	}
}

// TestParseParallelInputRejectsOversizedBatch pins the audit fix: a batch with
// more than parallelMaxElements items is rejected locally (input-amplification
// resource-exhaustion guard), rather than fanning out unboundedly.
func TestParseParallelInputRejectsOversizedBatch(t *testing.T) {
	big := make([]any, parallelMaxElements+1)
	for i := range big {
		big[i] = map[string]any{"op_id": "x"}
	}
	if _, err := parseParallelInput(big); err == nil {
		t.Fatalf("expected error for batch of %d (> max %d)", len(big), parallelMaxElements)
	}
	// A batch exactly at the cap is accepted.
	okBatch := make([]any, parallelMaxElements)
	for i := range okBatch {
		okBatch[i] = map[string]any{"op_id": "x"}
	}
	if _, err := parseParallelInput(okBatch); err != nil {
		t.Fatalf("batch at the cap (%d) should be accepted: %v", parallelMaxElements, err)
	}
}
