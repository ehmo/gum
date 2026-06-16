package dispatch

import (
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"
)

// TestVerifyConfirmationTokenMalformedReturnsMissing pins
// VerifyConfirmationToken's `len(parts) != 6 || parts[0] != version`
// arm (confirmation_token.go:117-119). Reached by a token with the
// wrong number of dot-separated fields — must surface as `missing`.
func TestVerifyConfirmationTokenMalformedReturnsMissing(t *testing.T) {
	t.Parallel()
	params := confirmationBindingParams(time.Minute)
	// 3 fields instead of 6.
	err := VerifyConfirmationToken("v1.a.b", params)
	if err == nil {
		t.Fatal("VerifyConfirmationToken(malformed) err=nil; want missing")
	}
	var se *StructuredError
	if !errors.As(err, &se) || se.Detail["reason"] != tokenReasonMissing {
		t.Errorf("err=%v; want reason=%q", err, tokenReasonMissing)
	}
}

// TestVerifyConfirmationTokenIssuedAtNotIntReturnsMissing pins
// `Sscanf(parts[1]) err → missing` (confirmation_token.go:121-123).
// Reached by a token whose issuedAt slot is non-numeric.
func TestVerifyConfirmationTokenIssuedAtNotIntReturnsMissing(t *testing.T) {
	t.Parallel()
	params := confirmationBindingParams(time.Minute)
	bad := "v1.abc.123.gum_confirm_destructive.deadbeef.cafefeed"
	err := VerifyConfirmationToken(bad, params)
	if err == nil {
		t.Fatal("VerifyConfirmationToken(bad issuedAt) err=nil; want missing")
	}
	var se *StructuredError
	if !errors.As(err, &se) || se.Detail["reason"] != tokenReasonMissing {
		t.Errorf("err=%v; want reason=%q", err, tokenReasonMissing)
	}
}

// TestVerifyConfirmationTokenExpiryNotIntReturnsMissing pins
// `Sscanf(parts[2]) err → missing` (confirmation_token.go:124-126).
// Reached by a token whose expiry slot is non-numeric.
func TestVerifyConfirmationTokenExpiryNotIntReturnsMissing(t *testing.T) {
	t.Parallel()
	params := confirmationBindingParams(time.Minute)
	bad := "v1.123.xyz.gum_confirm_destructive.deadbeef.cafefeed"
	err := VerifyConfirmationToken(bad, params)
	if err == nil {
		t.Fatal("VerifyConfirmationToken(bad expiry) err=nil; want missing")
	}
	var se *StructuredError
	if !errors.As(err, &se) || se.Detail["reason"] != tokenReasonMissing {
		t.Errorf("err=%v; want reason=%q", err, tokenReasonMissing)
	}
}

// TestVerifyConfirmationTokenSigHexInvalidReturnsMismatch pins
// `hex.DecodeString(sigHex) err → mismatch` (confirmation_token.go:135-138).
// Reached by replacing the sig field with non-hex characters; expiry
// must be in the future so we get past the expiry check.
func TestVerifyConfirmationTokenSigHexInvalidReturnsMismatch(t *testing.T) {
	t.Parallel()
	params := confirmationBindingParams(time.Minute)
	good, err := IssueConfirmationToken(params)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	parts := strings.SplitN(good, ".", 6)
	parts[5] = "zzzz" // not valid hex
	bad := strings.Join(parts, ".")
	err = VerifyConfirmationToken(bad, params)
	if err == nil {
		t.Fatal("VerifyConfirmationToken(bad sigHex) err=nil; want mismatch")
	}
	var se *StructuredError
	if !errors.As(err, &se) || se.Detail["reason"] != tokenReasonMismatch {
		t.Errorf("err=%v; want reason=%q", err, tokenReasonMismatch)
	}
}

// TestVerifyConfirmationTokenBindingHashHexInvalidReturnsMismatch
// pins `hex.DecodeString(bindingHashHex) err → mismatch`
// (confirmation_token.go:139-142). Reached by replacing the binding
// hash field with non-hex characters.
func TestVerifyConfirmationTokenBindingHashHexInvalidReturnsMismatch(t *testing.T) {
	t.Parallel()
	params := confirmationBindingParams(time.Minute)
	good, err := IssueConfirmationToken(params)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	parts := strings.SplitN(good, ".", 6)
	parts[4] = "zzzz"
	bad := strings.Join(parts, ".")
	err = VerifyConfirmationToken(bad, params)
	if err == nil {
		t.Fatal("VerifyConfirmationToken(bad bindingHashHex) err=nil; want mismatch")
	}
	var se *StructuredError
	if !errors.As(err, &se) || se.Detail["reason"] != tokenReasonMismatch {
		t.Errorf("err=%v; want reason=%q", err, tokenReasonMismatch)
	}
}

// TestVerifyConfirmationTokenBindingHashTamperedReturnsMismatch pins
// the final `subtle.ConstantTimeCompare(bindingHash) != 1 → mismatch`
// arm (confirmation_token.go:159-161). Reached by issuing a valid
// token then swapping the bindingHashHex slot with a different but
// valid-length hex string. The signature still validates (it's bound
// to the original hash, which matches what current params recompute),
// but the in-token bindingHashHex no longer matches expected — the
// defence-in-depth check fires.
func TestVerifyConfirmationTokenBindingHashTamperedReturnsMismatch(t *testing.T) {
	t.Parallel()
	params := confirmationBindingParams(time.Minute)
	good, err := IssueConfirmationToken(params)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	parts := strings.SplitN(good, ".", 6)
	// Decode original then flip a byte → still valid hex, wrong content.
	orig, err := hex.DecodeString(parts[4])
	if err != nil {
		t.Fatalf("decode orig bindingHash: %v", err)
	}
	orig[0] ^= 0xFF
	parts[4] = hex.EncodeToString(orig)
	bad := strings.Join(parts, ".")
	err = VerifyConfirmationToken(bad, params)
	if err == nil {
		t.Fatal("VerifyConfirmationToken(tampered bindingHash) err=nil; want mismatch")
	}
	var se *StructuredError
	if !errors.As(err, &se) || se.Detail["reason"] != tokenReasonMismatch {
		t.Errorf("err=%v; want reason=%q", err, tokenReasonMismatch)
	}
}
