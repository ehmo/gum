package adapters

// Spec §9.0.1 "Aggregate batch output ceiling" (docs/test-matrix.md).
//
// Two ceilings bound a gum_parallel batch run from inside gum.code. The
// pre-dispatch one refuses a batch whose declared input already exceeds
// code.output_limit_bytes x concurrency. The element-level one cuts result
// elements at a UTF-8 boundary once the enclosing script's §6.1 cumulative
// budget runs out, and flags every element that lost bytes plus the outer
// envelope.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"unicode/utf8"

	"github.com/ehmo/gum/internal/dispatch"
)

// ceilingPayloadBytes is the toon payload each mock element returns. Large
// enough that a cut is unmistakable, small enough to keep the fixtures cheap.
const ceilingPayloadBytes = 2000

// toonMock returns a dispatcher whose every element answers with an n-byte
// ASCII toon payload.
func toonMock(n int) *whiteboxMockDispatcher {
	body := strings.Repeat("x", n)
	return &whiteboxMockDispatcher{fn: func(context.Context, *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
		return &dispatch.ShapedResponse{Format: "toon", Body: []byte(body)}, nil
	}}
}

// batchOf builds n identical elements naming opID.
func batchOf(n int, opID string) []parallelElement {
	out := make([]parallelElement, n)
	for i := range out {
		out[i] = parallelElement{OpID: opID, Args: map[string]any{}}
	}
	return out
}

// envelopeBytes is the encoded size of an assembled batch envelope.
func envelopeBytes(t *testing.T, env map[string]any) int {
	t.Helper()

	b, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	return len(b)
}

// fixedBudget binds a constant remainder, the way a live sandbox binds its
// own counter.
func fixedBudget(remaining, limitBytes, concurrency int) parallelBudget {
	return parallelBudget{
		remaining:   func() (int, bool) { return remaining, true },
		limitBytes:  limitBytes,
		concurrency: concurrency,
	}
}

// itemTruncated reports the per-element §13 marker.
func itemTruncated(item map[string]any) bool {
	v, _ := item[codeOutputTruncatedKey].(bool)
	return v
}

