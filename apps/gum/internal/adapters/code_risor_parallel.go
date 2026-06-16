package adapters

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ehmo/gum/internal/dispatch"
)

// parallelMaxWorkers is the bounded fan-out per spec §6.3 / §6.1.1.
const parallelMaxWorkers = 8

// parallelMaxElements caps the number of items in a single gum_parallel batch.
// Each element re-dispatches a full API call, so an unbounded list is an
// input-amplification resource-exhaustion vector.
const parallelMaxElements = 256

// parallel429DefaultRetryAfter is the fallback pause when an upstream 429
// response omits a Retry-After hint (spec §6.3 line 1171: "from the 429
// response header, or 60s if absent").
const parallel429DefaultRetryAfter = 60 * time.Second

// parallel429StaggerStep is the per-worker-index delay applied after a
// family-pause expires, to spread retries and avoid a thundering-herd
// re-attempt (spec §6.3 line 1171: "staggered by 50ms × worker_index").
const parallel429StaggerStep = 50 * time.Millisecond

// parallelElement is the normalised input shape for one element of the
// gum_parallel batch: an op_id, args, and optional variant_id.
type parallelElement struct {
	OpID      string
	Args      map[string]any
	VariantID string
}

// buildParallelFn returns the gum_parallel closure for one Risor execution.
// The closure captures the enclosing context so cancellation propagates to all
// in-flight workers (spec §6.3 lines 1007-1016).
func buildParallelFn(parentCtx context.Context, disp dispatch.Dispatcher, allowWrite, allowDestructive bool) func(...any) (any, error) {
	return func(args ...any) (any, error) {
		if disp == nil {
			return nil, dispatch.NewStructuredError(dispatch.ErrCodeInvalidArgs,
				"gum_parallel is not wired in this execution context (no dispatcher reference)")
		}
		if len(args) == 0 {
			return nil, dispatch.NewStructuredError(dispatch.ErrCodeInvalidArgs,
				"gum_parallel: expected a list of {op, args} entries")
		}
		elements, err := parseParallelInput(args[0])
		if err != nil {
			return nil, err
		}

		return runParallelBatch(parentCtx, disp, elements, allowWrite, allowDestructive), nil
	}
}

// parseParallelInput normalises Risor's []any input into []parallelElement.
// Each entry must be a map with at least an op_id key (alias: "op"). The
// optional "args" key is forwarded to dispatch.Invocation.Args; "variant_id"
// is forwarded for variant resolution.
func parseParallelInput(raw any) ([]parallelElement, error) {
	list, ok := raw.([]any)
	if !ok {
		return nil, dispatch.NewStructuredError(dispatch.ErrCodeInvalidArgs,
			fmt.Sprintf("gum_parallel: expected list, got %T", raw))
	}
	if len(list) > parallelMaxElements {
		return nil, dispatch.NewStructuredError(dispatch.ErrCodeInvalidArgs,
			fmt.Sprintf("gum_parallel: batch of %d exceeds the maximum of %d", len(list), parallelMaxElements))
	}
	out := make([]parallelElement, 0, len(list))
	for i, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, dispatch.NewStructuredError(dispatch.ErrCodeInvalidArgs,
				fmt.Sprintf("gum_parallel: element %d is not a map, got %T", i, item))
		}
		opID := stringField(m, "op_id")
		if opID == "" {
			opID = stringField(m, "op")
		}
		if opID == "" {
			return nil, dispatch.NewStructuredError(dispatch.ErrCodeInvalidArgs,
				fmt.Sprintf("gum_parallel: element %d missing 'op_id' (or 'op')", i))
		}
		el := parallelElement{
			OpID:      opID,
			VariantID: stringField(m, "variant_id"),
		}
		if a, ok := m["args"]; ok {
			am, ok := a.(map[string]any)
			if !ok {
				return nil, dispatch.NewStructuredError(dispatch.ErrCodeInvalidArgs,
					fmt.Sprintf("gum_parallel: element %d 'args' is not a map, got %T", i, a))
			}
			el.Args = am
		} else {
			el.Args = map[string]any{}
		}
		out = append(out, el)
	}
	return out, nil
}

