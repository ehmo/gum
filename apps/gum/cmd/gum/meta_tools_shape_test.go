package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestNewReadCmdShape pins the cobra surface: --args + --format flags
// with empty defaults; ExactArgs(1) gate; non-empty Short.
func TestNewReadCmdShape(t *testing.T) {
	cmd := newReadCmd()
	if cmd.Use != "read <op_id>" {
		t.Errorf("Use=%q", cmd.Use)
	}
	for _, name := range []string{"args", "format"} {
		f := cmd.Flags().Lookup(name)
		if f == nil {
			t.Errorf("flag %q missing", name)
			continue
		}
		if f.DefValue != "" {
			t.Errorf("flag %q default=%q; want empty", name, f.DefValue)
		}
	}
	if err := cmd.Args(cmd, nil); err == nil {
		t.Error("accepted zero args")
	}
}

// TestNewReadCmdInvalidArgsJSONSurfaces exercises the parseArgsJSON
// error branch via RunE: an unparseable --args must return the wrapped
// `--args must be a JSON object` error before any dispatch happens.
func TestNewReadCmdInvalidArgsJSONSurfaces(t *testing.T) {
	cmd := newReadCmd()
	cmd.SetArgs([]string{"gum.list_messages", "--args", "not-json"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid --args JSON")
	}
	if !strings.Contains(err.Error(), "--args") {
		t.Errorf("err=%q; want --args hint", err)
	}
}

// TestNewWriteCmdShape: --args, --format, --allow-write all registered;
// --allow-write defaults false so the policy gate stays closed-by-default.
func TestNewWriteCmdShape(t *testing.T) {
	cmd := newWriteCmd()
	if cmd.Use != "write <op_id>" {
		t.Errorf("Use=%q", cmd.Use)
	}
	for _, want := range []struct{ name, def string }{
		{"args", ""},
		{"format", ""},
		{"allow-write", "false"},
	} {
		f := cmd.Flags().Lookup(want.name)
		if f == nil {
			t.Errorf("flag %q missing", want.name)
			continue
		}
		if f.DefValue != want.def {
			t.Errorf("flag %q default=%q; want %q", want.name, f.DefValue, want.def)
		}
	}
}

// TestNewWriteCmdInvalidArgsJSONSurfaces exercises the parseArgsJSON
// error branch through write's RunE (the same parseArgsJSON wrapper).
func TestNewWriteCmdInvalidArgsJSONSurfaces(t *testing.T) {
	cmd := newWriteCmd()
	cmd.SetArgs([]string{"gum.send_message", "--args", "not-json"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid --args JSON")
	}
}

// TestNewDestructiveCmdShape: --args, --format, --confirmed, --token
// all wired with safe defaults (confirmed=false, token="") so a
// missing-token destructive call refuses cleanly.
func TestNewDestructiveCmdShape(t *testing.T) {
	cmd := newDestructiveCmd()
	if cmd.Use != "destructive <op_id>" {
		t.Errorf("Use=%q", cmd.Use)
	}
	for _, want := range []struct{ name, def string }{
		{"args", ""},
		{"format", ""},
		{"confirmed", "false"},
		{"token", ""},
	} {
		f := cmd.Flags().Lookup(want.name)
		if f == nil {
			t.Errorf("flag %q missing", want.name)
			continue
		}
		if f.DefValue != want.def {
			t.Errorf("flag %q default=%q; want %q", want.name, f.DefValue, want.def)
		}
	}
}

// TestNewDestructiveCmdInvalidArgsJSONSurfaces drives the parseArgsJSON
// error branch for the destructive path too — keeps the three meta-tool
// commands in lockstep on argument validation.
func TestNewDestructiveCmdInvalidArgsJSONSurfaces(t *testing.T) {
	cmd := newDestructiveCmd()
	cmd.SetArgs([]string{"gum.delete_message", "--args", "not-json", "--confirmed", "--token", "x"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid --args JSON")
	}
}

// TestNewSearchCmdShape: --top-k defaults to 10 (matches the spec
// example shapes), --format empty (TTY-aware), MinimumNArgs(1).
func TestNewSearchCmdShape(t *testing.T) {
	cmd := newSearchCmd()
	if cmd.Use != "search <query>" {
		t.Errorf("Use=%q", cmd.Use)
	}
	f := cmd.Flags().Lookup("top-k")
	if f == nil {
		t.Fatal("--top-k flag missing")
	}
	if f.DefValue != "10" {
		t.Errorf("top-k default=%q; want 10", f.DefValue)
	}
	if err := cmd.Args(cmd, nil); err == nil {
		t.Error("MinimumNArgs(1) accepted zero")
	}
}

// TestNewSearchCmdHappyPathProducesJSON drives the JSON branch of
// newSearchCmd end-to-end: a query against the embedded catalog with
// --format=json must emit a parseable {"results": [...]} envelope.
func TestNewSearchCmdHappyPathProducesJSON(t *testing.T) {
	cmd := newSearchCmd()
	cmd.SetArgs([]string{"gmail", "--format", "json"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), `"results"`) {
		t.Errorf("output missing results field:\n%s", out.String())
	}
}

// TestNewDescribeCmdShape: ExactArgs(1) gate is what callers rely on;
// the actual op_id lookup lives downstream.
func TestNewDescribeCmdShape(t *testing.T) {
	cmd := newDescribeCmd()
	if cmd.Use != "describe <op_id>" {
		t.Errorf("Use=%q", cmd.Use)
	}
	if err := cmd.Args(cmd, nil); err == nil {
		t.Error("ExactArgs(1) accepted zero")
	}
	if err := cmd.Args(cmd, []string{"a", "b"}); err == nil {
		t.Error("ExactArgs(1) accepted two")
	}
}

// TestNewDescribeCmdUnknownOpReturnsNotFound drives the OP_NOT_FOUND
// branch via Execute (loadCatalog returns the embedded snapshot; the
// arg won't match anything in it).
func TestNewDescribeCmdUnknownOpReturnsNotFound(t *testing.T) {
	cmd := newDescribeCmd()
	cmd.SetArgs([]string{"definitely.not.a.real.op_id"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected OP_NOT_FOUND error")
	}
	if !strings.Contains(err.Error(), "OP_NOT_FOUND") {
		t.Errorf("err=%q; want OP_NOT_FOUND", err)
	}
}
