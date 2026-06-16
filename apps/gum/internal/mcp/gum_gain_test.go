// Package mcp — Red team failing tests for gum-9vuq.9.
//
// Covers: gum.gain handler GainResult envelope (9 required top-level keys per spec §2793),
// tokenizer header (spec §2866), default mode (spec §2518), terminal error envelopes for
// GAIN_DISABLED (spec §2570) and GAIN_LEDGER_UNAVAILABLE (spec §2541).
//
// These tests MUST FAIL today because:
//   - handleGain returns {total_calls, total_tokens_saved, mean_savings_per_call, p50, p95, p99}
//     instead of the 9-key GainResult shape.
//   - No GAIN_DISABLED or GAIN_LEDGER_UNAVAILABLE terminal error envelopes are emitted.
package mcp

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// makeGainRequest returns a no-arg CallToolRequest for gum.gain.
func makeGainRequest() *sdkmcp.CallToolRequest {
	return &sdkmcp.CallToolRequest{
		Params: &sdkmcp.CallToolParamsRaw{
			Name:      "gum.gain",
			Arguments: json.RawMessage(`{}`),
		},
	}
}

// invokeGainExpectSuccess calls handleGain and asserts IsError==false.
// Returns the parsed top-level JSON map.
func invokeGainExpectSuccess(t *testing.T, srv *Server) map[string]any {
	t.Helper()
	res, err := srv.handleGain(context.Background(), makeGainRequest())
	if err != nil {
		t.Fatalf("handleGain returned Go error: %v", err)
	}
	if res == nil {
		t.Fatal("handleGain returned nil result")
	}
	if res.IsError {
		var text string
		if len(res.Content) > 0 {
			if tc, ok := res.Content[0].(*sdkmcp.TextContent); ok {
				text = tc.Text
			}
		}
		t.Fatalf("handleGain returned unexpected error result: %s", text)
	}
	if len(res.Content) == 0 {
		t.Fatal("handleGain returned empty content")
	}
	tc, ok := res.Content[0].(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("content[0] is not TextContent; got %T", res.Content[0])
	}
	var m map[string]any
	if jerr := json.Unmarshal([]byte(tc.Text), &m); jerr != nil {
		t.Fatalf("handleGain result is not JSON: %v; text: %s", jerr, tc.Text)
	}
	return m
}

// invokeGainExpectError calls handleGain and asserts IsError==true.
// Returns the parsed JSON body from the error text content.
func invokeGainExpectError(t *testing.T, srv *Server) map[string]any {
	t.Helper()
	res, err := srv.handleGain(context.Background(), makeGainRequest())
	if err != nil {
		t.Fatalf("handleGain returned Go error: %v", err)
	}
	if res == nil {
		t.Fatal("handleGain returned nil result")
	}
	if !res.IsError {
		// Extract body for debugging.
		var text string
		if len(res.Content) > 0 {
			if tc, ok := res.Content[0].(*sdkmcp.TextContent); ok {
				text = tc.Text
			}
		}
		t.Fatalf("handleGain should have returned IsError=true but got success result: %s", text)
	}
	if len(res.Content) == 0 {
		t.Fatal("handleGain returned empty content on error path")
	}
	tc, ok := res.Content[0].(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("error content[0] is not TextContent; got %T", res.Content[0])
	}
	var m map[string]any
	if jerr := json.Unmarshal([]byte(tc.Text), &m); jerr != nil {
		t.Fatalf("handleGain error body is not JSON: %v; text: %s", jerr, tc.Text)
	}
	return m
}

// ---------------------------------------------------------------------------
// Test 1: Success envelope has all 9 required GainResult top-level keys (spec §2793)
// ---------------------------------------------------------------------------

// TestGainSuccessEnvelopeHasAllRequiredKeys invokes handleGain against a fresh ledger
// in a temp HOME and asserts all 9 spec §2793 top-level keys are present:
// mode, window, baseline_tokens, actual_tokens, savings_tokens,
// savings_pct, end_to_end_savings, batch_envelope_overhead, tokenizer.
//
// Current handler emits {total_calls, total_tokens_saved, mean_savings_per_call,
// p50, p95, p99} — 0 of 9 required keys present.  MUST FAIL.
func TestGainSuccessEnvelopeHasAllRequiredKeys(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	srv := NewServer(noopDispatcher{})
	m := invokeGainExpectSuccess(t, srv)

	required := []string{
		"mode",
		"window",
		"baseline_tokens",
		"actual_tokens",
		"savings_tokens",
		"savings_pct",
		"end_to_end_savings",
		"batch_envelope_overhead",
		"tokenizer",
	}
	for _, key := range required {
		if _, ok := m[key]; !ok {
			t.Errorf("missing required GainResult key %q (spec §2793)", key)
		}
	}
}

// ---------------------------------------------------------------------------
// Test 2: tokenizer field equals "cl100k_base" (spec §2866)
// ---------------------------------------------------------------------------

