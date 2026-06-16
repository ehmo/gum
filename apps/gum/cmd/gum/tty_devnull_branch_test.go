//go:build !windows

package main

import (
	"os"
	"testing"
)

// TestResolveOutputFormatCharDeviceReturnsTable pins
// resolveOutputFormat's `isTerminal(w) → return "table"` arm
// (tty.go:42-44). The existing tty_test.go suite skipped this branch
// because `go test`'s stdout is a pipe; we exercise it via /dev/null,
// which on macOS and Linux carries os.ModeCharDevice exactly like a
// real PTY. Build-tagged !windows because /dev/null doesn't exist on
// Windows.
func TestResolveOutputFormatCharDeviceReturnsTable(t *testing.T) {
	f, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		t.Skipf("open /dev/null: %v", err)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		t.Skipf("stat /dev/null: %v", err)
	}
	if info.Mode()&os.ModeCharDevice == 0 {
		t.Skip("/dev/null is not a char device on this platform")
	}
	if got := resolveOutputFormat("", f); got != "table" {
		t.Errorf("resolveOutputFormat(/dev/null) = %q; want \"table\" (TTY default)", got)
	}
}
