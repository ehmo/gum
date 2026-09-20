package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/auth"
	"github.com/spf13/cobra"
	keyringlib "github.com/zalando/go-keyring"
)

// isolateXDG points every per-profile directory at fresh temp dirs so a doctor
// probe never reads or writes the developer's real gum state.
func isolateXDG(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
}

// TestDoctorAuditFailureArms covers each way the audit probe reports a broken
// sink: an unparseable profile, an unresolvable data dir, a data dir that
// cannot be created, an audit.broken that cannot be stat'd, and a directory
// that refuses the write probe.
func TestDoctorAuditFailureArms(t *testing.T) {
	t.Run("invalid profile", func(t *testing.T) {
		got := doctorAudit("bad/name")
		if got.OK || got.Summary != "invalid profile" {
			t.Fatalf("got %+v, want an invalid-profile failure", got)
		}
	})

	t.Run("cannot resolve audit dir", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", "")
		t.Setenv("HOME", "")
		got := doctorAudit("alpha")
		if got.OK || got.Summary != "cannot resolve audit dir" {
			t.Fatalf("got %+v, want an unresolvable data dir", got)
		}
	})

	t.Run("cannot create audit dir", func(t *testing.T) {
		blocker := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
			t.Fatalf("setup: %v", err)
		}
		t.Setenv("XDG_DATA_HOME", blocker)
		got := doctorAudit("alpha")
		if got.OK || got.Summary != "cannot create audit dir" {
			t.Fatalf("got %+v, want a mkdir failure", got)
		}
	})

	t.Run("cannot inspect audit.broken", func(t *testing.T) {
		base := t.TempDir()
		t.Setenv("XDG_DATA_HOME", base)
		dir := filepath.Join(base, "gum", "alpha")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("setup: %v", err)
		}
		// No search permission: MkdirAll still succeeds (the dir exists) but
		// stat'ing a name inside it fails with something other than ENOENT.
		t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
		if err := os.Chmod(dir, 0o600); err != nil {
			t.Fatalf("chmod: %v", err)
		}
		got := doctorAudit("alpha")
		if got.OK || got.Summary != "cannot inspect audit.broken" {
			t.Fatalf("got %+v, want a stat failure", got)
		}
	})

	t.Run("audit dir not writable", func(t *testing.T) {
		base := t.TempDir()
		t.Setenv("XDG_DATA_HOME", base)
		dir := filepath.Join(base, "gum", "alpha")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("setup: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
		if err := os.Chmod(dir, 0o500); err != nil {
			t.Fatalf("chmod: %v", err)
		}
		got := doctorAudit("alpha")
		if got.OK || got.Summary != "audit dir not writable" {
			t.Fatalf("got %+v, want a write-probe failure", got)
		}
	})
}

// TestReadAuditBrokenHintArms covers the three degraded reads of the sentinel:
// it cannot be opened, it cannot be read, and it is empty.
func TestReadAuditBrokenHintArms(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "audit.broken")
		if got := readAuditBrokenHint(path); !strings.Contains(got, "no such file") {
			t.Fatalf("got %q, want the open error", got)
		}
	})

	t.Run("not a regular file", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "audit.broken")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatalf("setup: %v", err)
		}
		if got := readAuditBrokenHint(dir); got == "" || got == dir {
			t.Fatalf("got %q, want the read error", got)
		}
	})

	t.Run("empty sentinel falls back to the path", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "audit.broken")
		if err := os.WriteFile(path, []byte("  \n"), 0o600); err != nil {
			t.Fatalf("setup: %v", err)
		}
		if got := readAuditBrokenHint(path); got != path {
			t.Fatalf("got %q, want the path %q", got, path)
		}
	})
}

// TestDoctorAuthEnvFallbacks covers the two environment credential sources the
// auth probe accepts once the keychain has nothing to report.
func TestDoctorAuthEnvFallbacks(t *testing.T) {
	cases := []struct {
		name string
		env  string
	}{
		{"api key env", auth.EnvAPIKeyVar},
		{"service account env", auth.EnvServiceAccountKeyVar},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			keyringlib.MockInit()
			t.Cleanup(keyringlib.MockInit)
			t.Setenv(auth.EnvAPIKeyVar, "")
			t.Setenv(auth.EnvServiceAccountKeyVar, "")
			t.Setenv(tc.env, "value")

			cmd := &cobra.Command{Use: "doctor"}
			cmd.SetContext(context.Background())
			got := doctorAuth(cmd)
			if !got.OK || !strings.Contains(got.Summary, tc.env) {
				t.Fatalf("got %+v, want an OK naming %s", got, tc.env)
			}
		})
	}
}

