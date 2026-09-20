package profile_test

import (
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/output/profile"
)

// The notice builder picks a different lead for every clause depending on what
// already sits in the buffer: "note: the output profile ..." when the clause
// leads, " The output profile ..." / " It ..." when it follows. A caller that
// hits several stages at once gets one sentence, not three fragments, and the
// on_empty message always leads (spec §9.1 rule 2).
func TestShapingNoticeOnEmptyMessageLeads(t *testing.T) {
	got := profile.ShapingNotice(profile.NoticeInput{
		OnEmptyMessage: "No results matched.",
	})

	want := "note: No results matched."
	if got != want {
		t.Fatalf("ShapingNotice = %q, want %q", got, want)
	}
}

func TestShapingNoticeChainsEveryClauseAfterOnEmpty(t *testing.T) {
	got := profile.ShapingNotice(profile.NoticeInput{
		OnEmptyMessage: "Nothing left.",
		CollapsedArrays: []profile.CollapsedArray{
			{Field: "results", Kept: 20, Omitted: 80, CountKey: "results_omitted_count"},
		},
		MaxItemsHint: "--max-items all",
		DedupedRows:  1,
		LimitedRows:  7,
		DroppedPaths: []string{"results.closeVariants"},
		RawHint:      "--format raw",
	})

	for _, want := range []string{
		"note: Nothing left.",
		" The output profile omitted 80 of 100 results (results_omitted_count=80).",
		" Use --max-items all for every result.",
		" It collapsed 1 duplicate row ",
		" and dropped 7 rows past the profile limit.",
		" It removed 1 field from this response: results.closeVariants.",
		" Use --format raw for the complete body.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("ShapingNotice = %q\nmissing %q", got, want)
		}
	}

	// Only the first clause carries the "note: " prefix.
	if n := strings.Count(got, "note: "); n != 1 {
		t.Errorf("ShapingNotice = %q; want exactly one \"note: \" prefix, got %d", got, n)
	}
}

// rowNoun agrees the count with its noun. One deduped row is a "row"; more than
// one is "rows". The singular arm is otherwise unreachable from the applier,
// which only reports a dedupe when it collapsed at least one pair.
func TestShapingNoticeRowNounAgrees(t *testing.T) {
	one := profile.ShapingNotice(profile.NoticeInput{DedupedRows: 1, LimitedRows: 1})
	if !strings.Contains(one, "collapsed 1 duplicate row ") {
		t.Errorf("one deduped row: ShapingNotice = %q; want the singular noun", one)
	}
	if !strings.Contains(one, "dropped 1 row past the profile limit") {
		t.Errorf("one limited row: ShapingNotice = %q; want the singular noun", one)
	}

	many := profile.ShapingNotice(profile.NoticeInput{DedupedRows: 2, LimitedRows: 3})
	if !strings.Contains(many, "collapsed 2 duplicate rows ") {
		t.Errorf("two deduped rows: ShapingNotice = %q; want the plural noun", many)
	}
	if !strings.Contains(many, "dropped 3 rows past the profile limit") {
		t.Errorf("three limited rows: ShapingNotice = %q; want the plural noun", many)
	}
}
