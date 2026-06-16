// Package dispatch_test — Phase 5.3 HMAC confirmation token tests (gum-fii.6.5).
//
// All tests in this file are FAILING until the green team implements HMAC signing
// in confirmation.go. They exercise spec.md §6.1.2.
package dispatch_test

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/dispatch"
)

// hmacTokenRe matches the expected HMAC token format: base64url(payload).base64url(hmac).
var hmacTokenRe = regexp.MustCompile(`^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$`)

// newHMACStoreOrFatal constructs a TokenStore with a fresh temp keyDir or fatals.
func newHMACStoreOrFatal(t *testing.T, maxOutstanding int, ttl time.Duration) (*dispatch.TokenStore, string) {
	t.Helper()
	keyDir := t.TempDir()
	var store *dispatch.TokenStore
	var storeErr error
	msg, panicked := catchPanic(func() {
		store, storeErr = dispatch.NewTokenStore(maxOutstanding, ttl, keyDir)
	})
	if panicked {
		t.Fatalf("NewTokenStore panicked: %s", msg)
	}
	if storeErr != nil {
		t.Fatalf("NewTokenStore error: %v", storeErr)
	}
	return store, keyDir
}

// TestHMACTokenFormat verifies that IssueToken returns a token matching
// the HMAC format: base64url(payload).base64url(hmac).
func TestHMACTokenFormat(t *testing.T) {
	defer goleak.VerifyNone(t)

	store, _ := newHMACStoreOrFatal(t, 10, 5*time.Minute)

	var tok string
	var err error
	msg, panicked := catchPanic(func() {
		tok, err = store.IssueToken("delete")
	})
	if panicked {
		t.Fatalf("IssueToken panicked: %s — green team must implement HMAC IssueToken", msg)
	}
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	if !hmacTokenRe.MatchString(tok) {
		t.Errorf("IssueToken returned token %q; want format matching %s", tok, hmacTokenRe.String())
	}
}

// TestHMACTamperDetected verifies that flipping the last byte of the HMAC part
// causes ConsumeToken to return ErrMalformedToken.
func TestHMACTamperDetected(t *testing.T) {
	defer goleak.VerifyNone(t)

	store, _ := newHMACStoreOrFatal(t, 10, 5*time.Minute)

	var tok string
	var err error
	msg, panicked := catchPanic(func() {
		tok, err = store.IssueToken("delete")
	})
	if panicked {
		t.Fatalf("IssueToken panicked: %s — green team must implement HMAC IssueToken", msg)
	}
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}

	// Tamper: flip the last character of the hmac part.
	tampered := tamperLastByte(tok)
	if tampered == tok {
		t.Skip("tamperLastByte could not produce a different token (token too short?)")
	}

	consumeExpectError(t, store, tampered, "delete", dispatch.ErrMalformedToken)
}

// TestHMACPurposeCheckBeforeVerify verifies that an unknown purpose returns
// ErrUnknownPurpose even when the token itself would be valid — i.e., the purpose
// closed-enum check fires BEFORE HMAC verification (spec §6.1.2).
func TestHMACPurposeCheckBeforeVerify(t *testing.T) {
	defer goleak.VerifyNone(t)

	store, _ := newHMACStoreOrFatal(t, 10, 5*time.Minute)

	var tok string
	var err error
	msg, panicked := catchPanic(func() {
		tok, err = store.IssueToken("delete")
	})
	if panicked {
		t.Fatalf("IssueToken panicked: %s — green team must implement HMAC IssueToken", msg)
	}
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}

	// "yeet" is not in AllowedPurposes — must get ErrUnknownPurpose, not ErrMalformedToken.
	consumeExpectError(t, store, tok, "yeet", dispatch.ErrUnknownPurpose)
}

// TestHMACKeyRotation verifies that a token issued with key version N can still
// be verified after rotating to key version N+1, and that newly issued tokens use
// the new key version.
func TestHMACKeyRotation(t *testing.T) {
	defer goleak.VerifyNone(t)

	store, keyDir := newHMACStoreOrFatal(t, 10, 5*time.Minute)

	// Issue token with v1 key (auto-generated on first IssueToken).
	var tok1 string
	var err error
	msg, panicked := catchPanic(func() {
		tok1, err = store.IssueToken("delete")
	})
	if panicked {
		t.Fatalf("IssueToken (v1) panicked: %s — green team must implement HMAC IssueToken", msg)
	}
	if err != nil {
		t.Fatalf("IssueToken (v1): %v", err)
	}

	// Simulate rotation: rename confirmation.key → confirmation.key.1,
	// remove confirmation.key so the next IssueToken auto-generates a fresh key (v2).
	v1KeyPath := filepath.Join(keyDir, "confirmation.key")
	v1RotatedPath := filepath.Join(keyDir, "confirmation.key.1")
	if renameErr := os.Rename(v1KeyPath, v1RotatedPath); renameErr != nil {
		t.Fatalf("rotate key: %v", renameErr)
	}

	// Issue token with v2 key.
	var tok2 string
	msg, panicked = catchPanic(func() {
		tok2, err = store.IssueToken("replace")
	})
	if panicked {
		t.Fatalf("IssueToken (v2) panicked: %s", msg)
	}
	if err != nil {
		t.Fatalf("IssueToken (v2): %v", err)
	}

	// Old token (v1) must still verify.
	msg, panicked = catchPanic(func() {
		err = store.ConsumeToken(tok1, "delete")
	})
	if panicked {
		t.Fatalf("ConsumeToken (v1) panicked: %s — green team must implement HMAC ConsumeToken", msg)
	}
	if err != nil {
		t.Errorf("ConsumeToken (v1) after rotation: %v; want nil (v1 key still available as .1)", err)
	}

	// New token (v2) must also verify.
	msg, panicked = catchPanic(func() {
		err = store.ConsumeToken(tok2, "replace")
	})
	if panicked {
		t.Fatalf("ConsumeToken (v2) panicked: %s", msg)
	}
	if err != nil {
		t.Errorf("ConsumeToken (v2): %v; want nil", err)
	}
}

