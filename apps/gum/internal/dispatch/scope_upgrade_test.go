package dispatch

// Spec §13 "managed-scope re-consent", kernel half (docs/test-matrix.md row
// 136). The kernel owns the approval object, its request hash, the
// post-consent verification and the audit event. It never re-runs the
// operation.
//
// Every refusal path here has the same required outcome: nothing is stored,
// the allowlist does not move, and the caller gets the original SCOPE_MISSING
// envelope back. Exactly one audit event is written per attempt.

import (
	"context"
	"errors"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

const (
	upgradeOpID     = "test.upgrade.op"
	upgradeVariant  = "test.upgrade.op.v1"
	upgradeProfile  = "work"
	upgradeSubject  = "sha256:bound-account"
	upgradeScopeDrv = "https://www.googleapis.com/auth/drive"
	upgradeScopeCal = "https://www.googleapis.com/auth/calendar"
)

// captureAudit records every §11 entry the kernel appends.
type captureAudit struct{ entries []map[string]any }

func (c *captureAudit) Append(entry map[string]any) { c.entries = append(c.entries, entry) }

// only returns the single audit entry the attempt must have written.
func (c *captureAudit) only(t *testing.T) map[string]any {
	t.Helper()

	if len(c.entries) != 1 {
		t.Fatalf("audit wrote %d entries; §13 writes exactly one per attempt", len(c.entries))
	}
	return c.entries[0]
}

// upgradeCatalog carries one byo_oauth op declaring two scopes, plus an adc op
// declaring one. The adc op exists to prove the flow refuses strategies whose
// credential a loopback consent cannot produce.
func upgradeCatalog() *catalog.Catalog {
	variant := func(id string, strategy catalog.AuthStrategy, scopes ...string) catalog.Variant {
		return catalog.Variant{
			VariantID:     id,
			Stability:     catalog.StabilityStable,
			InterfaceKind: catalog.InterfaceKindSDKNative,
			BackendKind:   catalog.BackendKindTypedRestSDK,
			RiskClass:     catalog.RiskClassRead,
			AuthStrategy:  strategy,
			Scopes:        scopes,
			Binding: &catalog.Binding{
				BindingSchemaVersion: 1,
				AdapterKey:           "test.adapter",
				OperationKey:         id + ".exec",
			},
		}
	}
	return &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratedAt:          "2026-01-01T00:00:00Z",
		GeneratorVersion:     "test@0.0.0",
		Ops: []catalog.Op{
			{
				OpID:             upgradeOpID,
				OpSchemaVersion:  1,
				Title:            "Scoped op",
				Summary:          "Used by the §13 re-consent tests.",
				DefaultVariantID: upgradeVariant,
				// Authored unsorted and duplicated on purpose: the approval
				// object must normalize before it hashes.
				Variants: []catalog.Variant{variant(upgradeVariant, catalog.AuthStrategyBYOOAuth, upgradeScopeDrv, upgradeScopeCal, upgradeScopeDrv)},
			},
			{
				OpID:             "test.adc.op",
				OpSchemaVersion:  1,
				Title:            "ADC op",
				Summary:          "Used by the §13 re-consent tests.",
				DefaultVariantID: "test.adc.op.v1",
				Variants:         []catalog.Variant{variant("test.adc.op.v1", catalog.AuthStrategyADC, upgradeScopeDrv)},
			},
		},
	}
}

// newUpgradeDispatcher builds a kernel with the §13 login wired. login nil
// leaves the flow off, which is the shipped CLI configuration.
func newUpgradeDispatcher(login ScopeUpgradeLogin, audit *captureAudit) *dispatcher {
	d := &dispatcher{
		snapshot:             upgradeCatalog(),
		adapters:             map[string]Adapter{},
		profileName:          upgradeProfile,
		profilePolicy:        ProfilePolicy{},
		expectedAuthSubjects: map[string]string{string(catalog.AuthStrategyBYOOAuth): upgradeSubject},
		scopeUpgradeLogin:    login,
	}
	if audit != nil {
		d.auditSink = audit
	}
	return d
}

