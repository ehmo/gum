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
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ehmo/gum/internal/cache"
	"github.com/ehmo/gum/internal/catalog"
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
}

// ShapedResponse is the final output after step 8 (output pipeline).
//
// StructuredContent, when non-nil, carries the JSON-shaped data underlying Body
// — populated whenever Body is encoded from a parseable JSON tree (i.e. for
// "toon" and "json" formats). Raw passes leave it nil. MCP handlers project
// StructuredContent into CallToolResult.StructuredContent for clients that
// consume the machine-readable schema-validated shape; the encoded text in
// Body goes into the text content block.
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

// Adapter is what executors implement (typed-rest-sdk, code.risor, etc.).
type Adapter interface {
	Execute(ctx context.Context, inv *Invocation, rv *ResolvedVariant, creds *Credentials) (*Response, error)
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

	opIndexOnce sync.Once              // builds opIndex on first findOp (review gum-yvam)
	opIndex     map[string]*catalog.Op // canonical op_id + alias → *Op; snapshot is immutable post-construction
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
	all := make([]string, 0, len(p.Projection)+len(p.KeepFields))
	all = append(all, p.Projection...)
	all = append(all, p.KeepFields...)
	sort.Strings(all)
	out := ""
	for i, k := range all {
		if i > 0 {
			out += ","
		}
		out += k
	}
	return out
}

