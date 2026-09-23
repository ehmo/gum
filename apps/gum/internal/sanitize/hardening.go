package sanitize

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// This file holds the injection-hardening half of the build-time description
// sanitizer, rules 8 through 13 of spec §5.4. Rules 1-7 in sanitizer.go judge
// whether a description is well written: marketing tone, model hints, second
// person, length, risk disclosure, PII. Rules 8-13 judge whether it is trying
// to talk to the model instead of describing an operation.
//
// The two halves defend different inputs. A first-party title comes from a
// Google discovery document through a curator, so its risk is sloppiness. A
// plugin's advertised_tools[].description comes from whoever published the
// plugin, reaches the model verbatim through gum.search_apis and
// gum.describe_op, and nothing downstream re-reads it. Rules 8-13 are what
// stand between that text and the model's instruction channel.
//
// Rules 9 through 12 run against the NFKD form of the description rather than
// the bytes as authored. Rule 8 already rejects the compatibility characters
// that would carry an obfuscated payload, but its predicate is deliberately
// narrow, so normalizing first keeps the pattern rules independent of it.

// descriptionRuneCap is the rule 13 bound, in Unicode codepoints, on a
// third-party tool description. It is not applied to curated first-party text:
// the shipped catalog carries a 636-codepoint summary, and rules 4 and 5
// already bound a curated description in cl100k tokens.
const descriptionRuneCap = 400

// opaqueRunMinLen and opaqueRunPaddedMinLen are the rule 12 lengths. A padded
// run is allowed to be shorter because the "=" tail is itself evidence: no
// identifier ends that way.
const (
	opaqueRunMinLen       = 24
	opaqueRunPaddedMinLen = 20
)

var (
	// injectionTagRe is rule 9. It matches the four pseudo-instruction tags
	// §5.4 names plus the pseudo-role tags the layer-2 scrubber knows, in the
	// permissive form a model's own tag reader would accept: any case, spaces
	// inside the brackets, attributes, open or close.
	injectionTagRe = regexp.MustCompile(
		`(?i)<\s*/?\s*(?:system|prompt|instructions?|context|assistant|human)\b[^>]*>`,
	)

	// chatControlTokenRe is the rule 10 arm for chat-template control tokens.
	// scrubRoleMarkersRe matches these too, at runtime, over upstream error
	// text; here they fail the build instead.
	chatControlTokenRe = regexp.MustCompile(`(?i)` + chatControlTokenPattern)

	// directiveHeaderRe is the rule 10 arm for a Markdown heading used as an
	// instruction header. A description is one or two sentences about an
	// operation; a heading that addresses a reader as "## Important" is
	// formatting borrowed from a prompt.
	directiveHeaderRe = regexp.MustCompile(
		`(?im)^\s*#{2,}\s*(?:instructions?|system|prompt|task|note to|important|rules?)\b`,
	)

	// secretTokenRe is the first half of rule 11: the shapes a credential
	// takes. The named providers are matched by prefix and length because
	// those two together are what makes a string a token rather than a word.
	secretTokenRe = regexp.MustCompile(
		`(?i)AIza[0-9A-Za-z_\-]{35}` +
			`|ya29\.[0-9A-Za-z_\-]{10,}` +
			`|gh[pousr]_[0-9A-Za-z]{20,}` +
			`|sk-[0-9A-Za-z]{20,}` +
			`|AKIA[0-9A-Z]{16}` +
			`|xox[baprs]-[0-9A-Za-z\-]{10,}` +
			`|\b(?:api[_\-]?key|secret|password|passwd|token|credential)\s*[:=]\s*[^\s,;]{8,}`,
	)

	// filesystemPathRe is the second half of rule 11: somewhere on disk to
	// read the credential from or write it to. The credential-bearing file
	// names are matched on their own because "~/.ssh/id_rsa" is the whole
	// instruction and needs no directory prefix to be one.
	filesystemPathRe = regexp.MustCompile(
		`(?i)(?:^|[\s"'(\[])(?:~/[\w.\-/]+|/(?:etc|var|home|root|proc|opt|tmp|Users)/[\w.\-/]+|\.\./[\w.\-/]+)` +
			`|\.ssh/|\bid_rsa\b|\.env\b|credentials\.json`,
	)

	// opaqueRunRe is rule 12's candidate matcher. RE2 has no lookaround, so
	// the character-class test that separates a base64 payload from a long
	// identifier lives in isOpaqueRun.
	opaqueRunRe = regexp.MustCompile(`[A-Za-z0-9+/]{20,}={0,2}`)
)

