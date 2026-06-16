package main

import (
	"bytes"
	"os"
	"testing"
)

// TestResolveOutputFormatExplicitFlagWins pins the highest-priority
// branch: when --format is set, the value is used verbatim regardless of
// the writer type. This keeps `gum status --format=json | jq` reliable
// even on a TTY.
func TestResolveOutputFormatExplicitFlagWins(t *testing.T) {
	for _, name := range []string{"json", "table", "toon", "raw", "anything"} {
		if got := resolveOutputFormat(name, os.Stdout); got != name {
			t.Errorf("got %q; want %q (explicit flag must win)", got, name)
		}
	}
}

// TestResolveOutputFormatNonTTYDefaultsJSON pins the non-TTY branch:
// any non-*os.File writer (bytes.Buffer, pipes, /dev/null) gets JSON so
// scripted callers keep parsing the output uniformly.
func TestResolveOutputFormatNonTTYDefaultsJSON(t *testing.T) {
	if got := resolveOutputFormat("", &bytes.Buffer{}); got != "json" {
		t.Errorf("non-TTY default got %q; want json", got)
	}
}

// TestResolveOutputFormatTTYDefaultsTable cannot be exercised in `go
// test` (the test binary's stdout is a pipe, not a PTY). We instead
// assert the *os.File-but-not-TTY case still returns "json" — closing
// the loop on the isTerminal `info.Mode()&os.ModeCharDevice != 0` check
// when the file is a regular file rather than /dev/tty.
func TestResolveOutputFormatRegularFileFallsThroughToJSON(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "out-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if got := resolveOutputFormat("", f); got != "json" {
		t.Errorf("regular file got %q; want json (no ModeCharDevice)", got)
	}
}

// TestIsTerminalNonFileFalsy covers the writer-isn't-*os.File guard.
func TestIsTerminalNonFileFalsy(t *testing.T) {
	if isTerminal(&bytes.Buffer{}) {
		t.Error("bytes.Buffer reported as TTY")
	}
}
