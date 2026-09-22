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
	sandbox "github.com/ehmo/gum/internal/sandbox/risor"
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

// parallelBudget carries the §9.0.1 output accounting into one gum_parallel
// closure: what the enclosing script's §6.1 cumulative budget has left right
// now, the configured per-script ceiling, and the batch's worker count.
//
// remaining is nil-tolerant and reports ok=false when no sandbox bound it.
// A batch built without a budget skips both ceilings rather than truncating
// every element against a zero remainder.
type parallelBudget struct {
	remaining   func() (int, bool)
	limitBytes  int
	concurrency int
}

// buildParallelFn returns the gum_parallel closure for one Risor execution.
// The closure captures the enclosing context so cancellation propagates to all
// in-flight workers (spec §6.3 lines 1007-1016), and the §9.0.1 output budget
// so the batch can refuse or truncate its own encoding.
func buildParallelFn(parentCtx context.Context, disp dispatch.Dispatcher, allowWrite, allowDestructive bool, budget parallelBudget) func(...any) (any, error) {
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
		if err := checkBatchCeiling(elements, budget); err != nil {
			return nil, err
		}

		return runParallelBatch(parentCtx, disp, elements, allowWrite, allowDestructive, budget), nil
	}
}

// checkBatchCeiling applies the §9.0.1 pre-dispatch batch ceiling. The batch's
// declared input size is the byte length of the canonical JSON encoding of the
// declared-input object — the same `{"elements":[...]}` shape §12.3 hashes as
// the outer batch entry's args. It is refused when it exceeds
// code.output_limit_bytes x concurrency (32768 bytes at the defaults), before
// any worker dispatches, so no upstream request is made.
func checkBatchCeiling(elements []parallelElement, budget parallelBudget) error {
	if budget.limitBytes <= 0 || budget.concurrency <= 0 {
		return nil
	}
	limit := budget.limitBytes * budget.concurrency
	declared, err := json.Marshal(batchLedgerArgs(elements))
	if err != nil {
		return nil
	}
	if len(declared) <= limit {
		return nil
	}
	return dispatch.NewStructuredError(dispatch.ErrCodeCodeOutputLimitExceeded,
		fmt.Sprintf("gum_parallel batch input of %d bytes exceeds the %d-byte aggregate output ceiling (%d bytes x %d workers)",
			len(declared), limit, budget.limitBytes, budget.concurrency)).
		WithDetail("limit_bytes", limit).
		WithDetail("requested_bytes", len(declared)).
		WithRetryable(false)
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
func runParallelBatch(parentCtx context.Context, disp dispatch.Dispatcher, elements []parallelElement, allowWrite, allowDestructive bool, budget parallelBudget) map[string]any {
	// The batch id is generated before any dispatch, not when the envelope is
	// assembled, because every inner invocation has to carry it: §12.3 links
	// the outer ledger entry to its N inner entries by this value alone.
	batchID := newBatchID()

	results := make([]map[string]any, len(elements))
	if len(elements) == 0 {
		env := assembleParallelEnvelope(batchID, results)
		recordParallelBatch(disp, batchID, elements, results, env, false)
		return env
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
				results[j.idx] = dispatchOne(parentCtx, disp, batchID, j.idx, j.el, allowWrite, allowDestructive)
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

	env := assembleParallelEnvelope(batchID, results)
	// The ceiling runs after the hoist so it measures the shape the caller
	// actually receives, and before the ledger so §12.3 records what was
	// returned rather than what was assembled.
	applyOutputCeiling(env, results, budget)
	recordParallelBatch(disp, batchID, elements, results, env, parentCtx.Err() != nil)
	return env
}

// applyOutputCeiling applies the §9.0.1 element-level truncation. It charges
// the assembled envelope against what the enclosing script's §6.1 budget has
// left. Elements are walked in index order: each one that fits is paid for in
// full, and the first one that does not, plus every element after it, is cut
// at a UTF-8 boundary and marked `_code_output_truncated: true`.
//
// It owns the outer envelope flag too: the flag is set when at least one
// element lost bytes, and its own bytes are charged against the budget before
// any element is measured. An unbound budget, or a batch that fits, changes
// nothing.
//
// The cut is best-effort by construction: an element whose required §13 keys
// already cost more than its allowance still costs that much, because those
// keys are not droppable. The sandbox's own §6.1 counter remains the hard
// clamp on what any script can print.
func applyOutputCeiling(env map[string]any, results []map[string]any, budget parallelBudget) {
	if budget.remaining == nil {
		return
	}
	remaining, ok := budget.remaining()
	if !ok {
		return
	}

	full, err := json.Marshal(env)
	if err != nil || len(full) <= remaining {
		return
	}

	// The envelope's own keys are charged first; what is left belongs to the
	// result elements. The outer flag is set before the skeleton is measured
	// so its own bytes are inside the budget, and removed again if no element
	// turns out to have lost anything.
	env[codeOutputTruncatedKey] = true
	saved := env["results"]
	env["results"] = []any{}
	skeleton, err := json.Marshal(env)
	env["results"] = saved
	if err != nil {
		delete(env, codeOutputTruncatedKey)
		return
	}

	forResults := remaining - len(skeleton)
	spent := 0
	anyTruncated := false
	for i, item := range results {
		// Every element after the first also costs the comma json.Marshal
		// writes before it.
		sep := 0
		if i > 0 {
			sep = 1
		}
		encoded, err := json.Marshal(item)
		if err == nil && spent+sep+len(encoded) <= forResults {
			spent += sep + len(encoded)
			continue
		}
		allow := forResults - spent - sep
		if allow < 0 {
			allow = 0
		}
		cost, cut := truncateResultItem(item, allow)
		spent += sep + cost
		if cut {
			anyTruncated = true
		}
	}
	if !anyTruncated {
		delete(env, codeOutputTruncatedKey)
	}
}

// truncateResultItem cuts one result element so its JSON encoding fits in
// allow bytes, and reports what it now costs and whether anything was lost.
//
// An element that has nothing to give up is returned untouched: §13's
// ParallelResultItem oneOf requires `format` plus `toon` or `data` on a
// success element and `error_code` on a failure, so those keys are never
// dropped to make room.
func truncateResultItem(item map[string]any, allow int) (int, bool) {
	field, text := itemPayloadText(item)
	if field == "" {
		b, err := json.Marshal(item)
		if err != nil {
			return 0, false
		}
		return len(b), false
	}

	item[codeOutputTruncatedKey] = true
	setItemPayload(item, field, "")
	skeleton, err := json.Marshal(item)
	if err != nil {
		delete(item, codeOutputTruncatedKey)
		setItemPayload(item, field, text)
		return 0, false
	}

	room := allow - len(skeleton)
	if room < 0 {
		room = 0
	}
	// JSON escaping makes the encoded payload no shorter than its raw bytes
	// and sometimes longer, so the first cut can still overshoot. Re-cut
	// against the overshoot until the element fits or there is nothing left
	// to give. Each pass drops room by at least the overshoot, so this
	// terminates at room == 0 in the worst case.
	for {
		kept := sandbox.TruncateToUTF8Boundary([]byte(text), room)
		setItemPayload(item, field, string(kept))
		// The skeleton marshalled, and the only value that changed since is a
		// string, so this one cannot fail.
		b, _ := json.Marshal(item)
		if len(b) <= allow || room == 0 {
			if len(kept) == len(text) {
				// An element with an empty payload loses nothing to the cut,
				// so it must not claim it was truncated.
				delete(item, codeOutputTruncatedKey)
				b, _ = json.Marshal(item)
				return len(b), false
			}
			return len(b), true
		}
		room -= len(b) - allow
		if room < 0 {
			room = 0
		}
	}
}

// codeOutputTruncatedKey is the §13 per-element and outer truncation marker.
// It is a sibling of `_expression`, never a field inside it.
const codeOutputTruncatedKey = "_code_output_truncated"

// itemPayloadText names the one field of a result element whose text the
// §9.0.1 ceiling may cut, and returns that text. A success element gives up
// its payload; a failed element can give up only its free-text `message`,
// because §13 requires `error_code`. An empty name means nothing is cuttable.
//
// A `data` tree is rendered as its JSON text, because a cut at a UTF-8
// boundary is defined over bytes, not over a parsed tree. The truncated text
// replaces the tree: §13 declares `data` with an empty schema, so a string is
// a legal value there.
func itemPayloadText(item map[string]any) (string, string) {
	if s, ok := item["toon"].(string); ok {
		return "toon", s
	}
	if v, ok := item["data"]; ok {
		if s, ok := v.(string); ok {
			return "data", s
		}
		b, err := json.Marshal(v)
		if err != nil {
			return "", ""
		}
		return "data", string(b)
	}
	if errObj, ok := item["error"].(map[string]any); ok {
		if s, ok := errObj["message"].(string); ok && s != "" {
			return "error.message", s
		}
	}
	return "", ""
}

// setItemPayload writes text back to the field itemPayloadText named.
func setItemPayload(item map[string]any, field, text string) {
	switch field {
	case "toon", "data":
		item[field] = text
	case "error.message":
		if errObj, ok := item["error"].(map[string]any); ok {
			errObj["message"] = text
		}
	}
}

// recordParallelBatch hands the completed batch to the dispatcher's §12.3
// ledger accounting, when it offers that capability. Elements that dispatched
// wrote their own inner entry; the recorder supplies the outer sentinel and
// the inner entries for elements that were cancelled instead.
func recordParallelBatch(disp dispatch.Dispatcher, batchID string, elements []parallelElement, results []map[string]any, envelope map[string]any, cancelled bool) {
	rec, ok := disp.(dispatch.ParallelBatchRecorder)
	if !ok {
		return
	}

	recorded := make([]dispatch.ParallelBatchElement, len(elements))
	for i, el := range elements {
		recorded[i] = dispatch.ParallelBatchElement{
			OpID:      el.OpID,
			Args:      el.Args,
			Cancelled: itemWasCancelled(results[i]),
		}
	}

	rec.RecordParallelBatch(dispatch.ParallelBatch{
		BatchID:   batchID,
		Args:      batchLedgerArgs(elements),
		Envelope:  envelope,
		Elements:  recorded,
		Cancelled: cancelled,
	})
}

// itemWasCancelled reports whether a per-element envelope is the CANCELLED
// one. Reading the result rather than tracking a separate flag keeps the
// ledger's view of the batch identical to the caller's.
func itemWasCancelled(item map[string]any) bool {
	errObj, _ := item["error"].(map[string]any)
	if errObj == nil {
		return false
	}
	return errObj["error_code"] == string(dispatch.ErrCodeCancelled)
}

// batchLedgerArgs renders the batch's own input as the map the §12.3 outer
// entry hashes and prices. The Risor-side input is a list, and args_hash is
// defined over a JCS object, so the list is wrapped under one key.
func batchLedgerArgs(elements []parallelElement) map[string]any {
	list := make([]any, len(elements))
	for i, el := range elements {
		item := map[string]any{"op_id": el.OpID}
		if len(el.Args) > 0 {
			item["args"] = el.Args
		}
		if el.VariantID != "" {
			item["variant_id"] = el.VariantID
		}
		list[i] = item
	}
	return map[string]any{"elements": list}
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
func dispatchOne(ctx context.Context, disp dispatch.Dispatcher, batchID string, idx int, el parallelElement, allowWrite, allowDestructive bool) map[string]any {
	if ctx.Err() != nil {
		return cancelledItem(idx, el.OpID)
	}
	if err := refuseLRO(disp, el.OpID); err != nil {
		return errorItem(idx, el.OpID, err)
	}
	inv := &dispatch.Invocation{
		OpID:             el.OpID,
		Args:             el.Args,
		AllowWrite:       allowWrite,
		AllowDestructive: allowDestructive,
		BatchID:          batchID,
		BatchIndex:       idx,
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

// parallelBatchOpID is the op_id the outer §9.0.1 batch entry reports. It is
// a script builtin, not a catalog op, so nothing else resolves this name.
const parallelBatchOpID = "gum_parallel"

// elementExpression builds one per-element `_expression` object, before the
// §9.0.1 hoist strips the fields the whole batch shares.
//
// The shaped envelope is the authority when dispatch produced one. An element
// that failed, or a degraded path that shaped nothing, still gets the §13
// required field set: the receiver reconstructs an effective ExpressionMeta
// for every element and validates it against the full schema, so a per-result
// object carrying op_id alone is not a legal delta.
//
// op_id comes from the batch element either way. It is what the caller asked
// for, and dispatch copies it onto the invocation the envelope reports.
func elementExpression(opID string, meta *dispatch.ExpressionMeta) map[string]any {
	if meta == nil {
		meta = &dispatch.ExpressionMeta{}
	}
	fields := meta.Fields()
	fields["op_id"] = opID
	return fields
}

// successItem builds the per-element envelope for a successful dispatch.
// Carries `format` + `data` (parsed JSON tree); falls back to body string.
func successItem(idx int, opID string, shaped *dispatch.ShapedResponse) map[string]any {
	var meta *dispatch.ExpressionMeta
	if shaped != nil {
		meta = shaped.Expression
	}
	item := map[string]any{
		"_idx":        idx,
		"_expression": elementExpression(opID, meta),
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
		"_expression": elementExpression(opID, nil),
		"error":       errObj,
	}
}

// cancelledItem returns the canonical CANCELLED envelope for elements whose
// dispatch did not complete because the enclosing context was cancelled.
// Spec §1421 / §6.3 line 1003: `{"error_code":"CANCELLED","cancelled":true,...}`.
func cancelledItem(idx int, opID string) map[string]any {
	return map[string]any{
		"_idx":        idx,
		"_expression": elementExpression(opID, nil),
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
func assembleParallelEnvelope(batchID string, results []map[string]any) map[string]any {
	shared := hoistSharedExpressionFields(results)
	env := map[string]any{
		"format":   "parallel_results",
		"batch_id": batchID,
		"results":  toAnySlice(results),
		// The batch entry has no profile and no single variant (§12.3), but
		// §13 requires the whole ExpressionMeta field set on it, so it
		// reports the batch's own record count and a null variant_id
		// instead of a two-key stub.
		"_expression": (&dispatch.ExpressionMeta{
			OpID:        parallelBatchOpID,
			ResultCount: len(results),
		}).Fields(),
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
	// Remove hoisted fields from each per-result _expression. A non-empty
	// shared pool means every result carried an _expression map: the
	// allMatch loop above rejects any key a later result cannot produce.
	for _, r := range results {
		expr, _ := r["_expression"].(map[string]any)
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
	// crypto/rand.Read fills the buffer or crashes the program; it has not
	// returned an error since Go 1.24.
	_, _ = rand.Read(buf[:])
	return hex.EncodeToString(buf[:])
}
