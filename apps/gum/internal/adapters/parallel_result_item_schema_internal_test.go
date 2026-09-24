package adapters

// docs/test-matrix.md proof: the §13 `$defs/ParallelResultItem`
// schema is what validates `ParallelResults.results[]`, and a ToonResult
// schema is not a substitute for it.
//
// The shared §13 schema constants and helpers live in
// parallel_envelope_compression_internal_test.go.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ehmo/gum/internal/dispatch"
)

// toonResultSpecDef is the §13 ToonResult definition, inlined verbatim. It
// closes its property set, which is the structural reason a result item can
// never be validated against it.
const toonResultSpecDef = `"ToonResult": {
  "type": "object",
  "required": ["format", "toon", "_expression"],
  "properties": {
    "format": {"const": "toon"},
    "toon": {"type": "string"},
    "op": {"type": "string"},
    "variant": {"type": "string"},
    "next_page_token": {"type": "string"},
    "_expression": {"$ref": "#/$defs/ExpressionMeta"}
  },
  "additionalProperties": false
}`

// parallelResultItemOnlySchema validates a single results[] element.
const parallelResultItemOnlySchema = `{
  "$defs": {` + expressionMetaDeltaSpecDef + `,` + parallelResultItemSpecDef + `},
  "$ref": "#/$defs/ParallelResultItem"
}`

// toonResultOnlySchema validates a single element against ToonResult, the
// schema §13 tells consumers not to reuse here.
const toonResultOnlySchema = `{
  "$defs": {` + expressionMetaSpecDef + `,` + toonResultSpecDef + `},
  "$ref": "#/$defs/ToonResult"
}`

func TestParallelResultItemSchema(t *testing.T) {
	itemSchema := compileParallelSchema(t, parallelResultItemOnlySchema)
	items := mixedParallelItems(t)

	t.Run("every emitted item validates against the item schema", func(t *testing.T) {
		for i, item := range items {
			if err := itemSchema.Validate(wireForm(t, item)); err != nil {
				t.Errorf("results[%d] fails ParallelResultItem: %v\nitem: %v", i, err, item)
			}
		}
	})

	t.Run("items are not ToonResults", func(t *testing.T) {
		toonSchema := compileParallelSchema(t, toonResultOnlySchema)
		toonItem := itemWithKey(t, items, "toon")
		if err := toonSchema.Validate(wireForm(t, toonItem)); err == nil {
			t.Errorf("a toon-carrying result item validated as a ToonResult; "+
				"§13 forbids reusing that schema here\nitem: %v", toonItem)
		}
	})

	t.Run("_idx and _expression are required", func(t *testing.T) {
		for _, key := range []string{"_idx", "_expression"} {
			stripped := cloneItem(items[0])
			delete(stripped, key)
			if err := itemSchema.Validate(wireForm(t, stripped)); err == nil {
				t.Errorf("an item without %s validated; §13 requires it", key)
			}
		}
	})

	t.Run("_expression stays present when every field hoists", func(t *testing.T) {
		// A homogeneous batch hoists the whole delta into the shared pool.
		// The empty object must still be emitted: §13 requires the key.
		env := homogeneousParallelEnvelope(t)
		results, ok := env["results"].([]any)
		if !ok || len(results) != 2 {
			t.Fatalf("results = %v; want 2 elements", env["results"])
		}
		for i, raw := range results {
			item, ok := raw.(map[string]any)
			if !ok {
				t.Fatalf("results[%d] is %T; want an object", i, raw)
			}
			delta, ok := item["_expression"].(map[string]any)
			if !ok {
				t.Fatalf("results[%d]._expression = %v; want an object", i, item["_expression"])
			}
			if len(delta) != 0 {
				t.Errorf("results[%d]._expression = %v; want everything hoisted", i, delta)
			}
			if err := itemSchema.Validate(wireForm(t, item)); err != nil {
				t.Errorf("results[%d] with an empty delta fails the schema: %v", i, err)
			}
		}
	})

	t.Run("_code_output_truncated is an item-level boolean", func(t *testing.T) {
		flagged := cloneItem(items[0])
		flagged["_code_output_truncated"] = true
		if err := itemSchema.Validate(wireForm(t, flagged)); err != nil {
			t.Errorf("an item carrying _code_output_truncated:true fails the schema: %v", err)
		}

		wrongType := cloneItem(items[0])
		wrongType["_code_output_truncated"] = "yes"
		if err := itemSchema.Validate(wireForm(t, wrongType)); err == nil {
			t.Error("_code_output_truncated:\"yes\" validated; §13 types it boolean")
		}
	})

	t.Run("the flag never lands inside _expression", func(t *testing.T) {
		// §13 puts the signal on the enclosing item. ExpressionMeta.Fields
		// drops it for that reason, so a shaped response that set it cannot
		// leak it into the delta.
		truncated := true
		env := runSingleParallelElement(t, &dispatch.ExpressionMeta{
			Profile:             "default",
			OpID:                "gmail.messages.list",
			CodeOutputTruncated: &truncated,
		})
		results := env["results"].([]any)
		item := results[0].(map[string]any)
		delta, ok := item["_expression"].(map[string]any)
		if !ok {
			t.Fatalf("_expression = %v; want an object", item["_expression"])
		}
		if _, leaked := delta["_code_output_truncated"]; leaked {
			t.Errorf("_expression carries _code_output_truncated; §13 puts it on the item")
		}
		if shared, present := sharedPool(t, env); present {
			if _, leaked := shared["_code_output_truncated"]; leaked {
				t.Errorf("shared_expression_fields carries _code_output_truncated: %v", shared)
			}
		}
	})
}