// Dispatch drives all 9 lifecycle steps.
func (d *dispatcher) Dispatch(ctx context.Context, inv *Invocation) (*ShapedResponse, error) {
	dispatchStart := time.Now()
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
		slog.Debug("dispatch event",
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
	logEvent(EventResolveVariant, t0)
	if err := checkCancelled(ctx, "resolve_variant"); err != nil {
		return nil, err
	}
	if serr := d.enforceAdminFixtureOwnership(inv, rv); serr != nil {
		return nil, serr
	}

	// Step 3a: resolve the catalog-embedded (§9.2 third-layer) expression
	// profile. A presentation layer may have already set inv.OutputProfile from
	// the project-local / user-global filesystem layers (which take precedence);
	// only when it is unset do we look up the resolved variant's output_profile
	// name via the injected ProfileLookup. A miss leaves it nil → default
	// (empty-profile) shaping, so ops without a defined profile are unchanged.
	// Resolved here (before tee + shape) so recovery/tee and step-8 shaping agree.
	if inv.OutputProfile == nil && d.profileLookup != nil && rv.Variant != nil && rv.Variant.OutputProfile != "" {
		if p, ok := d.profileLookup(rv.Variant.OutputProfile); ok && p != nil {
			inv.OutputProfile = p
		}
	}

	// Step 3b: spec §9.1 field_mask_mode="dual_fetch" gate. The catalog
	// generator rejects ineligible variants at build time; this runtime check
	// guards profiles authored independently (user-global overrides). A
	// rejection surfaces as INVALID_ARGS with the failing variant_id + reason
	// so the operator sees which constraint failed.
	if inv.OutputProfile != nil && inv.OutputProfile.FieldMaskMode == profile.FieldMaskModeDualFetch {
		if gateErr := profile.ValidateDualFetchGate(inv.OutputProfile.FieldMaskMode, rv.Variant); gateErr != nil {
			return nil, NewStructuredError(ErrCodeInvalidArgs, "field_mask_mode=dual_fetch rejected: "+gateErr.Error()).
				WithDetail("field", "field_mask_mode").
				WithDetail("value", inv.OutputProfile.FieldMaskMode).
				WithDetail("op_id", inv.OpID).
				WithDetail("variant_id", rv.Variant.VariantID)
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
		shaped := &ShapedResponse{Body: cached.Body, Format: cached.Format, ValidationWarnings: validationWarnings}
		// Populate StructuredContent on the cache-HIT path too, so a warm call
		// returns the same typed structured content the cold (shapeResponse) path
		// does — otherwise an MCP client gets StructuredContent on the first call
		// and nil on every cached repeat. Best-effort: a non-JSON cached body
		// (e.g. raw/TOON) leaves it nil, matching the miss-path fallback.
		var structured any
		if json.Unmarshal(cached.Body, &structured) == nil {
			shaped.StructuredContent = structured
		}
		return d.recordAndReturn(ctx, inv, rv, shaped, &Response{Body: cached.Body, Format: cached.Format}, dispatchStart, true)
	}
	if err := checkCancelled(ctx, "cache_check"); err != nil {
		return nil, err
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

	// Step 7b: store successful response in cache.
	// SemanticCache (spec §10.3) is preferred; legacy MemCache stays for
	// callers that don't wire the semantic layer yet. Per-op TTL applies
	// only to SemanticCache; MemCache uses its constructor TTL.
	//
	// Only READ-class responses are cacheable: caching a write/destructive
	// response could serve a stale "success" for a later identical-arg call that
	// never actually ran (e.g. a duplicate send returning the prior message id
	// without sending). The §10.3 cache is a read-response cache.
	cacheable := rv != nil && rv.Variant != nil && rv.Variant.RiskClass == catalog.RiskClassRead
	if cacheable && d.semanticCache != nil && resp != nil {
		key := cache.SemanticKey(
			inv.OpID,
			rv.Variant.VariantID,
			canonicalizeArgs(d.canonicalArgs(inv.Args)),
			semanticFields(inv),
			semanticAuthFP(inv, creds),
		)
		d.semanticCache.Set(key, resp.Body, inv.OpID)
	} else if cacheable && d.cache != nil && d.auth == nil && resp != nil {
		// Legacy MemCache only: its KeyFor key has no auth-subject component, so
		// it would serve one principal's response to another. Restrict it to the
		// unauthenticated case; an authed dispatcher must use SemanticCache,
		// whose key includes the principal fingerprint (review gum-t8x1).
		key := cache.KeyFor(inv.OpID, canonicalizeArgs(d.canonicalArgs(inv.Args)), "", rv.Variant.VariantID)
		d.cache.Set(key, resp.Body)
	}

	// Step 7c: filesystem tee artifact (spec §9.0 stage 'artifact'). Writes
	// the post-upstream-projection payload before host-shaping so the recovery
	// handles can be projected into the §9.0 _expression envelope.
	teeArt, terr := d.writeTeeArtifact(inv, rv, creds, resp)
	if terr != nil {
		slog.Warn("tee artifact write failed", "op_id", inv.OpID, "err", terr)
	}

	// Step 8: shape response
	t0 = time.Now()
	shaped, err := d.shapeResponse(ctx, inv, rv, resp)
	if err != nil {
		logEvent(EventShapeResponse, t0)
		return nil, err
	}
	if shaped != nil && teeArt != nil {
		shaped.FullResultPath = teeArt.Path
		size := teeArt.Size
		shaped.FullResultSize = &size
		if teeArt.Recovery == "resource_link" {
			shaped.FullResultResource = "gum://results/" + teeArt.Hash
		}
	}
	if shaped != nil && len(validationWarnings) > 0 {
		shaped.ValidationWarnings = append(shaped.ValidationWarnings, validationWarnings...)
	}
	logEvent(EventShapeResponse, t0)

	// Step 9: record and return
	t0 = time.Now()
	result, err := d.recordAndReturn(ctx, inv, rv, shaped, resp, dispatchStart, false)
	logEvent(EventRecordAndReturn, t0)
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

func validateParams(op *catalog.Op, args map[string]any) (missing, unknown, typeErrors []string) {
	hasParams := len(op.ParamsRequired) > 0 || len(op.ParamsOptional) > 0
	hasFields := len(op.RequestFields) > 0
	if !hasParams && !hasFields {
		// Truly open schema: the op declares no params and no RequestFields, so
		// accept any args (e.g. searchconsole.sites.list, calendar.colors.get).
		return nil, nil, nil
	}
	// When the op has RequestFields but no hand-authored params lists (the 84
	// Discovery-enriched ops), the RequestFields below form the allowed set, so
	// unknown args (typos like emailq=foo) are rejected locally instead of being
	// forwarded to Google as spurious query params (gum-gatw).

	// Build allowed-key set once; used for unknown-key detection.
	allowed := make(map[string]struct{}, len(op.ParamsRequired)+len(op.ParamsOptional)+len(op.RequestFields)+5)
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
			allowed["body"] = struct{}{}
		} else {
			allowed[f.Name] = struct{}{}
		}
	}
	// Permanent allowlist: keys that are always valid but absent from the
	// per-op schema, so they must never be flagged unknown.
	//
	//  (a) Host-control keys the CLI (call.go) and MCP handler (handlers.go)
	//      inject into Args AFTER consulting the catalog schema:
	//        body       — POST ops carry a body from body:=json even when no
	//                     body-location RequestField exists.
	//        pageToken  — pagination continuation (--page-token).
	//        pageSize   — pagination size for newer Google APIs (--page-size).
	//        maxResults — pagination size for older Google APIs (--page-size).
	//
	//  (b) Google API global "system parameters" — valid on EVERY method but
	//      listed only at the top level of the Discovery doc, not per-method, so
	//      the Discovery walker never puts them in RequestFields. Rejecting them
	//      would wrongly fail a valid call (e.g. alt=json, quotaUser=…) on the
	//      84 enriched ops. (fields is both a host-control flag and a system
	//      parameter.) See cloud.google.com/apis/docs/system-parameters.
	permanent := []string{
		"body", "pageToken", "pageSize", "maxResults",
		"alt", "fields", "prettyPrint", "quotaUser", "userIp", "key",
		"oauth_token", "access_token", "callback", "uploadType",
		"upload_protocol", "$.xgafv",
	}
	for _, k := range permanent {
		allowed[k] = struct{}{}
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
//  3. Validate args against params_required / params_optional; aggregate ALL errors.
//  4. Compute ArgsHash.
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
			WithDetail("suggestions", suggestOpIDs(inv.OpID, d.opIDCandidates(), 3))
	}

	// Mutate inv.OpID to the canonical id (so downstream steps see it).
	inv.OpID = resolvedOp.OpID

	// 3. Validate args.
	missing, unknown, typeErrors := validateParams(resolvedOp, inv.Args)

	var warnings []string
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
		return nil, NewStructuredError(ErrCodeInvalidArgs, "invalid arguments").
			WithDetail("missing", emptyStrings(missing)).
			WithDetail("unknown", emptyStrings(unknown)).
			WithDetail("type_errors", emptyStrings(typeErrors))
	}

	// 4. Compute ArgsHash. Apply spec §10.0 Rule 4 datetime normalization when
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
func checkArgType(name string, val any, declType string) string {
	switch declType {
	case "string":
		if _, ok := val.(string); !ok {
			return fmt.Sprintf("%s: expected string, got %T", name, val)
		}
	case "integer":
		switch v := val.(type) {
		case int, int8, int16, int32, int64,
			uint, uint8, uint16, uint32, uint64,
			float32, float64:
			// ok
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

// findOpVariant returns the default variant for the given opID, or nil if not found.
func (d *dispatcher) findOpVariant(opID string) *catalog.Variant {
	op := d.findOp(opID)
	if op == nil {
		return nil
	}
	for j := range op.Variants {
		v := &op.Variants[j]
		if v.VariantID == op.DefaultVariantID {
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
			WithDetail("suggestions", suggestOpIDs(inv.OpID, d.opIDCandidates(), 3))
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

	// Step 1: if default_variant_id is set, use it directly.
	if op.DefaultVariantID != "" {
		for i := range op.Variants {
			v := &op.Variants[i]
			if v.VariantID == op.DefaultVariantID {
				// Check quarantine even on explicitly defaulted variants.
				if v.Quarantined {
					return nil, NewStructuredError(ErrCodeVariantQuarantined,
						fmt.Sprintf("variant %s is quarantined", v.VariantID)).
						WithDetail("op_id", inv.OpID).
						WithDetail("variant_id", v.VariantID)
				}
				return makeResolvedVariant(inv.OpID, op, v), nil
			}
		}
	}

	// Step 2: filter out quarantined variants.
	active := filterQuarantined(op)
	if len(active) == 0 {
		first := &op.Variants[0]
		return nil, NewStructuredError(ErrCodeVariantQuarantined,
			fmt.Sprintf("variant %s is quarantined", first.VariantID)).
			WithDetail("op_id", inv.OpID).
			WithDetail("variant_id", first.VariantID)
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
		return creds, nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil, err
	}
	var se *StructuredError
	if errors.As(err, &se) {
		return nil, err
	}
	return nil, NewStructuredError(ErrCodeAuthRequired, err.Error()).
		WithDetail("op_id", inv.OpID)
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
	return resp, err
}

// Step 8 — shape response (output pipeline).
//
// Selects the effective format from Invocation.Format (per spec §3.1 step 8 / §9):
//   - "raw"  → identity pass-through (caller wants the executor body verbatim)
//   - "toon" → encode as TOON (token-efficient default)
//   - "json" → re-encode JSON (no field-mask profile wiring yet — Phase 4)
//   - ""     → default to TOON
//   - other  → INVALID_ARGS structured error with field=format, value=<input>
func (d *dispatcher) shapeResponse(_ context.Context, inv *Invocation, _ *ResolvedVariant, resp *Response) (*ShapedResponse, error) {
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
	// the JSON-parsing profile pipeline regardless of inv.Format.
	if resp.Format == "raw" {
		return &ShapedResponse{Body: resp.Body, Format: "raw"}, nil
	}

	// Step 8: apply the resolved expression profile (§9.1). inv.OutputProfile is
	// set in step 3a (catalog-embedded) or by a presentation layer (filesystem
	// overrides); nil means no profile applies → default shaping.
	prof := inv.OutputProfile
	if prof == nil {
		prof = &profile.Profile{}
	}
	out, err := profile.Apply(prof, profile.ApplyInput{
		Body:       resp.Body,
		UserFormat: format,
	})
	if err != nil {
		return nil, err
	}
	var structured any
	if jerr := json.Unmarshal(resp.Body, &structured); jerr != nil {
		// raw bypass already handled above; if we got here resp.Body was valid
		// JSON for profile.Apply, so this branch only fires under a race or a
		// non-deterministic upstream — drop structuredContent rather than fail.
		structured = nil
	}
	return &ShapedResponse{Body: out.Body, Format: out.Format, StructuredContent: structured}, nil
}

// Step 9 — record audit / gain ledger and return (spec §3.1 line 237).
//
// Best-effort accounting: ledger errors are logged but never fail the dispatch
// — the caller already has a valid shaped response, and corrupting the success
// path because the ledger journal is full would hide useful work behind a
// bookkeeping problem.
func (d *dispatcher) recordAndReturn(_ context.Context, inv *Invocation, rv *ResolvedVariant, shaped *ShapedResponse, raw *Response, start time.Time, fromCache bool) (*ShapedResponse, error) {
	d.appendSuccessAudit(inv, rv)

	if d.gainLedger == nil {
		return shaped, nil
	}
	bytesIn := 0
	if raw != nil {
		bytesIn = len(raw.Body)
	}
	variantID := ""
	if rv != nil && rv.Variant != nil {
		variantID = rv.Variant.VariantID
	}
	entry := GainEntry{
		OpID:      inv.OpID,
		VariantID: variantID,
		Format:    shaped.Format,
		BytesIn:   bytesIn,
		BytesOut:  len(shaped.Body),
		WallMs:    time.Since(start).Milliseconds(),
		CacheHit:  fromCache,
		Timestamp: time.Now().UTC(),
	}
	if err := d.gainLedger.Append(entry); err != nil {
		slog.Warn("gain ledger append failed", "op_id", inv.OpID, "err", err)
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

// canonicalizeArgs produces a deterministic JSON serialisation of inv.Args for use
// as a cache key component. Keys are sorted so map iteration order doesn't affect
// the output.
func canonicalizeArgs(args map[string]any) string {
	if len(args) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Marshal as object with sorted keys.
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
