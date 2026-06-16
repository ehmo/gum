package auth

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	keyring "github.com/zalando/go-keyring"
)

// TestAuthKeychainStorageOnly verifies spec §7: refresh tokens are stored
// only in the OS keychain via go-keyring, never written as plaintext to the
// user's config directory. We round-trip Set/Get/Delete through the OS
// keyring (mocked in-process via go-keyring's MockInit) and assert no
// secret material lands in ~/.gum or $XDG_CONFIG_HOME/gum.
func TestAuthKeychainStorageOnly(t *testing.T) {
	keyring.MockInit()
	defer keyring.MockInit() // reset on exit

	// Sandbox HOME and XDG_CONFIG_HOME so the test owns the filesystem
	// surface that would otherwise mask a real plaintext leak.
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".xdgconfig"))

	kb := NewOSKeyring()
	const secret = "1//09xxxxxxxxx-fake-refresh-token"
	const key = "gum.byo_oauth.testfingerprint"

	if err := kb.Set(key, secret); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := kb.Get(key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != secret {
		t.Errorf("Get = %q; want round-trip %q", got, secret)
	}

	// Scan the sandboxed HOME tree to assert no file contains the secret.
	// This is the structural invariant for spec §7's "never plaintext"
	// rule: if a future regression starts shadowing the keyring write
	// with a file write, this catches it.
	var hits []string
	_ = filepath.WalkDir(tmp, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		body, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		if strings.Contains(string(body), secret) {
			hits = append(hits, path)
		}
		return nil
	})
	if len(hits) > 0 {
		t.Errorf("refresh token leaked to plaintext file(s): %v", hits)
	}

	// Round-trip Delete: removing then re-reading must return ("", nil)
	// to match the in-memory backend contract.
	if err := kb.Delete(key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if v, err := kb.Get(key); err != nil || v != "" {
		t.Errorf("Get after Delete = (%q, %v); want (\"\", nil)", v, err)
	}
}

// TestAuthKeychainUnavailable verifies that when the OS keychain backend is
// missing (e.g. Linux without libsecret, macOS without keychain access, or a
// sandboxed CI runner), keyring operations surface a typed
// AUTH_KEYCHAIN_UNAVAILABLE *AuthError rather than a bare go-keyring error
// string. The human remediation must point at the install/setup step.
func TestAuthKeychainUnavailable(t *testing.T) {
	// Inject ErrUnsupportedPlatform — the canonical "no backend" signal.
	keyring.MockInitWithError(keyring.ErrUnsupportedPlatform)
	defer keyring.MockInit()

	kb := NewOSKeyring()

	t.Run("Get surfaces AUTH_KEYCHAIN_UNAVAILABLE", func(t *testing.T) {
		_, err := kb.Get("gum.byo_oauth.x")
		var ae *AuthError
		if !errors.As(err, &ae) {
			t.Fatalf("err is not *AuthError: %T (%v)", err, err)
		}
		if ae.Code != "AUTH_KEYCHAIN_UNAVAILABLE" {
			t.Errorf("Code = %q; want AUTH_KEYCHAIN_UNAVAILABLE", ae.Code)
		}
		if !strings.Contains(ae.HumanRemediation, "libsecret") && !strings.Contains(ae.HumanRemediation, "macOS") {
			t.Errorf("HumanRemediation = %q; want install hint", ae.HumanRemediation)
		}
	})

	t.Run("Set surfaces AUTH_KEYCHAIN_UNAVAILABLE", func(t *testing.T) {
		err := kb.Set("gum.byo_oauth.x", "rt")
		var ae *AuthError
		if !errors.As(err, &ae) {
			t.Fatalf("err is not *AuthError: %T (%v)", err, err)
		}
		if ae.Code != "AUTH_KEYCHAIN_UNAVAILABLE" {
			t.Errorf("Code = %q; want AUTH_KEYCHAIN_UNAVAILABLE", ae.Code)
		}
	})

	t.Run("Delete surfaces AUTH_KEYCHAIN_UNAVAILABLE", func(t *testing.T) {
		err := kb.Delete("gum.byo_oauth.x")
		var ae *AuthError
		if !errors.As(err, &ae) {
			t.Fatalf("err is not *AuthError: %T (%v)", err, err)
		}
		if ae.Code != "AUTH_KEYCHAIN_UNAVAILABLE" {
			t.Errorf("Code = %q; want AUTH_KEYCHAIN_UNAVAILABLE", ae.Code)
		}
	})
}
