package agents

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// validateOptions closes the scope and toolset enums. An unknown value has to
// fail before any path is resolved, because a typo in --scope would otherwise
// silently install to the wrong root.
func TestInstallRejectsUnknownScopeAndToolset(t *testing.T) {
	cases := []struct {
		name string
		opts Options
		want string
	}{
		{"scope", Options{Scope: "global"}, "unsupported agents scope: global"},
		{"toolset", Options{Toolset: "everything"}, "unsupported MCP toolset: everything"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.opts.HomeDir = t.TempDir()
			tc.opts.WorkDir = t.TempDir()

			_, err := Install(tc.opts)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("Install err = %v, want %q", err, tc.want)
			}
		})
	}
}

// The roots are canonicalized through EvalSymlinks, so a root that does not
// exist fails the install rather than writing to a path the caller cannot see.
func TestInstallRejectsUnresolvableRoots(t *testing.T) {
	real := t.TempDir()
	missing := filepath.Join(t.TempDir(), "gone")

	cases := []struct {
		name string
		opts Options
		want string
	}{
		{"home", Options{HomeDir: missing, WorkDir: real}, "resolve home directory"},
		{"work", Options{HomeDir: real, WorkDir: missing}, "resolve working directory"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Install(tc.opts)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Install err = %v, want one naming %q", err, tc.want)
			}
		})
	}
}

// roots falls back to the process home and working directory when the caller
// names neither. Both fallbacks have to produce the same canonical form the
// explicit path would.
func TestRootsFallsBackToTheProcessEnvironment(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	}

	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(work); err != nil {
		t.Fatalf("chdir %s: %v", work, err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })

	gotHome, gotWork, err := roots(Options{})
	if err != nil {
		t.Fatalf("roots: %v", err)
	}

	wantHome, err := canonicalRoot(home)
	if err != nil {
		t.Fatalf("canonicalRoot home: %v", err)
	}
	wantWork, err := canonicalRoot(work)
	if err != nil {
		t.Fatalf("canonicalRoot work: %v", err)
	}
	if gotHome != wantHome {
		t.Errorf("home = %q, want %q", gotHome, wantHome)
	}
	if gotWork != wantWork {
		t.Errorf("work = %q, want %q", gotWork, wantWork)
	}
}

// A symlink anywhere on the write path is refused: following one would let a
// pre-planted link redirect a config write outside the resolved root.
func TestInstallRefusesSymlinkedPaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevation on windows")
	}

	t.Run("target_is_a_symlink", func(t *testing.T) {
		home := t.TempDir()
		work := t.TempDir()
		if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		decoy := filepath.Join(t.TempDir(), "decoy.json")
		if err := os.WriteFile(decoy, []byte("{}"), 0o600); err != nil {
			t.Fatalf("write decoy: %v", err)
		}
		if err := os.Symlink(decoy, filepath.Join(home, ".claude", "mcp.json")); err != nil {
			t.Fatalf("symlink: %v", err)
		}

		_, err := Install(Options{Target: TargetClaude, HomeDir: home, WorkDir: work, Features: []string{FeatureMCP}})
		if err == nil || !strings.Contains(err.Error(), "path contains symlink component") {
			t.Fatalf("Install err = %v, want a symlink refusal", err)
		}
	})

	t.Run("parent_is_a_symlink", func(t *testing.T) {
		home := t.TempDir()
		work := t.TempDir()
		if err := os.Symlink(t.TempDir(), filepath.Join(home, ".cursor")); err != nil {
			t.Fatalf("symlink: %v", err)
		}

		_, err := Install(Options{Target: TargetCursor, HomeDir: home, WorkDir: work, Features: []string{FeatureMCP}})
		if err == nil || !strings.Contains(err.Error(), "path contains symlink component") {
			t.Fatalf("Install err = %v, want a symlink refusal", err)
		}
	})

	t.Run("target_is_a_directory", func(t *testing.T) {
		home := t.TempDir()
		work := t.TempDir()
		if err := os.MkdirAll(filepath.Join(home, ".gemini", "settings.json"), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}

		_, err := Install(Options{Target: TargetGemini, HomeDir: home, WorkDir: work, Features: []string{FeatureMCP}})
		if err == nil || !strings.Contains(err.Error(), "path is a directory") {
			t.Fatalf("Install err = %v, want a directory refusal", err)
		}
	})

	t.Run("parent_component_is_a_file", func(t *testing.T) {
		home := t.TempDir()
		work := t.TempDir()
		if err := os.WriteFile(filepath.Join(home, ".codex"), []byte("not a dir"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}

		_, err := Install(Options{Target: TargetCodex, HomeDir: home, WorkDir: work, Features: []string{FeatureMCP}})
		if err == nil || !strings.Contains(err.Error(), "path component is not a directory") {
			t.Fatalf("Install err = %v, want a not-a-directory refusal", err)
		}
	})
}

