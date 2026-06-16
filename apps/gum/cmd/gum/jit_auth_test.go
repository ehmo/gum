package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	keyringlib "github.com/zalando/go-keyring"

	"github.com/ehmo/gum/internal/auth"
	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
)

// byoCatalog builds a tiny catalog with one byo_oauth op (default variant +
// an alternate variant with different scopes) so the scope-derivation logic
// can be tested without coupling to the shipped catalog.
func byoCatalog() *catalog.Catalog {
	return &catalog.Catalog{
		Ops: []catalog.Op{{
			OpID:             "demo.op",
			DefaultVariantID: "demo.default",
			Variants: []catalog.Variant{
				{
					VariantID:    "demo.default",
					AuthStrategy: catalog.AuthStrategyBYOOAuth,
					Scopes:       []string{"https://www.googleapis.com/auth/webmasters.readonly"},
				},
				{
					VariantID:    "demo.write",
					AuthStrategy: catalog.AuthStrategyBYOOAuth,
					Scopes:       []string{"https://www.googleapis.com/auth/webmasters"},
				},
			},
		}},
	}
}

// TestJITByoScopesDefaultVariant pins that with no --variant-id the default
// variant's scopes drive the JIT login.
func TestJITByoScopesDefaultVariant(t *testing.T) {
	got, ok := jitByoScopes(byoCatalog(), "demo.op", "")
	if !ok {
		t.Fatal("ok=false; want the default byo variant scopes")
	}
	if len(got) != 1 || got[0] != "https://www.googleapis.com/auth/webmasters.readonly" {
		t.Errorf("scopes=%v; want default-variant webmasters.readonly", got)
	}
}

// TestJITByoScopesPinnedVariant pins that --variant-id selects that variant's
// scopes, not the default's — so the authorized scopes match what the retry
// resolver will look up for the pinned variant.
func TestJITByoScopesPinnedVariant(t *testing.T) {
	got, ok := jitByoScopes(byoCatalog(), "demo.op", "demo.write")
	if !ok {
		t.Fatal("ok=false; want the pinned variant scopes")
	}
	if len(got) != 1 || got[0] != "https://www.googleapis.com/auth/webmasters" {
		t.Errorf("scopes=%v; want pinned-variant webmasters", got)
	}
}

// TestJITByoScopesNonByoStrategy pins that a non-byo_oauth op never triggers a
// loopback login (it cannot satisfy adc/compound/etc).
func TestJITByoScopesNonByoStrategy(t *testing.T) {
	cat := &catalog.Catalog{Ops: []catalog.Op{{
		OpID:             "adc.op",
		DefaultVariantID: "adc.v1",
		Variants: []catalog.Variant{{
			VariantID:    "adc.v1",
			AuthStrategy: catalog.AuthStrategyADC,
			Scopes:       []string{"https://www.googleapis.com/auth/cloud-platform"},
		}},
	}}}
	if _, ok := jitByoScopes(cat, "adc.op", ""); ok {
		t.Error("ok=true for an adc op; JIT login must not fire for non-byo_oauth")
	}
}

// TestJITByoScopesEdgeCases pins the defensive guards: nil catalog, unknown op,
// unknown pinned variant, and a byo variant declaring no scopes all return
// ok=false.
func TestJITByoScopesEdgeCases(t *testing.T) {
	if _, ok := jitByoScopes(nil, "demo.op", ""); ok {
		t.Error("nil catalog: ok=true; want false")
	}
	if _, ok := jitByoScopes(byoCatalog(), "nope.op", ""); ok {
		t.Error("unknown op: ok=true; want false")
	}
	if _, ok := jitByoScopes(byoCatalog(), "demo.op", "nope.variant"); ok {
		t.Error("unknown variant: ok=true; want false")
	}
	noScopes := &catalog.Catalog{Ops: []catalog.Op{{
		OpID:             "demo.op",
		DefaultVariantID: "v1",
		Variants:         []catalog.Variant{{VariantID: "v1", AuthStrategy: catalog.AuthStrategyBYOOAuth}},
	}}}
	if _, ok := jitByoScopes(noScopes, "demo.op", ""); ok {
		t.Error("scopeless byo variant: ok=true; want false")
	}
}

