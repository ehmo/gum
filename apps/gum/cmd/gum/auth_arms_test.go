package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	keyringlib "github.com/zalando/go-keyring"

	"github.com/ehmo/gum/internal/auth"
)

// newCmd returns a bare cobra command for the helpers that read stdin and the
// root --profile through a *cobra.Command.
func newCmd() *cobra.Command { return &cobra.Command{Use: "probe"} }

func TestReadClientSecretSources(t *testing.T) {
	t.Run("from file", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "secret")
		if err := os.WriteFile(p, []byte("  csec\n"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		got, given, err := readClientSecret(newCmd(), false, p)
		if err != nil || !given || got != "csec" {
			t.Fatalf("got (%q,%v,%v); want (\"csec\",true,nil)", got, given, err)
		}
	})

	t.Run("missing file", func(t *testing.T) {
		_, given, err := readClientSecret(newCmd(), false, filepath.Join(t.TempDir(), "absent"))
		if err == nil || !strings.Contains(err.Error(), "read --secret-file") {
			t.Fatalf("got %v; want a read --secret-file error", err)
		}
		if !given {
			t.Fatal("a named --secret-file must count as a supplied source")
		}
	})

	t.Run("stdin read error", func(t *testing.T) {
		boom := errors.New("broken pipe")
		c := newCmd()
		c.SetIn(errReader{err: boom})
		_, _, err := readClientSecret(c, true, "")
		if err == nil || !errors.Is(err, boom) {
			t.Fatalf("got %v; want the underlying read error", err)
		}
	})
}

func TestReadDeveloperTokenSources(t *testing.T) {
	t.Run("from file", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "tok")
		if err := os.WriteFile(p, []byte("dev-token\n"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		got, err := readDeveloperToken(newCmd(), p)
		if err != nil || got != "dev-token" {
			t.Fatalf("got (%q,%v); want (\"dev-token\",nil)", got, err)
		}
	})

	t.Run("missing file", func(t *testing.T) {
		_, err := readDeveloperToken(newCmd(), filepath.Join(t.TempDir(), "absent"))
		if err == nil || !strings.Contains(err.Error(), "read --from-file") {
			t.Fatalf("got %v; want a read --from-file error", err)
		}
	})

	t.Run("stdin read error", func(t *testing.T) {
		boom := errors.New("broken pipe")
		c := newCmd()
		c.SetIn(errReader{err: boom})
		_, err := readDeveloperToken(c, "")
		if err == nil || !errors.Is(err, boom) {
			t.Fatalf("got %v; want the underlying read error", err)
		}
	})
}

func TestReadAPIKeySurfacesAStdinError(t *testing.T) {
	boom := errors.New("broken pipe")
	c := newCmd()
	c.SetIn(errReader{err: boom})
	_, err := readAPIKey(c, true, "")
	if err == nil || !errors.Is(err, boom) {
		t.Fatalf("got %v; want the underlying read error", err)
	}
}

func TestUseAdsDeveloperTokenArms(t *testing.T) {
	t.Run("rejects a positional token", func(t *testing.T) {
		cmd := newAuthUseAdsDeveloperTokenCmd()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs([]string{"secret-token"})
		err := cmd.Execute()
		if err == nil || !strings.Contains(err.Error(), "does not accept positional arguments") {
			t.Fatalf("got %v; want the positional-argument refusal", err)
		}
		if strings.Contains(out.String(), "secret-token") {
			t.Fatal("the refusal must not echo the token")
		}
	})

	t.Run("surfaces a read error", func(t *testing.T) {
		cmd := newAuthUseAdsDeveloperTokenCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"--from-file", filepath.Join(t.TempDir(), "absent")})
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "read --from-file") {
			t.Fatalf("got %v; want a read --from-file error", err)
		}
	})

	t.Run("rejects an empty token", func(t *testing.T) {
		cmd := newAuthUseAdsDeveloperTokenCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetIn(strings.NewReader("   \n"))
		cmd.SetArgs([]string{"--stdin"})
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "developer token is empty") {
			t.Fatalf("got %v; want the empty-token refusal", err)
		}
	})

	t.Run("stores in the keychain", func(t *testing.T) {
		keyringlib.MockInit()
		t.Cleanup(keyringlib.MockInit)

		cmd := newAuthUseAdsDeveloperTokenCmd()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetIn(strings.NewReader("dev-token\n"))
		cmd.SetArgs([]string{"--stdin"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if !strings.Contains(out.String(), "stored in OS keychain") {
			t.Fatalf("output %q: want the stored-in-keychain confirmation", out.String())
		}
		if got := auth.LookupDeveloperToken(auth.NewOSKeyring(), auth.DefaultAPIKeyProfile); got != "dev-token" {
			t.Fatalf("LookupDeveloperToken: got %q; want %q", got, "dev-token")
		}
	})

	t.Run("a store fault exits non-zero", func(t *testing.T) {
		keyringlib.MockInitWithError(errors.New("keychain locked"))
		t.Cleanup(keyringlib.MockInit)

		cmd := newAuthUseAdsDeveloperTokenCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetIn(strings.NewReader("dev-token\n"))
		cmd.SetArgs([]string{"--stdin"})
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "store in OS keychain") {
			t.Fatalf("got %v; want a store-in-keychain error", err)
		}
	})
}

