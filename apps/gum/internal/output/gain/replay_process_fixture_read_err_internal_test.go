package gain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestProcessFixtureResponseReadErrorWraps pins processFixture's
// `os.ReadFile response.json err → "read response.json: %w"` arm
// (replay.go:178-180). The fault is reachable from the public
// RunFixtureReplay path: collectFixtureLeaves uses os.Stat (which
// succeeds on a 0o000 file given a 0o755 parent), so the leaf is
// added to the work list; processFixture's subsequent os.ReadFile
// then fails with EACCES. The "read response.json:" prefix is the
// only signal a release-blog tooling pipeline has to distinguish a
// permission failure on an individual fixture from a wholesale
// fixture-tree walk failure.
func TestProcessFixtureResponseReadErrorWraps(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses POSIX file-mode permission checks")
	}

	root := t.TempDir()
	leaf := filepath.Join(root, "perm-blocked")
	if err := os.MkdirAll(leaf, 0o755); err != nil {
		t.Fatalf("mkdir leaf: %v", err)
	}
	respPath := filepath.Join(leaf, "response.json")
	if err := os.WriteFile(respPath, []byte(`{"x":1}`), 0o600); err != nil {
		t.Fatalf("write response.json: %v", err)
	}
	// Strip read permission so processFixture's ReadFile sees EACCES,
	// but the parent dir is still 0o755 so collectFixtureLeaves can
	// still os.Stat the file and discover the leaf.
	if err := os.Chmod(respPath, 0o000); err != nil {
		t.Fatalf("chmod 0: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(respPath, 0o600) })

	_, err := RunFixtureReplay(root, "toon")
	if err == nil {
		t.Fatal("RunFixtureReplay(unreadable response.json) err=nil; want EACCES wrap")
	}
	if !strings.Contains(err.Error(), "read response.json") {
		t.Errorf("err=%q; want 'read response.json' substring", err.Error())
	}
}
