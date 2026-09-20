package profile_test

// file_test.go — gum-b3sm: the documented file shape
// (docs/expression-profile-dsl.md "File Shape", spec §9.2).
//
// Before this change Parse read bare keys only, so pasting any documented
// example into a file failed on line 1, and spec §9.2 [override_bindings] had
// no representation the loader accepted.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/output/profile"
)

// docExample is the spec §9.2 / DSL-doc example, verbatim in shape: two
// definitions under [output_profiles], one inheriting the other, plus the
// [override_bindings] table.
const docExample = `[output_profiles."_base.list_ops"]
format = "toon"
strip_nulls = true
collapse_arrays = { max_items = 20 }
recovery = "local_artifact"

[output_profiles."gmail.messages.list.v1"]
inherits = "_base.list_ops"
field_mask = "nextPageToken,messages(id,threadId)"
truncate_strings = { default_chars = 500, fields = { snippet = 180 } }
on_empty = "No matching messages."

[override_bindings]
"gmail.users.messages.list" = "gmail.messages.list.v1"
`

func TestParseFileReadsDocumentedEnvelope(t *testing.T) {
	f, err := profile.ParseFile(docExample)
	if err != nil {
		t.Fatalf("ParseFile(docExample): %v", err)
	}
	if !f.Envelope {
		t.Error("Envelope = false; the file uses [output_profiles] tables")
	}
	if len(f.Profiles) != 2 {
		t.Fatalf("len(Profiles) = %d, want 2", len(f.Profiles))
	}
	base := f.Lookup("_base.list_ops")
	if base == nil {
		t.Fatal(`Lookup("_base.list_ops") = nil`)
	}
	if base.DefaultFormat != "toon" || !base.StripNulls {
		t.Errorf("base: format=%q strip_nulls=%v, want toon/true", base.DefaultFormat, base.StripNulls)
	}
	if base.CollapseArrays == nil || base.CollapseArrays.MaxItems != 20 {
		t.Errorf("base: collapse_arrays = %+v, want max_items 20", base.CollapseArrays)
	}
	derived := f.Lookup("gmail.messages.list.v1")
	if derived == nil {
		t.Fatal(`Lookup("gmail.messages.list.v1") = nil`)
	}
	if derived.Inherits != "_base.list_ops" {
		t.Errorf("derived: inherits = %q, want _base.list_ops", derived.Inherits)
	}
	if derived.OnEmpty != "No matching messages." {
		t.Errorf("derived: on_empty = %q", derived.OnEmpty)
	}
	if got := f.OverrideBindings["gmail.users.messages.list"]; got != "gmail.messages.list.v1" {
		t.Errorf("override_bindings[gmail.users.messages.list] = %q, want gmail.messages.list.v1", got)
	}
}

// TestParseFileReadsBareKeys keeps the catalog-embedded shape working: a
// builtin ships bare keys and takes its name from its embed path, so ParseFile
// must still accept a file with no table header at all.
func TestParseFileReadsBareKeys(t *testing.T) {
	f, err := profile.ParseFile("format = \"toon\"\nsort_by = \"id\"\n")
	if err != nil {
		t.Fatalf("ParseFile(bare): %v", err)
	}
	if f.Envelope {
		t.Error("Envelope = true for a bare-key file")
	}
	if len(f.Profiles) != 1 {
		t.Fatalf("len(Profiles) = %d, want 1", len(f.Profiles))
	}
	if f.Profiles[0].DefaultFormat != "toon" || f.Profiles[0].SortBy != "id" {
		t.Errorf("bare profile = %+v", f.Profiles[0])
	}
}

// TestParseFileRejectsMixedShapes covers the one direction TOML leaves
// ambiguous. A key after a table header belongs to that table, so only a bare
// key BEFORE the first header is a genuine mix, and it has to be refused: the
// file would otherwise hold one unnamed profile and one named one.
func TestParseFileRejectsMixedShapes(t *testing.T) {
	cases := map[string]string{
		"table-after-bare":   "format = \"toon\"\n[output_profiles.\"x\"]\nformat = \"json\"\n",
		"binding-after-bare": "format = \"toon\"\n[override_bindings]\n\"a.b\" = \"x\"\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := profile.ParseFile(src); err == nil {
				t.Fatal("ParseFile accepted a file that mixes the two shapes")
			}
		})
	}
}

func TestParseFileRejectsDuplicates(t *testing.T) {
	t.Run("profile", func(t *testing.T) {
		src := "[output_profiles.\"x\"]\nformat = \"toon\"\n[output_profiles.\"x\"]\nformat = \"json\"\n"
		_, err := profile.ParseFile(src)
		if err == nil || !strings.Contains(err.Error(), "duplicate profile") {
			t.Fatalf("err = %v, want duplicate profile", err)
		}
	})
	t.Run("binding", func(t *testing.T) {
		src := "[override_bindings]\n\"a.b\" = \"x\"\n\"a.b\" = \"y\"\n"
		_, err := profile.ParseFile(src)
		if err == nil || !strings.Contains(err.Error(), "declared twice") {
			t.Fatalf("err = %v, want declared twice", err)
		}
	})
}

