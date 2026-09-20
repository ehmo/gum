package auth

import (
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	keyring "github.com/zalando/go-keyring"
)

// ErrKeychainUnsupported marks the one keychain failure that is not a fault:
// the platform ships no keychain backend at all. Callers that offer an env-var
// fallback gate on this sentinel, so a locked keychain or a denied write stays
// a real error and exits non-zero instead of printing "unavailable on this
// platform" and reporting success.
var ErrKeychainUnsupported = errors.New("AUTH_KEYCHAIN_UNAVAILABLE: no OS keychain backend on this platform")

// ErrKeychainTimeout marks a keychain call that never came back. On a Linux
// box whose Secret Service collection is locked -- headless, SSH, container,
// CI -- go-keyring parks on a D-Bus unlock prompt that nobody can answer, and
// the call blocks forever. go-keyring takes no context, so the bound has to
// live here.
var ErrKeychainTimeout = errors.New("AUTH_KEYCHAIN_UNAVAILABLE: keychain call timed out")

// defaultKeyringTimeout bounds one user-initiated keychain call. It is long
// enough for a macOS Keychain prompt the user actually answers.
const defaultKeyringTimeout = 20 * time.Second

// bestEffortKeyringTimeout caps a read gum makes on its own behalf, such as
// the granted-scope lookup on the dispatch path. Nobody asked for it, so it
// must not hold up the command for the interactive bound.
const bestEffortKeyringTimeout = 2 * time.Second

// keyringTimeoutEnv overrides defaultKeyringTimeout with a Go duration.
const keyringTimeoutEnv = "GUM_KEYRING_TIMEOUT"

// keyringMode selects the bound applied to one backend call.
type keyringMode int

const (
	// keyringInteractive is a call the user asked for, directly or through a
	// just-in-time credential prompt. It gets the full bound, and a timeout
	// latches the process.
	keyringInteractive keyringMode = iota
	// keyringBestEffort is a read gum makes on its own behalf. It gets the
	// short bound and never latches: a slow-but-working keychain must not
	// make the credential call that follows fail instantly.
	keyringBestEffort
)

// bound returns the per-call deadline for the mode. Raising
// GUM_KEYRING_TIMEOUT lengthens only the interactive bound; lowering it
// shortens both.
func (m keyringMode) bound() time.Duration {
	configured := keyringTimeout()
	if m == keyringBestEffort && configured > bestEffortKeyringTimeout {
		return bestEffortKeyringTimeout
	}
	return configured
}

// The three go-keyring entry points, indirected so tests can drive the
// timeout path without a real D-Bus session.
var (
	keyringGet    = keyring.Get
	keyringSet    = keyring.Set
	keyringDelete = keyring.Delete
)

// keyringWedged latches once an interactive call times out. The abandoned
// goroutine stays parked on the prompt for the life of the process, so a
// long-running MCP server that retried per request would leak one goroutine
// per call. After the first timeout every later call fails immediately.
var keyringWedged atomic.Bool

// resetKeyringWedged clears the latch. Tests use it; production never does.
func resetKeyringWedged() { keyringWedged.Store(false) }

// keyringTimeout returns the per-call bound. A value that is not a positive
// duration falls back to the compiled default rather than disabling the bound.
func keyringTimeout() time.Duration {
	raw := os.Getenv(keyringTimeoutEnv)
	if raw == "" {
		return defaultKeyringTimeout
	}

	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return defaultKeyringTimeout
	}
	return d
}

// callKeyring runs one go-keyring call under the timeout. Set and Delete pass
// a wrapper that discards the unused string so all three entry points share
// one bound. Callers read the go-keyring function var before they build the
// closure: on a timeout the goroutine is abandoned, and a var read inside it
// would race a test that restores the var afterwards.
func callKeyring(mode keyringMode, call func() (string, error)) (string, error) {
	if keyringWedged.Load() {
		return "", keychainTimedOut(mode)
	}

	type outcome struct {
		value string
		err   error
	}
	done := make(chan outcome, 1)
	go func() {
		v, err := call()
		done <- outcome{value: v, err: err}
	}()

	timer := time.NewTimer(mode.bound())
	defer timer.Stop()

	select {
	case got := <-done:
		return got.value, got.err
	case <-timer.C:
		if mode == keyringInteractive {
			keyringWedged.Store(true)
		}
		return "", keychainTimedOut(mode)
	}
}

