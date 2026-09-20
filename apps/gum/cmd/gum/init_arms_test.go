package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// runInitArms executes `gum init` under a temp home and a temp working
// directory, and returns the combined output plus the RunE error.
func runInitArms(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader(""))
	root.SetArgs(append([]string{"init"}, args...))
	err := root.Execute()
	return out.String(), err
}

// lockDir drops the write bit on dir and restores it at cleanup, so a later
// TempDir removal still works.
func lockDir(t *testing.T, dir string) {
	t.Helper()
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod %s: %v", dir, err)
	}
}

// TestWarnIfShadowedOnPathStaysQuietForItself pins the arm that makes the
// warning specific: the `gum` first on PATH resolving to this very binary is
// not a collision, so nothing is printed.
func TestWarnIfShadowedOnPathStaysQuietForItself(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PATH executable resolution differs on Windows")
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	dir := t.TempDir()
	if err := os.Symlink(self, filepath.Join(dir, "gum")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	t.Setenv("PATH", dir)

	var buf bytes.Buffer
	warnIfShadowedOnPath(&buf)
	if buf.Len() != 0 {
		t.Errorf("warning printed for this binary itself: %q", buf.String())
	}
}

// TestInitChecksThePathOnlyOnATerminal pins the guard in front of the shadow
// check. os.DevNull is a character device, so it satisfies isTerminal without
// a pty.
func TestInitChecksThePathOnlyOnATerminal(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	t.Chdir(t.TempDir())

	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	defer func() { _ = devnull.Close() }()
	if !isTerminal(devnull) {
		t.Skip("os.DevNull is not reported as a character device here")
	}

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(devnull)
	root.SetArgs([]string{"init", "--refresh"})
	if err := root.Execute(); err != nil {
		t.Fatalf("gum init --refresh: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "Refreshed") {
		t.Errorf("stdout missing the refresh line:\n%s", out.String())
	}
}

// TestInitRefreshSurfacesAWriteFailure pins the refresh branch's error arm: a
// GUM.md that cannot be written has to fail the command, not be reported as
// refreshed.
func TestInitRefreshSurfacesAWriteFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	t.Chdir(dir)
	lockDir(t, dir)

	out, err := runInitArms(t, "--refresh")
	if err == nil {
		t.Fatalf("want the GUM.md write failure, got output %q", out)
	}
}

// TestInitSurfacesAPlanFailure pins the PlanPatch error arm. An unreadable
// settings.json must stop the command before anything is written.
func TestInitSurfacesAPlanFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores the read bit")
	}
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	t.Chdir(dir)

	claude := filepath.Join(dir, ".claude")
	if err := os.MkdirAll(claude, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", claude, err)
	}
	settings := filepath.Join(claude, "settings.json")
	if err := os.WriteFile(settings, []byte(`{"mcpServers":{}}`), 0o600); err != nil {
		t.Fatalf("write %s: %v", settings, err)
	}
	t.Cleanup(func() { _ = os.Chmod(settings, 0o600) })
	if err := os.Chmod(settings, 0o000); err != nil {
		t.Fatalf("chmod %s: %v", settings, err)
	}

	out, err := runInitArms(t, "--yes")
	if err == nil {
		t.Fatalf("want the PlanPatch read failure, got output %q", out)
	}
}

// TestInitSurfacesAnApplyFailure pins the Apply error arm: the patch is
// planned and previewed, then the write fails, and gum must not claim the file
// was patched.
func TestInitSurfacesAnApplyFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores the write bit")
	}
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	t.Chdir(dir)

	claude := filepath.Join(dir, ".claude")
	if err := os.MkdirAll(claude, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", claude, err)
	}
	if err := os.WriteFile(filepath.Join(claude, "settings.json"), []byte(`{"mcpServers":{}}`), 0o600); err != nil {
		t.Fatalf("write settings.json: %v", err)
	}
	lockDir(t, claude)

	out, err := runInitArms(t, "--yes")
	if err == nil {
		t.Fatalf("want the Apply failure, got output %q", out)
	}
	if strings.Contains(out, "Patched ") {
		t.Errorf("output claims a patch that never landed:\n%s", out)
	}
}

// TestInitSurfacesAGUMmdFailureAfterANoOp pins the last error arm: the host
// config already carries the entry, so only GUM.md is left to write, and its
// failure has to fail the command.
func TestInitSurfacesAGUMmdFailureAfterANoOp(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores the write bit")
	}
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	t.Chdir(dir)

	if out, err := runInitArms(t, "--yes"); err != nil {
		t.Fatalf("first init --yes: %v\n%s", err, out)
	}
	lockDir(t, dir)

	out, err := runInitArms(t, "--yes")
	if err == nil {
		t.Fatalf("want the GUM.md write failure, got output %q", out)
	}
	if !strings.Contains(out, "No changes") {
		t.Errorf("output missing the no-op line:\n%s", out)
	}
}
