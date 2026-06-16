package plugins

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestReadExecutableDigestSidecarReadErrorWraps pins
// readExecutableDigestSidecar's `!IsNotExist → wrap untrusted` arm
// (host.go:530). Reached when the sidecar exists but is unreadable —
// here the parent directory is chmod 0o000 so os.ReadFile returns
// EACCES (which isn't IsNotExist). Skipped under euid 0.
func TestReadExecutableDigestSidecarReadErrorWraps(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("EACCES not surfaced when running as root")
	}
	installDir := t.TempDir()
	// Plant the sidecar with valid content first, then chmod the parent.
	sidecar := filepath.Join(installDir, executableDigestSidecar)
	if err := os.WriteFile(sidecar, []byte("deadbeef"), 0o600); err != nil {
		t.Fatalf("plant sidecar: %v", err)
	}
	if err := os.Chmod(installDir, 0o000); err != nil {
		t.Fatalf("chmod parent: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(installDir, 0o755) })

	_, err := readExecutableDigestSidecar(installDir)
	if err == nil {
		t.Fatal("readExecutableDigestSidecar(unreadable) err=nil; want untrusted wrap")
	}
	if !errors.Is(err, ErrExecutableUntrusted) {
		t.Errorf("err=%v; want ErrExecutableUntrusted", err)
	}
}

// TestReadExecutableDigestSidecarEmptyContentWraps pins the
// `digest == "" → empty digest wrap` arm (host.go:533-535). A
// whitespace-only sidecar must surface as untrusted rather than be
// silently accepted as a blank digest.
func TestReadExecutableDigestSidecarEmptyContentWraps(t *testing.T) {
	installDir := t.TempDir()
	sidecar := filepath.Join(installDir, executableDigestSidecar)
	if err := os.WriteFile(sidecar, []byte("   \n\t  \n"), 0o600); err != nil {
		t.Fatalf("plant empty sidecar: %v", err)
	}
	_, err := readExecutableDigestSidecar(installDir)
	if err == nil {
		t.Fatal("readExecutableDigestSidecar(empty) err=nil; want empty digest wrap")
	}
	if !errors.Is(err, ErrExecutableUntrusted) {
		t.Errorf("err=%v; want ErrExecutableUntrusted", err)
	}
}