// TestConfirmAuthorizeAnswers pins the [Y/n] grammar: bare Enter and y/yes mean
// yes; everything else means no. The scope list is always printed so the
// operator sees exactly what they are granting.
func TestConfirmAuthorizeAnswers(t *testing.T) {
	cases := map[string]bool{
		"\n":      true,
		"y\n":     true,
		"Y\n":     true,
		"yes\n":   true,
		" yes \n": true,
		"n\n":     false,
		"no\n":    false,
		"nope\n":  false,
		"":        true, // EOF with no input → default yes
	}
	for input, want := range cases {
		var out bytes.Buffer
		got := confirmAuthorize(strings.NewReader(input), &out, []string{"https://example/scope"})
		if got != want {
			t.Errorf("confirmAuthorize(%q)=%v; want %v", input, got, want)
		}
		if !strings.Contains(out.String(), "https://example/scope") {
			t.Errorf("input %q: prompt did not list the scope: %q", input, out.String())
		}
	}
}

// TestReaderIsTTY pins the non-interactive detections: a non-*os.File reader and
// a regular file are both reported as not-a-terminal so JIT never blocks a
// pipe or an agent.
func TestReaderIsTTY(t *testing.T) {
	if readerIsTTY(strings.NewReader("x")) {
		t.Error("strings.Reader reported as TTY")
	}
	f, err := os.CreateTemp(t.TempDir(), "tty")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })
	if readerIsTTY(f) {
		t.Error("regular file reported as TTY")
	}
	// A closed file makes Stat() error — that branch must report not-a-TTY too.
	closed, err := os.CreateTemp(t.TempDir(), "closed")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	_ = closed.Close()
	if readerIsTTY(closed) {
		t.Error("closed file (Stat error) reported as TTY")
	}
}

// TestJITStdinInteractiveDefault exercises the default jitStdinInteractive
// closure (not the test stub): a command whose stdin is a plain reader is not
// interactive, so JIT stays quiet for pipes.
func TestJITStdinInteractiveDefault(t *testing.T) {
	cmd := &cobra.Command{Use: "call"}
	cmd.SetIn(strings.NewReader("data"))
	if jitStdinInteractive(cmd) {
		t.Error("jitStdinInteractive=true for a non-TTY stdin reader")
	}
}

// authRequiredErr builds the structured error the dispatch boundary produces
// when credential resolution fails (lifecycle.go resolveAuth wraps every
// resolver error as AUTH_REQUIRED).
func authRequiredErr() error {
	return dispatch.NewStructuredError(dispatch.ErrCodeAuthRequired, "no usable credentials")
}

// scopeMissingErr builds the structured error the policy gate produces when a
// profile has no grant for the variant's required scope.
func scopeMissingErr() error {
	return dispatch.NewStructuredError(dispatch.ErrCodeScopeMissing, "missing scope").
		WithDetail("required_scope", "https://www.googleapis.com/auth/webmasters.readonly")
}

// newJITTestCmd builds a cobra command wired with the given stdin answer and a
// captured stderr, plus the root --profile flag resolveProfileFlag expects.
func newJITTestCmd(stdin string) (*cobra.Command, *bytes.Buffer) {
	cmd := &cobra.Command{Use: "call"}
	cmd.PersistentFlags().String("profile", "", "")
	var stderr bytes.Buffer
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetErr(&stderr)
	cmd.SetOut(&bytes.Buffer{})
	return cmd, &stderr
}

// withInteractiveJIT forces jitStdinInteractive on/off for a test.
func withInteractiveJIT(t *testing.T, v bool) {
	orig := jitStdinInteractive
	t.Cleanup(func() { jitStdinInteractive = orig })
	jitStdinInteractive = func(*cobra.Command) bool { return v }
}

