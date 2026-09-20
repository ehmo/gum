package dispatch

import (
	"crypto/rand"
	"encoding/hex"
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
		BaselineMethod: "estimated",
		Timestamp:      now.Format(time.RFC3339),
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
