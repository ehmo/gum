package main

// Profile-recorded credential subject, CLI half (bead gum-q0kd).
//
// The dispatcher refuses a credential whose auth_subject_fingerprint is not
// the one the profile recorded. These tests cover the other end of that wire:
// `gum login` records the fingerprint the consent returned, hands the
// recorded one to the flow so the login itself refuses a wrong account, and
// rebinds only when the operator passes --switch-account.

import (
	"context"
	"io"
	"testing"
	"time"

	keyringlib "github.com/zalando/go-keyring"

	"github.com/ehmo/gum/internal/auth"
	"github.com/ehmo/gum/internal/config"
)

// loginFixture isolates the keychain and config root, registers a BYO client,
// and swaps interactiveByoLogin for a stub that reports the config it was
// handed. The returned pointer is filled on each runLogin call.
func loginFixture(t *testing.T, creds *auth.Credentials) *auth.ByoOAuthConfig {
	t.Helper()

	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("GUM_PROFILE", "")
	keyringlib.MockInit()
	t.Cleanup(keyringlib.MockInit)

	if err := auth.StoreByoClient(auth.NewOSKeyring(), auth.DefaultAPIKeyProfile, auth.ByoClient{ClientID: "cid"}); err != nil {
		t.Fatalf("StoreByoClient: %v", err)
	}

	seen := &auth.ByoOAuthConfig{}
	orig := interactiveByoLogin
	t.Cleanup(func() { interactiveByoLogin = orig })
	interactiveByoLogin = func(_ context.Context, cfg auth.ByoOAuthConfig, _ func(string) error) (*auth.Credentials, error) {
		*seen = cfg
		return creds, nil
	}
	return seen
}

func loginCreds(fingerprint string) *auth.Credentials {
	return &auth.Credentials{
		Token:              "tok",
		ExpiresAt:          time.Now().Add(time.Hour),
		Scopes:             []string{"https://www.googleapis.com/auth/gmail.readonly"},
		StrategyName:       byoStrategy,
		SubjectFingerprint: fingerprint,
	}
}

func runLoginForTest(t *testing.T, switchAccount bool) {
	t.Helper()

	cmd := newCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := runLogin(cmd, []string{"gmail.readonly"}, nil, false, true, switchAccount); err != nil {
		t.Fatalf("runLogin: %v", err)
	}
}

func recordedSubject(t *testing.T, strategy string) string {
	t.Helper()

	cfg, _, err := config.Load(auth.DefaultAPIKeyProfile)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	value, _ := cfg.Get(expectedSubjectPrefix + strategy)
	return value
}

func TestLoginRecordsTheAccountFingerprint(t *testing.T) {
	seen := loginFixture(t, loginCreds("fp-A"))

	// Nothing recorded yet, so the flow must be given no expectation: a first
	// login has no account to compare against.
	runLoginForTest(t, false)
	if seen.ExpectedSubject != "" {
		t.Errorf("first login was handed ExpectedSubject %q, want empty", seen.ExpectedSubject)
	}
	if got := recordedSubject(t, byoStrategy); got != "fp-A" {
		t.Fatalf("recorded subject = %q, want fp-A", got)
	}

	// The second login is handed the recorded fingerprint, which is what
	// makes a wrong account a refusal inside the flow.
	runLoginForTest(t, false)
	if seen.ExpectedSubject != "fp-A" {
		t.Errorf("second login was handed ExpectedSubject %q, want fp-A", seen.ExpectedSubject)
	}
	if seen.AllowSubjectChange {
		t.Error("AllowSubjectChange is set without --switch-account")
	}
}

func TestLoginSwitchAccountRebindsTheProfile(t *testing.T) {
	seen := loginFixture(t, loginCreds("fp-A"))
	runLoginForTest(t, false)

	interactiveByoLoginReturns(t, loginCreds("fp-B"), seen)
	runLoginForTest(t, true)

	if !seen.AllowSubjectChange {
		t.Error("--switch-account did not set AllowSubjectChange")
	}
	if got := recordedSubject(t, byoStrategy); got != "fp-B" {
		t.Errorf("recorded subject = %q, want fp-B after --switch-account", got)
	}
}

// interactiveByoLoginReturns re-stubs the login closure mid-test so one test
// can run two logins as two different accounts.
func interactiveByoLoginReturns(t *testing.T, creds *auth.Credentials, seen *auth.ByoOAuthConfig) {
	t.Helper()

	orig := interactiveByoLogin
	t.Cleanup(func() { interactiveByoLogin = orig })
	interactiveByoLogin = func(_ context.Context, cfg auth.ByoOAuthConfig, _ func(string) error) (*auth.Credentials, error) {
		*seen = cfg
		return creds, nil
	}
}

func TestExpectedAuthSubjectsReadsEveryStrategy(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	cfg := &config.Config{}
	cfg.Set(expectedSubjectPrefix+"byo_oauth", "fp-byo")
	cfg.Set(expectedSubjectPrefix+"gum_oauth", "fp-managed")
	cfg.Set("output.default_format", "json")
	if err := config.Save(auth.DefaultAPIKeyProfile, cfg); err != nil {
		t.Fatalf("config.Save: %v", err)
	}

	got := expectedAuthSubjects(auth.DefaultAPIKeyProfile)
	if len(got) != 2 || got["byo_oauth"] != "fp-byo" || got["gum_oauth"] != "fp-managed" {
		t.Fatalf("expectedAuthSubjects = %v, want the two recorded strategies only", got)
	}
}

// TestExpectedAuthSubjectsIsNilWhenUnrecorded keeps a fresh profile usable:
// no recorded subject means no check, not a locked-out operator.
func TestExpectedAuthSubjectsIsNilWhenUnrecorded(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if got := expectedAuthSubjects(auth.DefaultAPIKeyProfile); got != nil {
		t.Fatalf("expectedAuthSubjects = %v, want nil for a profile with no recorded subject", got)
	}
}
