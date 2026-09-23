package auth

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"
)

// The two tests in this file are the release gates the managed-scope
// manifest's own preamble names: "runs TestManagedScopeVerificationEvidence
// and TestManagedOAuthProjectReadiness as CI gates". They cover the
// parts of spec §7 scope promotion that data/auth-managed-scopes.v1.schema.json
// cannot express.
//
// The schema already enforces the forward half of atomic promotion
// (status="active" implies verified/ready/passing plus non-empty evidence),
// and TestManagedScopeManifestSchema mutation-proves it. What the schema
// leaves open is what these gates close:
//
//   - managed_project's seven evidence strings are typed `string` with no
//     minLength, so a manifest can carry an active scope while
//     token_exchange_canary_evidence is "".
//   - nothing stops a scope from holding all four promoted states while
//     status is still "planned", which is a half-applied promotion.
//   - nothing ties the manifest to the runtime gate, so a planned scope
//     could still reach ManagedSupportedScopes or canStartGumOAuth.

// releaseGateManifest is the gate's view of the manifest. It is separate from
// the production managedScopesManifest because the gate reads fields the
// runtime never decodes.
type releaseGateManifest struct {
	ManagedProject releaseGateProject `json:"managed_project"`
	Scopes         []releaseGateScope `json:"scopes"`
}

type releaseGateProject struct {
	APIsEnabledEvidence         string `json:"apis_enabled_evidence"`
	OAuthConsentScreenEvidence  string `json:"oauth_consent_screen_evidence"`
	PublishingStatus            string `json:"publishing_status"`
	QuotaProjectStrategy        string `json:"quota_project_strategy"`
	BillingOrTermsEvidence      string `json:"billing_or_terms_evidence"`
	TokenExchangeCanaryEvidence string `json:"token_exchange_canary_evidence"`
	RefreshCanaryEvidence       string `json:"refresh_canary_evidence"`
	ReleaseRule                 string `json:"release_rule"`
}

type releaseGateScope struct {
	Scope                string `json:"scope"`
	Service              string `json:"service"`
	Category             string `json:"category"`
	Status               string `json:"status"`
	VerificationState    string `json:"verification_state"`
	ProjectEvidenceState string `json:"project_evidence_state"`
	LiveCanaryState      string `json:"live_canary_state"`
	TestingAllowed       bool   `json:"testing_allowed"`
	Evidence             string `json:"evidence"`
}

// projectReadinessGaps returns the missing_components list a
// GUM_OAUTH_MANAGED_CLIENT_NOT_READY envelope would carry for this manifest,
// empty when the managed project is ready to back an active scope.
//
// The gate fires only when the manifest claims at least one usable scope.
// A manifest with nothing active needs no project evidence yet; it reports
// active_scope_required, the same component canStartGumOAuth uses for an
// empty request.
func projectReadinessGaps(m releaseGateManifest) []string {
	usable := 0
	for _, s := range m.Scopes {
		if s.Status == "active" || s.TestingAllowed {
			usable++
		}
	}
	if usable == 0 {
		return []string{"active_scope_required"}
	}

	gaps := []string{}
	p := m.ManagedProject
	for _, f := range []struct {
		name  string
		value string
	}{
		{"apis_enabled_evidence", p.APIsEnabledEvidence},
		{"oauth_consent_screen_evidence", p.OAuthConsentScreenEvidence},
		{"billing_or_terms_evidence", p.BillingOrTermsEvidence},
		{"token_exchange_canary_evidence", p.TokenExchangeCanaryEvidence},
		{"refresh_canary_evidence", p.RefreshCanaryEvidence},
		{"release_rule", p.ReleaseRule},
	} {
		if strings.TrimSpace(f.value) == "" {
			gaps = append(gaps, f.name)
		}
	}
	switch p.PublishingStatus {
	case "planned", "testing", "in_review", "published":
	default:
		gaps = append(gaps, "publishing_status")
	}
	switch p.QuotaProjectStrategy {
	case "gum_managed_project", "user_byo_project":
	default:
		gaps = append(gaps, "quota_project_strategy")
	}
	sort.Strings(gaps)
	return gaps
}

