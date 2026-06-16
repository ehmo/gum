package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/ehmo/gum/internal/catalog"
)

// TestPrimaryArrayKeyPrefersWellKnown pins finding #27: a response with several
// arrays tables the conventional data key (rows/items/...), not the
// alphabetically-first one.
func TestPrimaryArrayKeyPrefersWellKnown(t *testing.T) {
	if got := primaryArrayKey([]string{"columnHeaders", "rows"}); got != "rows" {
		t.Errorf("primaryArrayKey([columnHeaders rows]) = %q, want rows", got)
	}
	if got := primaryArrayKey([]string{"alpha", "beta"}); got != "alpha" {
		t.Errorf("primaryArrayKey (no well-known) = %q, want alpha (first)", got)
	}
	if got := primaryArrayKey([]string{"data", "items"}); got != "items" {
		t.Errorf("primaryArrayKey = %q, want items (items ranks above data)", got)
	}
}

// TestObjectViewPrefersRowsArray pins #27 through objectView itself: the rows
// array becomes the primary table even though columnHeaders sorts first.
func TestObjectViewPrefersRowsArray(t *testing.T) {
	obj := map[string]any{
		"columnHeaders": []any{map[string]any{"name": "query"}},
		"rows":          []any{map[string]any{"clicks": float64(5)}},
	}
	v := objectView(obj)
	found := false
	for _, c := range v.cols {
		if c == "clicks" {
			found = true
		}
	}
	if !found {
		t.Errorf("objectView cols = %v, want the rows table (clicks column)", v.cols)
	}
}

// TestNeutralizeCSVCellLeadingWhitespace pins finding #15: a formula trigger
// hidden behind leading whitespace is still defused (LibreOffice trims first).
func TestNeutralizeCSVCellLeadingWhitespace(t *testing.T) {
	cases := map[string]string{
		" =SUM(A1:A2)": "' =SUM(A1:A2)",
		"\t=cmd":       "'\t=cmd",
		"\n=cmd":       "'\n=cmd",
		"\r=cmd":       "'\r=cmd",
		"=evil":        "'=evil",
		"safe":         "safe",
		"  ":           "  ",
		"":             "",
	}
	for in, want := range cases {
		if got := neutralizeCSVCell(in); got != want {
			t.Errorf("neutralizeCSVCell(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestWriteMarkdownTableEscapesCR pins finding #11: a carriage return in a cell
// is replaced rather than emitted verbatim (it corrupts terminal output).
func TestWriteMarkdownTableEscapesCR(t *testing.T) {
	var b strings.Builder
	if err := writeMarkdownTable(&b, []string{"col"}, [][]string{{"a\rb"}}); err != nil {
		t.Fatalf("writeMarkdownTable: %v", err)
	}
	if strings.Contains(b.String(), "\r") {
		t.Errorf("markdown output contains a raw CR: %q", b.String())
	}
}

// TestPageSizeParam pins finding #2's page-size mapping: --page-size targets the
// query parameter the op actually declares (pageSize vs maxResults).
func TestPageSizeParam(t *testing.T) {
	drive := []catalog.RequestField{{Name: "pageSize"}, {Name: "q"}}
	gmail := []catalog.RequestField{{Name: "maxResults"}, {Name: "q"}}
	none := []catalog.RequestField{{Name: "q"}}
	if got := pageSizeParam(drive); got != "pageSize" {
		t.Errorf("drive -> %q, want pageSize", got)
	}
	if got := pageSizeParam(gmail); got != "maxResults" {
		t.Errorf("gmail -> %q, want maxResults", got)
	}
	if got := pageSizeParam(none); got != "pageSize" {
		t.Errorf("none -> %q, want pageSize (default)", got)
	}
}

// TestPromptMissingFieldsRejectsEmptyArrayList pins finding #19: a separator-only
// answer (",") to a required array field is rejected, not stored as an empty list
// that bypasses the required-field guard.
func TestPromptMissingFieldsRejectsEmptyArrayList(t *testing.T) {
	args := map[string]any{}
	fields := []catalog.RequestField{{Name: "dims", Type: "array", Required: true}}
	err := promptMissingFields(strings.NewReader(",\n"), &strings.Builder{}, args, fields)
	if err == nil {
		t.Fatal("expected error for separator-only array input, got nil")
	}
	if _, present := args["dims"]; present {
		t.Errorf("empty array stored despite rejection: %#v", args["dims"])
	}
}

// TestApplyKebabFlagsMergesArrayWithPositional pins finding #25: an array kebab
// flag is merged with positional values for the same field, not silently
// clobbering them.
func TestApplyKebabFlagsMergesArrayWithPositional(t *testing.T) {
	fields := []catalog.RequestField{{Name: "dimensions", Type: "array"}}
	cmd := &cobra.Command{}
	cmd.Flags().StringArray("dimensions", nil, "")
	if err := cmd.Flags().Set("dimensions", "page"); err != nil {
		t.Fatalf("set flag: %v", err)
	}
	args := map[string]any{"dimensions": []any{"query"}} // positional already accumulated
	applyKebabFlags(cmd, args, fields)
	got, _ := args["dimensions"].([]any)
	if len(got) != 2 || got[0] != "query" || got[1] != "page" {
		t.Errorf("merged dimensions = %#v, want [query page]", args["dimensions"])
	}
}
