package bench

import "github.com/ehmo/gum/internal/output/profile"

// ProfileForReleaseOp returns the canonical compact expression-profile used
// by the release-fixture savings calculation (bead gum-wqk4) for opID.
// The profile is a Go-literal stand-in for the catalog-embedded profiles
// that gum-l5b2 / gum-zev5 will eventually ship; it MUST stay narrow
// enough that the shaped fixtures hit the spec §1/§2 ≥80% aggregate
// savings floor against the naive baseline.
//
// Returns nil when opID has no registered profile, in which case the
// caller falls back to raw TOON re-encoding of the response body. Every
// op_id present in internal/bench/fixtures/release/manifest.json MUST
// map to a non-nil profile.
func ProfileForReleaseOp(opID string) *profile.Profile {
	p, ok := releaseProfiles[opID]
	if !ok {
		return nil
	}
	return p
}

// releaseProfiles is the literal registry. KeepFields uses dot-paths,
// applied recursively (see internal/output/profile/applier.go
// applyKeepFields). DefaultFormat="toon" ensures the shaped body is
// tokenised under the spec §9 default encoding.
var releaseProfiles = map[string]*profile.Profile{
	// Workspace read ops — keep only the user-relevant payload fields,
	// drop wireframe metadata that wouldn't change a downstream LLM's
	// decision (etag, kind, mime sub-fields, raw byte counts).

	"gmail.users.messages.list": {
		Name:          "release/gmail.users.messages.list",
		DefaultFormat: "toon",
		KeepFields:    []string{"messages.id", "messages.threadId", "messages.snippet"},
		StripNulls:    true,
	},

	"gmail.users.messages.get": {
		Name:          "release/gmail.users.messages.get",
		DefaultFormat: "toon",
		KeepFields:    []string{"id", "threadId", "snippet", "labelIds"},
		StripNulls:    true,
	},

	"gmail.users.messages.send": {
		Name:          "release/gmail.users.messages.send",
		DefaultFormat: "toon",
		KeepFields:    []string{"id", "threadId", "labelIds"},
		StripNulls:    true,
	},

	"gmail.users.messages.trash": {
		Name:          "release/gmail.users.messages.trash",
		DefaultFormat: "toon",
		KeepFields:    []string{"id", "threadId", "labelIds"},
		StripNulls:    true,
	},

	"drive.files.list": {
		Name:          "release/drive.files.list",
		DefaultFormat: "toon",
		KeepFields:    []string{"files.id", "files.name", "files.mimeType", "files.modifiedTime"},
		StripNulls:    true,
	},

	"drive.files.get": {
		Name:          "release/drive.files.get",
		DefaultFormat: "toon",
		KeepFields:    []string{"id", "name", "mimeType", "modifiedTime"},
		StripNulls:    true,
	},

	"drive.files.create": {
		Name:          "release/drive.files.create",
		DefaultFormat: "toon",
		KeepFields:    []string{"id", "name", "mimeType"},
		StripNulls:    true,
	},

	"drive.files.delete": {
		Name:          "release/drive.files.delete",
		DefaultFormat: "toon",
		// 204-style empty response; profile applies the no-op pass.
		StripNulls: true,
	},

	"calendar.events.list": {
		Name:          "release/calendar.events.list",
		DefaultFormat: "toon",
		KeepFields:    []string{"items.id", "items.summary", "items.start.dateTime", "items.end.dateTime", "items.status"},
		StripNulls:    true,
	},

	"calendar.events.insert": {
		Name:          "release/calendar.events.insert",
		DefaultFormat: "toon",
		KeepFields:    []string{"id", "summary", "start.dateTime", "end.dateTime", "status", "htmlLink"},
		StripNulls:    true,
	},

	"sheets.spreadsheets.values.get": {
		Name:          "release/sheets.spreadsheets.values.get",
		DefaultFormat: "toon",
		KeepFields:    []string{"range", "values"},
		StripNulls:    true,
	},

	"maps.places.searchText": {
		Name:          "release/maps.places.searchText",
		DefaultFormat: "toon",
		KeepFields:    []string{"places.id", "places.displayName.text", "places.rating", "places.location"},
		StripNulls:    true,
	},

	"youtube.search.list": {
		Name:          "release/youtube.search.list",
		DefaultFormat: "toon",
		KeepFields:    []string{"items.id.videoId", "items.snippet.title", "items.snippet.channelTitle", "items.snippet.publishedAt"},
		StripNulls:    true,
	},

	"bigquery.jobs.query": {
		Name:          "release/bigquery.jobs.query",
		DefaultFormat: "toon",
		KeepFields:    []string{"schema.fields.name", "schema.fields.type", "rows", "totalRows"},
		StripNulls:    true,
	},

	"genai.models.generateContent": {
		Name:          "release/genai.models.generateContent",
		DefaultFormat: "toon",
		KeepFields:    []string{"candidates.content.parts.text", "candidates.finishReason"},
		StripNulls:    true,
	},

	// gum_parallel outer envelope — keep the per-element data payload.
	// Inner shaping is handled by the per-op profile when the element's
	// op_id matches another entry in this registry; the simplest safe
	// approach for the outer is to retain the data block (which a naive
	// dispatcher would emit verbatim) and re-encode as TOON.
	"gum_parallel": {
		Name:          "release/gum_parallel",
		DefaultFormat: "toon",
		KeepFields:    []string{"batch_id", "results.data"},
		StripNulls:    true,
	},
}
