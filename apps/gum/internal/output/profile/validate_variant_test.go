package profile_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/output/profile"
)

// TestValidateForVariantJoinsBothValidators keeps the bound-variant entry point
// honest about its contract: one call reports every fault a bound profile can
// have, not the first one. cmd/gen-catalog and `gum profile validate --variant`
// both go through it, so a check that silently dropped out here would vanish
// from both build-time gates at once.
func TestValidateForVariantJoinsBothValidators(t *testing.T) {
	p := &profile.Profile{
		Name:       "both",
		Recovery:   profile.RecoveryResourceLink,
		TeeMode:    profile.TeeModeOff,
		StripNulls: true,
		KeepFields: []string{"results.text"},
	}

	err := profile.ValidateForVariant(p, []string{"results.id"})
	if err == nil {
		t.Fatal("ValidateForVariant err=nil; want both a tee-mode conflict and a strip_nulls fault")
	}
	if !errors.Is(err, profile.ErrProfileTeeModeConflict) {
		t.Errorf("err=%v; want it to wrap ErrProfileTeeModeConflict", err)
	}
	if !errors.Is(err, profile.ErrProfileStripNullsUnsafe) {
		t.Errorf("err=%v; want it to wrap ErrProfileStripNullsUnsafe", err)
	}
	if !strings.Contains(err.Error(), "results.text") {
		t.Errorf("err=%v; want the offending keep_fields entry named", err)
	}
}

// TestValidateForVariantPasses covers the two clean outcomes: a nil profile and
// a bound profile whose keep_fields are inside the variant's safe set.
func TestValidateForVariantPasses(t *testing.T) {
	if err := profile.ValidateForVariant(nil, nil); err != nil {
		t.Errorf("ValidateForVariant(nil, nil) = %v; want nil", err)
	}

	p := &profile.Profile{
		Name:       "safe",
		StripNulls: true,
		KeepFields: []string{"results.text"},
	}
	if err := profile.ValidateForVariant(p, []string{"results.text", "results.id"}); err != nil {
		t.Errorf("ValidateForVariant(safe profile) = %v; want nil", err)
	}
}

// TestValidateForVariantReportsSemanticsAlone proves the semantics half still
// fires when the strip_nulls half has nothing to say, so a variant with no
// null_elision_safe_fields cannot mask an on_empty or tee_mode fault.
func TestValidateForVariantReportsSemanticsAlone(t *testing.T) {
	p := &profile.Profile{
		Name:     "semantics-only",
		Recovery: profile.RecoveryResourceLink,
		TeeMode:  profile.TeeModeFailures,
	}

	err := profile.ValidateForVariant(p, nil)
	if !errors.Is(err, profile.ErrProfileTeeModeConflict) {
		t.Fatalf("err=%v; want ErrProfileTeeModeConflict", err)
	}
	if errors.Is(err, profile.ErrProfileStripNullsUnsafe) {
		t.Errorf("err=%v; strip_nulls is off, so that check must stay silent", err)
	}
}
