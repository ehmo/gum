// Package profile — regression test for `gum profile test` result counts.
//
// Defect: runOneFixture called inspectShape with the unshaped fixture body, so
// ActualResultCount reported the upstream row count and ActualOmittedCount was
// always 0. The documented contract (docs/expression-profile-dsl.md Test
// Format) pairs expect_result_count = 20 with expect_omitted_count = 80 for a
// 100-row fixture capped at 20, which the old code could never produce.
package profile

import (
	"os"
	"path/filepath"
	"testing"
)

// TestFixtureCountsMeasureTheShapedBody caps a 5-row fixture at 2 and asserts
// the harness reports 2 kept and 3 omitted.
func TestFixtureCountsMeasureTheShapedBody(t *testing.T) {
	dir := t.TempDir()
	body := `{"messages":[{"id":"a"},{"id":"b"},{"id":"c"},{"id":"d"},{"id":"e"}]}`
	if err := os.WriteFile(filepath.Join(dir, "rows.json"), []byte(body), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	p := &Profile{
		CollapseArrays: &CollapseArraysSpec{MaxItems: 2},
		Tests: []TestFixture{{
			Name:                  "capped",
			Fixture:               "rows.json",
			ExpectResultCountSet:  true,
			ExpectResultCount:     2,
			ExpectOmittedCountSet: true,
			ExpectOmittedCount:    3,
		}},
	}

	res, err := RunFixtures(p, dir)
	if err != nil {
		t.Fatalf("RunFixtures: %v", err)
	}
	if len(res.Fixtures) != 1 {
		t.Fatalf("got %d fixture results; want 1", len(res.Fixtures))
	}
	got := res.Fixtures[0]
	if got.ActualResultCount != 2 {
		t.Errorf("ActualResultCount = %d; want 2 (the shaped row count, not the fixture's 5)",
			got.ActualResultCount)
	}
	if got.ActualOmittedCount != 3 {
		t.Errorf("ActualOmittedCount = %d; want 3 (stage 5 dropped 3 rows)", got.ActualOmittedCount)
	}
	if !got.Passed {
		t.Errorf("fixture failed: %v", got.Failures)
	}
}

// TestFixtureCountsSurviveToonOutput pins the reason the old code read the
// source body: a TOON-encoded output is opaque to a JSON decode. The counts must
// still be right.
func TestFixtureCountsSurviveToonOutput(t *testing.T) {
	dir := t.TempDir()
	body := `{"messages":[{"id":"a"},{"id":"b"},{"id":"c"}]}`
	if err := os.WriteFile(filepath.Join(dir, "rows.json"), []byte(body), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	p := &Profile{
		DefaultFormat:  "toon",
		CollapseArrays: &CollapseArraysSpec{MaxItems: 1},
		Tests:          []TestFixture{{Name: "toon", Fixture: "rows.json"}},
	}

	res, err := RunFixtures(p, dir)
	if err != nil {
		t.Fatalf("RunFixtures: %v", err)
	}
	got := res.Fixtures[0]
	if got.ActualFormat != "toon" {
		t.Fatalf("ActualFormat = %q; want toon", got.ActualFormat)
	}
	if got.ActualResultCount != 1 {
		t.Errorf("ActualResultCount = %d; want 1", got.ActualResultCount)
	}
	if got.ActualOmittedCount != 2 {
		t.Errorf("ActualOmittedCount = %d; want 2", got.ActualOmittedCount)
	}
}
