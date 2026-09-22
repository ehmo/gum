// Package dispatch owns the 9-step invocation lifecycle and policy kernel (spec.md §3.1, §14).
//
// parse → policy → routing → cache → auth → token bucket → executor → shape → return.
// Must not depend on internal/cli or internal/mcp. Must not import CGo.
package dispatch

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ehmo/gum/internal/cache"
	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/output/jcs"
	"github.com/ehmo/gum/internal/output/profile"
)

// newRequestID returns a collision-resistant request_id of the form
// "req-<16 hex chars>" backed by crypto/rand. UnixNano-only IDs collide
// under concurrent dispatch on modern hardware (gum-1n1t).
func newRequestID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("req-%d", time.Now().UnixNano())
	}
	return "req-" + hex.EncodeToString(b[:])
}

// Invocation is the normalised request produced by step 1 of the dispatch lifecycle.
type Invocation struct {
	OpID                     string
	Args                     map[string]any
	Format                   string // "toon" | "json" | "raw"
	Confirmed                bool
	ConfirmationToken        string
	AllowWrite               bool
	AllowDestructive         bool
	RequireWriteConfirmation bool
	RequestID                string // for tracing / slog structured logging

	// RequestedVariantID, when non-empty, pins variant resolution to this exact
	// variant_id (spec §5.1, §12.0 variant-selection rule). Unknown, removed,
	// quarantined, pending-restart, or needs-configuration variants fail with
	// VARIANT_NOT_FOUND / VARIANT_QUARANTINED before any upstream request,
	// matching the MCP dispatch error envelope. Empty string falls back to
	// the default-variant resolution order.
	RequestedVariantID string

	// OutputProfile, when non-nil, is the resolved expression profile for this
	// invocation. The dispatcher reads OutputProfile.Recovery and
	// OutputProfile.TeeMode to decide whether to write a filesystem tee
	// artifact (spec §9.0). The presentation layer is responsible for
	// resolution (catalog-embedded → user-global → project-local).
	OutputProfile *profile.Profile

	// SuppressFieldMask reports that the caller asked for no upstream field
	// mask at all (--no-field-mask on the CLI). It is distinct from "the
	// caller sent no fields arg": the latter lets the profile's field_mask
	// supply one, this one forbids it. Spec §12.0.
	SuppressFieldMask bool

	// MaxItems, when set, replaces the active profile's collapse_arrays cap
	// for this one invocation. The presentation layer maps its own surface
	// (--max-items on the CLI, max_items in MCP) onto it. The zero value
	// leaves the profile's own cap in force (gum-pmbp).
	MaxItems profile.MaxItemsOverride

	// AuthSubjectFingerprint is the stable per-principal opaque ID used as the
	// fourth component of the tee artifact hash (spec §9.0 line 1846,
	// §10.0.1). Falls back to Credentials.SubjectFingerprint when this field
	// is empty so call-sites that already populate the value on the resolved
	// credentials don't have to repeat it here.
	AuthSubjectFingerprint string

	// Caller is the closed-enum identifier of the presentation surface that
	// produced this invocation (spec §14.1 rule 4). One of CallerCLI,
	// CallerMCP, CallerRisor, CallerPlugin. Empty string is permitted at the
	// dispatcher level so unit tests and library embedders don't have to
	// declare it; the lifecycle logger surfaces the value verbatim.
	Caller Caller

	// IfNoneMatch is the §10.2 validator the kernel found for this exact
	// (op_id, variant_id_resolved, args_canonical, auth_subject_fingerprint).
	// An HTTP adapter sends it as the `If-None-Match` request header; an
	// adapter that makes no HTTP request ignores it. Empty means the kernel
	// holds no validator, so the request goes out unconditional.
	IfNoneMatch string

	// BatchID and BatchIndex link this invocation to the gum_parallel batch
	// that scheduled it (spec §12.3). gum_parallel sets both on every element
	// it dispatches; step 9 copies them onto the gain entry so the batch's
	// outer sentinel and its N inner entries share one 8-char hex id. Empty
	// BatchID means a standalone call, and BatchIndex is then ignored.
	BatchID    string
	BatchIndex int
}

// ResolvedVariant is the output of step 3 (routing / resolveVariant).
type ResolvedVariant struct {
	OpID       string
	Variant    *catalog.Variant
	AdapterKey string
	// Deprecated is true when the selected variant is listed in op.DeprecatedVariantIDs.
	// The variant is still invoked; the output pipeline uses this flag to attach the
	// VARIANT_DEPRECATED warning envelope field (spec §5.5, §1421).
	Deprecated bool
}

// CachedResponse is the output of step 4 (cache check).
type CachedResponse struct {
	Body       []byte
	Format     string
	CapturedAt time.Time
}

// Credentials carries resolved auth tokens for step 6 (auth).
// Phase 2 stub — fields filled in Phase 3.
type Credentials struct {
	Token string
	// APIKey, when non-empty, is forwarded as the X-Goog-Api-Key header by
	// REST adapters and replaces the Bearer Authorization header. Spec §7
	// auth_strategy=api_key variants populate this field exclusively; Token
	// MUST remain empty so the adapter does not double-sign the request.
	APIKey string
	// QuotaProjectID, when non-empty, is forwarded as the X-Goog-User-Project
	// header by REST adapters. Some Google APIs (e.g. Search Console) require
	// it when the caller is using user ADC instead of a service account.
	QuotaProjectID string
	// SubjectFingerprint is the stable per-principal opaque ID used by the
	// step-8 tee artifact write (spec §9.0 line 1846, §10.0.1). Typically the
	// SHA-256 of the OAuth subject claim or ADC service-account email.
	// Filled by the AuthResolver; empty when running unauthenticated stubs.
	SubjectFingerprint string
}

// Response is the raw executor output from step 7.
type Response struct {
	Body       []byte
	Format     string
	BytesIn    int
	BytesOut   int
	StatusCode int

	// CodeOutputTruncated reports that a gum.code script lost printed bytes to
	// the §6.1 cumulative output budget. shapeResponse projects it as
	// _expression._code_output_truncated.
	CodeOutputTruncated bool

	// ETag is the validator upstream returned with this response, verbatim
	// from the `ETag` header. The kernel stores it in the §10.2 cache so the
	// next identical call can revalidate instead of re-downloading. Empty
	// when upstream sent none, which is most non-HTTP adapters and any
	// endpoint that does not support conditional requests.
	ETag string
}

// ShapedResponse is the final output after step 8 (output pipeline).
//
// StructuredContent, when non-nil, carries the shaped JSON tree underlying Body
// — the same value Body encodes, before encoding. MCP handlers project it into
// CallToolResult.StructuredContent for clients that consume the
// machine-readable schema-validated shape; the encoded text in Body goes into
// the text content block.
//
// It is the shaped tree, not the upstream body. Building it from the upstream
// body made structuredContent contradict Body on every profiled op: a client
// reading it got every field the profile removed, the full row count the cap
// had trimmed, and none of the token saving (spec §13 line 2134).
type ShapedResponse struct {
	Body              []byte
	Format            string
	StructuredContent any

	// FullResultPath is the absolute filesystem path of the tee artifact
	// written during the §9.0 'artifact' stage. Always non-empty when tee
	// fires (recovery != "none" and tee_mode != "off"). The presentation layer
	// projects it as _expression.full_result_path.
	FullResultPath string

	// FullResultResource is the gum://results/<hash> recovery URI. Set only
	// when the active profile uses recovery = "resource_link" and we are in
	// MCP mode (spec §9.0 line 1845). The presentation layer projects it as
	// _expression.full_result_resource and emits a matching MCP
	// resource_link content block.
	FullResultResource string

	// FullResultSize is the decompressed byte length of the tee artifact
	// payload. Populated whenever tee fires (alongside FullResultPath);
	// the MCP layer threads it into ResourceLink.Size on the recovery
	// content block per spec §9.0 line 1846 ("size when known"). Nil when
	// tee did not fire.
	FullResultSize *int64

	// ValidationWarnings carries spec §5.7 read-only allowlist pass-through
	// notices. The presentation layer projects them as a top-level
	// `_validation_warnings` field on the response envelope. Empty/nil when
	// no allowlist applied.
	ValidationWarnings []string

	// DroppedPaths lists the response dot-paths the active expression profile's
	// field filters removed (profile.ApplyOutput.DroppedPaths). Nil for a raw
	// pass-through and for any op whose profile declares no field filter.
	//
	// The presentation layer must name these paths. A whitelist that omits a
	// field the op advertises otherwise returns valid, plausible, silently
	// incomplete JSON, and the caller has no signal to look for the rest
	// (gum-bpx0).
	DroppedPaths []string

	// CollapsedArrays lists the arrays collapse_arrays truncated, with the
	// kept and omitted counts and the sibling key holding the count. The
	// presentation layer names the omitted rows in its shaping notice, because
	// a notice that reports only the removed fields points the reader at the
	// smaller loss (gum-pmbp).
	CollapsedArrays []profile.CollapsedArray

	// DedupedRows and LimitedRows count the rows stage 7's dedupe and the
	// profile's limit removed. Neither writes a count into the body, so the
	// shaping notice is the only place the caller learns the result is short.
	DedupedRows int
	LimitedRows int

	// AnnotationPaths lists the dot-paths the executing adapter added in step
	// 8's annotate stage and shaping then kept. Nil for a raw pass-through,
	// for an adapter that annotates nothing, and for a field the profile
	// dropped again on its way through.
	//
	// The presentation layer names these paths when it offers `--format raw`.
	// Raw bypasses the annotator, so a caller who follows that advice to
	// recover a trimmed field loses these ones instead, and the notice that
	// sent them there is the only place they could have learned it (gum-9l5c).
	AnnotationPaths []string

	// Expression is the spec §13 `_expression` envelope describing what the
	// shaping pipeline did. Non-nil on every success path, including a raw
	// pass-through, because §13 makes profile, op_id, variant_id, lossy and
	// result_count required on every shaped result.
	//
	// The presentation layer projects it verbatim: MCP into structuredContent,
	// the CLI into its stderr shaping notice.
	Expression *ExpressionMeta
}

// CacheLayerStats holds a point-in-time snapshot of the semantic (in-process)
// cache counters surfaced by gum.cache_stats (spec §3003).
type CacheLayerStats struct {
	Hits      int64
	Misses    int64
	Evictions int64
	Entries   int64
	Bytes     int64
}

// Dispatcher is the public kernel interface.
type Dispatcher interface {
	Dispatch(ctx context.Context, inv *Invocation) (*ShapedResponse, error)
}

// ServiceFamilyResolver is an optional Dispatcher capability that reveals an
// op's catalog service_family (e.g. "workspace", "cloud", "maps", "genai",
// "plugin"). gum_parallel uses it to scope upstream-429 pauses so a Gmail
// quota hit does not stall unrelated BigQuery or Maps workers in the same
// batch (spec §6.3 line 1171). Mock dispatchers in tests may omit this
// capability; consumers must tolerate a missing resolver by falling back to
// a single shared pause group.
type ServiceFamilyResolver interface {
	ServiceFamily(opID string) string
}

// LROClassifier is an optional Dispatcher capability that reports whether an
// op's default variant is classified `lro_return` (spec §5.8). The code-mode
// host functions use it to raise LRO_UNSUPPORTED_IN_CODE before dispatch
// (§6.1): the restriction is a property of gum.code, not of the op, so the
// kernel supplies the classification and code mode decides what to do with
// it. Mock dispatchers in tests may omit this capability; consumers must
// tolerate a missing classifier by dispatching as before.
type LROClassifier interface {
	ReturnsLRO(opID string) bool
}