// grantingLogin returns a login that reports granted and records how often it
// ran and with which request.
func grantingLogin(granted []string, subject string, calls *int, seen *ScopeUpgradeRequest) ScopeUpgradeLogin {
	return func(_ context.Context, req ScopeUpgradeRequest) (ScopeUpgradeGrant, error) {
		*calls++
		if seen != nil {
			*seen = req
		}
		return ScopeUpgradeGrant{GrantedScopes: granted, AuthSubjectFingerprint: subject}, nil
	}
}

// upgradeRequest derives the approval object for a SCOPE_MISSING refusal of
// the byo_oauth op, failing the test when the kernel declines to offer one.
func upgradeRequest(t *testing.T, d *dispatcher) ScopeUpgradeRequest {
	t.Helper()

	inv := &Invocation{OpID: upgradeOpID, Args: map[string]any{"q": "x"}}
	se := NewStructuredError(ErrCodeScopeMissing, "missing")
	req, ok := d.ScopeUpgradeFor(inv, se)
	if !ok {
		t.Fatal("ScopeUpgradeFor declined a byo_oauth SCOPE_MISSING refusal")
	}
	return req
}

// acceptedReply is a fully bound approval for req.
func acceptedReply(req ScopeUpgradeRequest) ScopeUpgradeReply {
	return ScopeUpgradeReply{
		Action:         ScopeUpgradeAccept,
		Approved:       true,
		OpID:           req.OpID,
		VariantID:      req.VariantID,
		Profile:        req.Profile,
		RequestHash:    req.RequestHash,
		RequiredScopes: req.RequiredScopes,
	}
}