func TestGumParallelCodeOutputCeiling(t *testing.T) {
	t.Run("a batch under the ceiling dispatches", func(t *testing.T) {
		elements := batchOf(4, "op.x")
		err := checkBatchCeiling(elements, fixedBudget(0, 4096, parallelMaxWorkers))
		if err != nil {
			t.Fatalf("checkBatchCeiling refused a 4-element batch: %v", err)
		}
	})

	t.Run("an oversized batch is refused before dispatch", func(t *testing.T) {
		// One element carrying a 40000-byte argument overruns the 32768-byte
		// default ceiling on its own.
		elements := []parallelElement{{
			OpID: "op.x",
			Args: map[string]any{"q": strings.Repeat("a", 40000)},
		}}

		err := checkBatchCeiling(elements, fixedBudget(0, 4096, parallelMaxWorkers))
		if err == nil {
			t.Fatal("checkBatchCeiling accepted a 40000-byte batch against a 32768-byte ceiling")
		}
		var se *dispatch.StructuredError
		if !errors.As(err, &se) {
			t.Fatalf("err = %T %v; want *dispatch.StructuredError", err, err)
		}
		if se.ErrCode != dispatch.ErrCodeCodeOutputLimitExceeded {
			t.Fatalf("ErrCode = %q; want %q", se.ErrCode, dispatch.ErrCodeCodeOutputLimitExceeded)
		}
		if got := se.Detail["limit_bytes"]; got != 4096*parallelMaxWorkers {
			t.Errorf("limit_bytes = %v; want %d", got, 4096*parallelMaxWorkers)
		}
		requested, ok := se.Detail["requested_bytes"].(int)
		if !ok || requested < 40000 {
			t.Errorf("requested_bytes = %v; want the declared input size, at least 40000", se.Detail["requested_bytes"])
		}
	})

	t.Run("the refusal happens before the first dispatch", func(t *testing.T) {
		// Atomic: a regression that lets the batch through writes this from
		// every worker at once, and a data race would mask the real failure.
		var dispatched atomic.Bool
		mock := &whiteboxMockDispatcher{fn: func(context.Context, *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
			dispatched.Store(true)
			return &dispatch.ShapedResponse{Format: "toon", Body: []byte("ok")}, nil
		}}

		fn := buildParallelFn(context.Background(), mock, false, false,
			fixedBudget(4096, 4096, parallelMaxWorkers))
		_, err := fn([]any{map[string]any{"op": "op.x", "args": map[string]any{"q": strings.Repeat("a", 40000)}}})
		if err == nil {
			t.Fatal("gum_parallel accepted the oversized batch")
		}
		if dispatched.Load() {
			t.Fatal("the kernel was called; the §9.0.1 ceiling must refuse before any worker dispatches")
		}
	})

	t.Run("raising output_limit_bytes raises the ceiling", func(t *testing.T) {
		// The same batch that the 4096-byte default refuses is accepted once
		// code.output_limit_bytes doubles.
		elements := []parallelElement{{
			OpID: "op.x",
			Args: map[string]any{"q": strings.Repeat("a", 40000)},
		}}

		if err := checkBatchCeiling(elements, fixedBudget(0, 4096, parallelMaxWorkers)); err == nil {
			t.Fatal("the 32768-byte ceiling accepted a 40000-byte batch")
		}
		if err := checkBatchCeiling(elements, fixedBudget(0, 8192, parallelMaxWorkers)); err != nil {
			t.Fatalf("the 65536-byte ceiling refused a 40000-byte batch: %v", err)
		}
	})

	t.Run("no ceiling applies without a configured limit", func(t *testing.T) {
		elements := []parallelElement{{
			OpID: "op.x",
			Args: map[string]any{"q": strings.Repeat("a", 40000)},
		}}

		if err := checkBatchCeiling(elements, parallelBudget{}); err != nil {
			t.Fatalf("an unbound budget refused a batch: %v", err)
		}
	})

	t.Run("a batch that fits sets no flag", func(t *testing.T) {
		ctx := context.Background()
		mock := toonMock(20)
		env := runParallelBatch(ctx, mock, batchOf(3, "op.x"), false, false, parallelBudget{})
		size := envelopeBytes(t, env)

		env = runParallelBatch(ctx, mock, batchOf(3, "op.x"), false, false,
			fixedBudget(size+100, 4096, parallelMaxWorkers))
		if _, present := env[codeOutputTruncatedKey]; present {
			t.Errorf("outer %s set on a batch that fits the budget", codeOutputTruncatedKey)
		}
		for i, item := range env["results"].([]any) {
			if itemTruncated(item.(map[string]any)) {
				t.Errorf("element %d flagged truncated inside a sufficient budget", i)
			}
		}
	})

	t.Run("an exhausted budget cuts elements and flags them", func(t *testing.T) {
		ctx := context.Background()
		mock := toonMock(ceilingPayloadBytes)

		// Measure the untruncated batch, then re-run it against a budget
		// 3000 bytes short: element 0 is paid in full, element 1 gets what is
		// left, and element 2 gets nothing.
		full := envelopeBytes(t, runParallelBatch(ctx, mock, batchOf(3, "op.x"), false, false, parallelBudget{}))
		remaining := full - 3000

		env := runParallelBatch(ctx, mock, batchOf(3, "op.x"), false, false,
			fixedBudget(remaining, 4096, parallelMaxWorkers))

		if v, _ := env[codeOutputTruncatedKey].(bool); !v {
			t.Fatalf("outer %s = %v; the batch overran its budget", codeOutputTruncatedKey, env[codeOutputTruncatedKey])
		}
		items := env["results"].([]any)
		if len(items) != 3 {
			t.Fatalf("results length = %d; want 3", len(items))
		}

		first := items[0].(map[string]any)
		if itemTruncated(first) {
			t.Error("element 0 was cut; the budget covered it in full")
		}
		if got := first["toon"].(string); len(got) != ceilingPayloadBytes {
			t.Errorf("element 0 payload = %d bytes; want the full %d", len(got), ceilingPayloadBytes)
		}

		for _, idx := range []int{1, 2} {
			item := items[idx].(map[string]any)
			if !itemTruncated(item) {
				t.Errorf("element %d lost bytes but carries no per-element marker", idx)
			}
			if got := item["toon"].(string); len(got) >= ceilingPayloadBytes {
				t.Errorf("element %d payload = %d bytes; want less than %d", idx, len(got), ceilingPayloadBytes)
			}
			if _, ok := item["format"]; !ok {
				t.Errorf("element %d lost its format key; §13 requires it on a success element", idx)
			}
		}
		// Element 2 runs against an allowance the earlier elements already
		// spent, so it keeps its required keys and nothing else.
		if got := items[2].(map[string]any)["toon"].(string); got != "" {
			t.Errorf("element 2 payload = %d bytes; the budget was already spent", len(got))
		}
		if got := envelopeBytes(t, env); got >= full {
			t.Errorf("envelope = %d bytes; the cut must shrink it below the %d-byte original", got, full)
		}
	})

	t.Run("the cut lands on a UTF-8 boundary", func(t *testing.T) {
		ctx := context.Background()
		// A 2-byte rune cannot be split, so an odd allowance must walk back.
		mock := &whiteboxMockDispatcher{fn: func(context.Context, *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
			return &dispatch.ShapedResponse{Format: "toon", Body: []byte(strings.Repeat("é", 1000))}, nil
		}}

		full := envelopeBytes(t, runParallelBatch(ctx, mock, batchOf(1, "op.x"), false, false, parallelBudget{}))
		env := runParallelBatch(ctx, mock, batchOf(1, "op.x"), false, false,
			fixedBudget(full-777, 4096, parallelMaxWorkers))

		item := env["results"].([]any)[0].(map[string]any)
		if !itemTruncated(item) {
			t.Fatal("element 0 carries no truncation marker")
		}
		got := item["toon"].(string)
		if !utf8.ValidString(got) {
			t.Error("the cut payload is not valid UTF-8")
		}
		if len(got)%2 != 0 {
			t.Errorf("payload = %d bytes; a run of 2-byte runes can only end on an even boundary", len(got))
		}
		// One element has no separator cost and one payload to give, so the
		// cut envelope has to land inside the remainder exactly.
		if size, want := envelopeBytes(t, env), full-777; size > want {
			t.Errorf("envelope = %d bytes against a %d-byte remainder", size, want)
		}
	})

	t.Run("a failed element keeps its error_code", func(t *testing.T) {
		ctx := context.Background()
		mock := &whiteboxMockDispatcher{fn: func(context.Context, *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
			return nil, dispatch.NewStructuredError(dispatch.ErrCodeServiceDown, strings.Repeat("z", 3000))
		}}

		full := envelopeBytes(t, runParallelBatch(ctx, mock, batchOf(1, "op.x"), false, false, parallelBudget{}))
		env := runParallelBatch(ctx, mock, batchOf(1, "op.x"), false, false,
			fixedBudget(full-2000, 4096, parallelMaxWorkers))

		item := env["results"].([]any)[0].(map[string]any)
		if !itemTruncated(item) {
			t.Fatal("the failed element carries no truncation marker")
		}
		errObj := item["error"].(map[string]any)
		if errObj["error_code"] != string(dispatch.ErrCodeServiceDown) {
			t.Errorf("error_code = %v; §13 requires it to survive the cut", errObj["error_code"])
		}
		if got := errObj["message"].(string); len(got) >= 3000 {
			t.Errorf("message = %d bytes; want the cut one", len(got))
		}
		if _, present := item["format"]; present {
			t.Error("the cut added a payload key to a failed element, breaking the §13 XOR")
		}
	})

	t.Run("an unbound budget never truncates", func(t *testing.T) {
		ctx := context.Background()
		mock := toonMock(ceilingPayloadBytes)

		env := runParallelBatch(ctx, mock, batchOf(3, "op.x"), false, false, parallelBudget{})
		if _, present := env[codeOutputTruncatedKey]; present {
			t.Error("an unbound budget set the outer truncation flag")
		}
		for i, item := range env["results"].([]any) {
			if itemTruncated(item.(map[string]any)) {
				t.Errorf("element %d was cut against an unbound budget", i)
			}
		}
	})

	t.Run("a budget that reports nothing bound never truncates", func(t *testing.T) {
		ctx := context.Background()
		mock := toonMock(ceilingPayloadBytes)

		budget := parallelBudget{remaining: func() (int, bool) { return 0, false }}
		env := runParallelBatch(ctx, mock, batchOf(3, "op.x"), false, false, budget)
		if _, present := env[codeOutputTruncatedKey]; present {
			t.Error("an unbound Budget set the outer truncation flag")
		}
	})
}