// ParallelBatchRecorder is an optional Dispatcher capability that writes the
// spec §12.3 gum_parallel outer sentinel entry to the gain ledger, plus one
// inner entry per element the batch cancelled before dispatch. gum_parallel
// calls it once, after the batch envelope is assembled, because the outer
// entry's token counts are the envelope's own cost and nothing inside the
// dispatch lifecycle can see them.
//
// Mock dispatchers in tests may omit this capability; gum_parallel tolerates
// a missing recorder by skipping batch accounting, exactly as it tolerates a
// missing ServiceFamilyResolver.
type ParallelBatchRecorder interface {
	RecordParallelBatch(b ParallelBatch)
}

// ParallelBatch is one completed gum_parallel call as the ledger sees it.
type ParallelBatch struct {
	// BatchID is the 8-char hex id gum_parallel put on the envelope and on
	// every Invocation it dispatched.
	BatchID string

	// Args is the batch's own arguments, whose JCS canonical form the outer
	// entry hashes and prices as request_tokens.
	Args map[string]any

	// Envelope is the assembled batch result, priced as response_tokens.
	Envelope map[string]any

	// Elements is one record per batch element, in dispatch order.
	Elements []ParallelBatchElement

	// Cancelled reports that the outer context was cancelled after scheduling
	// began (spec §12.3 outer-entry `cancelled`).
	Cancelled bool
}

// ParallelBatchElement is one element of a ParallelBatch. Elements that
// reached the dispatcher already wrote their own inner entry at step 9; only
// the cancelled ones need the recorder to write one, because a cancelled
// element never produced a response to account for.
type ParallelBatchElement struct {
	OpID string
	Args map[string]any

	// Cancelled reports that this element never ran, or was interrupted, due
	// to context cancellation.
	Cancelled bool
}

// Adapter is what executors implement (typed-rest-sdk, code.risor, etc.).
type Adapter interface {
	Execute(ctx context.Context, inv *Invocation, rv *ResolvedVariant, creds *Credentials) (*Response, error)
}

// ResponseAnnotator is the optional half of Adapter for upstream responses that
// need a field the service does not send. The kernel calls it inside step 8,
// on the shaping path only, which pins two properties the annotation would
// otherwise break:
//
//   - `--format raw` returns the upstream bytes, because step 8 bypasses
//     shaping for that format before the annotator runs.
//   - The step 7b semantic cache stores the unannotated executor body, so a
//     warm call re-runs the annotation for its own format instead of serving
//     another format's.
//
// The kernel stays generic: what to add and when belongs to the adapter.
type ResponseAnnotator interface {
	// AnnotateResponse returns the body to shape and the dot-paths it added.
	// It must not modify body in place. Returning body unchanged is both the
	// normal answer and the answer for a response the adapter cannot read: the
	// upstream body is still a correct response, so an unrecognised shape is
	// not an error.
	//
	// The paths are what the shaping notice names when it offers the caller
	// `--format raw`: raw skips this step, so every added field is a cost of
	// switching, and the notice has to say so (gum-9l5c). Paths use the same
	// dot-path vocabulary as ShapedResponse.DroppedPaths, with no array
	// indices, and are empty whenever the body comes back unchanged.
	AnnotateResponse(inv *Invocation, rv *ResolvedVariant, body []byte) ([]byte, []string)
}

// NewDispatcher constructs the dispatch kernel with a catalog snapshot and a map of adapters keyed
// by adapter_key (e.g. "code.risor").
func NewDispatcher(snapshot *catalog.Catalog, adapters map[string]Adapter) Dispatcher {
	return &dispatcher{
		snapshot: snapshot,
		adapters: adapters,
	}
}

// dispatcher is the concrete kernel. unexported; obtained via NewDispatcher or NewDispatcherWithConfig.
type dispatcher struct {
	snapshot                *catalog.Catalog
	adapters                map[string]Adapter
	auth                    AuthResolver                               // Phase 3: optional auth resolver
	cache                   *cache.MemCache                            // Phase 3: optional response cache (legacy)
	semanticCache           *cache.SemanticCache                       // §10.3 semantic response cache (preferred)
	httpCache               *cache.HTTPCache                           // §10.2 HTTP/ETag revalidation cache
	tokenBucket             TokenBucket                                // Phase 3: optional rate limiter
	auditSink               auditSink                                  // optional audit log sink; receives panic entries per spec §3.1 step 7
	profilePolicy           ProfilePolicy                              // gum-vq4z.2: per-profile policy gates
	profileName             string                                     // active profile bound into confirmation tokens
	confirmationReplayDir   string                                     // profile data dir for durable confirmation replay markers
	preferredInterfaceKinds []string                                   // gum-vq4z.3: interface_kind tie-breaking order
	gainLedger              GainLedger                                 // gum-vq4z.9: optional gain-ledger sink (step 9)
	teeConfig               TeeConfig                                  // gum-66wd: filesystem tee artifact policy (spec §9.0)
	normalizeDatetimes      bool                                       // gum-y1n: spec §10.0 Rule 4 UTC normalization of RFC 3339 datetime args
	profileLookup           func(name string) (*profile.Profile, bool) // §9.2 catalog-embedded profile resolver (step 8)
	profileBindings         func() map[string]string                   // §9.2 [override_bindings]: op_id/variant_id -> profile name
	argDefaulter            ArgDefaulter                               // gum-puum: configured arg defaults (step 1)
	expectedAuthSubjects    map[string]string                          // gum-q0kd: auth_strategy -> profile's bound auth_subject_fingerprint
	scopeUpgradeLogin       ScopeUpgradeLogin                          // gum-f6kq: §13 managed-scope re-consent; nil disables the flow

	opIndexOnce sync.Once              // builds opIndex on first findOp (review gum-yvam)
	opIndex     map[string]*catalog.Op // canonical op_id + alias → *Op; snapshot is immutable post-construction

	retries retryTracker // §12.3 is_retry window: session+op_family+args_hash seen inside 5 minutes

	// upgradedScopes holds the scopes a §13 re-consent granted after
	// construction. scopeMu guards it: the grant lands while other goroutines
	// are inside policy gate 5.
	upgradedScopes []string
	scopeMu        sync.RWMutex

	logger *slog.Logger // §14.1 rule 2 injected logger; nil means slog.Default()
}

// opByID returns a lazily-built lookup of canonical op_ids and their deprecated
// aliases to *catalog.Op. The snapshot is immutable after construction, so the
// index is built once. findOp previously did two O(n) scans of snapshot.Ops on
// every call, and is invoked ~11×/dispatch (review gum-yvam).
func (d *dispatcher) opByID() map[string]*catalog.Op {
	d.opIndexOnce.Do(func() {
		if d.snapshot == nil {
			return
		}
		m := make(map[string]*catalog.Op, len(d.snapshot.Ops)*2)
		for i := range d.snapshot.Ops {
			m[d.snapshot.Ops[i].OpID] = &d.snapshot.Ops[i]
		}
		// Aliases second so a canonical id is never shadowed by an alias.
		for i := range d.snapshot.Ops {
			op := &d.snapshot.Ops[i]
			for _, alias := range op.DeprecatedOpIDs {
				if _, exists := m[alias]; !exists {
					m[alias] = op
				}
			}
		}
		d.opIndex = m
	})
	return d.opIndex
}

// CacheStats returns a live snapshot of the semantic cache counters for
// gum.cache_stats (spec §3003). Returns zero-value if no cache is wired.
// SemanticCache (§10.3) wins over the legacy MemCache when both are set.
func (d *dispatcher) CacheStats() CacheLayerStats {
	if d.semanticCache != nil {
		s := d.semanticCache.Stats()
		return CacheLayerStats{
			Hits:      s.Hits,
			Misses:    s.Misses,
			Evictions: s.Evictions,
			Entries:   int64(d.semanticCache.Len()),
			Bytes:     d.semanticCache.Bytes(),
		}
	}
	if d.cache == nil {
		return CacheLayerStats{}
	}
	s := d.cache.Stats()
	return CacheLayerStats{
		Hits:      s.Hits,
		Misses:    s.Misses,
		Evictions: s.Evictions,
		Entries:   int64(d.cache.Len()),
		Bytes:     d.cache.Bytes(),
	}
}

// semanticAuthFP returns the active auth-subject fingerprint for cache
// keying. The dispatcher prefers the explicit inv.AuthSubjectFingerprint
// (set by gum_parallel and other batch callers); falls back to the resolved
// credentials. Empty string is acceptable — spec §10.3 keys still hash
// distinctly when the FP is missing.
func semanticAuthFP(inv *Invocation, creds *Credentials) string {
	if inv != nil && inv.AuthSubjectFingerprint != "" {
		return inv.AuthSubjectFingerprint
	}
	if creds != nil && creds.SubjectFingerprint != "" {
		return creds.SubjectFingerprint
	}
	return ""
}

// semanticFields returns the active field-mask projection for cache keying.
// Composes Projection + KeepFields when present (the upstream + post-shaping
// signals that fully determine the returned payload shape); empty string
// falls back gracefully. Spec §10.3 keys on whatever the LLM asked for so
// two requests with different projections of the same upstream payload
// don't collide.
func semanticFields(inv *Invocation) string {
	if inv == nil || inv.OutputProfile == nil {
		return ""
	}
	p := inv.OutputProfile
	if len(p.Projection) == 0 && len(p.KeepFields) == 0 {
		return ""
	}
	// The two lists are kept in separate, labelled groups. Concatenating and
	// sorting them collapsed projection=["a"] keep=["b"] and projection=["b"]
	// keep=["a"] to the same "a,b" key, and because the cache stores the
	// upstream body (masked by the adapter to the projection), a warm call
	// under one profile was served the other profile's narrower body.
	return "p=" + joinSorted(p.Projection) + ";k=" + joinSorted(p.KeepFields)
}

func joinSorted(in []string) string {
	if len(in) == 0 {
		return ""
	}
	sorted := make([]string, len(in))
	copy(sorted, in)
	sort.Strings(sorted)
	return strings.Join(sorted, ",")
}

// Dispatch drives all 9 lifecycle steps and guarantees the §7 envelope.
//
// dispatchSteps holds the lifecycle. It can return a raw error, because an
// adapter or an injected collaborator is free to fail with plain fmt.Errorf,
// and such an error used to travel out of the kernel unwrapped: the caller got
// free text with no error_code, no retryable flag, and nothing to branch on.
// wrapKernelError is the single place that closes that hole.
func (d *dispatcher) Dispatch(ctx context.Context, inv *Invocation) (*ShapedResponse, error) {
	var resolved *ResolvedVariant
	shaped, err := d.dispatchSteps(ctx, inv, &resolved)
	if err != nil {
		wrapped := wrapKernelError(inv, err)
		// Step 9 never ran, so the §12.3 entry for this call is written here.
		d.recordFailedDispatch(inv, resolved, wrapped)
		return shaped, wrapped
	}
	return shaped, nil
}

