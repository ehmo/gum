// format_key_test.go — the profile DSL's wire-encoding key.
//
// Spec §9.1 and docs/expression-profile-dsl.md both name the key `format` with
// the enum toon|csv|json|markdown. The parser read `default_format` and allowed
// toon|json|raw, so every profile printed in the docs failed to load and the
// two encoders Apply already implements were unreachable from a profile.
package profile_test

import (
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/output/profile"
)

func TestParseReadsTheDocumentedFormatKey(t *testing.T) {
	p, err := profile.Parse("format = \"json\"\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if p.DefaultFormat != "json" {
		t.Errorf("DefaultFormat = %q; want %q", p.DefaultFormat, "json")
	}
}

// TestParseAcceptsCSVAndMarkdown covers the half of the enum Apply already
// encodes. Before the fix the parser rejected both, so no profile could select
// them and no fixture could assert them.
func TestParseAcceptsCSVAndMarkdown(t *testing.T) {
	for _, want := range []string{"toon", "csv", "json", "markdown"} {
		p, err := profile.Parse("format = \"" + want + "\"\n")
		if err != nil {
			t.Fatalf("Parse(format=%q): %v", want, err)
		}
		if p.DefaultFormat != want {
			t.Errorf("format=%q: DefaultFormat = %q", want, p.DefaultFormat)
		}
	}
}

// TestParseRejectsDefaultFormatKey pins the rename. default_format is the
// config key (output.default_format), not a profile key, and keeping both
// spellings would leave the docs describing one and the builtins using the
// other.
func TestParseRejectsDefaultFormatKey(t *testing.T) {
	_, err := profile.Parse("default_format = \"json\"\n")
	if err == nil {
		t.Fatal("Parse(default_format): no error; want unknown key")
	}
	if !strings.Contains(err.Error(), "default_format") || !strings.Contains(err.Error(), "format") {
		t.Errorf("error = %q; want it to name the old key and the new one", err)
	}
}

// TestParseRejectsRawAsAProfileFormat keeps the profile enum equal to the
// schema's. raw is a caller-side choice (--format raw) that skips shaping
// entirely; a profile that asked for it silently encoded TOON.
func TestParseRejectsRawAsAProfileFormat(t *testing.T) {
	_, err := profile.Parse("format = \"raw\"\n")
	if err == nil {
		t.Fatal("Parse(format=raw): no error; want an invalid-value error")
	}
	if !strings.Contains(err.Error(), "--format raw") {
		t.Errorf("error = %q; want it to point at the caller-side spelling", err)
	}
}

// TestFixtureExpectFormatCoversEveryReportedFormat pins the fixture key against
// the set ApplyOutput.Format can actually hold, which is the profile enum plus
// raw, because --format raw is reportable.
func TestFixtureExpectFormatCoversEveryReportedFormat(t *testing.T) {
	for _, want := range []string{"toon", "csv", "json", "markdown", "raw"} {
		src := "format = \"toon\"\n\n[[tests]]\nname = \"f\"\nfixture = \"x.json\"\nexpect_format = \"" + want + "\"\n"
		p, err := profile.Parse(src)
		if err != nil {
			t.Fatalf("Parse(expect_format=%q): %v", want, err)
		}
		if len(p.Tests) != 1 || p.Tests[0].ExpectFormat != want {
			t.Errorf("expect_format=%q: got %+v", want, p.Tests)
		}
	}
}
