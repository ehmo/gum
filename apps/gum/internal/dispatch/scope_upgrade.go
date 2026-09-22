package dispatch

// Spec §13 "managed-scope re-consent". A scoped op that policy gate 5 refuses
// with SCOPE_MISSING is a dead end for an MCP agent: the missing scope can only
// be granted by a human at a Google consent screen. §13 says that when the
// session can elicit, gum offers ONE structured approval instead of the bare
// refusal, runs the consent for exactly the refused scopes, verifies what came
// back, and reports SCOPE_GRANTED without re-running the operation.
//
// The kernel owns the parts that must not move into a presentation layer: the
// approval object and its request hash, the post-login verification, the audit
// event, and the in-process scope allowlist refresh. internal/mcp owns the
// elicitation wire mechanics; cmd/gum injects the login that actually talks to
// Google, because internal/dispatch cannot import internal/auth (cycle).
//
// The flow applies to byo_oauth only. It is the one strategy whose credential a
// loopback consent can produce, and the only OAuth strategy v1 builds ship
// (§1.2); gum_oauth, which the §13 sketch names, is not dispatchable here.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"

	"github.com/ehmo/gum/internal/catalog"
)

// ScopeUpgradeStatusGranted is the §13 success status. It reports that the
// grant is recorded, NOT that the original operation ran: §13 forbids the
// auto-retry, so the caller re-issues the call itself.
const ScopeUpgradeStatusGranted = "SCOPE_GRANTED"

// scopeUpgradeAuditEvent is the §11 audit `event` value for every outcome of
// one re-consent, accepted or not.
const scopeUpgradeAuditEvent = "managed_scope_reconsent"

// Reply actions, mirroring the MCP elicitation result actions.
const (
	ScopeUpgradeAccept  = "accept"
	ScopeUpgradeDecline = "decline"
	ScopeUpgradeCancel  = "cancel"
)

// Audit `outcome` values. Exactly one is written per re-consent attempt.
const (
	scopeUpgradeOutcomeGranted     = "granted"
	scopeUpgradeOutcomeNotApproved = "not_approved"
	scopeUpgradeOutcomeBinding     = "binding_mismatch"
	scopeUpgradeOutcomeLoginFailed = "login_failed"
	scopeUpgradeOutcomeShortfall   = "scope_shortfall"
	scopeUpgradeOutcomeSubject     = "subject_mismatch"
	scopeUpgradeOutcomeUnavailable = "login_unavailable"
)

// ScopeUpgradeRequest is the §13 approval object. Every field is a binding the
// approval carries and the acceptance is checked against, so an approval
// collected for one call cannot authorize another.
type ScopeUpgradeRequest struct {
	OpID      string // the refused op
	VariantID string // the variant policy resolved, not the op default
	Profile   string // the profile whose credential the consent replaces
	// RequiredScopes is the variant's complete declared scope set, sorted and
	// deduplicated. §13 wants the exact set displayed and granted, not the
	// single scope that happened to be missing.
	RequiredScopes []string
	// ExpectedSubject is the auth_subject_fingerprint the profile is bound to
	// for this strategy, or "" when the profile is not bound yet.
	ExpectedSubject string
	// RequestHash is sha256 over op_id, variant_id, args_canonical and
	// required_scopes. It is recomputed from live state on the retry, so a
	// reply minted for a different call fails the comparison.
	RequestHash string
}

// ScopeUpgradeReply is the approval the presentation layer collected.
type ScopeUpgradeReply struct {
	// Action is accept, decline or cancel.
	Action string
	// Approved is the operator's answer inside an accepted form. An accepted
	// form carrying approve=false is a refusal, not a grant.
	Approved bool
	// The bindings the client echoed. An empty value means "not echoed" and is
	// not compared; a populated one MUST equal the request's.
	OpID           string
	VariantID      string
	Profile        string
	RequestHash    string
	RequiredScopes []string
}

// ScopeUpgradeGrant is what one consent actually produced.
type ScopeUpgradeGrant struct {
	// GrantedScopes is the stored grant read back after login, already
	// expanded with the scopes a broader grant subsumes.
	GrantedScopes []string
	// AuthSubjectFingerprint identifies the Google account that consented.
	AuthSubjectFingerprint string
}

// ScopeUpgradeLogin runs one interactive consent for req.RequiredScopes on
// req.Profile and reports what Google granted. It MUST store nothing when the
// consent comes back short of req.RequiredScopes or on a different account
// than req.ExpectedSubject.
type ScopeUpgradeLogin func(ctx context.Context, req ScopeUpgradeRequest) (ScopeUpgradeGrant, error)

// ScopeUpgradeOutcome is the §13 accepted-path return value.
type ScopeUpgradeOutcome struct {
	Status                 string   `json:"status"`
	OpID                   string   `json:"op_id"`
	RequestHash            string   `json:"request_hash"`
	Profile                string   `json:"profile"`
	GrantedScopes          []string `json:"granted_scopes"`
	AuthSubjectFingerprint string   `json:"auth_subject_fingerprint,omitempty"`
}

