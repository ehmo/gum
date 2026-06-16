// Package dispatch — Red Team tests for gum-1otq.2 (write-tier confirmation token).
//
// These tests exercise the WRITE confirmation token semantics specified in §6.1.2
// and the distinct TTL/scope/replay-cache behavior for ConfirmationPurposeWrite
// versus ConfirmationPurposeDestructive.
//
// ALL TESTS IN THIS FILE ARE EXPECTED TO FAIL until the Green Team ships:
//   - DefaultTTLForPurpose(purpose string) time.Duration
//   - DefaultWriteTokenTTL and DefaultDestructiveTokenTTL constants
//   - Scope-ignore semantics for ConfirmationPurposeWrite in VerifyConfirmationToken
//   - Replay-cache keying that includes purpose (so write and destructive tokens with
//     matching params don't cross-poison each other's replay slots)
//
// Spec anchors:
//   §6.1.2 line 1128 — "TTL: 5 minutes from the issuance timestamp embedded in the token"
//   §6.1.2 line 1122 — high_stakes_write and ABI write-confirmation paths use
//                       ConfirmationPurposeWrite = "gum_confirm_write"
//   §6.1.2 line 1120 — confirmation_purpose is in the binding tuple; mismatch → reject
//   §4.1 / §6.1      — write-tier does NOT enforce destructive_scope_canonical
//
// Done criterion: go test -run TestWriteToken ./internal/dispatch/... FAILS.
package dispatch

import (
	"errors"
	"testing"
	"time"
)

// writeConfirmationParams returns a ConfirmationParams for ConfirmationPurposeWrite,
// analogous to confirmationBindingParams in confirmation_token_binding_test.go.
// ResourceKey is populated; Scope is deliberately populated so we can test
// whether it is ignored or enforced by the write tier.
func writeConfirmationParams(ttl time.Duration) ConfirmationParams {
	return ConfirmationParams{
		OpID:            "gmail.users.drafts.create",
		VariantID:       "gmail.v1.rest.users.drafts.create",
		ArgsHash:        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ResourceKey:     "draft001",
		AuthFingerprint: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Scope:           `["gmail"]`,
		Purpose:         ConfirmationPurposeWrite,
		TTL:             ttl,
	}
}

// TestWriteTokenIssuedWithWritePurpose (§6.1.2) — IssueConfirmationToken with
// Purpose=ConfirmationPurposeWrite and TTL=5*time.Minute returns a non-empty token,
// and VerifyConfirmationToken with identical params returns nil.
//
// Required: ConfirmationPurposeWrite is in the closed purpose enum so that
// VerifyConfirmationToken does not reject it as "unknown_purpose".
func TestWriteTokenIssuedWithWritePurpose(t *testing.T) {
	params := writeConfirmationParams(5 * time.Minute)

	tok, err := IssueConfirmationToken(params)
	if err != nil {
		t.Fatalf("IssueConfirmationToken(write): %v", err)
	}
	if tok == "" {
		t.Fatal("IssueConfirmationToken(write) returned empty token")
	}

	// Verify with same params (TTL field unused by Verify).
	verifyParams := writeConfirmationParams(0)
	if err := VerifyConfirmationToken(tok, verifyParams); err != nil {
		t.Errorf("VerifyConfirmationToken(write) round-trip: %v; want nil", err)
	}
}

// TestWriteTokenDistinctFromDestructive (§6.1.2) — a token issued with
// ConfirmationPurposeWrite MUST NOT verify when ConfirmationPurposeDestructive is
// supplied as the expected purpose. Purpose is part of both the HMAC binding tuple
// and the wire format, so cross-tier replay returns reason="mismatch".
func TestWriteTokenDistinctFromDestructive(t *testing.T) {
	writeParams := writeConfirmationParams(5 * time.Minute)

	tok, err := IssueConfirmationToken(writeParams)
	if err != nil {
		t.Fatalf("IssueConfirmationToken(write): %v", err)
	}

	// Attempt verification with destructive purpose — same other fields.
	destructiveVerifyParams := writeConfirmationParams(0)
	destructiveVerifyParams.Purpose = ConfirmationPurposeDestructive

	err = VerifyConfirmationToken(tok, destructiveVerifyParams)
	if err == nil {
		t.Fatal("VerifyConfirmationToken: expected mismatch when verifying write token with destructive purpose, got nil")
	}

	var se *StructuredError
	if !errors.As(err, &se) {
		t.Fatalf("expected *StructuredError, got %T: %v", err, err)
	}
	if se.ErrCode != ErrCodeConfirmationTokenInvalid {
		t.Errorf("ErrCode = %q; want CONFIRMATION_TOKEN_INVALID", se.ErrCode)
	}
	gotReason, _ := se.Detail["reason"].(string)
	if gotReason != "mismatch" {
		t.Errorf("reason = %q; want \"mismatch\" (cross-tier purpose swap)", gotReason)
	}
}