func TestParseFileRejectsEmptyFile(t *testing.T) {
	if _, err := profile.ParseFile("# comment only\n"); err == nil {
		t.Fatal("ParseFile accepted a file declaring neither a profile nor a binding")
	}
}

func TestParseFileRejectsUnquotedProfileName(t *testing.T) {
	_, err := profile.ParseFile("[output_profiles.gmail.messages]\nformat = \"toon\"\n")
	if err == nil || !strings.Contains(err.Error(), "must be quoted") {
		t.Fatalf("err = %v, want a must-be-quoted error", err)
	}
}

// TestParseFileAttachesTestsToNamedProfile pins the [[tests]] rule: the fixture's
// `profile` key names its target, and a file with several definitions and a
// fixture that names none is an error rather than an arbitrary choice.
func TestParseFileAttachesTestsToNamedProfile(t *testing.T) {
	src := `[output_profiles."a"]
format = "toon"

[output_profiles."b"]
format = "json"

[[tests]]
name = "b-case"
profile = "b"
fixture = "in.json"
`
	f, err := profile.ParseFile(src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if got := len(f.Lookup("a").Tests); got != 0 {
		t.Errorf("profile a got %d fixtures, want 0", got)
	}
	if got := len(f.Lookup("b").Tests); got != 1 {
		t.Fatalf("profile b got %d fixtures, want 1", got)
	}

	t.Run("ambiguous", func(t *testing.T) {
		bad := strings.Replace(src, "profile = \"b\"\n", "", 1)
		if _, err := profile.ParseFile(bad); err == nil {
			t.Fatal("ParseFile accepted a fixture with no profile key in a two-profile file")
		}
	})
	t.Run("dangling", func(t *testing.T) {
		bad := strings.Replace(src, "profile = \"b\"", "profile = \"nope\"", 1)
		if _, err := profile.ParseFile(bad); err == nil {
			t.Fatal("ParseFile accepted a fixture naming an undefined profile")
		}
	})
}

func TestMergeOverrideBindingsFirstLayerWins(t *testing.T) {
	project := map[string]string{"a.b": "project", "only.project": "p"}
	user := map[string]string{"a.b": "user", "only.user": "u"}
	got := profile.MergeOverrideBindings(project, user)
	want := map[string]string{"a.b": "project", "only.project": "p", "only.user": "u"}
	if len(got) != len(want) {
		t.Fatalf("merged %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("merged[%q] = %q, want %q", k, got[k], v)
		}
	}
	if profile.MergeOverrideBindings() != nil {
		t.Error("MergeOverrideBindings() with no layers must return nil")
	}
}

func TestValidateOverrideBindings(t *testing.T) {
	f, err := profile.ParseFile(docExample)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	known := func(id string) bool { return id == "gmail.users.messages.list" }
	defined := func(name string) bool { return f.Lookup(name) != nil }

	if err := profile.ValidateOverrideBindings(f, known, defined); err != nil {
		t.Fatalf("valid bindings rejected: %v", err)
	}

	t.Run("dangling-profile", func(t *testing.T) {
		err := profile.ValidateOverrideBindings(f, known, func(string) bool { return false })
		if !errors.Is(err, profile.ErrOverrideBindingInvalid) {
			t.Fatalf("err = %v, want ErrOverrideBindingInvalid", err)
		}
	})
	t.Run("unknown-target", func(t *testing.T) {
		err := profile.ValidateOverrideBindings(f, func(string) bool { return false }, defined)
		if !errors.Is(err, profile.ErrOverrideBindingInvalid) {
			t.Fatalf("err = %v, want ErrOverrideBindingInvalid", err)
		}
	})
	t.Run("nil-callbacks-skip", func(t *testing.T) {
		if err := profile.ValidateOverrideBindings(f, nil, nil); err != nil {
			t.Fatalf("nil callbacks must skip both checks, got %v", err)
		}
	})
	t.Run("no-bindings", func(t *testing.T) {
		if err := profile.ValidateOverrideBindings(nil, known, defined); err != nil {
			t.Fatalf("nil file: %v", err)
		}
	})
}

// TestResolveProfileReadsEnvelopeFile covers the resolution consequence of the
// envelope: one file defines several names, so a lookup cannot stop at
// `<name>.toml`.
func TestResolveProfileReadsEnvelopeFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "empty_config"))
	dir := filepath.Join(tmp, "project", ".gum", "profiles")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "profiles.toml"), []byte(docExample), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	root := filepath.Join(tmp, "project")
	p, src, err := profile.ResolveProfile(root, "gmail.messages.list.v1", nil)
	if err != nil {
		t.Fatalf("ResolveProfile: %v", err)
	}
	if src != profile.SourceProjectLocal {
		t.Errorf("source = %q, want project-local", src)
	}
	// inherits resolved from the sibling definition in the same file.
	if p.DefaultFormat != "toon" {
		t.Errorf("format = %q, want toon inherited from _base.list_ops", p.DefaultFormat)
	}
	if p.Inherits != "" {
		t.Errorf("inherits = %q, want cleared after merge", p.Inherits)
	}
	if p.OnEmpty != "No matching messages." {
		t.Errorf("on_empty = %q, want the derived profile's own value", p.OnEmpty)
	}

	bindings, err := profile.LoadOverrideBindings(root)
	if err != nil {
		t.Fatalf("LoadOverrideBindings: %v", err)
	}
	if bindings["gmail.users.messages.list"] != "gmail.messages.list.v1" {
		t.Errorf("bindings = %v", bindings)
	}
}

