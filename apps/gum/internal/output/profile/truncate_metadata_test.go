package profile_test

import (
	"encoding/json"
	"testing"

	"github.com/ehmo/gum/internal/output/profile"
)

// docs/profile-dsl-reference.md §2.8: "Truncated values are followed by sibling
// metadata `<field>_truncated = true`", and spec §9.1 step 6 caps text "with
// truncated: true metadata". Without the sibling a consumer cannot tell a
// clamped value from a genuine one (gum-zvlm).
func TestTruncateStringsWritesSiblingMetadata(t *testing.T) {
	p := &profile.Profile{
		TruncateStrings: &profile.TruncateStringsSpec{DefaultChars: 500, Fields: map[string]int{"snippet": 10}},
		DefaultFormat:   "json",
	}
	body := []byte(`{"snippet":"the quick brown fox jumps","subject":"short"}`)

	out, err := profile.Apply(p, profile.ApplyInput{Body: body, UserFormat: "json"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got["snippet_truncated"] != true {
		t.Errorf("snippet_truncated = %v; want true", got["snippet_truncated"])
	}
	if _, ok := got["subject_truncated"]; ok {
		t.Errorf("subject fit inside the limit but carries subject_truncated = %v", got["subject_truncated"])
	}
}

// The declared limit is the length of what the consumer receives. Appending the
// ellipsis after runes[:limit] returned limit+1 characters, so a profile that
// asked for 180 got 181 (gum-zvlm).
func TestTruncateStringsCapIncludesEllipsis(t *testing.T) {
	p := &profile.Profile{
		TruncateStrings: &profile.TruncateStringsSpec{DefaultChars: 5},
		DefaultFormat:   "json",
	}
	body := []byte(`{"note":"abcdefghij"}`)

	out, err := profile.Apply(p, profile.ApplyInput{Body: body, UserFormat: "json"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	note, _ := got["note"].(string)
	if n := len([]rune(note)); n != 5 {
		t.Errorf("note = %q (%d runes); want 5, the declared limit", note, n)
	}
	if note != "abcd…" {
		t.Errorf("note = %q; want abcd…", note)
	}
}
