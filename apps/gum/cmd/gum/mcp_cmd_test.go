package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestNewMCPCmdRequiresStdio verifies the cobra wiring: invoking `gum mcp`
// without --stdio errors with a clear remediation, never bringing up the
// MCP server. v0.1.0 has no other transports and the user-facing message
// must say so.
func TestNewMCPCmdRequiresStdio(t *testing.T) {
	cmd := newMCPCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when --stdio is omitted")
	}
	if !strings.Contains(err.Error(), "--stdio") {
		t.Errorf("err=%q missing --stdio hint", err)
	}
}

// TestNewMCPCmdShortLongHelp pins the surface advertised in --help so
// tooling that scrapes the cobra help text (homebrew completions, docs
// site) doesn't silently drift.
func TestNewMCPCmdShortLongHelp(t *testing.T) {
	cmd := newMCPCmd()
	if cmd.Use != "mcp" {
		t.Errorf("Use = %q, want mcp", cmd.Use)
	}
	if !strings.Contains(cmd.Short, "MCP") {
		t.Errorf("Short = %q, missing MCP", cmd.Short)
	}
	if !strings.Contains(cmd.Long, "--stdio") {
		t.Errorf("Long = %q, missing --stdio mention", cmd.Long)
	}
}