// scopeEvidenceGaps returns one message per scope whose promotion is
// half-applied. It covers the reverse direction of the schema's conditional:
// a scope that is not active must not already claim every promoted state,
// and a restricted or sensitive scope that is active must name its evidence.
func scopeEvidenceGaps(m releaseGateManifest) []string {
	gaps := []string{}
	for _, s := range m.Scopes {
		promoted := s.VerificationState == "verified" &&
			s.ProjectEvidenceState == "ready" &&
			s.LiveCanaryState == "passing"

		if s.Status == "active" {
			if !promoted {
				gaps = append(gaps, s.Scope+": active but not (verified, ready, passing)")
			}
			if strings.TrimSpace(s.Evidence) == "" {
				gaps = append(gaps, s.Scope+": active with no evidence pointer")
			}
			if s.Category == "restricted" || s.Category == "sensitive" {
				if !strings.Contains(s.Evidence, "20") {
					gaps = append(gaps, s.Scope+": "+s.Category+" evidence names no date")
				}
			}
			continue
		}

		if promoted {
			gaps = append(gaps, s.Scope+": status "+s.Status+" but every readiness state is promoted")
		}
	}
	sort.Strings(gaps)
	return gaps
}

// TestManagedOAuthProjectReadiness is the spec §7 release gate over the
// managed_project block: once any scope is usable by the managed client, the
// GUM-owned Google Cloud project must carry API-enablement, consent-screen,
// billing-or-terms, token-exchange canary and refresh canary evidence, and
// declare a publishing status and quota strategy inside their enums.
// Otherwise the release is GUM_OAUTH_MANAGED_CLIENT_NOT_READY.
func TestManagedOAuthProjectReadiness(t *testing.T) {
	doc := loadReleaseGateManifest(t)

	// Vacuity guard. Every assertion below is conditional on the manifest
	// claiming a usable scope, so a manifest with none would make the gate
	// pass while proving nothing.
	active := 0
	for _, s := range doc.Scopes {
		if s.Status == "active" {
			active++
		}
	}
	if active == 0 {
		t.Fatal("no scope has status=active; the readiness gate would be vacuous and release tagging requires one")
	}

	if gaps := projectReadinessGaps(doc); len(gaps) != 0 {
		t.Errorf("GUM_OAUTH_MANAGED_CLIENT_NOT_READY: managed_project missing %v", gaps)
	}

	t.Run("a blank canary evidence string is a gap", func(t *testing.T) {
		mutated := doc
		mutated.ManagedProject.TokenExchangeCanaryEvidence = "   "
		mutated.ManagedProject.RefreshCanaryEvidence = ""
		want := []string{"refresh_canary_evidence", "token_exchange_canary_evidence"}
		if got := projectReadinessGaps(mutated); !equalStrings(got, want) {
			t.Errorf("gaps=%v; want %v", got, want)
		}
	})

	t.Run("an out-of-enum publishing status is a gap", func(t *testing.T) {
		mutated := doc
		mutated.ManagedProject.PublishingStatus = "almost_published"
		mutated.ManagedProject.QuotaProjectStrategy = "whatever"
		want := []string{"publishing_status", "quota_project_strategy"}
		if got := projectReadinessGaps(mutated); !equalStrings(got, want) {
			t.Errorf("gaps=%v; want %v", got, want)
		}
	})

	t.Run("a manifest with no usable scope needs a scope before evidence", func(t *testing.T) {
		mutated := doc
		mutated.Scopes = nil
		want := []string{"active_scope_required"}
		if got := projectReadinessGaps(mutated); !equalStrings(got, want) {
			t.Errorf("gaps=%v; want %v", got, want)
		}
	})
}

