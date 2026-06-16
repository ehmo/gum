// Package dispatch — Red Team failing tests for step 1 parse-and-validate (issue gum-vq4z.1).
//
// Spec anchors:
//   - §3.1 step 2 (catalog resolution, alias normalization, op_id lookup)
//   - §4.1 op_id validation ("OP_NOT_FOUND + BM25 suggestions")
//   - §5.3/§5.4 params_required / params_optional type grammar
//   - §8.42 / spec line 842: INVALID_ARGS envelope {"missing":[...],"unknown":[...],"type_errors":[...]}
//
// These tests are intentionally written against the *target* parseAndValidate
// signature:
//
//	func (d *dispatcher) parseAndValidate(ctx, inv) (*parsedInvocation, *StructuredError)
//
// The current stub returns (*Invocation, error), so ALL tests in this file fail
// to compile until Green Team implements the new surface. That is the desired state.
//
// Required new types / helpers (Green Team must add):
//
//	type parsedInvocation struct {
//	    OpID     string         // canonical (alias-resolved) op_id
//	    Args     map[string]any // nil-safe copy
//	    ArgsHash string         // JCS-canonical SHA-256 hex of Args
//	    // ... any additional dispatch-internal fields
//	}
//
// The parseAndValidate method signature must change to:
//
//	func (d *dispatcher) parseAndValidate(ctx context.Context, inv *Invocation) (*parsedInvocation, *StructuredError)
//
// testCatalog() constructs a minimal *catalog.Catalog inline, bypassing the JSON
// fixture, so tests remain self-contained and fast.
package dispatch

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/catalog"
)

// ── helpers ──────────────────────────────────────────────────────────────────

// testCatalog builds a minimal *catalog.Catalog containing two ops:
//
//   - "test.op.required"  params_required: [["foo","string"]]
//     params_optional:  [["bar","integer"]]
//   - "test.op.alias"     canonical id; alias "test.alias" points to it
//
// The catalog is not validated (Validate() would need correct metadata);
// instead the fields needed by parseAndValidate are populated directly.
func testCatalog() *catalog.Catalog {
	makeVariant := func(id string) catalog.Variant {
		return catalog.Variant{
			VariantID:     id,
			Stability:     catalog.StabilityStable,
			InterfaceKind: catalog.InterfaceKindSDKNative,
			BackendKind:   catalog.BackendKindTypedRestSDK,
			RiskClass:     catalog.RiskClassRead,
			Binding: &catalog.Binding{
				BindingSchemaVersion: 1,
				AdapterKey:           "test.adapter",
				OperationKey:         id + ".exec",
			},
		}
	}

	return &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratedAt:          time.Now().UTC().Format(time.RFC3339),
		GeneratorVersion:     "test@0.0.0",
		Ops: []catalog.Op{
			{
				// Op with one required string param, one optional integer param.
				OpID:             "test.op.required",
				OpSchemaVersion:  1,
				Title:            "Test op with required args",
				Summary:          "Used by Red Team step-1 tests.",
				DefaultVariantID: "test.op.required.v1",
				ParamsRequired:   [][]string{{"foo", "string"}},
				ParamsOptional:   [][]string{{"bar", "integer"}},
				Variants:         []catalog.Variant{makeVariant("test.op.required.v1")},
			},
			{
				// Op with no required args — used to verify valid-args pass-through.
				OpID:             "test.op.noargs",
				OpSchemaVersion:  1,
				Title:            "Test op with no required args",
				Summary:          "Used by Red Team step-1 tests.",
				DefaultVariantID: "test.op.noargs.v1",
				ParamsRequired:   nil,
				ParamsOptional:   nil,
				Variants:         []catalog.Variant{makeVariant("test.op.noargs.v1")},
			},
			{
				// Op whose deprecated_op_ids list contains an alias.
				// parseAndValidate must resolve "test.alias" → "test.op.alias".
				OpID:             "test.op.alias",
				OpSchemaVersion:  1,
				Title:            "Test op with alias",
				Summary:          "Used by Red Team alias-normalization test.",
				DefaultVariantID: "test.op.alias.v1",
				ParamsRequired:   nil,
				ParamsOptional:   nil,
				DeprecatedOpIDs:  []string{"test.alias"},
				Variants:         []catalog.Variant{makeVariant("test.op.alias.v1")},
			},
		},
	}
}

