package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestInitUserHomeDirFailureWrapsInitPrefix pins the
// `os.UserHomeDir err → return fmt.Errorf("init: resolve home directory: %w", err)`
// arm (init.go:33-35). The "init:" prefix is the operator's grep
// handle distinguishing pre-flight filesystem failures from later
// ResolveTarget/PlanPatch surfaces — without the wrap, the raw
// stdlib err "$HOME is not defined" leaves callers guessing which
// command produced it.
//
// On Unix, UserHomeDir checks $HOME then falls back to user.Current
// for /etc/passwd lookup. Clearing HOME + USER + LOGNAME forces both
// paths to fail (user.Current can't resolve the uid without one of
// the env vars seeded in CI/test environments).
func TestInitUserHomeDirFailureWrapsInitPrefix(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USER", "")
	t.Setenv("LOGNAME", "")

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"init"})

	err := root.Execute()
	if err == nil {
		t.Fatal("init with empty HOME/USER succeeded; want UserHomeDir err wrap")
	}
	if !strings.HasPrefix(err.Error(), "init: resolve home directory:") {
		t.Errorf("err=%q; want 'init: resolve home directory:' prefix", err)
	}
}
