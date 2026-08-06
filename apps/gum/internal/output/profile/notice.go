package profile

import (
	"fmt"
	"strings"
)

// noticeMaxPaths caps how many dot-paths a notice names. A profile that keeps
// six fields out of a hundred would otherwise print a wall of text longer than
// the response it annotates.
const noticeMaxPaths = 8

// DroppedPathsNotice returns a one-line notice naming the response fields an
// expression profile removed, or "" when it removed nothing.
//
// rawHint is the surface-specific way to ask for the unshaped body (e.g.
// "--format raw"); it is omitted from the notice when empty. fullResultPath is
// the tee artifact holding the complete pre-shaping payload, also optional.
//
// The notice exists because a profile whitelist is otherwise invisible: the
// caller receives valid JSON with no marker, no count, and no way to tell that
// the operation's headline field is missing (gum-bpx0).
func DroppedPathsNotice(paths []string, rawHint, fullResultPath string) string {
	if len(paths) == 0 {
		return ""
	}
	noun := "fields"
	if len(paths) == 1 {
		noun = "field"
	}
	shown := paths
	suffix := ""
	if len(paths) > noticeMaxPaths {
		shown = paths[:noticeMaxPaths]
		suffix = fmt.Sprintf(", and %d more", len(paths)-noticeMaxPaths)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "note: the output profile removed %d %s from this response: %s%s.",
		len(paths), noun, strings.Join(shown, ", "), suffix)
	if rawHint != "" {
		fmt.Fprintf(&sb, " Use %s for the complete body.", rawHint)
	}
	if fullResultPath != "" {
		fmt.Fprintf(&sb, " Full result: %s", fullResultPath)
	}
	return sb.String()
}
