package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/ehmo/gum/internal/catalog"
)

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