// dispatchSteps is the lifecycle body. Call Dispatch, not this.
//
// resolved receives the variant step 3 picked, so Dispatch can name it on the
// error path. It stays nil when the call fails before routing. An out-param
// carries it because the body has some thirty error returns, and threading a
// third result through all of them would say nothing the one assignment below
// does not.
func (d *dispatcher) dispatchSteps(ctx context.Context, inv *Invocation, resolved **ResolvedVariant) (*ShapedResponse, error) {
	requestID := inv.RequestID
	if requestID == "" {
		requestID = newRequestID()
	}

	// Spec §14.1 rule 4 — every dispatch event entry MUST carry event, op_id,
	// variant_id_resolved, risk_class, caller, duration_ms. rvRef is updated
	// in place after step 3 so post-resolution entries surface the variant.
	var rvRef *ResolvedVariant
	logEvent := func(event DispatchEvent, start time.Time) {
		variantID := ""
		riskClass := ""
		if rvRef != nil && rvRef.Variant != nil {
			variantID = rvRef.Variant.VariantID
			riskClass = string(rvRef.Variant.RiskClass)
		}
		d.log().Debug("dispatch event",
			"event", string(event),
			"request_id", requestID,
			"op_id", inv.OpID,
			"variant_id_resolved", variantID,
			"risk_class", riskClass,
			"caller", string(inv.Caller),
			"duration_ms", time.Since(start).Milliseconds(),
		)
	}

	// Step 1: parse and validate
	t0 := time.Now()
	parsed, serr := d.parseAndValidate(ctx, inv)
	if serr != nil {
		logEvent(EventParseAndValidate, t0)
		return nil, serr
	}
	logEvent(EventParseAndValidate, t0)
	if err := checkCancelled(ctx, "parse_and_validate"); err != nil {
		return nil, err
	}
	var validationWarnings []string
	if parsed != nil {
		validationWarnings = parsed.ValidationWarnings
	}

	// Step 2: evaluate policy
	t0 = time.Now()
	if serr2 := d.evaluatePolicy(ctx, inv); serr2 != nil {
		logEvent(EventEvaluatePolicy, t0)
		return nil, serr2
	}
	logEvent(EventEvaluatePolicy, t0)
	if err := checkCancelled(ctx, "evaluate_policy"); err != nil {
		return nil, err
	}

	// Step 3: resolve variant (routing)
	t0 = time.Now()
	rv, serr3 := d.resolveVariant(ctx, inv)
	if serr3 != nil {
		logEvent(EventResolveVariant, t0)
		return nil, serr3
	}
	rvRef = rv // post-resolution entries inherit variant_id_resolved + risk_class
	if resolved != nil {
		*resolved = rv
	}
	logEvent(EventResolveVariant, t0)
	if err := checkCancelled(ctx, "resolve_variant"); err != nil {
		return nil, err
	}
	if serr := d.enforceAdminFixtureOwnership(inv, rv); serr != nil {
		return nil, serr
	}

	// Step 3a-pre: spec §927 capability gate. A typed_executor_required or
	// schema_only variant is describable but not invokable, and the refusal
	// must land before auth, before the rate limiter, and before any upstream
	// request.
	if serr := capabilityGate(inv, rv); serr != nil {
		return nil, serr
	}

	// Step 3a: resolve the catalog-embedded (§9.2 third-layer) expression
	// profile. A presentation layer may have already set inv.OutputProfile from
	// the project-local / user-global filesystem layers (which take precedence);
	// only when it is unset do we look up the resolved variant's output_profile
	// name via the injected ProfileLookup. A miss leaves it nil → default
	// (empty-profile) shaping, so ops without a defined profile are unchanged.
	// Resolved here (before tee + shape) so recovery/tee and step-8 shaping agree.
	if inv.OutputProfile == nil && d.profileLookup != nil && rv.Variant != nil {
		name := d.profileNameFor(inv, rv)
		if name != "" {
			if p, ok := d.profileLookup(name); ok && p != nil {
				inv.OutputProfile = p
			}
		}
	}

	// Step 3b: spec §9.1 field_mask_mode="dual_fetch" eligibility gate.
	//
	// dual_fetch issues a second upstream request, so it is restricted to
	// variants that are safe to call twice: risk_class=read AND
	// annotations.idempotent=true. The catalog generator rejects the rest at
	// build time; this guards profiles authored independently of the catalog
	// (a user-global or project-local override).
	//
	// It runs before the mask injection below so an ineligible variant is
	// refused before any upstream request, and reports the constraint it broke
	// rather than a generic mode error.
	if inv.OutputProfile != nil && inv.OutputProfile.FieldMaskMode == profile.FieldMaskModeDualFetch {
		if gateErr := profile.ValidateDualFetchGate(inv.OutputProfile.FieldMaskMode, rv.Variant); gateErr != nil {
			return nil, NewStructuredError(ErrCodeInvalidArgs, "field_mask_mode=dual_fetch rejected: "+gateErr.Error()).
				WithDetail("field", "field_mask_mode").
				WithDetail("value", inv.OutputProfile.FieldMaskMode).
				WithDetail("op_id", inv.OpID).
				WithDetail("variant_id", rv.Variant.VariantID)
		}
	}

	// Step 3c: spec §9.1 stage 1, upstream projection. The only mask gum puts
	// on the wire is the universal Google `fields` query parameter, so the
	// profile's field_mask is injected as that arg. It runs before step 4 auth
	// and step 5 cache so the mask is part of the §10.3 cache key: two calls
	// that differ only by mask must not share a cached body.
	//
	// The §9.1 DSL field table defaults `field_mask` to the variant's
	// `default_fields`, so a profile that omits the key still projects. Without
	// the fallback the documented default never fired and an omitted key was
	// field_mask_mode="none" on the wire (gum-jnxs).
	//
	// Three things veto the injection. An explicit caller `fields` arg wins,
	// because it is the narrower instruction. field_mask_mode="none" selects
	// host-side shaping only. SuppressFieldMask is --no-field-mask, which
	// already deleted any caller mask and must not have one put back.
	if inv.OutputProfile != nil && !inv.SuppressFieldMask &&
		inv.OutputProfile.FieldMaskMode != profile.FieldMaskModeNone {
		mask := inv.OutputProfile.FieldMask
		if mask == "" && rv.Variant != nil {
			mask = rv.Variant.DefaultFields
		}
		if mask != "" {
			// inv.Args is never nil here: step 1 (parseAndValidate) normalizes it.
			if existing, ok := inv.Args["fields"].(string); !ok || strings.TrimSpace(existing) == "" {
				inv.Args["fields"] = mask
			}
		}
	}

	// Step 4: resolve auth — BEFORE the cache lookup. Spec §3.1 lists cache
	// before auth, but the §10.3 semantic cache key includes the auth-subject
	// fingerprint, which is not known until auth resolves (gum-vd63.1). With the
	// old order the step-4 lookup keyed on an empty fingerprint while the step-7b
	// store keyed on the resolved one, so authenticated reads NEVER hit. Resolving
	// auth first makes the lookup key match the store key, and makes a cache hit
	// correctly require proving you are that principal rather than serving a prior
	// principal's response. The token bucket still runs only on a miss (below), so
	// hits do not consume quota. (Divergence tracked in docs/known-divergences.md.)
	t0 = time.Now()
	creds, err := d.resolveAuth(ctx, inv, rv)
	if err != nil {
		logEvent(EventResolveAuth, t0)
		return nil, err
	}
	logEvent(EventResolveAuth, t0)
	if err := checkCancelled(ctx, "resolve_auth"); err != nil {
		return nil, err
	}

	// Step 5: cache check, keyed on the resolved principal.
	t0 = time.Now()
	cached, hit, err := d.cacheCheck(ctx, inv, rv, creds)
	if err != nil {
		logEvent(EventCacheCheck, t0)
		return nil, err
	}
	logEvent(EventCacheCheck, t0)
	if hit && cached != nil {
		// The cache stores the raw executor body (step 7b), so a hit must run the
		// same step-8 pipeline the cold path runs. Returning cached.Body verbatim
		// made an identical warm call answer with unshaped upstream JSON in a
		// format the caller did not ask for (gum-n110).
		cachedResp := &Response{Body: cached.Body, Format: cached.Format}
		shaped, serr := d.shapeResponse(ctx, inv, rv, cachedResp)
		if serr != nil {
			// A bad inv.Format is the caller's error either way: surface it so a
			// warm call reports the same INVALID_ARGS a cold call reports.
			var se *StructuredError
			if errors.As(serr, &se) {
				return nil, serr
			}
			// A cached body the profile cannot shape (opaque bytes from an
			// adapter whose Format the cache did not preserve) must not fail a
			// call that would have succeeded cold: serve it verbatim.
			d.log().Warn("cached body failed shaping; serving verbatim", "op_id", inv.OpID, "err", serr)
			shaped = &ShapedResponse{
				Body:   cached.Body,
				Format: cached.Format,
				// Nothing was shaped, so the envelope reports the "_raw"
				// sentinel rather than naming a profile that did not run.
				Expression: newExpressionMeta(inv, rv, nil, &profile.ApplyOutput{Format: "raw"}),
			}
			var structured any
			if json.Unmarshal(cached.Body, &structured) == nil {
				shaped.StructuredContent = structured
			}
		}
		// Step 7c on the warm path. A hit fires the same expression profile as a
		// cold call, so it drops the same fields and spec §9.0 owes it the same
		// recovery artifact. Skipping it left the warm response naming dropped
		// paths with nothing to recover them from, and a resource_link profile
		// answered with no link at all. Writing after shaping keeps a rejected
		// inv.Format from leaving a stray artifact behind. A cached response
		// carries no StatusCode, so tee_mode="failures" correctly writes
		// nothing: a served cache entry is a successful read.
		// Spec §9.1 second fetch on the warm path. The cache key carries the
		// mask, so a hit returns the masked body; teeing it under a
		// field_mask_mode="dual_fetch" profile would hand back a
		// full_result_path claiming pre-mask data it does not hold. The
		// recovery request is what the mode buys, warm or cold, so it runs
		// here too — and takes its own rate-limit token, because unlike the
		// served hit it is a real upstream call.
		var warmDual *dualFetchResult
		if d.dualFetchWanted(inv) {
			warmDual = d.runDualFetch(ctx, inv, rv, creds)
		}
		teeArt, terr := d.writeTeeArtifact(inv, rv, creds, warmDual.teeSource(cachedResp))
		if terr != nil {
			d.log().Warn("tee artifact write failed", "op_id", inv.OpID, "err", terr)
		}
		d.attachTeeHandles(shaped, teeArt)
		shaped.ValidationWarnings = append(shaped.ValidationWarnings, validationWarnings...)
		warmDual.warn(shaped)
		// A served hit was by definition cache-eligible, so the ledger reports
		// "hit" rather than the "not_applicable" an ineligible op would get.
		warmResult, warmErr := d.recordAndReturn(ctx, inv, rv, creds, shaped, cachedResp, true, true)
		d.appendDualFetchAudit(warmDual)
		return warmResult, warmErr
	}
	if err := checkCancelled(ctx, "cache_check"); err != nil {
		return nil, err
	}

	// Step 5b: the §10.2 HTTP/ETag cache. The §10.3 lookup above missed, so
	// this call is going upstream either way; a stored validator turns it into
	// a conditional request that may come back as a bodiless 304. The key
	// needs the resolved variant (§3.1 step 2) and the credential subject
	// (§10.0.1), both of which the steps above have produced.
	var httpKey string
	var validator cache.HTTPEntry
	if d.httpCacheable(rv) {
		httpKey = d.httpCacheKey(inv, rv, creds)
		if stored, ok := d.httpCache.Lookup(httpKey); ok {
			validator = stored
			inv.IfNoneMatch = stored.ETag
		}
	}

	// Step 6: token bucket
	t0 = time.Now()
	if err := d.tokenBucketStep(ctx, inv, rv); err != nil {
		logEvent(EventTokenBucket, t0)
		return nil, mapRateLimited(err)
	}
	logEvent(EventTokenBucket, t0)
	if err := checkCancelled(ctx, "token_bucket"); err != nil {
		return nil, err
	}

	// Step 7: execute adapter
	t0 = time.Now()
	resp, err := d.executeAdapter(ctx, inv, rv, creds)
	if err != nil {
		logEvent(EventExecuteAdapter, t0)
		// Step 7c, failure branch. tee_mode = "failures" exists for exactly this
		// path, and the success-side write below is unreachable from here. Only
		// step-7 errors reach this line, which is what keeps the normative
		// pre-step-7 exclusion in docs/expression-profile-dsl.md true.
		d.writeFailureTee(inv, rv, creds, resp, err)
		// An adapter that returns BOTH a non-nil response body AND an error has
		// packed a structured error envelope into the body (the plugin path).
		// Surface its error_code/retryable/retry_after_ms instead of dropping
		// resp and letting only the opaque error string through.
		if resp != nil {
			if se := structuredErrorFromEnvelope(resp.Body); se != nil {
				return nil, se
			}
		}
		return nil, mapRateLimited(err)
	}
	logEvent(EventExecuteAdapter, t0)

	// Step 7a1: §2024. Upstream says the copy gum already holds is current, so
	// the answer is the validator alone. Returning here is what skips stages
	// 1-8, the field mask, the tee artifact and the results handle; every one
	// of those lives below this line.
	if isNotModified(resp, validator) {
		return d.serveNotModified(inv, rv, creds, validator)
	}

	// Step 7a2: spec §9.1 second, unmasked fetch. It runs after the shaped
	// request succeeds, because a failed first request has nothing to recover
	// and the caller is already getting an error. It runs before step 7b so
	// the cache still stores the masked body the masked key names, and before
	// step 7c so stage 9 can write the unmasked payload instead of it.
	var dual *dualFetchResult
	if d.dualFetchWanted(inv) {
		dual = d.runDualFetch(ctx, inv, rv, creds)
	}

	// Step 7b: store successful response in cache.
	// SemanticCache (spec §10.3) is preferred; legacy MemCache stays for
	// callers that don't wire the semantic layer yet. Per-op TTL applies
	// only to SemanticCache; MemCache uses its constructor TTL.
	//
	// Only READ-class responses are cacheable: caching a write/destructive
	// response could serve a stale "success" for a later identical-arg call that
	// never actually ran (e.g. a duplicate send returning the prior message id
	// without sending). The §10.3 cache is a read-response cache.
	//
	// Format "raw" responses (opaque executor bytes, e.g. gum.code printed
	// output) are excluded: the cache stores only bytes, so a hit would come
	// back labelled "json" and hit the step-8 fallback on every warm call
	// (gum-n110).
	cacheable := rv != nil && rv.Variant != nil && rv.Variant.RiskClass == catalog.RiskClassRead &&
		resp != nil && resp.Format != "raw"
	if cacheable && d.semanticCache != nil {
		key := cache.SemanticKey(
			inv.OpID,
			rv.Variant.VariantID,
			canonicalizeArgs(d.canonicalArgs(inv.Args)),
			semanticFields(inv),
			semanticAuthFP(inv, creds),
		)
		d.semanticCache.Set(key, resp.Body, inv.OpID)
	} else if cacheable && d.cache != nil && d.auth == nil {
		// Legacy MemCache only: its KeyFor key has no auth-subject component, so
		// it would serve one principal's response to another. Restrict it to the
		// unauthenticated case; an authed dispatcher must use SemanticCache,
		// whose key includes the principal fingerprint (review gum-t8x1).
		key := cache.KeyFor(inv.OpID, canonicalizeArgs(d.canonicalArgs(inv.Args)), "", rv.Variant.VariantID)
		d.cache.Set(key, resp.Body)
	}

	// Step 7b2: store the §10.2 validator. Upstream sent an ETag, so the next
	// identical call revalidates instead of re-downloading. The body is stored
	// with it because a 304 carries none and the ledger needs the size of the
	// response the caller did not have to receive.
	if httpKey != "" && resp != nil && resp.ETag != "" {
		if err := d.httpCache.Store(httpKey, cache.HTTPEntry{
			ETag:   resp.ETag,
			Body:   resp.Body,
			Format: resp.Format,
			OpID:   inv.OpID,
		}); err != nil {
			d.log().Warn("http etag cache store failed", "op_id", inv.OpID, "err", err)
		}
	}

	// Step 7c: filesystem tee artifact (spec §9.0 stage 'artifact'). Writes
	// the post-upstream-projection payload before host-shaping so the recovery
	// handles can be projected into the §9.0 _expression envelope.
	teeArt, terr := d.writeTeeArtifact(inv, rv, creds, dual.teeSource(resp))
	if terr != nil {
		d.log().Warn("tee artifact write failed", "op_id", inv.OpID, "err", terr)
	}

	// Step 8: shape response
	t0 = time.Now()
	shaped, err := d.shapeResponse(ctx, inv, rv, resp)
	if err != nil {
		logEvent(EventShapeResponse, t0)
		return nil, err
	}
	d.attachTeeHandles(shaped, teeArt)
	if shaped != nil && len(validationWarnings) > 0 {
		shaped.ValidationWarnings = append(shaped.ValidationWarnings, validationWarnings...)
	}
	logEvent(EventShapeResponse, t0)

	dual.warn(shaped)

	// Step 9: record and return.
	t0 = time.Now()
	result, err := d.recordAndReturn(ctx, inv, rv, creds, shaped, resp, false, cacheable && d.cacheConfigured())
	logEvent(EventRecordAndReturn, t0)
	// The §9.1 unmasked request is audited after the masked one it followed,
	// so the log reads in request order. recordAndReturn holds the masked
	// entry until step 9, so appending earlier would have inverted them.
	d.appendDualFetchAudit(dual)
	return result, err
}