func TestScopeUpgradeApprovalObject(t *testing.T) {
	t.Run("carries all five bindings", func(t *testing.T) {
		req := upgradeRequest(t, newUpgradeDispatcher(grantingLogin(nil, "", new(int), nil), nil))

		if req.OpID != upgradeOpID {
			t.Errorf("op_id = %q; want %q", req.OpID, upgradeOpID)
		}
		if req.VariantID != upgradeVariant {
			t.Errorf("variant_id = %q; want %q", req.VariantID, upgradeVariant)
		}
		if req.Profile != upgradeProfile {
			t.Errorf("profile = %q; want %q", req.Profile, upgradeProfile)
		}
		if req.ExpectedSubject != upgradeSubject {
			t.Errorf("expected_subject = %q; want the profile's bound account %q", req.ExpectedSubject, upgradeSubject)
		}
		if req.RequestHash == "" {
			t.Error("request_hash is empty; the approval would bind to nothing")
		}
		if len(req.RequestHash) != 64 {
			t.Errorf("request_hash is %d characters; want 64 hex characters of sha256", len(req.RequestHash))
		}
	})

	t.Run("requests the variant's exact scope set, sorted and deduplicated", func(t *testing.T) {
		req := upgradeRequest(t, newUpgradeDispatcher(grantingLogin(nil, "", new(int), nil), nil))

		want := []string{upgradeScopeCal, upgradeScopeDrv}
		if len(req.RequiredScopes) != len(want) {
			t.Fatalf("required_scopes = %v; want %v", req.RequiredScopes, want)
		}
		for i := range want {
			if req.RequiredScopes[i] != want[i] {
				t.Fatalf("required_scopes = %v; want %v", req.RequiredScopes, want)
			}
		}
	})

	t.Run("the hash binds the args", func(t *testing.T) {
		d := newUpgradeDispatcher(grantingLogin(nil, "", new(int), nil), nil)
		se := NewStructuredError(ErrCodeScopeMissing, "missing")

		first, _ := d.ScopeUpgradeFor(&Invocation{OpID: upgradeOpID, Args: map[string]any{"q": "x"}}, se)
		second, _ := d.ScopeUpgradeFor(&Invocation{OpID: upgradeOpID, Args: map[string]any{"q": "y"}}, se)
		same, _ := d.ScopeUpgradeFor(&Invocation{OpID: upgradeOpID, Args: map[string]any{"q": "x"}}, se)

		if first.RequestHash == second.RequestHash {
			t.Error("two calls with different args share a request_hash; an approval would transfer between them")
		}
		if first.RequestHash != same.RequestHash {
			t.Error("the same call derives two different request hashes; the reply could never match")
		}
	})

	t.Run("gate 5 reports the hash the approval is checked against", func(t *testing.T) {
		d := newUpgradeDispatcher(grantingLogin(nil, "", new(int), nil), nil)
		inv := &Invocation{OpID: upgradeOpID, Args: map[string]any{"q": "x"}}

		se := d.evaluatePolicy(context.Background(), inv)
		if se == nil || se.ErrCode != ErrCodeScopeMissing {
			t.Fatalf("evaluatePolicy = %v; want SCOPE_MISSING with no scopes granted", se)
		}
		req := upgradeRequest(t, d)
		if got := se.Detail["request_hash"]; got != req.RequestHash {
			t.Errorf("refusal request_hash = %v; approval request_hash = %v", got, req.RequestHash)
		}
	})

	t.Run("declines what it cannot re-consent", func(t *testing.T) {
		scopeMissing := NewStructuredError(ErrCodeScopeMissing, "missing")
		inv := &Invocation{OpID: upgradeOpID, Args: map[string]any{}}

		cases := []struct {
			name  string
			d     *dispatcher
			inv   *Invocation
			se    *StructuredError
			cause string
		}{
			{"no login wired", newUpgradeDispatcher(nil, nil), inv, scopeMissing, "the flow is off outside the MCP server"},
			{"a different error", newUpgradeDispatcher(grantingLogin(nil, "", new(int), nil), nil), inv, NewStructuredError(ErrCodeAuthRequired, "x"), "only SCOPE_MISSING is re-consentable"},
			{"a non-byo_oauth strategy", newUpgradeDispatcher(grantingLogin(nil, "", new(int), nil), nil), &Invocation{OpID: "test.adc.op"}, scopeMissing, "a loopback consent cannot produce an ADC credential"},
			{"an unknown op", newUpgradeDispatcher(grantingLogin(nil, "", new(int), nil), nil), &Invocation{OpID: "no.such.op"}, scopeMissing, "no variant, no scopes"},
			{"a nil error", newUpgradeDispatcher(grantingLogin(nil, "", new(int), nil), nil), inv, nil, "there is nothing to re-consent"},
		}
		for _, tc := range cases {
			if _, ok := tc.d.ScopeUpgradeFor(tc.inv, tc.se); ok {
				t.Errorf("%s: ScopeUpgradeFor offered a re-consent; %s", tc.name, tc.cause)
			}
		}
	})
}