// stubLogin replaces interactiveByoLogin, recording the config it was called
// with and returning the supplied error.
func stubLogin(t *testing.T, err error, gotCfg *auth.ByoOAuthConfig) {
	orig := interactiveByoLogin
	t.Cleanup(func() { interactiveByoLogin = orig })
	interactiveByoLogin = func(_ context.Context, cfg auth.ByoOAuthConfig, _ func(string) error) (*auth.Credentials, error) {
		if gotCfg != nil {
			*gotCfg = cfg
		}
		if err != nil {
			return nil, err
		}
		return &auth.Credentials{Token: "tok", StrategyName: "byo_oauth", Scopes: cfg.Scopes, ExpiresAt: time.Now().Add(time.Hour)}, nil
	}
}

// storeJITClient registers a BYO OAuth client in a mocked keychain.
func storeJITClient(t *testing.T) {
	keyringlib.MockInit()
	t.Cleanup(keyringlib.MockInit)
	if err := auth.StoreByoClient(auth.NewOSKeyring(), auth.DefaultAPIKeyProfile, auth.ByoClient{ClientID: "cid-jit"}); err != nil {
		t.Fatalf("StoreByoClient: %v", err)
	}
}

// firstEmbeddedByoOp finds a real byo_oauth op (with scopes) in the shipped
// catalog so maybeJITLogin can be exercised end-to-end against loadCatalog().
func firstEmbeddedByoOp(t *testing.T) string {
	t.Helper()
	cat := loadCatalog()
	if cat == nil {
		t.Skip("embedded catalog unavailable")
	}
	for i := range cat.Ops {
		op := &cat.Ops[i]
		if v := defaultVariant(op); v != nil && v.AuthStrategy == catalog.AuthStrategyBYOOAuth && len(v.Scopes) > 0 {
			return op.OpID
		}
	}
	t.Skip("no byo_oauth op in embedded catalog")
	return ""
}

// TestMaybeJITLoginNonAuthError pins that a non-AUTH_REQUIRED failure is never
// hijacked by JIT — the original error must reach the caller untouched.
func TestMaybeJITLoginNonAuthError(t *testing.T) {
	var called bool
	orig := interactiveByoLogin
	t.Cleanup(func() { interactiveByoLogin = orig })
	interactiveByoLogin = func(context.Context, auth.ByoOAuthConfig, func(string) error) (*auth.Credentials, error) {
		called = true
		return nil, nil
	}
	cmd, _ := newJITTestCmd("y\n")
	other := dispatch.NewStructuredError(dispatch.ErrCodeRiskToolMismatch, "wrong risk")
	if maybeJITLogin(cmd, "demo.op", "", other) {
		t.Error("maybeJITLogin=true for a non-auth error")
	}
	if called {
		t.Error("login attempted for a non-auth error")
	}
}

// TestMaybeJITLoginNotInteractive pins the agent/pipe path: even with a valid
// auth error and a registered client, a non-TTY stdin must not prompt or log
// in — the structured error is left for the agent.
func TestMaybeJITLoginNotInteractive(t *testing.T) {
	storeJITClient(t)
	withInteractiveJIT(t, false)
	var cfg auth.ByoOAuthConfig
	stubLogin(t, nil, &cfg)
	op := firstEmbeddedByoOp(t)

	cmd, _ := newJITTestCmd("y\n")
	if maybeJITLogin(cmd, op, "", authRequiredErr()) {
		t.Error("maybeJITLogin=true for non-interactive stdin")
	}
	if cfg.ClientID != "" {
		t.Error("login attempted despite non-interactive stdin")
	}
}

