package sanitize

import (
	"regexp"
	"strings"
)

// ExternalDataOpen and ExternalDataClose are the spec §11 layer-1 semantic
// markers. A model that honours the annotation reads what they enclose as data
// a third party wrote, not as instructions addressed to it.
const (
	ExternalDataOpen  = `<external_data trusted="false">`
	ExternalDataClose = `</external_data>`
)

// externalDataTagRe matches any external_data tag, open or close, with any
// attributes, any letter case and interior whitespace. A payload that carries
// one would otherwise close the fence early and put the rest of itself back in
// the model's instruction channel, so the neutralizer has to be at least as
// permissive as a model's own tag reader.
var externalDataTagRe = regexp.MustCompile(`(?i)<\s*/?\s*external_data\b[^>]*>`)

// MarkExternalData fences body in the layer-1 markers.
//
// The fence goes on at the MCP presentation boundary, not upstream of it. The
// markers are bytes, and the §9.1 expression pipeline, the output profiles, the
// field masks, the TOON encoder, the `outputSchema` conformance check and the
// gain ledger's token math all measure the bytes of the shaped body. Fencing
// before any of those stages would make each one measure the fence.
//
// An empty body comes back untouched: a fence around nothing tells the model
// the call returned something it cannot see. A body that already carries the
// fence comes back untouched too, so two presentation boundaries cannot nest
// one fence inside another.
func MarkExternalData(body string) string {
	if body == "" {
		return body
	}
	if IsExternalDataMarked(body) {
		return body
	}

	return ExternalDataOpen + "\n" + neutralizeExternalDataTags(body) + "\n" + ExternalDataClose
}

// IsExternalDataMarked reports whether s is already fenced. It matches the
// exact markers MarkExternalData emits, because the question here is whether
// this code fenced the string, not whether the payload mentions the tag.
func IsExternalDataMarked(s string) bool {
	trimmed := strings.TrimSpace(s)

	return strings.HasPrefix(trimmed, ExternalDataOpen) && strings.HasSuffix(trimmed, ExternalDataClose)
}

// neutralizeExternalDataTags replaces every external_data tag inside a payload
// with RedactionMarker. Without it a calendar event titled `</external_data>`
// would end the fence, and everything after it would read as trusted text.
func neutralizeExternalDataTags(s string) string {
	if !externalDataTagRe.MatchString(s) {
		return s
	}

	return collapseMarkers(externalDataTagRe.ReplaceAllLiteralString(s, RedactionMarker))
}
