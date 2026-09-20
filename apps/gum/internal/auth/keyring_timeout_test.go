package auth

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// blockKeyring replaces the three go-keyring entry points with stubs that
// never return, the way a locked Secret Service collection behaves when
// its D-Bus unlock prompt has nobody to answer it. It returns a counter
// of how many times a stub was entered.
func blockKeyring(t *testing.T) *atomic.Int64 {
	t.Helper()
	release := make(chan struct{})
	calls := &atomic.Int64{}
	prevGet, prevSet, prevDelete := keyringGet, keyringSet, keyringDelete
	keyringGet = func(string, string) (string, error) {
		calls.Add(1)
		<-release
		return "", nil
	}
	keyringSet = func(string, string, string) error {
		calls.Add(1)
		<-release
		return nil
	}
	keyringDelete = func(string, string) error {
		calls.Add(1)
		<-release
		return nil
	}
	t.Cleanup(func() {
		close(release)
		keyringGet, keyringSet, keyringDelete = prevGet, prevSet, prevDelete
		resetKeyringWedged()
	})
	return calls
}

// TestOSKeyringGetTimesOutOnABlockedBackend is the regression test for the
// linux startup hang: `gum mcp --stdio` on a box whose keyring is locked
// blocked forever inside SecretService.Unlock, printing nothing and never
// answering the MCP handshake. Every keychain call has to be bounded.
func TestOSKeyringGetTimesOutOnABlockedBackend(t *testing.T) {
	blockKeyring(t)
	t.Setenv("GUM_KEYRING_TIMEOUT", "50ms")

	done := make(chan error, 1)
	go func() {
		kr := OSKeyring{}
		_, err := kr.Get("gum.byo_oauth_client.default")
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Get err = nil; want the timeout error")
		}
		if !errors.Is(err, ErrKeychainTimeout) {
			t.Errorf("Get err = %v; want it to wrap ErrKeychainTimeout", err)
		}
		var authErr *AuthError
		if !errors.As(err, &authErr) {
			t.Fatalf("Get err = %T; want an *AuthError", err)
		}
		if authErr.Code != "AUTH_KEYCHAIN_UNAVAILABLE" {
			t.Errorf("code = %q; want AUTH_KEYCHAIN_UNAVAILABLE", authErr.Code)
		}
		if !strings.Contains(authErr.HumanRemediation, "GUM_KEYRING_TIMEOUT") {
			t.Errorf("remediation = %q; want it to name GUM_KEYRING_TIMEOUT", authErr.HumanRemediation)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Get did not return; the keychain call is still unbounded")
	}
}

// TestOSKeyringSetAndDeleteTimeOut pins the other two entry points. A
// write path that hangs strands `gum auth use-oauth-client` just as badly
// as a hung read strands startup.
func TestOSKeyringSetAndDeleteTimeOut(t *testing.T) {
	kr := OSKeyring{}
	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"Set", func() error { return kr.Set("k", "v") }},
		{"Delete", func() error { return kr.Delete("k") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			blockKeyring(t)
			t.Setenv("GUM_KEYRING_TIMEOUT", "50ms")

			done := make(chan error, 1)
			go func() { done <- tc.call() }()

			select {
			case err := <-done:
				if !errors.Is(err, ErrKeychainTimeout) {
					t.Errorf("%s err = %v; want it to wrap ErrKeychainTimeout", tc.name, err)
				}
			case <-time.After(10 * time.Second):
				t.Fatalf("%s did not return; the keychain call is still unbounded", tc.name)
			}
		})
	}
}

// TestOSKeyringStopsCallingAWedgedBackend pins the one-shot guard. The
// abandoned goroutine stays parked on the D-Bus prompt, so a long-running
// MCP server that retried per request would leak one goroutine per call.
func TestOSKeyringStopsCallingAWedgedBackend(t *testing.T) {
	calls := blockKeyring(t)
	t.Setenv("GUM_KEYRING_TIMEOUT", "50ms")
	kr := OSKeyring{}

	if _, err := kr.Get("k"); !errors.Is(err, ErrKeychainTimeout) {
		t.Fatalf("first Get err = %v; want the timeout", err)
	}

	start := time.Now()
	if _, err := kr.Get("k"); !errors.Is(err, ErrKeychainTimeout) {
		t.Fatalf("second Get err = %v; want the timeout", err)
	}
	if elapsed := time.Since(start); elapsed > 20*time.Millisecond {
		t.Errorf("second Get took %v; want an immediate short circuit", elapsed)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("backend entered %d times; want 1", got)
	}
}

// TestKeyringTimeoutReadsTheEnvOverride pins the override parse. A value
// that is not a positive duration falls back to the compiled default
// rather than disabling the bound.
func TestKeyringTimeoutReadsTheEnvOverride(t *testing.T) {
	for _, tc := range []struct {
		env  string
		want time.Duration
	}{
		{"", defaultKeyringTimeout},
		{"not-a-duration", defaultKeyringTimeout},
		{"0s", defaultKeyringTimeout},
		{"-5s", defaultKeyringTimeout},
		{"90s", 90 * time.Second},
	} {
		t.Setenv("GUM_KEYRING_TIMEOUT", tc.env)
		if got := keyringTimeout(); got != tc.want {
			t.Errorf("keyringTimeout() with %q = %v; want %v", tc.env, got, tc.want)
		}
	}
}

// TestBestEffortBoundCapsTheInteractiveTimeout pins the two tiers. Raising
// GUM_KEYRING_TIMEOUT buys a slow interactive prompt more room without
// lengthening the read gum makes on its own behalf; lowering it shortens both.
func TestBestEffortBoundCapsTheInteractiveTimeout(t *testing.T) {
	for _, tc := range []struct {
		env             string
		wantEffort      time.Duration
		wantInteractive time.Duration
	}{
		{"", bestEffortKeyringTimeout, defaultKeyringTimeout},
		{"60s", bestEffortKeyringTimeout, 60 * time.Second},
		{"50ms", 50 * time.Millisecond, 50 * time.Millisecond},
	} {
		t.Setenv("GUM_KEYRING_TIMEOUT", tc.env)
		if got := keyringBestEffort.bound(); got != tc.wantEffort {
			t.Errorf("best-effort bound with %q = %v; want %v", tc.env, got, tc.wantEffort)
		}
		if got := keyringInteractive.bound(); got != tc.wantInteractive {
			t.Errorf("interactive bound with %q = %v; want %v", tc.env, got, tc.wantInteractive)
		}
	}
}

// TestBestEffortKeyringDoesNotLatchTheProcess pins the other half of the
// tier split. A slow but working keychain must not turn one abandoned
// startup read into an instant failure for the credential call that follows.
func TestBestEffortKeyringDoesNotLatchTheProcess(t *testing.T) {
	calls := blockKeyring(t)
	t.Setenv("GUM_KEYRING_TIMEOUT", "50ms")

	if _, err := NewBestEffortOSKeyring().Get("k"); !errors.Is(err, ErrKeychainTimeout) {
		t.Fatalf("best-effort Get err = %v; want the timeout", err)
	}

	interactive := OSKeyring{}
	if _, err := interactive.Get("k"); !errors.Is(err, ErrKeychainTimeout) {
		t.Fatalf("interactive Get err = %v; want the timeout", err)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("backend entered %d times; want 2 (the best-effort call must not latch)", got)
	}
}
