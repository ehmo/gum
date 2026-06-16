package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestNewAuthSetupCmdShape locks the argument validation gate: cobra
// must accept exactly one op_id positional, rejecting both zero and
// multiple. This is the only branch reachable without spawning the
// composite auth machinery.
func TestNewAuthSetupCmdShape(t *testing.T) {
	cmd := newAuthSetupCmd()
	if cmd.Use != "setup <op_id>" {
		t.Errorf("Use = %q", cmd.Use)
	}
	if err := cmd.Args(cmd, []string{}); err == nil {
		t.Error("Args accepted zero args; want rejection")
	}
	if err := cmd.Args(cmd, []string{"a", "b"}); err == nil {
		t.Error("Args accepted two args; want rejection")
	}
}

// TestNewAuthSetupCmdEmptyOpID locks the validation: a whitespace-only
// op_id must be rejected before any envelope construction.
func TestNewAuthSetupCmdEmptyOpID(t *testing.T) {
	cmd := newAuthSetupCmd()
	cmd.SetArgs([]string{"   "})
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for empty op_id")
	}
	if !strings.Contains(err.Error(), "<op_id> is empty") {
		t.Errorf("err=%q missing empty op_id hint", err)
	}
}

// TestNewAuthSetupCmdEmitsAuthEnvelope runs the happy path: a non-empty
// op_id produces a JSON envelope on stdout carrying AUTH_REQUIRED and
// the per-op setup command. The structured fields must be present so
// callers can route on them.
func TestNewAuthSetupCmdEmitsAuthEnvelope(t *testing.T) {
	cmd := newAuthSetupCmd()
	cmd.SetArgs([]string{"gmail.users.messages.list"})
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v; stderr=%q", err, stderr.String())
	}
	var env map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("Unmarshal: %v\nstdout=%s", err, stdout.String())
	}
	if env["error_code"] != "AUTH_REQUIRED" {
		t.Errorf("error_code = %v, want AUTH_REQUIRED", env["error_code"])
	}
	if env["op_id"] != "gmail.users.messages.list" {
		t.Errorf("op_id = %v, want gmail.users.messages.list", env["op_id"])
	}
	if setup, _ := env["setup_command"].(string); !strings.HasPrefix(setup, "gum auth setup ") {
		t.Errorf("setup_command = %v, want gum auth setup prefix", env["setup_command"])
	}
}
