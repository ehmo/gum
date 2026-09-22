package profile_test

import (
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/output/profile"
)

// gum-9l5c: the raw hint used to read "Use --format raw for the complete
// body." on every trimmed response. On a merged keyword batch that advice
// costs the caller matchedInputs, because raw bypasses the adapter annotator
// that adds it, and the notice said nothing. These tests pin both arms.

func TestRawHintNamesTheAnnotationFieldsRawDrops(t *testing.T) {
	got := profile.ShapingNotice(profile.NoticeInput{
		DroppedPaths:    []string{"results.closeVariants"},
		RawHint:         "--format raw",
		AnnotationPaths: []string{"results.matchedInputs", "unmatchedInputs"},
	})

	want := "note: the output profile removed 1 field from this response: results.closeVariants." +
		" Use --format raw for the complete upstream body;" +
		" raw omits the gum-added fields results.matchedInputs, unmatchedInputs."
	if got != want {
		t.Fatalf("ShapingNotice = %q\nwant %q", got, want)
	}
}

func TestRawHintAgreesTheAnnotationNoun(t *testing.T) {
	got := profile.ShapingNotice(profile.NoticeInput{
		DroppedPaths:    []string{"results.closeVariants"},
		RawHint:         `format: "raw"`,
		AnnotationPaths: []string{"unmatchedInputs"},
	})

	if !strings.HasSuffix(got, ` Use format: "raw" for the complete upstream body; raw omits the gum-added field unmatchedInputs.`) {
		t.Errorf("ShapingNotice = %q; want the singular noun and the MCP hint", got)
	}
}

// The unannotated response keeps the wording every other surface and test
// already assert, so adding the clause cost nothing to the common case.
func TestRawHintUnchangedWithoutAnnotations(t *testing.T) {
	for name, in := range map[string]profile.NoticeInput{
		"nil": {
			DroppedPaths: []string{"results.closeVariants"},
			RawHint:      "--format raw",
		},
		"empty": {
			DroppedPaths:    []string{"results.closeVariants"},
			RawHint:         "--format raw",
			AnnotationPaths: []string{},
		},
	} {
		t.Run(name, func(t *testing.T) {
			got := profile.ShapingNotice(in)
			want := "note: the output profile removed 1 field from this response: results.closeVariants." +
				" Use --format raw for the complete body."
			if got != want {
				t.Errorf("ShapingNotice = %q\nwant %q", got, want)
			}
		})
	}
}

// No dropped field means no raw hint at all, so annotation paths on their own
// never print. Raw is only worth offering when shaping took something out.
func TestAnnotationPathsAloneAddNoHint(t *testing.T) {
	got := profile.ShapingNotice(profile.NoticeInput{
		DedupedRows:     2,
		RawHint:         "--format raw",
		AnnotationPaths: []string{"results.matchedInputs"},
	})

	if strings.Contains(got, "raw") {
		t.Errorf("ShapingNotice = %q; want no raw hint when the profile dropped no field", got)
	}
}
