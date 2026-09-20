// Package profile_test — the semantic profile validators.
//
// docs/expression-profile-dsl.md:13 requires both the structural validator and
// the semantic validator before any profile is accepted. Only the structural
// one existed: ON_EMPTY_TOO_LONG and PROFILE_TEE_MODE_CONFLICT were named in
// the spec, the DSL doc and the test matrix, and produced by nothing.
package profile_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/embedded"
	"github.com/ehmo/gum/internal/output/profile"
)

// decomposed returns n copies of a combining-accent "é" (U+0065 U+0301), which
// is two codepoints raw and one after NFC. It is the only construction that
// tells a raw count apart from a normalized one.
func decomposed(n int) string {
	return strings.Repeat("é", n)
}

// TestValidateSemanticsRejectsLongOnEmpty pins the ON_EMPTY_TOO_LONG cap from
// docs/spec.md:2103: on_empty is capped at 500 codepoints.
func TestValidateSemanticsRejectsLongOnEmpty(t *testing.T) {
	p := &profile.Profile{OnEmpty: strings.Repeat("a", 501)}

	err := profile.ValidateSemantics(p)
	if err == nil {
		t.Fatal("ValidateSemantics = nil; want ON_EMPTY_TOO_LONG for a 501-codepoint on_empty")
	}
	if !strings.Contains(err.Error(), "ON_EMPTY_TOO_LONG") {
		t.Errorf("err = %v; want the literal code ON_EMPTY_TOO_LONG", err)
	}
}

// TestValidateSemanticsAcceptsExactly500 keeps the cap inclusive.
func TestValidateSemanticsAcceptsExactly500(t *testing.T) {
	if err := profile.ValidateSemantics(&profile.Profile{OnEmpty: strings.Repeat("a", 500)}); err != nil {
		t.Errorf("ValidateSemantics = %v; want nil at exactly 500 codepoints", err)
	}
}

// TestOnEmptyLengthCountsAfterNFC pins the normative ordering in
// docs/expression-profile-dsl.md:88: normalize, then count. Both strings here
// exceed 500 raw codepoints; only the second exceeds it after NFC.
func TestOnEmptyLengthCountsAfterNFC(t *testing.T) {
	ok := &profile.Profile{OnEmpty: decomposed(500)} // 1000 raw, 500 NFC
	if err := profile.ValidateSemantics(ok); err != nil {
		t.Errorf("ValidateSemantics = %v; want nil (1000 raw codepoints, 500 after NFC)", err)
	}

	bad := &profile.Profile{OnEmpty: decomposed(501)} // 1002 raw, 501 NFC
	err := profile.ValidateSemantics(bad)
	if err == nil || !strings.Contains(err.Error(), "ON_EMPTY_TOO_LONG") {
		t.Errorf("ValidateSemantics = %v; want ON_EMPTY_TOO_LONG at 501 codepoints after NFC", err)
	}
}

// TestParseNormalizesOnEmpty pins the "propagated form" rule from
// docs/expression-profile-dsl.md:88: the value the runtime carries in
// _expression.on_empty_message is the NFC form, not the raw TOML author value.
func TestParseNormalizesOnEmpty(t *testing.T) {
	p, err := profile.Parse("format = \"json\"\non_empty = \"café\"\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if p.OnEmpty != "café" {
		t.Errorf("OnEmpty = %q (% x); want the NFC form %q", p.OnEmpty, p.OnEmpty, "café")
	}
}

// TestValidateSemanticsRejectsResourceLinkWithoutAlwaysTee pins
// PROFILE_TEE_MODE_CONFLICT. A resource link with no guaranteed artifact
// behind it is a dangling handle (docs/spec.md:1953).
func TestValidateSemanticsRejectsResourceLinkWithoutAlwaysTee(t *testing.T) {
	for _, mode := range []string{"off", "failures"} {
		p := &profile.Profile{Recovery: "resource_link", TeeMode: mode}

		err := profile.ValidateSemantics(p)
		if err == nil {
			t.Fatalf("tee_mode=%q: ValidateSemantics = nil; want PROFILE_TEE_MODE_CONFLICT", mode)
		}
		if !strings.Contains(err.Error(), "PROFILE_TEE_MODE_CONFLICT") {
			t.Errorf("tee_mode=%q: err = %v; want the literal code PROFILE_TEE_MODE_CONFLICT", mode, err)
		}
	}
}

// TestValidateSemanticsAcceptsResourceLinkWithAlwaysOrDefault keeps the two
// valid shapes. An unset tee_mode defaults to "always" whenever recovery is
// not "none", so an omitted key is not a conflict.
func TestValidateSemanticsAcceptsResourceLinkWithAlwaysOrDefault(t *testing.T) {
	for _, mode := range []string{"always", ""} {
		p := &profile.Profile{Recovery: "resource_link", TeeMode: mode}
		if err := profile.ValidateSemantics(p); err != nil {
			t.Errorf("tee_mode=%q: ValidateSemantics = %v; want nil", mode, err)
		}
	}
}

