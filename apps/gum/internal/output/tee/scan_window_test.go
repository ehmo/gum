package tee_test

import (
	"math"
	"testing"

	"github.com/ehmo/gum/internal/output/tee"
)

// ScanWindowDays maps a retention window in hours onto the number of UTC-day
// directories FindArtifact walks. Scanning too few days reports
// RESULT_ARTIFACT_EXPIRED for a file still on disk and still inside the expiry
// gum advertised, so the mapping must always round up and then add the day
// boundary (bead gum-sd58).
func TestScanWindowDays(t *testing.T) {
	cases := []struct {
		name  string
		hours int
		want  int
	}{
		// 0 and below mean "unset or unusable"; both take the spec default of
		// 24 hours, which must keep yielding the 2 days the handler scanned
		// before the value was configurable at all.
		{name: "unset takes the default", hours: 0, want: 2},
		{name: "negative takes the default", hours: -5, want: 2},
		{name: "spec default", hours: 24, want: 2},
		{name: "under one day still spans a boundary", hours: 1, want: 2},
		{name: "part of a second day rounds up", hours: 25, want: 3},
		{name: "two days", hours: 48, want: 3},
		{name: "one week", hours: 168, want: 8},
		{name: "at the cap", hours: tee.MaxRetentionHours, want: 3653},
		{name: "past the cap", hours: tee.MaxRetentionHours * 4, want: 3653},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tee.ScanWindowDays(tc.hours); got != tc.want {
				t.Errorf("ScanWindowDays(%d) = %d; want %d", tc.hours, got, tc.want)
			}
		})
	}
}

// The window must never be narrower than the retention it is derived from, at
// any hour count, or gum can promise an expiry it cannot serve.
func TestScanWindowNeverNarrowerThanRetention(t *testing.T) {
	for hours := 1; hours <= 24*30; hours++ {
		days := tee.ScanWindowDays(hours)
		if days*24 < hours {
			t.Fatalf("ScanWindowDays(%d) = %d days = %dh; the scan window must cover the full retention", hours, days, days*24)
		}
	}
}

// A retention near math.MaxInt reaches here whenever someone fat-fingers
// `gum config set output.tee_retention_hours=...`: strconv.Atoi accepts it and
// the value is positive, so nothing upstream rejects it. Unclamped the +23
// wraps negative, FindArtifact rejects any maxDays below 1, and every
// gum://results/{hash} read answers RESULT_ARTIFACT_EXPIRED -- the exact
// failure this whole change removes.
func TestScanWindowDaysSurvivesAnAbsurdRetention(t *testing.T) {
	for _, hours := range []int{math.MaxInt, math.MaxInt - 1, math.MaxInt32, tee.MaxRetentionHours + 1} {
		days := tee.ScanWindowDays(hours)
		if days < 1 {
			t.Errorf("ScanWindowDays(%d) = %d; a window below 1 day makes FindArtifact reject every hash", hours, days)
		}
		if days > 3653 {
			t.Errorf("ScanWindowDays(%d) = %d; want the ten-year cap of 3653 days", hours, days)
		}
	}
}

func TestDefaultRetentionHoursIsTheSpecWindow(t *testing.T) {
	if tee.DefaultRetentionHours != 24 {
		t.Errorf("DefaultRetentionHours = %d; spec §9.0 sets the default artifact retention to 24 hours", tee.DefaultRetentionHours)
	}
}