func TestScopeUpgradeApplyGrants(t *testing.T) {
	var calls int
	var seen ScopeUpgradeRequest
	audit := &captureAudit{}
	granted := []string{upgradeScopeCal, upgradeScopeDrv, "https://www.googleapis.com/auth/userinfo.email"}
	d := newUpgradeDispatcher(grantingLogin(granted, upgradeSubject, &calls, &seen), audit)
	req := upgradeRequest(t, d)

	outcome := d.ApplyScopeUpgrade(context.Background(), req, acceptedReply(req))
	if outcome == nil {
		t.Fatal("ApplyScopeUpgrade refused an approved, fully bound reply against a complete grant")
	}

	t.Run("reports SCOPE_GRANTED", func(t *testing.T) {
		if outcome.Status != ScopeUpgradeStatusGranted {
			t.Errorf("status = %q; want %q", outcome.Status, ScopeUpgradeStatusGranted)
		}
		if outcome.OpID != upgradeOpID || outcome.RequestHash != req.RequestHash || outcome.Profile != upgradeProfile {
			t.Errorf("outcome = %+v; want the approval's op_id, request_hash and profile", outcome)
		}
		if outcome.AuthSubjectFingerprint != upgradeSubject {
			t.Errorf("auth_subject_fingerprint = %q; want %q", outcome.AuthSubjectFingerprint, upgradeSubject)
		}
		if len(outcome.GrantedScopes) != len(granted) {
			t.Errorf("granted_scopes = %v; want all %d scopes the consent returned", outcome.GrantedScopes, len(granted))
		}
	})

	t.Run("the consent asks for exactly the approved scopes", func(t *testing.T) {
		if calls != 1 {
			t.Fatalf("login ran %d times; want exactly 1", calls)
		}
		if !sameScopes(seen.RequiredScopes, req.RequiredScopes) {
			t.Errorf("login asked for %v; the approval covered %v", seen.RequiredScopes, req.RequiredScopes)
		}
		if seen.ExpectedSubject != upgradeSubject {
			t.Errorf("login expected subject %q; want %q", seen.ExpectedSubject, upgradeSubject)
		}
	})

	t.Run("gate 5 stops refusing the op", func(t *testing.T) {
		inv := &Invocation{OpID: upgradeOpID, Args: map[string]any{"q": "x"}}
		if se := d.evaluatePolicy(context.Background(), inv); se != nil {
			t.Errorf("evaluatePolicy = %v after the grant; the allowlist did not pick up the new scopes", se)
		}
	})

	t.Run("writes one granted audit event", func(t *testing.T) {
		entry := audit.only(t)
		if entry["event"] != scopeUpgradeAuditEvent {
			t.Errorf("event = %v; want %q", entry["event"], scopeUpgradeAuditEvent)
		}
		if entry["outcome"] != scopeUpgradeOutcomeGranted {
			t.Errorf("outcome = %v; want %q", entry["outcome"], scopeUpgradeOutcomeGranted)
		}
		if entry["auth_subject_fingerprint"] != upgradeSubject {
			t.Errorf("auth_subject_fingerprint = %v; want %q", entry["auth_subject_fingerprint"], upgradeSubject)
		}
		if _, ok := entry["reason"]; ok {
			t.Errorf("a granted event carries reason = %v; a reason names a refusal", entry["reason"])
		}
	})
}

