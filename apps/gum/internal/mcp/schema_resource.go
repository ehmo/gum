// Spec §13 line 3156 + §8.2 line 1601: body materialiser for
// gum://schema/{ref}. This file owns the four-step resolution chain:
//
//  1. Decode the URI tail and validate it against the safe served-ref grammar
//     (`^[a-z0-9][a-z0-9._-]{0,127}$`, no `..`, no raw or URI-encoded path
//     separators). Grammar violations return RESOURCE_NOT_FOUND BEFORE any
//     filesystem path is built — that's the §8.2 line 1601 invariant.
//
//  2. Check the active first-party catalog snapshot for an op or variant
//     binding referencing the ref. If found and the embedded gen/schemas/
//     store carries a body file, return the JCS-canonical bytes.
//
//  3. Check the profile-local plugin inventory (plugin-catalog.json
//     variants[].schema_hashes). When the ref is owned by an active plugin
//     and the matching `<ref>.<sha256>.json` body exists under
//     `<profileDir>/plugin-schemas/`, return its bytes verbatim.
//
//  4. Anything else (unknown ref, inactive/quarantined owner, missing file
//     on disk) returns the canonical RESOURCE_NOT_FOUND envelope per the
//     §13 line 3156 closing sentence.
//
// The v0.1.0 first-party store has no generator yet (deferred to v0.2.0 via
// gum-zev5); the only embedded body is the `test-fixture.v1` placeholder used
// by TestSchemaResourceFirstPartyHit. Plugin schemas are fully functional in
// v0.1.0 because plugin install already writes `<ref>.<sha256>.json` to disk
// (see internal/plugins/install_registry.go).

package mcp

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/embedded"
	"github.com/ehmo/gum/internal/output/jcs"
)

// safeServedRefPattern is the §8.2 line 1601 grammar: lowercase ASCII alnum
// first char, then up to 127 lowercase alnum / dot / underscore / hyphen. The
// upper bound of 128 total characters keeps every ref well inside POSIX
// NAME_MAX (255) so the on-disk `<ref>.<sha256>.json` filename never exceeds
// 128 + 1 + 64 + 5 = 198 bytes.
var safeServedRefPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)

// isSafeServedRef enforces the §8.2 line 1601 grammar on a candidate ref.
// Returns true only when the ref matches the regex AND contains no `..`
// substring. Path-separator characters (`/`, `\`) are already rejected by the
// regex; this helper is the second gate beyond parseTemplateParam (which only
// blocks `/`, `?`, `#`) so callers cannot smuggle `..` or uppercase letters
// past us via the schema URI.
func isSafeServedRef(ref string) bool {
	if !safeServedRefPattern.MatchString(ref) {
		return false
	}
	if strings.Contains(ref, "..") {
		return false
	}
	return true
}

// decodeSchemaRef applies URL decoding to the raw URI tail. Per spec §13 line
// 3156 the grammar check runs AFTER URI decoding, so a percent-encoded slash
// (`%2f`) decodes to `/` and is then rejected by the regex. Decoding failures
// return the raw input so the grammar check still trips.
func decodeSchemaRef(raw string) string {
	decoded, err := url.PathUnescape(raw)
	if err != nil {
		return raw
	}
	return decoded
}

// resolveSchemaBody is the single entry point for the schema resource. It
// returns the JCS-canonical body bytes when the ref is reachable via the
// active first-party snapshot or the profile-local plugin inventory, or
// (nil, "<diagnostic>", false) when the canonical RESOURCE_NOT_FOUND envelope
// should be returned. The diagnostic string is plumbed into the envelope's
// `detail` field for operator observability.
func (s *Server) resolveSchemaBody(ref string) ([]byte, string, bool) {
	if !isSafeServedRef(ref) {
		return nil, "schema ref grammar rejected after URI decoding", false
	}
	if firstParty, ok := s.loadFirstPartySchema(ref); ok {
		return firstParty, "", true
	}
	if pluginBody, ok := s.loadPluginSchema(ref); ok {
		return pluginBody, "", true
	}
	return nil, "schema ref " + ref + " not in active snapshot or active plugin inventory", false
}

