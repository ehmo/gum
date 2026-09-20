package main

// profile_envelope_cli_test.go — gum-b3sm. `gum profile validate` used to
// reject every example in docs/expression-profile-dsl.md on line 1, because
// the loader read bare keys only. These pin the documented file shape and the
// OVERRIDE_BINDING_INVALID check on its [override_bindings] table.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// knownCatalogTarget returns an op_id the embedded catalog carries, so a
// binding key in these fixtures is valid without hard-coding an op that a
// later catalog build may drop.
func knownCatalogTarget(t *testing.T) string {
	t.Helper()
	c := loadCatalog()
	if c == nil || len(c.Ops) == 0 {
		t.Skip("binary embeds no catalog; the binding key check has nothing to check against")
	}
	return c.Ops[0].OpID
}

func writeProfileFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "profiles.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestProfileValidateAcceptsDocumentedEnvelope(t *testing.T) {
	target := knownCatalogTarget(t)
	path := writeProfileFile(t, `[output_profiles."_base.list_ops"]
format = "toon"
collapse_arrays = { max_items = 20 }
recovery = "local_artifact"

[output_profiles."gmail.messages.list.v1"]
inherits = "_base.list_ops"
field_mask = "nextPageToken,messages(id,threadId)"
on_empty = "No matching messages."

[override_bindings]
"`+target+`" = "gmail.messages.list.v1"
`)
	out, err := runCLI(t, "profile", "validate", path)
	if err != nil {
		t.Fatalf("validate documented example: %v (out=%s)", err, out)
	}
	if !strings.Contains(out, "ok") {
		t.Errorf("out = %q, want ok", out)
	}
}

func TestProfileValidateRejectsDanglingBindingProfile(t *testing.T) {
	target := knownCatalogTarget(t)
	path := writeProfileFile(t, `[output_profiles."x.v1"]
format = "toon"

[override_bindings]
"`+target+`" = "no.such.profile"
`)
	_, err := runCLI(t, "profile", "validate", path)
	if err == nil {
		t.Fatal("validate accepted a binding naming an unresolvable profile")
	}
	if !strings.Contains(err.Error(), "OVERRIDE_BINDING_INVALID") {
		t.Errorf("err = %v, want OVERRIDE_BINDING_INVALID", err)
	}
}

func TestProfileValidateRejectsUnknownBindingTarget(t *testing.T) {
	knownCatalogTarget(t) // skip when the binary embeds no catalog
	path := writeProfileFile(t, `[output_profiles."x.v1"]
format = "toon"

[override_bindings]
"not.a.real.op" = "x.v1"
`)
	_, err := runCLI(t, "profile", "validate", path)
	if err == nil {
		t.Fatal("validate accepted a binding key that is neither an op_id nor a variant_id")
	}
	if !strings.Contains(err.Error(), "OVERRIDE_BINDING_INVALID") {
		t.Errorf("err = %v, want OVERRIDE_BINDING_INVALID", err)
	}
}

const twoProfileFile = `[output_profiles."keep.text"]
format = "json"
keep_fields = ["results.text"]

[output_profiles."keep.drop"]
format = "json"
keep_fields = ["results.drop"]
`

// TestProfileTestSelectsNamedProfile: a file with several definitions has no
// defensible default, so `gum profile test` demands --name and honours it.
func TestProfileTestSelectsNamedProfile(t *testing.T) {
	dir := t.TempDir()
	profilePath := filepath.Join(dir, "profiles.toml")
	if err := os.WriteFile(profilePath, []byte(twoProfileFile), 0o644); err != nil {
		t.Fatalf("write profile: %v", err)
	}
	inputPath := filepath.Join(dir, "in.json")
	if err := os.WriteFile(inputPath, []byte(`{"results":[{"text":"shoes","drop":"gone"}]}`), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	out, err := runCLI(t, "profile", "test", profilePath, "--name", "keep.drop", "--input", inputPath)
	if err != nil {
		t.Fatalf("profile test --name keep.drop: %v (out=%s)", err, out)
	}
	if !strings.Contains(out, "gone") || strings.Contains(out, "shoes") {
		t.Errorf("--name keep.drop applied the wrong definition; out=%s", out)
	}

	t.Run("ambiguous", func(t *testing.T) {
		_, err := runCLI(t, "profile", "test", profilePath, "--input", inputPath)
		if err == nil {
			t.Fatal("profile test ran a two-profile file with no --name")
		}
		if !strings.Contains(err.Error(), "PROFILE_AMBIGUOUS") {
			t.Errorf("err = %v, want PROFILE_AMBIGUOUS", err)
		}
	})
	t.Run("unknown-name", func(t *testing.T) {
		_, err := runCLI(t, "profile", "test", profilePath, "--name", "nope", "--input", inputPath)
		if err == nil {
			t.Fatal("profile test accepted an undefined --name value")
		}
		if !strings.Contains(err.Error(), "PROFILE_NOT_FOUND") {
			t.Errorf("err = %v, want PROFILE_NOT_FOUND", err)
		}
	})
}