func (d *dispatcher) enforceAdminFixtureOwnership(inv *Invocation, rv *ResolvedVariant) *StructuredError {
	if rv == nil || rv.Variant == nil || rv.Variant.AdminPolicy == nil || !rv.Variant.AdminPolicy.FixtureOwnershipRequired {
		return nil
	}
	if err := catalog.ValidateAdminFixtureOwnership(inv.Args, rv.Variant.AdminPolicy); err != nil {
		msg := "admin fixture ownership required"
		if errors.Is(err, catalog.ErrAdminFixtureOwnership) {
			msg = "admin fixture ownership violation"
		}
		return NewStructuredError(ErrCodePolicyDenied, msg).
			WithDetail("op_id", inv.OpID).
			WithDetail("variant_id", rv.Variant.VariantID).
			WithDetail("blast_radius", string(rv.Variant.AdminPolicy.BlastRadius)).
			WithDetail("fixture_marker_prefix", rv.Variant.AdminPolicy.FixtureMarkerPrefix).
			WithDetail("fixture_resource_keys", rv.Variant.AdminPolicy.FixtureResourceKeys)
	}
	return nil
}

// parsedInvocation is the output of step 1: alias-resolved op_id, nil-safe args, and a
// deterministic ArgsHash for use as a cache key / confirmation token binding.
type parsedInvocation struct {
	OpID     string         // canonical (alias-resolved) op_id
	Args     map[string]any // nil-safe normalized copy
	ArgsHash string         // SHA-256 hex of JCS-canonical args
	// ValidationWarnings carries spec §5.7 read-only allowlist pass-through
	// notices. Each entry is a human-readable string surfaced to the caller via
	// the response envelope's `_validation_warnings` field. Empty when no
	// allowlist applied or no unknown args appeared.
	ValidationWarnings []string
}

// findOp returns the catalog Op whose OpID or DeprecatedOpIDs matches opID.
// Exact match is preferred; alias scan is the fallback. Returns nil if not found
// or if the snapshot is nil.
func (d *dispatcher) findOp(opID string) *catalog.Op {
	// O(1) lookup over a lazily-built index (canonical ids + aliases). Behavior
	// matches the previous two-pass scan: exact canonical match wins, then alias.
	return d.opByID()[opID]
}

// opIDCandidates returns every canonical op_id in the snapshot, used as the
// search space for the OP_NOT_FOUND "did you mean" suggestions. Returns nil
// when the snapshot is absent (suggestOpIDs then yields an empty slice).
func (d *dispatcher) opIDCandidates() []string {
	if d.snapshot == nil {
		return nil
	}
	ids := make([]string, 0, len(d.snapshot.Ops))
	for i := range d.snapshot.Ops {
		ids = append(ids, d.snapshot.Ops[i].OpID)
	}
	return ids
}

// validateParams checks required/optional param declarations against inv.Args.
// It returns (missing, unknown, typeErrors) slices, all pre-sorted.
// When the op declares no params the schema is open (all slices nil).
// emptyStrings returns a non-nil slice so JSON marshals [] instead of null for
// empty error-detail arrays (review gum-s985).
func emptyStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// bodyArgKey mirrors adapters.BodyArgKey — the reserved Args key carrying the
// JSON request body. Declared locally because internal/dispatch must not import
// internal/adapters (the dependency runs the other way).
const bodyArgKey = "body"

// applyFieldDefaults fills in the args the caller omitted from the op's
// request-field defaults, mutating args in place. It runs before validateParams
// so an injected default is part of the validated args, the ArgsHash, the cache
// key, and the audit record — the same value the adapter puts on the wire.
//
// Before gum-3gcv nothing read catalog.RequestField.Default, so `gum describe`
// advertised defaults the dispatcher never sent and a caller who trusted the
// listed default got different data with no error. Applying the key here makes
// the declaration load-bearing: what a field declares is what gets sent.
//
// Rules:
//   - An arg the caller supplied is never overwritten, including an explicit
//     null and an explicit body field of the same name.
//   - A body-location default goes into the reserved "body" arg, because that is
//     where validateParams and the executors expect body fields. A non-object
//     explicit body is left untouched.
//   - A required field's default is skipped: absence must stay a clean
//     INVALID_ARGS instead of becoming a silent placeholder. The catalog
//     invariant test rejects the required+default combination outright.
//
// It returns validation warnings for defaults it could not decode (possible for
// a plugin-supplied catalog op, which no build-time test gates); an undecodable
// default is skipped rather than failing the call.
func applyFieldDefaults(op *catalog.Op, args map[string]any) []string {
	var warnings []string
	var body map[string]any
	bodyResolved := false

	for _, f := range op.RequestFields {
		if !f.HasDefault() || f.Required {
			continue
		}
		if f.Location == catalog.RequestFieldBody {
			if !bodyResolved {
				body = existingBodyMap(args)
				bodyResolved = true
			}
			if body == nil {
				continue // explicit non-object body wins; don't second-guess it
			}
			if _, provided := body[f.Name]; provided {
				continue
			}
			v, err := f.DefaultValue()
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("op %s: skipped undecodable default for body field %q: %v", op.OpID, f.Name, err))
				continue
			}
			body[f.Name] = v
			args[bodyArgKey] = body
			continue
		}
		if _, provided := args[f.Name]; provided {
			continue
		}
		v, err := f.DefaultValue()
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("op %s: skipped undecodable default for arg %q: %v", op.OpID, f.Name, err))
			continue
		}
		args[f.Name] = v
	}
	return warnings
}

// existingBodyMap returns the args' body object, creating an empty one when the
// arg is absent. It returns nil when the caller passed a body that is not a JSON
// object, which signals the caller to leave the body alone.
func existingBodyMap(args map[string]any) map[string]any {
	existing, ok := args[bodyArgKey]
	if !ok || existing == nil {
		return map[string]any{}
	}
	if m, isMap := existing.(map[string]any); isMap {
		return m
	}
	return nil
}