// keychainTimedOut builds the spec §7 envelope for a timed-out call. The
// remediation names the override so the user can raise it when a slow
// hardware-backed keychain is the real cause.
func keychainTimedOut(mode keyringMode) *AuthError {
	return &AuthError{
		Code:     "AUTH_KEYCHAIN_UNAVAILABLE",
		Strategy: "",
		HumanRemediation: fmt.Sprintf(
			"OS keychain did not respond within %s; unlock the login keyring, or raise the bound with %s (a Go duration, e.g. 60s)",
			mode.bound(), keyringTimeoutEnv),
		Cause: ErrKeychainTimeout,
	}
}

// keyringService is the constant service name used for all gum secrets in the
// OS keychain. Per spec §7, the service name is `gum`; per-credential
// uniqueness is encoded in the user key (e.g. `gum.byo_oauth.<scope-hash>`).
const keyringService = "gum"

// OSKeyring is the production KeyringBackend backed by github.com/zalando/go-keyring.
// On macOS this writes to Keychain Services; on Linux, libsecret
// (gnome-keyring / kwallet via D-Bus). Platforms without a
// supported backend surface AUTH_KEYCHAIN_UNAVAILABLE (spec §7) so callers can
// guide the user to install libsecret or pick a different machine instead of
// silently falling back to plaintext storage.
type OSKeyring struct {
	// mode selects the per-call bound. The zero value is keyringInteractive,
	// so a bare OSKeyring{} behaves like a user-initiated call.
	mode keyringMode
}

// NewOSKeyring returns the platform-native keychain backend for a call the
// user asked for.
func NewOSKeyring() *OSKeyring { return &OSKeyring{} }

// NewBestEffortOSKeyring returns the platform-native keychain backend for a
// read gum makes on its own behalf. A locked or missing keychain must degrade
// the caller, not stall it, so this tier uses the short bound and leaves the
// process unlatched.
func NewBestEffortOSKeyring() *OSKeyring { return &OSKeyring{mode: keyringBestEffort} }

// Get retrieves a secret stored under key. Returns ("", nil) when the key is
// absent (matches the in-memory test backend behavior); returns
// AUTH_KEYCHAIN_UNAVAILABLE when the OS backend is missing or returns an
// error other than "not found".
func (k OSKeyring) Get(key string) (string, error) {
	get := keyringGet
	v, err := callKeyring(k.mode, func() (string, error) { return get(keyringService, key) })
	if err == nil {
		return v, nil
	}
	if errors.Is(err, keyring.ErrNotFound) {
		return "", nil
	}
	return "", keychainUnavailable(err)
}

// Set stores value under key in the OS keychain. Returns
// AUTH_KEYCHAIN_UNAVAILABLE if the backend is missing.
func (k OSKeyring) Set(key, value string) error {
	set := keyringSet
	_, err := callKeyring(k.mode, func() (string, error) { return "", set(keyringService, key, value) })
	if err != nil {
		return keychainUnavailable(err)
	}
	return nil
}

// Delete removes the secret stored under key. Absent keys are not an error.
func (k OSKeyring) Delete(key string) error {
	del := keyringDelete
	_, err := callKeyring(k.mode, func() (string, error) { return "", del(keyringService, key) })
	if err == nil || errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return keychainUnavailable(err)
}

// keychainUnavailable wraps a go-keyring error in the spec §7
// AUTH_KEYCHAIN_UNAVAILABLE envelope. Strategy stays empty because the
// keychain backend is strategy-agnostic storage. Cause carries
// ErrKeychainUnsupported only for a missing platform backend, so callers can
// separate "nothing to fix here" from a genuine store failure.
func keychainUnavailable(err error) *AuthError {
	var timedOut *AuthError
	if errors.As(err, &timedOut) && errors.Is(err, ErrKeychainTimeout) {
		return timedOut
	}

	cause := err
	if errors.Is(err, keyring.ErrUnsupportedPlatform) {
		cause = fmt.Errorf("%w: %v", ErrKeychainUnsupported, err)
	}
	return &AuthError{
		Code:             "AUTH_KEYCHAIN_UNAVAILABLE",
		Strategy:         "",
		HumanRemediation: keychainHumanHint(err),
		Cause:            cause,
	}
}

// keychainHumanHint surfaces the OS-specific install advice when the keychain
// backend is missing, falling back to the underlying error message otherwise.
func keychainHumanHint(err error) string {
	if errors.Is(err, keyring.ErrUnsupportedPlatform) {
		return "OS keychain backend not available; on Linux install libsecret (`apt install libsecret-1-0` / `dnf install libsecret`) or use macOS"
	}
	return "keychain operation failed: " + err.Error()
}

// Compile-time check that OSKeyring satisfies the KeyringBackend interface
// consumed by ByoOAuth and the (future) gum_oauth flow.
var _ KeyringBackend = (*OSKeyring)(nil)
