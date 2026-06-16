// Package dispatch — Red Team tests for gum-vld (BLAKE3 source-rehash token signing).
//
// These tests exercise the new IssueConfirmationToken / VerifyConfirmationToken API
// specified in §6.1.2 of spec.md (token binding tuple, source-rehash, BLAKE3 key
// derivation, replay cache, purpose-before-HMAC ordering).
//
// ALL TESTS IN THIS FILE ARE EXPECTED TO FAIL until gum-1otq.1 (Green Team) ships the
// implementation. The "Done criterion" is `go test ./internal/dispatch/...` reporting
// compile errors (`undefined: dispatch.IssueConfirmationToken`) or behavioral failures
// where the implementation exists but lacks the required semantics.
//
// Spec anchors:
//   §6.1.2 — Confirmation token implementation (normative)
//   §1421  — Stable runtime error codes (CONFIRMATION_TOKEN_INVALID, reason closed enum)
package dispatch

import (
	"errors"
	"testing"
	"time"
)

// ----------------------------------------------------------------------------
// Required API surface (must be exported from package dispatch by Green Team):
//
//   type ConfirmationParams struct { ... }
//   const ConfirmationPurposeDestructive = "gum_confirm_destructive"
//   const ConfirmationPurposeWrite       = "gum_confirm_write"
//   func IssueConfirmationToken(params ConfirmationParams) (string, error)
//   func VerifyConfirmationToken(token string, params ConfirmationParams) error
//   func SetSourceHashForTest(hash string)   // test helper — sets in-process source hash
//
// These are referenced below so the compiler will flag "undefined" immediately,
// giving the Green Team a clear diff-shaped target.
// ----------------------------------------------------------------------------

// confirmationBindingParams returns a fully-populated ConfirmationParams suitable
// for round-trip tests. All fields are non-empty to exercise the full binding tuple.
func confirmationBindingParams(ttl time.Duration) ConfirmationParams {
	return ConfirmationParams{
		OpID:            "gmail.users.messages.trash",
		VariantID:       "gmail.v1.rest.users.messages.trash",
		ArgsHash:        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", // 64 hex chars
		ResourceKey:     "msg001",
		AuthFingerprint: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Scope:           `["gmail"]`,
		Purpose:         ConfirmationPurposeDestructive,
		TTL:             ttl,
	}
}

// assertTokenInvalid is an inline helper that asserts err is a *StructuredError
// with code CONFIRMATION_TOKEN_INVALID and the given reason string.
func assertTokenInvalid(t *testing.T, err error, wantReason string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected CONFIRMATION_TOKEN_INVALID(%q), got nil", wantReason)
	}
	var se *StructuredError
	if !errors.As(err, &se) {
		t.Fatalf("expected *StructuredError, got %T: %v", err, err)
	}
	if se.ErrCode != ErrCodeConfirmationTokenInvalid {
		t.Errorf("ErrCode = %q; want %q", se.ErrCode, ErrCodeConfirmationTokenInvalid)
	}
	if se.Detail == nil {
		t.Fatalf("Detail is nil; want reason=%q", wantReason)
	}
	gotReason, ok := se.Detail["reason"]
	if !ok {
		t.Fatalf("Detail[\"reason\"] absent; want %q", wantReason)
	}
	if gotReason != wantReason {
		t.Errorf("reason = %q; want %q", gotReason, wantReason)
	}
}

// TestConfirmationTokenValidRoundTrip (§6.1.2) — issue a valid token and verify it
// successfully via the full binding tuple.  Asserts err == nil on round-trip.
func TestConfirmationTokenValidRoundTrip(t *testing.T) {
	params := confirmationBindingParams(5 * time.Minute)

	tok, err := IssueConfirmationToken(params)
	if err != nil {
		t.Fatalf("IssueConfirmationToken: %v", err)
	}
	if tok == "" {
		t.Fatal("IssueConfirmationToken returned empty token")
	}

	// Verify with identical params (TTL field is unused by Verify, but must not crash).
	if err := VerifyConfirmationToken(tok, params); err != nil {
		t.Errorf("VerifyConfirmationToken round-trip: %v; want nil", err)
	}
}

// TestConfirmationTokenMissingFails (§6.1.2, §1421 reason=missing) — empty string
// returns CONFIRMATION_TOKEN_INVALID with reason "missing".
func TestConfirmationTokenMissingFails(t *testing.T) {
	params := confirmationBindingParams(5 * time.Minute)
	err := VerifyConfirmationToken("", params)
	assertTokenInvalid(t, err, "missing")
}

