package main

// profile_shadowing_cli_test.go — the §9.2 shadowing warning at the
// `gum profile validate` firing point (gum-03bc). The warning class is
// OVERRIDE_DISABLES_LOSSY_STAGE; it goes to stderr and never fails the
// command, because a profile that saves fewer tokens is still a valid profile.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/output/profile"
)

// runCLISplit runs the CLI with stdout and stderr captured separately, which is
// what lets a test tell a warning from a result.
func runCLISplit(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()

	var out, errBuf strings.Builder
	root := newRootCmd()
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetArgs(args)

	err = root.Execute()
	return out.String(), errBuf.String(), err
}

// shadowableProfile returns an op_id whose default variant carries a
// catalog-embedded profile with at least one loss-driving field, plus that
// profile's name. Nothing is hard-coded: a later catalog build may rename or
// drop any one profile.
func shadowableProfile(t *testing.T) (opID, profileName string) {
	t.Helper()

	c := loadCatalog()
	if c == nil {
		t.Skip("binary embeds no catalog; there is no catalog-embedded profile to shadow")
	}
	for i := range c.Ops {
		op := &c.Ops[i]
		for j := range op.Variants {
			name := op.Variants[j].OutputProfile
			if name == "" {
				continue
			}
			p, ok := profile.BuiltinLookup(name)
			if !ok {
				continue
			}
			if len(profile.DetectShadowing(op.OpID, p, &profile.Profile{})) > 0 {
				return op.OpID, name
			}
		}
	}
	t.Skip("no catalog-embedded profile sets a loss-driving field")
	return "", ""
}

func TestProfileValidateWarnsOnSameNameShadow(t *testing.T) {
	_, name := shadowableProfile(t)
	path := writeProfileFile(t, `[output_profiles."`+name+`"]
format = "toon"
`)

	stdout, stderr, err := runCLISplit(t, "profile", "validate", path)
	if err != nil {
		t.Fatalf("validate: %v (stderr=%s)", err, stderr)
	}
	if !strings.Contains(stdout, "ok") {
		t.Errorf("stdout = %q; the warning must not fail the command", stdout)
	}
	if !strings.Contains(stderr, profile.WarnOverrideDisablesLossyStage) {
		t.Errorf("stderr = %q; want the %s class", stderr, profile.WarnOverrideDisablesLossyStage)
	}
	if !strings.Contains(stderr, "removing a lossy-compression stage") {
		t.Errorf("stderr = %q; want the §9.2 warning text", stderr)
	}
	if !strings.Contains(stderr, name) {
		t.Errorf("stderr = %q; want the shadowed profile name %q", stderr, name)
	}
}

func TestProfileValidateWarnsOnBindingShadow(t *testing.T) {
	opID, _ := shadowableProfile(t)
	path := writeProfileFile(t, `[output_profiles."bare.v1"]
format = "toon"

[override_bindings]
"`+opID+`" = "bare.v1"
`)

	_, stderr, err := runCLISplit(t, "profile", "validate", path)
	if err != nil {
		t.Fatalf("validate: %v (stderr=%s)", err, stderr)
	}
	if !strings.Contains(stderr, opID) {
		t.Errorf("stderr = %q; want the bound op %q named", stderr, opID)
	}
	if !strings.Contains(stderr, "Pass --no-warn-lossy to suppress.") {
		t.Errorf("stderr = %q; want the suppression hint", stderr)
	}
}

// An override that keeps every loss-driving field is silent, so the warning
// stays useful.
func TestProfileValidateSilentWhenOverrideKeepsStages(t *testing.T) {
	_, name := shadowableProfile(t)
	p, ok := profile.BuiltinLookup(name)
	if !ok {
		t.Fatalf("BuiltinLookup(%q) = false", name)
	}

	body := `[output_profiles."` + name + `"]
format = "toon"
`
	if p.Recovery != "" {
		body += "recovery = \"" + p.Recovery + "\"\n"
	}
	if p.CollapseArrays != nil {
		body += "collapse_arrays = { max_items = 1 }\n"
	}
	if p.StripNulls {
		body += "strip_nulls = true\n"
	}
	if p.FieldMaskMode != "" {
		body += "field_mask_mode = \"" + p.FieldMaskMode + "\"\n"
	}
	if p.Dedupe != nil {
		body += "dedupe = { by = [\"id\"] }\n"
	}
	if p.TruncateStrings != nil {
		body += "truncate_strings = { default_chars = 10 }\n"
	}

	_, stderr, err := runCLISplit(t, "profile", "validate", writeProfileFile(t, body))
	if err != nil {
		t.Fatalf("validate: %v (stderr=%s)", err, stderr)
	}
	if strings.Contains(stderr, profile.WarnOverrideDisablesLossyStage) {
		t.Errorf("stderr = %q; an override that keeps every stage must be silent", stderr)
	}
}

func TestProfileValidateNoWarnLossySuppresses(t *testing.T) {
	_, name := shadowableProfile(t)
	path := writeProfileFile(t, `[output_profiles."`+name+`"]
format = "toon"
`)

	for _, flag := range []string{"--no-warn-lossy", "--no-warn-recovery"} {
		stdout, stderr, err := runCLISplit(t, "profile", "validate", path, flag)
		if err != nil {
			t.Fatalf("%s: validate: %v (stderr=%s)", flag, err, stderr)
		}
		if !strings.Contains(stdout, "ok") {
			t.Errorf("%s: stdout = %q; want ok", flag, stdout)
		}
		if strings.Contains(stderr, profile.WarnOverrideDisablesLossyStage) {
			t.Errorf("%s did not suppress the warning: stderr = %q", flag, stderr)
		}
	}
}

// --no-warn-recovery is the legacy alias §9.2 keeps for one release cycle. It
// stays out of help so nobody learns the name that is going away.
func TestNoWarnRecoveryIsAHiddenAlias(t *testing.T) {
	root := newRootCmd()
	f := root.PersistentFlags().Lookup("no-warn-recovery")
	if f == nil {
		t.Fatal("--no-warn-recovery is not registered")
	}
	if f.Deprecated == "" {
		t.Error("--no-warn-recovery is not marked deprecated, so help still advertises it")
	}
	if root.PersistentFlags().Lookup("no-warn-lossy") == nil {
		t.Fatal("--no-warn-lossy is not registered")
	}
}

// A file the resolver would never reach still validates: the warning is about
// loss-driving fields, not about where the file sits.
func TestProfileValidateShadowWarningIsPerProfile(t *testing.T) {
	_, name := shadowableProfile(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "profiles.toml")
	body := `[output_profiles."` + name + `"]
format = "toon"

[output_profiles."unrelated.v1"]
format = "json"
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}

	_, stderr, err := runCLISplit(t, "profile", "validate", path)
	if err != nil {
		t.Fatalf("validate: %v (stderr=%s)", err, stderr)
	}
	if strings.Count(stderr, "unrelated.v1") != 0 {
		t.Errorf("stderr = %q; unrelated.v1 shadows no catalog profile", stderr)
	}
	if !strings.Contains(stderr, name) {
		t.Errorf("stderr = %q; want a warning for %q", stderr, name)
	}
}