// TestMaybeJITLoginNoClient pins that an interactive operator with no OAuth
// client registered is not prompted (the structured error already points at
// `gum auth use-oauth-client`).
func TestMaybeJITLoginNoClient(t *testing.T) {
	keyringlib.MockInit()
	t.Cleanup(keyringlib.MockInit)
	withInteractiveJIT(t, true)
	var cfg auth.ByoOAuthConfig
	stubLogin(t, nil, &cfg)
	op := firstEmbeddedByoOp(t)

	cmd, _ := newJITTestCmd("y\n")
	if maybeJITLogin(cmd, op, "", authRequiredErr()) {
		t.Error("maybeJITLogin=true with no client configured")
	}
	if cfg.ClientID != "" {
		t.Error("login attempted with no client configured")
	}
}

// TestMaybeJITLoginDeclined pins that answering 'n' aborts: no login runs and
// the caller does not retry.
func TestMaybeJITLoginDeclined(t *testing.T) {
	storeJITClient(t)
	withInteractiveJIT(t, true)
	var cfg auth.ByoOAuthConfig
	stubLogin(t, nil, &cfg)
	op := firstEmbeddedByoOp(t)

	cmd, _ := newJITTestCmd("n\n")
	if maybeJITLogin(cmd, op, "", authRequiredErr()) {
		t.Error("maybeJITLogin=true after the operator declined")
	}
	if cfg.ClientID != "" {
		t.Error("login attempted after the operator declined")
	}
}

// TestMaybeJITLoginAccepted pins the happy path: an interactive operator with a
// registered client who answers yes triggers a login for exactly the op's
// scopes and gets a retry signal.
func TestMaybeJITLoginAccepted(t *testing.T) {
	storeJITClient(t)
	withInteractiveJIT(t, true)
	var cfg auth.ByoOAuthConfig
	stubLogin(t, nil, &cfg)
	op := firstEmbeddedByoOp(t)
	want, _ := jitByoScopes(loadCatalog(), op, "")

	cmd, _ := newJITTestCmd("\n") // bare Enter = yes
	if !maybeJITLogin(cmd, op, "", authRequiredErr()) {
		t.Fatal("maybeJITLogin=false on the accepted happy path")
	}
	if cfg.ClientID != "cid-jit" {
		t.Errorf("login cfg ClientID=%q; want cid-jit", cfg.ClientID)
	}
	if strings.Join(cfg.Scopes, ",") != strings.Join(want, ",") {
		t.Errorf("login scopes=%v; want the op's resolved scopes %v", cfg.Scopes, want)
	}
}

// TestMaybeJITLoginAcceptedScopeMissing pins the real BYO logout/JIT path:
// after logout the profile has no granted scopes, so the first dispatch fails
// at the scope gate with SCOPE_MISSING before the resolver can return
// AUTH_REQUIRED. JIT must still offer the browser login and retry.
func TestMaybeJITLoginAcceptedScopeMissing(t *testing.T) {
	storeJITClient(t)
	withInteractiveJIT(t, true)
	var cfg auth.ByoOAuthConfig
	stubLogin(t, nil, &cfg)
	op := firstEmbeddedByoOp(t)
	want, _ := jitByoScopes(loadCatalog(), op, "")

	cmd, _ := newJITTestCmd("\n") // bare Enter = yes
	if !maybeJITLogin(cmd, op, "", scopeMissingErr()) {
		t.Fatal("maybeJITLogin=false for SCOPE_MISSING")
	}
	if cfg.ClientID != "cid-jit" {
		t.Errorf("login cfg ClientID=%q; want cid-jit", cfg.ClientID)
	}
	if strings.Join(cfg.Scopes, ",") != strings.Join(want, ",") {
		t.Errorf("login scopes=%v; want the op's resolved scopes %v", cfg.Scopes, want)
	}
}

// TestMaybeJITLoginLoginFails pins that a failed browser authorization does not
// signal a retry and reports the failure on stderr.
func TestMaybeJITLoginLoginFails(t *testing.T) {
	storeJITClient(t)
	withInteractiveJIT(t, true)
	stubLogin(t, context.Canceled, nil)
	op := firstEmbeddedByoOp(t)

	cmd, stderr := newJITTestCmd("y\n")
	if maybeJITLogin(cmd, op, "", authRequiredErr()) {
		t.Error("maybeJITLogin=true even though the login failed")
	}
	if !strings.Contains(stderr.String(), "authorization failed") {
		t.Errorf("stderr=%q; want an authorization-failed notice", stderr.String())
	}
}