// runParallelBatch fans out the elements to a bounded pool, collects results
// in input order, and assembles the §9.0.1 envelope with shared-field hoist.
// Workers honour per-service-family 429 isolation: a RATE_LIMITED result on a
// gmail op pauses other gmail-family workers for retry_after_ms but does NOT
// stall workers in other families (spec §6.3 line 1171).
func runParallelBatch(parentCtx context.Context, disp dispatch.Dispatcher, elements []parallelElement, allowWrite, allowDestructive bool) map[string]any {
	results := make([]map[string]any, len(elements))
	if len(elements) == 0 {
		return assembleParallelEnvelope(results)
	}

	nWorkers := parallelMaxWorkers
	if len(elements) < nWorkers {
		nWorkers = len(elements)
	}

	var familyOf func(opID string) string
	if r, ok := disp.(dispatch.ServiceFamilyResolver); ok {
		familyOf = r.ServiceFamily
	} else {
		familyOf = func(string) string { return "" }
	}
	gate := newFamilyGate()

	type job struct {
		idx int
		el  parallelElement
	}
	jobs := make(chan job)
	var wg sync.WaitGroup

	for w := 0; w < nWorkers; w++ {
		wg.Add(1)
		go func(workerIdx int) {
			defer wg.Done()
			for j := range jobs {
				family := familyOf(j.el.OpID)
				if gate.wait(parentCtx, family, workerIdx) {
					if parentCtx.Err() != nil {
						results[j.idx] = cancelledItem(j.idx, j.el.OpID)
						continue
					}
				}
				results[j.idx] = dispatchOne(parentCtx, disp, j.idx, j.el, allowWrite, allowDestructive)
				if family != "" {
					if pause := extractRateLimitedPause(results[j.idx]); pause > 0 {
						gate.pause(family, pause)
					}
				}
			}
		}(w)
	}

	go func() {
		defer close(jobs)
		for i, el := range elements {
			select {
			case <-parentCtx.Done():
				return
			case jobs <- job{idx: i, el: el}:
			}
		}
	}()

	wg.Wait()

	// Any element never received by a worker (cancelled before scheduling)
	// gets the canonical CANCELLED envelope.
	for i := range results {
		if results[i] == nil {
			results[i] = cancelledItem(i, elements[i].OpID)
		}
	}
	return assembleParallelEnvelope(results)
}

// familyGate tracks per-service-family pause windows for gum_parallel 429
// isolation (spec §6.3 line 1171). Workers consult the gate before each
// dispatch and block (with the parent context honoured) until any pause for
// their op's family expires. The gate is concurrent-safe.
type familyGate struct {
	mu          sync.Mutex
	pausedUntil map[string]time.Time
}

func newFamilyGate() *familyGate {
	return &familyGate{pausedUntil: map[string]time.Time{}}
}

// pause records that workers operating on `family` must wait `d` from now.
// Successive 429s within the same window extend, never shorten, the pause.
func (g *familyGate) pause(family string, d time.Duration) {
	if d <= 0 {
		d = parallel429DefaultRetryAfter
	}
	until := time.Now().Add(d)
	g.mu.Lock()
	defer g.mu.Unlock()
	if cur, ok := g.pausedUntil[family]; !ok || until.After(cur) {
		g.pausedUntil[family] = until
	}
}

