package dispatch

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/ehmo/gum/internal/output/gain"
)

// gain.go builds the spec §12.3 gain-ledger entry step 9 appends.
//
// Every field below comes from state the kernel already holds at step 9. The
// two token counts that matter for gain.Stats are raw_tokens (the upstream
// body before shaping) and shaped_tokens (what the caller receives); their
// difference is the savings figure §2 gates the release on.

// gainRetryWindow is the "same session + op_family + args_hash" interval that
// marks an entry as a retry (spec §12.3 `is_retry`).
const gainRetryWindow = 5 * time.Minute

// sessionID is the 8-character hex session identifier spec §12.3 requires.
// It is random per process and deliberately not derived from anything
// user-stable: the telemetry schema calls it "8-char random per-process, NOT
// user-stable", so it must not survive a restart or identify a machine.
var sessionID = newSessionID()

func newSessionID() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		// A ledger entry with no session is still usable for savings math,
		// and a failing CSPRNG must not take a dispatch down.
		return "00000000"
	}
	return hex.EncodeToString(b[:])
}

// retryTracker answers "has this session already made this exact call within
// the last 5 minutes?". Entries older than the window are dropped on the next
// observation, so the map tracks live traffic rather than the whole session.
type retryTracker struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

// observe records key at now and reports whether the same key was seen
// inside gainRetryWindow.
func (t *retryTracker) observe(key string, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.seen == nil {
		t.seen = map[string]time.Time{}
	}

	prev, ok := t.seen[key]
	isRetry := ok && now.Sub(prev) < gainRetryWindow

	for k, ts := range t.seen {
		if now.Sub(ts) >= gainRetryWindow {
			delete(t.seen, k)
		}
	}
	t.seen[key] = now

	return isRetry
}

// gainCacheStatus maps the step-6 cache outcome onto the closed §12.3 enum.
// The kernel has one cache, the §10.3 semantic cache, and only read-class
// variants are eligible for it; an op that was never a cache candidate is
// "not_applicable" rather than a miss it never had the chance to hit.
func gainCacheStatus(cacheable, fromCache bool) string {
	switch {
	case !cacheable:
		return "not_applicable"
	case fromCache:
		return "hit"
	default:
		return "miss"
	}
}

// gainFieldMaskStatus reports whether the call carried an upstream field mask.
// The only mask gum puts on the wire is the universal Google `fields` param
// (§12.4 --no-field-mask deletes exactly that arg). "skipped" means the
// resolved profile declares a projection the call did not send upstream, which
// is what --no-field-mask and a raw pass-through both produce.
func gainFieldMaskStatus(inv *Invocation) string {
	if inv == nil {
		return "not_applicable"
	}
	if s, ok := inv.Args["fields"].(string); ok && strings.TrimSpace(s) != "" {
		return "applied"
	}
	if semanticFields(inv) != "" {
		return "skipped"
	}
	return "not_applicable"
}