// TestResolveProfileFailsClosedOnMalformedFile: a typo in a profile file must
// surface, not resolve to "no profile" and silently widen every response.
func TestResolveProfileFailsClosedOnMalformedFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "empty_config"))
	dir := filepath.Join(tmp, "project", ".gum", "profiles")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "broken.toml"), []byte("[nonsense]\nformat = \"toon\"\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, _, err := profile.ResolveProfile(filepath.Join(tmp, "project"), "anything", nil)
	if err == nil {
		t.Fatal("ResolveProfile ignored a malformed file in the search path")
	}
	if errors.Is(err, profile.ErrProfileNotFound) {
		t.Fatalf("malformed file reported as not-found: %v", err)
	}
}

// TestParseFileAcceptsBindingsOnlyFile: a file may carry [override_bindings]
// and no profile at all. The names it binds resolve from another layer, so
// rejecting the file would make a per-project binding table impossible.
func TestParseFileAcceptsBindingsOnlyFile(t *testing.T) {
	f, err := profile.ParseFile("[override_bindings]\n\"gmail.users.messages.list\" = \"elsewhere.v1\"\n")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if len(f.Profiles) != 0 {
		t.Errorf("profiles = %d, want 0", len(f.Profiles))
	}
	if f.OverrideBindings["gmail.users.messages.list"] != "elsewhere.v1" {
		t.Errorf("bindings = %v", f.OverrideBindings)
	}
	// The profile lives in another layer, so the file alone cannot resolve it.
	resolves := func(name string) bool { return name == "elsewhere.v1" }
	if err := profile.ValidateOverrideBindings(f, nil, resolves); err != nil {
		t.Errorf("bindings-only file rejected: %v", err)
	}
}

func TestParseFileRejectsStructuralBindingErrors(t *testing.T) {
	cases := map[string]string{
		"empty-key":      "[override_bindings]\n\"\" = \"p\"\n",
		"unquoted-value": "[override_bindings]\n\"gmail.users.messages.list\" = p\n",
		"no-equals":      "[override_bindings]\n\"gmail.users.messages.list\"\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := profile.ParseFile(src); err == nil {
				t.Fatal("ParseFile accepted a malformed override_bindings line")
			}
		})
	}
}

// TestLoadOverrideBindingsProjectBeatsUserGlobal proves the §9.2 layer order
// end to end, not just in MergeOverrideBindings: the nearer layer wins per key
// and the farther layer still contributes the keys it alone declares.
func TestLoadOverrideBindingsProjectBeatsUserGlobal(t *testing.T) {
	tmp := t.TempDir()
	config := filepath.Join(tmp, "config")
	t.Setenv("XDG_CONFIG_HOME", config)

	userDir := filepath.Join(config, "gum", "profiles")
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	userSrc := "[override_bindings]\n" +
		"\"gmail.users.messages.list\" = \"user.v1\"\n" +
		"\"drive.files.list\" = \"user.only.v1\"\n"
	if err := os.WriteFile(filepath.Join(userDir, "bindings.toml"), []byte(userSrc), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	projectDir := filepath.Join(tmp, "project", ".gum", "profiles")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	projectSrc := "[override_bindings]\n\"gmail.users.messages.list\" = \"project.v1\"\n"
	if err := os.WriteFile(filepath.Join(projectDir, "bindings.toml"), []byte(projectSrc), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := profile.LoadOverrideBindings(filepath.Join(tmp, "project"))
	if err != nil {
		t.Fatalf("LoadOverrideBindings: %v", err)
	}
	if got["gmail.users.messages.list"] != "project.v1" {
		t.Errorf("gmail binding = %q, want project.v1", got["gmail.users.messages.list"])
	}
	if got["drive.files.list"] != "user.only.v1" {
		t.Errorf("drive binding = %q, want user.only.v1", got["drive.files.list"])
	}
}
