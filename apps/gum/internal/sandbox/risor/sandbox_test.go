package risor_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	risorlib "github.com/deepnoodle-ai/risor/v2"
	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/sandbox/risor"
)

func TestRisorRunPrintsToBuffer(t *testing.T) {
	defer goleak.VerifyNone(t)

	// gum_print must be injected; the sandbox exposes it as a global.
	var printed []byte
	opts := risor.Options{
		Globals: map[string]any{
			// The green team must wire gum_print into the Risor env.
			// This test supplies a Go func so the sandbox can call it.
			"gum_print": func(s string) {
				printed = append(printed, s...)
			},
		},
	}
	out, err := risor.Run(context.Background(), `gum_print("hi")`, opts)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
	got := strings.TrimSpace(string(out.Printed))
	if got != "hi" {
		t.Errorf("expected Printed to contain %q (trimmed), got %q", "hi", got)
	}
	_ = printed // silence unused warning; assertion is on out.Printed
}

func TestRisorRunCancelHonored(t *testing.T) {
	defer goleak.VerifyNone(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled before invocation

	_, err := risor.Run(ctx, `gum_print("should not run")`, risor.Options{})
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "cancel") && !strings.Contains(msg, "deadline") {
		t.Errorf("expected error to mention 'cancel' or 'deadline', got: %v", err)
	}
}

// TestRisorRunStepLimitExceeded pins the CPU-exhaustion guard (gum-vmwq): a
// script that executes more VM instructions than Options.MaxSteps must abort
// with the typed risor.ErrStepLimitExceeded sentinel, NOT a generic eval error
// and NOT a wall-clock timeout. Without a step ceiling a tight loop pins a core
// for the full ScriptTimeout window. Risor v2 has no for/while keyword, so the
// loop is built from the stdlib (list/range/each) injected via Globals — the
// same proven step-consumer Risor's own resource-limit test uses.
func TestRisorRunStepLimitExceeded(t *testing.T) {
	defer goleak.VerifyNone(t)

	src := `let sum = 0; list(range(100000)).each(function(i) { sum = sum + i }); sum`
	opts := risor.Options{
		MaxSteps: 5000, // step check fires every 1000 instr; 100k iterations blow past this
		Globals:  risorlib.Builtins(),
	}
	_, err := risor.Run(context.Background(), src, opts)
	if err == nil {
		t.Fatal("expected step-limit error, got nil")
	}
	if !errors.Is(err, risor.ErrStepLimitExceeded) {
		t.Fatalf("error = %v, want errors.Is(err, risor.ErrStepLimitExceeded)", err)
	}
}

// TestRisorRunStepLimitNotExceeded is the negative control: a small script well
// under the step budget runs to completion and returns its value.
func TestRisorRunStepLimitNotExceeded(t *testing.T) {
	defer goleak.VerifyNone(t)

	src := `let sum = 0; list(range(10)).each(function(i) { sum = sum + i }); sum`
	opts := risor.Options{
		MaxSteps: 1_000_000,
		Globals:  risorlib.Builtins(),
	}
	out, err := risor.Run(context.Background(), src, opts)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
	if got, ok := out.Value.(int); !ok || got != 45 {
		t.Errorf("Value = %v (%T), want int 45", out.Value, out.Value)
	}
}

// TestGumPrintUtf8Boundary verifies that the PrintByteCap truncation lands on
// a UTF-8 boundary.  spec.md §14 / docs/test-matrix.md TestGumPrintUtf8Boundary.
//
// Strategy: "é" (U+00E9) is 2 bytes in UTF-8. 33000 × "é" = 66000 bytes.
// With PrintByteCap=65000, the cap must cut at a 2-byte boundary (even offset),
// producing valid UTF-8 of at most 65000 bytes.
func TestGumPrintUtf8Boundary(t *testing.T) {
	defer goleak.VerifyNone(t)

	const cap = 65000
	source_str := strings.Repeat("é", 33000) // 66000 bytes
	opts := risor.Options{
		PrintByteCap: cap,
		Globals: map[string]any{
			"gum_print": func(s string) {},
		},
	}
	code := `gum_print("` + source_str + `")`
	out, err := risor.Run(context.Background(), code, opts)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
	if len(out.Printed) > cap {
		t.Errorf("Printed length %d exceeds cap %d", len(out.Printed), cap)
	}
	if !utf8.ValidString(string(out.Printed)) {
		t.Error("Printed bytes are not valid UTF-8 after cap truncation")
	}
}
