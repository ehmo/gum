package profile_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/output/profile"
)

// writeProfileDir creates <parent>/.gum/profiles and writes each named file
// into it. It returns the profiles directory.
func writeProfileDir(t *testing.T, parent string, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(parent, ".gum", "profiles")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

// isolateHome points both profile home lookups at an empty directory so a
// developer's real ~/.config/gum/profiles cannot change a test outcome.
func isolateHome(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
}

// TestResolveInheritsFallsBackToTheLayerWalk pins the arm that makes a shared
// _base profile work when it lives in its own file: the sibling lookup in the
// defining file misses, so the resolver walks the three layers again for the
// parent name.
func TestResolveInheritsFallsBackToTheLayerWalk(t *testing.T) {
	isolateHome(t)
	root := t.TempDir()
	writeProfileDir(t, root, map[string]string{
		"_base.toml": "format = \"json\"\nlimit = 7\n",
		"child.toml": "inherits = \"_base\"\nstrip_nulls = true\n",
	})

	p, source, err := profile.ResolveProfile(root, "child", nil)
	if err != nil {
		t.Fatalf("ResolveProfile: %v", err)
	}
	if source != profile.SourceProjectLocal {
		t.Errorf("source=%q; want project-local", source)
	}
	if p.Limit != 7 {
		t.Errorf("limit=%d; want 7 inherited from _base", p.Limit)
	}
	if !p.StripNulls {
		t.Error("strip_nulls=false; want the child's own value to survive the merge")
	}
	if p.Inherits != "" {
		t.Errorf("inherits=%q; want it cleared on the merged profile", p.Inherits)
	}
}

// TestResolveInheritsMissingParentIsAnError keeps a typo in `inherits` from
// resolving to the child alone. ResolveProfile has to surface the failure, so
// this also pins its second error arm.
func TestResolveInheritsMissingParentIsAnError(t *testing.T) {
	isolateHome(t)
	root := t.TempDir()
	writeProfileDir(t, root, map[string]string{
		"child.toml": "inherits = \"nosuchbase\"\nformat = \"json\"\n",
	})

	_, _, err := profile.ResolveProfile(root, "child", nil)
	if err == nil {
		t.Fatal("ResolveProfile err=nil; want the missing-parent rejection")
	}
	if !strings.Contains(err.Error(), "nosuchbase") {
		t.Errorf("err=%v; want the missing parent named", err)
	}
}

// TestLookupInDirScansSiblingEnvelopeFiles pins the fallback scan: when
// <name>.toml exists but defines other names, the resolver reads the rest of
// the directory and must skip the file it already read.
func TestLookupInDirScansSiblingEnvelopeFiles(t *testing.T) {
	isolateHome(t)
	root := t.TempDir()
	dir := writeProfileDir(t, root, map[string]string{
		"wanted.toml": "[output_profiles.\"other\"]\nformat = \"json\"\n",
		"zz.toml":     "[output_profiles.\"wanted\"]\nformat = \"csv\"\nlimit = 3\n",
	})
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("ignored\n"), 0o600); err != nil {
		t.Fatalf("write notes.md: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "nested"), 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}

	p, _, err := profile.ResolveProfile(root, "wanted", nil)
	if err != nil {
		t.Fatalf("ResolveProfile: %v", err)
	}
	if p.Limit != 3 {
		t.Errorf("limit=%d; want 3 from the sibling envelope file", p.Limit)
	}
}

// TestLoadOverrideBindingsReadsBothLayers pins the merge and the two error
// arms that a malformed or unreadable directory produces. Project-local wins a
// shared key, per docs/expression-profile-dsl.md rule 4.
func TestLoadOverrideBindingsReadsBothLayers(t *testing.T) {
	t.Run("project_wins_a_shared_key", func(t *testing.T) {
		userHome := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", userHome)
		userDir := filepath.Join(userHome, "gum", "profiles")
		if err := os.MkdirAll(userDir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", userDir, err)
		}
		if err := os.WriteFile(filepath.Join(userDir, "b.toml"),
			[]byte("[override_bindings]\n\"a.b\" = \"user\"\n\"u.only\" = \"user\"\n"), 0o600); err != nil {
			t.Fatalf("write user bindings: %v", err)
		}

		root := t.TempDir()
		writeProfileDir(t, root, map[string]string{
			"a.toml": "[override_bindings]\n\"a.b\" = \"project\"\n",
		})

		got, err := profile.LoadOverrideBindings(root)
		if err != nil {
			t.Fatalf("LoadOverrideBindings: %v", err)
		}
		if got["a.b"] != "project" {
			t.Errorf("a.b=%q; want the project-local value", got["a.b"])
		}
		if got["u.only"] != "user" {
			t.Errorf("u.only=%q; want the user-global value to survive", got["u.only"])
		}
	})

	t.Run("malformed_file_is_an_error", func(t *testing.T) {
		userHome := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", userHome)
		userDir := filepath.Join(userHome, "gum", "profiles")
		if err := os.MkdirAll(userDir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", userDir, err)
		}
		if err := os.WriteFile(filepath.Join(userDir, "bad.toml"), []byte("format = xml\n"), 0o600); err != nil {
			t.Fatalf("write bad.toml: %v", err)
		}

		if _, err := profile.LoadOverrideBindings(""); err == nil {
			t.Fatal("LoadOverrideBindings err=nil; want the parse failure surfaced")
		}
	})
}

// TestUnreadableProfileDirSurfacesTheError keeps a permission fault from
// reading as "this layer declares nothing". A directory the process may
// traverse but not list has to fail loudly on both the resolve path and the
// bindings path.
func TestUnreadableProfileDirSurfacesTheError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root; directory permissions are not enforced")
	}
	userHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", userHome)
	dir := filepath.Join(userHome, "gum", "profiles")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	// Execute-only: a child file opens by name, but the directory will not list.
	if err := os.Chmod(dir, 0o300); err != nil {
		t.Fatalf("chmod %s: %v", dir, err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	if _, _, err := profile.ResolveProfile("", "anything", nil); err == nil {
		t.Error("ResolveProfile err=nil; want the unreadable-directory error")
	}
	if _, err := profile.LoadOverrideBindings(""); err == nil {
		t.Error("LoadOverrideBindings err=nil; want the unreadable-directory error")
	}

	root := t.TempDir()
	projDir := filepath.Join(root, ".gum", "profiles")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", projDir, err)
	}
	if err := os.Chmod(projDir, 0o300); err != nil {
		t.Fatalf("chmod %s: %v", projDir, err)
	}
	t.Cleanup(func() { _ = os.Chmod(projDir, 0o755) })

	if _, err := profile.LoadOverrideBindings(root); err == nil {
		t.Error("LoadOverrideBindings(project) err=nil; want the unreadable-directory error")
	}
}
