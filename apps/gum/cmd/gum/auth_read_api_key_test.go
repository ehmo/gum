package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestReadAPIKeyFromFile covers the --from-file branch: contents are read
// verbatim, surrounding whitespace trimmed (a trailing newline from
// `echo > file` must not propagate into the stored key).
func TestReadAPIKeyFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "key.txt")
	if err := os.WriteFile(path, []byte("  AIza-from-file\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cmd := &cobra.Command{}
	got, err := readAPIKey(cmd, false, path)
	if err != nil {
		t.Fatalf("readAPIKey: %v", err)
	}
	if got != "AIza-from-file" {
		t.Errorf("got %q, want AIza-from-file (whitespace must be trimmed)", got)
	}
}

// TestReadAPIKeyFromFileMissing locks the error envelope: a non-existent
// --from-file surfaces a wrapped error with the "gum auth use-api-key"
// prefix so an operator can grep the message back to the command.
func TestReadAPIKeyFromFileMissing(t *testing.T) {
	cmd := &cobra.Command{}
	_, err := readAPIKey(cmd, false, "/definitely/not/a/real/path/key.txt")
	if err == nil {
		t.Fatal("expected error for missing --from-file")
	}
	if !strings.Contains(err.Error(), "gum auth use-api-key") {
		t.Errorf("err=%q missing command prefix", err)
	}
	if !strings.Contains(err.Error(), "--from-file") {
		t.Errorf("err=%q missing flag context", err)
	}
}

// TestReadAPIKeyFromStdin covers the default branch: when --from-file is
// empty, the helper reads cmd.InOrStdin() and trims whitespace.
func TestReadAPIKeyFromStdin(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader("\n  AIza-via-stdin  \n"))

	got, err := readAPIKey(cmd, true, "")
	if err != nil {
		t.Fatalf("readAPIKey: %v", err)
	}
	if got != "AIza-via-stdin" {
		t.Errorf("got %q, want AIza-via-stdin", got)
	}
}
