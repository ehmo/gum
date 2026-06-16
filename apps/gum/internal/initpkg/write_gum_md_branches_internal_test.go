package initpkg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWriteGUMmdProjectBranchWritesUnderProjectDir pins the
// `global=false → dest = filepath.Join(projectDir, "GUM.md")` arm.
// The companion TestWriteGUMmdRendersTemplate exercises the
// `global=true` path (homeDir/GUM.md); this test pins the project-
// local branch that `gum init --local` calls into. Without this arm
// pinned, a refactor swapping homeDir/projectDir could silently
// stop emitting per-project GUM.md files even though the global
// flow still works in CI.
func TestWriteGUMmdProjectBranchWritesUnderProjectDir(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	dest, err := WriteGUMmd(home, project, "v0.1.0-proj", false)
	if err != nil {
		t.Fatalf("WriteGUMmd: %v", err)
	}
	want := filepath.Join(project, "GUM.md")
	if dest != want {
		t.Errorf("dest = %s; want %s", dest, want)
	}
	// File MUST be written to the project dir, NOT the home dir.
	if _, err := os.Stat(want); err != nil {
		t.Errorf("project GUM.md missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "GUM.md")); err == nil {
		t.Error("home GUM.md present despite global=false; project-branch must not leak into homeDir")
	}
	body, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read GUM.md: %v", err)
	}
	if !strings.Contains(string(body), "v0.1.0-proj") {
		t.Errorf("GUM.md missing version interpolation: %s", body)
	}
}
