package sanitize

import (
	"regexp"
	"strings"
)

// RedactionMarker replaces every span the layer-2 scrubber matches. A marker
// is used instead of a silent deletion so an operator reading an error can
// see that text was removed rather than that upstream said nothing.
const RedactionMarker = "[redacted]"

// chatControlTokenPattern and pseudoRoleTagPattern are the two halves of the
// role-marker alternation. They are separate constants because rule 10 of the
// build-time sanitizer (hardening.go) needs the control tokens without the
// pseudo-role tags: rule 9 already owns the tags and reports them with a
// message about tags rather than about directives.
const (
	chatControlTokenPattern = `<\|(?:im_start|im_end|system|user|assistant|endoftext)\|>|\[/?INST\]|<</?SYS>>`
	pseudoRoleTagPattern    = `</?(?:system|assistant|human)\s*>`
)

// Layer-2 patterns. Each one matches a construct that only appears in text
// written to steer a model, never in a Google API diagnostic. Keeping the set
// narrow is deliberate: a pattern that also fires on genuine error text turns
// the scrubber into a source of misleading diagnostics.
var (
	// scrubRoleMarkersRe matches chat-template control tokens and pseudo-role
	// tags. These delimit turns in a prompt; an upstream error body has no
	// legitimate reason to carry one.
	scrubRoleMarkersRe = regexp.MustCompile(
		`(?i)` + chatControlTokenPattern + `|` + pseudoRoleTagPattern,
	)

	// scrubOverrideRe matches instruction-override phrasing, the canonical
	// "ignore previous instructions" family.
	scrubOverrideRe = regexp.MustCompile(
		`(?i)\b(?:ignore|disregard|forget|override)\s+(?:all\s+|any\s+|the\s+|your\s+|my\s+|these\s+|those\s+|previous\s+|prior\s+|earlier\s+|above\s+|preceding\s+)+(?:instructions?|prompts?|rules?|directions?|context|guardrails?)\b`,
	)

	// scrubOverrideEverythingRe matches the same family phrased without a
	// noun for what is being overridden ("ignore everything above").
	scrubOverrideEverythingRe = regexp.MustCompile(
		`(?i)\b(?:ignore|disregard|forget)\s+(?:everything|anything|all)\s+(?:above|before|prior|previous|earlier|preceding)\b`,
	)

	// scrubReassignRe matches persona and task reassignment: the injection
	// tells the model what it now is, or announces a replacement instruction
	// block.
	scrubReassignRe = regexp.MustCompile(
		`(?i)\byou\s+are\s+now\s+(?:a|an|the)\b|\bnew\s+(?:instructions?|system\s+prompt|task)\s*:|\bsystem\s+prompt\s*:`,
	)
)

// scrubPatterns is the ordered set the scrubber applies. Order does not
// change the result — the patterns match disjoint constructs — but a fixed
// order keeps the output deterministic when two of them overlap.
var scrubPatterns = []*regexp.Regexp{
	scrubRoleMarkersRe,
	scrubOverrideRe,
	scrubOverrideEverythingRe,
	scrubReassignRe,
}

// Scrub is the spec §11 layer-2 syntactic scrubber. It replaces every known
// prompt-injection construct in s with RedactionMarker and reports whether
// anything was replaced.
//
// It runs over upstream error text only (§7, §12.4). Success bodies are not
// scrubbed: they carry the mail, documents and calendar entries the caller
// asked for, and deleting a phrase out of a message body would corrupt the
// answer rather than defend it. §11 states that scope.
func Scrub(s string) (string, bool) {
	if s == "" {
		return s, false
	}
	out := s
	for _, re := range scrubPatterns {
		out = re.ReplaceAllLiteralString(out, RedactionMarker)
	}
	if out == s {
		return s, false
	}
	return collapseMarkers(out), true
}

// collapseMarkers folds a run of adjacent markers into one. Two patterns can
// match neighbouring spans ("<system>ignore previous instructions"), and a
// caller reading "[redacted][redacted]" learns nothing the single marker does
// not say.
func collapseMarkers(s string) string {
	doubled := RedactionMarker + RedactionMarker
	for strings.Contains(s, doubled) {
		s = strings.ReplaceAll(s, doubled, RedactionMarker)
	}
	return s
}

// ScrubJSON applies Scrub to every string inside a decoded JSON value and
// reports whether any of them changed. Maps and slices are walked in place;
// every other type is returned untouched.
//
// Error details reach the caller as a nested tree (§4.1 emits detail keys at
// the top level of the envelope), so scrubbing the message alone would leave
// an injected string one key away from the model.
func ScrubJSON(v any) (any, bool) {
	switch typed := v.(type) {
	case string:
		return Scrub(typed)
	case map[string]any:
		changed := false
		for k, elem := range typed {
			scrubbed, elemChanged := ScrubJSON(elem)
			if elemChanged {
				typed[k] = scrubbed
				changed = true
			}
		}
		return typed, changed
	case []any:
		changed := false
		for i, elem := range typed {
			scrubbed, elemChanged := ScrubJSON(elem)
			if elemChanged {
				typed[i] = scrubbed
				changed = true
			}
		}
		return typed, changed
	case []string:
		changed := false
		for i, elem := range typed {
			scrubbed, elemChanged := Scrub(elem)
			if elemChanged {
				typed[i] = scrubbed
				changed = true
			}
		}
		return typed, changed
	default:
		return v, false
	}
}
