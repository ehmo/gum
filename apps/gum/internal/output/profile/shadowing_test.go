package profile

import (
	"strings"
	"testing"
)

// The §9.2 shadowing warning. `gum profile validate` and the runtime profile
// loader emit OVERRIDE_DISABLES_LOSSY_STAGE whenever a project-local or
// user-global override removes or weakens a loss-driving field relative to the
// catalog-embedded profile. One test per row of the spec's trigger table.

func TestShadowingRecoveryDisabled(t *testing.T) {
	got := DetectShadowing("gmail.messages.list",
		&Profile{Recovery: "resource_link"},
		&Profile{Recovery: "none"})

	if len(got) != 1 {
		t.Fatalf("warnings = %v; want 1", got)
	}
	w := got[0]
	if w.Class != WarnOverrideDisablesLossyStage {
		t.Errorf("Class = %q; want %q", w.Class, WarnOverrideDisablesLossyStage)
	}
	if w.Field != "recovery" || w.OldValue != "resource_link" || w.NewValue != "none" {
		t.Errorf("field/old/new = %q/%q/%q; want recovery/resource_link/none", w.Field, w.OldValue, w.NewValue)
	}
	const want = "Warning: profile override for gmail.messages.list sets recovery=none, " +
		"removing a lossy-compression stage (was resource_link). Token savings for this op " +
		"may be reduced. Pass --no-warn-lossy to suppress."
	if w.String() != want {
		t.Errorf("String() =\n%q\nwant\n%q", w.String(), want)
	}
}

func TestShadowingFieldMaskModeDisabled(t *testing.T) {
	for _, catalogValue := range []string{"upstream", "dual_fetch"} {
		got := DetectShadowing("t",
			&Profile{FieldMaskMode: catalogValue},
			&Profile{FieldMaskMode: "none"})
		if len(got) != 1 || got[0].Field != "field_mask_mode" || got[0].OldValue != catalogValue {
			t.Fatalf("catalog %q: warnings = %v; want one field_mask_mode warning", catalogValue, got)
		}
	}
}

func TestShadowingStripNullsDisabled(t *testing.T) {
	got := DetectShadowing("t", &Profile{StripNulls: true}, &Profile{StripNulls: false})
	if len(got) != 1 || got[0].Field != "strip_nulls" {
		t.Fatalf("warnings = %v; want one strip_nulls warning", got)
	}
	if got[0].OldValue != "true" || got[0].NewValue != "false" {
		t.Errorf("old/new = %q/%q; want true/false", got[0].OldValue, got[0].NewValue)
	}
}

func TestShadowingCollapseArraysDropped(t *testing.T) {
	got := DetectShadowing("t", &Profile{CollapseArrays: &CollapseArraysSpec{MaxItems: 5}}, &Profile{})
	if len(got) != 1 || got[0].Field != "collapse_arrays" {
		t.Fatalf("warnings = %v; want one collapse_arrays warning", got)
	}
	if got[0].OldValue != "max_items=5" || got[0].NewValue != "absent" {
		t.Errorf("old/new = %q/%q; want max_items=5/absent", got[0].OldValue, got[0].NewValue)
	}
}

func TestShadowingTruncateStringsDropped(t *testing.T) {
	got := DetectShadowing("t", &Profile{TruncateStrings: &TruncateStringsSpec{
		DefaultChars: 200,
		Fields:       map[string]int{"snippet": 80, "body": 40},
	}}, &Profile{})
	if len(got) != 1 || got[0].Field != "truncate_strings" {
		t.Fatalf("warnings = %v; want one truncate_strings warning", got)
	}
	// Field limits are sorted so the message is identical on every run.
	if got[0].OldValue != "default_chars=200,body=40,snippet=80" {
		t.Errorf("OldValue = %q", got[0].OldValue)
	}
}

func TestShadowingDedupeDropped(t *testing.T) {
	got := DetectShadowing("t", &Profile{Dedupe: &DedupeSpec{By: []string{"thread_id", "id"}}}, &Profile{})
	if len(got) != 1 || got[0].Field != "dedupe" {
		t.Fatalf("warnings = %v; want one dedupe warning", got)
	}
	if got[0].OldValue != "by=thread_id,id" {
		t.Errorf("OldValue = %q; want by=thread_id,id", got[0].OldValue)
	}
}

