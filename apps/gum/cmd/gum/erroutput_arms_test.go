package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/dispatch"
)

// TestScopeMissingPrefersTheAuthEnvelope pins spec §7 lines 1378-1381: when the
// failure carries its own setup_command, that command wins over the canned
// "re-authenticate with the missing scopes" text.
func TestScopeMissingPrefersTheAuthEnvelope(t *testing.T) {
	se := &dispatch.StructuredError{
		ErrCode: dispatch.ErrCodeScopeMissing,
		Message: "scope missing",
		Detail: map[string]any{
			"setup_command":  "gum auth use-service-account --file sa.json",
			"scopes_missing": []any{"https://www.googleapis.com/auth/gmail.readonly"},
		},
	}
	got := howToFix(se, nil)
	if !strings.Contains(got, "gum auth use-service-account") {
		t.Errorf("hint %q does not carry the envelope's setup_command", got)
	}
	if strings.Contains(got, "gum auth login --scopes") {
		t.Errorf("hint %q fell through to the canned scopes text", got)
	}
}

// TestDetailStringsReadsAJSONRoundTrip pins the []any arm. missing_components
// arrives as []string from internal/auth and as []any through MCP, and both
// have to produce the same list. Non-strings and empty strings are dropped.
func TestDetailStringsReadsAJSONRoundTrip(t *testing.T) {
	detail := map[string]any{
		"missing_components": []any{"client_id", "", 42, "client_secret", nil},
	}
	got := detailStrings(detail, "missing_components")
	want := []string{"client_id", "client_secret"}
	if len(got) != len(want) {
		t.Fatalf("detailStrings = %v; want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("detailStrings[%d] = %q; want %q", i, got[i], want[i])
		}
	}
}

// TestConfirmationHintLabelNamesTheWritePurpose pins the write label. A write
// confirmation must not be described as destructive.
func TestConfirmationHintLabelNamesTheWritePurpose(t *testing.T) {
	se := &dispatch.StructuredError{
		ErrCode: dispatch.ErrCodeRequiresConfirmation,
		Detail:  map[string]any{"confirmation_purpose": dispatch.ConfirmationPurposeWrite},
	}
	if got := confirmationHintLabel(se, nil); got != "Write op" {
		t.Errorf("confirmationHintLabel = %q; want %q", got, "Write op")
	}
}

// TestRenderStructuredEnvelopeSurfacesAMarshalFailure pins the defensive guard
// on the encode. extras is caller-supplied, so a value JSON cannot represent
// must return an error rather than emit a truncated envelope.
func TestRenderStructuredEnvelopeSurfacesAMarshalFailure(t *testing.T) {
	se := &dispatch.StructuredError{ErrCode: dispatch.ErrCodeInvalidArgs, Message: "bad"}
	var buf bytes.Buffer
	err := renderStructuredEnvelope(&buf, se, map[string]any{"bad": make(chan int)})
	if err == nil {
		t.Fatalf("want a marshal error, got nil; wrote %q", buf.String())
	}
	if buf.Len() != 0 {
		t.Errorf("a failed marshal still wrote %q", buf.String())
	}
}

// TestRenderStructuredEnvelopeSurfacesAWriteFailure pins the write arm: a
// closed stdout has to propagate, not be swallowed into a silent success.
func TestRenderStructuredEnvelopeSurfacesAWriteFailure(t *testing.T) {
	se := &dispatch.StructuredError{ErrCode: dispatch.ErrCodeInvalidArgs, Message: "bad"}
	if err := renderStructuredEnvelope(failWriter{}, se, nil); err == nil {
		t.Error("want the stdout write failure, got nil")
	}
}