// buildGainEntry assembles the §12.3 record for one completed dispatch.
func (d *dispatcher) buildGainEntry(inv *Invocation, rv *ResolvedVariant, creds *Credentials, shaped *ShapedResponse, raw *Response, fromCache, cacheable bool) gain.Entry {
	var rawBody []byte
	if raw != nil {
		rawBody = raw.Body
	}
	var shapedBody []byte
	if shaped != nil {
		shapedBody = shaped.Body
	}

	// A tokenizer failure must not fail the dispatch. measureTokens returns 0,
	// which reads as "no evidence" in gain.Stats rather than a false saving.
	rawTokens := measureGainTokens(rawBody)
	shapedTokens := measureGainTokens(shapedBody)

	var variantID *string
	if rv != nil && rv.Variant != nil {
		v := rv.Variant.VariantID
		variantID = &v
	}

	var outputProfile *string
	if shaped != nil && shaped.Expression != nil && shaped.Expression.Profile != "" {
		p := shaped.Expression.Profile
		outputProfile = &p
	}

	canonical := d.canonicalArgs(inv.Args)
	argsHash := argsHashHex(canonical)
	family := gain.OpFamily(inv.OpID)
	now := time.Now().UTC()

	return gain.Entry{
		Session:                sessionID,
		OpID:                   inv.OpID,
		OpFamily:               family,
		VariantID:              variantID,
		OutputProfile:          outputProfile,
		ArgsHash:               argsHash,
		AuthSubjectFingerprint: semanticAuthFP(inv, creds),
		// request_tokens is what the caller spent describing the call, and
		// response_tokens is what it gets back. The canonical args are the
		// request GUM composes; the shaped body is the response it returns.
		RequestTokens:   measureGainTokens([]byte(canonicalizeArgs(canonical))),
		ResponseTokens:  shapedTokens,
		RawTokens:       rawTokens,
		ShapedTokens:    shapedTokens,
		CacheStatus:     gainCacheStatus(cacheable, fromCache),
		FieldMaskStatus: gainFieldMaskStatus(inv),
		ServedFromCache: fromCache,
		IsRetry:         d.retries.observe(sessionID+"|"+family+"|"+argsHash, now),
		// Live entries are "estimated": only fixture replay produces the
		// reproducible numbers §2 allows in a release claim.
		BaselineMethod: gain.BaselineEstimated,
		Timestamp:      now.Format(time.RFC3339),
		BatchID:        inv.BatchID,
		BatchIndex:     gainBatchIndex(inv),
	}
}

// gainBatchIndex returns the §12.3 batch_index pointer for inv: the element's
// position when gum_parallel dispatched it, nil for a standalone call. The
// pointer is what keeps index 0 distinguishable from "not in a batch", since
// both marshal the same int.
func gainBatchIndex(inv *Invocation) *int {
	if inv == nil || inv.BatchID == "" {
		return nil
	}
	i := inv.BatchIndex
	return &i
}

// RecordParallelBatch writes the §12.3 outer sentinel entry for one
// gum_parallel call, plus one inner entry per element the batch cancelled.
//
// Elements that reached the dispatcher wrote their own inner entry at step 9.
// A cancelled element never did: it produced no response, so step 9 never ran
// for it, and without this the batch's element_count would disagree with the
// number of inner entries carrying its batch_id.
//
// Accounting is best-effort, like step 9: a ledger error is logged and the
// batch result still returns.
func (d *dispatcher) RecordParallelBatch(b ParallelBatch) {
	if d.gainLedger == nil || b.BatchID == "" {
		return
	}

	canonical := canonicalizeArgs(d.canonicalArgs(b.Args))
	envelope, err := json.Marshal(b.Envelope)
	if err != nil {
		// An envelope gum itself assembled always marshals; price it as 0
		// rather than drop the batch's accounting entirely.
		envelope = nil
	}

	now := time.Now().UTC().Format(time.RFC3339)

	outer := gain.NewGumParallelOuterEntry(
		sessionID,
		b.BatchID,
		argsHashHex(d.canonicalArgs(b.Args)),
		len(b.Elements),
		measureGainTokens([]byte(canonical)),
		measureGainTokens(envelope),
		gain.BaselineEstimated,
	)
	outer.Cancelled = b.Cancelled
	outer.Timestamp = now
	d.appendGainEntry(outer, gain.OpIDGumParallel)

	for i, el := range b.Elements {
		if !el.Cancelled {
			continue
		}
		d.appendGainEntry(cancelledInnerEntry(d, b.BatchID, i, el, now), el.OpID)
	}
}

// cancelledInnerEntry is the §12.3 inner entry for an element gum_parallel
// cancelled. Every token count is 0: no request went upstream and no response
// came back, so claiming either would invent savings the batch never made.
//
// variant_id and output_profile are null for the same reason. A cancelled
// element stopped before routing, so it has no resolved variant and no
// resolved profile; an empty string would assert ids that never existed.
func cancelledInnerEntry(d *dispatcher, batchID string, index int, el ParallelBatchElement, ts string) gain.Entry {
	idx := index
	return gain.Entry{
		Session:                sessionID,
		OpID:                   el.OpID,
		OpFamily:               gain.OpFamily(el.OpID),
		VariantID:              nil,
		OutputProfile:          nil,
		ArgsHash:               argsHashHex(d.canonicalArgs(el.Args)),
		AuthSubjectFingerprint: "",
		CacheStatus:            "not_applicable",
		FieldMaskStatus:        "not_applicable",
		BaselineMethod:         gain.BaselineEstimated,
		BatchID:                batchID,
		BatchIndex:             &idx,
		ErrorCode:              "CANCELLED",
		Cancelled:              true,
		Timestamp:              ts,
	}
}

