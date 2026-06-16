package sanitize_test

import (
	"strings"
	"testing"

	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/sanitize"
)

// hasViolation reports whether vs contains at least one violation for rule r.
func hasViolation(vs []sanitize.Violation, r sanitize.Rule) bool {
	for _, v := range vs {
		if v.Rule == r {
			return true
		}
	}
	return false
}

// ── Rule 1: RuleNoMarketing ──────────────────────────────────────────────────

// TestSanitizeMarketingRejected verifies that marketing superlatives ("revolutionary",
// "best-in-class", "industry-leading") trigger RuleNoMarketing.
func TestSanitizeMarketingRejected(t *testing.T) {
	defer goleak.VerifyNone(t)

	cases := []struct {
		desc string
		in   string
	}{
		{"revolutionary", "This revolutionary tool lists messages."},
		{"best-in-class", "Our best-in-class API gives you access."},
		{"industry-leading", "Use this industry-leading service to get results."},
		{"seamless", "Provides a seamless integration with Gmail."},
		{"cutting-edge", "A cutting-edge solution for email management."},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			_, vs, err := sanitize.Sanitize(tc.in, "convenience", "read")
			if err != nil {
				t.Fatalf("Sanitize: %v", err)
			}
			if !hasViolation(vs, sanitize.RuleNoMarketing) {
				t.Errorf("expected RuleNoMarketing violation for %q, got violations: %v", tc.in, vs)
			}
		})
	}
}

// TestSanitizeMarketingPasses verifies that a clean description does not
// trigger RuleNoMarketing.
func TestSanitizeMarketingPasses(t *testing.T) {
	defer goleak.VerifyNone(t)

	clean := "Lists messages in the authenticated user's Gmail mailbox."
	_, vs, err := sanitize.Sanitize(clean, "convenience", "read")
	if err != nil {
		t.Fatalf("Sanitize: %v", err)
	}
	if hasViolation(vs, sanitize.RuleNoMarketing) {
		t.Errorf("unexpected RuleNoMarketing violation for clean description: %v", vs)
	}
}

// ── Rule 2: RuleNoModelHints ─────────────────────────────────────────────────

// TestSanitizeNoModelHints verifies that LLM-oriented phrases trigger RuleNoModelHints.
func TestSanitizeNoModelHints(t *testing.T) {
	defer goleak.VerifyNone(t)

	cases := []struct {
		desc string
		in   string
	}{
		{"designed for AI", "This tool is designed for AI assistants to use."},
		{"easy for LLMs", "This is easy to understand for LLMs."},
		{"optimized for language models", "Optimized for language models to call directly."},
		{"AI-friendly", "An AI-friendly interface for Gmail."},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			_, vs, err := sanitize.Sanitize(tc.in, "convenience", "read")
			if err != nil {
				t.Fatalf("Sanitize: %v", err)
			}
			if !hasViolation(vs, sanitize.RuleNoModelHints) {
				t.Errorf("expected RuleNoModelHints violation for %q, got: %v", tc.in, vs)
			}
		})
	}
}

// TestSanitizeNoModelHintsPasses verifies a clean description does not trigger
// RuleNoModelHints.
func TestSanitizeNoModelHintsPasses(t *testing.T) {
	defer goleak.VerifyNone(t)

	clean := "Sends a message on behalf of the authenticated user."
	_, vs, err := sanitize.Sanitize(clean, "convenience", "write")
	if err != nil {
		t.Fatalf("Sanitize: %v", err)
	}
	if hasViolation(vs, sanitize.RuleNoModelHints) {
		t.Errorf("unexpected RuleNoModelHints violation: %v", vs)
	}
}

// ── Rule 3: RuleNoSecondPerson ───────────────────────────────────────────────

// TestSanitizeSecondPersonRejected verifies second-person pronouns trigger
// RuleNoSecondPerson.
func TestSanitizeSecondPersonRejected(t *testing.T) {
	defer goleak.VerifyNone(t)

	cases := []struct {
		desc string
		in   string
	}{
		{"you can", "You can use this to send messages."},
		{"your gmail", "Manages your Gmail inbox efficiently."},
		{"you'll", "You'll be able to list messages after authenticating."},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			_, vs, err := sanitize.Sanitize(tc.in, "convenience", "read")
			if err != nil {
				t.Fatalf("Sanitize: %v", err)
			}
			if !hasViolation(vs, sanitize.RuleNoSecondPerson) {
				t.Errorf("expected RuleNoSecondPerson violation for %q, got: %v", tc.in, vs)
			}
		})
	}
}