// applyArgDefaulter fills omitted top-level args from the configured
// ArgDefaulter. A key the caller supplied always wins, including an explicit
// null or empty string: that is how a caller opts out of a default per call.
func (d *dispatcher) applyArgDefaulter(op *catalog.Op, args map[string]any) *StructuredError {
	if d.argDefaulter == nil {
		return nil
	}
	defaults, err := d.argDefaulter.ArgDefaults(op, args)
	if err != nil {
		return NewStructuredError(ErrCodeInvalidArgs, err.Error()).
			WithDetail("missing", []string{}).
			WithDetail("unknown", []string{}).
			WithDetail("type_errors", []string{}).
			WithDetail("hint", "Fix or unset the configured default named in the message, or pass the arg explicitly.")
	}
	for name, v := range defaults {
		if _, provided := args[name]; provided {
			continue
		}
		args[name] = v
	}
	return nil
}

// missingArgHint asks the ArgDefaulter how to supply required args that are
// still missing after all defaults.
func (d *dispatcher) missingArgHint(op *catalog.Op, missing []string) string {
	if d.argDefaulter == nil || len(missing) == 0 {
		return ""
	}
	return d.argDefaulter.MissingArgHint(op, missing)
}

// AllowedArgKeys returns every top-level arg name op accepts: its declared
// params, its non-body RequestFields, the reserved "body" key when it carries
// any body-located field, plus the permanent allowlist below. A nil result
// means the op declares no schema at all, so every key is allowed.
//
// validateParams and the MCP convenience-schema gate both read this set, so a
// schema cannot advertise a property the kernel would reject as unknown.
func AllowedArgKeys(op *catalog.Op) map[string]struct{} {
	hasParams := len(op.ParamsRequired) > 0 || len(op.ParamsOptional) > 0
	hasFields := len(op.RequestFields) > 0
	if !hasParams && !hasFields {
		// Truly open schema: the op declares no params and no RequestFields, so
		// accept any args (e.g. searchconsole.sites.list, calendar.colors.get).
		return nil
	}
	// When the op has RequestFields but no hand-authored params lists (the 84
	// Discovery-enriched ops), the RequestFields below form the allowed set, so
	// unknown args (typos like emailq=foo) are rejected locally instead of being
	// forwarded to Google as spurious query params (gum-gatw).
	allowed := make(map[string]struct{}, len(op.ParamsRequired)+len(op.ParamsOptional)+len(op.RequestFields)+len(permanentArgKeys))
	for _, pair := range op.ParamsRequired {
		if len(pair) == 2 {
			allowed[pair[0]] = struct{}{}
		}
	}
	for _, pair := range op.ParamsOptional {
		if len(pair) == 2 {
			allowed[pair[0]] = struct{}{}
		}
	}
	// RequestFields are an authoritative param set (derived from the API
	// Discovery doc or hand-curated), so always admit them — otherwise a typed
	// flag / key=value for a real parameter would be rejected as "unknown" when
	// the op also declares a narrower, hand-authored params_optional allowlist.
	// Body-located fields are assembled into the reserved "body" arg.
	for _, f := range op.RequestFields {
		if f.Location == catalog.RequestFieldBody {
			allowed[bodyArgKey] = struct{}{}
		} else {
			allowed[f.Name] = struct{}{}
		}
	}
	for _, k := range permanentArgKeys {
		allowed[k] = struct{}{}
	}
	return allowed
}

// permanentArgKeys are always valid but absent from the per-op schema, so they
// must never be flagged unknown.
//
//	(a) Host-control keys the CLI (call.go) and MCP handler (handlers.go)
//	    inject into Args AFTER consulting the catalog schema:
//	      body       — POST ops carry a body from body:=json even when no
//	                   body-location RequestField exists.
//	      pageToken  — pagination continuation (--page-token).
//	      pageSize   — pagination size for newer Google APIs (--page-size).
//	      maxResults — pagination size for older Google APIs (--page-size).
//
//	(b) Google API global "system parameters" — valid on EVERY method but
//	    listed only at the top level of the Discovery doc, not per-method, so
//	    the Discovery walker never puts them in RequestFields. Rejecting them
//	    would wrongly fail a valid call (e.g. alt=json, quotaUser=…) on the
//	    84 enriched ops. (fields is both a host-control flag and a system
//	    parameter.) See cloud.google.com/apis/docs/system-parameters.
var permanentArgKeys = []string{
	"body", "pageToken", "pageSize", "maxResults",
	"alt", "fields", "prettyPrint", "quotaUser", "userIp", "key",
	"oauth_token", "access_token", "callback", "uploadType",
	"upload_protocol", "$.xgafv",
}

func validateParams(op *catalog.Op, args map[string]any) (missing, unknown, typeErrors []string) {
	allowed := AllowedArgKeys(op)
	if allowed == nil {
		return nil, nil, nil
	}

	// Check required params: presence + type.
	for _, pair := range op.ParamsRequired {
		if len(pair) != 2 {
			continue
		}
		name, declType := pair[0], pair[1]
		val, provided := args[name]
		if !provided {
			missing = append(missing, name)
			continue
		}
		if msg := checkArgType(name, val, declType); msg != "" {
			typeErrors = append(typeErrors, msg)
		}
	}

	// Check optional params: type only (absence is fine).
	for _, pair := range op.ParamsOptional {
		if len(pair) != 2 {
			continue
		}
		name, declType := pair[0], pair[1]
		if val, provided := args[name]; provided {
			if msg := checkArgType(name, val, declType); msg != "" {
				typeErrors = append(typeErrors, msg)
			}
		}
	}

	// For the Discovery-enriched ops (RequestFields present, no hand-authored
	// params lists), enforce presence of required PATH parameters: they are
	// structurally required — the URL template cannot be built without them — so
	// a missing one becomes a clean local INVALID_ARGS instead of a malformed URL
	// or an opaque upstream 400 (matters most on the MCP path, which has no CLI
	// wizard). Only path location is enforced: Discovery's `required` flag on
	// query params is less reliable, and body fields live under "body".
	if len(op.RequestFields) > 0 {
		missingSet := make(map[string]struct{}, len(missing))
		for _, m := range missing {
			missingSet[m] = struct{}{}
		}
		for _, f := range op.RequestFields {
			if f.Location != catalog.RequestFieldPath || !f.Required {
				continue
			}
			if _, provided := args[f.Name]; provided {
				continue
			}
			if _, already := missingSet[f.Name]; already {
				continue
			}
			missing = append(missing, f.Name)
			missingSet[f.Name] = struct{}{}
		}
	}

	// Check for unknown keys.
	for k := range args {
		if _, ok := allowed[k]; !ok {
			unknown = append(unknown, k)
		}
	}

	sort.Strings(missing)
	sort.Strings(unknown)
	sort.Strings(typeErrors)
	return missing, unknown, typeErrors
}

// Step 1 — parse and validate.
//
// Responsibilities (spec §3.1 step 1–2, §4.1, §5.3, §8.42):
//  1. Normalize nil args to {}.
//  2. Resolve op_id: exact match, then alias scan via Op.DeprecatedOpIDs.
//  3. Apply configured defaults, then request-field defaults, for args the
//     caller omitted.
//  4. Validate args against params_required / params_optional; aggregate ALL errors.
//  5. Compute ArgsHash.
//
// Side-effect: if an alias is resolved, inv.OpID is updated to the canonical id
// so that downstream steps (evaluatePolicy, resolveVariant, …) see the canonical id.
func (d *dispatcher) parseAndValidate(ctx context.Context, inv *Invocation) (*parsedInvocation, *StructuredError) {
	// 1. Normalize nil args.
	if inv.Args == nil {
		inv.Args = map[string]any{}
	}

	if inv.OpID == "" {
		return nil, NewStructuredError(ErrCodeInvalidArgs, "op_id is required").
			WithDetail("missing", []string{"op_id"}).
			WithDetail("unknown", []string{}).
			WithDetail("type_errors", []string{})
	}

	// 2. Resolve op_id (exact match first, then alias scan).
	resolvedOp := d.findOp(inv.OpID)
	if resolvedOp == nil {
		return nil, NewStructuredError(ErrCodeOpNotFound, fmt.Sprintf("op not found: %s", inv.OpID)).
			WithDetail("op_id", inv.OpID).
			WithDetail("suggestions", suggestOpIDs(inv.OpID, d.opIDCandidates(), MaxOpSuggestions))
	}

	// Mutate inv.OpID to the canonical id (so downstream steps see it).
	inv.OpID = resolvedOp.OpID

	// 3. Apply defaults before validation, so validation, the ArgsHash, the
	// cache key, and the audit record all see the args that actually go on the
	// wire (gum-3gcv). Configured defaults go first so they rank above catalog
	// defaults (gum-puum).
	if serr := d.applyArgDefaulter(resolvedOp, inv.Args); serr != nil {
		return nil, serr
	}
	var warnings []string
	warnings = append(warnings, applyFieldDefaults(resolvedOp, inv.Args)...)

	// 4. Validate args.
	missing, unknown, typeErrors := validateParams(resolvedOp, inv.Args)

	if len(unknown) > 0 {
		if remaining, warning, applied := applyReadOnlyAllowlist(resolvedOp, &d.profilePolicy, unknown, inv.AllowWrite, inv.AllowDestructive); applied {
			unknown = remaining
			if warning != "" {
				warnings = append(warnings, warning)
			}
		}
	}

	if len(missing) > 0 || len(unknown) > 0 || len(typeErrors) > 0 {
		// Emit [] rather than null for the empty arrays so JS/Python consumers
		// can iterate every field unconditionally (review gum-s985).
		serr := NewStructuredError(ErrCodeInvalidArgs, "invalid arguments").
			WithDetail("missing", emptyStrings(missing)).
			WithDetail("unknown", emptyStrings(unknown)).
			WithDetail("type_errors", emptyStrings(typeErrors))
		if hint := d.missingArgHint(resolvedOp, missing); hint != "" {
			serr = serr.WithDetail("hint", hint)
		}
		return nil, serr
	}

	// 5. Compute ArgsHash. Apply spec §10.0 Rule 4 datetime normalization when
	// enabled so the audit/replay-detection hash matches the cache key.
	canonical := canonicalizeArgs(d.canonicalArgs(inv.Args))
	sum := sha256.Sum256([]byte(canonical))
	argsHash := hex.EncodeToString(sum[:])

	return &parsedInvocation{
		OpID:               inv.OpID,
		Args:               inv.Args,
		ArgsHash:           argsHash,
		ValidationWarnings: warnings,
	}, nil
}

// applyReadOnlyAllowlist consults the §5.7 read-only allowlist escape hatch.
// Returns (remainingUnknown, warning, applied):
//
//   - remainingUnknown: the subset of `unknown` that the allowlist did NOT
//     cover and that should still be rejected by parseAndValidate.
//   - warning: the `_validation_warnings` string surfaced to the caller. Empty
//     when no keys were waived.
//   - applied: true when the allowlist gate fired (even partially). False when
//     the gate is disabled or inapplicable (write/destructive, typed-rest-sdk
//     backend, strict mode on, or no allowlist entry for this op).
//
// The gate is intentionally conservative: a write or destructive invocation,
// or a typed-rest-sdk variant, always reaches the default reject path. Only
// raw-http / discovery-rest read-only variants get the warning pass-through.
func applyReadOnlyAllowlist(op *catalog.Op, policy *ProfilePolicy, unknown []string, allowWrite, allowDestructive bool) ([]string, string, bool) {
	if policy == nil || policy.StrictValidation {
		return unknown, "", false
	}
	if allowWrite || allowDestructive {
		return unknown, "", false
	}
	allowed, ok := policy.UnknownReadParamsAllowlist[op.OpID]
	if !ok || len(allowed) == 0 {
		return unknown, "", false
	}
	// Default-variant must be a long-tail read REST backend. The default
	// variant is the canonical execution path; if it isn't read+raw-http /
	// discovery-rest the allowlist doesn't fire.
	var defaultVariant *catalog.Variant
	for i := range op.Variants {
		if op.Variants[i].VariantID == op.DefaultVariantID {
			defaultVariant = &op.Variants[i]
			break
		}
	}
	if defaultVariant == nil {
		return unknown, "", false
	}
	if defaultVariant.RiskClass != catalog.RiskClassRead {
		return unknown, "", false
	}
	if defaultVariant.BackendKind != catalog.BackendKindRawHTTP && defaultVariant.BackendKind != catalog.BackendKindDiscoveryREST {
		return unknown, "", false
	}
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, k := range allowed {
		allowedSet[k] = struct{}{}
	}
	var waived, remaining []string
	for _, k := range unknown {
		if _, ok := allowedSet[k]; ok {
			waived = append(waived, k)
		} else {
			remaining = append(remaining, k)
		}
	}
	if len(waived) == 0 {
		return unknown, "", false
	}
	warning := fmt.Sprintf("unknown args: %s — not in discovery doc; passed through via read-only allowlist", strings.Join(waived, ", "))
	return remaining, warning, true
}

