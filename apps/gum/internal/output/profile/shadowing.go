// shadowing.go — §9.2 shadowing warning.
package profile

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// WarnOverrideDisablesLossyStage is the §9.2 warning class raised when a
// project-local or user-global override removes or weakens a loss-driving field
// the catalog-embedded profile set. It never blocks execution: a profile that
// saves fewer tokens is still a valid profile, and only the operator can decide
// whether that trade is wanted.
const WarnOverrideDisablesLossyStage = "OVERRIDE_DISABLES_LOSSY_STAGE"

// shadowSuppressFlag is the flag name the warning tells the operator to pass.
// The legacy alias --no-warn-recovery is accepted by the CLI but never named
// here, so the message points at the flag that survives.
const shadowSuppressFlag = "--no-warn-lossy"

// absentValue is the new value reported for a sub-table the override drops
// rather than sets. collapse_arrays, truncate_strings and dedupe have no "off"
// spelling: omitting the table is how a profile turns the stage off.
const absentValue = "absent"

// ShadowWarning is one §9.2 shadowing warning. Target names what the override
// applies to: the op_id or variant_id of an [override_bindings] key, or the
// profile name when a same-named file shadows a catalog-embedded profile.
type ShadowWarning struct {
	Class    string
	Target   string
	Field    string
	OldValue string
	NewValue string
}

// String renders the stderr / log line §9.2 fixes verbatim.
func (w ShadowWarning) String() string {
	return fmt.Sprintf(
		"Warning: profile override for %s sets %s=%s, removing a lossy-compression stage (was %s). "+
			"Token savings for this op may be reduced. Pass %s to suppress.",
		w.Target, w.Field, w.NewValue, w.OldValue, shadowSuppressFlag)
}

// DetectShadowing reports every loss-driving field the override removes or
// weakens relative to catalogProfile. Warnings come back in the order §9.2's
// trigger table lists the fields, so repeated runs over one pair print
// identical lines.
//
// An unset catalog value never triggers: a stage the catalog profile does not
// run is not a stage the override removed. That is what keeps the resolver from
// warning about every profile that declares no recovery.
func DetectShadowing(target string, catalogProfile, override *Profile) []ShadowWarning {
	if catalogProfile == nil || override == nil {
		return nil
	}

	var out []ShadowWarning
	add := func(field, oldValue, newValue string) {
		out = append(out, ShadowWarning{
			Class:    WarnOverrideDisablesLossyStage,
			Target:   target,
			Field:    field,
			OldValue: oldValue,
			NewValue: newValue,
		})
	}

	// An omitted recovery counts as a removal, not just an explicit "none".
	// Resolution replaces the catalog profile wholesale rather than merging it
	// field by field, and effectiveTeeMode treats an empty Recovery exactly as
	// "none": tee off, no artifact. The spec's table says "non-none catalog
	// value -> none override", and absence is how an operator writes that.
	if catalogProfile.Recovery != "" && catalogProfile.Recovery != RecoveryNone && recoveryIsOff(override.Recovery) {
		newValue := override.Recovery
		if newValue == "" {
			newValue = absentValue
		}
		add("recovery", catalogProfile.Recovery, newValue)
	}

	if isUpstreamMask(catalogProfile.FieldMaskMode) && override.FieldMaskMode == FieldMaskModeNone {
		add("field_mask_mode", catalogProfile.FieldMaskMode, FieldMaskModeNone)
	}

	if catalogProfile.StripNulls && !override.StripNulls {
		add("strip_nulls", "true", "false")
	}

	if catalogProfile.CollapseArrays != nil && override.CollapseArrays == nil {
		add("collapse_arrays", describeCollapseArrays(catalogProfile.CollapseArrays), absentValue)
	}

	if catalogProfile.TruncateStrings != nil && override.TruncateStrings == nil {
		add("truncate_strings", describeTruncateStrings(catalogProfile.TruncateStrings), absentValue)
	}

	if catalogProfile.Dedupe != nil && override.Dedupe == nil {
		add("dedupe", describeDedupe(catalogProfile.Dedupe), absentValue)
	}

	return out
}

// recoveryIsOff reports whether a profile's recovery field leaves no artifact
// behind. An empty value and an explicit "none" are the same thing downstream,
// which is why both count.
func recoveryIsOff(recovery string) bool {
	return recovery == "" || recovery == RecoveryNone
}

// isUpstreamMask reports whether mode sends a mask upstream, which is the half
// of the field_mask_mode enum the "none" override takes away.
func isUpstreamMask(mode string) bool {
	return mode == FieldMaskModeUpstream || mode == FieldMaskModeDualFetch
}

// describeCollapseArrays renders the stage the override dropped.
func describeCollapseArrays(spec *CollapseArraysSpec) string {
	return "max_items=" + strconv.Itoa(spec.MaxItems)
}

// describeTruncateStrings renders the stage the override dropped. Per-field
// limits are sorted by field name: a Go map iterates in random order, and a
// warning whose text changes between runs cannot be matched by a log filter.
func describeTruncateStrings(spec *TruncateStringsSpec) string {
	var parts []string
	if spec.DefaultChars > 0 {
		parts = append(parts, "default_chars="+strconv.Itoa(spec.DefaultChars))
	}

	fields := make([]string, 0, len(spec.Fields))
	for name := range spec.Fields {
		fields = append(fields, name)
	}
	sort.Strings(fields)

	for _, name := range fields {
		parts = append(parts, name+"="+strconv.Itoa(spec.Fields[name]))
	}

	if len(parts) == 0 {
		return "set"
	}

	return strings.Join(parts, ",")
}

// describeDedupe renders the stage the override dropped.
func describeDedupe(spec *DedupeSpec) string {
	if len(spec.By) == 0 {
		return "set"
	}

	return "by=" + strings.Join(spec.By, ",")
}
