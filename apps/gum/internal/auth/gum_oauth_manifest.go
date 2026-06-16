package auth

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/ehmo/gum/internal/embedded"
)

// managedScopesManifest is the parsed shape of
// docs/auth-managed-scopes.v1.json (embedded as
// embedded.AuthManagedScopesJSON). Only the fields the gate consults are
// modeled.
type managedScopesManifest struct {
	SchemaVersion  int                 `json:"schema_version"`
	ClientPolicy   managedClientPolicy `json:"client_policy"`
	ManagedProject managedProject      `json:"managed_project"`
	Scopes         []managedScopeEntry `json:"scopes"`
}

type managedClientPolicy struct {
	Flow                 string `json:"flow"`
	EmbeddedClientSecret bool   `json:"embedded_client_secret"`
	RedirectMethod       string `json:"redirect_method"`
	ScopeExpansionMode   string `json:"scope_expansion_mode"`
	ActiveScopeRule      string `json:"active_scope_rule"`
	ClientID             string `json:"client_id"`
}

// managedProject models the app-wide GUM-owned OAuth project state. Only
// publishing_status is consulted by the gate (the testing-window path); the
// remaining evidence fields are validated by the spec §7 promotion CI gates.
type managedProject struct {
	PublishingStatus string `json:"publishing_status"`
}

type managedScopeEntry struct {
	Scope                string `json:"scope"`
	Status               string `json:"status"`
	VerificationState    string `json:"verification_state"`
	ProjectEvidenceState string `json:"project_evidence_state"`
	LiveCanaryState      string `json:"live_canary_state"`
	// TestingAllowed is an explicit per-scope opt-in for the Google "testing"
	// publishing window: when the managed project's publishing_status is
	// "testing", a scope with TestingAllowed=true is eligible for gum_oauth
	// even before full verification completes. It is inert under any other
	// publishing status, so production stays strict by default.
	TestingAllowed bool `json:"testing_allowed"`
}

// loadManagedScopesManifest parses the embedded manifest. body, when non-nil,
// overrides the embedded bytes — tests use this to simulate a promoted scope
// without mutating the on-disk manifest.
func loadManagedScopesManifest(body []byte) (*managedScopesManifest, error) {
	if body == nil {
		body = embedded.AuthManagedScopesJSON
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("auth: managed-scopes manifest is empty")
	}
	var m managedScopesManifest
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("auth: managed-scopes manifest decode: %w", err)
	}
	if m.SchemaVersion != 1 {
		return nil, fmt.Errorf("auth: managed-scopes manifest schema_version = %d; want 1", m.SchemaVersion)
	}
	return &m, nil
}

// ManagedSupportedScopes returns the OAuth scopes listed in the managed OAuth
// manifest as testing-allowed or active. The public v1 login path does not use
// this set; it remains a manifest helper for gum_oauth internals and tests.
func ManagedSupportedScopes() ([]string, error) {
	m, err := loadManagedScopesManifest(nil)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := []string{}
	for _, s := range m.Scopes {
		if !s.TestingAllowed && s.Status != "active" {
			continue
		}
		if seen[s.Scope] {
			continue
		}
		seen[s.Scope] = true
		out = append(out, s.Scope)
	}
	sort.Strings(out)
	return out, nil
}

// canStartGumOAuth enforces the manifest's active_scope_rule: every requested
// scope must be (status=active, verification_state=verified,
// project_evidence_state=ready, live_canary_state=passing). The empty scope
// set is treated as ineligible — gum_oauth needs at least one scope to
// authorize for.
//
// Returns nil when all scopes are eligible. On failure returns an *AuthError
// whose Code is GUM_OAUTH_MANAGED_CLIENT_NOT_READY and whose
// MissingComponents lists the offending scopes (or "active_scope_required"
// when none were supplied).
func canStartGumOAuth(m *managedScopesManifest, scopes []string) error {
	if len(scopes) == 0 {
		return &AuthError{
			Code:              "GUM_OAUTH_MANAGED_CLIENT_NOT_READY",
			Strategy:          "gum_oauth",
			MissingComponents: []string{"active_scope_required"},
			SetupCommand:      "gum auth use-oauth-client",
			HumanRemediation:  "gum_oauth needs at least one scope; the managed scope manifest currently has no active scopes (see docs/auth-managed-scopes.v1.json).",
			UserMessage:       "No managed OAuth scopes are active yet. Use byo_oauth or adc for v0.1.0.",
		}
	}
	if m.ClientPolicy.EmbeddedClientSecret {
		return &AuthError{
			Code:             "GUM_OAUTH_MANIFEST_INVALID",
			Strategy:         "gum_oauth",
			HumanRemediation: "manifest declares embedded_client_secret=true; spec §7 line 1220 forbids this",
		}
	}
	testingWindow := m.ManagedProject.PublishingStatus == "testing"
	eligible := map[string]bool{}
	for _, s := range m.Scopes {
		fullyPromoted := s.Status == "active" &&
			s.VerificationState == "verified" &&
			s.ProjectEvidenceState == "ready" &&
			s.LiveCanaryState == "passing"
		// Testing-window opt-in: a scope the operator has explicitly enabled
		// (testing_allowed=true) is eligible while the project sits in Google
		// "testing" publishing status, before full verification lands. The flag
		// is inert outside that window so production stays strict by default.
		testingEligible := testingWindow && s.TestingAllowed
		if fullyPromoted || testingEligible {
			eligible[s.Scope] = true
		}
	}
	missing := []string{}
	for _, want := range scopes {
		if !eligible[want] {
			missing = append(missing, want)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)
	return &AuthError{
		Code:              "GUM_OAUTH_MANAGED_CLIENT_NOT_READY",
		Strategy:          "gum_oauth",
		MissingComponents: missing,
		SetupCommand:      "gum auth use-oauth-client",
		HumanRemediation:  "one or more requested scopes is not yet promoted to active in the managed-scope manifest; v0.1.0 ships with all scopes planned/pending pending live canary evidence.",
		UserMessage:       "gum_oauth is not yet available for these scopes; use byo_oauth or adc.",
		RequiredScopes:    append([]string{}, scopes...),
	}
}
