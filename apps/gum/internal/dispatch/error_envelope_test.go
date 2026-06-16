package dispatch

// error_envelope_test.go — RED TEAM tests for gum-vq4z.11
// These tests are intentionally written against types that do not yet exist.
// They must fail at compile time with "undefined: dispatch.StructuredError" etc.
// DO NOT add implementation here.

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Test 1 — Basic Error() shape
// spec ref: §1421 (stable runtime error codes list); §3.1 step 7 (SERVICE_DOWN)
// ---------------------------------------------------------------------------

func TestStructuredErrorBasicShape(t *testing.T) {
	err := NewStructuredError(ErrCodeOpNotFound, "no such op")
	s := err.Error()
	if !strings.Contains(s, "OP_NOT_FOUND") {
		t.Errorf("Error() = %q; want string containing %q", s, "OP_NOT_FOUND")
	}
	if !strings.Contains(s, "no such op") {
		t.Errorf("Error() = %q; want string containing %q", s, "no such op")
	}
}

// ---------------------------------------------------------------------------
// Test 2 — JSON shape for a simple error (no detail, no retryable)
// spec ref: §4.1 op_id validation: {"error_code":"OP_NOT_FOUND","message":"...","suggestions":[...]}
// ---------------------------------------------------------------------------

func TestStructuredErrorJSONShape(t *testing.T) {
	se := NewStructuredError(ErrCodeOpNotFound, "no such op")
	b, err := json.Marshal(se)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	if got, ok := m["error_code"]; !ok || got != "OP_NOT_FOUND" {
		t.Errorf("JSON field error_code = %v; want %q", got, "OP_NOT_FOUND")
	}
	if got, ok := m["message"]; !ok || got != "no such op" {
		t.Errorf("JSON field message = %v; want %q", got, "no such op")
	}
}

// ---------------------------------------------------------------------------
// Test 3 — WithDetail fields are flattened into top-level JSON (NOT nested under "detail")
// spec ref: §4.1 RISK_TOOL_MISMATCH envelope, §8.42 INVALID_ARGS envelope (all flattened)
// ---------------------------------------------------------------------------

func TestStructuredErrorWithDetailFlattened(t *testing.T) {
	se := NewStructuredError(ErrCodeAmbiguousVariant, "ambiguous").
		WithDetail("op_id", "gmail.users.messages.list").
		WithDetail("variants", []string{"a", "b"})

	b, err := json.Marshal(se)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	if _, ok := m["error_code"]; !ok {
		t.Error("missing top-level key: error_code")
	}
	if _, ok := m["message"]; !ok {
		t.Error("missing top-level key: message")
	}
	if _, ok := m["op_id"]; !ok {
		t.Errorf("op_id should be a top-level key; JSON was: %s", string(b))
	}
	if _, ok := m["variants"]; !ok {
		t.Errorf("variants should be a top-level key; JSON was: %s", string(b))
	}
}

// ---------------------------------------------------------------------------
// Test 4 — WithRetryable(false) emits "retryable": false in JSON
// spec ref: §3.1 step 7 panic recovery: "retryable": false in SERVICE_DOWN envelope
// ---------------------------------------------------------------------------

func TestStructuredErrorRetryableEmitted(t *testing.T) {
	se := NewStructuredError(ErrCodeServiceDown, "down").WithRetryable(false)
	b, err := json.Marshal(se)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	// We must confirm the key is literally present (even with false value).
	// Use raw string search because standard Unmarshal drops false booleans
	// only if omitempty; we need to confirm the codec actually emits it.
	raw := string(b)
	if !strings.Contains(raw, `"retryable"`) {
		t.Errorf("expected key %q in JSON output; got: %s", "retryable", raw)
	}
	if !strings.Contains(raw, `false`) {
		t.Errorf("expected value false in JSON output; got: %s", raw)
	}
}

// ---------------------------------------------------------------------------
// Test 5 — implements error interface; errors.Is; IsStructuredError helper
// spec ref: §1421 (typed sentinel errors; replace ad-hoc fmt.Errorf)
// ---------------------------------------------------------------------------

