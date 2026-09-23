package catalog

// Spec §5.4 denylist gate for `risk_override_reason`.
//
// The field is curator-authored or plugin-authored prose that gum replays
// verbatim in three places: `catalog.json`, the `gum.describe_op` payload the
// model reads, and the audit log. `gum catalog list-overrides` prints it to a
// terminal. Nothing downstream re-checks it, so an ANSI escape or a bidi
// override written into a plugin manifest renders as control output on the
// operator's screen and as invisible text in the model's context. This file is
// the single gate; §5.4 places it before the 13-rule description sanitizer,
// which `cmd/gen-catalog` runs as a second pass over the validated value.

import (
	"errors"
	"fmt"
	"unicode/utf8"
)

// Sentinel errors for the two spec §7 registry codes that guard
// `risk_override_reason`.
var (
	// ErrRiskOverrideReasonInvalid marks a reason carrying a denylisted
	// character class or a length outside 1-200 codepoints.
	ErrRiskOverrideReasonInvalid = errors.New("catalog: RISK_OVERRIDE_REASON_INVALID")
	// ErrRiskOverrideMissingReason marks a variant that claims risk_override
	// without stating why. The override downgrades a risk class, so an
	// unexplained one silently weakens the §12 risk gate.
	ErrRiskOverrideMissingReason = errors.New("catalog: RISK_OVERRIDE_MISSING_REASON")
)

// MaxRiskOverrideReasonRunes is the spec §7 upper bound, in Unicode
// codepoints. It matches the `maxLength: 200` the audit-log schema declares
// for the same field, so a reason that passes here always fits the log row.
const MaxRiskOverrideReasonRunes = 200

// ValidateRiskOverrideReason applies the spec §5.4 denylist to one variant's
// reason string. It is exported because `gum catalog list-overrides` reads
// plugin-catalog.json straight off disk rather than through
// MergePluginVariants, so the command needs the same gate.
//
// An empty reason means the field is absent, which is the normal case: the
// shipped 228-op catalog carries no override at all. Absence is checked
// against `risk_override` by validateRiskOverride, not here, so this function
// accepts it and leaves the pairing rule to its caller.
func ValidateRiskOverrideReason(variantID, reason string) error {
	if reason == "" {
		return nil
	}

	if n := utf8.RuneCountInString(reason); n > MaxRiskOverrideReasonRunes {
		return fmt.Errorf("%w: risk_override_reason for variant '%s' is %d codepoints; the field must be between 1 and %d codepoints",
			ErrRiskOverrideReasonInvalid, variantID, n, MaxRiskOverrideReasonRunes)
	}

	for _, r := range reason {
		if !riskOverrideReasonRuneDenied(r) {
			continue
		}

		return fmt.Errorf("%w: risk_override_reason for variant '%s' contains a disallowed character (control code, zero-width character, bidirectional control character, or < / >); these characters are prohibited to prevent injection attacks",
			ErrRiskOverrideReasonInvalid, variantID)
	}

	return nil
}

// riskOverrideReasonRuneDenied reports whether r falls in one of the four
// denied classes spec §7 lists. Everything else is permitted, including
// letters outside ASCII, digits, spaces and ordinary punctuation.
func riskOverrideReasonRuneDenied(r rune) bool {
	switch {
	case r <= 0x1F, r == 0x7F: // ASCII control codes, incl. ESC (ANSI escapes).
		return true
	case r == 0x200B, r == 0x200C, r == 0x200D, r == 0x200E, r == 0x200F: // Zero-width and LRM/RLM.
		return true
	case r == 0x2028, r == 0x2029, r == 0xFEFF: // Line/paragraph separator, BOM.
		return true
	case r == 0x061C: // Arabic letter mark.
		return true
	case r >= 0x202A && r <= 0x202E: // Bidi embeddings and overrides.
		return true
	case r >= 0x2066 && r <= 0x2069: // Bidi isolates.
		return true
	case r == '<', r == '>': // HTML-like injection in downstream rendering.
		return true
	}

	return false
}

// validateRiskOverride checks both §5.4 rules a variant's override must satisfy:
// a set `risk_override` requires a non-empty reason, and any reason present
// must clear the denylist.
func validateRiskOverride(variantID string, override bool, reason string) error {
	if override && reason == "" {
		return fmt.Errorf("%w: variant '%s' sets risk_override with no risk_override_reason",
			ErrRiskOverrideMissingReason, variantID)
	}

	return ValidateRiskOverrideReason(variantID, reason)
}