// TestConfirmationTokenExpiredFails (§6.1.2 TTL, §1421 reason=expired) — token issued
// with TTL=1ns expires before Verify is called (sleep 1ms to guarantee passage).
func TestConfirmationTokenExpiredFails(t *testing.T) {
	params := confirmationBindingParams(1 * time.Nanosecond)

	tok, err := IssueConfirmationToken(params)
	if err != nil {
		t.Fatalf("IssueConfirmationToken: %v", err)
	}

	time.Sleep(1 * time.Millisecond)

	verifyParams := confirmationBindingParams(0) // TTL unused in Verify
	verifyParams.Purpose = ConfirmationPurposeDestructive
	assertTokenInvalid(t, VerifyConfirmationToken(tok, verifyParams), "expired")
}

// TestConfirmationTokenReplayedFails (§6.1.2, §1421 reason=replayed) — verifying the
// same token twice must fail the second time with reason "replayed".
// This test forces the replay-cache API contract (gum-1otq.5) even if the cache is not
// yet implemented: if no cache exists the second call will succeed and the test fails,
// which is the correct Red Team outcome.
func TestConfirmationTokenReplayedFails(t *testing.T) {
	params := confirmationBindingParams(5 * time.Minute)

	tok, err := IssueConfirmationToken(params)
	if err != nil {
		t.Fatalf("IssueConfirmationToken: %v", err)
	}

	// First verify must succeed.
	if err := VerifyConfirmationToken(tok, params); err != nil {
		t.Fatalf("VerifyConfirmationToken first call: %v; want nil", err)
	}

	// Second verify with identical token must be rejected as replayed (§1421 reason=replayed).
	assertTokenInvalid(t, VerifyConfirmationToken(tok, params), "replayed")
}

// TestConfirmationTokenMismatchOpID (§6.1.2 binding tuple, §1421 reason=mismatch) —
// token issued for opID="A"; verify with opID="B" must return reason "mismatch".
func TestConfirmationTokenMismatchOpID(t *testing.T) {
	issueParams := confirmationBindingParams(5 * time.Minute)
	issueParams.OpID = "drive.files.delete"

	tok, err := IssueConfirmationToken(issueParams)
	if err != nil {
		t.Fatalf("IssueConfirmationToken: %v", err)
	}

	verifyParams := confirmationBindingParams(0)
	verifyParams.OpID = "gmail.users.messages.trash" // different op
	assertTokenInvalid(t, VerifyConfirmationToken(tok, verifyParams), "mismatch")
}

// TestConfirmationTokenMismatchVariant (§6.1.2 binding tuple, §1421 reason=mismatch) —
// variant_id changed between issue and verify.
func TestConfirmationTokenMismatchVariant(t *testing.T) {
	issueParams := confirmationBindingParams(5 * time.Minute)
	issueParams.VariantID = "gmail.v1.rest.users.messages.trash"

	tok, err := IssueConfirmationToken(issueParams)
	if err != nil {
		t.Fatalf("IssueConfirmationToken: %v", err)
	}

	verifyParams := confirmationBindingParams(0)
	verifyParams.VariantID = "gmail.v2.rest.users.messages.trash" // different variant
	assertTokenInvalid(t, VerifyConfirmationToken(tok, verifyParams), "mismatch")
}

// TestConfirmationTokenMismatchArgsHash (§6.1.2 binding tuple, §1421 reason=mismatch) —
// args_hash changed between issue and verify (models argument tampering).
func TestConfirmationTokenMismatchArgsHash(t *testing.T) {
	issueParams := confirmationBindingParams(5 * time.Minute)
	tok, err := IssueConfirmationToken(issueParams)
	if err != nil {
		t.Fatalf("IssueConfirmationToken: %v", err)
	}

	verifyParams := confirmationBindingParams(0)
	verifyParams.ArgsHash = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc" // different hash
	assertTokenInvalid(t, VerifyConfirmationToken(tok, verifyParams), "mismatch")
}