func TestStructuredErrorImplementsErrorInterface(t *testing.T) {
	se := NewStructuredError(ErrCodeOpNotFound, "no such op")

	// Compile-time check: *StructuredError satisfies the error interface.
	var iface error = se
	// Exercise the Error() method through the interface — round-trips the
	// implementation rather than relying on a tautological nil check.
	if iface.Error() == "" {
		t.Fatal("*StructuredError.Error() returned empty string through error interface")
	}

	// IsStructuredError helper
	if !IsStructuredError(se, ErrCodeOpNotFound) {
		t.Error("IsStructuredError(se, ErrCodeOpNotFound) should be true")
	}
	if IsStructuredError(se, ErrCodeServiceDown) {
		t.Error("IsStructuredError(se, ErrCodeServiceDown) should be false for OP_NOT_FOUND error")
	}

	// errors.Is semantics — wrapped errors should still match
	wrapped := errors.Join(errors.New("outer context"), se)
	if !IsStructuredError(wrapped, ErrCodeOpNotFound) {
		t.Error("IsStructuredError should find StructuredError through errors.Join wrapping")
	}
}

// ---------------------------------------------------------------------------
// Test 6 — All 28 spec-stable error code string values match spec §1421
// spec ref: §1421 (full stable runtime error codes list)
// ---------------------------------------------------------------------------

func TestStructuredErrorAllSpecCodes(t *testing.T) {
	table := []struct {
		code    ErrorCode
		wantStr string
	}{
		{ErrCodeOpNotFound, "OP_NOT_FOUND"},
		{ErrCodeInvalidArgs, "INVALID_ARGS"},
		{ErrCodeAmbiguousVariant, "AMBIGUOUS_VARIANT"},
		{ErrCodeVariantNotFound, "VARIANT_NOT_FOUND"},
		{ErrCodeRiskToolMismatch, "RISK_TOOL_MISMATCH"},
		{ErrCodeRequiresConfirmation, "REQUIRES_CONFIRMATION"},
		{ErrCodeConfirmationTokenInvalid, "CONFIRMATION_TOKEN_INVALID"},
		{ErrCodeDestructiveBudgetExceeded, "DESTRUCTIVE_BUDGET_EXCEEDED"},
		{ErrCodeDestructiveScopeMismatch, "DESTRUCTIVE_SCOPE_MISMATCH"},
		{ErrCodeUnsupportedCapability, "UNSUPPORTED_CAPABILITY"},
		{ErrCodeRateLimited, "RATE_LIMITED"},
		{ErrCodeServiceDown, "SERVICE_DOWN"},
		{ErrCodeCancelled, "CANCELLED"},
		{ErrCodeAuthRequired, "AUTH_REQUIRED"},
		{ErrCodeScopeMissing, "SCOPE_MISSING"},
		{ErrCodeLROTimeout, "LRO_TIMEOUT"},
		{ErrCodeLROUnroutable, "LRO_UNROUTABLE"},
		{ErrCodeCodeOutputLimitExceeded, "CODE_OUTPUT_LIMIT_EXCEEDED"},
		{ErrCodeProjectRootRequired, "PROJECT_ROOT_REQUIRED"},
		{ErrCodeTeeSecretCorrupt, "TEE_SECRET_CORRUPT"},
		{ErrCodeResultArtifactExpired, "RESULT_ARTIFACT_EXPIRED"},
		{ErrCodeResourceNotFound, "RESOURCE_NOT_FOUND"},
		{ErrCodeGainDisabled, "GAIN_DISABLED"},
		{ErrCodeGainLedgerUnavailable, "GAIN_LEDGER_UNAVAILABLE"},
		{ErrCodeVariantQuarantined, "VARIANT_QUARANTINED"},
		{ErrCodeVariantDeprecated, "VARIANT_DEPRECATED"},
		{ErrCodeCLIArgDuplicate, "CLI_ARG_DUPLICATE"},
		{ErrCodeCLIArgInvalid, "CLI_ARG_INVALID"},
		{ErrCodePolicyDenied, "POLICY_DENIED"},
		{ErrCodeResponseTooLarge, "RESPONSE_TOO_LARGE"},
	}
	if len(table) != 30 {
		t.Fatalf("expected 30 error codes in table, got %d", len(table))
	}
	for _, tt := range table {
		if got := string(tt.code); got != tt.wantStr {
			t.Errorf("ErrorCode constant %q has string value %q; want %q", tt.wantStr, got, tt.wantStr)
		}
	}
}

