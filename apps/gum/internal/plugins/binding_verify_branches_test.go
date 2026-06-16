package plugins

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestVerifyExecutableBindingNilReturnsUntrusted pins the
// `b == nil → nil binding` arm (binding.go:48-50). Required so the
// caller can confidently pass through a *ExecutableBinding fetched
// from an external map without a nil-check.
func TestVerifyExecutableBindingNilReturnsUntrusted(t *testing.T) {
	t.Parallel()
	err := VerifyExecutableBinding(nil)
	if err == nil {
		t.Fatal("VerifyExecutableBinding(nil) err=nil; want untrusted wrap")
	}
	if !errors.Is(err, ErrExecutableUntrusted) {
		t.Errorf("err=%v; want ErrExecutableUntrusted wrap", err)
	}
}

// TestVerifyExecutableBindingIncompleteFieldsReturnsUntrusted pins the
// `InstallRoot|ExecutablePath|ExecutableSHA256 empty → untrusted` arm
// (binding.go:51-53). All three are required for the trust check to
// even begin.
func TestVerifyExecutableBindingIncompleteFieldsReturnsUntrusted(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		b    *ExecutableBinding
	}{
		{"empty_install_root", &ExecutableBinding{ExecutablePath: "/x", ExecutableSHA256: "abc"}},
		{"empty_exec_path", &ExecutableBinding{InstallRoot: "/r", ExecutableSHA256: "abc"}},
		{"empty_sha", &ExecutableBinding{InstallRoot: "/r", ExecutablePath: "/x"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := VerifyExecutableBinding(tc.b)
			if err == nil {
				t.Fatalf("VerifyExecutableBinding(%s) err=nil; want untrusted", tc.name)
			}
			if !errors.Is(err, ErrExecutableUntrusted) {
				t.Errorf("err=%v; want ErrExecutableUntrusted wrap", err)
			}
		})
	}
}

// TestVerifyExecutableBindingInstallRootEvalSymlinksError pins the
// `filepath.EvalSymlinks(InstallRoot) err → return wrap` arm
// (binding.go:63-65). Reached when the install_root path doesn't
// resolve — here a non-existent absolute path is supplied.
func TestVerifyExecutableBindingInstallRootEvalSymlinksError(t *testing.T) {
	t.Parallel()
	b := &ExecutableBinding{
		Name:             "ghost",
		InstallRoot:      "/does/not/exist/install/root",
		ExecutablePath:   "/does/not/exist/install/root/ghost",
		ExecutableSHA256: "deadbeef",
	}
	err := VerifyExecutableBinding(b)
	if err == nil {
		t.Fatal("VerifyExecutableBinding(missing root) err=nil; want install_root resolve wrap")
	}
	if !errors.Is(err, ErrExecutableUntrusted) {
		t.Errorf("err=%v; want ErrExecutableUntrusted wrap", err)
	}
}

// TestVerifyExecutableBindingExecutableEvalSymlinksError pins the
// `filepath.EvalSymlinks(ExecutablePath) err → return wrap` arm
// (binding.go:67-69). Reached when InstallRoot resolves (real dir)
// but ExecutablePath doesn't (missing file under the real root).
func TestVerifyExecutableBindingExecutableEvalSymlinksError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	b := &ExecutableBinding{
		Name:             "ghost",
		InstallRoot:      root,
		ExecutablePath:   filepath.Join(root, "missing-exec"),
		ExecutableSHA256: "deadbeef",
	}
	err := VerifyExecutableBinding(b)
	if err == nil {
		t.Fatal("VerifyExecutableBinding(missing exec) err=nil; want executable resolve wrap")
	}
	if !errors.Is(err, ErrExecutableUntrusted) {
		t.Errorf("err=%v; want ErrExecutableUntrusted wrap", err)
	}
}

// TestVerifyExecutableBindingHashError pins the `hashFileSHA256 err
// → return hash-wrap` arm (binding.go:77-79). Reached when the
// executable exists and resolves under install_root but is
// unreadable (chmod 0o000). Skipped under euid 0 because root
// bypasses mode-based permission checks.
func TestVerifyExecutableBindingHashError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("EACCES not surfaced when running as root")
	}
	t.Parallel()
	root := t.TempDir()
	execPath := filepath.Join(root, "exec")
	if err := os.WriteFile(execPath, []byte("body"), 0o600); err != nil {
		t.Fatalf("plant exec: %v", err)
	}
	if err := os.Chmod(execPath, 0o000); err != nil {
		t.Fatalf("chmod exec: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(execPath, 0o600) })

	b := &ExecutableBinding{
		Name:             "unreadable",
		InstallRoot:      root,
		ExecutablePath:   execPath,
		ExecutableSHA256: "deadbeef",
	}
	err := VerifyExecutableBinding(b)
	if err == nil {
		t.Fatal("VerifyExecutableBinding(unreadable) err=nil; want hash err wrap")
	}
	if !errors.Is(err, ErrExecutableUntrusted) {
		t.Errorf("err=%v; want ErrExecutableUntrusted wrap", err)
	}
}
