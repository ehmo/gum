package profile_test

import (
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/output/profile"
)

// A profile whitelist removes fields with no marker in the output: the body is
// valid JSON, just smaller. gum-bpx0 was reported after a research corpus
// recorded a 12-month average as a monthly figure because the per-month array
// never appeared and nothing said it had been removed. These tests hold the
// applier to reporting every path its field filters drop.

// applyJSON runs p over body and fails the test on error.
func applyJSON(t *testing.T, p *profile.Profile, body string) profile.ApplyOutput {
	t.Helper()
	out, err := profile.Apply(p, profile.ApplyInput{Body: []byte(body), UserFormat: "json"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	return out
}

func TestKeepFieldsReportsDroppedPaths(t *testing.T) {
	p := &profile.Profile{KeepFields: []string{"results.text", "results.metrics.avg"}}
	out := applyJSON(t, p, `{
		"results": [
			{"text": "a", "metrics": {"avg": 1, "monthly": [{"m": "JULY"}]}, "annotations": {"x": 1}},
			{"text": "b", "metrics": {"avg": 2, "monthly": [{"m": "JULY"}]}}
		],
		"totalSize": 2
	}`)

	want := []string{"results.annotations", "results.metrics.monthly", "totalSize"}
	assertPaths(t, out.DroppedPaths, want)
}

// TestDroppedPathsCarryNoArrayIndices pins the dedupe rule: a field missing from
// every element of a 500-row array is one finding, not 500 lines of notice.
func TestDroppedPathsCarryNoArrayIndices(t *testing.T) {
	var rows []string
	for i := 0; i < 500; i++ {
		rows = append(rows, `{"keep": 1, "drop": 2}`)
	}
	body := `{"rows": [` + strings.Join(rows, ",") + `]}`

	out := applyJSON(t, &profile.Profile{KeepFields: []string{"rows.keep"}}, body)
	assertPaths(t, out.DroppedPaths, []string{"rows.drop"})
}

func TestProjectionReportsDroppedPaths(t *testing.T) {
	out := applyJSON(t, &profile.Profile{Projection: []string{"id"}},
		`{"id": "1", "subject": "hi", "body": "long"}`)
	assertPaths(t, out.DroppedPaths, []string{"body", "subject"})
}

// TestProjectionOverArrayReportsDroppedPaths covers the []any arm: projection
// over a top-level array projects each element map.
func TestProjectionOverArrayReportsDroppedPaths(t *testing.T) {
	out := applyJSON(t, &profile.Profile{Projection: []string{"id"}},
		`[{"id": "1", "subject": "hi"}, {"id": "2", "snippet": "x"}]`)
	assertPaths(t, out.DroppedPaths, []string{"snippet", "subject"})
}

func TestDropFieldsReportsDroppedPaths(t *testing.T) {
	out := applyJSON(t, &profile.Profile{DropFields: []string{"payload.raw", "etag"}},
		`{"etag": "abc", "payload": {"raw": "...", "parsed": {"ok": true}}}`)
	assertPaths(t, out.DroppedPaths, []string{"etag", "payload.raw"})
}

// TestDropFieldsReportsOnlyPathsThatExisted keeps the notice truthful: a
// denylist entry that matched nothing removed nothing.
func TestDropFieldsReportsOnlyPathsThatExisted(t *testing.T) {
	out := applyJSON(t, &profile.Profile{DropFields: []string{"absent", "etag"}},
		`{"etag": "abc", "id": "1"}`)
	assertPaths(t, out.DroppedPaths, []string{"etag"})
}

// TestNoFieldFilterDropsNothing: the lossy-but-marked stages are not reported.
// collapse_arrays writes its own omitted_count, truncate_strings shortens values
// it keeps, and strip_nulls removes only null-like values — none of them makes a
// declared field vanish without a trace.
func TestNoFieldFilterDropsNothing(t *testing.T) {
	p := &profile.Profile{
		StripNulls:      true,
		CollapseArrays:  &profile.CollapseArraysSpec{MaxItems: 1},
		TruncateStrings: &profile.TruncateStringsSpec{DefaultChars: 3},
	}
	out := applyJSON(t, p, `{"note": "abcdefgh", "empty": null, "rows": [1, 2, 3]}`)
	if len(out.DroppedPaths) != 0 {
		t.Errorf("DroppedPaths = %v; want none (no field filter ran)", out.DroppedPaths)
	}
}

// TestRawBypassDropsNothing: --format raw returns the executor body verbatim, so
// there is never anything to report.
func TestRawBypassDropsNothing(t *testing.T) {
	out, err := profile.Apply(
		&profile.Profile{KeepFields: []string{"a"}},
		profile.ApplyInput{Body: []byte(`{"a": 1, "b": 2}`), UserFormat: "raw"},
	)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(out.DroppedPaths) != 0 {
		t.Errorf("DroppedPaths = %v; want none on the raw bypass", out.DroppedPaths)
	}
}

// TestPreservedDefsAreNotReportedAsDropped: keep_fields drops $defs on the way
// through, but the restore puts it back, so reporting it would send the caller
// looking for data that is in front of them.
func TestPreservedDefsAreNotReportedAsDropped(t *testing.T) {
	out := applyJSON(t, &profile.Profile{KeepFields: []string{"id"}},
		`{"id": "1", "gone": 2, "$defs": {"T": {"type": "string"}}}`)
	assertPaths(t, out.DroppedPaths, []string{"gone"})

	if !strings.Contains(string(out.Body), "$defs") {
		t.Fatalf("body lost $defs: %s", out.Body)
	}
}

func assertPaths(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("DroppedPaths = %v; want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("DroppedPaths = %v; want %v (sorted)", got, want)
		}
	}
}

func TestShapingNotice(t *testing.T) {
	cases := []struct {
		name string
		in   profile.NoticeInput
		want string
	}{
		{
			name: "nothing removed yields no notice",
		},
		{
			name: "one field is singular",
			in: profile.NoticeInput{
				DroppedPaths: []string{"results.keywordMetrics.monthlySearchVolumes"},
				RawHint:      "--format raw",
			},
			want: "note: the output profile removed 1 field from this response: " +
				"results.keywordMetrics.monthlySearchVolumes. Use --format raw for the complete body.",
		},
		{
			name: "several fields are plural",
			in:   profile.NoticeInput{DroppedPaths: []string{"a", "b"}},
			want: "note: the output profile removed 2 fields from this response: a, b.",
		},
		{
			name: "artifact path is named when tee fired",
			in: profile.NoticeInput{
				DroppedPaths:   []string{"a"},
				RawHint:        "--format raw",
				FullResultPath: "/tmp/gum/tee/2026-08-05/op/abc.json.gz",
			},
			want: "note: the output profile removed 1 field from this response: a. " +
				"Use --format raw for the complete body. Full result: /tmp/gum/tee/2026-08-05/op/abc.json.gz",
		},
		{
			name: "long lists are capped",
			in:   profile.NoticeInput{DroppedPaths: []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}},
			want: "note: the output profile removed 10 fields from this response: a, b, c, d, e, f, g, h, and 2 more.",
		},
		{
			name: "omitted results lead and name their count key",
			in: profile.NoticeInput{
				CollapsedArrays: []profile.CollapsedArray{
					{Field: "results", CountKey: "results_omitted_count", Kept: 100, Omitted: 143},
				},
				MaxItemsHint: "--max-items all",
			},
			want: "note: the output profile omitted 143 of 243 results (results_omitted_count=143). " +
				"Use --max-items all for every result.",
		},
		{
			name: "a bare array body reports items",
			in: profile.NoticeInput{
				CollapsedArrays: []profile.CollapsedArray{
					{CountKey: "omitted_count", Kept: 2, Omitted: 3},
				},
			},
			want: "note: the output profile omitted 3 of 5 items (omitted_count=3).",
		},
		{
			name: "several collapsed arrays are joined",
			in: profile.NoticeInput{
				CollapsedArrays: []profile.CollapsedArray{
					{Field: "a", CountKey: "a_omitted_count", Kept: 1, Omitted: 2},
					{Field: "b", CountKey: "b_omitted_count", Kept: 3, Omitted: 4},
				},
			},
			want: "note: the output profile omitted 2 of 3 a (a_omitted_count=2); omitted 4 of 7 b (b_omitted_count=4).",
		},
		{
			name: "omitted results outrank the removed field",
			in: profile.NoticeInput{
				DroppedPaths: []string{"results.closeVariants"},
				CollapsedArrays: []profile.CollapsedArray{
					{Field: "results", CountKey: "results_omitted_count", Kept: 100, Omitted: 143},
				},
				RawHint:        "--format raw",
				MaxItemsHint:   "--max-items all",
				FullResultPath: "/tmp/gum/tee/abc.json.gz",
			},
			want: "note: the output profile omitted 143 of 243 results (results_omitted_count=143). " +
				"Use --max-items all for every result. It removed 1 field from this response: " +
				"results.closeVariants. Use --format raw for the complete body. " +
				"Full result: /tmp/gum/tee/abc.json.gz",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := profile.ShapingNotice(tc.in); got != tc.want {
				t.Errorf("got  %q\nwant %q", got, tc.want)
			}
		})
	}
}