// TestSanitizeSecondPersonPasses verifies that third-person descriptions pass.
func TestSanitizeSecondPersonPasses(t *testing.T) {
	defer goleak.VerifyNone(t)

	clean := "Lists labels in the authenticated user's Gmail mailbox."
	_, vs, err := sanitize.Sanitize(clean, "convenience", "read")
	if err != nil {
		t.Fatalf("Sanitize: %v", err)
	}
	if hasViolation(vs, sanitize.RuleNoSecondPerson) {
		t.Errorf("unexpected RuleNoSecondPerson violation: %v", vs)
	}
}

// ── Rule 4: RuleTokenBudgetConvenience ───────────────────────────────────────

// TestSanitizeTokenBudgetConvenience verifies that a description exceeding
// 220 cl100k tokens triggers RuleTokenBudgetConvenience.
func TestSanitizeTokenBudgetConvenience(t *testing.T) {
	defer goleak.VerifyNone(t)

	// Generate a description that is definitely > 220 tokens. Each word is
	// roughly 1 token. 300 distinct filler words should comfortably exceed the
	// limit.
	long := strings.Repeat("word ", 300)
	_, vs, err := sanitize.Sanitize(long, "convenience", "read")
	if err != nil {
		t.Fatalf("Sanitize: %v", err)
	}
	if !hasViolation(vs, sanitize.RuleTokenBudgetConvenience) {
		t.Errorf("expected RuleTokenBudgetConvenience violation for long description, got: %v", vs)
	}
}

// TestSanitizeTokenBudgetConveniencePasses verifies a short description passes.
func TestSanitizeTokenBudgetConveniencePasses(t *testing.T) {
	defer goleak.VerifyNone(t)

	short := "Lists messages in a Gmail mailbox."
	_, vs, err := sanitize.Sanitize(short, "convenience", "read")
	if err != nil {
		t.Fatalf("Sanitize: %v", err)
	}
	if hasViolation(vs, sanitize.RuleTokenBudgetConvenience) {
		t.Errorf("unexpected RuleTokenBudgetConvenience violation for short description: %v", vs)
	}
}

// TestSanitizeTokenBudgetMeta verifies that a description exceeding 360 cl100k
// tokens triggers RuleTokenBudgetMeta when toolKind="meta".
func TestSanitizeTokenBudgetMeta(t *testing.T) {
	defer goleak.VerifyNone(t)

	// 500 words should be well over 360 tokens.
	long := strings.Repeat("word ", 500)
	_, vs, err := sanitize.Sanitize(long, "meta", "read")
	if err != nil {
		t.Fatalf("Sanitize: %v", err)
	}
	if !hasViolation(vs, sanitize.RuleTokenBudgetMeta) {
		t.Errorf("expected RuleTokenBudgetMeta violation for 500-word description, got: %v", vs)
	}
}

// ── Rule 6: RuleRequireRiskDisclosure ────────────────────────────────────────

// TestSanitizeRequireRiskDisclosure verifies that a destructive op description
// without "permanently deletes" (or equivalent) triggers RuleRequireRiskDisclosure.
func TestSanitizeRequireRiskDisclosure(t *testing.T) {
	defer goleak.VerifyNone(t)

	noDisclosure := "Move a Gmail message to the Trash."
	_, vs, err := sanitize.Sanitize(noDisclosure, "convenience", "destructive")
	if err != nil {
		t.Fatalf("Sanitize: %v", err)
	}
	if !hasViolation(vs, sanitize.RuleRequireRiskDisclosure) {
		t.Errorf("expected RuleRequireRiskDisclosure violation for destructive op without disclosure, got: %v", vs)
	}
}

// TestSanitizeRequireRiskDisclosurePasses verifies that a destructive op description
// containing a risk-disclosure phrase passes RuleRequireRiskDisclosure.
func TestSanitizeRequireRiskDisclosurePasses(t *testing.T) {
	defer goleak.VerifyNone(t)

	withDisclosure := "Move a Gmail message to the Trash. This permanently deletes the message after 30 days."
	_, vs, err := sanitize.Sanitize(withDisclosure, "convenience", "destructive")
	if err != nil {
		t.Fatalf("Sanitize: %v", err)
	}
	if hasViolation(vs, sanitize.RuleRequireRiskDisclosure) {
		t.Errorf("unexpected RuleRequireRiskDisclosure violation for op with disclosure: %v", vs)
	}
}