// newTestDispatcher constructs a dispatcher wired to the inline test catalog.
func newTestDispatcher() *dispatcher {
	return &dispatcher{
		snapshot: testCatalog(),
		adapters: map[string]Adapter{},
	}
}

// ── tests ─────────────────────────────────────────────────────────────────────

// TestParseAndValidateOpNotFound verifies that an unknown op_id returns a
// StructuredError with ErrCode == ErrCodeOpNotFound and a "suggestions" detail
// key (spec §4.1, spec line 331: "up to 3 BM25-fuzzy matches").
func TestParseAndValidateOpNotFound(t *testing.T) {
	d := newTestDispatcher()
	inv := &Invocation{OpID: "non.existent.op", Args: nil}

	_, serr := d.parseAndValidate(context.Background(), inv)
	if serr == nil {
		t.Fatal("expected *StructuredError for unknown op_id, got nil")
	}
	if serr.ErrCode != ErrCodeOpNotFound {
		t.Errorf("expected ErrCode=%q, got %q", ErrCodeOpNotFound, serr.ErrCode)
	}
	// Detail["suggestions"] must exist and be a slice (possibly empty per spec v0.1.0 note).
	sug, ok := serr.Detail["suggestions"]
	if !ok {
		t.Errorf("expected detail key 'suggestions' in StructuredError, got detail=%v", serr.Detail)
	}
	// It must be a slice type ([]string or []any); just assert it is not nil.
	if sug == nil {
		t.Errorf("detail['suggestions'] must not be nil (should be an empty or populated slice)")
	}
}

// TestParseAndValidateOpNotFoundSuggestsNearMiss pins the gum-l0op #2 wiring:
// a typo'd op_id must surface the closest real op_id in the envelope's
// "suggestions" detail (nearest first), turning the always-empty list into an
// actionable "did you mean". This guards the integration seam between
// parseAndValidate and suggestOpIDs, not just the matcher in isolation.
func TestParseAndValidateOpNotFoundSuggestsNearMiss(t *testing.T) {
	d := newTestDispatcher()
	// "test.op.requierd" is a one-transposition typo of "test.op.required".
	inv := &Invocation{OpID: "test.op.requierd", Args: nil}

	_, serr := d.parseAndValidate(context.Background(), inv)
	if serr == nil {
		t.Fatal("expected *StructuredError for near-miss op_id, got nil")
	}
	if serr.ErrCode != ErrCodeOpNotFound {
		t.Errorf("expected ErrCode=%q, got %q", ErrCodeOpNotFound, serr.ErrCode)
	}
	sug, ok := serr.Detail["suggestions"].([]string)
	if !ok {
		t.Fatalf("detail['suggestions'] must be []string, got %T", serr.Detail["suggestions"])
	}
	if len(sug) == 0 {
		t.Fatal("near-miss op_id must yield at least one suggestion, got empty")
	}
	if sug[0] != "test.op.required" {
		t.Errorf("nearest suggestion = %q; want 'test.op.required' first (got %v)", sug[0], sug)
	}
}

// TestParseAndValidateMissingRequiredArgs verifies that calling an op without
// its required args returns INVALID_ARGS with detail["missing"] populated.
// Spec §5.3 / spec line 837 / spec line 842.
func TestParseAndValidateMissingRequiredArgs(t *testing.T) {
	d := newTestDispatcher()
	// "test.op.required" requires "foo"; call with no args.
	inv := &Invocation{OpID: "test.op.required", Args: map[string]any{}}

	_, serr := d.parseAndValidate(context.Background(), inv)
	if serr == nil {
		t.Fatal("expected *StructuredError for missing required arg, got nil")
	}
	if serr.ErrCode != ErrCodeInvalidArgs {
		t.Errorf("expected ErrCode=%q, got %q", ErrCodeInvalidArgs, serr.ErrCode)
	}
	missing, ok := serr.Detail["missing"]
	if !ok {
		t.Fatalf("expected detail key 'missing', got detail=%v", serr.Detail)
	}
	missingSlice, ok := missing.([]string)
	if !ok {
		t.Fatalf("detail['missing'] must be []string, got %T", missing)
	}
	if !containsStr(missingSlice, "foo") {
		t.Errorf("expected 'foo' in missing, got %v", missingSlice)
	}
}