// failedDispatchEntry is the §12.3 record for a dispatch that raised an error.
//
// Spec §12.3 says every completed dispatch appends one entry, and lists an
// optional error_code "for failed dispatches". Step 9 runs only after a
// response exists, so before this the ledger held successes alone: a profile
// that failed half its calls read exactly like one that failed none, and the
// error_code column could never be populated.
//
// raw_tokens and shaped_tokens are 0 because no upstream body arrived and no
// shaped body left. That keeps the entry inert in the release subtotal, which
// admits only baseline_method="fixture_replay" rows, while still counting the
// call in the window's totals.
//
// rv is the variant the call resolved to, or nil when it failed before step 3.
// A nil variant leaves variant_id null rather than empty: an empty string would
// assert an id that never existed.
func (d *dispatcher) failedDispatchEntry(inv *Invocation, rv *ResolvedVariant, code ErrorCode) gain.Entry {
	var variantID *string
	if rv != nil && rv.Variant != nil {
		v := rv.Variant.VariantID
		variantID = &v
	}

	canonical := d.canonicalArgs(inv.Args)

	return gain.Entry{
		Session:                sessionID,
		OpID:                   inv.OpID,
		OpFamily:               gain.OpFamily(inv.OpID),
		VariantID:              variantID,
		OutputProfile:          nil,
		ArgsHash:               argsHashHex(canonical),
		AuthSubjectFingerprint: "",
		// The caller still spent these tokens describing a call that failed.
		RequestTokens:   measureGainTokens([]byte(canonicalizeArgs(canonical))),
		CacheStatus:     "not_applicable",
		FieldMaskStatus: "not_applicable",
		BaselineMethod:  gain.BaselineEstimated,
		ErrorCode:       string(code),
		Cancelled:       code == ErrCodeCancelled,
		BatchID:         inv.BatchID,
		BatchIndex:      gainBatchIndex(inv),
		Timestamp:       time.Now().UTC().Format(time.RFC3339),
	}
}

// recordFailedDispatch appends one failure entry, unless gum_parallel already
// owns the row.
//
// RecordParallelBatch writes a cancelled inner entry for every element the
// batch cut short, and it cannot tell an element cancelled before dispatch from
// one cancelled inside it. Recording both would double-count that element, so
// a cancelled batch element yields to the batch recorder.
func (d *dispatcher) recordFailedDispatch(inv *Invocation, rv *ResolvedVariant, err error) {
	if d.gainLedger == nil || inv == nil {
		return
	}
	var se *StructuredError
	if !errors.As(err, &se) {
		return
	}
	if inv.BatchID != "" && se.ErrCode == ErrCodeCancelled {
		return
	}
	d.appendGainEntry(d.failedDispatchEntry(inv, rv, se.ErrCode), inv.OpID)
}

// appendGainEntry writes one entry and logs a failure without propagating it.
func (d *dispatcher) appendGainEntry(e gain.Entry, opID string) {
	if err := d.gainLedger.Append(e); err != nil {
		d.log().Warn("gain ledger append failed", "op_id", opID, "err", err)
	}
}

// measureGainTokens counts cl100k_base tokens, returning 0 when the tokenizer
// is unavailable. Accounting is best-effort: step 9 must never fail a call
// that already produced a valid response.
func measureGainTokens(b []byte) int {
	if len(b) == 0 {
		return 0
	}
	n, err := gain.MeasureTokensCl100k(b)
	if err != nil {
		return 0
	}
	return n
}
