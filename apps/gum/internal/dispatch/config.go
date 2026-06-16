package dispatch

import (
	"context"
	"time"

	"github.com/ehmo/gum/internal/cache"
	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/output/profile"
)

// TokenBucket is the typed seam between the dispatch kernel and a rate-limiter.
// Implementations must block until a token is available or ctx is cancelled.
type TokenBucket interface {
	// Wait blocks until a request slot is available for (opID, credsID).
	// Returns ctx.Err() if the context is cancelled while waiting.
	Wait(ctx context.Context, opID, credsID string) error
}

// GainEntry is the typed payload appended to a gain ledger by step 9. It is a
// dispatch-local view of the gain.Entry contract; the gain package's ledger
// satisfies GainLedger via a thin adapter (gain.LedgerAdapter) so this package
// never imports internal/output/gain.
type GainEntry struct {
	OpID      string
	VariantID string
	Format    string
	BytesIn   int
	BytesOut  int
	WallMs    int64
	CacheHit  bool
	Timestamp time.Time
}

// GainLedger is the typed seam between the dispatch kernel and the gain ledger.
// Implementations append entries in step 9 (spec §3.1 line 237).
type GainLedger interface {
	Append(e GainEntry) error
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
	// Ledger, when non-nil, receives a GainEntry append in step 9.
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
	}
}