// TestGainSuccessTokenizerHeader asserts result["tokenizer"] == "cl100k_base".
// Current handler has no "tokenizer" field.  MUST FAIL.
func TestGainSuccessTokenizerHeader(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	srv := NewServer(noopDispatcher{})
	m := invokeGainExpectSuccess(t, srv)

	raw, ok := m["tokenizer"]
	if !ok {
		t.Fatal("missing field \"tokenizer\" (spec §2866)")
	}
	got, ok := raw.(string)
	if !ok {
		t.Fatalf("tokenizer is %T; want string", raw)
	}
	if got != "cl100k_base" {
		t.Errorf("tokenizer = %q; want \"cl100k_base\" (spec §2866)", got)
	}
}

// ---------------------------------------------------------------------------
// Test 3: Default mode is "summary" (spec §2518)
// ---------------------------------------------------------------------------

// TestGainSuccessModeIsSummary asserts that a default v0.1.0 invocation sets
// result["mode"] == "summary" per spec §2518.
// Current handler has no "mode" field.  MUST FAIL.
func TestGainSuccessModeIsSummary(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	srv := NewServer(noopDispatcher{})
	m := invokeGainExpectSuccess(t, srv)

	raw, ok := m["mode"]
	if !ok {
		t.Fatal("missing field \"mode\" (spec §2518)")
	}
	got, ok := raw.(string)
	if !ok {
		t.Fatalf("mode is %T; want string", raw)
	}
	if got != "summary" {
		t.Errorf("mode = %q; want \"summary\" for v0.1.0 default invocation (spec §2518)", got)
	}
}

// ---------------------------------------------------------------------------
// Test 4: GAIN_DISABLED terminal error (spec §2570)
// ---------------------------------------------------------------------------

// TestGainDisabledReturnsTerminalError asserts that when gain is disabled
// (signalled via env var GUM_GAIN_DISABLED=1), handleGain returns IsError=true
// with error_code == "GAIN_DISABLED", and the body contains no "mode" key
// (i.e. it is not a GainResult success shape).
//
// Green should check os.Getenv("GUM_GAIN_DISABLED") == "1" (or a Server field)
// at the top of handleGain and return jsonErrorResult(map[string]any{
//   "error_code": "GAIN_DISABLED",
// }) immediately.
//
// Current handler never checks this env var and always proceeds to open the ledger.
// MUST FAIL.
func TestGainDisabledReturnsTerminalError(t *testing.T) {
	t.Setenv("GUM_GAIN_DISABLED", "1")
	t.Setenv("HOME", t.TempDir())

	srv := NewServer(noopDispatcher{})
	m := invokeGainExpectError(t, srv)

	code, _ := m["error_code"].(string)
	if code != "GAIN_DISABLED" {
		t.Errorf("error_code = %q; want \"GAIN_DISABLED\" (spec §2570)", code)
	}

	// Must NOT contain the "mode" field — this is not a GainResult success envelope.
	if _, hasMode := m["mode"]; hasMode {
		t.Error("error envelope must not contain \"mode\" field (spec §1421: terminal errors do not use GainResult schema)")
	}
}

// ---------------------------------------------------------------------------
// Test 5: GAIN_LEDGER_UNAVAILABLE terminal error (spec §2541)
// ---------------------------------------------------------------------------

// TestGainLedgerUnavailableReturnsTerminalError asserts that when the ledger
// path is unwritable/unusable, handleGain returns IsError=true with
// error_code == "GAIN_LEDGER_UNAVAILABLE" and a non-empty "hint" field.
//
// Green must distinguish "file/dir didn't exist yet (OK, fresh ledger)" from a
// real failure such as permission denied.  We trigger the failure by setting HOME
// to a path that is a regular file, so MkdirAll inside NewLedger will fail with
// ENOTDIR.
//
// Current handler returns a plain string "GAIN_LEDGER_OPEN_FAILED: …" without
// IsError=true and without the spec-mandated JSON envelope.  MUST FAIL.
func TestGainLedgerUnavailableReturnsTerminalError(t *testing.T) {
	// Create a regular file and use it as HOME so that
	// ~/.local/share/gum/ cannot be created (MkdirAll will fail with ENOTDIR).
	fakeHome := t.TempDir()
	blockerPath := fakeHome + "/fake-home-file"
	if err := os.WriteFile(blockerPath, []byte("not a dir"), 0o444); err != nil {
		t.Fatalf("setup: write blocker file: %v", err)
	}
	t.Setenv("HOME", blockerPath)

	srv := NewServer(noopDispatcher{})
	m := invokeGainExpectError(t, srv)

	code, _ := m["error_code"].(string)
	if code != "GAIN_LEDGER_UNAVAILABLE" {
		t.Errorf("error_code = %q; want \"GAIN_LEDGER_UNAVAILABLE\" (spec §2541)", code)
	}

	hint, _ := m["hint"].(string)
	if hint == "" {
		t.Error("\"hint\" must be present and non-empty (spec §2541)")
	}
}
