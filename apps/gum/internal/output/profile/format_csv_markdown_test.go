package profile_test

import (
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/output/profile"
)

// The §9.1 stage-8 encoder must produce the format it reports. Before this,
// "csv" and "markdown" fell through to the TOON arm while ApplyOutput.Format
// kept the requested name, so every consumer of the body got TOON bytes under
// a label they were not.

const listBody = `{"messages":[{"id":"m1","subject":"hello"},{"id":"m2","subject":"bye"}]}`

func TestApplyEncodesCSV(t *testing.T) {
	out, err := profile.Apply(&profile.Profile{}, profile.ApplyInput{
		Body:       []byte(listBody),
		UserFormat: "csv",
	})
	if err != nil {
		t.Fatalf("Apply csv: %v", err)
	}
	if out.Format != "csv" {
		t.Errorf("Format = %q, want csv", out.Format)
	}
	got := string(out.Body)
	want := "id,subject\nm1,hello\nm2,bye\n"
	if got != want {
		t.Errorf("csv body = %q, want %q", got, want)
	}
}

func TestApplyEncodesMarkdown(t *testing.T) {
	out, err := profile.Apply(&profile.Profile{}, profile.ApplyInput{
		Body:       []byte(listBody),
		UserFormat: "markdown",
	})
	if err != nil {
		t.Fatalf("Apply markdown: %v", err)
	}
	if out.Format != "markdown" {
		t.Errorf("Format = %q, want markdown", out.Format)
	}
	got := string(out.Body)
	if !strings.HasPrefix(got, "| id | subject |") {
		t.Errorf("markdown body does not start with a table header: %q", got)
	}
	if !strings.Contains(got, "| m1 | hello |") {
		t.Errorf("markdown body is missing the first row: %q", got)
	}
}

// TestApplyUnknownFormatStillEncodesTOON keeps the fallback for a name the
// encoder does not implement: TOON bytes, reported as TOON.
func TestApplyUnknownFormatStillEncodesTOON(t *testing.T) {
	out, err := profile.Apply(&profile.Profile{}, profile.ApplyInput{
		Body:       []byte(listBody),
		UserFormat: "yaml",
	})
	if err != nil {
		t.Fatalf("Apply yaml: %v", err)
	}
	if out.Format != "toon" {
		t.Errorf("Format = %q, want toon for an unimplemented format", out.Format)
	}
	if strings.Contains(string(out.Body), "| id |") {
		t.Errorf("unimplemented format produced a markdown table: %q", out.Body)
	}
}