// mixedParallelItems returns one result item per §13 payload shape: a toon
// success, a data success, a dispatch error, and a cancellation.
func mixedParallelItems(t *testing.T) []map[string]any {
	t.Helper()
	disp := &recordingBatchDispatcher{fn: func(_ context.Context, inv *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
		meta := &dispatch.ExpressionMeta{
			Profile:     "default",
			OpID:        inv.OpID,
			ResultCount: inv.BatchIndex,
		}
		switch inv.BatchIndex {
		case 0:
			return &dispatch.ShapedResponse{Format: "toon", Body: []byte("id,subject\n1,hi\n"), Expression: meta}, nil
		case 1:
			return &dispatch.ShapedResponse{
				Format:            "json",
				StructuredContent: map[string]any{"messages": []any{}},
				Expression:        meta,
			}, nil
		default:
			return nil, &dispatch.StructuredError{
				ErrCode:   dispatch.ErrCodeServiceDown,
				Message:   "upstream unavailable",
				Retryable: true,
			}
		}
	}}
	elements := []parallelElement{
		{OpID: "gmail.messages.list", Args: map[string]any{}},
		{OpID: "calendar.events.list", Args: map[string]any{}},
		{OpID: "drive.files.list", Args: map[string]any{}},
	}
	env := runParallelBatch(context.Background(), disp, elements, false, false, parallelBudget{})
	raw, ok := env["results"].([]any)
	if !ok || len(raw) != len(elements) {
		t.Fatalf("results = %v; want %d elements", env["results"], len(elements))
	}
	items := make([]map[string]any, 0, len(raw)+1)
	for i, r := range raw {
		item, ok := r.(map[string]any)
		if !ok {
			t.Fatalf("results[%d] is %T; want an object", i, r)
		}
		items = append(items, item)
	}
	// The cancellation arm never reaches a dispatcher, so it is built from
	// the same constructor the batch uses when the context is cancelled.
	return append(items, cancelledItem(len(elements), "drive.files.list"))
}

// homogeneousParallelEnvelope runs two elements whose deltas are identical,
// so hoisting empties every per-item _expression.
func homogeneousParallelEnvelope(t *testing.T) map[string]any {
	t.Helper()
	meta := &dispatch.ExpressionMeta{Profile: "default", OpID: "gmail.messages.list"}
	disp := &recordingBatchDispatcher{fn: func(_ context.Context, _ *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
		return &dispatch.ShapedResponse{
			Format:            "json",
			StructuredContent: map[string]any{"ok": true},
			Expression:        meta,
		}, nil
	}}
	elements := []parallelElement{
		{OpID: "gmail.messages.list", Args: map[string]any{}},
		{OpID: "gmail.messages.list", Args: map[string]any{}},
	}
	return runParallelBatch(context.Background(), disp, elements, false, false, parallelBudget{})
}

// runSingleParallelElement dispatches one element carrying meta.
func runSingleParallelElement(t *testing.T, meta *dispatch.ExpressionMeta) map[string]any {
	t.Helper()
	disp := &recordingBatchDispatcher{fn: func(_ context.Context, _ *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
		return &dispatch.ShapedResponse{
			Format:            "json",
			StructuredContent: map[string]any{"ok": true},
			Expression:        meta,
		}, nil
	}}
	return runParallelBatch(context.Background(), disp,
		[]parallelElement{{OpID: meta.OpID, Args: map[string]any{}}}, false, false, parallelBudget{})
}

// itemWithKey returns the first item carrying key, failing the test if none does.
func itemWithKey(t *testing.T, items []map[string]any, key string) map[string]any {
	t.Helper()
	for _, item := range items {
		if _, ok := item[key]; ok {
			return item
		}
	}
	t.Fatalf("no result item carries %q", key)
	return nil
}

// cloneItem deep-copies an item through JSON so a mutation cannot reach the
// shared fixture.
func cloneItem(item map[string]any) map[string]any {
	raw, err := json.Marshal(item)
	if err != nil {
		panic(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		panic(err)
	}
	return out
}