// An override that keeps every loss-driving field, or strengthens one, is
// silent. The warning exists to report lost savings, not to report any diff.
func TestShadowingSilentWhenNothingWeakens(t *testing.T) {
	catalogProfile := &Profile{
		Recovery:        "local_artifact",
		FieldMaskMode:   "upstream",
		StripNulls:      true,
		CollapseArrays:  &CollapseArraysSpec{MaxItems: 5},
		TruncateStrings: &TruncateStringsSpec{DefaultChars: 100},
		Dedupe:          &DedupeSpec{By: []string{"id"}},
	}
	override := &Profile{
		Recovery:        "resource_link",
		FieldMaskMode:   "dual_fetch",
		StripNulls:      true,
		CollapseArrays:  &CollapseArraysSpec{MaxItems: 2},
		TruncateStrings: &TruncateStringsSpec{DefaultChars: 50},
		Dedupe:          &DedupeSpec{By: []string{"id", "thread_id"}},
	}
	if got := DetectShadowing("t", catalogProfile, override); len(got) != 0 {
		t.Fatalf("warnings = %v; want none", got)
	}
}

// An unset catalog value is not a lossy stage, so removing nothing warns about
// nothing. Without this the resolver would warn on every profile that declares
// no recovery at all.
func TestShadowingIgnoresUnsetCatalogValues(t *testing.T) {
	if got := DetectShadowing("t", &Profile{}, &Profile{Recovery: "none", FieldMaskMode: "none"}); len(got) != 0 {
		t.Fatalf("warnings = %v; want none", got)
	}
}

func TestShadowingNilProfilesAreSilent(t *testing.T) {
	if got := DetectShadowing("t", nil, &Profile{}); got != nil {
		t.Errorf("nil catalog profile: warnings = %v; want nil", got)
	}
	if got := DetectShadowing("t", &Profile{}, nil); got != nil {
		t.Errorf("nil override: warnings = %v; want nil", got)
	}
}

// Every trigger fires independently, and the order follows the spec's table so
// two runs over the same pair print the same lines.
func TestShadowingReportsEveryTriggerInSpecOrder(t *testing.T) {
	catalogProfile := &Profile{
		Recovery:        "resource_link",
		FieldMaskMode:   "upstream",
		StripNulls:      true,
		CollapseArrays:  &CollapseArraysSpec{MaxItems: 5},
		TruncateStrings: &TruncateStringsSpec{DefaultChars: 100},
		Dedupe:          &DedupeSpec{By: []string{"id"}},
	}
	override := &Profile{Recovery: "none", FieldMaskMode: "none"}

	got := DetectShadowing("gmail.messages.list", catalogProfile, override)
	want := []string{"recovery", "field_mask_mode", "strip_nulls", "collapse_arrays", "truncate_strings", "dedupe"}
	if len(got) != len(want) {
		t.Fatalf("warnings = %d; want %d: %v", len(got), len(want), got)
	}
	for i, field := range want {
		if got[i].Field != field {
			t.Errorf("warning %d field = %q; want %q", i, got[i].Field, field)
		}
		if !strings.Contains(got[i].String(), "Pass --no-warn-lossy to suppress.") {
			t.Errorf("warning %d omits the suppression hint: %s", i, got[i].String())
		}
	}
}

// TestShadowingRecoveryOmittedIsARemoval pins the common shape of a weaker
// override: it omits `recovery` rather than writing `recovery = "none"`.
// Resolution replaces the catalog profile wholesale, it does not merge field by
// field, and effectiveTeeMode in internal/dispatch treats an empty Recovery
// exactly as "none" (tee off, no artifact). So the omission removes the stage
// and has to warn, or the warning misses the case operators actually write.
func TestShadowingRecoveryOmittedIsARemoval(t *testing.T) {
	catalogProfile := &Profile{Name: "p", Recovery: RecoveryResourceLink}
	override := &Profile{Name: "p"}

	got := DetectShadowing("gmail.messages.list", catalogProfile, override)
	if len(got) != 1 {
		t.Fatalf("got %d warnings; want 1: %+v", len(got), got)
	}
	if got[0].Field != "recovery" {
		t.Errorf("field = %q; want recovery", got[0].Field)
	}
	if got[0].OldValue != RecoveryResourceLink {
		t.Errorf("old = %q; want %q", got[0].OldValue, RecoveryResourceLink)
	}
	if got[0].NewValue != absentValue {
		t.Errorf("new = %q; want %q", got[0].NewValue, absentValue)
	}
}

// TestShadowingFieldMaskModeOmittedIsNotARemoval pins the asymmetry. An empty
// field_mask_mode is documented as the "upstream" default (field_mask_mode.go),
// and dispatch applies the mask unless the value is "none", so omitting the
// field keeps the stage. Only an explicit "none" weakens it.
func TestShadowingFieldMaskModeOmittedIsNotARemoval(t *testing.T) {
	catalogProfile := &Profile{Name: "p", FieldMaskMode: FieldMaskModeUpstream}
	override := &Profile{Name: "p"}

	if got := DetectShadowing("gmail.messages.list", catalogProfile, override); len(got) != 0 {
		t.Errorf("got %d warnings for an omitted field_mask_mode; want 0: %+v", len(got), got)
	}
}
