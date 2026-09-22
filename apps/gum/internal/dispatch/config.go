package dispatch

import (
	"context"
	"log/slog"

	"github.com/ehmo/gum/internal/cache"
	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/output/gain"
	"github.com/ehmo/gum/internal/output/profile"
)

// TokenBucket is the typed seam between the dispatch kernel and a rate-limiter.
// Implementations must block until a token is available or ctx is cancelled.
type TokenBucket interface {
	// Wait blocks until a request slot is available for (opID, credsID).
	// Returns ctx.Err() if the context is cancelled while waiting.
	Wait(ctx context.Context, opID, credsID string) error
}

// GainLedger is the typed seam between the dispatch kernel and the gain
// ledger. Step 9 (spec §3.1 line 237) appends one spec §12.3 entry per
// dispatch. *gain.Ledger satisfies it directly; tests substitute a capture.
//
// The seam used to carry a reduced dispatch-local GainEntry (op_id, format,
// byte counts) on the theory that this package must not import
// internal/output/gain. It already does, transitively through
// internal/output/profile, and the reduced view could not express the entry
// spec §12.3 requires: it had no token counts at all, so gain.Stats -- which
// sums raw_tokens minus shaped_tokens -- would have reported zero savings on
// every call even once a ledger was wired.
type GainLedger interface {
	Append(e gain.Entry) error
}

// AuthResolver is the typed seam between the dispatch kernel and the auth package.
// It is satisfied by *auth.ADCResolver, *auth.ByoOAuth, or any composite resolver.
//
// The interface is declared here (in dispatch) rather than in auth to avoid an
// import cycle: auth imports catalog; dispatch imports catalog. By putting the
// interface in dispatch we keep dispatch→auth as a one-way dependency and allow
// auth to remain ignorant of dispatch.
type AuthResolver interface {
	// ResolveAuth returns dispatch.Credentials from whatever auth strategy is
	// appropriate for inv and rv. The kernel calls this in step 5.
	ResolveAuth(ctx context.Context, inv *Invocation, rv *ResolvedVariant) (*Credentials, error)
}

// ArgDefaulter supplies arg defaults from sources outside the catalog, such as
// GUM_* environment variables and the profile config file (gum-puum). The
// kernel consults it in step 1, before catalog defaults, so a configured value
// ranks above a built-in one (spec §12.2 global config precedence).
type ArgDefaulter interface {
	// ArgDefaults returns top-level arg values for op. args holds the caller's
	// args and is read-only; the kernel applies a returned key only when the
	// caller omitted it. A non-nil error fails the call with INVALID_ARGS
	// carrying the error text, so the text should name the bad source.
	ArgDefaults(op *catalog.Op, args map[string]any) (map[string]any, error)
	// MissingArgHint returns remediation for required args still missing after
	// all defaults, or "" when the defaulter has none for them.
	MissingArgHint(op *catalog.Op, missing []string) string
}