func TestUseOAuthClientErrorArms(t *testing.T) {
	t.Run("secret read error", func(t *testing.T) {
		cmd := newAuthUseOAuthClientCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"--client-id", "cid", "--secret-file", filepath.Join(t.TempDir(), "absent")})
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "read --secret-file") {
			t.Fatalf("got %v; want a read --secret-file error", err)
		}
	})

	t.Run("stored-client read error", func(t *testing.T) {
		keyringlib.MockInitWithError(errors.New("keychain locked"))
		t.Cleanup(keyringlib.MockInit)

		cmd := newAuthUseOAuthClientCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"--client-id", "cid"})
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "read stored client") {
			t.Fatalf("got %v; want a read-stored-client error", err)
		}
	})

	t.Run("store error", func(t *testing.T) {
		keyringlib.MockInitWithError(errors.New("keychain locked"))
		t.Cleanup(keyringlib.MockInit)

		cmd := newAuthUseOAuthClientCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetIn(strings.NewReader("csec\n"))
		// --secret-stdin marks the secret as supplied, so the read-stored-client
		// branch is skipped and the store itself is what fails.
		cmd.SetArgs([]string{"--client-id", "cid", "--secret-stdin"})
		err := cmd.Execute()
		if err == nil || !strings.Contains(err.Error(), "use-oauth-client") {
			t.Fatalf("got %v; want a store error", err)
		}
		if strings.Contains(err.Error(), "read stored client") {
			t.Fatalf("got %v; want the store error, not the load error", err)
		}
	})
}

func TestAuthProbeResolverStrategyArms(t *testing.T) {
	stub := probeResolverFunc(func(context.Context, []string) (*auth.Credentials, error) {
		return &auth.Credentials{StrategyName: "stub"}, nil
	})
	origADC, origBYO := newAuthProbeADCResolver, newAuthProbeByoResolver
	t.Cleanup(func() {
		newAuthProbeADCResolver, newAuthProbeByoResolver = origADC, origBYO
	})
	newAuthProbeADCResolver = func() auth.Resolver { return stub }
	newAuthProbeByoResolver = func(auth.ByoOAuthConfig) auth.Resolver { return stub }

	t.Run("empty strategy falls back to auto", func(t *testing.T) {
		keyringlib.MockInit()
		t.Cleanup(keyringlib.MockInit)
		r, scopes, err := authProbeResolver(newCmd(), []string{"gmail.readonly"}, "  ")
		if err != nil || r == nil || len(scopes) != 1 {
			t.Fatalf("got (%v,%v,%v); want the ADC fallback", r, scopes, err)
		}
	})

	t.Run("byo_oauth without a client", func(t *testing.T) {
		keyringlib.MockInit()
		t.Cleanup(keyringlib.MockInit)
		_, _, err := authProbeResolver(newCmd(), []string{"gmail.readonly"}, "byo_oauth")
		var ae *auth.AuthError
		if !errors.As(err, &ae) || ae.Code != "BYO_OAUTH_CLIENT_NOT_CONFIGURED" {
			t.Fatalf("got %v; want BYO_OAUTH_CLIENT_NOT_CONFIGURED", err)
		}
	})

	t.Run("byo_oauth with a client", func(t *testing.T) {
		keyringlib.MockInit()
		t.Cleanup(keyringlib.MockInit)
		if err := auth.StoreByoClient(auth.NewOSKeyring(), auth.DefaultAPIKeyProfile, auth.ByoClient{ClientID: "cid"}); err != nil {
			t.Fatalf("StoreByoClient: %v", err)
		}
		r, _, err := authProbeResolver(newCmd(), []string{"gmail.readonly"}, "byo_oauth")
		if err != nil || r == nil {
			t.Fatalf("got (%v,%v); want the byo resolver", r, err)
		}
	})

	t.Run("byo_oauth with a keychain fault", func(t *testing.T) {
		keyringlib.MockInitWithError(errors.New("keychain locked"))
		t.Cleanup(keyringlib.MockInit)
		_, _, err := authProbeResolver(newCmd(), []string{"gmail.readonly"}, "byo_oauth")
		if err == nil || !strings.Contains(err.Error(), "read OAuth client from keychain") {
			t.Fatalf("got %v; want a keychain read error", err)
		}
	})

	t.Run("unknown strategy", func(t *testing.T) {
		_, _, err := authProbeResolver(newCmd(), nil, "gcloud")
		if err == nil || !strings.Contains(err.Error(), "--strategy must be one of") {
			t.Fatalf("got %v; want the strategy refusal", err)
		}
	})
}

