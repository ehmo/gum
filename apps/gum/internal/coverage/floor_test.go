package coverage

import (
	"strings"
	"testing"
)

// TestFloorIs85 pins the canonical floor at 85% per the bead. Lowering
// FloorPercent without a matching docs/test-matrix.md and bead update
// is a regression and MUST break this test.
func TestFloorIs85(t *testing.T) {
	if got, want := FloorPercent, 85.0; got != want {
		t.Fatalf("FloorPercent: got %.1f want %.1f", got, want)
	}
}

// TestRatchetEntriesHaveBeadReferences verifies every retention baseline
// points at an owning bead and carries a valid percentage. Under the
// gum-5wkg retention model a baseline Min may sit above OR below
// FloorPercent (it records the level actually held), so the only bound is
// the open percentage range (0, 100]. Stripping the Bead field is a
// regression because the baseline's whole point is a trackable owner.
func TestRatchetEntriesHaveBeadReferences(t *testing.T) {
	for _, r := range Ratchets {
		if r.Bead == "" {
			t.Errorf("ratchet entry for %s missing Bead reference", r.Package)
		}
		if r.Min <= 0 || r.Min > 100 {
			t.Errorf("ratchet entry %s has Min=%.1f outside (0, 100]", r.Package, r.Min)
		}
	}
}

// TestRatchetEntriesAreUnique guards against a copy-paste duplicate package
// in the retention table, which would let the first entry silently shadow
// the second in Threshold's linear scan.
func TestRatchetEntriesAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range Ratchets {
		if seen[r.Package] {
			t.Errorf("duplicate ratchet entry for %s", r.Package)
		}
		seen[r.Package] = true
	}
}

// TestThresholdPicksRatchet verifies Threshold returns the ratcheted
// value for an allowlisted package and FloorPercent for one that is
// not allowlisted. Uses a synthetic ratchet so the test stays meaningful
// when the production Ratchets slice is empty.
func TestThresholdPicksRatchet(t *testing.T) {
	withSyntheticRatchets(t, []Ratchet{
		{Package: "synthetic/pkg", Min: 70.0, Bead: "test-only"},
	})
	if got, want := Threshold("synthetic/pkg"), 70.0; got != want {
		t.Errorf("ratcheted threshold: got %.1f want %.1f", got, want)
	}
	if got, want := Threshold("github.com/ehmo/gum/internal/dispatch"), FloorPercent; got != want {
		t.Errorf("default threshold: got %.1f want %.1f", got, want)
	}
}

// withSyntheticRatchets temporarily replaces the production Ratchets
// slice for the duration of the test. Tests of Check/Threshold/
// FormatViolations need a non-empty ratchet entry to exercise their
// branches; using a synthetic one keeps them independent of which
// package the production allowlist currently covers (or whether it is
// empty at all).
func withSyntheticRatchets(t *testing.T, rs []Ratchet) {
	t.Helper()
	prev := Ratchets
	Ratchets = rs
	t.Cleanup(func() { Ratchets = prev })
}

// TestCheckFlagsBelowFloor verifies a reading at 80% in an
// un-ratcheted package counts as a violation while a reading at 90%
// passes. Ratchets are cleared for the test so the packages fall back to
// the pure FloorPercent path (both carry retention baselines in
// production).
func TestCheckFlagsBelowFloor(t *testing.T) {
	withSyntheticRatchets(t, nil)
	readings := []Reading{
		{Package: "github.com/ehmo/gum/internal/dispatch", Percent: 80.0, HasTests: true},
		{Package: "github.com/ehmo/gum/internal/output/fieldmask", Percent: 90.0, HasTests: true},
	}
	violations := Check(readings)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d: %+v", len(violations), violations)
	}
	if violations[0].Reading.Package != "github.com/ehmo/gum/internal/dispatch" {
		t.Errorf("violation package: got %s", violations[0].Reading.Package)
	}
}

// TestCheckRespectsRatchet verifies a ratchet'd package whose reading
// is at or above its Min passes, while one strictly below fails. Uses
// a synthetic ratchet so the test stays independent of the production
// allowlist.
func TestCheckRespectsRatchet(t *testing.T) {
	withSyntheticRatchets(t, []Ratchet{
		{Package: "synthetic/pkg", Min: 83.0, Bead: "test-only"},
	})
	readings := []Reading{
		{Package: "github.com/ehmo/gum/internal/dispatch", Percent: 86.0, HasTests: true}, // above floor (85.0)
		{Package: "synthetic/pkg", Percent: 82.0, HasTests: true},                          // below ratchet (83.0)
	}
	violations := Check(readings)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if got, want := violations[0].Reading.Package, "synthetic/pkg"; got != want {
		t.Errorf("violation package: got %s want %s", got, want)
	}
}

// TestCheckSkipsEmptyTestPackages verifies packages with no test files
// are not flagged (a different gate class).
func TestCheckSkipsEmptyTestPackages(t *testing.T) {
	readings := []Reading{
		{Package: "github.com/ehmo/gum/internal/output", Percent: 0, HasTests: false},
	}
	if got := Check(readings); len(got) != 0 {
		t.Errorf("empty-test package should not be flagged, got %+v", got)
	}
}