// checkArgType returns an error message if val does not match the declared type,
// or "" if the value is acceptable.
// declaredArgTypes is the closed vocabulary checkArgType understands. Anything
// outside it silently skips validation, so TestCatalogDeclTypesAreKnown asserts
// the embedded catalog never declares a type that is missing here.
//
// "int" is an accepted spelling of "integer": gum.code declares
// destructive_budget as "int" in the currently embedded catalog while
// cmd/gen-catalog/gen_meta.go now emits "integer". Both must validate, or the
// param loses type checking on whichever side is stale.
var declaredArgTypes = map[string]struct{}{
	"string":   {},
	"integer":  {},
	"int":      {},
	"bool":     {},
	"string[]": {},
}

func checkArgType(name string, val any, declType string) string {
	switch declType {
	case "string":
		if _, ok := val.(string); !ok {
			return fmt.Sprintf("%s: expected string, got %T", name, val)
		}
	case "integer", "int":
		switch v := val.(type) {
		case int, int8, int16, int32, int64,
			uint, uint8, uint16, uint32, uint64:
			// ok
		case float32:
			if msg := checkWholeNumber(name, float64(v)); msg != "" {
				return msg
			}
		case float64:
			// JSON numbers decode to float64, so this is the shape every MCP
			// arg arrives in. 2 must pass and 2.5 must not: forwarding a
			// fractional value upstream turns a locally-detectable mistake into
			// an opaque 400, which is what local validation exists to prevent.
			if msg := checkWholeNumber(name, v); msg != "" {
				return msg
			}
		case string:
			// These params are query/path values (the hand-authored
			// ParamsRequired/ParamsOptional allowlist) — strings on the wire. A
			// `key=value` positional yields a string, so accept one that parses
			// as an integer. This matches the discovery-enriched ops, which
			// type-check nothing, so `maxResults=2` works uniformly.
			if _, err := strconv.ParseInt(v, 10, 64); err != nil {
				return fmt.Sprintf("%s: expected integer, got %q", name, v)
			}
		default:
			return fmt.Sprintf("%s: expected integer, got %T", name, val)
		}
	case "bool":
		switch v := val.(type) {
		case bool:
			// ok
		case string:
			if _, err := strconv.ParseBool(v); err != nil {
				return fmt.Sprintf("%s: expected bool, got %q", name, v)
			}
		default:
			return fmt.Sprintf("%s: expected bool, got %T", name, val)
		}
	case "string[]":
		switch v := val.(type) {
		case []string:
			// ok
		case []any:
			for _, elem := range v {
				if _, ok := elem.(string); !ok {
					return fmt.Sprintf("%s: expected string[] but element is %T", name, elem)
				}
			}
		default:
			return fmt.Sprintf("%s: expected string[], got %T", name, val)
		}
	}
	return ""
}

// checkWholeNumber rejects the float values that cannot stand in for an
// integer: a fractional part, NaN, or an infinity. It reports the value with
// %v so 2.5 reads as 2.5 rather than 2.500000.
func checkWholeNumber(name string, f float64) string {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return fmt.Sprintf("%s: expected integer, got %v", name, f)
	}
	if f != math.Trunc(f) {
		return fmt.Sprintf("%s: expected integer, got %v", name, f)
	}
	return ""
}

// findOpVariant returns the default variant for the given opID, or nil when the
// op is unknown, declares no matching default, or the default is quarantined.
// The quarantine skip mirrors resolveVariant step 1: both must agree on which
// variant runs, or the risk gate reads one variant while dispatch executes
// another.
func (d *dispatcher) findOpVariant(opID string) *catalog.Variant {
	op := d.findOp(opID)
	if op == nil {
		return nil
	}
	for j := range op.Variants {
		v := &op.Variants[j]
		if v.VariantID == op.DefaultVariantID && !v.Quarantined {
			return v
		}
	}
	return nil
}

// policyVariant returns the variant the risk gate (evaluatePolicy) must evaluate
// — the SAME variant resolveVariant will execute. It honors an explicit
// variant_id pin so a caller cannot pin a higher-risk variant past a gate that
// was evaluated on the (lower-risk) default variant. When the pin names an
// unknown variant it returns nil: the gate is skipped, which is safe because
// resolveVariant rejects the call with VARIANT_NOT_FOUND before any execution.
func (d *dispatcher) policyVariant(inv *Invocation) *catalog.Variant {
	op := d.findOp(inv.OpID)
	if op == nil {
		return nil
	}
	if reqID := inv.RequestedVariantID; reqID != "" {
		for j := range op.Variants {
			if op.Variants[j].VariantID == reqID {
				if op.Variants[j].Quarantined {
					// Defer to resolveVariant so a pinned-but-quarantined variant
					// surfaces VARIANT_QUARANTINED, not a risk-gate error.
					return nil
				}
				return &op.Variants[j]
			}
		}
		return nil
	}
	// No explicit pin: gate on the SAME variant resolveVariant will execute.
	// findOpVariant only matches default_variant_id; when that is empty/unmatched
	// resolveVariant falls back to the highest-stability active variant, so the
	// gate must too — otherwise an op with default_variant_id="" could execute a
	// write/destructive fallback variant past a SKIPPED gate (fail-open). This
	// path is unreachable for the embedded catalog (Validate rejects an empty
	// default_variant_id) but defends a hand-built/un-validated catalog.
	if v := d.findOpVariant(inv.OpID); v != nil {
		return v
	}
	active := filterQuarantined(op)
	if len(active) == 0 {
		return nil // resolveVariant returns VARIANT_QUARANTINED before executing
	}
	candidates := pickHighestStabilityGroup(active)
	if len(candidates) == 1 {
		return candidates[0]
	}
	if v := applyInterfaceKindPreference(candidates, d.preferredInterfaceKinds); v != nil {
		return v
	}
	// Still ambiguous: resolveVariant returns AMBIGUOUS_VARIANT (no execution),
	// so deferring the gate here is safe.
	return nil
}

// stabilityRank maps stability strings to a numeric rank for comparison.
// Lower rank = higher preference (stable=0, beta=1, alpha=2, unknown=3).
func stabilityRank(s catalog.Stability) int {
	switch s {
	case catalog.StabilityStable:
		return 0
	case catalog.StabilityBeta:
		return 1
	case catalog.StabilityAlpha:
		return 2
	default:
		return 3
	}
}

// makeResolvedVariant builds a ResolvedVariant for the given op and variant.
// It populates AdapterKey from the binding (if present) and sets Deprecated when
// the variant's ID appears in op.DeprecatedVariantIDs (spec §5.5 rule 2–3).
func makeResolvedVariant(opID string, op *catalog.Op, v *catalog.Variant) *ResolvedVariant {
	adapterKey := ""
	if v.Binding != nil {
		adapterKey = v.Binding.AdapterKey
	}
	deprecated := false
	for _, did := range op.DeprecatedVariantIDs {
		if did == v.VariantID {
			deprecated = true
			break
		}
	}
	return &ResolvedVariant{
		OpID:       opID,
		Variant:    v,
		AdapterKey: adapterKey,
		Deprecated: deprecated,
	}
}

// filterQuarantined returns the non-quarantined variants from the op's variant list.
// If all variants are quarantined it returns nil; the caller should report
// VARIANT_QUARANTINED using the first variant in op.Variants.
func filterQuarantined(op *catalog.Op) []*catalog.Variant {
	active := make([]*catalog.Variant, 0, len(op.Variants))
	for i := range op.Variants {
		if !op.Variants[i].Quarantined {
			active = append(active, &op.Variants[i])
		}
	}
	return active
}

// pickHighestStabilityGroup returns the subset of variants that share the best
// (lowest) stability rank from active. The ordering is stable=0 < beta=1 < alpha=2
// (spec §5.1.1: "stable > beta > alpha").
// active must be non-empty.
func pickHighestStabilityGroup(active []*catalog.Variant) []*catalog.Variant {
	best := stabilityRank(active[0].Stability)
	for _, v := range active[1:] {
		if r := stabilityRank(v.Stability); r < best {
			best = r
		}
	}
	candidates := make([]*catalog.Variant, 0, len(active))
	for _, v := range active {
		if stabilityRank(v.Stability) == best {
			candidates = append(candidates, v)
		}
	}
	return candidates
}

// applyInterfaceKindPreference returns the first candidate whose InterfaceKind
// matches an entry in prefs (checked in order). Returns nil when no candidate
// matches any preference.
func applyInterfaceKindPreference(candidates []*catalog.Variant, prefs []string) *catalog.Variant {
	for _, pref := range prefs {
		for _, v := range candidates {
			if string(v.InterfaceKind) == pref {
				return v
			}
		}
	}
	return nil
}

