// Spec §12.4 contract for `gum call --unsanitized`: the flag reaches the
// kernel, it always prints the warning, and outside an interactive session it
// needs --yes-unsanitized. The guard is the point of the flag — an agent that
// inherited it from a shell alias would feed raw upstream error text to a
// model, which is the case §11 layer 2 exists to cover.

package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/dispatch"
)

// callPiped runs `gum call` with a non-TTY stdin, the shape a script or an
// agent produces, and returns the recorded invocation, stderr and the error.
func callPiped(t *testing.T, args ...string) (*dispatch.Invocation, string, error) {
	t.Helper()
	cap := &capturingDispatcher{}
	orig := newCallDispatcher
	t.Cleanup(func() { newCallDispatcher = orig })
	newCallDispatcher = func(string) dispatch.Dispatcher { return cap }

	var stderr bytes.Buffer
	cmd := newCallCmd()
	cmd.PersistentFlags().String("profile", "", "")
	cmd.SetIn(bytes.NewReader(nil))
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return cap.inv, stderr.String(), err
}

func TestCallUnsanitizedRequiresConfirmationWhenPiped(t *testing.T) {
	inv, _, err := callPiped(t,
		"gmail.users.messages.list", "--risk=read", "--unsanitized", "userId=me")
	if err == nil {
		t.Fatal("--unsanitized was accepted on a non-TTY stdin; want CLI_ARG_INVALID")
	}
	if !strings.Contains(err.Error(), "--yes-unsanitized") {
		t.Errorf("err = %v; want a message naming --yes-unsanitized", err)
	}
	if inv != nil {
		t.Error("the dispatcher ran; the guard must fail before any upstream call")
	}
}

func TestCallUnsanitizedConfirmedReachesKernel(t *testing.T) {
	inv, stderr, err := callPiped(t,
		"gmail.users.messages.list", "--risk=read", "--unsanitized", "--yes-unsanitized", "userId=me")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if inv == nil {
		t.Fatal("dispatcher was not called")
	}
	if !inv.SkipErrorSanitizer {
		t.Error("inv.SkipErrorSanitizer = false; --unsanitized must reach the kernel")
	}
	if !strings.Contains(stderr, unsanitizedWarning) {
		t.Errorf("stderr = %q; want the §12.4 warning", stderr)
	}
}

func TestCallWithoutUnsanitizedKeepsScrubberOn(t *testing.T) {
	inv, stderr, err := callPiped(t,
		"gmail.users.messages.list", "--risk=read", "userId=me")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if inv == nil {
		t.Fatal("dispatcher was not called")
	}
	if inv.SkipErrorSanitizer {
		t.Error("inv.SkipErrorSanitizer = true without --unsanitized")
	}
	if strings.Contains(stderr, "unsanitized") {
		t.Errorf("stderr = %q; want no bypass warning on an ordinary call", stderr)
	}
}
