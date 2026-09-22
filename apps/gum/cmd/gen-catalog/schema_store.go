package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ehmo/gum/internal/catalog"
)

// schemaStoreSuffix is the on-disk extension of a stored request schema. The
// cleanup pass only removes files it recognises by this suffix, so the
// hand-written test fixture in the store survives a regeneration.
const schemaStoreSuffix = ".request.json"

// emitSchemaStore is the offline -emit-schemas mode. It derives one JSON
// Schema 2020-12 document per op that declares RequestFields, writes the body
// into schemaDir, sets binding.request_ref on every variant of that op, and
// rewrites catalog.json plus its .sha256 in lockstep.
//
// Response schemas are out of scope. The repository holds no offline source
// for them: catalog.json carries zero response_ref values and the only
// response-shaped fixture is an 11-op field-name tree, not a schema. Emitting
// a response document here would mean inventing one.
//
// The store is written indented rather than JCS-canonical. The §8.2 resource
// reader re-canonicalises on read, so the wire bytes are canonical either way,
// and an indented file is reviewable in a diff.
func emitSchemaStore(catalogPath, schemaDir string) error {
	data, err := os.ReadFile(catalogPath)
	if err != nil {
		return fmt.Errorf("emit-schemas: read %s: %w", catalogPath, err)
	}
	var cat catalog.Catalog
	if err := json.Unmarshal(data, &cat); err != nil {
		return fmt.Errorf("emit-schemas: parse %s: %w", catalogPath, err)
	}

	written, skipped, err := writeRequestSchemas(&cat, schemaDir)
	if err != nil {
		return err
	}

	removed, err := pruneRequestSchemas(schemaDir, written)
	if err != nil {
		return err
	}

	if err := validateGeneratedCatalog(&cat); err != nil {
		return fmt.Errorf("emit-schemas: validate catalog: %w", err)
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(&cat); err != nil {
		return fmt.Errorf("emit-schemas: encode catalog: %w", err)
	}
	if err := os.WriteFile(catalogPath, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("emit-schemas: write %s: %w", catalogPath, err)
	}
	sum := sha256.Sum256(buf.Bytes())
	checksumLine := fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), filepath.Base(catalogPath))
	if err := os.WriteFile(catalogPath+".sha256", []byte(checksumLine), 0o644); err != nil {
		return fmt.Errorf("emit-schemas: write %s.sha256: %w", catalogPath, err)
	}

	fmt.Fprintf(os.Stderr,
		"gen-catalog: wrote %d request schema(s) to %s, pruned %d stale, skipped %d op(s) with no request_fields (offline)\n",
		len(written), schemaDir, removed, len(skipped))
	for _, opID := range skipped {
		fmt.Fprintf(os.Stderr, "emit-schemas: no request_fields, no request_ref: %s\n", opID)
	}
	return nil
}

// writeRequestSchemas derives and writes one schema per op that declares
// RequestFields, and stamps the ref onto that op's variant bindings. It
// returns the set of filenames it owns plus the op_ids it skipped.
func writeRequestSchemas(cat *catalog.Catalog, schemaDir string) (map[string]bool, []string, error) {
	if err := os.MkdirAll(schemaDir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("emit-schemas: mkdir %s: %w", schemaDir, err)
	}

	written := map[string]bool{}
	var skipped []string

	for i := range cat.Ops {
		op := &cat.Ops[i]

		doc, err := catalog.RequestSchema(*op)
		if err != nil {
			return nil, nil, fmt.Errorf("emit-schemas: %w", err)
		}
		if doc == nil {
			skipped = append(skipped, op.OpID)
			continue
		}

		ref := catalog.RequestSchemaRef(op.OpID)
		if err := validateServedRef(ref); err != nil {
			return nil, nil, fmt.Errorf("emit-schemas: op %s: %w", op.OpID, err)
		}

		name := ref + ".json"
		if written[name] {
			return nil, nil, fmt.Errorf("emit-schemas: op %s: ref %s collides with an earlier op", op.OpID, ref)
		}

		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetIndent("", "  ")
		if err := enc.Encode(doc); err != nil {
			return nil, nil, fmt.Errorf("emit-schemas: op %s: encode schema: %w", op.OpID, err)
		}
		if err := os.WriteFile(filepath.Join(schemaDir, name), buf.Bytes(), 0o644); err != nil {
			return nil, nil, fmt.Errorf("emit-schemas: op %s: write schema: %w", op.OpID, err)
		}
		written[name] = true

		for j := range op.Variants {
			if op.Variants[j].Binding == nil {
				continue
			}
			op.Variants[j].Binding.RequestRef = ref
		}
	}

	return written, skipped, nil
}

// pruneRequestSchemas deletes store files this run no longer owns, so an op
// that leaves the catalog does not leave its schema behind. Only files ending
// in schemaStoreSuffix are candidates.
func pruneRequestSchemas(schemaDir string, written map[string]bool) (int, error) {
	entries, err := os.ReadDir(schemaDir)
	if err != nil {
		return 0, fmt.Errorf("emit-schemas: read %s: %w", schemaDir, err)
	}

	var stale []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), schemaStoreSuffix) {
			continue
		}
		if !written[e.Name()] {
			stale = append(stale, e.Name())
		}
	}
	sort.Strings(stale)

	for _, name := range stale {
		if err := os.Remove(filepath.Join(schemaDir, name)); err != nil {
			return 0, fmt.Errorf("emit-schemas: remove stale %s: %w", name, err)
		}
	}
	return len(stale), nil
}

// validateServedRef enforces the spec §8.2 served-ref grammar
// ^[a-z0-9][a-z0-9._-]{0,127}$ with no "..", so a generated ref can never
// escape the store directory or fail the resource reader.
func validateServedRef(ref string) error {
	if ref == "" {
		return fmt.Errorf("schema ref is empty")
	}
	if len(ref) > 128 {
		return fmt.Errorf("schema ref %q is %d chars; the grammar allows 128", ref, len(ref))
	}
	if strings.Contains(ref, "..") {
		return fmt.Errorf("schema ref %q contains %q", ref, "..")
	}
	for i, r := range ref {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			continue
		case (r == '.' || r == '_' || r == '-') && i > 0:
			continue
		default:
			return fmt.Errorf("schema ref %q has an invalid character %q at %d", ref, r, i)
		}
	}
	return nil
}