// unmarshalable is a value json.Marshal always rejects. It stands in for an
// adapter payload that cannot be encoded at all.
func unmarshalable() any { return make(chan int) }

func TestParallelItemTruncationArms(t *testing.T) {
	t.Run("a string data payload is cut in place", func(t *testing.T) {
		item := map[string]any{
			"_idx":   0,
			"format": "json",
			"data":   strings.Repeat("d", 500),
		}

		cost, cut := truncateResultItem(item, 120)
		if !cut {
			t.Fatal("a 500-byte data string against a 120-byte allowance was not cut")
		}
		if cost > 120 {
			t.Errorf("element costs %d bytes against a 120-byte allowance", cost)
		}
		s, ok := item["data"].(string)
		if !ok {
			t.Fatalf("data = %T; a cut string payload stays a string", item["data"])
		}
		if len(s) >= 500 {
			t.Errorf("data kept %d of 500 bytes; it was not cut", len(s))
		}
		if !itemTruncated(item) {
			t.Error("a cut element did not set the §13 per-element flag")
		}
	})

	t.Run("a data tree is cut as its JSON text", func(t *testing.T) {
		item := map[string]any{
			"_idx":   0,
			"format": "json",
			"data":   map[string]any{"rows": strings.Repeat("r", 500)},
		}

		if _, cut := truncateResultItem(item, 120); !cut {
			t.Fatal("a 500-byte data tree against a 120-byte allowance was not cut")
		}
		s, ok := item["data"].(string)
		if !ok {
			t.Fatalf("data = %T; a cut tree is replaced by its truncated JSON text", item["data"])
		}
		if !strings.HasPrefix(s, `{"rows":"rrr`) {
			t.Errorf("data = %q; want the head of the tree's JSON text", s)
		}
	})

	t.Run("an element with nothing cuttable is left alone", func(t *testing.T) {
		// A cancelled element carries error_code and no free text, so §13
		// leaves it with no droppable bytes.
		item := map[string]any{
			"_idx":  0,
			"error": map[string]any{"error_code": "CANCELLED", "message": ""},
		}
		before, err := json.Marshal(item)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}

		cost, cut := truncateResultItem(item, 1)
		if cut {
			t.Error("an element with no cuttable field reported a cut")
		}
		if cost != len(before) {
			t.Errorf("cost = %d; want %d, the untouched element", cost, len(before))
		}
		if itemTruncated(item) {
			t.Error("an untouched element was flagged as truncated")
		}
	})

	t.Run("an empty payload loses nothing and is not flagged", func(t *testing.T) {
		// The element overruns its allowance on its required keys alone, so
		// the cut has nothing to take.
		item := map[string]any{
			"_idx":        0,
			"format":      "toon",
			"toon":        "",
			"_expression": map[string]any{"op_id": strings.Repeat("o", 200)},
		}

		_, cut := truncateResultItem(item, 10)
		if cut {
			t.Error("an element with an empty payload reported a cut")
		}
		if itemTruncated(item) {
			t.Error("an element that lost no bytes was flagged as truncated")
		}
	})

	t.Run("escaping overshoot forces a second cut", func(t *testing.T) {
		// Each control byte encodes as the 6-byte \u00xx escape, so the first
		// cut overshoots its allowance by far more than the room it took.
		item := map[string]any{
			"_idx":   0,
			"format": "toon",
			"toon":   strings.Repeat("\x01", 200),
		}

		cost, cut := truncateResultItem(item, 60)
		if !cut {
			t.Fatal("a 200-byte escaped payload against a 60-byte allowance was not cut")
		}
		if s := item["toon"].(string); len(s) != 0 {
			t.Errorf("toon kept %d bytes; the allowance leaves no room once escaped", len(s))
		}
		if cost <= 0 {
			t.Errorf("cost = %d; the element still carries its required keys", cost)
		}
	})

	t.Run("an unencodable payload leaves the element untouched", func(t *testing.T) {
		item := map[string]any{"_idx": 0, "data": unmarshalable()}

		cost, cut := truncateResultItem(item, 100)
		if cut || cost != 0 {
			t.Errorf("cost, cut = %d, %v; want 0, false for an element that cannot be encoded", cost, cut)
		}
	})

	t.Run("an unencodable sibling key leaves the element untouched", func(t *testing.T) {
		// The payload is cuttable, but the element as a whole never encodes,
		// so the cut must restore what it borrowed.
		item := map[string]any{
			"_idx":   unmarshalable(),
			"format": "toon",
			"toon":   strings.Repeat("t", 500),
		}

		cost, cut := truncateResultItem(item, 100)
		if cut || cost != 0 {
			t.Errorf("cost, cut = %d, %v; want 0, false for an element that cannot be encoded", cost, cut)
		}
		if got := item["toon"].(string); len(got) != 500 {
			t.Errorf("toon kept %d of 500 bytes; a failed cut must restore the payload", len(got))
		}
		if itemTruncated(item) {
			t.Error("a failed cut left the truncation flag behind")
		}
	})

	t.Run("an error element without a message has nothing to give", func(t *testing.T) {
		item := map[string]any{"_idx": 0, "error": map[string]any{"error_code": "X"}}

		field, text := itemPayloadText(item)
		if field != "" || text != "" {
			t.Errorf("itemPayloadText = %q, %q; want empty, error_code is not cuttable", field, text)
		}
	})
}