// Step 3 — resolve variant (routing).
//
// Selection order (spec §3.1 step 3, §5.1.1, §5.5):
//  1. If op.DefaultVariantID is set and matches a variant, return that one
//     (checking quarantine first, then setting Deprecated if needed).
//  2. Filter out quarantined variants; if all variants are quarantined, return
//     VARIANT_QUARANTINED.
//  3. Group remaining variants by stability; pick the group with the best rank
//     (stable > beta > alpha).
//  4. If only one variant in that group, return it.
//  5. If multiple, apply d.preferredInterfaceKinds in order; first match wins.
//  6. If still tied, return AMBIGUOUS_VARIANT.
func (d *dispatcher) resolveVariant(ctx context.Context, inv *Invocation) (*ResolvedVariant, *StructuredError) {
	op := d.findOp(inv.OpID)
	if op == nil {
		return nil, NewStructuredError(ErrCodeOpNotFound, fmt.Sprintf("op not found: %s", inv.OpID)).
			WithDetail("op_id", inv.OpID).
			WithDetail("suggestions", suggestOpIDs(inv.OpID, d.opIDCandidates(), MaxOpSuggestions))
	}
	// A catalog op must declare at least one variant. Guard the [0] indexing
	// below (and the default/stability paths) against a malformed zero-variant
	// op rather than panicking.
	if len(op.Variants) == 0 {
		return nil, NewStructuredError(ErrCodeVariantNotFound,
			fmt.Sprintf("op %s declares no variants", inv.OpID)).
			WithDetail("op_id", inv.OpID)
	}

	// Step 0: explicit variant_id pin (spec §5.1 alias normalization is handled
	// upstream; this branch fires only when the caller supplies a literal
	// variant_id). Selection rules: exact match → quarantine check → return.
	// Unknown variant returns VARIANT_NOT_FOUND so the caller can re-resolve;
	// quarantined returns VARIANT_QUARANTINED.
	if reqID := inv.RequestedVariantID; reqID != "" {
		for i := range op.Variants {
			v := &op.Variants[i]
			if v.VariantID != reqID {
				continue
			}
			if v.Quarantined {
				return nil, NewStructuredError(ErrCodeVariantQuarantined,
					fmt.Sprintf("variant %s is quarantined", v.VariantID)).
					WithDetail("op_id", inv.OpID).
					WithDetail("variant_id", v.VariantID)
			}
			return makeResolvedVariant(inv.OpID, op, v), nil
		}
		return nil, NewStructuredError(ErrCodeVariantNotFound,
			fmt.Sprintf("variant %s not found for op %s", reqID, inv.OpID)).
			WithDetail("op_id", inv.OpID).
			WithDetail("variant_id", reqID)
	}

	// Step 1: if default_variant_id names a variant that can still run, use it.
	// A quarantined default falls through to step 2 instead of ending the call:
	// §5.5 makes "active, non-quarantined" part of what a default is, and
	// quarantine is runtime state, so a catalog that was valid at generation can
	// carry a quarantined default hours later. Refusing the op then would take a
	// healthy sibling down with it.
	if op.DefaultVariantID != "" {
		for i := range op.Variants {
			v := &op.Variants[i]
			if v.VariantID == op.DefaultVariantID && !v.Quarantined {
				return makeResolvedVariant(inv.OpID, op, v), nil
			}
		}
	}

	// Step 2: filter out quarantined variants.
	active := filterQuarantined(op)
	if len(active) == 0 {
		// Name the default when the op has one: that is the variant the caller
		// asked for, even though they never spelled it out.
		blamed := &op.Variants[0]
		for i := range op.Variants {
			if op.Variants[i].VariantID == op.DefaultVariantID {
				blamed = &op.Variants[i]
				break
			}
		}
		return nil, NewStructuredError(ErrCodeVariantQuarantined,
			fmt.Sprintf("variant %s is quarantined", blamed.VariantID)).
			WithDetail("op_id", inv.OpID).
			WithDetail("variant_id", blamed.VariantID)
	}

	// Steps 3–4: pick the highest-stability group; return immediately if unambiguous.
	candidates := pickHighestStabilityGroup(active)
	if len(candidates) == 1 {
		return makeResolvedVariant(inv.OpID, op, candidates[0]), nil
	}

	// Step 5: tie-break by preferred interface_kind order.
	if v := applyInterfaceKindPreference(candidates, d.preferredInterfaceKinds); v != nil {
		return makeResolvedVariant(inv.OpID, op, v), nil
	}

	// Step 6: still tied — AMBIGUOUS_VARIANT.
	variantIDs := make([]string, 0, len(candidates))
	for _, v := range candidates {
		variantIDs = append(variantIDs, v.VariantID)
	}
	return nil, NewStructuredError(ErrCodeAmbiguousVariant,
		fmt.Sprintf("multiple variants available for op %s; specify variant_id or configure PreferredInterfaceKinds", inv.OpID)).
		WithDetail("op_id", inv.OpID).
		WithDetail("variants", variantIDs)
}

// cacheCheck — the §10.3 semantic cache lookup. The key is the 5-tuple
// (op_id, variant_id, args_canonical, fields, auth_subject_fingerprint); the
// legacy MemCache uses the 4-tuple §10.2 form for backward compat. SemanticCache
// wins when both are wired. creds are resolved before this runs (gum-vd63.1), so
// the lookup keys on the SAME auth-subject fingerprint the step-7b store uses —
// otherwise authenticated reads never hit.
// cacheConfigured reports whether any §10.3 cache is wired and usable on this
// dispatcher. A dispatcher with no cache never has a miss to report, so the
// gain ledger records "not_applicable" rather than blaming the op.
func (d *dispatcher) cacheConfigured() bool {
	return d.semanticCache != nil || (d.cache != nil && d.auth == nil)
}

func (d *dispatcher) cacheCheck(ctx context.Context, inv *Invocation, rv *ResolvedVariant, creds *Credentials) (*CachedResponse, bool, error) {
	if d.semanticCache != nil {
		key := cache.SemanticKey(
			inv.OpID,
			rv.Variant.VariantID,
			canonicalizeArgs(d.canonicalArgs(inv.Args)),
			semanticFields(inv),
			semanticAuthFP(inv, creds),
		)
		if val, ok := d.semanticCache.Get(key); ok {
			return &CachedResponse{
				Body:       val,
				Format:     "json",
				CapturedAt: time.Now(),
			}, true, nil
		}
		return nil, false, nil
	}
	// Legacy MemCache lacks a per-principal key component, so it must never
	// serve a response across accounts: skip it whenever an auth resolver is
	// configured (review gum-t8x1). Unauthenticated dispatchers may still use it.
	if d.cache == nil || d.auth != nil {
		return nil, false, nil
	}
	key := cache.KeyFor(inv.OpID, canonicalizeArgs(d.canonicalArgs(inv.Args)), "", rv.Variant.VariantID)
	if val, ok := d.cache.Get(key); ok {
		return &CachedResponse{
			Body:       val,
			Format:     "json",
			CapturedAt: time.Now(),
		}, true, nil
	}
	return nil, false, nil
}

// Step 5 — resolve auth credentials.
//
// Wraps plain resolver errors as AUTH_REQUIRED (spec §3.1 step 5, §1421 stable
// runtime error codes) so downstream surfaces get a consistent structured code.
// Errors that are already structured (e.g. SCOPE_MISSING, the per-strategy
// AUTH_REQUIRED variant from auth/byooauth.go) and context.Canceled /
// context.DeadlineExceeded are passed through unchanged so cancellation can
// propagate and richer codes are not flattened.
func (d *dispatcher) resolveAuth(ctx context.Context, inv *Invocation, rv *ResolvedVariant) (*Credentials, error) {
	if d.auth == nil {
		return nil, nil
	}
	creds, err := d.auth.ResolveAuth(ctx, inv, rv)
	if err == nil {
		if mismatch := d.checkAuthSubject(inv, rv, creds); mismatch != nil {
			return nil, mismatch
		}
		return creds, nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil, err
	}
	var se *StructuredError
	if errors.As(err, &se) {
		return nil, err
	}
	// An *auth.AuthError carries the full §7 envelope but is not a
	// *StructuredError, so errors.As above cannot see it. Ask it for its own
	// envelope rather than flattening four distinct codes into AUTH_REQUIRED.
	var carrier StructuredErrorCarrier
	if errors.As(err, &carrier) {
		if envelope := carrier.AsStructuredError(); envelope != nil {
			return nil, envelope
		}
	}
	return nil, NewStructuredError(ErrCodeAuthRequired, err.Error()).
		WithDetail("op_id", inv.OpID)
}

// checkAuthSubject enforces the spec §7 credential-resolution rule that a
// profile's credential is used "only if its auth_subject_fingerprint matches
// the selected profile's expected subject when one is recorded". The login
// guard in internal/auth catches an interactive account switch; this catches
// every other way a credential reaches the profile, including an edited
// keychain entry and a legacy grant whose refresh-token-derived fingerprint
// moved. Refusing matters beyond reading the wrong mailbox: the fingerprint
// keys the semantic cache, tee artifact paths, recovery URIs and gain-ledger
// rows (§10.0.1), so a silent switch writes rows the profile cannot reach
// again.
//
// The expectation is keyed per auth strategy because the fingerprint
// namespace is per strategy: the same account yields a different value under
// byo_oauth than under gum_oauth. An unlisted strategy, an empty expectation,
// or a credential with no subject at all is not checked (bead gum-q0kd).
func (d *dispatcher) checkAuthSubject(inv *Invocation, rv *ResolvedVariant, creds *Credentials) error {
	if len(d.expectedAuthSubjects) == 0 || creds == nil || creds.SubjectFingerprint == "" {
		return nil
	}

	strategy := ""
	variantID := ""
	if rv != nil && rv.Variant != nil {
		strategy = string(rv.Variant.AuthStrategy)
		variantID = rv.Variant.VariantID
	}

	want := d.expectedAuthSubjects[strategy]
	if want == "" || want == creds.SubjectFingerprint {
		return nil
	}

	// §10.0.1 requires the event on a subject change, and the refusal below is
	// exactly that change detected.
	if d.auditSink != nil {
		d.auditSink.Append(map[string]any{
			"event":                        "credential_subject_changed",
			"op_id":                        inv.OpID,
			"variant_id":                   variantID,
			"client_id":                    callerToClientID(inv.Caller),
			"auth_strategy":                strategy,
			"expected_subject_fingerprint": want,
			"resolved_subject_fingerprint": creds.SubjectFingerprint,
		})
	}

	return NewStructuredError(ErrCodeAuthSubjectMismatch,
		"the resolved credential belongs to a different account than this profile is bound to; nothing was called. Run `gum login --switch-account` to rebind the profile, or select the profile that owns this account").
		WithDetail("op_id", inv.OpID).
		WithDetail("auth_strategy", strategy).
		WithDetail("expected_subject_fingerprint", want).
		WithDetail("resolved_subject_fingerprint", creds.SubjectFingerprint)
}

// Step 6 — token bucket (rate limiting).
//
// The "credsID" position in the Wait signature is used as the service-family
// key: token buckets are scoped per upstream service family (e.g. "gmail",
// "drive") so a flood of one Google service does not starve another. Spec
// §3.1 step 6, §6.2 backoff/jitter.
func (d *dispatcher) tokenBucketStep(ctx context.Context, inv *Invocation, rv *ResolvedVariant) error {
	if d.tokenBucket == nil {
		return nil
	}
	family := d.serviceFamilyFor(inv.OpID)
	return d.tokenBucket.Wait(ctx, inv.OpID, family)
}

// ServiceFamily exposes the op's catalog service_family for callers that
// hold a Dispatcher interface (e.g. gum_parallel's 429 isolation, spec §6.3
// line 1171). Returns "" when the op is unknown to the snapshot.
func (d *dispatcher) ServiceFamily(opID string) string {
	return d.serviceFamilyFor(opID)
}

// ReturnsLRO exposes the op's default-variant `lro_return` classification for
// callers that hold a Dispatcher interface (the §6.1 code-mode gate). Returns
// false when the op is unknown to the snapshot: an op that does not exist
// fails later with OP_NOT_FOUND, which is the more useful error.
func (d *dispatcher) ReturnsLRO(opID string) bool {
	if d.snapshot == nil {
		return false
	}
	op := d.findOp(opID)
	return op != nil && op.DefaultVariantReturnsLRO()
}

// serviceFamilyFor returns the op's service_family (catalog metadata) used as
// the rate-limit partition key. Returns "" if the op is missing from the
// snapshot — defense in depth; routing has already validated existence.
func (d *dispatcher) serviceFamilyFor(opID string) string {
	if d.snapshot == nil {
		return ""
	}
	if op := d.findOp(opID); op != nil {
		return op.ServiceFamily
	}
	return ""
}