func TestScopeUpgradeRefusals(t *testing.T) {
	// Every case must leave the allowlist untouched and write one audit event
	// naming its outcome. loginGrant nil means the login must never run.
	cases := []struct {
		name       string
		reply      func(ScopeUpgradeRequest) ScopeUpgradeReply
		loginGrant *ScopeUpgradeGrant
		loginErr   error
		outcome    string
		wantLogin  bool
	}{
		{
			name:    "a declined form",
			reply:   func(ScopeUpgradeRequest) ScopeUpgradeReply { return ScopeUpgradeReply{Action: ScopeUpgradeDecline} },
			outcome: scopeUpgradeOutcomeNotApproved,
		},
		{
			name:    "a cancelled form",
			reply:   func(ScopeUpgradeRequest) ScopeUpgradeReply { return ScopeUpgradeReply{Action: ScopeUpgradeCancel} },
			outcome: scopeUpgradeOutcomeNotApproved,
		},
		{
			name: "an accepted form carrying approve=false",
			reply: func(req ScopeUpgradeRequest) ScopeUpgradeReply {
				r := acceptedReply(req)
				r.Approved = false
				return r
			},
			outcome: scopeUpgradeOutcomeNotApproved,
		},
		{
			name: "an approval minted for another call",
			reply: func(req ScopeUpgradeRequest) ScopeUpgradeReply {
				r := acceptedReply(req)
				r.RequestHash = "0000000000000000000000000000000000000000000000000000000000000000"
				return r
			},
			outcome: scopeUpgradeOutcomeBinding,
		},
		{
			name: "an approval naming another op",
			reply: func(req ScopeUpgradeRequest) ScopeUpgradeReply {
				r := acceptedReply(req)
				r.OpID = "test.adc.op"
				return r
			},
			outcome: scopeUpgradeOutcomeBinding,
		},
		{
			name: "an approval naming another profile",
			reply: func(req ScopeUpgradeRequest) ScopeUpgradeReply {
				r := acceptedReply(req)
				r.Profile = "personal"
				return r
			},
			outcome: scopeUpgradeOutcomeBinding,
		},
		{
			name: "an approval widening the scope set",
			reply: func(req ScopeUpgradeRequest) ScopeUpgradeReply {
				r := acceptedReply(req)
				r.RequiredScopes = append(append([]string(nil), req.RequiredScopes...), "https://mail.google.com/")
				return r
			},
			outcome: scopeUpgradeOutcomeBinding,
		},
		{
			name:      "a failed consent",
			reply:     acceptedReply,
			loginErr:  errors.New("consent window closed"),
			outcome:   scopeUpgradeOutcomeLoginFailed,
			wantLogin: true,
		},
		{
			name:       "a partial consent",
			reply:      acceptedReply,
			loginGrant: &ScopeUpgradeGrant{GrantedScopes: []string{upgradeScopeDrv}, AuthSubjectFingerprint: upgradeSubject},
			outcome:    scopeUpgradeOutcomeShortfall,
			wantLogin:  true,
		},
		{
			name:       "a consent on another account",
			reply:      acceptedReply,
			loginGrant: &ScopeUpgradeGrant{GrantedScopes: []string{upgradeScopeCal, upgradeScopeDrv}, AuthSubjectFingerprint: "sha256:someone-else"},
			outcome:    scopeUpgradeOutcomeSubject,
			wantLogin:  true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls int
			audit := &captureAudit{}
			login := func(context.Context, ScopeUpgradeRequest) (ScopeUpgradeGrant, error) {
				calls++
				if tc.loginErr != nil {
					return ScopeUpgradeGrant{}, tc.loginErr
				}
				if tc.loginGrant != nil {
					return *tc.loginGrant, nil
				}
				return ScopeUpgradeGrant{}, nil
			}
			d := newUpgradeDispatcher(login, audit)
			req := upgradeRequest(t, d)

			if outcome := d.ApplyScopeUpgrade(context.Background(), req, tc.reply(req)); outcome != nil {
				t.Fatalf("ApplyScopeUpgrade returned %+v; a refusal must return nil so the caller re-emits SCOPE_MISSING", outcome)
			}
			if tc.wantLogin && calls != 1 {
				t.Errorf("login ran %d times; want 1", calls)
			}
			if !tc.wantLogin && calls != 0 {
				t.Errorf("login ran %d times; the reply was refused before any consent", calls)
			}
			if got := d.allowedScopes(); len(got) != 0 {
				t.Errorf("allowed scopes = %v; a refused re-consent must not widen the allowlist", got)
			}
			if se := d.evaluatePolicy(context.Background(), &Invocation{OpID: upgradeOpID}); se == nil || se.ErrCode != ErrCodeScopeMissing {
				t.Errorf("evaluatePolicy = %v; want the op still refused with SCOPE_MISSING", se)
			}
			entry := audit.only(t)
			if entry["outcome"] != tc.outcome {
				t.Errorf("audit outcome = %v; want %q", entry["outcome"], tc.outcome)
			}
			if entry["reason"] == nil || entry["reason"] == "" {
				if tc.outcome != scopeUpgradeOutcomeNotApproved {
					t.Errorf("audit entry carries no reason for outcome %q", tc.outcome)
				}
			}
			if entry["op_id"] != upgradeOpID || entry["request_hash"] != req.RequestHash {
				t.Errorf("audit entry = %v; want the approval's op_id and request_hash", entry)
			}
		})
	}
}

func TestScopeUpgradeConcurrentAllowlist(t *testing.T) {
	// A re-consent rewrites the allowlist while other invocations are inside
	// gate 5. The race detector runs this test as part of the suite.
	var calls int
	d := newUpgradeDispatcher(grantingLogin([]string{upgradeScopeCal, upgradeScopeDrv}, upgradeSubject, &calls, nil), nil)
	req := upgradeRequest(t, d)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			_ = d.evaluatePolicy(context.Background(), &Invocation{OpID: upgradeOpID})
		}
	}()
	if outcome := d.ApplyScopeUpgrade(context.Background(), req, acceptedReply(req)); outcome == nil {
		t.Fatal("ApplyScopeUpgrade refused a valid approval")
	}
	<-done

	if se := d.evaluatePolicy(context.Background(), &Invocation{OpID: upgradeOpID}); se != nil {
		t.Fatalf("evaluatePolicy = %v after the grant; want the op allowed", se)
	}
}
