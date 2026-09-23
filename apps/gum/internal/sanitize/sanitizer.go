// Package sanitize holds gum's two text defenses. They share a package
// because both are pure text transforms with no other internal dependency,
// which is what lets the build-time generator import one of them (§14 rule 4).
// They are otherwise unrelated and never run on the same input.
//
// Sanitize, in this file, is the 13-rule build-time description sanitizer
// (spec.md §5.4, "Build-time description sanitizer"). It rejects a description,
// it never repairs one, and it never sees an upstream response body. Two
// callers gate on it: validateOpDescriptions in cmd/gen-catalog fails the
// build on a violation in any op title or summary, and plugins.LoadManifest
// fails the manifest load on a violation in any advertised tool description.
//
// Rules 1-7 live in this file and judge how a description is written. Rules
// 8-13 live in hardening.go and judge whether it is addressing the model
// instead of describing an operation.
//
// Scrub, in scrub.go, is the §11 layer-2 runtime syntactic scrubber. It
// repairs rather than rejects, and it runs over upstream error text at the
// dispatch boundary. See scrub.go for why its scope stops at error text.
//
// Rules (applied in order):
//  1. RuleNoMarketing       — reject marketing language ("revolutionary", "best-in-class", etc.)
//  2. RuleNoModelHints      — reject LLM-oriented hints ("designed for AI", "easy for LLMs", etc.)
//  3. RuleNoSecondPerson    — reject second-person address ("you can use this to...", "your", etc.)
//  4. RuleTokenBudgetConvenience — description ≤220 cl100k tokens when toolKind="convenience"
//  5. RuleTokenBudgetMeta   — description ≤360 cl100k tokens when toolKind="meta"
//  6. RuleRequireRiskDisclosure — write/destructive ops must contain a risk-disclosure
//     phrase. A destructive op must name the loss ("permanently deletes"); a write op
//     must name the mutation ("appends rows", "submits a sitemap").
//  7. RuleNoPIIPatterns     — reject email addresses, phone numbers, SSN patterns
//  8. RuleNoCompatibilityChars — reject a codepoint whose NFKD form is plain ASCII
//  9. RuleNoInstructionTags  — reject <system>, <prompt>, <instruction>, <context>
//  10. RuleNoInjectionDirectives — reject override, reassignment and directive headers
//  11. RuleNoSecretWithPath  — reject a credential-shaped token beside a filesystem path
//  12. RuleNoOpaqueBlob      — reject an encoded run that carries hidden text
//  13. RuleDescriptionRuneCap — third-party description ≤400 codepoints when toolKind="plugin"
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
	// RuleNoCompatibilityChars rejects a non-ASCII codepoint whose NFKD
	// decomposition is plain ASCII, the shape a homoglyph payload takes.
	RuleNoCompatibilityChars
	// RuleNoInstructionTags rejects pseudo-instruction tags such as <system>.
	RuleNoInstructionTags
	// RuleNoInjectionDirectives rejects text that instructs a reader rather
	// than describing an operation.
	RuleNoInjectionDirectives
	// RuleNoSecretWithPath rejects a credential-shaped token that appears
	// beside a filesystem path.
	RuleNoSecretWithPath
	// RuleNoOpaqueBlob rejects an encoded run long enough to carry a payload.
	RuleNoOpaqueBlob
	// RuleDescriptionRuneCap enforces ≤400 codepoints on a third-party tool
	// description (tool kind "plugin").
	RuleDescriptionRuneCap
)

