// Package sanitize implements the 7-rule build-time and runtime description
// sanitizer (spec.md §5.4, §11).
//
// Rules (applied in order):
//  1. RuleNoMarketing       — reject marketing language ("revolutionary", "best-in-class", etc.)
//  2. RuleNoModelHints      — reject LLM-oriented hints ("designed for AI", "easy for LLMs", etc.)
//  3. RuleNoSecondPerson    — reject second-person address ("you can use this to...", "your", etc.)
//  4. RuleTokenBudgetConvenience — description ≤220 cl100k tokens when toolKind="convenience"
//  5. RuleTokenBudgetMeta   — description ≤360 cl100k tokens when toolKind="meta"
//  6. RuleRequireRiskDisclosure — write/destructive ops must contain a risk-disclosure phrase
//     (e.g. "permanently deletes" for destructive ops)
//  7. RuleNoPIIPatterns     — reject email addresses, phone numbers, SSN patterns
package sanitize

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/tiktoken-go/tokenizer"
)

// Rule is an enum of the 7 normative sanitizer rules from spec §5.4.
type Rule int

const (
	// RuleNoMarketing rejects marketing superlatives and promotional phrases.
	RuleNoMarketing Rule = iota + 1
	// RuleNoModelHints rejects LLM-oriented or AI-hint phrases.
	RuleNoModelHints
	// RuleNoSecondPerson rejects second-person address (you/your/you'll/etc.).
	RuleNoSecondPerson
	// RuleTokenBudgetConvenience enforces ≤220 cl100k tokens for convenience tools.
	RuleTokenBudgetConvenience
	// RuleTokenBudgetMeta enforces ≤360 cl100k tokens for meta tools.
	RuleTokenBudgetMeta
	// RuleRequireRiskDisclosure requires write/destructive ops to carry a
	// risk-disclosure phrase (e.g. "permanently deletes").
	RuleRequireRiskDisclosure
	// RuleNoPIIPatterns rejects email, phone, SSN-like patterns in descriptions.
	RuleNoPIIPatterns
)

// Violation reports which rule fired and the offending substring.
type Violation struct {
	Rule      Rule
	Offending string
	Reason    string
}

// ErrTokenizerFailure is returned by Sanitize when the cl100k tokenizer cannot
// count tokens (internal error, not a rule violation).
var ErrTokenizerFailure = errors.New("sanitize: tokenizer failure")

// Pre-compiled regexes for each rule.
var (
	ruleMarketingRe = regexp.MustCompile(
		`(?i)\b(revolutionary|best-in-class|industry-leading|cutting-edge|world-class|next-generation|state-of-the-art|game-changing|seamless(ly)?|effortless(ly)?|unparalleled|unprecedented|innovative)\b`,
	)

	ruleModelHintsRe = regexp.MustCompile(
		`(?i)(for (?:LLMs?|AI|models?|agents?|language models?)|LLM-friendly|AI-friendly|easy (?:to|for) (?:LLMs?|AI|models?) to (?:understand|parse|use)|designed for (?:AI|LLMs?|agents?|language models?)|optimi[sz]ed for (?:AI|LLMs?|language models?))`,
	)

	ruleSecondPersonRe = regexp.MustCompile(
		`(?i)\b(you|your|you're|you'll|you've|you'd)\b`,
	)

	// Risk disclosure phrases / verb forms (case-insensitive). Matches any
	// inflection of the listed verbs so that op titles like "Send Gmail
	// message" satisfy the rule alongside summaries like "Sends a Gmail
	// message".
	riskDisclosureRe = regexp.MustCompile(
		`(?i)\b(send(s|ing|er)?|sent|delete(s|d|ing)?|create(s|d|ing)?|update(s|d|ing)?|modif(y|ies|ied|ying)|remove(s|d|ing)?|write(s|n|ing)?|wrote|permanently|irreversible|cannot be undone)\b`,
	)

	// PII patterns
	piiEmailRe = regexp.MustCompile(
		`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`,
	)
	piiPhoneRe = regexp.MustCompile(
		`\b\+?\d{1,3}[-.\s]?\(?\d{2,4}\)?[-.\s]?\d{3,4}[-.\s]?\d{3,4}\b`,
	)
	piiSSNRe = regexp.MustCompile(
		`\b\d{3}-\d{2}-\d{4}\b`,
	)
)

