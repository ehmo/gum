package topics

import (
	"strings"
	"testing"
)

// TestAuthTopicContainsConsoleWalkthrough is the acceptance test for gum-4ra:
// the gum://help/auth body must include a Google Cloud Console walkthrough for
// byo_oauth and compound auth so users can self-serve setup. The test pins the
// section heading plus each numbered step's pivotal verb so a future
// refactor cannot silently delete the walkthrough.
func TestAuthTopicContainsConsoleWalkthrough(t *testing.T) {
	body, ok := Read("auth")
	if !ok {
		t.Fatalf("Read(%q) returned ok=false", "auth")
	}
	text := string(body)

	mustContain := []string{
		"## Google Cloud Console setup (byo_oauth)",
		"https://console.cloud.google.com/projectcreate",
		"https://console.cloud.google.com/apis/library",
		"https://console.cloud.google.com/apis/credentials/consent",
		"https://console.cloud.google.com/apis/credentials",
		"OAuth client ID",
		"Desktop app",
		"gum auth use-oauth-client",
		"gum auth login --scope",
		"gum auth probe --scopes",
		"scope strings come from the catalog",
		"AUTH_SCOPE_MISSING",
	}
	for _, want := range mustContain {
		if !strings.Contains(text, want) {
			t.Errorf("auth.md missing required walkthrough fragment: %q", want)
		}
	}

	// Sanity: the walkthrough must come AFTER the existing "Errors" section so
	// readers scrolling top-to-bottom hit overview → quick commands → errors →
	// console steps. (Swapping the order would push verbose Console steps in
	// front of the high-frequency error troubleshooting block.)
	walkIdx := strings.Index(text, "## Google Cloud Console setup")
	errIdx := strings.Index(text, "## Errors")
	if walkIdx < 0 || errIdx < 0 || walkIdx < errIdx {
		t.Errorf("walkthrough must appear after the Errors section (walk=%d, err=%d)", walkIdx, errIdx)
	}
}