// ScopeUpgrader is the optional dispatcher capability a presentation layer
// type-asserts for. A dispatcher built without a ScopeUpgradeLogin still
// implements it and always declines, so the assertion never decides whether
// the flow is live — ScopeUpgradeFor does.
type ScopeUpgrader interface {
	// ScopeUpgradeFor derives the approval object for a SCOPE_MISSING failure.
	// ok is false when the failure is not eligible for re-consent.
	ScopeUpgradeFor(inv *Invocation, se *StructuredError) (req ScopeUpgradeRequest, ok bool)
	// ApplyScopeUpgrade runs the consent and verifies it. A nil outcome means
	// nothing was granted and the caller MUST return the original
	// SCOPE_MISSING envelope unchanged (§13).
	ApplyScopeUpgrade(ctx context.Context, req ScopeUpgradeRequest, reply ScopeUpgradeReply) *ScopeUpgradeOutcome
}

// ScopeUpgradeFor implements ScopeUpgrader.
func (d *dispatcher) ScopeUpgradeFor(inv *Invocation, se *StructuredError) (ScopeUpgradeRequest, bool) {
	if d.scopeUpgradeLogin == nil || inv == nil || se == nil {
		return ScopeUpgradeRequest{}, false
	}
	if se.ErrCode != ErrCodeScopeMissing {
		return ScopeUpgradeRequest{}, false
	}
	v := d.policyVariant(inv)
	if v == nil || len(v.Scopes) == 0 {
		return ScopeUpgradeRequest{}, false
	}
	if v.AuthStrategy != catalog.AuthStrategyBYOOAuth {
		return ScopeUpgradeRequest{}, false
	}
	scopes := sortedUniqueScopes(v.Scopes)
	return ScopeUpgradeRequest{
		OpID:            inv.OpID,
		VariantID:       v.VariantID,
		Profile:         d.profileName,
		RequiredScopes:  scopes,
		ExpectedSubject: d.expectedAuthSubjects[string(v.AuthStrategy)],
		RequestHash:     d.scopeUpgradeHash(inv, v, scopes),
	}, true
}

// scopeUpgradeHash binds one approval to one call. Gate 5 and ScopeUpgradeFor
// both go through it, so the hash the refusal reports is the hash the reply is
// checked against.
func (d *dispatcher) scopeUpgradeHash(inv *Invocation, v *catalog.Variant, scopes []string) string {
	return scopeRequestHash(inv.OpID, v.VariantID, canonicalizeArgs(d.canonicalArgs(inv.Args)), scopes)
}

// ApplyScopeUpgrade implements ScopeUpgrader.
func (d *dispatcher) ApplyScopeUpgrade(ctx context.Context, req ScopeUpgradeRequest, reply ScopeUpgradeReply) *ScopeUpgradeOutcome {
	if reply.Action != ScopeUpgradeAccept || !reply.Approved {
		d.auditScopeUpgrade(req, scopeUpgradeOutcomeNotApproved, reply.Action, nil, "", "")
		return nil
	}
	if mismatch := scopeUpgradeBindingMismatch(req, reply); mismatch != "" {
		d.auditScopeUpgrade(req, scopeUpgradeOutcomeBinding, reply.Action, nil, "", "binding mismatch: "+mismatch)
		return nil
	}
	login := d.scopeUpgradeLogin
	if login == nil {
		d.auditScopeUpgrade(req, scopeUpgradeOutcomeUnavailable, reply.Action, nil, "", "no login wired")
		return nil
	}

	grant, err := login(ctx, req)
	if err != nil {
		d.auditScopeUpgrade(req, scopeUpgradeOutcomeLoginFailed, reply.Action, nil, "", err.Error())
		return nil
	}
	// §13: verify the grant covers exactly the approved set before it counts.
	// The login is expected to refuse a short grant before storing anything;
	// this is the kernel-side check that the refusal happened.
	if missing := missingScopes(req.RequiredScopes, grant.GrantedScopes); len(missing) > 0 {
		d.auditScopeUpgrade(req, scopeUpgradeOutcomeShortfall, reply.Action, grant.GrantedScopes, grant.AuthSubjectFingerprint, "consent did not grant "+strings.Join(missing, " "))
		return nil
	}
	if req.ExpectedSubject != "" && grant.AuthSubjectFingerprint != req.ExpectedSubject {
		d.auditScopeUpgrade(req, scopeUpgradeOutcomeSubject, reply.Action, grant.GrantedScopes, grant.AuthSubjectFingerprint, "consent returned a different Google account")
		return nil
	}

	// Refresh gate 5's allowlist in place. Without it the caller's retry hits
	// the same SCOPE_MISSING for the life of the process, because the policy
	// allowlist is read once at construction from the stored grant.
	d.addAllowedScopes(grant.GrantedScopes)
	d.auditScopeUpgrade(req, scopeUpgradeOutcomeGranted, reply.Action, grant.GrantedScopes, grant.AuthSubjectFingerprint, "")

	return &ScopeUpgradeOutcome{
		Status:                 ScopeUpgradeStatusGranted,
		OpID:                   req.OpID,
		RequestHash:            req.RequestHash,
		Profile:                req.Profile,
		GrantedScopes:          append([]string(nil), grant.GrantedScopes...),
		AuthSubjectFingerprint: grant.AuthSubjectFingerprint,
	}
}