// Sanitize returns the rewritten description and any violations.
//
//   - toolKind ∈ {"meta","convenience"}; if empty, no token-budget rule fires.
//   - riskClass ∈ {"read","write","destructive"}; if empty, RuleRequireRiskDisclosure
//     is skipped.
//
// Returns (sanitized, violations, error). error is non-nil only on internal
// tokenizer failure; violations is the actionable list for the caller to act on.
// The sanitized string is the description after any automated rewrites; if no
// rewrite is possible, it equals the input.
func Sanitize(description, toolKind, riskClass string) (string, []Violation, error) {
	var violations []Violation

	// Rule 1: No marketing language
	if m := ruleMarketingRe.FindString(description); m != "" {
		violations = append(violations, Violation{
			Rule:      RuleNoMarketing,
			Offending: m,
			Reason:    fmt.Sprintf("marketing language: %q", m),
		})
	}

	// Rule 2: No model hints
	if m := ruleModelHintsRe.FindString(description); m != "" {
		violations = append(violations, Violation{
			Rule:      RuleNoModelHints,
			Offending: m,
			Reason:    fmt.Sprintf("LLM/AI-oriented hint: %q", m),
		})
	}

	// Rule 3: No second person
	if m := ruleSecondPersonRe.FindString(description); m != "" {
		violations = append(violations, Violation{
			Rule:      RuleNoSecondPerson,
			Offending: m,
			Reason:    fmt.Sprintf("second-person pronoun: %q", m),
		})
	}

	// Rules 4 & 5: Token budget (only when toolKind is set)
	if toolKind == "convenience" || toolKind == "meta" {
		enc, err := tokenizer.Get(tokenizer.Cl100kBase)
		if err != nil {
			return "", nil, fmt.Errorf("SANITIZER_TOKENIZER_FAILED: %w", err)
		}
		ids, _, err := enc.Encode(description)
		if err != nil {
			return "", nil, fmt.Errorf("SANITIZER_TOKENIZER_FAILED: %w", err)
		}
		n := len(ids)

		if toolKind == "convenience" && n > 220 {
			violations = append(violations, Violation{
				Rule:      RuleTokenBudgetConvenience,
				Offending: fmt.Sprintf("%d tokens", n),
				Reason:    fmt.Sprintf("convenience tool description exceeds 220 cl100k tokens (got %d)", n),
			})
		} else if toolKind == "meta" && n > 360 {
			violations = append(violations, Violation{
				Rule:      RuleTokenBudgetMeta,
				Offending: fmt.Sprintf("%d tokens", n),
				Reason:    fmt.Sprintf("meta tool description exceeds 360 cl100k tokens (got %d)", n),
			})
		}
	}

	// Rule 6: Risk disclosure for write/destructive ops
	if riskClass == "write" || riskClass == "destructive" {
		if !riskDisclosureRe.MatchString(description) {
			violations = append(violations, Violation{
				Rule:      RuleRequireRiskDisclosure,
				Offending: "missing risk disclosure",
				Reason:    fmt.Sprintf("risk_class=%s requires explicit risk disclosure", riskClass),
			})
		}
	}

	// Rule 7: No PII patterns
	// Email check (skip placeholder forms)
	if emailMatches := piiEmailRe.FindAllString(description, -1); len(emailMatches) > 0 {
		for _, m := range emailMatches {
			// Allow: example@example.com placeholder or anything with < or >
			if m == "example@example.com" || strings.Contains(m, "<") || strings.Contains(m, ">") {
				continue
			}
			violations = append(violations, Violation{
				Rule:      RuleNoPIIPatterns,
				Offending: m,
				Reason:    fmt.Sprintf("PII pattern (email): %q", m),
			})
			break // report first
		}
	}

	// Phone check
	if phoneMatch := piiPhoneRe.FindString(description); phoneMatch != "" {
		violations = append(violations, Violation{
			Rule:      RuleNoPIIPatterns,
			Offending: phoneMatch,
			Reason:    fmt.Sprintf("PII pattern (phone): %q", phoneMatch),
		})
	}

	// SSN check
	if ssnMatch := piiSSNRe.FindString(description); ssnMatch != "" {
		violations = append(violations, Violation{
			Rule:      RuleNoPIIPatterns,
			Offending: ssnMatch,
			Reason:    fmt.Sprintf("PII pattern (SSN): %q", ssnMatch),
		})
	}

	sanitized := strings.TrimRight(description, " \t\r\n")
	return sanitized, violations, nil
}