// wait blocks until the current pause for `family` (if any) has elapsed, then
// applies a `workerIdx * 50ms` stagger to spread thundering-herd retries.
// Returns true iff a pause was honoured (so the caller can re-check ctx).
// An empty family ("" — op not in catalog) is never paused.
func (g *familyGate) wait(ctx context.Context, family string, workerIdx int) bool {
	if family == "" {
		return false
	}
	g.mu.Lock()
	until, ok := g.pausedUntil[family]
	g.mu.Unlock()
	if !ok {
		return false
	}
	remaining := time.Until(until)
	if remaining <= 0 {
		return false
	}
	select {
	case <-time.After(remaining):
	case <-ctx.Done():
		return true
	}
	if stagger := time.Duration(workerIdx) * parallel429StaggerStep; stagger > 0 {
		select {
		case <-time.After(stagger):
		case <-ctx.Done():
		}
	}
	return true
}

// extractRateLimitedPause returns the pause duration to apply to the op's
// service family when the per-element result envelope reports RATE_LIMITED.
// Returns 0 for non-rate-limited results. Honours upstream retry_after_ms
// when positive; otherwise falls back to parallel429DefaultRetryAfter.
func extractRateLimitedPause(result map[string]any) time.Duration {
	if result == nil {
		return 0
	}
	errObj, _ := result["error"].(map[string]any)
	if errObj == nil {
		return 0
	}
	if errObj["error_code"] != string(dispatch.ErrCodeRateLimited) {
		return 0
	}
	switch v := errObj["retry_after_ms"].(type) {
	case int64:
		if v > 0 {
			return time.Duration(v) * time.Millisecond
		}
	case int:
		if v > 0 {
			return time.Duration(v) * time.Millisecond
		}
	case float64:
		if v > 0 {
			return time.Duration(v) * time.Millisecond
		}
	}
	return parallel429DefaultRetryAfter
}

// dispatchOne executes a single element via the kernel and maps the result to
// a ParallelResultItem shape (success/error XOR per spec §9.0.1).
func dispatchOne(ctx context.Context, disp dispatch.Dispatcher, idx int, el parallelElement, allowWrite, allowDestructive bool) map[string]any {
	if ctx.Err() != nil {
		return cancelledItem(idx, el.OpID)
	}
	inv := &dispatch.Invocation{
		OpID:             el.OpID,
		Args:             el.Args,
		AllowWrite:       allowWrite,
		AllowDestructive: allowDestructive,
	}
	shaped, err := disp.Dispatch(ctx, inv)
	if err != nil {
		if ctx.Err() != nil {
			return cancelledItem(idx, el.OpID)
		}
		return errorItem(idx, el.OpID, err)
	}
	return successItem(idx, el.OpID, shaped)
}

// successItem builds the per-element envelope for a successful dispatch.
// Carries `format` + `data` (parsed JSON tree); falls back to body string.
func successItem(idx int, opID string, shaped *dispatch.ShapedResponse) map[string]any {
	item := map[string]any{
		"_idx":        idx,
		"_expression": map[string]any{"op_id": opID},
	}
	if shaped == nil {
		return item
	}
	if shaped.Format != "" {
		item["format"] = shaped.Format
	}
	if shaped.StructuredContent != nil {
		item["data"] = shaped.StructuredContent
		return item
	}
	if len(shaped.Body) > 0 {
		if shaped.Format == "toon" {
			item["toon"] = string(shaped.Body)
		} else {
			var v any
			if err := json.Unmarshal(shaped.Body, &v); err == nil {
				item["data"] = v
			} else {
				item["data"] = string(shaped.Body)
			}
		}
	}
	return item
}

// errorItem builds the per-element envelope for a failed dispatch. Carries
// the canonical {error_code, op_id, retryable} envelope per §6.3 line 1001,
// plus any structured detail keys (e.g. retry_after_ms on RATE_LIMITED, used
// by the 429 service-family pause gate, spec §6.3 line 1171).
func errorItem(idx int, opID string, err error) map[string]any {
	code := string(dispatch.ErrCodeServiceDown)
	retryable := false
	var detail map[string]any
	var se *dispatch.StructuredError
	if errors.As(err, &se) {
		code = string(se.ErrCode)
		retryable = se.Retryable
		detail = se.Detail
	}
	errObj := map[string]any{
		"error_code": code,
		"op_id":      opID,
		"retryable":  retryable,
		"message":    err.Error(),
	}
	for k, v := range detail {
		if _, reserved := errObj[k]; reserved {
			continue
		}
		errObj[k] = v
	}
	return map[string]any{
		"_idx":        idx,
		"_expression": map[string]any{"op_id": opID},
		"error":       errObj,
	}
}

