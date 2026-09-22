package dispatch

import (
	"time"

	"github.com/ehmo/gum/internal/output/profile"
	"github.com/ehmo/gum/internal/output/tee"
)

// defaultTeeRetentionHours is the spec §9.0 artifact retention window used when
// output.tee_retention_hours is unset. It aliases the tee package's constant so
// the expiry gum advertises and the window gum://results/{hash} scans cannot
// drift apart.
const defaultTeeRetentionHours = tee.DefaultRetentionHours

// rawProfileSentinel is the §13 profile name for a response that bypassed the
// expression pipeline. A raw pass-through reports it instead of the profile it
// skipped, so a client cannot mistake unshaped bytes for shaped ones.
const rawProfileSentinel = "_raw"

// ExpressionMeta is the spec §13 `_expression` envelope: what the shaping
// pipeline did to a response, reported alongside the response.
//
// Without it a caller cannot tell 20 rows from 20 of 243, nor an upstream empty
// result from one the profile emptied. Every field below answers one of those
// questions. The struct mirrors the registered MCP output schema, so the JSON
// tags are part of the wire contract.
type ExpressionMeta struct {
	// Profile is the resolved expression-profile name, or "_raw" for a
	// pass-through (§13 result-schema selection rule).
	Profile string `json:"profile"`

	// OpID and VariantID name what ran. VariantID is a pointer because §13
	// allows null for the outer gum_parallel batch entry, which has no single
	// variant, and required-presence forbids omitting the key.
	OpID      string  `json:"op_id"`
	VariantID *string `json:"variant_id"`

	// Lossy is true when the profile ran a stage that can remove data.
	Lossy bool `json:"lossy"`

	// ResultCount is the number of records in the shaped body. OmittedCount is
	// how many the profile dropped. Both are always emitted (§9.1 rule 3): a
	// count that appears only when non-zero cannot distinguish "none dropped"
	// from "the field is missing".
	ResultCount  int `json:"result_count"`
	OmittedCount int `json:"omitted_count"`

	// OnEmptyMessage is the profile's on_empty string when shaping left an
	// empty record set, nil otherwise (§9.1 rule 2).
	OnEmptyMessage *string `json:"on_empty_message"`

	// FullResultPath and FullResultResource are the recovery handles for the
	// unshaped payload. Omitted when tee did not fire.
	FullResultPath     string `json:"full_result_path,omitempty"`
	FullResultResource string `json:"full_result_resource,omitempty"`

	// ArtifactExpiresAt is when the tee artifact is deleted, RFC 3339 UTC.
	// Clients poll against it to detect staleness before a read fails
	// (spec §7 RESULT_ARTIFACT_EXPIRED). Nil when tee did not fire.
	ArtifactExpiresAt *string `json:"artifact_expires_at,omitempty"`

	// IntentionalZeroMaxItems marks a profile that dropped every row on
	// purpose (§9.1 discriminator 5). Set only together with a non-null
	// OnEmptyMessage, which §13 makes a normative invariant.
	IntentionalZeroMaxItems *bool `json:"intentional_zero_max_items,omitempty"`

	// ProjectRootURI and ProfileResolutionWarning report where the profile
	// came from when a project-local root participated (§9.2).
	ProjectRootURI           *string `json:"project_root_uri,omitempty"`
	ProfileResolutionWarning *string `json:"_profile_resolution_warning,omitempty"`

	// CodeOutputTruncated marks a gum.code result cut short by the cumulative
	// output budget.
	CodeOutputTruncated *bool `json:"_code_output_truncated,omitempty"`

	// UnsupportedCapabilities is the spec §932 warning field. A `partial`
	// variant executes, so the call succeeds, and this names the capability
	// atoms that did not run. Omitted for every other execution_support.
	//
	// It is deliberately absent from the registered MCP outputSchema. §13
	// keeps ExpressionMeta open for exactly this case: "future minor releases
	// can add diagnostic fields ... without breaking client validators that
	// pin to the registered v0.1 outputSchema". Registering one more property
	// also costs 783 tokens across every tool's outputSchema, and
	// TestGainReleaseFixtureSavingsFloor has 9 tokens of headroom above the
	// published 80% claim.
	UnsupportedCapabilities []string `json:"_unsupported_capabilities,omitempty"`
}