// TestParseAndValidateUnknownArgs verifies that passing an arg not declared in
// params_required ∪ params_optional returns INVALID_ARGS with
// detail["unknown"] populated. Spec line 839.
func TestParseAndValidateUnknownArgs(t *testing.T) {
	d := newTestDispatcher()
	// "test.op.required" only allows "foo" and "bar"; "baz" is unknown.
	inv := &Invocation{
		OpID: "test.op.required",
		Args: map[string]any{"foo": "hello", "baz": 99},
	}

	_, serr := d.parseAndValidate(context.Background(), inv)
	if serr == nil {
		t.Fatal("expected *StructuredError for unknown arg, got nil")
	}
	if serr.ErrCode != ErrCodeInvalidArgs {
		t.Errorf("expected ErrCode=%q, got %q", ErrCodeInvalidArgs, serr.ErrCode)
	}
	unknown, ok := serr.Detail["unknown"]
	if !ok {
		t.Fatalf("expected detail key 'unknown', got detail=%v", serr.Detail)
	}
	unknownSlice, ok := unknown.([]string)
	if !ok {
		t.Fatalf("detail['unknown'] must be []string, got %T", unknown)
	}
	if !containsStr(unknownSlice, "baz") {
		t.Errorf("expected 'baz' in unknown, got %v", unknownSlice)
	}
}

// TestParseAndValidateTypeErrors verifies that passing an arg whose runtime type
// does not match the declared catalog type returns INVALID_ARGS with
// detail["type_errors"] populated with a message mentioning the field name and
// the expected type. Spec line 838 / line 842.
func TestParseAndValidateTypeErrors(t *testing.T) {
	d := newTestDispatcher()
	// "foo" is declared as type "string"; pass an integer instead.
	inv := &Invocation{
		OpID: "test.op.required",
		Args: map[string]any{"foo": 42},
	}

	_, serr := d.parseAndValidate(context.Background(), inv)
	if serr == nil {
		t.Fatal("expected *StructuredError for type mismatch, got nil")
	}
	if serr.ErrCode != ErrCodeInvalidArgs {
		t.Errorf("expected ErrCode=%q, got %q", ErrCodeInvalidArgs, serr.ErrCode)
	}
	typeErrs, ok := serr.Detail["type_errors"]
	if !ok {
		t.Fatalf("expected detail key 'type_errors', got detail=%v", serr.Detail)
	}
	typeErrsSlice, ok := typeErrs.([]string)
	if !ok {
		t.Fatalf("detail['type_errors'] must be []string, got %T", typeErrs)
	}
	if len(typeErrsSlice) == 0 {
		t.Fatal("detail['type_errors'] must be non-empty")
	}
	// At least one entry must mention "foo" and "string" (the expected type).
	found := false
	for _, msg := range typeErrsSlice {
		if strings.Contains(msg, "foo") && strings.Contains(msg, "string") {
			found = true
		}
	}
	if !found {
		t.Errorf("no type_errors entry mentions 'foo' and 'string'; got: %v", typeErrsSlice)
	}
}

// TestParseAndValidateValidArgsReturnsParsedInvocation verifies that a valid
// invocation returns a non-nil *parsedInvocation and a nil *StructuredError.
// Spec §3.1 step 1: parse succeeds → proceed.
func TestParseAndValidateValidArgsReturnsParsedInvocation(t *testing.T) {
	d := newTestDispatcher()
	inv := &Invocation{
		OpID: "test.op.noargs",
		Args: map[string]any{},
	}

	parsed, serr := d.parseAndValidate(context.Background(), inv)
	if serr != nil {
		t.Fatalf("expected nil error for valid invocation, got: %v", serr)
	}
	if parsed == nil {
		t.Fatal("expected non-nil *parsedInvocation for valid invocation")
	}
	if parsed.OpID != "test.op.noargs" {
		t.Errorf("parsedInvocation.OpID = %q, want %q", parsed.OpID, "test.op.noargs")
	}
}

// TestParseAndValidateNormalizesAlias verifies that invoking with a deprecated
// alias resolves to the canonical op_id in parsedInvocation.OpID.
// Spec §3.1 step 2: "normalize aliases, resolve op_id".
func TestParseAndValidateNormalizesAlias(t *testing.T) {
	d := newTestDispatcher()
	// Invoke with the alias; expect canonical OpID in result.
	inv := &Invocation{
		OpID: "test.alias",
		Args: map[string]any{},
	}

	parsed, serr := d.parseAndValidate(context.Background(), inv)
	if serr != nil {
		t.Fatalf("expected nil error for alias invocation, got: %v", serr)
	}
	if parsed == nil {
		t.Fatal("expected non-nil *parsedInvocation for alias invocation")
	}
	if parsed.OpID != "test.op.alias" {
		t.Errorf("alias not resolved: parsedInvocation.OpID = %q, want canonical %q", parsed.OpID, "test.op.alias")
	}
}