// scopeUpgradeBindingMismatch names the first echoed binding that disagrees
// with the request, or "" when every echoed field matches. An empty echo is
// not a mismatch: a client may drop schema fields it did not render.
func scopeUpgradeBindingMismatch(req ScopeUpgradeRequest, reply ScopeUpgradeReply) string {
	for _, b := range []struct{ name, want, got string }{
		{"op_id", req.OpID, reply.OpID},
		{"variant_id", req.VariantID, reply.VariantID},
		{"profile", req.Profile, reply.Profile},
		{"request_hash", req.RequestHash, reply.RequestHash},
	} {
		if b.got != "" && b.got != b.want {
			return b.name
		}
	}
	if len(reply.RequiredScopes) > 0 && !sameScopes(req.RequiredScopes, reply.RequiredScopes) {
		return "required_scopes"
	}
	return ""
}

// scopeRequestHash is the §13 request_hash: sha256 over op_id,
// variant_id_resolved, args_canonical and required_scopes. The parts are
// newline-joined so no two different tuples can concatenate to one string.
func scopeRequestHash(opID, variantID, argsCanonical string, requiredScopes []string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		opID,
		variantID,
		argsCanonical,
		strings.Join(requiredScopes, " "),
	}, "\n")))
	return hex.EncodeToString(sum[:])
}

// auditScopeUpgrade writes the §13 audit event. It carries the requested and
// granted scopes, the profile, op_id, request_hash and the resulting
// fingerprint, so a reader can tell a grant from a decline from a refusal.
// Exactly one call runs per re-consent attempt.
func (d *dispatcher) auditScopeUpgrade(req ScopeUpgradeRequest, outcome, action string, granted []string, subject, reason string) {
	if d.auditSink == nil {
		return
	}
	entry := map[string]any{
		"event":                        scopeUpgradeAuditEvent,
		"outcome":                      outcome,
		"action":                       action,
		"op_id":                        req.OpID,
		"variant_id":                   req.VariantID,
		"profile":                      req.Profile,
		"request_hash":                 req.RequestHash,
		"requested_scopes":             req.RequiredScopes,
		"granted_scopes":               granted,
		"auth_subject_fingerprint":     subject,
		"expected_subject_fingerprint": req.ExpectedSubject,
	}
	if reason != "" {
		entry["reason"] = reason
	}
	d.auditSink.Append(entry)
}

// allowedScopes returns the scope set gate 5 accepts: the profile's stored
// grant plus anything a §13 re-consent added during this process.
// profilePolicy is immutable after construction, so only the upgrade set needs
// the lock.
func (d *dispatcher) allowedScopes() []string {
	d.scopeMu.RLock()
	defer d.scopeMu.RUnlock()
	if len(d.upgradedScopes) == 0 {
		return d.profilePolicy.AllowedScopes
	}
	out := make([]string, 0, len(d.profilePolicy.AllowedScopes)+len(d.upgradedScopes))
	out = append(out, d.profilePolicy.AllowedScopes...)
	return append(out, d.upgradedScopes...)
}

// addAllowedScopes records newly granted scopes for the rest of the process.
// It deliberately leaves profilePolicy alone: other gates copy that struct
// without the lock, and persisting the grant is the login's job. The write
// replaces the slice rather than appending in place, so a reader holding the
// previous slice keeps a consistent view.
func (d *dispatcher) addAllowedScopes(granted []string) {
	if len(granted) == 0 {
		return
	}
	d.scopeMu.Lock()
	defer d.scopeMu.Unlock()
	have := make(map[string]bool, len(d.profilePolicy.AllowedScopes)+len(d.upgradedScopes))
	for _, s := range d.profilePolicy.AllowedScopes {
		have[s] = true
	}
	for _, s := range d.upgradedScopes {
		have[s] = true
	}
	merged := append([]string(nil), d.upgradedScopes...)
	for _, s := range granted {
		if !have[s] {
			have[s] = true
			merged = append(merged, s)
		}
	}
	d.upgradedScopes = merged
}

// missingScopes returns the required scopes absent from have, in order.
func missingScopes(required, have []string) []string {
	granted := make(map[string]bool, len(have))
	for _, s := range have {
		granted[s] = true
	}
	var missing []string
	for _, s := range required {
		if !granted[s] {
			missing = append(missing, s)
		}
	}
	return missing
}

// sortedUniqueScopes returns scopes sorted and deduplicated, leaving the input
// untouched. The approval object is hashed, so its scope order must not depend
// on catalog authoring order.
func sortedUniqueScopes(scopes []string) []string {
	if len(scopes) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(scopes))
	out := make([]string, 0, len(scopes))
	for _, s := range scopes {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// sameScopes reports whether two scope sets carry the same members,
// disregarding order and duplicates.
func sameScopes(a, b []string) bool {
	as, bs := sortedUniqueScopes(a), sortedUniqueScopes(b)
	if len(as) != len(bs) {
		return false
	}
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}
	return true
}
