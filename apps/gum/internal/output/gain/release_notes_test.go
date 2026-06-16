package gain_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// TestReleaseNotesPublishBothFormatBaselines is the release gate for gum-8dl:
// spec §2 requires every release to publish *two* fixture-replay savings
// numbers — one for the shaped TOON default and one for the JSON default the
// caller sees when output is unshaped. The contract is enforced here so a
// future release cannot tag without both rows present.
//
// The test walks docs/release-notes-v*.md and asserts each file:
//  1. Contains the "Token savings" heading.
//  2. References both `toon` and `json` defaults inside a table.
//  3. Includes at least one numeric percentage for each row (so the columns
//     are not left as template placeholders).
//
// The release-notes-template.md is exempt because it intentionally ships with
// "___" placeholders.
func TestReleaseNotesPublishBothFormatBaselines(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	docsDir := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..", "..", "docs")

	entries, err := os.ReadDir(docsDir)
	if err != nil {
		t.Fatalf("read docs dir: %v", err)
	}

	tableRowRe := regexp.MustCompile(`\|\s*` + "`" + `(toon|json)` + "`" + `\s*\|[^|]*\|[^|]*\|[^|]*\|\s*([0-9]+(?:\.[0-9]+)?)\s*%`)

	checked := 0
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "release-notes-v") || !strings.HasSuffix(name, ".md") {
			continue
		}
		checked++
		path := filepath.Join(docsDir, name)
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		text := string(body)

		if !strings.Contains(text, "Token savings") {
			t.Errorf("%s: missing 'Token savings' section heading (spec §2 release-gate row)", name)
			continue
		}

		matches := tableRowRe.FindAllStringSubmatch(text, -1)
		hasTOON, hasJSON := false, false
		for _, m := range matches {
			switch m[1] {
			case "toon":
				hasTOON = true
			case "json":
				hasJSON = true
			}
		}
		if !hasTOON {
			t.Errorf("%s: missing TOON-default row with a numeric percentage", name)
		}
		if !hasJSON {
			t.Errorf("%s: missing JSON-default row with a numeric percentage", name)
		}
	}

	if checked == 0 {
		t.Fatal("no release-notes-v*.md found; release-gate cannot be enforced")
	}
}