// Tool kinds. The kind selects the rule 4 or rule 5 token budget, and
// ToolKindPlugin additionally selects the rule 13 codepoint cap: a plugin
// description is written by whoever published the plugin, so it is bounded
// harder than curator-authored catalog text.
const (
	ToolKindConvenience = "convenience"
	ToolKindMeta        = "meta"
	ToolKindPlugin      = "plugin"
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

	// destructiveDisclosureRe is the rule 6 vocabulary for risk_class
	// "destructive". It matches any inflection of the listed verbs, so an op
	// title like "Delete Gmail label" satisfies the rule alongside a summary
	// like "Permanently delete a draft. Unrecoverable.". A destructive op that
	// only says where the resource went ("Move a Gmail message to the Trash")
	// does not disclose, so the euphemism list stays out.
	destructiveDisclosureRe = regexp.MustCompile(
		`(?i)\b(send(s|ing|er)?|sent|delete(s|d|ing)?|create(s|d|ing)?|update(s|d|ing)?|modif(y|ies|ied|ying)|remove(s|d|ing)?|write(s|n|ing)?|wrote|permanently|irreversible|cannot be undone)\b`,
	)

	// writeDisclosureRe is the rule 6 vocabulary for risk_class "write". It
	// extends destructiveDisclosureRe with the rest of the mutation verbs the
	// catalog uses, because a write op discloses by naming the mutation it
	// performs: "Append Rows to a Sheet" and "Submit a sitemap" say as much as
	// "Creates a draft". The stronger destructive list is deliberately not
	// widened this way.
	writeDisclosureRe = regexp.MustCompile(
		`(?i)\b(` + strings.Join(writeDisclosureVerbs, `|`) + `)\b`,
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

// writeDisclosureVerbs are the regex alternatives that satisfy rule 6 for a
// write-class op, on top of the destructive vocabulary. Each names a mutation
// the first-party catalog or a plugin manifest actually performs; none of them
// is strong enough on its own for a destructive op.
var writeDisclosureVerbs = []string{
	`send(s|ing|er)?`, `sent`,
	`delete(s|d|ing)?`,
	`create(s|d|ing)?`,
	`update(s|d|ing)?`,
	`modif(y|ies|ied|ying)`,
	`remove(s|d|ing)?`,
	`write(s|n|ing)?`, `wrote`,
	`permanently`, `irreversible`, `cannot be undone`,
	`add(s|ed|ing)?`,
	`append(s|ed|ing)?`,
	`clear(s|ed|ing)?`,
	`close(s|d)?`, `closing`,
	`cop(y|ies|ied|ying)`,
	`edit(s|ed|ing)?`,
	`grant(s|ed|ing)?`,
	`import(s|ed|ing)?`,
	`ingest(s|ed|ing)?`,
	`insert(s|ed|ing)?`,
	`mov(e|es|ed|ing)`,
	`mutat(e|es|ed|ing)`,
	`patch(es|ed|ing)?`,
	`post(s|ed|ing)?`,
	`publish(es|ed|ing)?`,
	`renam(e|es|ed|ing)`,
	`replac(e|es|ed|ing|ement)`,
	`restor(e|es|ed|ing)`,
	`revok(e|es|ed|ing)`,
	`shar(e|es|ed|ing)`,
	`submit(s|ted|ting)?`,
	`(un)?subscrib(e|es|ed|ing)`,
	`(un)?trash(es|ed|ing)?`,
	`upload(s|ed|ing)?`,
}

// budgetCodec returns the cl100k tokenizer that rules 4 and 5 count with.
// It is a package var so a test can drive the two SANITIZER_TOKENIZER_FAILED
// arms; production always gets the real cl100k_base codec.
var budgetCodec = func() (tokenizer.Codec, error) {
	return tokenizer.Get(tokenizer.Cl100kBase)
}

// Sanitize returns the rewritten description and any violations.
//
//   - toolKind ∈ {"meta","convenience","plugin"}; if empty, no token-budget
//     rule fires. "plugin" takes the convenience budget plus rule 13.
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

	// Rules 4 & 5: Token budget (only when toolKind is set). A plugin
	// description is a convenience description with a tighter cap, so it is
	// budgeted the same way.
	if toolKind == ToolKindConvenience || toolKind == ToolKindMeta || toolKind == ToolKindPlugin {
		enc, err := budgetCodec()
		if err != nil {
			return "", nil, fmt.Errorf("SANITIZER_TOKENIZER_FAILED: %w", err)
		}
		ids, _, err := enc.Encode(description)
		if err != nil {
			return "", nil, fmt.Errorf("SANITIZER_TOKENIZER_FAILED: %w", err)
		}
		n := len(ids)

		if (toolKind == ToolKindConvenience || toolKind == ToolKindPlugin) && n > 220 {
			violations = append(violations, Violation{
				Rule:      RuleTokenBudgetConvenience,
				Offending: fmt.Sprintf("%d tokens", n),
				Reason:    fmt.Sprintf("convenience tool description exceeds 220 cl100k tokens (got %d)", n),
			})
		} else if toolKind == ToolKindMeta && n > 360 {
			violations = append(violations, Violation{
				Rule:      RuleTokenBudgetMeta,
				Offending: fmt.Sprintf("%d tokens", n),
				Reason:    fmt.Sprintf("meta tool description exceeds 360 cl100k tokens (got %d)", n),
			})
		}
	}

	// Rule 6: Risk disclosure for write/destructive ops. A destructive op must
	// name the loss; a write op must name the mutation.
	if riskClass == "write" || riskClass == "destructive" {
		disclosure := destructiveDisclosureRe
		if riskClass == "write" {
			disclosure = writeDisclosureRe
		}
		if !disclosure.MatchString(description) {
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

	// Rules 8-13: injection hardening (hardening.go).
	violations = append(violations, hardeningViolations(description, toolKind)...)

	sanitized := strings.TrimRight(description, " \t\r\n")
	return sanitized, violations, nil
}