// A hand-edited config that kept the begin marker but lost the end marker has
// no safe splice point, so the merge refuses instead of appending a second
// block or eating the rest of the file.
func TestMergeCodexTOMLRejectsATruncatedManagedBlock(t *testing.T) {
	existing := []byte("# BEGIN gum managed mcp server\n[mcp_servers.gum]\ncommand = \"gum\"\n")

	_, err := mergeCodexTOML(existing, DefaultToolset, nil)
	if err == nil || !strings.Contains(err.Error(), "missing end marker") {
		t.Fatalf("mergeCodexTOML err = %v, want a missing-end-marker refusal", err)
	}
}

// mcpWriteFor is keyed on the target enum. validateOptions rejects an unknown
// target first, so the fallthrough exists only to keep the switch total: it
// must produce a write with no path, which the planner then skips.
func TestMCPWriteForUnknownTargetHasNoPath(t *testing.T) {
	got := mcpWriteFor("emacs", ScopeUser, "/home", "/work", DefaultToolset, nil)

	if got.path != "" {
		t.Errorf("path = %q, want empty for an unknown target", got.path)
	}
	if got.kind != FeatureMCP || got.target != "emacs" {
		t.Errorf("write = %#v, want the target and kind echoed back", got)
	}
	if got.merge != nil {
		t.Error("merge is set; an unknown target has no config format to merge")
	}
}

// dedupeWrites folds two targets that resolve to the same path into one action
// and records both target names. It must not record the same target twice when
// a target contributes the same path more than once.
func TestContainsTargetMatchesAnExistingName(t *testing.T) {
	cases := []struct {
		raw    string
		target string
		want   bool
	}{
		{"codex", "codex", true},
		{"codex,cursor", "cursor", true},
		{"codex,cursor", "gemini", false},
		{"", "codex", false},
	}

	for _, tc := range cases {
		if got := containsTarget(tc.raw, tc.target); got != tc.want {
			t.Errorf("containsTarget(%q, %q) = %v, want %v", tc.raw, tc.target, got, tc.want)
		}
	}
}

// chdirToRemovedDir moves the process into a directory and then deletes it, so
// os.Getwd and filepath.Abs fail. It reports false when the platform still
// answers Getwd for a deleted directory.
func chdirToRemovedDir(t *testing.T) bool {
	t.Helper()

	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir, err := os.MkdirTemp("", "gum-agents-gone")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(prev)
		_ = os.RemoveAll(dir)
	})
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("remove %s: %v", dir, err)
	}

	_, err = os.Getwd()
	return err != nil
}

// When the caller names no root, roots leans on the process environment. Both
// lookups can fail, and each failure has to say which root it was resolving.
func TestRootsSurfacesProcessEnvironmentFailures(t *testing.T) {
	t.Run("home", func(t *testing.T) {
		t.Setenv("HOME", "")
		if runtime.GOOS == "windows" {
			t.Setenv("USERPROFILE", "")
		}
		if _, err := os.UserHomeDir(); err == nil {
			t.Skip("this platform resolves a home directory without HOME")
		}

		_, _, err := roots(Options{WorkDir: t.TempDir()})
		if err == nil || !strings.Contains(err.Error(), "resolve home directory") {
			t.Fatalf("roots err = %v, want one naming the home directory", err)
		}
	})

	t.Run("work", func(t *testing.T) {
		home := t.TempDir()
		if !chdirToRemovedDir(t) {
			t.Skip("this platform still answers Getwd for a deleted directory")
		}

		_, _, err := roots(Options{HomeDir: home})
		if err == nil || !strings.Contains(err.Error(), "resolve working directory") {
			t.Fatalf("roots err = %v, want one naming the working directory", err)
		}
	})

	t.Run("relative_root_without_a_working_directory", func(t *testing.T) {
		if !chdirToRemovedDir(t) {
			t.Skip("this platform still answers Getwd for a deleted directory")
		}

		if _, err := canonicalRoot("relative"); err == nil {
			t.Fatal("canonicalRoot err = nil, want the Abs failure")
		}
	})
}