// newExpressionMeta builds the §9.1 envelope from a shaping result.
//
// prof may be nil (no profile applies), in which case the envelope reports an
// empty profile name and a lossless pass.
func newExpressionMeta(inv *Invocation, rv *ResolvedVariant, prof *profile.Profile, out *profile.ApplyOutput) *ExpressionMeta {
	meta := &ExpressionMeta{OpID: opIDOf(inv)}
	if prof != nil {
		meta.Profile = prof.Name
	}
	if rv != nil && rv.Variant != nil {
		id := rv.Variant.VariantID
		meta.VariantID = &id
		meta.UnsupportedCapabilities = partialCapabilityWarning(rv.Variant)
	}
	if out == nil {
		return meta
	}

	meta.Lossy = out.Lossy
	meta.ResultCount = out.ResultCount
	meta.OmittedCount = out.OmittedCount
	if out.OnEmptyMessage != "" {
		msg := out.OnEmptyMessage
		meta.OnEmptyMessage = &msg
	}
	if out.IntentionalZeroMaxItems {
		flag := true
		meta.IntentionalZeroMaxItems = &flag
	}

	// §2705: a pass-through reports the sentinel profile and never claims
	// lossy, whatever the profile it skipped would have done.
	if out.Format == "raw" {
		meta.Profile = rawProfileSentinel
		meta.Lossy = false
	}
	return meta
}

// attachArtifactHandles copies the tee recovery handles onto the envelope and
// derives the expiry the §7 expired-artifact contract tells clients to poll.
func (m *ExpressionMeta) attachArtifactHandles(path, resource string, retentionHours int, now time.Time) {
	if m == nil || path == "" {
		return
	}
	m.FullResultPath = path
	m.FullResultResource = resource
	if retentionHours <= 0 {
		retentionHours = defaultTeeRetentionHours
	}
	// Cap before the multiply: time.Duration is int64 nanoseconds, so a
	// retention of roughly 2.6 million hours or more wraps and advertises an
	// artifact_expires_at in the past, telling every client the fresh artifact
	// it just wrote is already gone.
	// Cap before the multiply: time.Duration is int64 nanoseconds, so a
	// retention of roughly 2.6 million hours or more wraps and advertises an
	// artifact_expires_at in the past, telling every client the fresh artifact
	// it just wrote is already gone.
	if retentionHours > tee.MaxRetentionHours {
		retentionHours = tee.MaxRetentionHours
	}
	expires := now.UTC().Add(time.Duration(retentionHours) * time.Hour).Format(time.RFC3339)
	m.ArtifactExpiresAt = &expires
}

// opIDOf is nil-safe so the envelope builder stays callable from a degraded path.
func opIDOf(inv *Invocation) string {
	if inv == nil {
		return ""
	}
	return inv.OpID
}

// Fields projects the envelope into its JSON object form, using the struct's
// own key names and omission rules.
//
// gum_parallel needs the map form because spec §9.0.1 hoists individual
// fields out of each per-result envelope into the batch's shared pool, and
// puts the remainder back as an ExpressionMetaDelta. A struct cannot express
// "this field was hoisted away"; a map can.
//
// _code_output_truncated is deliberately absent. §13 places that flag on the
// enclosing ParallelResultItem, as a sibling of _expression, not inside the
// delta. Callers that need it read CodeOutputTruncated directly.
//
// Returns nil for a nil receiver so a degraded path that never shaped a
// response can call it unguarded.
func (m *ExpressionMeta) Fields() map[string]any {
	if m == nil {
		return nil
	}

	// The five §13 required fields plus the two counters are always present:
	// a count that appears only when non-zero cannot distinguish "none" from
	// "not reported", and required-presence forbids omitting a null.
	out := map[string]any{
		"profile":          m.Profile,
		"op_id":            m.OpID,
		"variant_id":       derefAny(m.VariantID),
		"lossy":            m.Lossy,
		"result_count":     m.ResultCount,
		"omitted_count":    m.OmittedCount,
		"on_empty_message": derefAny(m.OnEmptyMessage),
	}

	if m.FullResultPath != "" {
		out["full_result_path"] = m.FullResultPath
	}
	if m.FullResultResource != "" {
		out["full_result_resource"] = m.FullResultResource
	}
	if m.ArtifactExpiresAt != nil {
		out["artifact_expires_at"] = *m.ArtifactExpiresAt
	}
	if m.IntentionalZeroMaxItems != nil {
		out["intentional_zero_max_items"] = *m.IntentionalZeroMaxItems
	}
	if m.ProjectRootURI != nil {
		out["project_root_uri"] = *m.ProjectRootURI
	}
	if m.ProfileResolutionWarning != nil {
		out["_profile_resolution_warning"] = *m.ProfileResolutionWarning
	}
	if len(m.UnsupportedCapabilities) > 0 {
		out["_unsupported_capabilities"] = m.UnsupportedCapabilities
	}
	return out
}

// derefAny returns *p as an untyped value, or an untyped nil. A typed nil
// pointer in an any-valued map serializes to JSON null but compares unequal
// to nil in Go, which would break every consumer that tests for absence.
func derefAny[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}