// fakeCallDispatcher fails the first Dispatch with AUTH_REQUIRED and succeeds
// every subsequent call, modelling the JIT login + retry sequence.
type fakeCallDispatcher struct {
	calls    int
	firstErr error
	body     []byte
}

func (f *fakeCallDispatcher) Dispatch(context.Context, *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
	f.calls++
	if f.calls == 1 {
		if f.firstErr != nil {
			return nil, f.firstErr
		}
		return nil, authRequiredErr()
	}
	return &dispatch.ShapedResponse{Body: f.body, Format: "json"}, nil
}

type staticCallDispatcher struct {
	calls int
	err   error
	body  []byte
}

func (s *staticCallDispatcher) Dispatch(context.Context, *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return &dispatch.ShapedResponse{Body: s.body, Format: "json"}, nil
}

// TestCallRetriesAfterJITLogin pins the end-to-end wiring: an unauthorized
// `gum call` that triggers a successful JIT login is re-dispatched once and
// then emits the upstream body with no error.
func TestCallRetriesAfterJITLogin(t *testing.T) {
	storeJITClient(t)
	withInteractiveJIT(t, true)
	stubLogin(t, nil, nil)
	op := firstEmbeddedByoOp(t)

	fake := &fakeCallDispatcher{body: []byte(`{"ok":true}`)}
	origDisp := newCallDispatcher
	t.Cleanup(func() { newCallDispatcher = origDisp })
	newCallDispatcher = func(string) dispatch.Dispatcher { return fake }

	cmd := newCallCmd()
	cmd.PersistentFlags().String("profile", "", "")
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(strings.NewReader("y\n"))
	cmd.SetArgs([]string{op, "--risk=read"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if fake.calls != 2 {
		t.Errorf("dispatch calls=%d; want 2 (initial + retry)", fake.calls)
	}
	if !strings.Contains(stdout.String(), `"ok":true`) {
		t.Errorf("stdout=%q; want the retry body", stdout.String())
	}
}

// TestCallRetriesAfterJITLoginScopeMissing covers the live failure mode from a
// logged-out BYO profile: the dispatcher returns SCOPE_MISSING, JIT authorizes
// the variant scopes, and gum call retries once.
func TestCallRetriesAfterJITLoginScopeMissing(t *testing.T) {
	storeJITClient(t)
	withInteractiveJIT(t, true)
	stubLogin(t, nil, nil)
	op := firstEmbeddedByoOp(t)

	stale := &staticCallDispatcher{err: scopeMissingErr()}
	fresh := &staticCallDispatcher{body: []byte(`{"ok":true}`)}
	var factoryCalls int
	origDisp := newCallDispatcher
	t.Cleanup(func() { newCallDispatcher = origDisp })
	newCallDispatcher = func(string) dispatch.Dispatcher {
		factoryCalls++
		if factoryCalls == 1 {
			return stale
		}
		return fresh
	}

	cmd := newCallCmd()
	cmd.PersistentFlags().String("profile", "", "")
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(strings.NewReader("y\n"))
	cmd.SetArgs([]string{op, "--risk=read"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if factoryCalls != 2 {
		t.Errorf("dispatcher factory calls=%d; want 2 (initial + post-login rebuild)", factoryCalls)
	}
	if stale.calls != 1 {
		t.Errorf("stale dispatcher calls=%d; want 1", stale.calls)
	}
	if fresh.calls != 1 {
		t.Errorf("fresh dispatcher calls=%d; want 1", fresh.calls)
	}
	if !strings.Contains(stdout.String(), `"ok":true`) {
		t.Errorf("stdout=%q; want the retry body", stdout.String())
	}
}
