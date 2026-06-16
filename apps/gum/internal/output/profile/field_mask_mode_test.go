// Acceptance test for the spec §9.1 dual_fetch gate. The catalog generator
// rejects ineligible variants at build time; this file pins the runtime
// helper that internal/dispatch invokes after resolveVariant so profile
// overrides authored independently from the catalog don't slip through.

package profile_test

import (
	"errors"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/output/profile"
)

// TestDualFetchReadOnlyIdempotentGate pins the §9.1 gate matrix:
//   - mode != dual_fetch → always nil
//   - mode == dual_fetch + read + idempotent → nil
//   - mode == dual_fetch + non-read OR non-idempotent → ErrDualFetchGateRejected
func TestDualFetchReadOnlyIdempotentGate(t *testing.T) {
	t.Parallel()

	read := &catalog.Variant{VariantID: "v.read.idem", RiskClass: catalog.RiskClassRead, Annotations: &catalog.Annotation{Idempotent: true}}
	readNonIdem := &catalog.Variant{VariantID: "v.read.nonidem", RiskClass: catalog.RiskClassRead, Annotations: &catalog.Annotation{Idempotent: false}}
	readMissingAnn := &catalog.Variant{VariantID: "v.read.noann", RiskClass: catalog.RiskClassRead}
	write := &catalog.Variant{VariantID: "v.write.idem", RiskClass: catalog.RiskClassWrite, Annotations: &catalog.Annotation{Idempotent: true}}
	destructive := &catalog.Variant{VariantID: "v.dest.idem", RiskClass: catalog.RiskClassDestructive, Annotations: &catalog.Annotation{Idempotent: true}}

	cases := []struct {
		name    string
		mode    string
		variant *catalog.Variant
		wantErr bool
	}{
		{"empty mode + nil variant", "", nil, false},
		{"empty mode + write variant", "", write, false},
		{"upstream mode + write variant", profile.FieldMaskModeUpstream, write, false},
		{"none mode + write variant", profile.FieldMaskModeNone, write, false},
		{"dual_fetch + read + idempotent", profile.FieldMaskModeDualFetch, read, false},
		{"dual_fetch + read + non-idempotent", profile.FieldMaskModeDualFetch, readNonIdem, true},
		{"dual_fetch + read + missing annotations", profile.FieldMaskModeDualFetch, readMissingAnn, true},
		{"dual_fetch + write + idempotent", profile.FieldMaskModeDualFetch, write, true},
		{"dual_fetch + destructive + idempotent", profile.FieldMaskModeDualFetch, destructive, true},
		{"dual_fetch + nil variant", profile.FieldMaskModeDualFetch, nil, true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := profile.ValidateDualFetchGate(tc.mode, tc.variant)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ValidateDualFetchGate(%q, %v) = nil; want ErrDualFetchGateRejected", tc.mode, tc.variant)
				}
				if !errors.Is(err, profile.ErrDualFetchGateRejected) {
					t.Errorf("err = %v; want errors.Is(err, ErrDualFetchGateRejected)", err)
				}
				return
			}
			if err != nil {
				t.Errorf("ValidateDualFetchGate(%q, %v) = %v; want nil", tc.mode, tc.variant, err)
			}
		})
	}
}

// TestFieldMaskModeParserAcceptsValidEnum confirms the parser accepts each
// valid mode and rejects unknown values + bare identifiers.
func TestFieldMaskModeParserAcceptsValidEnum(t *testing.T) {
	t.Parallel()

	cases := []struct {
		raw     string
		want    string
		wantErr bool
	}{
		{`field_mask_mode = "upstream"`, "upstream", false},
		{`field_mask_mode = "dual_fetch"`, "dual_fetch", false},
		{`field_mask_mode = "none"`, "none", false},
		{`field_mask_mode = "shadow"`, "", true},
		{`field_mask_mode = upstream`, "", true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.raw, func(t *testing.T) {
			t.Parallel()
			got, err := profile.Parse(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("Parse(%q) = (%v, nil); want error", tc.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.raw, err)
			}
			if got.FieldMaskMode != tc.want {
				t.Errorf("FieldMaskMode=%q; want %q", got.FieldMaskMode, tc.want)
			}
		})
	}
}

// TestFieldMaskModeMergeOverridesEmpty proves MergeProfiles preserves the
// first-declared FieldMaskMode (presentation-layer precedence: project-local
// beats user-global beats catalog-embedded).
func TestFieldMaskModeMergeOverridesEmpty(t *testing.T) {
	t.Parallel()

	high := &profile.Profile{FieldMaskMode: "dual_fetch"}
	low := &profile.Profile{FieldMaskMode: "upstream"}

	merged := profile.MergeProfiles(high, low)
	if merged.FieldMaskMode != "dual_fetch" {
		t.Errorf("merged.FieldMaskMode=%q; want dual_fetch (high precedence wins)", merged.FieldMaskMode)
	}

	merged2 := profile.MergeProfiles(&profile.Profile{}, low)
	if merged2.FieldMaskMode != "upstream" {
		t.Errorf("merged2.FieldMaskMode=%q; want upstream (empty falls through to lower layer)", merged2.FieldMaskMode)
	}
}

// TestFieldMaskModeRoundTrip pins Serialize → Parse fidelity for the new key.
func TestFieldMaskModeRoundTrip(t *testing.T) {
	t.Parallel()

	original := &profile.Profile{FieldMaskMode: "dual_fetch"}
	src := original.Serialize()
	got, err := profile.Parse(src)
	if err != nil {
		t.Fatalf("re-parse %q: %v", src, err)
	}
	if got.FieldMaskMode != "dual_fetch" {
		t.Errorf("round-trip FieldMaskMode=%q; want dual_fetch", got.FieldMaskMode)
	}
}
