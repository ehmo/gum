package adapters

// Result-envelope compression gate for gum_parallel (spec §9.0.1, bead
// gum-3dhe, docs/test-matrix.md row "gum_parallel result envelope
// compression").
//
// The row names five fixtures this file supplies:
//
//	(a) a heterogeneous batch where no field is identical across results,
//	(b) an all-null field that hoists with the value null,
//	(c) a field that is null in one result and absent in another, which
//	    rule 1 forbids hoisting,
//	(d) the outer _expression.variant_id being null per §12.3, and
//	(e) intentional_zero_max_items:true paired with a byte-identical
//	    non-null on_empty_message, both hoisted, with the §13 invariant
//	    holding on every reconstructed effective ExpressionMeta.
//
// Schemas below are inlined verbatim from docs/spec.md §13 so the surface
// under test is readable here; re-extract them when the spec changes.

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/dispatch"
)

// expressionMetaSpecDef is the §13 ExpressionMeta definition. Every
// reconstructed effective object must validate against it.
const expressionMetaSpecDef = `"ExpressionMeta": {
  "type": "object",
  "required": ["profile", "op_id", "variant_id", "lossy", "result_count"],
  "properties": {
    "profile":      {"type": "string"},
    "op_id":        {"type": "string"},
    "variant_id":   {"type": ["string", "null"]},
    "lossy":        {"type": "boolean"},
    "result_count": {"type": "integer", "minimum": 0},
    "omitted_count":{"type": "integer", "minimum": 0},
    "on_empty_message": {"type": ["string", "null"]},
    "full_result_path": {"type": "string"},
    "full_result_resource": {"type": "string"},
    "project_root_uri": {"type": ["string", "null"]},
    "_profile_resolution_warning": {"type": ["string", "null"]},
    "artifact_expires_at": {"type": ["string", "null"]},
    "intentional_zero_max_items": {"type": ["boolean", "null"]},
    "_code_output_truncated": {"type": ["boolean", "null"]}
  },
  "additionalProperties": true
}`

// expressionMetaDeltaSpecDef is the §13 ExpressionMetaDelta definition: the
// same field set with nothing required, because any field may have hoisted.
const expressionMetaDeltaSpecDef = `"ExpressionMetaDelta": {
  "type": "object",
  "properties": {
    "profile": {"type": "string"},
    "op_id": {"type": "string"},
    "variant_id": {"type": ["string", "null"]},
    "lossy": {"type": "boolean"},
    "result_count": {"type": "integer", "minimum": 0},
    "omitted_count": {"type": "integer", "minimum": 0},
    "on_empty_message": {"type": ["string", "null"]},
    "full_result_path": {"type": "string"},
    "full_result_resource": {"type": "string"},
    "artifact_expires_at": {"type": ["string", "null"]},
    "intentional_zero_max_items": {"type": ["boolean", "null"]}
  },
  "additionalProperties": true
}`

// parallelResultItemSpecDef is the §13 ParallelResultItem definition,
// including the success/error XOR.
const parallelResultItemSpecDef = `"ParallelResultItem": {
  "type": "object",
  "required": ["_idx", "_expression"],
  "properties": {
    "_idx": {"type": "integer", "minimum": 0},
    "_expression": {"$ref": "#/$defs/ExpressionMetaDelta"},
    "format": {"enum": ["toon", "json", "markdown", "csv"]},
    "toon": {"type": "string"},
    "data": {},
    "_code_output_truncated": {"type": "boolean"},
    "error": {
      "type": "object",
      "properties": {
        "error_code": {"type": "string"},
        "user_message": {"type": "string"},
        "cancelled": {"type": "boolean"}
      },
      "required": ["error_code"],
      "allOf": [
        {
          "if": {"properties": {"cancelled": {"const": true}}, "required": ["cancelled"]},
          "then": {"properties": {"error_code": {"const": "CANCELLED"}}}
        }
      ],
      "additionalProperties": true
    }
  },
  "oneOf": [
    {
      "required": ["format"],
      "anyOf": [{"required": ["toon"]}, {"required": ["data"]}],
      "not": {"required": ["error"]}
    },
    {
      "required": ["error"],
      "not": {"anyOf": [{"required": ["format"]}, {"required": ["toon"]}, {"required": ["data"]}]}
    }
  ],
  "additionalProperties": true
}`