// DispatcherConfig carries optional Phase-3 extensions to the dispatch kernel.
// All fields are optional; zero values yield Phase-2 behaviour (no-op stubs).
//
// Design choice: config struct over functional options. Reason: the kernel has a
// small, stable set of extension points; a struct makes them all visible in one
// place and avoids variadic-option ordering surprises in tests.
type DispatcherConfig struct {
	// Auth, when non-nil, is called in step 5 instead of the Phase-2 nil stub.
	Auth AuthResolver
	// Cache, when non-nil, is consulted in step 4 and populated after step 7.
	// Deprecated: prefer SemanticCache, which keys on the spec §10.3 tuple
	// (op_id, variant_id, args_canonical, fields, auth_subject_fingerprint).
	// When both are set, SemanticCache wins.
	Cache *cache.MemCache
	// SemanticCache, when non-nil, replaces Cache in step 4 and step 7b.
	// Semantic cache key includes the active field-mask and auth-subject
	// fingerprint so two callers with different projections or principals
	// never collide (spec §10.3).
	SemanticCache *cache.SemanticCache
	// HTTPCache, when non-nil, is the spec §10.2 HTTP/ETag cache. The kernel
	// consults it after the §10.3 lookup misses and before the executor runs:
	// a stored validator goes out as `If-None-Match`, and a 304 answer
	// short-circuits the expression pipeline into `{"unchanged": true,
	// "etag": "..."}` (§2024). Nil disables conditional requests; every call
	// then fetches the full body.
	HTTPCache *cache.HTTPCache
	// RateLimiter, when non-nil, is called in step 6 before the executor.
	RateLimiter TokenBucket
	// Policy configures the per-profile allowlist/denylist and scope gates
	// enforced during step 2 (gum-vq4z.2).
	Policy ProfilePolicy
	// ProfileName is the active profile name bound into destructive
	// confirmation tokens. Empty defaults to the historical unbound behavior
	// for tests and embedders that do not configure a profile.
	ProfileName string
	// ConfirmationReplayDir is the active profile data directory used for
	// durable confirmation-token replay markers.
	ConfirmationReplayDir string
	// PreferredInterfaceKinds is an ordered list of interface_kind values used
	// to break ties when two variants share the same stability rank (step 3).
	// The first entry that matches a candidate variant wins.
	PreferredInterfaceKinds []string
	// Ledger, when non-nil, receives one gain.Entry append in step 9, for
	// both cache hits and cold calls. A nil Ledger disables accounting.
	Ledger GainLedger
	// Tee configures the §9.0 'artifact' stage filesystem tee. Zero value
	// disables tee writes (ProfileDir empty).
	Tee TeeConfig
	// Audit, when non-nil, receives one Append per successful dispatch (and
	// one per recovered adapter panic) per spec §11. The package-local
	// auditSink interface is structurally `Append(map[string]any)`; any
	// value satisfying that shape works (e.g. *auditlog.Writer).
	Audit interface{ Append(entry map[string]any) }
	// NormalizeDatetimes enables spec §10.0 Rule 4 UTC normalization of
	// RFC 3339 date-time string args before JCS canonicalization. When true,
	// equivalent instants in different representations
	// ("2026-05-19T00:00:00.000Z" vs. "2026-05-19T00:00:00Z") collapse to
	// the same cache key, args_hash, and tee artifact hash — the documented
	// "date-format cache misses" mitigation for Calendar/Gmail/Drive sessions
	// (spec §10.0 Rule 4, §10.2 cache-miss-class). Default false preserves
	// Rule 3 verbatim-string semantics.
	NormalizeDatetimes bool
	// ProfileLookup, when non-nil, resolves a variant's output_profile NAME to a
	// built-in (catalog-embedded) expression profile at step 8 — the third
	// resolution layer of spec §9.2 (project-local → user-global →
	// catalog-embedded). The first two layers are filesystem overrides a
	// presentation layer sets directly on Invocation.OutputProfile, which takes
	// precedence. On a miss, shaping falls back to the default (empty) profile,
	// so ops without a defined profile are unchanged.
	ProfileLookup func(name string) (*profile.Profile, bool)
	// ProfileBindings, when non-nil, returns the merged §9.2
	// [override_bindings] table: op_id or variant_id to profile name. It is
	// consulted after variant resolution and before ProfileLookup, so a binding
	// attaches a profile to an op whose catalog variant names none, and
	// replaces the name when it names one. Spec §9.2: the binding substitutes
	// the profile without touching catalog data.
	ProfileBindings func() map[string]string
	// ArgDefaults, when non-nil, fills omitted args from configured defaults
	// and supplies the hint for still-missing required args (gum-puum).
	ArgDefaults ArgDefaulter
	// ExpectedAuthSubjects maps an auth_strategy name to the
	// auth_subject_fingerprint the active profile is bound to. Step 5 refuses
	// any credential resolved under a listed strategy whose fingerprint
	// differs, which is the §7 rule that a profile's credential is used "only
	// if its auth_subject_fingerprint matches the selected profile's expected
	// subject when one is recorded". The key is the strategy because the same
	// account fingerprints differently per strategy namespace. A nil map, an
	// unlisted strategy, or an empty expectation disables the check.
	ExpectedAuthSubjects map[string]string
	// ScopeUpgradeLogin, when non-nil, enables the spec §13 managed-scope
	// re-consent flow: a SCOPE_MISSING refusal on a byo_oauth op becomes an
	// approval a presentation layer can put to the operator, and an accepted
	// approval runs this login for exactly the refused scopes. It lives here
	// because the consent needs internal/auth, which this package cannot
	// import. A nil value keeps the bare SCOPE_MISSING refusal.
	ScopeUpgradeLogin ScopeUpgradeLogin
	// Logger receives every diagnostic this package emits (spec §14.1 rule
	// 2). A nil Logger falls back to slog.Default(), so an embedder that
	// wires nothing keeps the process-wide handler. Injecting
	// slog.New(slog.DiscardHandler) silences the kernel without silencing
	// the rest of the process.
	Logger *slog.Logger
}

// NewDispatcherWithConfig constructs a dispatch kernel that honours Phase-3
// extension points (auth, cache, rate-limiter) in addition to the base
// snapshot + adapters map from Phase 2.
//
// Callers that do not need Phase-3 features should continue using NewDispatcher.
func NewDispatcherWithConfig(snapshot *catalog.Catalog, adapters map[string]Adapter, cfg DispatcherConfig) Dispatcher {
	return &dispatcher{
		snapshot:                snapshot,
		adapters:                adapters,
		auth:                    cfg.Auth,
		cache:                   cfg.Cache,
		semanticCache:           cfg.SemanticCache,
		httpCache:               cfg.HTTPCache,
		tokenBucket:             cfg.RateLimiter,
		profilePolicy:           cfg.Policy,
		profileName:             cfg.ProfileName,
		confirmationReplayDir:   cfg.ConfirmationReplayDir,
		preferredInterfaceKinds: cfg.PreferredInterfaceKinds,
		gainLedger:              cfg.Ledger,
		teeConfig:               cfg.Tee,
		auditSink:               cfg.Audit,
		normalizeDatetimes:      cfg.NormalizeDatetimes,
		profileLookup:           cfg.ProfileLookup,
		profileBindings:         cfg.ProfileBindings,
		argDefaulter:            cfg.ArgDefaults,
		expectedAuthSubjects:    cfg.ExpectedAuthSubjects,
		scopeUpgradeLogin:       cfg.ScopeUpgradeLogin,
		logger:                  cfg.Logger,
	}
}

// log returns the injected logger, or slog.Default() when the caller wired
// none. Every diagnostic in this package goes through it, so a caller that
// injects slog.New(slog.DiscardHandler) sees no kernel output at all
// (spec §14.1 rule 2).
func (d *dispatcher) log() *slog.Logger {
	if d.logger != nil {
		return d.logger
	}
	return slog.Default()
}