// TestAuthProbeResolverDefaults exercises the production constructors behind
// the two injection points. Both only build a struct; neither touches the
// network until Resolve is called.
func TestAuthProbeResolverDefaults(t *testing.T) {
	if newAuthProbeADCResolver() == nil {
		t.Fatal("default ADC resolver is nil")
	}
	if newAuthProbeByoResolver(auth.ByoOAuthConfig{ClientID: "cid"}) == nil {
		t.Fatal("default byo resolver is nil")
	}
}

func TestAuthProbeCmdSurfacesResolverAndResolveErrors(t *testing.T) {
	origADC, origBYO := newAuthProbeADCResolver, newAuthProbeByoResolver
	t.Cleanup(func() {
		newAuthProbeADCResolver, newAuthProbeByoResolver = origADC, origBYO
	})

	t.Run("resolver selection error", func(t *testing.T) {
		cmd := newAuthProbeCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"--strategy", "gcloud"})
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "--strategy must be one of") {
			t.Fatalf("got %v; want the strategy refusal", err)
		}
	})

	t.Run("resolve error", func(t *testing.T) {
		keyringlib.MockInit()
		t.Cleanup(keyringlib.MockInit)
		boom := errors.New("no credentials")
		newAuthProbeADCResolver = func() auth.Resolver {
			return probeResolverFunc(func(context.Context, []string) (*auth.Credentials, error) {
				return nil, boom
			})
		}
		cmd := newAuthProbeCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"--strategy", "adc"})
		if err := cmd.Execute(); !errors.Is(err, boom) {
			t.Fatalf("got %v; want the resolver's error", err)
		}
	})
}

func TestRunLoginErrorArms(t *testing.T) {
	t.Run("keychain read fault", func(t *testing.T) {
		keyringlib.MockInitWithError(errors.New("keychain locked"))
		t.Cleanup(keyringlib.MockInit)
		err := runLogin(newCmd(), nil, nil, false, true)
		if err == nil || !strings.Contains(err.Error(), "read OAuth client from keychain") {
			t.Fatalf("got %v; want a keychain read error", err)
		}
	})

	t.Run("scope resolution error", func(t *testing.T) {
		keyringlib.MockInit()
		t.Cleanup(keyringlib.MockInit)
		if err := auth.StoreByoClient(auth.NewOSKeyring(), auth.DefaultAPIKeyProfile, auth.ByoClient{ClientID: "cid"}); err != nil {
			t.Fatalf("StoreByoClient: %v", err)
		}
		err := runLogin(newCmd(), nil, []string{"no-such-service"}, false, true)
		if err == nil || !strings.Contains(err.Error(), "no OAuth scopes for service(s)") {
			t.Fatalf("got %v; want the unknown-service error", err)
		}
	})

	t.Run("login error", func(t *testing.T) {
		keyringlib.MockInit()
		t.Cleanup(keyringlib.MockInit)
		if err := auth.StoreByoClient(auth.NewOSKeyring(), auth.DefaultAPIKeyProfile, auth.ByoClient{ClientID: "cid"}); err != nil {
			t.Fatalf("StoreByoClient: %v", err)
		}
		boom := errors.New("consent denied")
		orig := interactiveByoLogin
		t.Cleanup(func() { interactiveByoLogin = orig })
		interactiveByoLogin = func(context.Context, auth.ByoOAuthConfig, func(string) error) (*auth.Credentials, error) {
			return nil, boom
		}
		if err := runLogin(newCmd(), []string{"gmail.readonly"}, nil, false, true); !errors.Is(err, boom) {
			t.Fatalf("got %v; want the login error", err)
		}
	})
}

// TestInteractiveByoLoginDefaultRefusesAnEmptyClient runs the production
// closure. An empty client id fails before any listener binds or browser opens.
func TestInteractiveByoLoginDefaultRefusesAnEmptyClient(t *testing.T) {
	_, err := interactiveByoLogin(context.Background(), auth.ByoOAuthConfig{}, func(string) error { return nil })
	var ae *auth.AuthError
	if !errors.As(err, &ae) || ae.Code != "BYO_OAUTH_CLIENT_NOT_CONFIGURED" {
		t.Fatalf("got %v; want BYO_OAUTH_CLIENT_NOT_CONFIGURED", err)
	}
}