// Step 7 — execute adapter.
// The deferred recoverAdapterPanic call catches any executor panic and converts
// it to a SERVICE_DOWN error (spec §3.1 step 7, line 235).
func (d *dispatcher) executeAdapter(ctx context.Context, inv *Invocation, rv *ResolvedVariant, creds *Credentials) (resp *Response, err error) {
	defer d.recoverAdapterPanic(inv, rv, &resp, &err)

	adapter, ok := d.adapters[rv.AdapterKey]
	if !ok {
		return nil, NewStructuredError(ErrCodeServiceDown, "adapter not registered: "+rv.AdapterKey).
			WithDetail("adapter_key", rv.AdapterKey).
			WithDetail("op_id", inv.OpID)
	}
	resp, err = adapter.Execute(ctx, inv, rv, creds)
	if err != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
		return nil, newCancelledError(err)
	}
	if err == nil && resp == nil {
		// The Adapter contract requires a response on the success path. A
		// third-party or plugin adapter that returns (nil, nil) — the obvious
		// early return for an empty result set — used to reach shapeResponse,
		// which dereferences resp.Format after the recover window closed and
		// took the whole gum mcp --stdio session down (spec §3.1 step 7).
		return nil, NewStructuredError(ErrCodeServiceDown, "adapter returned no response").
			WithDetail("adapter_key", rv.AdapterKey).
			WithDetail("op_id", inv.OpID).
			WithRetryable(false)
	}
	return resp, err
}

// attachTeeHandles projects a step-7c artifact onto the shaped response.
//
// The cold path and the cache-hit path share it because both run step 8: the
// handle belongs to the profile that fired, not to where the payload came from.
func (d *dispatcher) attachTeeHandles(shaped *ShapedResponse, art *teeArtifact) {
	if shaped == nil || art == nil {
		return
	}
	shaped.FullResultPath = art.Path
	size := art.Size
	shaped.FullResultSize = &size
	if art.Recovery == "resource_link" {
		shaped.FullResultResource = "gum://results/" + art.Hash
	}
	// The artifact was written moments ago, so its expiry is now plus the
	// retention window. Clients poll this instead of discovering expiry on a
	// failed gum://results read (spec §7, §3269).
	shaped.Expression.attachArtifactHandles(
		shaped.FullResultPath, shaped.FullResultResource, d.teeConfig.RetentionHours, time.Now())
}

// Step 8 — shape response (output pipeline).
//
// Selects the effective format from Invocation.Format (per spec §3.1 step 8 / §9):
//   - "raw"  → identity pass-through (caller wants the executor body verbatim)
//   - "toon" → encode as TOON (token-efficient default)
//   - "json" → re-encode JSON (no field-mask profile wiring yet — Phase 4)
//   - ""     → default to TOON
//   - other  → INVALID_ARGS structured error with field=format, value=<input>
func (d *dispatcher) shapeResponse(_ context.Context, inv *Invocation, rv *ResolvedVariant, resp *Response) (*ShapedResponse, error) {
	if resp == nil {
		return nil, NewStructuredError(ErrCodeServiceDown, "adapter returned no response").
			WithDetail("op_id", inv.OpID).
			WithRetryable(false)
	}
	format := inv.Format
	if format == "" {
		format = "toon"
	}
	switch format {
	case "raw", "toon", "json":
	default:
		return nil, NewStructuredError(ErrCodeInvalidArgs, "unknown output format").
			WithDetail("field", "format").
			WithDetail("value", inv.Format)
	}

	// Executor signals opaque bytes (e.g. gum.code Risor printed output): bypass
	// the JSON-parsing profile pipeline regardless of inv.Format. The envelope
	// still goes out, reporting the "_raw" sentinel profile (spec §2705).
	if resp.Format == "raw" {
		meta := newExpressionMeta(inv, rv, nil, &profile.ApplyOutput{Format: "raw"})
		if resp.CodeOutputTruncated {
			// §13 makes the field optional and true-only, so it is set rather
			// than always emitted: absent means nothing was cut.
			cut := true
			meta.CodeOutputTruncated = &cut
		}
		return &ShapedResponse{
			Body:       resp.Body,
			Format:     "raw",
			Expression: meta,
		}, nil
	}

	// A caller who asked for raw wants the upstream bytes, so the annotator runs
	// only on the shaped path.
	body := resp.Body
	var annotated []string
	if format != "raw" {
		body, annotated = d.annotateResponse(inv, rv, body)
	}

	// Step 8: apply the resolved expression profile (§9.1). inv.OutputProfile is
	// set in step 3a (catalog-embedded) or by a presentation layer (filesystem
	// overrides); nil means no profile applies → default shaping.
	prof := inv.OutputProfile
	if prof == nil {
		prof = &profile.Profile{}
	}
	// op/variant fill the §9.0 TOON header. The variant is the resolved one,
	// not the requested one, so a caller who omitted it still learns which
	// variant answered.
	variantID := ""
	if rv != nil && rv.Variant != nil {
		variantID = rv.Variant.VariantID
	}
	out, err := profile.Apply(prof, profile.ApplyInput{
		Body:       body,
		UserFormat: format,
		MaxItems:   inv.MaxItems,
		Op:         opIDOf(inv),
		Variant:    variantID,
	})
	if err != nil {
		return nil, err
	}
	return &ShapedResponse{
		Body:   out.Body,
		Format: out.Format,
		// The shaped tree, which is what Body encodes. Nil only when the body
		// was not JSON, which the raw bypass above has already handled.
		StructuredContent: out.Shaped,
		DroppedPaths:      out.DroppedPaths,
		CollapsedArrays:   out.CollapsedArrays,
		DedupedRows:       out.DedupedRows,
		LimitedRows:       out.LimitedRows,
		AnnotationPaths:   survivingAnnotations(annotated, out.DroppedPaths),
		Expression:        newExpressionMeta(inv, rv, prof, &out),
	}, nil
}

// survivingAnnotations drops the annotation paths the profile removed again.
// A field the caller cannot see in the shaped body either is not a reason to
// stay off raw, so naming it would make the notice wrong in the one direction
// that matters: talking the caller out of the format that has the data.
func survivingAnnotations(annotated, dropped []string) []string {
	if len(annotated) == 0 {
		return nil
	}

	gone := make(map[string]struct{}, len(dropped))
	for _, path := range dropped {
		gone[path] = struct{}{}
	}

	out := make([]string, 0, len(annotated))
	for _, path := range annotated {
		if _, removed := gone[path]; removed {
			continue
		}
		out = append(out, path)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// annotateResponse gives the executing adapter a chance to add fields the
// upstream response lacks. An adapter that does not implement
// ResponseAnnotator returns body as is.
func (d *dispatcher) annotateResponse(inv *Invocation, rv *ResolvedVariant, body []byte) ([]byte, []string) {
	if rv == nil {
		return body, nil
	}

	annotator, ok := d.adapters[rv.AdapterKey].(ResponseAnnotator)
	if !ok {
		return body, nil
	}

	// AnnotateResponse is adapter-owned code running after executeAdapter's
	// recover has already returned. A panic here used to terminate the process,
	// which spec §3.1 step 7 forbids for a long-running mcp --stdio session.
	// The annotation is additive by contract, so dropping it and serving the
	// upstream body is strictly better than failing the call.
	out, paths := d.annotateSafely(annotator, inv, rv, body)
	if out == nil {
		return body, nil
	}
	return out, paths
}

func (d *dispatcher) annotateSafely(annotator ResponseAnnotator, inv *Invocation, rv *ResolvedVariant, body []byte) (out []byte, paths []string) {
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		d.log().Error("response annotator panic",
			"op_id", inv.OpID,
			"adapter_key", rv.AdapterKey,
			"request_id", inv.RequestID,
			"panic_value", fmt.Sprintf("%v", r),
			"stack", sanitizeStackForLog(string(debug.Stack())),
		)
		if d.auditSink != nil {
			d.auditSink.Append(panicAuditEntry(inv, rv, d.canonicalArgs(inv.Args)))
		}
		out, paths = nil, nil
	}()
	return annotator.AnnotateResponse(inv, rv, body)
}

// Step 9 — record audit / gain ledger and return (spec §3.1 line 237).
//
// Best-effort accounting: ledger errors are logged but never fail the dispatch
// — the caller already has a valid shaped response, and corrupting the success
// path because the ledger journal is full would hide useful work behind a
// bookkeeping problem.
func (d *dispatcher) recordAndReturn(_ context.Context, inv *Invocation, rv *ResolvedVariant, creds *Credentials, shaped *ShapedResponse, raw *Response, fromCache, cacheable bool) (*ShapedResponse, error) {
	d.appendSuccessAudit(inv, rv)

	if d.gainLedger == nil {
		return shaped, nil
	}
	if err := d.gainLedger.Append(d.buildGainEntry(inv, rv, creds, shaped, raw, fromCache, cacheable)); err != nil {
		d.log().Warn("gain ledger append failed", "op_id", inv.OpID, "err", err)
	}
	return shaped, nil
}

// appendSuccessAudit emits the normative §11 audit entry for a successful
// dispatch. No-op when no audit sink is wired (tests and library embedders).
// The entry shape is built in dispatch/audit.go's successAuditEntry helper to
// keep recordAndReturn focused on bookkeeping bookkeeping.
func (d *dispatcher) appendSuccessAudit(inv *Invocation, rv *ResolvedVariant) {
	if d.auditSink == nil {
		return
	}
	d.auditSink.Append(successAuditEntry(inv, rv, d.canonicalArgs(inv.Args)))
}

// canonicalizeArgs produces the spec §10.0 args_canonical string: the RFC 8785
// JCS serialization of args with null-valued keys removed (Rule 1). It is the
// shared input to the cache key, the audit args_hash, the gain ledger and the
// tee artifact hash, so every one of those must see the same bytes for two
// callers who differ only in whether they spelled an absent optional field as
// null.
//
// jcs.Marshal supplies UTF-16 key ordering and JCS number and string rules that
// the previous hand-rolled sort.Strings + json.Marshal loop did not: json.Marshal
// escapes < > & to \u003c and friends, which JCS does not, so an externally
// computed hash never matched gum's.
func canonicalizeArgs(args map[string]any) string {
	if len(args) == 0 {
		return "{}"
	}
	pruned, _ := pruneNullsForJCS(args).(map[string]any)
	if len(pruned) == 0 {
		return "{}"
	}
	if out, err := jcs.Marshal(pruned); err == nil {
		return string(out)
	}
	// jcs.Marshal rejects values with no JSON form (NaN, chan, func). Args come
	// from decoded JSON in every production path, so this is a programming-error
	// fallback: stay deterministic rather than collapse distinct arg sets to "{}".
	return fallbackCanonicalArgs(pruned)
}

// pruneNullsForJCS removes every map key whose value is nil, at every depth,
// per spec §10.0 Rule 1. Array elements keep their nulls: dropping one would
// renumber the array and change what the caller sent.
func pruneNullsForJCS(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			if val == nil {
				continue
			}
			out[k] = pruneNullsForJCS(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = pruneNullsForJCS(item)
		}
		return out
	default:
		return v
	}
}

func fallbackCanonicalArgs(args map[string]any) string {
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	buf := []byte{'{'}
	for i, k := range keys {
		keyB, _ := json.Marshal(k)
		valB, _ := json.Marshal(args[k])
		buf = append(buf, keyB...)
		buf = append(buf, ':')
		buf = append(buf, valB...)
		if i < len(keys)-1 {
			buf = append(buf, ',')
		}
	}
	buf = append(buf, '}')
	return string(buf)
}

// profileNameFor picks the expression-profile name for one invocation. A §9.2
// [override_bindings] entry wins over the variant's own output_profile, the
// resolved variant_id being more specific than the op_id. An empty result means
// no profile applies and shaping falls back to the default.
func (d *dispatcher) profileNameFor(inv *Invocation, rv *ResolvedVariant) string {
	if d.profileBindings != nil {
		bindings := d.profileBindings()
		if name, ok := bindings[rv.Variant.VariantID]; ok {
			return name
		}
		if name, ok := bindings[inv.OpID]; ok {
			return name
		}
	}
	return rv.Variant.OutputProfile
}