// TestHMACKeyAutoGenerated verifies that IssueToken creates confirmation.key
// when keyDir is empty, the file has mode 0600, and contains 32 bytes.
func TestHMACKeyAutoGenerated(t *testing.T) {
	defer goleak.VerifyNone(t)

	store, keyDir := newHMACStoreOrFatal(t, 10, 5*time.Minute)

	// Confirm keyDir is actually empty before first issue.
	entries, err := os.ReadDir(keyDir)
	if err != nil {
		t.Fatalf("ReadDir keyDir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("keyDir not empty before first IssueToken: %v", entries)
	}

	var tok string
	msg, panicked := catchPanic(func() {
		tok, err = store.IssueToken("delete")
	})
	if panicked {
		t.Fatalf("IssueToken panicked: %s — green team must implement HMAC IssueToken", msg)
	}
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	_ = tok

	keyPath := filepath.Join(keyDir, "confirmation.key")
	data, readErr := os.ReadFile(keyPath)
	if readErr != nil {
		t.Fatalf("confirmation.key not created: %v", readErr)
	}
	if len(data) != 32 {
		t.Errorf("confirmation.key size = %d; want 32", len(data))
	}
}

// TestHMACRejectsPhase4Stub verifies that a STUB-PHASE4-* token is rejected
// with ErrMalformedToken (no back-compat with Phase 4 token format).
func TestHMACRejectsPhase4Stub(t *testing.T) {
	defer goleak.VerifyNone(t)

	store, _ := newHMACStoreOrFatal(t, 10, 5*time.Minute)

	// Construct a well-formed Phase 4 stub token (32 hex chars).
	phase4Token := "STUB-PHASE4-deadbeefdeadbeefdeadbeefdeadbeef"

	consumeExpectError(t, store, phase4Token, "delete", dispatch.ErrMalformedToken)
}

// TestHMACKeyFileMode verifies that the auto-generated confirmation.key has
// file permissions 0600 (owner read+write only).
func TestHMACKeyFileMode(t *testing.T) {
	defer goleak.VerifyNone(t)

	store, keyDir := newHMACStoreOrFatal(t, 10, 5*time.Minute)

	var err error
	var tok string
	msg, panicked := catchPanic(func() {
		tok, err = store.IssueToken("delete")
	})
	if panicked {
		t.Fatalf("IssueToken panicked: %s — green team must implement HMAC IssueToken", msg)
	}
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	_ = tok

	keyPath := filepath.Join(keyDir, "confirmation.key")
	info, statErr := os.Stat(keyPath)
	if statErr != nil {
		t.Fatalf("os.Stat(%q): %v", keyPath, statErr)
	}
	got := info.Mode().Perm()
	if got != 0o600 {
		t.Errorf("confirmation.key mode = %04o; want 0600", got)
	}
}

// tamperLastByte flips the last byte of the hmac (second) part of the token.
// Returns the original string unchanged if tampering is impossible.
func tamperLastByte(tok string) string {
	b := []byte(tok)
	if len(b) == 0 {
		return tok
	}
	last := len(b) - 1
	// Flip one bit in the last character.
	b[last] ^= 0x01
	// If the result is not a valid base64url char, use a known-safe substitute.
	if !isBase64URLChar(b[last]) {
		if b[last] == byte('A') {
			b[last] = 'B'
		} else {
			b[last] = 'A'
		}
	}
	// Make sure we actually changed something.
	if string(b) == tok {
		b[last] ^= 0x02
	}
	return string(b)
}

// isBase64URLChar returns true if c is a valid base64url alphabet character.
func isBase64URLChar(c byte) bool {
	return (c >= 'A' && c <= 'Z') ||
		(c >= 'a' && c <= 'z') ||
		(c >= '0' && c <= '9') ||
		c == '-' || c == '_'
}
