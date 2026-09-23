package main

import (
	"path/filepath"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/output/profile"
	"github.com/spf13/cobra"
)

// TestBoolFlagMissingFlag pins boolFlag's two defensive paths. A bare command
// has no root persistent flags, and a command whose root never registered the
// flag returns an error from GetBool; both mean "not suppressed" rather than a
// panic, because the runtime loader reads this on every dispatch.
func TestBoolFlagMissingFlag(t *testing.T) {
	if boolFlag(nil, shadowWarnFlag) {
		t.Error("boolFlag(nil) = true; want false")
	}
	bare := &cobra.Command{Use: "bare"}
	if boolFlag(bare, shadowWarnFlag) {
		t.Error("boolFlag on a command with no such flag = true; want false")
	}
}

// TestFileShadowWarningsNilFile pins the nil guard: profile validate calls this
// after parsing, and a parse that produced nothing has nothing to compare.
func TestFileShadowWarningsNilFile(t *testing.T) {
	if got := fileShadowWarnings("x.toml", nil); got != nil {
		t.Errorf("fileShadowWarnings(nil) = %v; want nil", got)
	}
}

// TestCatalogProfileForTargetNilCatalog pins the no-catalog build: a binary
// without an embedded catalog cannot say what a binding displaces, so it warns
// about nothing rather than guessing.
func TestCatalogProfileForTargetNilCatalog(t *testing.T) {
	if _, ok := catalogProfileForTarget(nil, "svc.res.get"); ok {
		t.Error("catalogProfileForTarget(nil) reported a profile")
	}
}

// TestCatalogProfileForTargetVariantID pins the variant-precise branch: §9.2
// allows a binding keyed on a variant_id, and that variant's own output_profile
// is the baseline, not the default variant's.
func TestCatalogProfileForTargetVariantID(t *testing.T) {
	name := builtinLossyProfile(t)
	c := &catalog.Catalog{Ops: []catalog.Op{{
		OpID:             "svc.res.get",
		DefaultVariantID: "svc.v1.res.get",
		Variants: []catalog.Variant{
			{VariantID: "svc.v1.res.get", OutputProfile: ""},
			{VariantID: "svc.v2.res.get", OutputProfile: name},
		},
	}}}

	got, ok := catalogProfileForTarget(c, "svc.v2.res.get")
	if !ok {
		t.Fatalf("variant_id %q did not resolve a catalog profile", "svc.v2.res.get")
	}
	if got.Name != name {
		t.Errorf("profile = %q; want %q", got.Name, name)
	}

	if _, ok := catalogProfileForTarget(c, "svc.res.get"); ok {
		t.Error("default variant names no profile; want no baseline")
	}
	if _, ok := catalogProfileForTarget(c, "svc.unknown.get"); ok {
		t.Error("unknown target reported a profile")
	}
}

// TestResolveBoundProfileFallsBackToHierarchy pins the second half of binding
// resolution: §9.2 says the bound name "MUST resolve through the same
// three-level hierarchy", so a name no sibling defines still resolves from the
// catalog layer, and a name nothing defines resolves to nil.
func TestResolveBoundProfileFallsBackToHierarchy(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	name := builtinLossyProfile(t)
	root := t.TempDir()
	f := &profile.File{}

	got := resolveBoundProfile(root, f, name)
	if got == nil {
		t.Fatalf("builtin %q did not resolve through the hierarchy", name)
	}
	if got.Name != name {
		t.Errorf("profile = %q; want %q", got.Name, name)
	}

	if got := resolveBoundProfile(root, f, "no.such.profile.v1"); got != nil {
		t.Errorf("resolveBoundProfile of an unknown name = %+v; want nil", got)
	}
}

// TestFileShadowWarningsSkipsUnknownBindingTarget pins the skip: a binding on an
// op the catalog does not carry has no baseline to compare, and §9.2 leaves that
// case to OVERRIDE_BINDING_INVALID in validateFileBindings.
func TestFileShadowWarningsSkipsUnknownBindingTarget(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	name := builtinLossyProfile(t)
	f := &profile.File{
		OverrideBindings: map[string]string{"svc.absent.get": name},
	}

	if got := fileShadowWarnings(filepath.Join(t.TempDir(), "p.toml"), f); len(got) != 0 {
		t.Errorf("got %d warnings for an unknown target; want 0: %v", len(got), got)
	}
}

// TestWarnIfBindingsShadowSkipsUnresolvableProfile pins the runtime loader's
// skip: a binding naming a profile that no layer defines cannot be compared, and
// the loader must not fail the call over it.
func TestWarnIfBindingsShadowSkipsUnresolvableProfile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	target, _ := shadowableProfile(t)
	buf := captureShadowStderr(t)

	warnIfBindingsShadow(t.TempDir(), map[string]string{target: "no.such.profile.v1"})
	warnIfBindingsShadow(t.TempDir(), nil)

	if got := buf.String(); got != "" {
		t.Errorf("stderr = %q; want silence when the bound profile does not resolve", got)
	}
}