// hardeningViolations returns the rule 8-13 violations in description.
//
// toolKind selects rule 13 only: ToolKindPlugin means the text is
// third-party, so the codepoint cap applies. Every other rule here applies to
// every caller, because none of them has a legitimate form.
func hardeningViolations(description, toolKind string) []Violation {
	var violations []Violation

	if r, decomposed, found := compatibilityHomoglyph(description); found {
		violations = append(violations, Violation{
			Rule:      RuleNoCompatibilityChars,
			Offending: string(r),
			Reason: fmt.Sprintf("compatibility character U+%04X decomposes to %q; write the ASCII form",
				r, decomposed),
		})
	}

	normalized := norm.NFKD.String(description)

	if m := injectionTagRe.FindString(normalized); m != "" {
		violations = append(violations, Violation{
			Rule:      RuleNoInstructionTags,
			Offending: m,
			Reason:    fmt.Sprintf("pseudo-instruction tag: %q", m),
		})
	}

	for _, re := range directivePatterns {
		m := re.FindString(normalized)
		if m == "" {
			continue
		}
		violations = append(violations, Violation{
			Rule:      RuleNoInjectionDirectives,
			Offending: m,
			Reason:    fmt.Sprintf("instruction addressed to a reader: %q", m),
		})

		break
	}

	if secret := secretTokenRe.FindString(normalized); secret != "" {
		if path := filesystemPathRe.FindString(normalized); path != "" {
			violations = append(violations, Violation{
				Rule:      RuleNoSecretWithPath,
				Offending: secret,
				Reason: fmt.Sprintf("credential-shaped token %q together with path %q reads as an exfiltration step",
					secret, strings.TrimSpace(path)),
			})
		}
	}

	if run := opaqueRun(normalized); run != "" {
		violations = append(violations, Violation{
			Rule:      RuleNoOpaqueBlob,
			Offending: run,
			Reason:    fmt.Sprintf("opaque encoded run of %d characters: %q", len(run), run),
		})
	}

	if toolKind == ToolKindPlugin {
		if n := utf8.RuneCountInString(description); n > descriptionRuneCap {
			violations = append(violations, Violation{
				Rule:      RuleDescriptionRuneCap,
				Offending: fmt.Sprintf("%d codepoints", n),
				Reason: fmt.Sprintf("third-party tool description exceeds %d codepoints (got %d)",
					descriptionRuneCap, n),
			})
		}
	}

	return violations
}

// directivePatterns is the rule 10 set. The three override and reassignment
// patterns are shared with the layer-2 runtime scrubber: the same phrasing that
// gets redacted out of an upstream error at runtime fails the build when it
// appears in a description. Only the first match is reported; a description
// with one of these is rejected whole, so listing the rest adds nothing.
var directivePatterns = []*regexp.Regexp{
	chatControlTokenRe,
	scrubOverrideRe,
	scrubOverrideEverythingRe,
	scrubReassignRe,
	directiveHeaderRe,
}

// compatibilityHomoglyph returns the first codepoint in s that is non-ASCII and
// whose NFKD decomposition is nothing but ASCII letters, digits and spaces.
// Such a codepoint has an ASCII spelling that renders the same or nearly the
// same, which is what makes it useful for hiding a payload from rules 9-12:
// "ignore" written in mathematical bold matches no pattern.
//
// The predicate is narrow on purpose. An accented letter decomposes to a base
// letter plus a combining mark, and the mark is not ASCII, so "café" passes. An
// ellipsis decomposes to three periods, which are ASCII but not alphanumeric,
// so "…" passes. What does not pass is fullwidth text, mathematical
// alphanumerics, circled and parenthesized letters, superscript digits,
// ligatures, and a non-breaking space, which decomposes to a plain space and
// would otherwise defeat the \s+ in every pattern in rule 10.
func compatibilityHomoglyph(s string) (rune, string, bool) {
	for _, r := range s {
		if r <= unicode.MaxASCII {
			continue
		}

		decomposed := norm.NFKD.String(string(r))
		if decomposed == string(r) || !isASCIIAlnumOrSpace(decomposed) {
			continue
		}

		return r, decomposed, true
	}

	return 0, "", false
}

// isASCIIAlnumOrSpace reports whether s is non-empty and made only of ASCII
// letters, digits and spaces.
func isASCIIAlnumOrSpace(s string) bool {
	if s == "" {
		return false
	}

	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == ' ':
		default:
			return false
		}
	}

	return true
}

// opaqueRun returns the first run in s that reads as an encoded payload rather
// than as a word, or "" when there is none.
//
// The bare base64 alphabet is not a usable test on its own: the shipped catalog
// contains "productDestinationId" and "addParents/removeParents", both of which
// match it. What separates a payload from an identifier is the character mix. A
// base64 encoding of English prose carries digits as well as both letter cases,
// and a padded run ends in "=", which no identifier does.
func opaqueRun(s string) string {
	for _, candidate := range opaqueRunRe.FindAllString(s, -1) {
		if strings.HasSuffix(candidate, "=") && len(candidate) >= opaqueRunPaddedMinLen {
			return candidate
		}
		if len(candidate) < opaqueRunMinLen {
			continue
		}
		if hasDigit(candidate) && hasUpper(candidate) && hasLower(candidate) {
			return candidate
		}
	}

	return ""
}

func hasDigit(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool { return r >= '0' && r <= '9' })
}

func hasUpper(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool { return r >= 'A' && r <= 'Z' })
}

func hasLower(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool { return r >= 'a' && r <= 'z' })
}