// TestParseAndValidateNilArgs verifies that a nil Args map is treated as empty
// and does not error when all params are optional. Spec §3.1 step 1 implicit:
// nil args ≡ {} for ops with no required params.
func TestParseAndValidateNilArgs(t *testing.T) {
	d := newTestDispatcher()
	inv := &Invocation{
		OpID: "test.op.noargs",
		Args: nil,
	}

	parsed, serr := d.parseAndValidate(context.Background(), inv)
	if serr != nil {
		t.Fatalf("nil Args should be treated as {}: got error %v", serr)
	}
	if parsed == nil {
		t.Fatal("expected non-nil *parsedInvocation when nil Args is normalised")
	}
}

// TestParseAndValidateArgsHashStable verifies that two invocations with the same
// logical args (same keys+values) produce identical ArgsHash regardless of
// Go map iteration order. This validates JCS-canonical hashing (spec §3.1
// step 1; required for cache key and confirmation token binding).
func TestParseAndValidateArgsHashStable(t *testing.T) {
	d := newTestDispatcher()

	// We can't force different insertion orders in Go map literals, but we can
	// call parseAndValidate twice and verify idempotency.
	args1 := map[string]any{"foo": "alpha"}
	args2 := map[string]any{"foo": "alpha"}

	inv1 := &Invocation{OpID: "test.op.required", Args: args1}
	inv2 := &Invocation{OpID: "test.op.required", Args: args2}

	// Note: these invocations have the required "foo" arg present and correctly typed.
	p1, serr1 := d.parseAndValidate(context.Background(), inv1)
	p2, serr2 := d.parseAndValidate(context.Background(), inv2)

	if serr1 != nil {
		t.Fatalf("inv1 unexpected error: %v", serr1)
	}
	if serr2 != nil {
		t.Fatalf("inv2 unexpected error: %v", serr2)
	}
	if p1.ArgsHash == "" {
		t.Fatal("parsedInvocation.ArgsHash must not be empty")
	}
	if p1.ArgsHash != p2.ArgsHash {
		t.Errorf("ArgsHash must be stable: got %q vs %q", p1.ArgsHash, p2.ArgsHash)
	}
}

// TestParseAndValidateAllErrorsAggregated verifies that validation returns ALL
// errors (missing + unknown + type_errors) in a single envelope rather than
// short-circuiting on the first failure.
// Spec line 842: all three keys are returned in one envelope.
func TestParseAndValidateAllErrorsAggregated(t *testing.T) {
	d := newTestDispatcher()
	// "foo" is required (missing), "baz" is unknown, "bar" is integer but we pass a string.
	inv := &Invocation{
		OpID: "test.op.required",
		Args: map[string]any{
			"baz": "unknown_key_value", // unknown
			"bar": "not_an_integer",    // type error (bar is integer)
			// "foo" is absent → missing
		},
	}

	_, serr := d.parseAndValidate(context.Background(), inv)
	if serr == nil {
		t.Fatal("expected *StructuredError with aggregated errors, got nil")
	}
	if serr.ErrCode != ErrCodeInvalidArgs {
		t.Errorf("expected ErrCode=%q, got %q", ErrCodeInvalidArgs, serr.ErrCode)
	}

	// All three keys must be present and non-empty.
	for _, key := range []string{"missing", "unknown", "type_errors"} {
		val, ok := serr.Detail[key]
		if !ok {
			t.Errorf("expected detail key %q, got detail=%v", key, serr.Detail)
			continue
		}
		slice, ok := val.([]string)
		if !ok {
			t.Errorf("detail[%q] must be []string, got %T", key, val)
			continue
		}
		if len(slice) == 0 {
			t.Errorf("detail[%q] must be non-empty (all errors must be aggregated)", key)
		}
	}
}

// ── test utilities ────────────────────────────────────────────────────────────

func containsStr(ss []string, target string) bool {
	for _, s := range ss {
		if s == target {
			return true
		}
	}
	return false
}