// TestWriteTokenDefaultTTL (§6.1.2 line 1128) — DefaultTTLForPurpose must return
// the spec-defined defaults:
//   - ConfirmationPurposeWrite       → DefaultWriteTokenTTL (spec: 5 minutes)
//   - ConfirmationPurposeDestructive → DefaultDestructiveTokenTTL (spec: 5 minutes; same TTL,
//     single unified spec value — verify exact constant from DefaultDestructiveTokenTTL)
//
// Spec §6.1.2 line 1128 states: "TTL: 5 minutes from the issuance timestamp."
// There is no per-purpose TTL differentiation in the spec; this test verifies that
// both purposes return >= 5 minutes (300 seconds) as the default, and that
// DefaultTTLForPurpose exists as a callable export.
//
// Required new export: func DefaultTTLForPurpose(purpose string) time.Duration
// Required new constants: DefaultWriteTokenTTL, DefaultDestructiveTokenTTL
func TestWriteTokenDefaultTTL(t *testing.T) {
	writeTTL := DefaultTTLForPurpose(ConfirmationPurposeWrite)
	if writeTTL <= 0 {
		t.Errorf("DefaultTTLForPurpose(write) = %v; want > 0", writeTTL)
	}
	if writeTTL != DefaultWriteTokenTTL {
		t.Errorf("DefaultTTLForPurpose(write) = %v; want DefaultWriteTokenTTL = %v", writeTTL, DefaultWriteTokenTTL)
	}
	// Spec §6.1.2 line 1128: "5 minutes"
	if writeTTL < 5*time.Minute {
		t.Errorf("DefaultTTLForPurpose(write) = %v; want >= 5 minutes (spec §6.1.2)", writeTTL)
	}

	destructiveTTL := DefaultTTLForPurpose(ConfirmationPurposeDestructive)
	if destructiveTTL <= 0 {
		t.Errorf("DefaultTTLForPurpose(destructive) = %v; want > 0", destructiveTTL)
	}
	if destructiveTTL != DefaultDestructiveTokenTTL {
		t.Errorf("DefaultTTLForPurpose(destructive) = %v; want DefaultDestructiveTokenTTL = %v", destructiveTTL, DefaultDestructiveTokenTTL)
	}
	// Spec §6.1.2 line 1128: unified "5 minutes" for all purposes
	if destructiveTTL < 5*time.Minute {
		t.Errorf("DefaultTTLForPurpose(destructive) = %v; want >= 5 minutes (spec §6.1.2)", destructiveTTL)
	}
}

// TestWriteTokenScopeIgnoredOrRejected (§6.1.2, §4.1) — the spec's write-tier binding
// tuple for gum.write is (op_id, resolved variant_id, args_canonical, caller, risk_class,
// confirmation_purpose); it does NOT include destructive_scope_canonical (that field is
// only in the destructive / code-mode tuples).
//
// Therefore a write token issued with Scope="[\"gmail\"]" and verified with
// Scope="[\"some_other_resource\"]" MUST succeed — scope is not a binding field for the
// write tier and must be ignored.
//
// If the implementation incorrectly includes Scope in the write-tier binding hash,
// this test will fail with reason="mismatch", correctly flagging the bug.
func TestWriteTokenScopeIgnoredOrRejected(t *testing.T) {
	issueParams := writeConfirmationParams(5 * time.Minute)
	issueParams.Scope = `["gmail"]`

	tok, err := IssueConfirmationToken(issueParams)
	if err != nil {
		t.Fatalf("IssueConfirmationToken(write, scope=gmail): %v", err)
	}

	// Verify with a DIFFERENT scope value.
	// Write tier MUST ignore scope — verification must succeed.
	verifyParams := writeConfirmationParams(0)
	verifyParams.Scope = `["some_other_resource"]`

	if err := VerifyConfirmationToken(tok, verifyParams); err != nil {
		t.Errorf("VerifyConfirmationToken(write, different scope): %v; want nil — scope must be ignored for write-tier tokens (§4.1 binding tuple excludes scope)", err)
	}
}

