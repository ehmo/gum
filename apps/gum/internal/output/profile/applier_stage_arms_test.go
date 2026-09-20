package profile_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/output/profile"
)

// A limit larger than the row count leaves the array alone. The stage still
// runs, so the pass-through arm has to return the original slice rather than a
// zero-length re-slice.
func TestApplyLimitLeavesAShorterArrayAlone(t *testing.T) {
	p := &profile.Profile{Limit: 10}
	body := []byte(`{"messages":[{"id":"a"},{"id":"b"}]}`)

	out, err := profile.Apply(p, profile.ApplyInput{Body: body, UserFormat: "json"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(out.Body, &got); err != nil {
		t.Fatalf("unmarshal shaped body: %v", err)
	}
	rows, ok := got["messages"].([]any)
	if !ok {
		t.Fatalf("messages = %T, want []any", got["messages"])
	}
	if len(rows) != 2 {
		t.Errorf("row count = %d, want 2; limit 10 must not touch a 2-row array", len(rows))
	}
}

// $defs is deep-copied before the mutating stages run, so a nested array inside
// a schema fragment has to survive strip_nulls verbatim. An array child is the
// only way into the slice arm of the copier.
func TestApplyPreservesArraysInsideDefs(t *testing.T) {
	p := &profile.Profile{StripNulls: true}
	body := []byte(`{"$defs":{"Status":{"enum":["ACTIVE",null,"PAUSED"],"type":"string"}},"results":[{"id":"1","note":null}]}`)

	out, err := profile.Apply(p, profile.ApplyInput{Body: body, UserFormat: "json"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(out.Body, &got); err != nil {
		t.Fatalf("unmarshal shaped body: %v", err)
	}
	defs, ok := got["$defs"].(map[string]any)
	if !ok {
		t.Fatalf("$defs = %T, want map[string]any", got["$defs"])
	}
	status, ok := defs["Status"].(map[string]any)
	if !ok {
		t.Fatalf("$defs.Status = %T, want map[string]any", defs["Status"])
	}
	enum, ok := status["enum"].([]any)
	if !ok {
		t.Fatalf("$defs.Status.enum = %T, want []any", status["enum"])
	}
	if len(enum) != 3 || enum[1] != nil {
		t.Errorf("$defs.Status.enum = %v; the copy must keep every member, null included", enum)
	}

	// The same null outside $defs is stripped, which is what makes the
	// preserved copy meaningful.
	rows, ok := got["results"].([]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("results = %v, want one row", got["results"])
	}
	row := rows[0].(map[string]any)
	if _, present := row["note"]; present {
		t.Errorf("results[0].note survived strip_nulls: %v", row)
	}
}

// on_empty fires when shaping left an empty object or array behind. It stays
// silent for a scalar body, which is not a result set at all.
func TestApplyOnEmptyDiscriminatesEmptyShapes(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"empty_array", `[]`, "nothing here"},
		{"empty_object", `{}`, "nothing here"},
		{"scalar_string", `"hello"`, ""},
		{"scalar_number", `7`, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &profile.Profile{OnEmpty: "nothing here"}

			out, err := profile.Apply(p, profile.ApplyInput{Body: []byte(tc.body), UserFormat: "json"})
			if err != nil {
				t.Fatalf("Apply: %v", err)
			}
			if out.OnEmptyMessage != tc.want {
				t.Errorf("OnEmptyMessage = %q, want %q", out.OnEmptyMessage, tc.want)
			}
		})
	}
}

// The row stages look for a single record array. A scalar body has none, so
// limit, sort_by and dedupe pass it through untouched instead of failing.
func TestApplyRowStagesPassScalarBodiesThrough(t *testing.T) {
	p := &profile.Profile{
		Limit:  1,
		SortBy: "id",
		Dedupe: &profile.DedupeSpec{By: []string{"id"}},
	}
	body := []byte(`"a bare string response"`)

	out, err := profile.Apply(p, profile.ApplyInput{Body: body, UserFormat: "json"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if strings.TrimSpace(string(out.Body)) != `"a bare string response"` {
		t.Errorf("Body = %s; the row stages must leave a scalar alone", out.Body)
	}
}

// truncate_strings resolves a per-field limit by dot-path first and by bare
// field name second. A bare name in the table has to reach a nested field whose
// dot-path is not listed, or the shorthand is useless below the top level.
func TestTruncateStringsFallsBackToTheBareFieldName(t *testing.T) {
	p := &profile.Profile{
		TruncateStrings: &profile.TruncateStringsSpec{
			Fields: map[string]int{"note": 5},
		},
	}
	body := []byte(`{"meta":{"note":"abcdefghijkl"}}`)

	out, err := profile.Apply(p, profile.ApplyInput{Body: body, UserFormat: "json"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(out.Body, &got); err != nil {
		t.Fatalf("unmarshal shaped body: %v", err)
	}
	meta, ok := got["meta"].(map[string]any)
	if !ok {
		t.Fatalf("meta = %T, want map[string]any", got["meta"])
	}
	note, _ := meta["note"].(string)
	if len([]rune(note)) != 5 {
		t.Errorf("meta.note = %q (%d runes), want the 5-char bare-name limit", note, len([]rune(note)))
	}
	if meta["note_truncated"] != true {
		t.Errorf("meta.note_truncated = %v, want true", meta["note_truncated"])
	}
}
