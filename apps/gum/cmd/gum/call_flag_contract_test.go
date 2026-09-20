// Spec §12.0 flag contract for `gum call`. Both flags below were declared,
// documented in --help, and then had no effect on the invocation:
//
//   - --raw was read into a variable and discarded with `_ = raw`, so every
//     --raw call was shaped by the active expression profile anyway.
//   - --no-field-mask suppressed only the --fields flag. A mask supplied
//     positionally (`fields=...`) still reached the wire, so the flag did not
//     skip stage 1 of §9.1 as documented.

package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/dispatch"
)

// callWithCapture runs `gum call` with args against a capturing dispatcher and
// returns the recorded invocation plus the Execute error.
func callWithCapture(t *testing.T, args ...string) (*dispatch.Invocation, error) {
	t.Helper()
	cap := &capturingDispatcher{}
	orig := newCallDispatcher
	t.Cleanup(func() { newCallDispatcher = orig })
	newCallDispatcher = func(string) dispatch.Dispatcher { return cap }

	cmd := newCallCmd()
	cmd.PersistentFlags().String("profile", "", "")
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	err := cmd.Execute()
	return cap.inv, err
}

// TestCallRawSetsRawDispatchFormat pins the §12.0 --raw contract: the kernel
// implements the shaping bypass when Invocation.Format == "raw", so the flag
// must reach it.
func TestCallRawSetsRawDispatchFormat(t *testing.T) {
	inv, err := callWithCapture(t,
		"gmail.users.messages.list", "--risk=read", "--raw", "userId=me")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if inv == nil {
		t.Fatal("dispatcher was not called")
	}
	if inv.Format != "raw" {
		t.Errorf("inv.Format = %q; want %q (--raw must bypass shaping)", inv.Format, "raw")
	}
}

// TestCallRawRejectsConflictingFormat pins the composition rule: --raw returns
// the upstream JSON verbatim, so it cannot also render TOON or a table. A
// silent win for either side would hide which one the caller got.
func TestCallRawRejectsConflictingFormat(t *testing.T) {
	for _, format := range []string{"toon", "table", "csv", "markdown"} {
		t.Run(format, func(t *testing.T) {
			_, err := callWithCapture(t,
				"gmail.users.messages.list", "--risk=read", "--raw",
				"--output", format, "userId=me")
			if err == nil {
				t.Fatalf("--raw --output %s was accepted; want CLI_ARG_INVALID", format)
			}
			if !strings.Contains(err.Error(), "--raw") {
				t.Errorf("err = %v; want a message naming --raw", err)
			}
		})
	}
}

// TestCallRawAllowsJSONFormat pins the other half: --raw output IS JSON, so
// --raw --output json is not a conflict.
func TestCallRawAllowsJSONFormat(t *testing.T) {
	inv, err := callWithCapture(t,
		"gmail.users.messages.list", "--risk=read", "--raw", "--output", "json", "userId=me")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if inv == nil || inv.Format != "raw" {
		t.Fatalf("inv = %#v; want Format=raw", inv)
	}
}

// TestCallNoFieldMaskDropsPositionalFields pins the §12.0 --no-field-mask
// contract: it skips stage 1 (upstream projection). The mask travels as the
// universal `fields` arg whether it came from --fields or a positional, so
// both forms must be gone from the invocation.
func TestCallNoFieldMaskDropsPositionalFields(t *testing.T) {
	inv, err := callWithCapture(t,
		"gmail.users.messages.list", "--risk=read", "--no-field-mask",
		"userId=me", "fields=messages(id)")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if inv == nil {
		t.Fatal("dispatcher was not called")
	}
	if v, present := inv.Args["fields"]; present {
		t.Errorf("args[fields] = %#v; want absent (--no-field-mask skips stage 1)", v)
	}
}

// TestCallNoFieldMaskRejectsExplicitFields pins the contradiction: --fields
// asks for a mask and --no-field-mask asks for none. Dropping the mask without
// a word left the caller believing the narrow projection was sent.
func TestCallNoFieldMaskRejectsExplicitFields(t *testing.T) {
	_, err := callWithCapture(t,
		"gmail.users.messages.list", "--risk=read",
		"--no-field-mask", "--fields", "messages(id)", "userId=me")
	if err == nil {
		t.Fatal("--fields --no-field-mask was accepted; want CLI_ARG_INVALID")
	}
	if !strings.Contains(err.Error(), "--no-field-mask") {
		t.Errorf("err = %v; want a message naming --no-field-mask", err)
	}
}