// TestManagedScopeVerificationEvidence is the spec §7 release gate over the
// scope rows. It closes the reverse half of atomic promotion, which the
// schema's `if status=active then ...` conditional cannot state, and ties the
// manifest to the runtime: a planned scope is neither offered by
// ManagedSupportedScopes nor accepted by canStartGumOAuth.
func TestManagedScopeVerificationEvidence(t *testing.T) {
	doc := loadReleaseGateManifest(t)

	planned := 0
	for _, s := range doc.Scopes {
		if s.Status == "planned" && !s.TestingAllowed {
			planned++
		}
	}
	if planned == 0 {
		t.Fatal("no planned scope in the manifest; the not-requested assertions below would be vacuous")
	}

	if gaps := scopeEvidenceGaps(doc); len(gaps) != 0 {
		t.Errorf("GUM_OAUTH_MANAGED_CLIENT_NOT_READY: %v", gaps)
	}

	t.Run("a half-applied promotion is a gap in both directions", func(t *testing.T) {
		mutated := doc
		mutated.Scopes = append([]releaseGateScope(nil), doc.Scopes...)
		for i := range mutated.Scopes {
			switch mutated.Scopes[i].Status {
			case "active":
				mutated.Scopes[i].LiveCanaryState = "failing"
			case "planned":
				mutated.Scopes[i].VerificationState = "verified"
				mutated.Scopes[i].ProjectEvidenceState = "ready"
				mutated.Scopes[i].LiveCanaryState = "passing"
			}
		}
		gaps := scopeEvidenceGaps(mutated)
		if len(gaps) != len(doc.Scopes) {
			t.Fatalf("gaps=%v; want one per scope (%d)", gaps, len(doc.Scopes))
		}
		if !strings.Contains(strings.Join(gaps, "\n"), "active but not (verified, ready, passing)") {
			t.Errorf("gaps=%v; want the forward direction reported", gaps)
		}
		if !strings.Contains(strings.Join(gaps, "\n"), "every readiness state is promoted") {
			t.Errorf("gaps=%v; want the reverse direction reported", gaps)
		}
	})

	t.Run("an active scope with no evidence pointer is a gap", func(t *testing.T) {
		mutated := doc
		mutated.Scopes = append([]releaseGateScope(nil), doc.Scopes...)
		hit := false
		for i := range mutated.Scopes {
			if mutated.Scopes[i].Status == "active" {
				mutated.Scopes[i].Evidence = ""
				hit = true
			}
		}
		if !hit {
			t.Fatal("no active scope to blank")
		}
		gaps := strings.Join(scopeEvidenceGaps(mutated), "\n")
		if !strings.Contains(gaps, "active with no evidence pointer") {
			t.Errorf("gaps=%q; want the missing-pointer arm", gaps)
		}
	})

	t.Run("planned scopes are not offered and not accepted", func(t *testing.T) {
		offered, err := ManagedSupportedScopes()
		if err != nil {
			t.Fatalf("ManagedSupportedScopes: %v", err)
		}
		offeredSet := map[string]bool{}
		for _, s := range offered {
			offeredSet[s] = true
		}

		m, err := loadManagedScopesManifest(nil)
		if err != nil {
			t.Fatalf("loadManagedScopesManifest: %v", err)
		}
		for _, s := range doc.Scopes {
			if s.Status == "active" || s.TestingAllowed {
				if !offeredSet[s.Scope] {
					t.Errorf("usable scope %q is not offered by ManagedSupportedScopes", s.Scope)
				}
				continue
			}
			if offeredSet[s.Scope] {
				t.Errorf("planned scope %q is offered by ManagedSupportedScopes", s.Scope)
			}
			err := canStartGumOAuth(m, []string{s.Scope})
			var authErr *AuthError
			if !errors.As(err, &authErr) {
				t.Errorf("canStartGumOAuth(%q) err=%v; want an *AuthError", s.Scope, err)
				continue
			}
			if authErr.Code != "GUM_OAUTH_MANAGED_CLIENT_NOT_READY" {
				t.Errorf("canStartGumOAuth(%q) code=%q; want GUM_OAUTH_MANAGED_CLIENT_NOT_READY", s.Scope, authErr.Code)
			}
			if !containsString(authErr.MissingComponents, s.Scope) {
				t.Errorf("missing_components=%v; want the rejected scope named", authErr.MissingComponents)
			}
		}
	})
}

func loadReleaseGateManifest(t *testing.T) releaseGateManifest {
	t.Helper()
	body, err := embeddedManagedScopes()
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var doc releaseGateManifest
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("manifest is not JSON: %v", err)
	}
	if len(doc.Scopes) == 0 {
		t.Fatal("manifest declares no scopes")
	}
	return doc
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func containsString(hay []string, needle string) bool {
	for _, s := range hay {
		if s == needle {
			return true
		}
	}
	return false
}
