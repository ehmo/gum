// Plugin schema-ref materialization (docs/plugin-contract.md "Schema Refs").
//
// A tool's `schema_ref` names a JSON Schema 2020-12 bundle at
// `schemas/<schema_ref>.json` inside the plugin source tree. The bundle
// carries object-valued `$defs.request` and `$defs.response`. Install splits
// those two subdocuments out, canonicalizes each with JCS, and copies them
// into the selected profile's schema store as
// `plugin-schemas/<ref>.<sha256>.json`. The derived served refs are
// `<schema_ref>.request` and `<schema_ref>.response`; a manifest never
// declares them.
//
// The write side and the MCP read side (internal/mcp/schema_resource.go)
// share one grammar and one digest rule, so a ref this file writes is a ref
// that file can serve.

package plugins

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ehmo/gum/internal/output/jcs"
)

// ErrSchemaRefInvalid is the stable PLUGIN_SCHEMA_REF_INVALID error. It
// covers a ref that breaks the §8.2 served-ref grammar, a bundle that cannot
// be read or parsed, and a bundle missing either object-valued def.
var ErrSchemaRefInvalid = errors.New("PLUGIN_SCHEMA_REF_INVALID")

// pluginSchemaDirName is the per-profile schema store directory.
const pluginSchemaDirName = "plugin-schemas"

// schemaBundleDirName is the directory inside the plugin source tree that
// `schema_ref` resolves against.
const schemaBundleDirName = "schemas"

// The two derived ref suffixes. Build/install appends them to the manifest's
// schema_ref; plugin manifests never declare the derived refs themselves.
const (
	requestRefSuffix  = ".request"
	responseRefSuffix = ".response"
)

// safeServedRefPattern is the §8.2 grammar: lowercase ASCII alnum
// first char, then up to 127 lowercase alnum / dot / underscore / hyphen. The
// 128-character bound keeps every ref well inside POSIX NAME_MAX (255), so
// the on-disk `<ref>.<sha256>.json` name never exceeds 198 bytes.
var safeServedRefPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)

// IsSafeServedRef reports whether ref may become a path segment under a
// profile's plugin-schemas directory. A ref passes only when it matches the
// grammar and contains no `..`. Path separators and percent signs are
// already outside the character class, so a URI-encoded separator fails here
// after the reader decodes it.
func IsSafeServedRef(ref string) bool {
	if !safeServedRefPattern.MatchString(ref) {
		return false
	}
	return !strings.Contains(ref, "..")
}

// MaterializedSchema is one served ref, the JCS-canonical body stored for it,
// and that body's SHA-256. The hash is both the collision key and the
// filename segment, so the stored file and the recorded schema_hashes entry
// can never drift apart.
type MaterializedSchema struct {
	Ref  string
	Hash string
	Body []byte
}

// materializeToolSchema splits one bundle into its request and response
// halves. An empty schemaRef returns no schemas and no error: a tool that
// serves no schema has nothing to materialize and nothing to collide with.
func materializeToolSchema(sourceDir, schemaRef string) ([]MaterializedSchema, error) {
	if schemaRef == "" {
		return nil, nil
	}
	if !IsSafeServedRef(schemaRef) {
		return nil, fmt.Errorf("%w: schema_ref %q violates the safe served-ref grammar", ErrSchemaRefInvalid, schemaRef)
	}

	requestRef := schemaRef + requestRefSuffix
	responseRef := schemaRef + responseRefSuffix
	for _, derived := range []string{requestRef, responseRef} {
		if !IsSafeServedRef(derived) {
			return nil, fmt.Errorf("%w: derived ref %q violates the safe served-ref grammar", ErrSchemaRefInvalid, derived)
		}
	}

	// The grammar check above is what makes this join safe; schemaRef can
	// carry no separator and no traversal marker by the time we get here.
	bundlePath := filepath.Join(sourceDir, schemaBundleDirName, schemaRef+".json")
	raw, err := os.ReadFile(bundlePath)
	if err != nil {
		return nil, fmt.Errorf("%w: read schema bundle %s: %v", ErrSchemaRefInvalid, schemaRef+".json", err)
	}

	var bundle map[string]any
	if err := json.Unmarshal(raw, &bundle); err != nil {
		return nil, fmt.Errorf("%w: schema bundle %s is not a JSON object: %v", ErrSchemaRefInvalid, schemaRef+".json", err)
	}
	defs, ok := bundle["$defs"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: schema bundle %s has no object-valued $defs", ErrSchemaRefInvalid, schemaRef+".json")
	}

	out := make([]MaterializedSchema, 0, 2)
	for _, part := range []struct{ name, ref string }{
		{"request", requestRef},
		{"response", responseRef},
	} {
		def, ok := defs[part.name].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%w: schema bundle %s has no object-valued $defs.%s", ErrSchemaRefInvalid, schemaRef+".json", part.name)
		}
		body, err := jcs.Marshal(def)
		if err != nil {
			return nil, fmt.Errorf("%w: canonicalize $defs.%s of %s: %v", ErrSchemaRefInvalid, part.name, schemaRef+".json", err)
		}
		sum := sha256.Sum256(body)
		out = append(out, MaterializedSchema{
			Ref:  part.ref,
			Hash: hex.EncodeToString(sum[:]),
			Body: body,
		})
	}
	return out, nil
}