// cancelledItem returns the canonical CANCELLED envelope for elements whose
// dispatch did not complete because the enclosing context was cancelled.
// Spec §1421 / §6.3 line 1003: `{"error_code":"CANCELLED","cancelled":true,...}`.
func cancelledItem(idx int, opID string) map[string]any {
	return map[string]any{
		"_idx":        idx,
		"_expression": map[string]any{"op_id": opID},
		"error": map[string]any{
			"error_code": string(dispatch.ErrCodeCancelled),
			"op_id":      opID,
			"cancelled":  true,
			"retryable":  false,
		},
	}
}

// assembleParallelEnvelope builds the outer §9.0.1 envelope: format,
// batch_id, shared_expression_fields, results, and the outer _expression
// sentinel (`op_id="gum_parallel"`, `variant_id=null`).
func assembleParallelEnvelope(results []map[string]any) map[string]any {
	shared := hoistSharedExpressionFields(results)
	env := map[string]any{
		"format":   "parallel_results",
		"batch_id": newBatchID(),
		"results":  toAnySlice(results),
		"_expression": map[string]any{
			"op_id":      "gum_parallel",
			"variant_id": nil,
		},
	}
	if len(shared) > 0 {
		env["shared_expression_fields"] = shared
	}
	return env
}

// hoistSharedExpressionFields walks all results' `_expression` maps and hoists
// any field whose value is identical across every result (compared by canonical
// JSON serialization, per spec §9.0.1 rule 1) into a shared pool. Hoisted
// fields are removed from each per-result `_expression`. Only applies when
// N≥2 (rule 5); single-element batches keep their _expression intact.
func hoistSharedExpressionFields(results []map[string]any) map[string]any {
	if len(results) < 2 {
		return nil
	}
	// Collect candidate fields: those present in result[0]._expression.
	first, ok := results[0]["_expression"].(map[string]any)
	if !ok || len(first) == 0 {
		return nil
	}
	shared := map[string]any{}
	for key, v0 := range first {
		canon0, err := canonicalJSON(v0)
		if err != nil {
			continue
		}
		allMatch := true
		for i := 1; i < len(results); i++ {
			expr, ok := results[i]["_expression"].(map[string]any)
			if !ok {
				allMatch = false
				break
			}
			vi, present := expr[key]
			if !present {
				// Absent ≠ explicit value (rule 1, "null and absent are NOT identical").
				allMatch = false
				break
			}
			canoni, err := canonicalJSON(vi)
			if err != nil || canoni != canon0 {
				allMatch = false
				break
			}
		}
		if allMatch {
			shared[key] = v0
		}
	}
	if len(shared) == 0 {
		return nil
	}
	// Remove hoisted fields from each per-result _expression.
	for _, r := range results {
		expr, ok := r["_expression"].(map[string]any)
		if !ok {
			continue
		}
		for key := range shared {
			delete(expr, key)
		}
	}
	return shared
}

// canonicalJSON returns the canonical JSON serialization of v as a string,
// used as the identity comparator for shared_expression_fields hoisting.
func canonicalJSON(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// stringField returns m[key] coerced to string, or "" if absent / wrong type.
func stringField(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

// toAnySlice converts []map[string]any to []any so Risor can iterate it.
func toAnySlice(in []map[string]any) []any {
	out := make([]any, len(in))
	for i, m := range in {
		out[i] = m
	}
	return out
}

// newBatchID returns an 8-char hex random id for parallel envelopes
// (spec §9.0.1: "<8-char hex>" + §11 outer-entry batch_id).
func newBatchID() string {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "00000000"
	}
	return hex.EncodeToString(buf[:])
}
