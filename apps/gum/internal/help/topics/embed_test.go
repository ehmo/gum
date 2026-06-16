package topics

import (
	"sort"
	"strings"
	"testing"
)

// TestErrTopicTooLargeError locks the HELP_TOPIC_TOO_LARGE message shape —
// the spec §11 sentinel + topic name + observed size + ceiling are all
// surfaced verbatim so an operator can see exactly which body overflowed.
func TestErrTopicTooLargeError(t *testing.T) {
	err := &ErrTopicTooLarge{Topic: "gmail", Size: 9000}
	msg := err.Error()
	for _, want := range []string{
		"HELP_TOPIC_TOO_LARGE",
		`"gmail"`,
		"9000 bytes",
		"max 8192",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("Error()=%q, missing %q", msg, want)
		}
	}
}

// TestValidateSizes is a smoke test for the build-time guard: the embedded
// bodies must all fit inside MaxTopicBytes (8 KiB). A regression here means
// someone landed an oversize topic and the gate didn't catch it locally.
func TestValidateSizes(t *testing.T) {
	if err := ValidateSizes(); err != nil {
		t.Errorf("ValidateSizes() = %v, want nil (all embedded topics must fit in 8 KiB)", err)
	}
}

// TestNames verifies the topic list:
//   - Returns the full set of embedded topics (one entry per *.md file).
//   - No file extensions in the returned names.
//   - Specific spec-mandated topics (auth, gmail, drive, sheets, calendar,
//     docs, gain, plugins) are all present.
func TestNames(t *testing.T) {
	names := Names()
	if len(names) == 0 {
		t.Fatal("Names() returned empty slice; want >=8 embedded topics")
	}
	for _, n := range names {
		if strings.HasSuffix(n, ".md") {
			t.Errorf("Names() returned %q with .md suffix; want stripped", n)
		}
	}
	// Spot-check spec topics — exact list is intentionally not asserted so
	// new topics don't break this test.
	required := []string{"auth", "gmail", "drive", "sheets", "calendar", "docs", "gain", "plugins"}
	sort.Strings(names)
	for _, want := range required {
		i := sort.SearchStrings(names, want)
		if i >= len(names) || names[i] != want {
			t.Errorf("Names() missing required topic %q; got %v", want, names)
		}
	}
}
