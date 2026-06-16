package plugins

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestAssertInsideInstallRootShapes pins every observable branch in the
// install-root containment check: a clean child path passes; a sibling
// that EvalSymlinks-resolves outside the root is rejected; missing
// install_dir + missing exec both wrap ErrExecutableUntrusted so the
// caller can errors.Is-route them to the "refuse to spawn" path.
func TestAssertInsideInstallRootShapes(t *testing.T) {
	root := t.TempDir()
	exec := filepath.Join(root, "bin", "plugin")
	if err := os.MkdirAll(filepath.Dir(exec), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(exec, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatalf("write exec: %v", err)
	}

	t.Run("inside_root_passes", func(t *testing.T) {
		if err := assertInsideInstallRoot(root, exec); err != nil {
			t.Errorf("got %v; want nil", err)
		}
	})

	t.Run("install_dir_resolve_fails", func(t *testing.T) {
		err := assertInsideInstallRoot(filepath.Join(root, "nope"), exec)
		if err == nil {
			t.Fatalf("expected error")
		}
		if !errors.Is(err, ErrExecutableUntrusted) {
			t.Errorf("err=%v; want wraps ErrExecutableUntrusted", err)
		}
	})

	t.Run("executable_resolve_fails", func(t *testing.T) {
		err := assertInsideInstallRoot(root, filepath.Join(root, "missing-exec"))
		if err == nil {
			t.Fatalf("expected error")
		}
		if !errors.Is(err, ErrExecutableUntrusted) {
			t.Errorf("err=%v; want wraps ErrExecutableUntrusted", err)
		}
	})

	t.Run("escapes_install_root", func(t *testing.T) {
		// A second sibling root with its own real executable — exec
		// resolves to a path that's outside the requested install_dir.
		otherRoot := t.TempDir()
		otherExec := filepath.Join(otherRoot, "stray")
		if err := os.WriteFile(otherExec, []byte("x"), 0o700); err != nil {
			t.Fatalf("write stray: %v", err)
		}
		err := assertInsideInstallRoot(root, otherExec)
		if err == nil {
			t.Fatalf("expected escape error")
		}
		if !errors.Is(err, ErrExecutableUntrusted) {
			t.Errorf("err=%v; want wraps ErrExecutableUntrusted", err)
		}
	})
}