// ---------------------------------------------------------------------------
// Test 7 — JSON canonical key order: error_code before message in output
// spec ref: §1421 (MCP error envelope shape; error_code is the discriminator field)
// ---------------------------------------------------------------------------

func TestStructuredErrorJSONCanonicalOrder(t *testing.T) {
	se := NewStructuredError(ErrCodeInvalidArgs, "bad input").
		WithDetail("missing", []string{"userId"}).
		WithRetryable(false)
	b, err := json.Marshal(se)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	raw := string(b)
	posCode := strings.Index(raw, `"error_code"`)
	posMsg := strings.Index(raw, `"message"`)
	if posCode == -1 {
		t.Fatal("key error_code not found in JSON output")
	}
	if posMsg == -1 {
		t.Fatal("key message not found in JSON output")
	}
	if posCode >= posMsg {
		t.Errorf("expected error_code (pos %d) to appear before message (pos %d) in: %s", posCode, posMsg, raw)
	}
}

// ---------------------------------------------------------------------------
// Test 8 — The literal "detail" key must NOT appear in JSON output
// spec ref: §1421 (detail fields are flattened; struct field tagged json:"-")
// ---------------------------------------------------------------------------

func TestStructuredErrorMarshalDoesNotIncludeDetailKey(t *testing.T) {
	se := NewStructuredError(ErrCodeInvalidArgs, "bad input").
		WithDetail("missing", []string{"userId"}).
		WithDetail("unknown", []string{"bogusField"})
	b, err := json.Marshal(se)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	raw := string(b)
	// The literal JSON key "detail" must not appear anywhere.
	if strings.Contains(raw, `"detail"`) {
		t.Errorf("JSON output must NOT contain key %q (detail fields must be flattened); got: %s", "detail", raw)
	}
}

// ---------------------------------------------------------------------------
// Test 9 — RISK_TOOL_MISMATCH envelope exact shape (spec §4.1 / §1421)
// spec ref: §4.1 line ~330: {"error_code":"RISK_TOOL_MISMATCH","op_id":"...","variant_id":"...",
//           "variant_risk_class":"...","required_tool":"gum.write"}
// ---------------------------------------------------------------------------

func TestStructuredErrorRiskToolMismatchEnvelope(t *testing.T) {
	se := NewStructuredError(ErrCodeRiskToolMismatch, "risk class mismatch").
		WithDetail("op_id", "gmail.users.messages.send").
		WithDetail("variant_id", "gmail.users.messages.send:default").
		WithDetail("variant_risk_class", "write").
		WithDetail("required_tool", "gum.write")

	b, err := json.Marshal(se)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	checkStr := func(key, want string) {
		t.Helper()
		v, ok := m[key]
		if !ok {
			t.Errorf("missing key %q in RISK_TOOL_MISMATCH envelope; JSON: %s", key, string(b))
			return
		}
		if got, ok2 := v.(string); !ok2 || got != want {
			t.Errorf("key %q = %v (%T); want string %q", key, v, v, want)
		}
	}

	checkStr("error_code", "RISK_TOOL_MISMATCH")
	checkStr("op_id", "gmail.users.messages.send")
	checkStr("variant_id", "gmail.users.messages.send:default")
	checkStr("variant_risk_class", "write")
	checkStr("required_tool", "gum.write")

	// Confirm no "detail" nesting
	if _, ok := m["detail"]; ok {
		t.Errorf("RISK_TOOL_MISMATCH envelope must not have a nested \"detail\" key; JSON: %s", string(b))
	}
}
