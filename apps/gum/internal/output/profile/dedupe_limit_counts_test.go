// Package profile_test — the row stages that removed rows silently.
//
// dedupe, sort_by and limit run through applyToRowArray, which recorded
// nothing. ShapingNotice returned "" while the rows were gone, and dedupe
// collapsed repeated rows without the occurrence counts spec §9.1 stage 7
// requires.
package profile_test

import (
	"encoding/json"
	"strings"
	"testing"

	profile "github.com/ehmo/gum/internal/output/profile"
)

// rowsOf decodes the shaped body and returns its "messages" record array.
func rowsOf(t *testing.T, body []byte) []map[string]any {
	t.Helper()

	var got struct {
		Messages []map[string]any `json:"messages"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal shaped body: %v; body=%s", err, body)
	}
	return got.Messages
}

func TestDedupeCountsOccurrences(t *testing.T) {
	const body = `{"messages":[{"id":"a","v":1},{"id":"a","v":2},{"id":"a","v":3},{"id":"b","v":4}]}`

	out, err := profile.Apply(
		&profile.Profile{DefaultFormat: "json", Dedupe: &profile.DedupeSpec{By: []string{"id"}}},
		profile.ApplyInput{Body: []byte(body)},
	)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	rows := rowsOf(t, out.Body)
	if len(rows) != 2 {
		t.Fatalf("shaped body has %d rows; want 2: %s", len(rows), out.Body)
	}

	count, ok := rows[0]["occurrence_count"]
	if !ok {
		t.Fatalf("row collapsing 3 duplicates carries no occurrence_count: %s", out.Body)
	}
	if n, _ := count.(float64); n != 3 {
		t.Errorf("occurrence_count = %v; want 3 (spec §9.1 stage 7)", count)
	}
}

func TestDedupeLeavesUniqueRowsUnmarked(t *testing.T) {
	const body = `{"messages":[{"id":"a"},{"id":"b"}]}`

	out, err := profile.Apply(
		&profile.Profile{DefaultFormat: "json", Dedupe: &profile.DedupeSpec{By: []string{"id"}}},
		profile.ApplyInput{Body: []byte(body)},
	)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	for i, row := range rowsOf(t, out.Body) {
		if _, ok := row["occurrence_count"]; ok {
			t.Errorf("row %d occurred once and still carries occurrence_count: %s", i, out.Body)
		}
	}
}

// TestDedupeKeepsAnUpstreamOccurrenceCount pins the no-overwrite rule: the
// stage annotates rows, and an annotation that replaces an upstream field is
// the data loss the stage exists to report.
func TestDedupeKeepsAnUpstreamOccurrenceCount(t *testing.T) {
	const body = `{"messages":[{"id":"a","occurrence_count":41},{"id":"a","occurrence_count":9}]}`

	out, err := profile.Apply(
		&profile.Profile{DefaultFormat: "json", Dedupe: &profile.DedupeSpec{By: []string{"id"}}},
		profile.ApplyInput{Body: []byte(body)},
	)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	rows := rowsOf(t, out.Body)
	if len(rows) != 1 {
		t.Fatalf("shaped body has %d rows; want 1: %s", len(rows), out.Body)
	}
	if n, _ := rows[0]["occurrence_count"].(float64); n != 41 {
		t.Errorf("occurrence_count = %v; want the upstream 41", rows[0]["occurrence_count"])
	}
}

func TestApplyReportsRowsDedupeRemoved(t *testing.T) {
	const body = `{"messages":[{"id":"a"},{"id":"a"},{"id":"a"},{"id":"b"}]}`

	out, err := profile.Apply(
		&profile.Profile{DefaultFormat: "json", Dedupe: &profile.DedupeSpec{By: []string{"id"}}},
		profile.ApplyInput{Body: []byte(body)},
	)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if out.DedupedRows != 2 {
		t.Errorf("DedupedRows = %d; want 2", out.DedupedRows)
	}
	if out.LimitedRows != 0 {
		t.Errorf("LimitedRows = %d; want 0 (no limit set)", out.LimitedRows)
	}
}

func TestApplyReportsRowsLimitRemoved(t *testing.T) {
	const body = `{"messages":[{"id":"a"},{"id":"b"},{"id":"c"},{"id":"d"},{"id":"e"}]}`

	out, err := profile.Apply(
		&profile.Profile{DefaultFormat: "json", Limit: 2},
		profile.ApplyInput{Body: []byte(body)},
	)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if out.LimitedRows != 3 {
		t.Errorf("LimitedRows = %d; want 3", out.LimitedRows)
	}
}

func TestNoticeNamesRowsDedupeAndLimitRemoved(t *testing.T) {
	notice := profile.ShapingNotice(profile.NoticeInput{DedupedRows: 2, LimitedRows: 7})
	if notice == "" {
		t.Fatal("shaping notice is empty after dedupe and limit removed rows (spec §9.1 shaping notice)")
	}
	if !strings.Contains(notice, "2") || !strings.Contains(notice, "7") {
		t.Errorf("notice %q names neither removed-row count", notice)
	}
}

func TestNoticeStaysEmptyWhenNoRowsRemoved(t *testing.T) {
	if notice := profile.ShapingNotice(profile.NoticeInput{}); notice != "" {
		t.Errorf("notice = %q; want \"\" when shaping removed nothing", notice)
	}
}