// TestValidateSemanticsReportsBothFailures keeps the validator from stopping at
// the first fault: an operator fixing a profile should see the whole list.
func TestValidateSemanticsReportsBothFailures(t *testing.T) {
	p := &profile.Profile{
		OnEmpty:  strings.Repeat("a", 900),
		Recovery: "resource_link",
		TeeMode:  "off",
	}

	err := profile.ValidateSemantics(p)
	if err == nil {
		t.Fatal("ValidateSemantics = nil; want both codes")
	}
	for _, code := range []string{"ON_EMPTY_TOO_LONG", "PROFILE_TEE_MODE_CONFLICT"} {
		if !strings.Contains(err.Error(), code) {
			t.Errorf("err = %v; want it to name %s", err, code)
		}
	}
}

// TestBuiltinProfilesPassSemanticValidation is the regression the shipped
// profiles need: catalog-embedded profiles are accepted at runtime without a
// CLI validate step, so a semantic fault in one has to fail the build.
func TestBuiltinProfilesPassSemanticValidation(t *testing.T) {
	names := profile.BuiltinNames()
	if len(names) == 0 {
		t.Fatal("BuiltinNames() is empty; the check would be vacuous")
	}
	for _, name := range names {
		p, ok := profile.BuiltinLookup(name)
		if !ok {
			t.Fatalf("BuiltinLookup(%q) = false after BuiltinNames listed it", name)
		}
		if err := profile.ValidateSemantics(p); err != nil {
			t.Errorf("builtin %q: ValidateSemantics = %v; want nil", name, err)
		}
	}
}

// TestResolveProfileRunsTheSemanticValidator closes the other half of
// docs/expression-profile-dsl.md:13: the validator has to run before a
// *project-local* or *user-global* profile is accepted, not only when an
// operator remembers to run `gum profile validate`. Nothing else stands
// between a file on disk and the runtime.
func TestResolveProfileRunsTheSemanticValidator(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".gum", "profiles")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	src := "format = \"json\"\nrecovery = \"resource_link\"\ntee_mode = \"off\"\n"
	if err := os.WriteFile(filepath.Join(dir, "bad.toml"), []byte(src), 0o600); err != nil {
		t.Fatalf("write profile: %v", err)
	}

	_, _, err := profile.ResolveProfile(root, "bad", nil)
	if err == nil {
		t.Fatal("ResolveProfile = nil error; want PROFILE_TEE_MODE_CONFLICT")
	}
	if !strings.Contains(err.Error(), "PROFILE_TEE_MODE_CONFLICT") {
		t.Errorf("err = %v; want PROFILE_TEE_MODE_CONFLICT", err)
	}
}

// TestResolveProfileUserGlobalRunsTheSemanticValidator is the same gate on the
// second resolution layer.
func TestResolveProfileUserGlobalRunsTheSemanticValidator(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	dir := filepath.Join(base, "gum", "profiles")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	src := "format = \"json\"\non_empty = \"" + strings.Repeat("a", 501) + "\"\n"
	if err := os.WriteFile(filepath.Join(dir, "bad.toml"), []byte(src), 0o600); err != nil {
		t.Fatalf("write profile: %v", err)
	}

	_, _, err := profile.ResolveProfile("", "bad", nil)
	if err == nil {
		t.Fatal("ResolveProfile = nil error; want ON_EMPTY_TOO_LONG")
	}
	if !strings.Contains(err.Error(), "ON_EMPTY_TOO_LONG") {
		t.Errorf("err = %v; want ON_EMPTY_TOO_LONG", err)
	}
}

// TestBuiltinProfilesPassStripNullsSafetyForTheirVariant is the build-time half
// of PROFILE_STRIP_NULLS_UNSAFE. The check needs a profile bound to a variant,
// and the catalog is where that binding exists: a variant's output_profile
// names the builtin. No variant ships null_elision_safe_fields today, so any
// builtin that turned strip_nulls on would fail here.
func TestBuiltinProfilesPassStripNullsSafetyForTheirVariant(t *testing.T) {
	var cat catalog.Catalog
	if err := json.Unmarshal(embedded.CatalogJSON, &cat); err != nil {
		t.Fatalf("unmarshal embedded catalog: %v", err)
	}

	bound := 0
	for i := range cat.Ops {
		for j := range cat.Ops[i].Variants {
			v := &cat.Ops[i].Variants[j]
			if v.OutputProfile == "" {
				continue
			}
			p, ok := profile.BuiltinLookup(v.OutputProfile)
			if !ok {
				// Plugin-supplied profiles resolve at install, not here.
				continue
			}
			bound++
			if err := profile.ValidateStripNullsSafety(p, v.NullElisionSafeFields); err != nil {
				t.Errorf("variant %s -> profile %s: %v", v.VariantID, v.OutputProfile, err)
			}
		}
	}
	if bound == 0 {
		t.Fatal("no catalog variant resolved to a builtin profile; the check would be vacuous")
	}
}