// An unwritable root is the common failure on a locked-down machine. Each of
// the three write stages has to surface it instead of reporting a plan that
// never landed.
func TestInstallSurfacesFilesystemFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits do not gate directory writes on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permission bits")
	}

	lockDir := func(t *testing.T, dir string) {
		t.Helper()
		if err := os.Chmod(dir, 0o500); err != nil {
			t.Fatalf("chmod %s: %v", dir, err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	}

	t.Run("mkdir", func(t *testing.T) {
		home := t.TempDir()
		lockDir(t, home)

		_, err := Install(Options{Target: TargetClaude, HomeDir: home, WorkDir: t.TempDir(), Features: []string{FeatureMCP}})
		if err == nil {
			t.Fatal("Install err = nil, want the MkdirAll failure")
		}
	})

	t.Run("read_existing", func(t *testing.T) {
		home := t.TempDir()
		dir := filepath.Join(home, ".claude")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "mcp.json"), []byte("{}"), 0o000); err != nil {
			t.Fatalf("write: %v", err)
		}

		_, err := Install(Options{Target: TargetClaude, HomeDir: home, WorkDir: t.TempDir(), Features: []string{FeatureMCP}})
		if err == nil {
			t.Fatal("Install err = nil, want the ReadFile failure")
		}
	})

	t.Run("write", func(t *testing.T) {
		home := t.TempDir()
		dir := filepath.Join(home, ".claude")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		lockDir(t, dir)

		_, err := Install(Options{Target: TargetClaude, HomeDir: home, WorkDir: t.TempDir(), Features: []string{FeatureMCP}})
		if err == nil {
			t.Fatal("Install err = nil, want the atomic write failure")
		}
	})
}

// The path guards are also called on their own, so each rejection is pinned
// here rather than only through whichever Install arm happens to reach it.
func TestPathGuardsRejectUnsafeTargets(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation and permission bits behave differently on windows")
	}

	t.Run("regular_file_exists", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, "as-dir"), 0o700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.Symlink(filepath.Join(dir, "as-dir"), filepath.Join(dir, "as-link")); err != nil {
			t.Fatalf("symlink: %v", err)
		}

		if _, err := regularFileExists(filepath.Join(dir, "as-link")); err == nil ||
			!strings.Contains(err.Error(), "path contains symlink component") {
			t.Errorf("symlink err = %v, want a symlink refusal", err)
		}
		if _, err := regularFileExists(filepath.Join(dir, "as-dir")); err == nil ||
			!strings.Contains(err.Error(), "path is a directory") {
			t.Errorf("directory err = %v, want a directory refusal", err)
		}
	})

	t.Run("relative_path_has_no_parent_to_walk", func(t *testing.T) {
		if err := rejectExistingParentSymlinks("bare-name"); err != nil {
			t.Fatalf("rejectExistingParentSymlinks err = %v, want nil", err)
		}
	})

	t.Run("unreadable_parent", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root bypasses permission bits")
		}
		// The temp root is canonicalized first: on darwin /var is a symlink to
		// /private/var, and the walk would refuse the path for that instead of
		// for the permission bits under test.
		dir, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatalf("evalsymlinks: %v", err)
		}
		locked := filepath.Join(dir, "locked")
		if err := os.MkdirAll(filepath.Join(locked, "sub"), 0o700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.Chmod(locked, 0o000); err != nil {
			t.Fatalf("chmod: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })

		if err := rejectExistingParentSymlinks(filepath.Join(locked, "sub", "child")); err == nil {
			t.Error("rejectExistingParentSymlinks err = nil, want the Lstat failure")
		}
		if _, err := resolveSafeWrite(filepath.Join(locked, "child")); err == nil {
			t.Error("resolveSafeWrite err = nil, want the Lstat failure")
		}
		if _, err := regularFileExists(filepath.Join(locked, "child")); err == nil {
			t.Error("regularFileExists err = nil, want the Lstat failure")
		}
	})
}

// An empty Codex config gets the managed block alone, with no leading blank
// line carried over from a file that had no content.
func TestMergeCodexTOMLWritesTheBlockIntoAnEmptyFile(t *testing.T) {
	for _, existing := range []string{"", "   \n\n\t"} {
		got, err := mergeCodexTOML([]byte(existing), DefaultToolset, nil)
		if err != nil {
			t.Fatalf("mergeCodexTOML(%q): %v", existing, err)
		}
		if !strings.HasPrefix(string(got), "# BEGIN gum managed mcp server\n") {
			t.Errorf("mergeCodexTOML(%q) = %q, want the block alone", existing, got)
		}
	}
}
