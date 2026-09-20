package main

import (
	"errors"
	"fmt"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/output/profile"
)

// errOutputProfileNotFound reports a variant whose output_profile names no
// built-in expression profile. The generated catalog and the built-in profile
// set ship inside the same binary, so a name that misses BuiltinLookup at
// generation time is a dangling reference: nothing in the shipped artifact can
// ever resolve it, and the variant dispatches unshaped.
var errOutputProfileNotFound = errors.New("catalog: output_profile names no built-in expression profile")

// validateVariantProfiles is the spec §7 "Build" firing point for
// ON_EMPTY_TOO_LONG, PROFILE_TEE_MODE_CONFLICT and PROFILE_STRIP_NULLS_UNSAFE.
// Before this gate the three codes had no build-time producer at all: the only
// enforcement was TestBuiltinProfilesPassStripNullsSafetyForTheirVariant, a
// test that a generator run never consults.
//
// It resolves every variant's output_profile through lookup and runs the
// bound-profile checks against that variant's null_elision_safe_fields. Every
// failure is collected, so one run shows the whole list. main passes
// profile.BuiltinLookup; the parameter exists so tests can bind a profile the
// embedded built-in set does not ship.
func validateVariantProfiles(cat *catalog.Catalog, lookup func(string) (*profile.Profile, bool)) error {
	if cat == nil || lookup == nil {
		return nil
	}

	var errs []error

	for _, op := range cat.Ops {
		for _, v := range op.Variants {
			if v.OutputProfile == "" {
				continue
			}

			p, ok := lookup(v.OutputProfile)
			if !ok {
				errs = append(errs, fmt.Errorf("op %s: variant %s: output_profile %q: %w",
					op.OpID, v.VariantID, v.OutputProfile, errOutputProfileNotFound))
				continue
			}

			if err := profile.ValidateForVariant(p, v.NullElisionSafeFields); err != nil {
				errs = append(errs, fmt.Errorf("op %s: variant %s: output_profile %q: %w",
					op.OpID, v.VariantID, v.OutputProfile, err))
			}
		}
	}

	return errors.Join(errs...)
}

// validateGeneratedCatalog is the single gate every generator path runs before
// it writes a snapshot: the catalog ABI checks from Catalog.Validate plus the
// spec §7 build-time output-profile checks. The offline inject paths rewrite
// catalog.json without touching the network, so each one needs the same gate
// or a dangling profile reference re-enters the snapshot through a side door.
func validateGeneratedCatalog(cat *catalog.Catalog) error {
	if err := cat.Validate(); err != nil {
		return err
	}
	return validateVariantProfiles(cat, profile.BuiltinLookup)
}
