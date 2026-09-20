// semantics.go — the cross-field profile validators that the line parser
// cannot run.
//
// docs/expression-profile-dsl.md:13 requires a Go implementation to run the
// structural validator *and* the semantic validator before it accepts a
// catalog, plugin, user-global or project-local profile. Parse is the
// structural half: it checks syntax, enum membership and value types one key at
// a time. The checks here read two keys against each other, or read a value
// against a limit the schema deliberately cannot express.
//
// ValidateStripNullsSafety (strip_nulls_safety.go) is the third validator. It
// stays separate because it needs the bound variant's null_elision_safe_fields,
// which a standalone profile file does not carry.
package profile

import (
	"errors"
	"fmt"
	"sort"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// OnEmptyMaxCodepoints caps on_empty (docs/spec.md:2103). The cap keeps the
// message inside a reasonable token budget and stops a profile field being
// repurposed as a freeform documentation blob. It counts codepoints after NFC
// normalization, which is why expression-profile-dsl.json carries no literal
// maxLength: JSON Schema counts raw codepoints.
const OnEmptyMaxCodepoints = 500

// Recovery values (docs/expression-profile-dsl.md Field Reference).
const (
	RecoveryNone          = "none"
	RecoveryLocalArtifact = "local_artifact"
	RecoveryResourceLink  = "resource_link"
)

// Tee-mode values (docs/expression-profile-dsl.md Field Reference).
const (
	TeeModeOff      = "off"
	TeeModeFailures = "failures"
	TeeModeAlways   = "always"
)

var (
	// ErrOnEmptyTooLong is the sentinel for spec §7 error code
	// ON_EMPTY_TOO_LONG (docs/spec.md:1584).
	ErrOnEmptyTooLong = errors.New("ON_EMPTY_TOO_LONG")

	// ErrProfileTeeModeConflict is the sentinel for spec §7 error code
	// PROFILE_TEE_MODE_CONFLICT (docs/spec.md:1583).
	ErrProfileTeeModeConflict = errors.New("PROFILE_TEE_MODE_CONFLICT")

	// ErrOverrideBindingInvalid is the sentinel for spec §7 error code
	// OVERRIDE_BINDING_INVALID (docs/spec.md:1581).
	ErrOverrideBindingInvalid = errors.New("OVERRIDE_BINDING_INVALID")
)

// ValidateOverrideBindings checks a file's [override_bindings] table against
// docs/expression-profile-dsl.md rules 1 and 2: every key names an op_id or
// variant_id the caller's catalog view carries, and every value names a profile
// that resolves. It reports all failures at once.
//
// targetKnown and profileKnown may be nil, which skips that half of the check.
// A caller with no catalog snapshot to hand (an offline lint, say) passes nil
// and MUST say the key check was skipped, because a dangling key is exactly the
// fault this code names.
func ValidateOverrideBindings(f *File, targetKnown func(id string) bool, profileKnown func(name string) bool) error {
	if f == nil || len(f.OverrideBindings) == 0 {
		return nil
	}
	targets := make([]string, 0, len(f.OverrideBindings))
	for target := range f.OverrideBindings {
		targets = append(targets, target)
	}
	sort.Strings(targets)

	var errs []error
	for _, target := range targets {
		name := f.OverrideBindings[target]
		if targetKnown != nil && !targetKnown(target) {
			errs = append(errs, fmt.Errorf("%w: override_bindings key %q is not an op_id or variant_id in the catalog",
				ErrOverrideBindingInvalid, target))
		}
		if profileKnown != nil && !profileKnown(name) {
			errs = append(errs, fmt.Errorf("%w: override_bindings[%q] names profile %q, which does not resolve in this file, the project-local layer, the user-global layer, or the catalog",
				ErrOverrideBindingInvalid, target, name))
		}
	}
	return errors.Join(errs...)
}

// ValidateSemantics runs every profile check that needs no bound variant. It
// reports all failures at once, so an operator fixing a profile sees the whole
// list instead of one fault per run.
//
// A nil profile passes: "no profile" is a valid state everywhere a profile is
// optional, and rejecting it here would turn an absent profile into an error.
func ValidateSemantics(p *Profile) error {
	if p == nil {
		return nil
	}

	var errs []error

	if n := utf8.RuneCountInString(NormalizeOnEmpty(p.OnEmpty)); n > OnEmptyMaxCodepoints {
		errs = append(errs, fmt.Errorf("%w: on_empty is %d codepoints after NFC normalization; the cap is %d",
			ErrOnEmptyTooLong, n, OnEmptyMaxCodepoints))
	}

	// A resource link is a handle to an artifact. tee_mode "off" writes no
	// artifact and "failures" writes one only on an upstream error, so either
	// one hands the caller a URI that resolves to nothing on the success path
	// the link exists for (docs/spec.md:1953). An unset tee_mode is not a
	// conflict: it defaults to "always" whenever recovery is not "none".
	if p.Recovery == RecoveryResourceLink && (p.TeeMode == TeeModeOff || p.TeeMode == TeeModeFailures) {
		errs = append(errs, fmt.Errorf("%w: recovery=%q needs a guaranteed backing artifact, so tee_mode=%q is invalid; set tee_mode=%q or leave it unset",
			ErrProfileTeeModeConflict, RecoveryResourceLink, p.TeeMode, TeeModeAlways))
	}

	return errors.Join(errs...)
}

// NormalizeOnEmpty returns s in NFC form. docs/expression-profile-dsl.md:88
// makes NFC the propagated form: the value the runtime carries in
// _expression.on_empty_message is normalized, not the raw TOML author value.
// Parse applies it, so the length check and the runtime message read the same
// string. NFC is idempotent, so applying it twice is safe.
func NormalizeOnEmpty(s string) string {
	if s == "" {
		return ""
	}
	return norm.NFC.String(s)
}
