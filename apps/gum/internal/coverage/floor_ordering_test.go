package coverage

import (
	"runtime"
	"testing"
)

// TestCheckSortsViolationsByPackage pins the ordering of the failure report.
// CI prints violations verbatim, so an unstable order turns a one-package
// regression into a diff that looks like several.
func TestCheckSortsViolationsByPackage(t *testing.T) {
	readings := []Reading{
		{Package: "z/pkg", Percent: 1, HasTests: true},
		{Package: "a/pkg", Percent: 1, HasTests: true},
		{Package: "m/pkg", Percent: 1, HasTests: true},
	}
	got := Check(readings)
	if len(got) != 3 {
		t.Fatalf("Check returned %d violations; want 3", len(got))
	}
	want := []string{"a/pkg", "m/pkg", "z/pkg"}
	for i, w := range want {
		if got[i].Reading.Package != w {
			t.Errorf("violation %d = %q; want %q", i, got[i].Reading.Package, w)
		}
	}
}

// TestOpportunitiesUsesTheRuntimePlatform pins the exported wrapper to the
// host GOOS. The platform argument is what suppresses a hint measured off the
// baseline, so the wrapper must not be able to disagree with OpportunitiesOn.
func TestOpportunitiesUsesTheRuntimePlatform(t *testing.T) {
	// A ratchet already within the margin of 100% cannot qualify, so the
	// reading has to name one that leaves room. The table's first entry does
	// not always.
	pkg := ""
	for _, r := range Ratchets {
		if 100.0 >= r.Min+RatchetOpportunityMargin {
			pkg = r.Package
			break
		}
	}
	if pkg == "" {
		t.Skip("every ratchet sits within the margin of 100%")
	}
	readings := []Reading{{Package: pkg, Percent: 100, HasTests: true}}

	got := Opportunities(readings)
	want := OpportunitiesOn(readings, runtime.GOOS)
	if len(got) != len(want) {
		t.Fatalf("Opportunities returned %d; OpportunitiesOn(runtime.GOOS) returned %d", len(got), len(want))
	}
	if runtime.GOOS == BaselineGOOS && len(got) != 1 {
		t.Errorf("on the baseline platform Opportunities returned %d; want 1", len(got))
	}
	if runtime.GOOS != BaselineGOOS && len(got) != 0 {
		t.Errorf("off the baseline platform Opportunities returned %d; want 0", len(got))
	}
}

// TestOpportunitiesAreSortedByPackage pins the ordering of the ratchet hints.
// cmd/coverage-floor prints them as a to-do list, and an unordered list
// re-orders itself between runs for no reason a reader can act on.
func TestOpportunitiesAreSortedByPackage(t *testing.T) {
	if len(Ratchets) < 3 {
		t.Skipf("need at least 3 ratchets to prove ordering; have %d", len(Ratchets))
	}
	// Feed the ratcheted packages back in reverse table order, all well above
	// their baselines so every one qualifies.
	var readings []Reading
	for i := len(Ratchets) - 1; i >= 0; i-- {
		readings = append(readings, Reading{Package: Ratchets[i].Package, Percent: 100, HasTests: true})
	}

	// A ratchet already within the margin of 100% cannot qualify.
	eligible := 0
	for _, r := range Ratchets {
		if 100.0 >= r.Min+RatchetOpportunityMargin {
			eligible++
		}
	}

	got := OpportunitiesOn(readings, BaselineGOOS)
	if len(got) != eligible {
		t.Fatalf("got %d opportunities; want the %d ratchets that are more than the margin below 100%%", len(got), eligible)
	}
	if len(got) < 3 {
		t.Fatalf("only %d opportunities qualify; ordering is not provable", len(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].Reading.Package >= got[i].Reading.Package {
			t.Fatalf("opportunities are not sorted: %q came before %q",
				got[i-1].Reading.Package, got[i].Reading.Package)
		}
	}
}
