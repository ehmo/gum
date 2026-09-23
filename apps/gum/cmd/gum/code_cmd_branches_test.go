package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestNewCodeCmdElevatedFlagsReachDispatch drives the elevated arm of
// newCodeCmd (--allow-write plus --allow-destructive, consented with --yes)
// and reads the outcome instead of discarding it. With no
// --destructive-budget the run must die on the §6.1.1 budget refusal, which
// proves the invocation reached the executor rather than failing earlier on an
// argument gum.code never declared.
func TestNewCodeCmdElevatedFlagsReachDispatch(t *testing.T) {
	cmd := newCodeCmd()
	cmd.SetArgs([]string{"1+1", "--allow-write", "--allow-destructive", "--yes"})
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err := cmd.Execute()
	out := stdout.String() + stderr.String()
	if err != nil {
		out += err.Error()
	}
	if !strings.Contains(out, "destructive_budget") {
		t.Fatalf("elevated run did not reach the destructive-budget gate: %q", out)
	}
	if strings.Contains(out, "unknown argument") {
		t.Fatalf("elevated run stamped an argument gum.code does not declare: %q", out)
	}
}

// TestNewCodeCmdReadOnlyRunNeedsNoConsent pins the inverse arm: neither
// capability flag set means no confirmation gate, so the script runs.
func TestNewCodeCmdReadOnlyRunNeedsNoConsent(t *testing.T) {
	cmd := newCodeCmd()
	cmd.SetArgs([]string{`gum_print("read-only-ok")`})
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("read-only run failed: %v\nstderr: %s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "read-only-ok") {
		t.Fatalf("stdout = %q, want the script output", stdout.String())
	}
}