// loadFirstPartySchema walks the active catalog snapshot for any op or
// variant binding that references `ref` and, if found, returns the embedded
// `gen/schemas/<ref>.json` body re-canonicalised through JCS. Refs that are
// not referenced by any active record return (nil, false) — the spec calls
// for RESOURCE_NOT_FOUND in that case.
func (s *Server) loadFirstPartySchema(ref string) ([]byte, bool) {
	if !activeSnapshotReferencesRef(s.snapshot, ref) {
		return nil, false
	}
	data, err := embedded.SchemaFS.ReadFile("schemas/" + ref + ".json")
	if err != nil {
		return nil, false
	}
	// Re-canonicalise on read so the wire bytes are JCS regardless of how
	// the build-time generator formats the on-disk file. The cost is one
	// json.Unmarshal + jcs.Marshal per request; acceptable for an MCP
	// resource read at v0.1.0 traffic levels.
	canonical, err := jcsCanonicaliseBytes(data)
	if err != nil {
		return nil, false
	}
	return canonical, true
}

// loadPluginSchema scans plugin-catalog.json variants for any row whose
// schema_hashes map contains `ref`. When found, the owning plugin's state
// must be "active" (per spec §13 line 3156 "inactive plugin refs ...
// return RESOURCE_NOT_FOUND") and the on-disk body file
// `<profileDir>/plugin-schemas/<ref>.<hash>.json` must be readable.
func (s *Server) loadPluginSchema(ref string) ([]byte, bool) {
	profileDir := s.profilePluginDir()
	if profileDir == "" {
		return nil, false
	}
	catalogTop := loadPluginFileEnvelope(filepath.Join(profileDir, "plugin-catalog.json"))
	if catalogTop == nil {
		return nil, false
	}
	hash, owner, ok := lookupPluginSchemaHash(catalogTop, ref)
	if !ok {
		return nil, false
	}
	stateRow := s.lookupStateRow(owner)
	if stateRow == nil {
		return nil, false
	}
	if quar, _ := stateRow["quarantined"].(bool); quar {
		return nil, false
	}
	if status := resolvePluginStatus(stateRow); status != "active" {
		return nil, false
	}
	bodyPath := filepath.Join(profileDir, "plugin-schemas", ref+"."+hash+".json")
	data, err := os.ReadFile(bodyPath)
	if err != nil {
		return nil, false
	}
	canonical, err := jcsCanonicaliseBytes(data)
	if err != nil {
		return nil, false
	}
	return canonical, true
}

// activeSnapshotReferencesRef returns true when any op.response_ref or any
// variant.binding.{request_ref,response_ref} matches `ref`. This is the
// "served by gum://op/{id} or gum://variant/{id}" check from spec §13 line
// 3156.
func activeSnapshotReferencesRef(c *catalog.Catalog, ref string) bool {
	if c == nil || ref == "" {
		return false
	}
	for i := range c.Ops {
		op := &c.Ops[i]
		if op.ResponseRef == ref {
			return true
		}
		for j := range op.Variants {
			b := op.Variants[j].Binding
			if b.RequestRef == ref || b.ResponseRef == ref {
				return true
			}
		}
	}
	return false
}

// jcsCanonicaliseBytes parses the on-disk JSON schema body and re-emits it
// in RFC 8785 canonical form. The build-time generator already writes
// canonical JSON, but third-party plugins are not held to that contract; this
// helper makes the wire bytes deterministic regardless of the source's
// formatting choices.
func jcsCanonicaliseBytes(raw []byte) ([]byte, error) {
	var tree any
	if err := json.Unmarshal(raw, &tree); err != nil {
		return nil, err
	}
	return jcs.Marshal(tree)
}

// lookupPluginSchemaHash scans plugin-catalog.json variants[] for the first
// row whose schema_hashes map contains `ref` and returns (hash, owner_plugin,
// true). When the same ref appears under multiple plugins, the lexicographic
// first row wins; the install-time SCHEMA_REF_COLLISION check guarantees
// that identical-ref rows share the same hash so the choice is harmless.
func lookupPluginSchemaHash(catalogTop map[string]any, ref string) (string, string, bool) {
	variants, _ := catalogTop["variants"].([]any)
	for _, raw := range variants {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		hashes, ok := row["schema_hashes"].(map[string]any)
		if !ok {
			continue
		}
		hash, ok := hashes[ref].(string)
		if !ok || hash == "" {
			continue
		}
		owner, _ := row["owner_plugin"].(string)
		if owner == "" {
			continue
		}
		return hash, owner, true
	}
	return "", "", false
}
