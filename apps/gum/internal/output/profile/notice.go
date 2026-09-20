package profile

import (
	"fmt"
	"strings"
)

// noticeMaxPaths caps how many dot-paths a notice names. A profile that keeps
// six fields out of a hundred would otherwise print a wall of text longer than
// the response it annotates.
const noticeMaxPaths = 8

// NoticeInput is everything a presentation layer needs to tell the caller what
// the expression profile took out of one response.
type NoticeInput struct {
	// DroppedPaths are the response fields a whitelist or drop rule removed.
	DroppedPaths []string

	// CollapsedArrays are the arrays collapse_arrays truncated.
	CollapsedArrays []CollapsedArray

	// RawHint is the surface-specific way to ask for the unshaped body, such
	// as "--format raw" or `format: "raw"`. Omitted when empty.
	RawHint string

	// MaxItemsHint is the surface-specific way to lift the result cap, such as
	// "--max-items all" or `max_items: "all"`. Omitted when empty.
	MaxItemsHint string

	// FullResultPath is the tee artifact holding the complete pre-shaping
	// payload. Omitted when empty.
	FullResultPath string

	// OnEmptyMessage is the profile's on_empty string when shaping left an
	// empty result set (spec §9.1 rule 2). It leads the notice, because a
	// reader who cannot tell an empty API response from an emptied one has no
	// use for the rest of it. Omitted when empty.
	OnEmptyMessage string

	// DedupedRows and LimitedRows are the row counts stage 7's dedupe and the
	// profile's limit removed. Both stages remove whole records and write no
	// count into the body, so a notice that omits them reports a complete
	// result the caller no longer has.
	DedupedRows int
	LimitedRows int
}

// ShapingNotice returns a one-line notice naming what the expression profile
// took out of a response, or "" when it took nothing out.
//
// The on_empty message leads when shaping emptied the result set, then the
// truncation counts, then the removed fields.
//
// The notice exists because shaping is otherwise invisible: the caller receives
// valid JSON with no marker and no way to tell that the operation's headline
// field is missing (gum-bpx0). Truncated results lead, because dropping 143 of
// 243 rows matters more than dropping one field, and a notice that names only
// the field points the reader at the smaller problem (gum-pmbp).
func ShapingNotice(in NoticeInput) string {
	var sb strings.Builder

	if in.OnEmptyMessage != "" {
		sb.WriteString("note: " + in.OnEmptyMessage)
	}

	if clause := collapsedClause(in.CollapsedArrays); clause != "" {
		lead := "note: the output profile "
		if sb.Len() > 0 {
			lead = " The output profile "
		}
		sb.WriteString(lead + clause + ".")
		if in.MaxItemsHint != "" {
			fmt.Fprintf(&sb, " Use %s for every result.", in.MaxItemsHint)
		}
	}

	if clause := rowStageClause(in.DedupedRows, in.LimitedRows); clause != "" {
		lead := "note: the output profile "
		if sb.Len() > 0 {
			lead = " It "
		}
		sb.WriteString(lead + clause + ".")
	}

	if len(in.DroppedPaths) > 0 {
		lead := "note: the output profile removed"
		if sb.Len() > 0 {
			lead = " It removed"
		}
		sb.WriteString(lead + " " + droppedClause(in.DroppedPaths) + ".")
		if in.RawHint != "" {
			fmt.Fprintf(&sb, " Use %s for the complete body.", in.RawHint)
		}
	}

	if sb.Len() == 0 {
		return ""
	}

	if in.FullResultPath != "" {
		fmt.Fprintf(&sb, " Full result: %s", in.FullResultPath)
	}

	return sb.String()
}

// collapsedClause renders the truncated arrays as "omitted 143 of 243 results
// (results_omitted_count=143)", one clause per array, joined by "; ".
func collapsedClause(arrays []CollapsedArray) string {
	if len(arrays) == 0 {
		return ""
	}

	clauses := make([]string, 0, len(arrays))
	for _, c := range arrays {
		noun := c.Field
		if noun == "" {
			noun = "items"
		}
		clauses = append(clauses, fmt.Sprintf("omitted %d of %d %s (%s=%d)",
			c.Omitted, c.Kept+c.Omitted, noun, c.CountKey, c.Omitted))
	}

	return strings.Join(clauses, "; ")
}

// droppedClause renders the removed field paths as "1 field from this response:
// results.closeVariants", capping the list at noticeMaxPaths.
func droppedClause(paths []string) string {
	noun := "fields"
	if len(paths) == 1 {
		noun = "field"
	}

	shown := paths
	suffix := ""
	if len(paths) > noticeMaxPaths {
		shown = paths[:noticeMaxPaths]
		suffix = fmt.Sprintf(", and %d more", len(paths)-noticeMaxPaths)
	}

	return fmt.Sprintf("%d %s from this response: %s%s", len(paths), noun, strings.Join(shown, ", "), suffix)
}

// rowStageClause renders the rows stage 7 and the profile limit removed as
// "collapsed 2 duplicate rows" / "dropped 7 rows past the profile limit". Both
// counts are absent from the body, so the notice is where the caller meets
// them.
func rowStageClause(deduped, limited int) string {
	clauses := make([]string, 0, 2)
	if deduped > 0 {
		clauses = append(clauses, fmt.Sprintf("collapsed %d duplicate %s (%s on the row that survived)",
			deduped, rowNoun(deduped), occurrenceCountKey))
	}
	if limited > 0 {
		clauses = append(clauses, fmt.Sprintf("dropped %d %s past the profile limit", limited, rowNoun(limited)))
	}

	return strings.Join(clauses, " and ")
}

// rowNoun agrees the row count with its noun.
func rowNoun(n int) string {
	if n == 1 {
		return "row"
	}
	return "rows"
}
