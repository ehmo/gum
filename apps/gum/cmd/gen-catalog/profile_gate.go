package main

import (
	"errors"
	"fmt"
	"slices"

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

// errDefaultVariantInvalid is the spec §7 build-time code DEFAULT_VARIANT_INVALID
// (docs/spec.md line 1580). The message is the code itself, matching
// ErrProfileStripNullsUnsafe, so a generator failure prints the contract name
// the spec tells operators to look up.
var errDefaultVariantInvalid = errors.New("DEFAULT_VARIANT_INVALID")

// variantIsExecutable reports whether v executes at least one declared
// capability atom through generic dispatch (§918). An omitted execution_support
// means "full", which Op.Validate also assumes.
//
// This is what makes a variant an alternative worth promoting. A
// typed_executor_required or schema_only variant answers every invocation with
// UNSUPPORTED_CAPABILITY, so picking one over a deprecated default would trade
// a warning for an outright failure.
func variantIsExecutable(v catalog.Variant) bool {
	switch v.ExecutionSupport {
	case "", catalog.ExecutionSupportFull, catalog.ExecutionSupportPartial:
		return true
	default:
		return false
	}
}

// validateDefaultVariantLifecycle enforces the spec §5.1 lifecycle-aware
// default rule (docs/spec.md line 434) at generation time.
//
// Two halves had no producer before this gate. A quarantined default is never
// allowed: the rule is absolute and carries no grace clause. A deprecated
// default is allowed only when the op has no non-deprecated executable variant
// to promote, which is the 90-day grace window the rule describes.
//
// The third half, a removed default, is already covered: Op.Validate rejects a
// default_variant_id that names no entry in variants[], and a removed variant
// is exactly one that is no longer there.
//
// Every violation is collected so one generator run shows the whole list.
func validateDefaultVariantLifecycle(cat *catalog.Catalog) error {
	if cat == nil {
		return nil
	}

	var errs []error

	for _, op := range cat.Ops {
		if op.DefaultVariantID == "" {
			continue // Op.Validate owns the empty case.
		}

		var def *catalog.Variant
		for i := range op.Variants {
			if op.Variants[i].VariantID == op.DefaultVariantID {
				def = &op.Variants[i]
				break
			}
		}
		if def == nil {
			continue // Op.Validate owns the dangling case.
		}

		if def.Quarantined {
			errs = append(errs, fmt.Errorf("op %s: default_variant_id %s is quarantined: %w",
				op.OpID, def.VariantID, errDefaultVariantInvalid))
			continue
		}

		if !slices.Contains(op.DeprecatedVariantIDs, def.VariantID) {
			continue
		}

		// A deprecated default stands only while no live executable variant
		// exists. Name the first alternative so the generator author sees what
		// the default should have been.
		for _, v := range op.Variants {
			if v.VariantID == def.VariantID || v.Quarantined {
				continue
			}
			if slices.Contains(op.DeprecatedVariantIDs, v.VariantID) || !variantIsExecutable(v) {
				continue
			}
			errs = append(errs, fmt.Errorf("op %s: default_variant_id %s is deprecated while %s is live and executable: %w",
				op.OpID, def.VariantID, v.VariantID, errDefaultVariantInvalid))
			break
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
	if err := validateDefaultVariantLifecycle(cat); err != nil {
		return err
	}
	return validateVariantProfiles(cat, profile.BuiltinLookup)
}
