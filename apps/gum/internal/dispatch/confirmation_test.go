package dispatch_test

import (
	"errors"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/dispatch"
)

// newStoreOrSkip constructs a TokenStore or fatals the test if not yet implemented.
func newStoreOrSkip(t *testing.T, maxOutstanding int, ttl time.Duration) *dispatch.TokenStore {
	t.Helper()
	var store *dispatch.TokenStore
	var storeErr error
	msg, panicked := catchPanic(func() {
		store, storeErr = dispatch.NewTokenStore(maxOutstanding, ttl, t.TempDir())
	})
	if panicked {
		t.Fatalf("NewTokenStore panicked: %s — green team must implement NewTokenStore", msg)
	}
	if storeErr != nil {
		t.Fatalf("NewTokenStore returned error: %v", storeErr)
	}
	return store
}

// issueOrFatal issues a token or fatals with the appropriate message.
func issueOrFatal(t *testing.T, store *dispatch.TokenStore, purpose string) string {
	t.Helper()
	var tok string
	var err error
	msg, panicked := catchPanic(func() {
		tok, err = store.IssueToken(purpose)
	})
	if panicked {
		t.Fatalf("IssueToken(%q) panicked: %s — green team must implement IssueToken", purpose, msg)
	}
	if err != nil {
		t.Fatalf("IssueToken(%q): %v", purpose, err)
	}
	return tok
}

// issueExpectError issues a token expecting a specific error.
func issueExpectError(t *testing.T, store *dispatch.TokenStore, purpose string, wantErr error) {
	t.Helper()
	var err error
	msg, panicked := catchPanic(func() {
		_, err = store.IssueToken(purpose)
	})
	if panicked {
		t.Fatalf("IssueToken(%q) panicked: %s — green team must implement IssueToken", purpose, msg)
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("IssueToken(%q): got %v; want %v", purpose, err, wantErr)
	}
}

// consumeExpectError consumes a token expecting a specific error.
func consumeExpectError(t *testing.T, store *dispatch.TokenStore, tok, purpose string, wantErr error) {
	t.Helper()
	var err error
	msg, panicked := catchPanic(func() {
		err = store.ConsumeToken(tok, purpose)
	})
	if panicked {
		t.Fatalf("ConsumeToken(%q, %q) panicked: %s — green team must implement ConsumeToken", tok, purpose, msg)
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("ConsumeToken(%q, %q): got %v; want %v", tok, purpose, err, wantErr)
	}
}

// TestConfirmationTokenLifecycle (G4.6) covers all specified confirmation-token scenarios.
func TestConfirmationTokenLifecycle(t *testing.T) {
	t.Run("happy-path", func(t *testing.T) {
		defer goleak.VerifyNone(t)

		store := newStoreOrSkip(t, 10, 5*time.Minute)
		tok := issueOrFatal(t, store, "delete")

		if tok == "" {
			t.Fatal("IssueToken returned empty token")
		}

		// First consume must succeed.
		var err error
		msg, panicked := catchPanic(func() {
			err = store.ConsumeToken(tok, "delete")
		})
		if panicked {
			t.Fatalf("ConsumeToken panicked: %s", msg)
		}
		if err != nil {
			t.Fatalf("ConsumeToken first: %v", err)
		}

		// Second consume must be rejected with ErrTokenAlreadyUsed.
		consumeExpectError(t, store, tok, "delete", dispatch.ErrTokenAlreadyUsed)
	})

	t.Run("expired", func(t *testing.T) {
		defer goleak.VerifyNone(t)

		store := newStoreOrSkip(t, 10, 1*time.Millisecond)
		tok := issueOrFatal(t, store, "delete")

		// Wait long enough for the TTL to elapse.
		time.Sleep(10 * time.Millisecond)

		consumeExpectError(t, store, tok, "delete", dispatch.ErrTokenExpired)
	})

	t.Run("off-purpose", func(t *testing.T) {
		defer goleak.VerifyNone(t)

		store := newStoreOrSkip(t, 10, 5*time.Minute)
		tok := issueOrFatal(t, store, "delete")

		consumeExpectError(t, store, tok, "move-to-trash", dispatch.ErrPurposeMismatch)
	})

	t.Run("unknown-purpose-issue", func(t *testing.T) {
		defer goleak.VerifyNone(t)

		store := newStoreOrSkip(t, 10, 5*time.Minute)
		issueExpectError(t, store, "yeet", dispatch.ErrUnknownPurpose)
	})

	t.Run("malformed-token", func(t *testing.T) {
		defer goleak.VerifyNone(t)

		store := newStoreOrSkip(t, 10, 5*time.Minute)
		consumeExpectError(t, store, "not-a-real-token", "delete", dispatch.ErrMalformedToken)
	})

	t.Run("store-full", func(t *testing.T) {
		defer goleak.VerifyNone(t)

		store := newStoreOrSkip(t, 2, 5*time.Minute)
		issueOrFatal(t, store, "delete")
		issueOrFatal(t, store, "replace")
		issueExpectError(t, store, "cancel", dispatch.ErrTokenStoreFull)
	})

	t.Run("all-allowed-purposes-issue", func(t *testing.T) {
		defer goleak.VerifyNone(t)

		store := newStoreOrSkip(t, 100, 5*time.Minute)

		for _, purpose := range dispatch.AllowedPurposes {
			tok := issueOrFatal(t, store, purpose)
			var err error
			msg, panicked := catchPanic(func() {
				err = store.ConsumeToken(tok, purpose)
			})
			if panicked {
				t.Errorf("ConsumeToken(%q) panicked: %s", purpose, msg)
				continue
			}
			if err != nil {
				t.Errorf("ConsumeToken(%q): %v", purpose, err)
			}
		}
	})

	// HMAC tampering test — moved to confirmation_hmac_test.go (Phase 5.3).
	t.Run("hmac-tamper", func(t *testing.T) {
		t.Skip("moved to confirmation_hmac_test.go")
	})
}

// TestConfirmationAllowedPurposesList asserts that AllowedPurposes contains
// exactly the 8 Phase-4 purposes.
func TestConfirmationAllowedPurposesList(t *testing.T) {
	defer goleak.VerifyNone(t)

	want := map[string]bool{
		"delete":         true,
		"move-to-trash":  true,
		"replace":        true,
		"bulk-update":    true,
		"revoke-grant":   true,
		"cancel":         true,
		"external-share": true,
		"permanent":      true,
	}

	if len(dispatch.AllowedPurposes) != len(want) {
		t.Errorf("AllowedPurposes has %d entries; want %d", len(dispatch.AllowedPurposes), len(want))
	}
	for _, p := range dispatch.AllowedPurposes {
		if !want[p] {
			t.Errorf("unexpected purpose in AllowedPurposes: %q", p)
		}
	}
}
