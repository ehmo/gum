package auth

import (
	"errors"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

// TestScopeExpansionPromptShapes pins the three relevant outcomes:
// incremental returns empty (no prompt forces an upgrade flow);
// full_reconsent + any unknown value default to "consent" so a typo'd
// manifest still drives the safer full re-consent screen.
func TestScopeExpansionPromptShapes(t *testing.T) {
	cases := []struct {
		mode string
		want string
	}{
		{"incremental", ""},
		{"full_reconsent", "consent"},
		{"", "consent"},
		{"unrecognized_mode", "consent"},
	}
	for _, tc := range cases {
		t.Run(tc.mode, func(t *testing.T) {
			m := &managedScopesManifest{ClientPolicy: managedClientPolicy{ScopeExpansionMode: tc.mode}}
			if got := scopeExpansionPrompt(m); got != tc.want {
				t.Errorf("mode=%q: got %q; want %q", tc.mode, got, tc.want)
			}
		})
	}
}

// TestManagedSubjectFingerprint covers the identity-derived gum_oauth
// subject fingerprint. The scope-set hash lives in vaultKey; the subject
// portion must depend only on the OpenID Connect sub claim.
func TestManagedSubjectFingerprint(t *testing.T) {
	a := managedSubjectFingerprintFromSub("account-A")
	b := managedSubjectFingerprintFromSub("account-A")
	if a != b {
		t.Errorf("same sub produced different fingerprints: %q vs %q", a, b)
	}
	if a == "" || a == "default" {
		t.Errorf("subject fingerprint = %q; want non-empty non-default", a)
	}

	diff := managedSubjectFingerprintFromSub("account-B")
	if diff == a {
		t.Errorf("different subjects produced same fingerprint: %q", a)
	}
}

func TestScopesWithOpenID(t *testing.T) {
	got := scopesWithOpenID([]string{"scope.b", "scope.a"})
	want := []string{"openid", "scope.a", "scope.b"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("scopesWithOpenID = %v; want %v", got, want)
	}
	got = scopesWithOpenID([]string{"openid", "scope.a"})
	if strings.Join(got, " ") != "openid scope.a" {
		t.Errorf("scopesWithOpenID duplicated openid: %v", got)
	}
}

// TestKeychainHumanHintShapes pins the OS-install hint for the
// ErrUnsupportedPlatform sentinel and the fallthrough format that
// embeds the underlying error message verbatim — operators rely on
// the embedded message to decide whether to retry vs. install a
// keychain backend.
func TestKeychainHumanHintShapes(t *testing.T) {
	t.Run("unsupported_platform", func(t *testing.T) {
		hint := keychainHumanHint(keyring.ErrUnsupportedPlatform)
		if !strings.Contains(hint, "libsecret") {
			t.Errorf("hint=%q; want libsecret install advice", hint)
		}
	})
	t.Run("wrapped_unsupported_platform", func(t *testing.T) {
		// errors.Is must still match through a wrap.
		wrapped := &wrappedErr{inner: keyring.ErrUnsupportedPlatform}
		hint := keychainHumanHint(wrapped)
		if !strings.Contains(hint, "libsecret") {
			t.Errorf("wrapped hint=%q; want libsecret advice", hint)
		}
	})
	t.Run("generic_error_passes_through", func(t *testing.T) {
		hint := keychainHumanHint(errors.New("EACCES on /tmp/keyring"))
		if !strings.Contains(hint, "EACCES on /tmp/keyring") {
			t.Errorf("hint=%q; want underlying message", hint)
		}
		if strings.Contains(hint, "libsecret") {
			t.Errorf("generic error should not surface libsecret hint: %q", hint)
		}
	})
}

type wrappedErr struct{ inner error }

func (w *wrappedErr) Error() string { return "wrap: " + w.inner.Error() }
func (w *wrappedErr) Unwrap() error { return w.inner }