// TestSanitizeRequireRiskDisclosureReadSkipped verifies that a read-class op
// is not required to carry a risk-disclosure phrase.
func TestSanitizeRequireRiskDisclosureReadSkipped(t *testing.T) {
	defer goleak.VerifyNone(t)

	readOp := "Lists events on the specified Google Calendar."
	_, vs, err := sanitize.Sanitize(readOp, "convenience", "read")
	if err != nil {
		t.Fatalf("Sanitize: %v", err)
	}
	if hasViolation(vs, sanitize.RuleRequireRiskDisclosure) {
		t.Errorf("unexpected RuleRequireRiskDisclosure violation for read op: %v", vs)
	}
}

// ── Rule 7: RuleNoPIIPatterns ────────────────────────────────────────────────

// TestSanitizePIIPatterns verifies that email, phone, and SSN-like patterns
// trigger RuleNoPIIPatterns.
func TestSanitizePIIPatterns(t *testing.T) {
	defer goleak.VerifyNone(t)

	cases := []struct {
		desc string
		in   string
	}{
		{"email address", "Contact support at admin@example.com for help."},
		{"phone number", "Call 555-123-4567 for assistance."},
		{"SSN pattern", "Employee SSN: 123-45-6789 must be provided."},
		{"phone parentheses", "Reach us at (800) 555-0199."},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			_, vs, err := sanitize.Sanitize(tc.in, "convenience", "read")
			if err != nil {
				t.Fatalf("Sanitize: %v", err)
			}
			if !hasViolation(vs, sanitize.RuleNoPIIPatterns) {
				t.Errorf("expected RuleNoPIIPatterns violation for %q, got: %v", tc.in, vs)
			}
		})
	}
}

// TestSanitizePIIPatternsPasses verifies that a clean description does not
// trigger RuleNoPIIPatterns.
func TestSanitizePIIPatternsPasses(t *testing.T) {
	defer goleak.VerifyNone(t)

	clean := "Returns the list of labels in the user's mailbox."
	_, vs, err := sanitize.Sanitize(clean, "convenience", "read")
	if err != nil {
		t.Fatalf("Sanitize: %v", err)
	}
	if hasViolation(vs, sanitize.RuleNoPIIPatterns) {
		t.Errorf("unexpected RuleNoPIIPatterns violation for clean description: %v", vs)
	}
}

// TestSanitizePIIEmailPlaceholderAllowed pins sanitizer.go:173-174 — the
// email loop's placeholder `continue`. A description that matches the
// email regex but is the documented placeholder (example@example.com) or
// carries angle brackets (<addr> template forms) must NOT raise
// RuleNoPIIPatterns: these are illustrative, not real PII. Without the
// continue, doc authors couldn't show an example address at all.
func TestSanitizePIIEmailPlaceholderAllowed(t *testing.T) {
	defer goleak.VerifyNone(t)

	cases := []struct {
		desc string
		in   string
	}{
		{"example placeholder", "Send a message; the from address defaults to example@example.com when unset."},
		{"angle-bracket template", "Delivers to the configured <user>@<domain> recipient."},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			_, vs, err := sanitize.Sanitize(tc.in, "convenience", "read")
			if err != nil {
				t.Fatalf("Sanitize: %v", err)
			}
			if hasViolation(vs, sanitize.RuleNoPIIPatterns) {
				t.Errorf("placeholder email %q must not raise RuleNoPIIPatterns, got: %v", tc.in, vs)
			}
		})
	}
}

// ── All 7 rules summary ──────────────────────────────────────────────────────

// TestSanitizeAllRulesHaveTests is a compile-time proof that the 7 Rule
// constants can all be referenced. If a constant is removed or renamed, this
// test file will fail to compile.
func TestSanitizeAllRulesHaveTests(t *testing.T) {
	defer goleak.VerifyNone(t)

	rules := []sanitize.Rule{
		sanitize.RuleNoMarketing,
		sanitize.RuleNoModelHints,
		sanitize.RuleNoSecondPerson,
		sanitize.RuleTokenBudgetConvenience,
		sanitize.RuleTokenBudgetMeta,
		sanitize.RuleRequireRiskDisclosure,
		sanitize.RuleNoPIIPatterns,
	}
	if len(rules) != 7 {
		t.Errorf("expected 7 rules, got %d", len(rules))
	}
}