// parallelResultsSpecSchema is the §13 ParallelResults envelope schema.
const parallelResultsSpecSchema = `{
  "$defs": {` + expressionMetaSpecDef + `,` + expressionMetaDeltaSpecDef + `,` + parallelResultItemSpecDef + `},
  "type": "object",
  "required": ["format", "batch_id", "results", "_expression"],
  "properties": {
    "format": {"const": "parallel_results"},
    "batch_id": {"type": "string", "pattern": "^[0-9a-f]{8}$"},
    "shared_expression_fields": {"type": "object", "additionalProperties": true},
    "results": {"type": "array", "items": {"$ref": "#/$defs/ParallelResultItem"}},
    "_code_output_truncated": {"type": "boolean"},
    "_expression": {"$ref": "#/$defs/ExpressionMeta"}
  },
  "additionalProperties": false
}`

// expressionMetaOnlySchema validates one reconstructed effective object.
const expressionMetaOnlySchema = `{
  "$defs": {` + expressionMetaSpecDef + `},
  "$ref": "#/$defs/ExpressionMeta"
}`

func strPtr(s string) *string { return &s }

func boolPtr(b bool) *bool { return &b }

// compileParallelSchema resolves one of the inlined §13 schemas.
func compileParallelSchema(t *testing.T, src string) *jsonschema.Resolved {
	t.Helper()
	var s jsonschema.Schema
	if err := json.Unmarshal([]byte(src), &s); err != nil {
		t.Fatalf("schema parse: %v", err)
	}
	r, err := s.Resolve(nil)
	if err != nil {
		t.Fatalf("schema resolve: %v", err)
	}
	return r
}

// wireForm round-trips a Go map through JSON so schema validation sees the
// same value a client receives, not the in-process Go types.
func wireForm(t *testing.T, v any) any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}

// runCompressionBatch dispatches len(metas) elements, giving element i the
// ExpressionMeta at metas[i], and returns the assembled envelope plus the
// dispatcher that recorded it for the ledger.
func runCompressionBatch(t *testing.T, opIDs []string, metas []*dispatch.ExpressionMeta) (map[string]any, *recordingBatchDispatcher) {
	t.Helper()
	if len(opIDs) != len(metas) {
		t.Fatalf("opIDs (%d) and metas (%d) must be the same length", len(opIDs), len(metas))
	}
	disp := &recordingBatchDispatcher{fn: func(_ context.Context, inv *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
		return &dispatch.ShapedResponse{
			Format:            "json",
			StructuredContent: map[string]any{"idx": inv.BatchIndex},
			Expression:        metas[inv.BatchIndex],
		}, nil
	}}
	elements := make([]parallelElement, len(opIDs))
	for i, op := range opIDs {
		elements[i] = parallelElement{OpID: op, Args: map[string]any{}}
	}
	return runParallelBatch(context.Background(), disp, elements, false, false, parallelBudget{}), disp
}

// sharedPool returns the envelope's shared_expression_fields and whether the
// key was emitted at all. Rule 4 makes absence meaningful, so the caller
// needs both.
func sharedPool(t *testing.T, env map[string]any) (map[string]any, bool) {
	t.Helper()
	raw, present := env["shared_expression_fields"]
	if !present {
		return nil, false
	}
	pool, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("shared_expression_fields is %T; want map", raw)
	}
	return pool, true
}

// resultDelta returns result i's _expression delta.
func resultDelta(t *testing.T, env map[string]any, i int) map[string]any {
	t.Helper()
	results, ok := env["results"].([]any)
	if !ok {
		t.Fatalf("results is %T; want []any", env["results"])
	}
	item, ok := results[i].(map[string]any)
	if !ok {
		t.Fatalf("results[%d] is %T; want map", i, results[i])
	}
	delta, ok := item["_expression"].(map[string]any)
	if !ok {
		t.Fatalf("results[%d]._expression is %T; want map", i, item["_expression"])
	}
	return delta
}

