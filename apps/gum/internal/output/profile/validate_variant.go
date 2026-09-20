package profile

import "errors"

// ValidateForVariant runs every expression-profile check that a bound variant
// makes possible: the variant-independent ones from ValidateSemantics plus the
// strip_nulls safety check, which needs the variant's null_elision_safe_fields.
// Both run, so one call reports the whole list instead of one fault per run.
//
// It is the single entry point for the spec §7 "Build" firing point of
// ON_EMPTY_TOO_LONG, PROFILE_TEE_MODE_CONFLICT and PROFILE_STRIP_NULLS_UNSAFE,
// so cmd/gen-catalog and `gum profile validate --variant` cannot drift apart on
// which checks a bound profile gets.
//
// A nil profile passes, for the same reason ValidateSemantics accepts one: an
// absent profile is a valid state wherever a profile is optional.
func ValidateForVariant(p *Profile, nullElisionSafeFields []string) error {
	return errors.Join(
		ValidateSemantics(p),
		ValidateStripNullsSafety(p, nullElisionSafeFields),
	)
}
