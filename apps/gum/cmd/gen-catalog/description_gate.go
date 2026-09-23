package main

import (
	"errors"
	"fmt"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/sanitize"
)

// errDescriptionRejected reports a catalog description that fails the §5.4
// build-time sanitizer. It is a build failure, not a warning: a rejected
// title or summary reaches the model through gum.search_apis and
// gum.describe_op, and nothing downstream re-checks it.
var errDescriptionRejected = errors.New("catalog: description fails the build-time sanitizer")

// metaServiceFamily selects the meta token budget (rule 5) instead of the
// convenience budget (rule 4).
const metaServiceFamily = "meta"

// validateOpDescriptions is the spec §5.4 "Build" firing point for the 13-rule
// description sanitizer. Before this gate the sanitizer had no production
// caller at all: internal/sanitize was imported only by its own tests and by
// TestSanitizerEnforcement, which rebuilds ops from the Gmail and Calendar
// discovery fixtures and never sees the shipped catalog. Every other service
// family reached catalog.json unchecked.
//
// Every rule is checked against title and summary on their own. Four are also
// properties of the op's description as a whole and run again over the joined
// text:
//
//   - Rule 6, risk disclosure. "Trash Gmail thread" plus "Permanently deleted
//     after 30 days" discloses even though neither field discloses alone, so the
//     per-field runs pass an empty risk class to skip it.
//   - Rules 9 and 10, pseudo-instruction tags and injection directives. A
//     payload can be split across the pair to evade a per-field match, which is
//     why §5.4 requires the concatenation re-scan.
//   - Rule 2, model hints. The phrases are multiword, so "designed for" in a
//     title and "AI agents" in a summary join into one neither field matches.
func validateOpDescriptions(cat *catalog.Catalog) error {
	if cat == nil {
		return nil
	}

	var errs []error

	for _, op := range cat.Ops {
		toolKind := sanitize.ToolKindConvenience
		if op.ServiceFamily == metaServiceFamily {
			toolKind = sanitize.ToolKindMeta
		}

		// perFieldRules records which rules a single field already failed, so
		// the cross-field re-scan reports only what the per-field runs could
		// not see. A rule that fired on title and fires again on the join for a
		// different span is reported once: the build fails either way, and the
		// curator re-runs the gate after the fix.
		perFieldRules := map[sanitize.Rule]bool{}

		for _, field := range []struct {
			name  string
			value string
		}{
			{"title", op.Title},
			{"summary", op.Summary},
		} {
			_, violations, err := sanitize.Sanitize(field.value, toolKind, "")
			if err != nil {
				errs = append(errs, fmt.Errorf("op %s: %s: %w", op.OpID, field.name, err))
				continue
			}
			for _, v := range violations {
				perFieldRules[v.Rule] = true
				errs = append(errs, fmt.Errorf("op %s: %s: rule %d: %s: %w",
					op.OpID, field.name, v.Rule, v.Reason, errDescriptionRejected))
			}
		}

		// §5.4 second pass. catalog.Op.Validate already ran the denylist over
		// every risk_override_reason; the sanitizer follows it on the
		// validated value. Tool kind and risk class are empty because a
		// reason has neither: rules 4 and 5 budget a tool description and
		// rule 6 judges an op, so only 1, 2, 3 and 7 apply here.
		for _, v := range op.Variants {
			if v.RiskOverrideReason == "" {
				continue
			}
			_, violations, err := sanitize.Sanitize(v.RiskOverrideReason, "", "")
			if err != nil {
				errs = append(errs, fmt.Errorf("op %s: variant %s: risk_override_reason: %w", op.OpID, v.VariantID, err))
				continue
			}
			for _, viol := range violations {
				errs = append(errs, fmt.Errorf("op %s: variant %s: risk_override_reason: rule %d: %s: %w",
					op.OpID, v.VariantID, viol.Rule, viol.Reason, errDescriptionRejected))
			}
		}

		// The joined pass runs for every op, not just the mutating ones: rule 6
		// needs a write or destructive risk class to fire at all, but the rule
		// 9 and 10 re-scan applies to a read op too.
		riskClass := defaultVariantRiskClass(op)
		_, violations, err := sanitize.Sanitize(op.Title+" "+op.Summary, "", riskClass)
		if err != nil {
			errs = append(errs, fmt.Errorf("op %s: title+summary: %w", op.OpID, err))
			continue
		}
		for _, v := range violations {
			if !crossFieldRules[v.Rule] || perFieldRules[v.Rule] {
				continue // per-field property, or already reported per field
			}
			errs = append(errs, fmt.Errorf("op %s: title+summary: rule %d: %s: %w",
				op.OpID, v.Rule, v.Reason, errDescriptionRejected))
		}
	}

	return errors.Join(errs...)
}

// crossFieldRules is the set §5.4 evaluates over title+summary rather than over
// each field alone. Every other rule the joined pass reports has already been
// reported per field, where the error names which field to fix.
//
// Each member has a phrase that can straddle the field boundary: a disclosure
// spread over the pair (rule 6), a tag or directive split at the join (rules 9
// and 10), and a multiword model hint whose two halves sit in different fields
// (rule 2, "... designed for" plus "AI ..."). Rule 3 is deliberately absent:
// its pronouns are single tokens matched on word boundaries, and joining two
// fields with a space cannot produce one that neither field carried.
var crossFieldRules = map[sanitize.Rule]bool{
	sanitize.RuleNoModelHints:          true,
	sanitize.RuleRequireRiskDisclosure: true,
	sanitize.RuleNoInstructionTags:     true,
	sanitize.RuleNoInjectionDirectives: true,
}

// defaultVariantRiskClass returns the risk class rule 6 judges the op by. An
// op whose default variant is missing is left to Op.Validate, so this reports
// the empty string and the caller skips rule 6.
func defaultVariantRiskClass(op catalog.Op) string {
	for _, v := range op.Variants {
		if v.VariantID == op.DefaultVariantID {
			return string(v.RiskClass)
		}
	}

	return ""
}