// TestFormatViolationsIncludesBead verifies the human-readable error
// surface shows the bead reference for ratcheted regressions so the
// operator immediately knows which follow-up bead owns the lift. Uses
// a synthetic ratchet so the test does not depend on the production
// allowlist's contents (which may be empty).
func TestFormatViolationsIncludesBead(t *testing.T) {
	withSyntheticRatchets(t, []Ratchet{
		{Package: "synthetic/pkg", Min: 83.0, Bead: "test-only-bead"},
	})
	violations := []Violation{{
		Reading:   Reading{Package: "synthetic/pkg", Percent: 80.0, HasTests: true},
		Threshold: 83.0,
	}}
	out := FormatViolations(violations)
	for _, want := range []string{"synthetic/pkg", "80.0%", "83.0%", "test-only-bead"} {
		if !strings.Contains(out, want) {
			t.Errorf("FormatViolations missing %q in:\n%s", want, out)
		}
	}
}

// TestFormatViolationsEmptySliceReturnsEmpty pins the documented
// short-circuit (FormatViolations returns "" for an empty slice) so a
// later refactor cannot accidentally produce a trailing newline that
// CI mistakes for a violation.
func TestFormatViolationsEmptySliceReturnsEmpty(t *testing.T) {
	if got := FormatViolations(nil); got != "" {
		t.Errorf("FormatViolations(nil) = %q, want empty", got)
	}
	if got := FormatViolations([]Violation{}); got != "" {
		t.Errorf("FormatViolations(empty) = %q, want empty", got)
	}
}

// TestFormatViolationsOmitsBeadForNonRatchetedPackage covers the
// branch where ratchetFor returns false: a violation against a
// package NOT in the allowlist is still formatted, but without the
// "(ratchet bead: …)" suffix.
func TestFormatViolationsOmitsBeadForNonRatchetedPackage(t *testing.T) {
	withSyntheticRatchets(t, nil)
	out := FormatViolations([]Violation{{
		Reading:   Reading{Package: "some/pkg", Percent: 70.0, HasTests: true},
		Threshold: FloorPercent,
	}})
	if !strings.Contains(out, "some/pkg") {
		t.Errorf("formatted output missing package name: %q", out)
	}
	if strings.Contains(out, "ratchet bead") {
		t.Errorf("formatted output should not mention bead for non-ratcheted package: %q", out)
	}
}

// TestGatedPackages locks the canonical gated set: the full tracked source
// surface (cmd/gum + all internal packages). Reorderings or removals here
// are coverage-policy changes that need a docs/test-matrix.md update.
func TestGatedPackages(t *testing.T) {
	got := GatedPackages()
	want := []string{
		"./cmd/gum/...",
		"./internal/...",
	}
	if len(got) != len(want) {
		t.Fatalf("GatedPackages returned %d entries, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("GatedPackages[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestOpportunitiesFlagsImprovedPackages verifies the warn-only ratchet
// hint: a ratcheted package whose measured coverage exceeds Min by at
// least RatchetOpportunityMargin is reported, one just under the margin is
// not, an un-ratcheted package is never reported, and empty-test packages
// are skipped. Uses synthetic ratchets so the assertion is independent of
// the production table.
func TestOpportunitiesFlagsImprovedPackages(t *testing.T) {
	withSyntheticRatchets(t, []Ratchet{
		{Package: "improved/pkg", Min: 90.0, Bead: "test-only"},
		{Package: "steady/pkg", Min: 90.0, Bead: "test-only"},
	})
	readings := []Reading{
		{Package: "improved/pkg", Percent: 90.0 + RatchetOpportunityMargin, HasTests: true}, // exactly at margin → flagged
		{Package: "steady/pkg", Percent: 91.0, HasTests: true},                              // +1.0 < margin → not flagged
		{Package: "unratcheted/pkg", Percent: 100.0, HasTests: true},                        // no baseline → never flagged
		{Package: "empty/pkg", Percent: 100.0, HasTests: false},                             // no tests → skipped
	}
	got := Opportunities(readings)
	if len(got) != 1 {
		t.Fatalf("expected 1 opportunity, got %d: %+v", len(got), got)
	}
	if got[0].Reading.Package != "improved/pkg" {
		t.Errorf("opportunity package = %q, want improved/pkg", got[0].Reading.Package)
	}
	if got[0].Min != 90.0 {
		t.Errorf("opportunity Min = %.1f, want 90.0", got[0].Min)
	}
}

// TestOpportunitiesEmptyWhenNoneImproved verifies the nil-return path when
// no package clears the margin, so cmd/coverage-floor prints no hint block.
func TestOpportunitiesEmptyWhenNoneImproved(t *testing.T) {
	withSyntheticRatchets(t, []Ratchet{{Package: "p", Min: 95.0, Bead: "test-only"}})
	if got := Opportunities([]Reading{{Package: "p", Percent: 95.5, HasTests: true}}); len(got) != 0 {
		t.Errorf("expected no opportunities, got %+v", got)
	}
}