// TestDoctorAuthFromKeyringArms covers the keychain probe: a nil backend defers
// to the env checks, a registered client with a grant is ready, a registered
// client with no grant is not, and a bare API key is accepted.
func TestDoctorAuthFromKeyringArms(t *testing.T) {
	const profile = "default"

	t.Run("nil backend defers", func(t *testing.T) {
		got, done := doctorAuthFromKeyring(context.Background(), nil, profile)
		if done || got.Name != "auth" {
			t.Fatalf("got (%+v,%v), want a deferred auth check", got, done)
		}
	})

	t.Run("byo client with a grant", func(t *testing.T) {
		keyringlib.MockInit()
		t.Cleanup(keyringlib.MockInit)
		kb := auth.NewOSKeyring()
		if err := auth.StoreByoClient(kb, profile, auth.ByoClient{ClientID: "client-xyz"}); err != nil {
			t.Fatalf("StoreByoClient: %v", err)
		}
		b := auth.NewByoOAuth(auth.ByoOAuthConfig{
			ClientID: "client-xyz",
			Profile:  profile,
			Scopes:   []string{"https://www.googleapis.com/auth/gmail.readonly"},
		}, kb)
		if err := b.StoreRefreshToken("rt-123"); err != nil {
			t.Fatalf("StoreRefreshToken: %v", err)
		}

		got, done := doctorAuthFromKeyring(context.Background(), kb, profile)
		if !done || !got.OK || !strings.Contains(got.Summary, "BYO OAuth ready") {
			t.Fatalf("got (%+v,%v), want a ready BYO grant", got, done)
		}
	})

	t.Run("byo client without a grant", func(t *testing.T) {
		keyringlib.MockInit()
		t.Cleanup(keyringlib.MockInit)
		kb := auth.NewOSKeyring()
		if err := auth.StoreByoClient(kb, profile, auth.ByoClient{ClientID: "client-xyz"}); err != nil {
			t.Fatalf("StoreByoClient: %v", err)
		}

		got, done := doctorAuthFromKeyring(context.Background(), kb, profile)
		if !done || got.OK || !strings.Contains(got.Summary, "no grant is stored") {
			t.Fatalf("got (%+v,%v), want a missing-grant failure", got, done)
		}
		if got.Hint == "" {
			t.Fatal("want a hint naming gum login")
		}
	})

	t.Run("api key in the keychain", func(t *testing.T) {
		keyringlib.MockInit()
		t.Cleanup(keyringlib.MockInit)
		kb := auth.NewOSKeyring()
		if err := auth.StoreAPIKey(kb, profile, "AIza-test"); err != nil {
			t.Fatalf("StoreAPIKey: %v", err)
		}

		got, done := doctorAuthFromKeyring(context.Background(), kb, profile)
		if !done || !got.OK || !strings.Contains(got.Summary, "API key present") {
			t.Fatalf("got (%+v,%v), want the stored API key", got, done)
		}
	})
}

// TestDoctorCacheFailureArms covers the cache probe's profile and dir
// resolution failures.
func TestDoctorCacheFailureArms(t *testing.T) {
	t.Run("invalid profile", func(t *testing.T) {
		got := doctorCache("bad/name")
		if got.OK || got.Summary != "invalid profile" {
			t.Fatalf("got %+v, want an invalid-profile failure", got)
		}
	})

	t.Run("cannot resolve cache dir", func(t *testing.T) {
		t.Setenv("XDG_CACHE_HOME", "")
		t.Setenv("HOME", "")
		got := doctorCache("alpha")
		if got.OK || got.Summary != "cannot resolve cache dir" {
			t.Fatalf("got %+v, want an unresolvable cache dir", got)
		}
	})
}

// TestDoctorConfigWarnsOnUnknownKeys pins the hint the config probe adds when
// the profile's config.toml carries keys this build does not recognize.
func TestDoctorConfigWarnsOnUnknownKeys(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	dir := filepath.Join(base, "gum", "alpha")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("setup: %v", err)
	}
	cfg := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfg, []byte("not.a.real.key = \"x\"\n"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	got := doctorConfig("alpha")
	if !got.OK {
		t.Fatalf("got %+v, want an OK config check", got)
	}
	if !strings.Contains(got.Hint, "config warning(s)") {
		t.Fatalf("hint = %q, want the unknown-key warning count", got.Hint)
	}
}

// TestDoctorCmdInvalidProfile covers the profile-resolution arm of the command
// itself: a malformed --profile fails before any subsystem is probed.
func TestDoctorCmdInvalidProfile(t *testing.T) {
	cmd := newDoctorCmd()
	cmd.PersistentFlags().String("profile", "", "")
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--profile", "bad/name"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("want a profile-resolution error")
	}
}

// TestDoctorCmdJSONWriteFailure covers the writeJSON arm: a stdout that cannot
// be written surfaces the write error, not the health verdict.
func TestDoctorCmdJSONWriteFailure(t *testing.T) {
	isolateXDG(t)
	cmd := newDoctorCmd()
	cmd.SetOut(failWriter{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--format", "json"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "pipe closed") {
		t.Fatalf("got %v, want the writer failure", err)
	}
}

// TestDoctorCmdHealthyExitsZero covers the healthy text path: with an isolated
// profile and a credential in the environment every check passes and the
// command returns nil.
func TestDoctorCmdHealthyExitsZero(t *testing.T) {
	isolateXDG(t)
	keyringlib.MockInit()
	t.Cleanup(keyringlib.MockInit)
	t.Setenv(auth.EnvAPIKeyVar, "AIza-test")

	var out bytes.Buffer
	cmd := newDoctorCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "[ok] auth") {
		t.Fatalf("output = %q, want a passing auth check", out.String())
	}
}