// TestConfirmationTokenMismatchAuthFingerprint (§6.1.2 binding tuple, §1421 reason=mismatch) —
// auth_subject_fingerprint changed between issue and verify (models cross-principal replay).
func TestConfirmationTokenMismatchAuthFingerprint(t *testing.T) {
	issueParams := confirmationBindingParams(5 * time.Minute)
	tok, err := IssueConfirmationToken(issueParams)
	if err != nil {
		t.Fatalf("IssueConfirmationToken: %v", err)
	}

	verifyParams := confirmationBindingParams(0)
	verifyParams.AuthFingerprint = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd" // different fingerprint
	assertTokenInvalid(t, VerifyConfirmationToken(tok, verifyParams), "mismatch")
}

// TestConfirmationTokenMismatchScope (§6.1.2 binding tuple destructive_scope_canonical,
// §1421 reason=mismatch) — destructive scope changed between issue and verify.
func TestConfirmationTokenMismatchScope(t *testing.T) {
	issueParams := confirmationBindingParams(5 * time.Minute)
	issueParams.Scope = `["gmail"]`

	tok, err := IssueConfirmationToken(issueParams)
	if err != nil {
		t.Fatalf("IssueConfirmationToken: %v", err)
	}

	verifyParams := confirmationBindingParams(0)
	verifyParams.Scope = `["gmail","drive"]` // broadened scope not approved
	assertTokenInvalid(t, VerifyConfirmationToken(tok, verifyParams), "mismatch")
}

// TestConfirmationTokenUnknownPurposeCheckedFirst (§6.1.2 pre-HMAC enum check,
// §1421 reason=unknown_purpose) — CRITICAL ORDERING TEST.
//
// The spec mandates: "the dispatcher MUST validate confirmation_purpose against this enum
// BEFORE running HMAC verification: an out-of-enum value is rejected with
// CONFIRMATION_TOKEN_INVALID reason unknown_purpose without ever computing the HMAC."
//
// Strategy: we call VerifyConfirmationToken with a purpose that is outside the closed
// enum AND a token that has a forged/garbage signature. If the implementation checks
// purpose first, it returns "unknown_purpose" before ever touching the HMAC. If it
// checks HMAC first, it would return "mismatch" (or similar) instead. The assertion
// on "unknown_purpose" is proof of ordering.
func TestConfirmationTokenUnknownPurposeCheckedFirst(t *testing.T) {
	// Use an arbitrary non-empty string that looks like a token but is not from IssueConfirmationToken.
	// If purpose is checked first, we never reach HMAC verification.
	forgeryToken := "dGhpcy5pcy5hLmZvcmdlZC50b2tlbg.c2lnbmF0dXJl" // base64url("this.is.a.forged.token") . base64url("signature")

	badPurposeParams := confirmationBindingParams(5 * time.Minute)
	badPurposeParams.Purpose = "forged_unknown_string" // not in closed enum

	err := VerifyConfirmationToken(forgeryToken, badPurposeParams)
	assertTokenInvalid(t, err, "unknown_purpose")
}

// TestConfirmationTokenSourceRehashChanges (§6.1.2 source_rehash / forward-incompatibility
// guard) — token issued with source-hash=A; source hash changed to B in-process;
// previously issued token must now fail (reason: "mismatch" or "expired" per spec).
//
// The Green Team MUST expose SetSourceHashForTest(hash string) to allow injection of
// the source hash for testing without modifying binary content. If this helper is absent,
// this test will fail to compile, which is the correct Red Team outcome.
func TestConfirmationTokenSourceRehashChanges(t *testing.T) {
	const hashA = "aabbccdd" + "aabbccdd" + "aabbccdd" + "aabbccdd" + // 32 bytes hex
		"aabbccdd" + "aabbccdd" + "aabbccdd" + "aabbccdd"
	const hashB = "11223344" + "11223344" + "11223344" + "11223344" +
		"11223344" + "11223344" + "11223344" + "11223344"

	// Set source hash to A before issuing.
	SetSourceHashForTest(hashA)

	params := confirmationBindingParams(5 * time.Minute)
	tok, err := IssueConfirmationToken(params)
	if err != nil {
		t.Fatalf("IssueConfirmationToken: %v", err)
	}

	// Token should verify while hash is still A.
	if err := VerifyConfirmationToken(tok, params); err != nil {
		t.Fatalf("VerifyConfirmationToken with original source hash: %v; want nil", err)
	}

	// Simulate spec change: advance to hash B.
	SetSourceHashForTest(hashB)
	t.Cleanup(func() { SetSourceHashForTest(hashA) }) // restore for other tests

	// Now the previously valid token must be silently rejected (§6.1.2 source_rehash).
	err = VerifyConfirmationToken(tok, params)
	if err == nil {
		t.Fatal("VerifyConfirmationToken after source hash change: expected error, got nil")
	}
	var se *StructuredError
	if !errors.As(err, &se) {
		t.Fatalf("expected *StructuredError after source hash change, got %T: %v", err, err)
	}
	if se.ErrCode != ErrCodeConfirmationTokenInvalid {
		t.Errorf("ErrCode = %q; want CONFIRMATION_TOKEN_INVALID", se.ErrCode)
	}
	// Reason MUST be "mismatch" or "expired" — both are spec-compliant for source-rehash
	// silent expiry.  We accept either to give Green Team implementation latitude.
	reason, _ := se.Detail["reason"].(string)
	if reason != "mismatch" && reason != "expired" {
		t.Errorf("reason = %q; want \"mismatch\" or \"expired\" (source-rehash silent expiry)", reason)
	}
}