// materializeManifestSchemas walks every advertised tool in manifest order
// and returns the served schemas, deduplicated by (ref, hash). Two tools that
// name one bundle produce one pair of files, which is the identical-body
// reuse the contract permits.
func materializeManifestSchemas(sourceDir string, m *Manifest) ([]MaterializedSchema, error) {
	var out []MaterializedSchema
	seen := make(map[string]string)
	for _, tool := range m.AdvertisedTools {
		schemas, err := materializeToolSchema(sourceDir, tool.SchemaRef)
		if err != nil {
			return nil, err
		}
		for _, s := range schemas {
			prior, ok := seen[s.Ref]
			if ok {
				if prior != s.Hash {
					return nil, fmt.Errorf("%w: ref %q resolves to two bodies within plugin %q", ErrSchemaRefCollision, s.Ref, m.PluginID)
				}
				continue
			}
			seen[s.Ref] = s.Hash
			out = append(out, s)
		}
	}
	return out, nil
}

// schemaRefsForOwner converts materialized schemas into the collision-check
// triples DetectSchemaRefCollision compares.
func schemaRefsForOwner(owner string, schemas []MaterializedSchema) []SchemaRef {
	refs := make([]SchemaRef, 0, len(schemas))
	for _, s := range schemas {
		refs = append(refs, SchemaRef{Ref: s.Ref, Hash: s.Hash, OwnerPlugin: owner})
	}
	return refs
}

// schemaHashesFor returns the plugin-catalog.json `schema_hashes` object for
// one tool's bundle: ref to hash for the request and response halves.
func schemaHashesFor(schemaRef string, schemas []MaterializedSchema) map[string]any {
	if schemaRef == "" {
		return nil
	}
	hashes := make(map[string]any, 2)
	for _, s := range schemas {
		if s.Ref == schemaRef+requestRefSuffix || s.Ref == schemaRef+responseRefSuffix {
			hashes[s.Ref] = s.Hash
		}
	}
	if len(hashes) == 0 {
		return nil
	}
	return hashes
}

// writePluginSchemas copies each canonical body into
// <profileDir>/plugin-schemas/<ref>.<hash>.json.
//
// The write precedes the registry transaction for the same reason the
// executable copy does: the hash recorded in plugin-catalog.json must name a
// file that already exists, or the MCP schema resource serves
// RESOURCE_NOT_FOUND for a ref the catalog advertises. A body file is
// content-addressed, so re-writing one is idempotent and an orphan left by an
// abandoned install is harmless.
func writePluginSchemas(profileDir string, schemas []MaterializedSchema) error {
	if len(schemas) == 0 {
		return nil
	}
	dir := filepath.Join(profileDir, pluginSchemaDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("plugin install: create schema store: %w", err)
	}
	for _, s := range schemas {
		path := filepath.Join(dir, s.Ref+"."+s.Hash+".json")
		if err := os.WriteFile(path, s.Body, 0o600); err != nil {
			return fmt.Errorf("plugin install: write schema %s: %w", s.Ref, err)
		}
	}
	return nil
}