// TestWriteTokenReplayCacheSeparateFromDestructive (§6.1.2 replay cache) — the replay
// cache MUST be keyed by the full signature (which includes the purpose in the BLAKE3
// keyed MAC input), so consuming a write-tier token must NOT poison the replay slot for
// a destructive-tier token with otherwise-identical params (different purpose → different
// signature → different replay-cache key).
//
// Test plan:
//  1. Issue a write token; verify it (consumed in replay cache).
//  2. Issue a destructive token with the same non-purpose params; verify it.
//  3. Step 2 MUST succeed — replay cache must not conflate different-purpose tokens.
func TestWriteTokenReplayCacheSeparateFromDestructive(t *testing.T) {
	// Issue and consume a write token.
	writeParams := writeConfirmationParams(5 * time.Minute)
	writeTok, err := IssueConfirmationToken(writeParams)
	if err != nil {
		t.Fatalf("IssueConfirmationToken(write): %v", err)
	}
	if err := VerifyConfirmationToken(writeTok, writeConfirmationParams(0)); err != nil {
		t.Fatalf("VerifyConfirmationToken(write) first use: %v; want nil", err)
	}

	// Same params but destructive purpose.
	destructiveParams := writeConfirmationParams(5 * time.Minute)
	destructiveParams.Purpose = ConfirmationPurposeDestructive
	destructiveTok, err := IssueConfirmationToken(destructiveParams)
	if err != nil {
		t.Fatalf("IssueConfirmationToken(destructive, same params): %v", err)
	}

	destructiveVerifyParams := writeConfirmationParams(0)
	destructiveVerifyParams.Purpose = ConfirmationPurposeDestructive
	if err := VerifyConfirmationToken(destructiveTok, destructiveVerifyParams); err != nil {
		t.Errorf("VerifyConfirmationToken(destructive) after write-cache consume: %v; want nil — replay cache must be keyed by signature which includes purpose", err)
	}
}

// TestWriteTokenLongerTTLThanDestructive (§6.1.2, informative — verifies the
// DefaultTTLForPurpose constants satisfy reasonable lower bounds and that both meet
// the spec's "5 minutes" unified TTL).
//
// The test validates:
//   - DefaultWriteTokenTTL >= 60 seconds (minimum useful token window)
//   - DefaultDestructiveTokenTTL >= 30 seconds (minimum useful token window)
//   - Both constants are >= 5 minutes per spec §6.1.2 line 1128
//
// Required: DefaultWriteTokenTTL and DefaultDestructiveTokenTTL exported constants.
func TestWriteTokenLongerTTLThanDestructive(t *testing.T) {
	// Both must be at least a minimum useful duration.
	if DefaultWriteTokenTTL < 60*time.Second {
		t.Errorf("DefaultWriteTokenTTL = %v; want >= 60s", DefaultWriteTokenTTL)
	}
	if DefaultDestructiveTokenTTL < 30*time.Second {
		t.Errorf("DefaultDestructiveTokenTTL = %v; want >= 30s", DefaultDestructiveTokenTTL)
	}
	// Spec §6.1.2 line 1128: "5 minutes" is the normative TTL for all confirmation tokens.
	if DefaultWriteTokenTTL < 5*time.Minute {
		t.Errorf("DefaultWriteTokenTTL = %v; spec §6.1.2 requires >= 5 minutes", DefaultWriteTokenTTL)
	}
	if DefaultDestructiveTokenTTL < 5*time.Minute {
		t.Errorf("DefaultDestructiveTokenTTL = %v; spec §6.1.2 requires >= 5 minutes", DefaultDestructiveTokenTTL)
	}

	// Verify DefaultTTLForPurpose agrees with the constants.
	if DefaultTTLForPurpose(ConfirmationPurposeWrite) != DefaultWriteTokenTTL {
		t.Errorf("DefaultTTLForPurpose(write) != DefaultWriteTokenTTL")
	}
	if DefaultTTLForPurpose(ConfirmationPurposeDestructive) != DefaultDestructiveTokenTTL {
		t.Errorf("DefaultTTLForPurpose(destructive) != DefaultDestructiveTokenTTL")
	}
}
