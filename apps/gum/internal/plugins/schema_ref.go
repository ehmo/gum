package plugins

import (
	"errors"
	"fmt"
	"sort"

	"github.com/ehmo/gum/internal/plugins/registry"
)

// ErrSchemaRefCollision is the spec §8.2 / plugin-contract.md sentinel
// returned by ValidateNewPluginSchemas when a candidate plugin would write
// a schema_ref already present in the active profile with a different
// JCS-canonical body digest. Identical-body reuse is allowed — only
// digest divergence trips this error.
var ErrSchemaRefCollision = errors.New("SCHEMA_REF_COLLISION")

// SchemaRef is one (ref-name, canonical body hash, owning plugin) triple
// projected out of plugin-catalog.json. The hash is the JCS-canonical
// SHA-256 of the schema body, hex-encoded; the comparison is byte-equal.
type SchemaRef struct {
	Ref         string
	Hash        string
	OwnerPlugin string
}

// SchemaRefsFromCatalog walks pc.Variants and returns every schema_hashes
// entry as a SchemaRef. The expected variant shape is:
//
//	{ "owner_plugin": "<name>",
//	  "schema_hashes": { "<ref>": "<hex sha256>", ... } }
//
// Variants without a schema_hashes object are skipped. Missing
// owner_plugin defaults to "" (acceptable — the collision check only cares
// about ref+hash; OwnerPlugin is informational for the error message).
func SchemaRefsFromCatalog(variants []any) []SchemaRef {
	var out []SchemaRef
	for _, raw := range variants {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		owner, _ := row["owner_plugin"].(string)
		hashes, ok := row["schema_hashes"].(map[string]any)
		if !ok {
			continue
		}
		for ref, h := range hashes {
			hash, _ := h.(string)
			if ref == "" || hash == "" {
				continue
			}
			out = append(out, SchemaRef{Ref: ref, Hash: hash, OwnerPlugin: owner})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Ref != out[j].Ref {
			return out[i].Ref < out[j].Ref
		}
		return out[i].OwnerPlugin < out[j].OwnerPlugin
	})
	return out
}

// DetectSchemaRefCollision compares an incoming candidate set against the
// existing inventory. The function returns ErrSchemaRefCollision wrapping a
// human-readable message naming the first colliding ref + the two
// conflicting owner plugins. Identical-body reuse (same ref + same hash) is
// not a collision and produces no error.
//
// Inputs may be nil; both ordering and duplicate entries inside `existing`
// are tolerated. The comparison is O(|existing|+|candidate|) via a single
// pass with a ref→hash map.
func DetectSchemaRefCollision(existing, candidate []SchemaRef) error {
	if len(candidate) == 0 {
		return nil
	}
	known := make(map[string]SchemaRef, len(existing))
	for _, s := range existing {
		if prior, ok := known[s.Ref]; ok && prior.Hash != s.Hash {
			// Existing inventory already contains a self-collision; surface
			// it deterministically so the caller can repair the registry.
			return fmt.Errorf("%w: ref %q has divergent hashes in existing inventory (%s owned by %s, %s owned by %s)",
				ErrSchemaRefCollision, s.Ref, prior.Hash, prior.OwnerPlugin, s.Hash, s.OwnerPlugin)
		}
		known[s.Ref] = s
	}
	for _, c := range candidate {
		prior, ok := known[c.Ref]
		if !ok {
			continue
		}
		if prior.Hash == c.Hash {
			continue // identical body — reuse permitted per plugin-contract.md
		}
		return fmt.Errorf("%w: ref %q hash %s (candidate %s) conflicts with existing hash %s (owner %s)",
			ErrSchemaRefCollision, c.Ref, c.Hash, c.OwnerPlugin, prior.Hash, prior.OwnerPlugin)
	}
	return nil
}

// ValidateNewPluginSchemas loads the active profile's plugin-catalog.json
// and applies DetectSchemaRefCollision against the candidate refs. The
// caller is responsible for hashing the candidate schemas (JCS canonical
// SHA-256) before invoking this function.
//
// A clean run returns nil; a collision returns ErrSchemaRefCollision
// wrapped with diagnostic context. Storage errors (registry load) are
// returned verbatim so the install path can distinguish "could not check"
// from "definitely conflicts".
func ValidateNewPluginSchemas(reg *registry.Registry, candidate []SchemaRef) error {
	files, err := reg.Load()
	if err != nil {
		return fmt.Errorf("schema collision check: load registry: %w", err)
	}
	existing := SchemaRefsFromCatalog(files.Catalog.Variants)
	return DetectSchemaRefCollision(existing, candidate)
}
