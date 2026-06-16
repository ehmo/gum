package main

import (
	"io"
	"os"
)

// isTerminal returns true when w is the process stdout AND that stdout is
// connected to a character device (terminal/PTY). Anything else — pipe,
// file, /dev/null, the bytes.Buffer tests use — is treated as non-TTY so
// scripted output stays JSON.
//
// We accept io.Writer (not *os.File) so callers can pass cmd.OutOrStdout()
// without a type assertion; the body downcasts to *os.File and only then
// runs the stat. Tests that inject a *bytes.Buffer get false automatically.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// isReaderTerminal reports whether r is an interactive terminal (stdin on a
// PTY). Used to gate the missing-arg wizard so piped/scripted/agent input
// never blocks on a prompt. A non-*os.File reader (the bytes.Reader tests use)
// is treated as non-interactive.
func isReaderTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// resolveOutputFormat picks the rendering format for human-readable
// subcommands (gum-me29 acceptance b, gum-4gey.9).
//
//   - Explicit --format wins ("json" | "table" | "toon").
//   - TTY default is "table".
//   - Non-TTY default is "json" — scripts that grep the output keep working.
//
// JSON-stream subcommands (gum read, gum call) intentionally do NOT call
// this helper. Flipping their default would break automation; see
// gum-me29 design note "JSON-stream subcommands STAY default-JSON".
func resolveOutputFormat(flagValue string, w io.Writer) string {
	if flagValue != "" {
		return flagValue
	}
	if isTerminal(w) {
		return "table"
	}
	return "json"
}