// reconstruct applies §9.0.1 rule 3: effective = shared ∪ delta, delta wins.
func reconstruct(shared, delta map[string]any) map[string]any {
	out := make(map[string]any, len(shared)+len(delta))
	for k, v := range shared {
		out[k] = v
	}
	for k, v := range delta {
		out[k] = v
	}
	return out
}

// assertEnvelopeValid checks the whole envelope against ParallelResults and
// every reconstructed effective object against the full ExpressionMeta.
func assertEnvelopeValid(t *testing.T, env map[string]any) []map[string]any {
	t.Helper()
	if err := compileParallelSchema(t, parallelResultsSpecSchema).Validate(wireForm(t, env)); err != nil {
		raw, _ := json.MarshalIndent(env, "", "  ")
		t.Fatalf("envelope fails ParallelResults schema: %v\nenvelope:\n%s", err, raw)
	}

	shared, _ := sharedPool(t, env)
	results, _ := env["results"].([]any)
	metaSchema := compileParallelSchema(t, expressionMetaOnlySchema)
	effective := make([]map[string]any, len(results))
	for i := range results {
		eff := reconstruct(shared, resultDelta(t, env, i))
		if err := metaSchema.Validate(wireForm(t, eff)); err != nil {
			raw, _ := json.MarshalIndent(eff, "", "  ")
			t.Errorf("result %d effective ExpressionMeta fails §13 schema: %v\neffective:\n%s", i, err, raw)
		}
		effective[i] = eff
	}
	return effective
}

