package dispatch

import (
	"errors"

	"github.com/ehmo/gum/internal/sanitize"
)

// scrubErrorEnvelope applies the spec §11 layer-2 syntactic scrubber to the
// caller-visible text of err and reports whether anything was replaced.
//
// The upstream error body is the one payload gum returns that is both
// attacker-influenced and not the answer the caller asked for. A Google 400
// echoes the argument that failed validation, so a calendar event title or a
// Drive file name written by a third party reaches the model inside the
// message. Scrubbing it costs nothing a diagnostic needs; scrubbing a success
// body would delete the mail the caller asked to read (§11).
//
// Both halves of the envelope are covered: Message, which §4.1 emits as
// "message", and every Detail value, which §4.1 emits at the top level
// alongside it.
func scrubErrorEnvelope(err error) bool {
	var se *StructuredError
	if !errors.As(err, &se) || se == nil {
		return false
	}

	scrubbed := false
	if message, changed := sanitize.Scrub(se.Message); changed {
		se.Message = message
		scrubbed = true
	}

	for key, value := range se.Detail {
		cleaned, changed := sanitize.ScrubJSON(value)
		if !changed {
			continue
		}
		se.Detail[key] = cleaned
		scrubbed = true
	}

	return scrubbed
}