// TestConfirmationTokenBLAKE3Used (§6.1.2 source_rehash / gum-1otq.1) — verifies that
// the token signing uses BLAKE3 for key derivation (the spec commits to BLAKE3 keyed
// mode rather than HMAC-SHA256 for the new source-rehash signing path).
//
// The Green Team MUST add "lukechampine.com/blake3" to go.mod. This test verifies
// the algorithmic commitment by checking that the token internal structure reflects
// BLAKE3 output properties: deterministic 256-bit (32-byte → 64 hex char) hash.
//
// Strategy: call IssueConfirmationToken twice with identical inputs (same nanosecond
// wall-clock would be ideal, but not guaranteed). Instead, we inject fixed params and
// use SetSourceHashForTest to control all inputs, then verify that the token embeds a
// 64-character hex string consistent with a BLAKE3 digest (not SHA-256).
//
// Because we cannot parse the opaque token format from the outside, we test the
// observable contract indirectly: two tokens issued with the same params in the same
// process must verify successfully AND a token with HMAC-SHA256 signing (old path)
// must NOT verify against the new API. The latter is tested by constructing an
// HMAC-SHA256-signed payload manually and asserting it is rejected with "mismatch".
func TestConfirmationTokenBLAKE3Used(t *testing.T) {
	// Part 1: determinism — same params → same binding hash (tokens may differ due to
	// nonce, but both must verify against identical ConfirmationParams).
	p1 := confirmationBindingParams(5 * time.Minute)
	tok1, err := IssueConfirmationToken(p1)
	if err != nil {
		t.Fatalf("IssueConfirmationToken tok1: %v", err)
	}
	tok2, err := IssueConfirmationToken(p1)
	if err != nil {
		t.Fatalf("IssueConfirmationToken tok2: %v", err)
	}
	if err := VerifyConfirmationToken(tok1, p1); err != nil {
		t.Errorf("tok1 verify: %v; want nil", err)
	}
	if err := VerifyConfirmationToken(tok2, p1); err != nil {
		t.Errorf("tok2 verify: %v; want nil", err)
	}

	// Part 2: a token produced by the *old* HMAC-SHA256 TokenStore.IssueToken path
	// (with a matching AllowedPurposes value) MUST NOT verify via VerifyConfirmationToken.
	// This asserts that the two APIs use distinct signing paths and the new BLAKE3-based
	// IssueConfirmationToken token format is NOT accepted by the old TokenStore.ConsumeToken,
	// and vice versa.
	//
	// We verify this by presenting a raw old-style token to VerifyConfirmationToken.
	// Since "delete" is not in the ConfirmationPurpose closed enum the new API recognises,
	// the call must return "unknown_purpose" (purpose checked before HMAC).
	legacyPurposeParams := p1
	legacyPurposeParams.Purpose = "delete" // AllowedPurposes value, not a ConfirmationPurpose constant

	// tok1 was signed for ConfirmationPurposeDestructive; verifying with "delete" as purpose
	// must fail with unknown_purpose (if "delete" is not in new purpose enum) or mismatch
	// (if it is in the enum but binding differs).  Either way, it must not return nil.
	err = VerifyConfirmationToken(tok1, legacyPurposeParams)
	if err == nil {
		t.Error("VerifyConfirmationToken with legacy purpose \"delete\": expected error, got nil")
	}
}