// TestGumParallelResultEnvelopeCompression is the docs/test-matrix.md proof
// artifact for §9.0.1. Spec §9.0.1 requires goleak as the first deferred
// call, because the batch runs a worker pool.
func TestGumParallelResultEnvelopeCompression(t *testing.T) {
	defer goleak.VerifyNone(t)

	t.Run("identical fields hoist and reconstruct byte-identically", func(t *testing.T) {
		metas := []*dispatch.ExpressionMeta{
			{Profile: "gmail.messages.list.v1", OpID: "gmail.users.messages.list", VariantID: strPtr("gmail.v1.rest.list"), Lossy: true, ResultCount: 3},
			{Profile: "gmail.messages.list.v1", OpID: "gmail.users.messages.list", VariantID: strPtr("gmail.v1.rest.list"), Lossy: true, ResultCount: 0, OnEmptyMessage: strPtr("No messages matched the filter.")},
		}
		want := []map[string]any{metas[0].Fields(), metas[1].Fields()}

		env, _ := runCompressionBatch(t, []string{"gmail.users.messages.list", "gmail.users.messages.list"}, metas)
		effective := assertEnvelopeValid(t, env)

		shared, present := sharedPool(t, env)
		if !present {
			t.Fatalf("no shared_expression_fields; want profile/op_id/variant_id/lossy hoisted")
		}
		for _, key := range []string{"profile", "op_id", "variant_id", "lossy", "omitted_count"} {
			if _, ok := shared[key]; !ok {
				t.Errorf("shared pool missing %q: %v", key, shared)
			}
		}
		if _, ok := shared["result_count"]; ok {
			t.Errorf("result_count hoisted despite differing (3 vs 0): %v", shared)
		}
		for i := range effective {
			if !reflect.DeepEqual(effective[i], want[i]) {
				t.Errorf("result %d round-trip mismatch:\ngot:  %v\nwant: %v", i, effective[i], want[i])
			}
		}
	})

	t.Run("(a) heterogeneous batch omits shared_expression_fields", func(t *testing.T) {
		metas := []*dispatch.ExpressionMeta{
			{Profile: "p0", OpID: "op.a", VariantID: strPtr("v0"), Lossy: false, ResultCount: 1, OmittedCount: 0},
			{Profile: "p1", OpID: "op.b", VariantID: strPtr("v1"), Lossy: true, ResultCount: 2, OmittedCount: 3, OnEmptyMessage: strPtr("empty")},
		}
		want := []map[string]any{metas[0].Fields(), metas[1].Fields()}

		env, _ := runCompressionBatch(t, []string{"op.a", "op.b"}, metas)
		effective := assertEnvelopeValid(t, env)

		if _, present := sharedPool(t, env); present {
			t.Errorf("shared_expression_fields emitted for a batch with no identical field: %v", env["shared_expression_fields"])
		}
		for i := range effective {
			if !reflect.DeepEqual(effective[i], want[i]) {
				t.Errorf("result %d does not match the un-hoisted form:\ngot:  %v\nwant: %v", i, effective[i], want[i])
			}
		}
	})

	t.Run("(b) an all-null field hoists with value null", func(t *testing.T) {
		metas := []*dispatch.ExpressionMeta{
			{Profile: "p", OpID: "op.a", ResultCount: 1},
			{Profile: "p", OpID: "op.a", ResultCount: 2},
		}
		env, _ := runCompressionBatch(t, []string{"op.a", "op.a"}, metas)
		assertEnvelopeValid(t, env)

		shared, present := sharedPool(t, env)
		if !present {
			t.Fatalf("no shared_expression_fields; want variant_id and on_empty_message hoisted as null")
		}
		for _, key := range []string{"variant_id", "on_empty_message"} {
			v, ok := shared[key]
			if !ok {
				t.Errorf("shared pool missing %q; an all-null field must hoist (rule 1)", key)
				continue
			}
			if v != nil {
				t.Errorf("shared[%q] = %v; want null", key, v)
			}
		}
	})

	t.Run("(c) null in one result and absent in another does not hoist", func(t *testing.T) {
		// The runtime projection always emits the nullable fields, so this
		// rule is exercised against the hoist function directly.
		results := []map[string]any{
			{"_expression": map[string]any{"op_id": "op.a", "artifact_expires_at": nil}},
			{"_expression": map[string]any{"op_id": "op.a"}},
		}
		shared := hoistSharedExpressionFields(results)
		if _, ok := shared["artifact_expires_at"]; ok {
			t.Errorf("artifact_expires_at hoisted although one result omits it: %v", shared)
		}
		if shared["op_id"] != "op.a" {
			t.Errorf("shared[op_id] = %v; want op.a (the identical field still hoists)", shared["op_id"])
		}
	})

	t.Run("(d) the outer _expression.variant_id is null", func(t *testing.T) {
		metas := []*dispatch.ExpressionMeta{
			{Profile: "p", OpID: "op.a", VariantID: strPtr("v0"), ResultCount: 1},
			{Profile: "p", OpID: "op.b", VariantID: strPtr("v1"), ResultCount: 1},
		}
		env, _ := runCompressionBatch(t, []string{"op.a", "op.b"}, metas)
		assertEnvelopeValid(t, env)

		outer, ok := env["_expression"].(map[string]any)
		if !ok {
			t.Fatalf("outer _expression is %T; want map", env["_expression"])
		}
		if outer["op_id"] != "gum_parallel" {
			t.Errorf("outer op_id = %v; want gum_parallel", outer["op_id"])
		}
		v, present := outer["variant_id"]
		if !present {
			t.Fatalf("outer _expression omits variant_id; §13 requires the key")
		}
		if v != nil {
			t.Errorf("outer variant_id = %v; want null (§12.3)", v)
		}
	})

	t.Run("(e) intentional_zero_max_items hoists with its on_empty_message", func(t *testing.T) {
		const onEmpty = "Every row was collapsed by max_items=0."
		metas := []*dispatch.ExpressionMeta{
			{Profile: "collapse.all.v1", OpID: "op.a", VariantID: strPtr("v0"), ResultCount: 0, OnEmptyMessage: strPtr(onEmpty), IntentionalZeroMaxItems: boolPtr(true)},
			{Profile: "collapse.all.v1", OpID: "op.a", VariantID: strPtr("v0"), ResultCount: 0, OnEmptyMessage: strPtr(onEmpty), IntentionalZeroMaxItems: boolPtr(true)},
			{Profile: "collapse.all.v1", OpID: "op.a", VariantID: strPtr("v0"), ResultCount: 0, OnEmptyMessage: strPtr(onEmpty), IntentionalZeroMaxItems: boolPtr(true)},
		}
		env, _ := runCompressionBatch(t, []string{"op.a", "op.a", "op.a"}, metas)
		effective := assertEnvelopeValid(t, env)

		shared, present := sharedPool(t, env)
		if !present {
			t.Fatalf("no shared_expression_fields; want both §13 invariant fields hoisted")
		}
		if shared["intentional_zero_max_items"] != true {
			t.Errorf("shared[intentional_zero_max_items] = %v; want true", shared["intentional_zero_max_items"])
		}
		if shared["on_empty_message"] != onEmpty {
			t.Errorf("shared[on_empty_message] = %v; want %q", shared["on_empty_message"], onEmpty)
		}
		for i, delta := range []map[string]any{resultDelta(t, env, 0), resultDelta(t, env, 1), resultDelta(t, env, 2)} {
			for _, key := range []string{"intentional_zero_max_items", "on_empty_message"} {
				if _, ok := delta[key]; ok {
					t.Errorf("result %d delta still carries hoisted %q: %v", i, key, delta)
				}
			}
		}
		// §13 invariant on every reconstructed effective object.
		for i, eff := range effective {
			if eff["intentional_zero_max_items"] != true {
				t.Errorf("result %d effective intentional_zero_max_items = %v; want true", i, eff["intentional_zero_max_items"])
			}
			if eff["on_empty_message"] == nil {
				t.Errorf("result %d violates the §13 invariant: intentional_zero_max_items=true with a null on_empty_message", i)
			}
		}
	})

	t.Run("each emitted delta validates against ExpressionMetaDelta", func(t *testing.T) {
		metas := []*dispatch.ExpressionMeta{
			{Profile: "p", OpID: "op.a", VariantID: strPtr("v"), ResultCount: 1},
			{Profile: "p", OpID: "op.a", VariantID: strPtr("v"), ResultCount: 2},
		}
		env, _ := runCompressionBatch(t, []string{"op.a", "op.a"}, metas)
		deltaSchema := compileParallelSchema(t, `{"$defs": {`+expressionMetaDeltaSpecDef+`}, "$ref": "#/$defs/ExpressionMetaDelta"}`)
		for i := 0; i < 2; i++ {
			if err := deltaSchema.Validate(wireForm(t, resultDelta(t, env, i))); err != nil {
				t.Errorf("result %d delta fails ExpressionMetaDelta: %v", i, err)
			}
		}
	})

	t.Run("hoisting shrinks the envelope the gain ledger prices", func(t *testing.T) {
		metas := []*dispatch.ExpressionMeta{
			{Profile: "gmail.messages.list.v1", OpID: "op.a", VariantID: strPtr("gmail.v1.rest.list"), Lossy: true, ResultCount: 1},
			{Profile: "gmail.messages.list.v1", OpID: "op.a", VariantID: strPtr("gmail.v1.rest.list"), Lossy: true, ResultCount: 2},
		}
		env, disp := runCompressionBatch(t, []string{"op.a", "op.a"}, metas)
		batch := oneBatch(t, disp)

		// internal/dispatch/gain.go prices the outer entry by marshalling
		// this exact envelope, so fewer bytes here is the ledger gain.
		hoisted, err := json.Marshal(batch.Envelope)
		if err != nil {
			t.Fatalf("marshal recorded envelope: %v", err)
		}

		shared, present := sharedPool(t, env)
		if !present {
			t.Fatalf("no shared_expression_fields; nothing was hoisted")
		}
		unhoisted := map[string]any{}
		for k, v := range env {
			unhoisted[k] = v
		}
		delete(unhoisted, "shared_expression_fields")
		items := env["results"].([]any)
		restored := make([]any, len(items))
		for i := range items {
			item := map[string]any{}
			for k, v := range items[i].(map[string]any) {
				item[k] = v
			}
			item["_expression"] = reconstruct(shared, resultDelta(t, env, i))
			restored[i] = item
		}
		unhoisted["results"] = restored
		plain, err := json.Marshal(unhoisted)
		if err != nil {
			t.Fatalf("marshal un-hoisted envelope: %v", err)
		}
		if len(hoisted) >= len(plain) {
			t.Errorf("hoisted envelope is %d bytes, un-hoisted is %d; the hoist must reduce what the ledger prices", len(hoisted), len(plain))
		}
	})
}
