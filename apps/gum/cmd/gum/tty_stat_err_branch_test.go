package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestIsTerminalStatErrorReturnsFalse pins the
// `f.Stat() err → return false` arm. A *os.File whose underlying
// fd has been closed (e.g. the caller reused the same handle across
// commands and an earlier Close raced into this path) MUST report
// non-TTY rather than panic. The choice of "false on err" preserves
// the script-safe default: scripted callers always get JSON.
//
// Reaching this arm requires a *os.File whose Stat fails. os.Stat
// uses fstat(2), which returns EBADF after the fd is closed. We
// create a temp file, close it, then pass the now-stale *os.File.
func TestIsTerminalStatErrorReturnsFalse(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "stale.txt"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// f is still a *os.File but the underlying fd is closed → Stat → EBADF.
	if isTerminal(f) {
		t.Error("isTerminal(closed file)=true; want false on Stat err")
	}
}
