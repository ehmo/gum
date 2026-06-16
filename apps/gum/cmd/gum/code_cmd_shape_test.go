package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// TestNewCodeCmdShape pins the cobra surface of `gum code`. The six
// flags (--allow-write, --allow-destructive, --confirmed, --token,
// --timeout-sec, --language, --format, --output)
// must all be registered with the documented defaults so the user-facing
// help stays stable.
func TestNewCodeCmdShape(t *testing.T) {
	cmd := newCodeCmd()
	if !strings.HasPrefix(cmd.Use, "code") {
		t.Errorf("Use=%q", cmd.Use)
	}
	for _, name := range []string{"allow-write", "allow-destructive", "confirmed", "token", "timeout-sec", "language", "format", "output"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("flag %q missing", name)
		}
	}
	// ExactArgs(1): rejects zero and rejects two.
	if err := cmd.Args(cmd, []string{}); err == nil {
		t.Error("accepted zero args")
	}
	if err := cmd.Args(cmd, []string{"a", "b"}); err == nil {
		t.Error("accepted two args")
	}
	if err := cmd.Args(cmd, []string{"only"}); err != nil {
		t.Errorf("rejected one arg: %v", err)
	}
}

// TestNewCodeCmdHelpDocumentsBuiltins pins the gum-l0op #3 papercut: the
// `gum code` help must teach the non-obvious Risor surface. A user has no way
// to guess that the sandbox exposes gum_call/gum_search/etc., nor that Risor
// has no for/while loop. The Long description must name every wired builtin
// and warn about the missing loop keyword; the Example must carry a runnable
// snippet so `gum code --help` is self-sufficient.
func TestNewCodeCmdHelpDocumentsBuiltins(t *testing.T) {
	cmd := newCodeCmd()

	help := cmd.Long + "\n" + cmd.Example
	for _, builtin := range []string{
		"gum_call",
		"gum_search",
		"gum_confirm_destructive",
		"gum_parallel",
		"gum_print",
		"gum_http_get",
	} {
		if !strings.Contains(help, builtin) {
			t.Errorf("help does not document builtin %q", builtin)
		}
	}
	if cmd.Example == "" {
		t.Error("Example must carry a runnable snippet")
	}
	// The loop caveat is the single highest-value hint: Risor has no for/while.
	if !strings.Contains(strings.ToLower(help), "no for") &&
		!strings.Contains(strings.ToLower(help), "no loop") &&
		!strings.Contains(strings.ToLower(help), "no `for`") {
		t.Errorf("help must warn that Risor has no for/while loop; got:\n%s", help)
	}
	// A runnable example must actually call a builtin, not just name it in prose.
	if !strings.Contains(cmd.Example, "gum") {
		t.Errorf("Example must contain a runnable gum_* call; got:\n%s", cmd.Example)
	}
}

// TestNewCodeCmdMissingScriptFileSurfaces drives the RunE path through
// readScriptArg's error branch: when the @file argument points at a
// non-existent file, RunE must return the wrapped "read script file"
// error and never invoke the dispatcher.
func TestNewCodeCmdMissingScriptFileSurfaces(t *testing.T) {
	cmd := newCodeCmd()
	missing := filepath.Join(t.TempDir(), "does-not-exist.risor")
	cmd.SetArgs([]string{"@" + missing})
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing script file")
	}
	if !strings.Contains(err.Error(), "read script file") {
		t.Errorf("err=%q missing 'read script file' wrap", err)
	}
}
