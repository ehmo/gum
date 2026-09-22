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
	"github.com/ehmo/gum/internal/output/fieldmask"
)

// default_fields.go implements the two offline gen-catalog passes behind the
// curated spec §9.1 stage-1 masks in default_fields_data.go:
//
//	-apply-default-fields        write the masks onto matching catalog variants
//	-emit-default-fields-schema  refresh testdata/default-fields-schema.json
//
// A mask is only useful if every field it names exists upstream: Google's
// partial-response filter silently drops an unknown selector, so one typo
// turns a response into a blank object with no error anywhere. Both passes
// therefore run every mask through parseCuratedMask (the shared
// internal/output/fieldmask grammar, minus the wildcard) and then through
// validateMaskAgainstSchema (field-by-field, against the real Discovery
// response schema), and refuse to write on the first failure.

// defaultFieldsSchemaFixture is the checked-in projection of the upstream
// response schemas the curated masks touch. Regenerate it, never hand-edit it.
type defaultFieldsSchemaFixture struct {
	GeneratedFrom map[string]string     `json:"generated_from"`
	Ops           map[string]schemaNode `json:"ops"`
}

// schemaNode is one level of an upstream response schema. Fields lists EVERY
// property the schema declares at that level, not only the masked ones, which
// is what makes validation catch a misspelled selector instead of rubber-
// stamping whatever the mask happens to say. Expansion is mask-guided: a
// property is expanded one level deeper only where a mask descends into it, so
// the fixture stays small and recursive schemas (MessagePart, GridData) cannot
// blow it up. An empty Fields therefore means either "scalar or opaque object"
// or "no mask descends here"; validateMaskAgainstSchema treats a descent into
// one as an error and says to regenerate.
type schemaNode struct {
	Ref    string                `json:"ref,omitempty"`
	Fields map[string]schemaNode `json:"fields,omitempty"`
}

const (
	// defaultFieldsSchemaFile is the fixture basename. The unit test resolves
	// it from its own testdata directory; the generator resolves it from the
	// module root, which is where every gen-catalog pass already runs.
	defaultFieldsSchemaFile = "default-fields-schema.json"
	defaultFieldsSchemaPath = "cmd/gen-catalog/testdata/" + defaultFieldsSchemaFile
)

// maskTree is a parsed mask re-expressed as a nested map, so expandSchema can
// descend exactly where a selector descends. internal/output/fieldmask owns the
// grammar; this is only a shape change over its Paths().
type maskTree map[string]maskTree

// parseCuratedMask parses one curated mask with the shared grammar and rejects
// the wildcard. `*` is legal Google syntax and legal to fieldmask.Parse, but a
// curated default that says "everything" saves nothing and would hide a stale
// entry behind a passing schema check.
func parseCuratedMask(mask string) (maskTree, error) {
	parsed, err := fieldmask.Parse(mask)
	if err != nil {
		return nil, err
	}
	root := maskTree{}
	for _, path := range parsed.Paths() {
		cur := root
		for _, seg := range path {
			if seg == "*" {
				return nil, fmt.Errorf("wildcard %q in %q: a curated default must name its fields",
					"*", strings.Join(path, "/"))
			}
			next, ok := cur[seg]
			if !ok {
				next = maskTree{}
				cur[seg] = next
			}
			cur = next
		}
	}
	return root, nil
}

// validateMaskAgainstSchema reports the first selector in mask that the
// upstream response schema does not define. Keys are walked in sorted order so
// a broken mask reports the same field on every run.
func validateMaskAgainstSchema(mask maskTree, schema schemaNode, path string) error {
	names := make([]string, 0, len(mask))
	for name := range mask {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		child, ok := schema.Fields[name]
		if !ok {
			return fmt.Errorf("%s%s: not a field of the upstream response schema %q",
				path, name, schemaRefOr(schema, "(root)"))
		}
		sub := mask[name]
		if len(sub) == 0 {
			continue
		}
		if len(child.Fields) == 0 {
			return fmt.Errorf("%s%s: the mask descends into it but the schema fixture has no sub-fields; "+
				"it is a scalar or opaque object, or the fixture predates this mask "+
				"(re-run: go run ./cmd/gen-catalog -emit-default-fields-schema)", path, name)
		}
		if err := validateMaskAgainstSchema(sub, child, path+name+"/"); err != nil {
			return err
		}
	}
	return nil
}

func schemaRefOr(n schemaNode, fallback string) string {
	if n.Ref != "" {
		return n.Ref
	}
	return fallback
}

// validateCuratedDefaultFields runs every curated mask through the grammar and
// then against fixture. It is the single gate both the apply pass and the unit
// test call, so they can never disagree about what a valid mask is.
func validateCuratedDefaultFields(masks map[string]string, fixture *defaultFieldsSchemaFixture) error {
	opIDs := make([]string, 0, len(masks))
	for opID := range masks {
		opIDs = append(opIDs, opID)
	}
	sort.Strings(opIDs)

	for _, opID := range opIDs {
		parsed, err := parseCuratedMask(masks[opID])
		if err != nil {
			return fmt.Errorf("%s: parse default_fields: %w", opID, err)
		}
		schema, ok := fixture.Ops[opID]
		if !ok {
			return fmt.Errorf("%s: no response schema in %s "+
				"(re-run: go run ./cmd/gen-catalog -emit-default-fields-schema)", opID, defaultFieldsSchemaPath)
		}
		if err := validateMaskAgainstSchema(parsed, schema, ""); err != nil {
			return fmt.Errorf("%s: default_fields: %w", opID, err)
		}
	}
	return nil
}

func loadDefaultFieldsSchema(path string) (*defaultFieldsSchemaFixture, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var fixture defaultFieldsSchemaFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &fixture, nil
}

// applyDefaultFields sets Variant.DefaultFields from the curated map on every
// variant of every matching op, then rewrites catalog.json and its .sha256 in
// lockstep. It is offline: the schema check reads the checked-in fixture, not
// the network.
func applyDefaultFields(catalogPath string) error {
	fixture, err := loadDefaultFieldsSchema(defaultFieldsSchemaPath)
	if err != nil {
		return fmt.Errorf("apply-default-fields: %w", err)
	}
	masks := tierADefaultFields()
	if err := validateCuratedDefaultFields(masks, fixture); err != nil {
		return fmt.Errorf("apply-default-fields: %w", err)
	}

	data, err := os.ReadFile(catalogPath)
	if err != nil {
		return fmt.Errorf("apply-default-fields: read %s: %w", catalogPath, err)
	}
	var cat catalog.Catalog
	if err := json.Unmarshal(data, &cat); err != nil {
		return fmt.Errorf("apply-default-fields: parse %s: %w", catalogPath, err)
	}

	applied := map[string]bool{}
	variants := 0
	for i := range cat.Ops {
		mask, ok := masks[cat.Ops[i].OpID]
		if !ok {
			continue
		}
		applied[cat.Ops[i].OpID] = true
		for j := range cat.Ops[i].Variants {
			cat.Ops[i].Variants[j].DefaultFields = mask
			variants++
		}
	}

	var missing []string
	for opID := range masks {
		if !applied[opID] {
			missing = append(missing, opID)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		return fmt.Errorf("apply-default-fields: curated op(s) not in %s: %v", catalogPath, missing)
	}

	if err := validateGeneratedCatalog(&cat); err != nil {
		return fmt.Errorf("apply-default-fields: validate catalog: %w", err)
	}
	if err := writeCatalogWithChecksum(catalogPath, &cat); err != nil {
		return fmt.Errorf("apply-default-fields: %w", err)
	}

	fmt.Fprintf(os.Stderr, "gen-catalog: applied default_fields to %d variant(s) across %d op(s)\n",
		variants, len(applied))
	return nil
}

// writeCatalogWithChecksum encodes cat and writes catalog.json and
// catalog.json.sha256 together. The checksum is over the exact bytes written,
// so the pair can never drift.
func writeCatalogWithChecksum(catalogPath string, cat *catalog.Catalog) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(cat); err != nil {
		return fmt.Errorf("encode catalog: %w", err)
	}
	if err := os.WriteFile(catalogPath, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", catalogPath, err)
	}
	sum := sha256.Sum256(buf.Bytes())
	line := fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), filepath.Base(catalogPath))
	if err := os.WriteFile(catalogPath+".sha256", []byte(line), 0o644); err != nil {
		return fmt.Errorf("write %s.sha256: %w", catalogPath, err)
	}
	return nil
}

// emitDefaultFieldsSchema fetches the live Discovery document for every service
// the curated map touches, resolves each op's response schema, and writes the
// mask-guided projection to testdata/default-fields-schema.json. Run it after
// adding or deepening a mask; the unit test reads only the checked-in file, so
// the test itself never touches the network.
func emitDefaultFieldsSchema(catalogPath string) error {
	data, err := os.ReadFile(catalogPath)
	if err != nil {
		return fmt.Errorf("emit-default-fields-schema: read %s: %w", catalogPath, err)
	}
	var cat catalog.Catalog
	if err := json.Unmarshal(data, &cat); err != nil {
		return fmt.Errorf("emit-default-fields-schema: parse %s: %w", catalogPath, err)
	}
	serviceOf := map[string]string{}
	for i := range cat.Ops {
		serviceOf[cat.Ops[i].OpID] = cat.Ops[i].Service
	}

	masks := tierADefaultFields()
	opIDs := make([]string, 0, len(masks))
	for opID := range masks {
		opIDs = append(opIDs, opID)
	}
	sort.Strings(opIDs)

	fixture := defaultFieldsSchemaFixture{
		GeneratedFrom: map[string]string{},
		Ops:           map[string]schemaNode{},
	}
	docs := map[string]map[string]any{}
	idx := map[string]map[string]map[string]any{}

	for _, opID := range opIDs {
		service, ok := serviceOf[opID]
		if !ok {
			return fmt.Errorf("emit-default-fields-schema: %s: not in %s", opID, catalogPath)
		}
		url, ok := discoveryURLFor(service)
		if !ok {
			return fmt.Errorf("emit-default-fields-schema: %s: no Discovery URL for service %q", opID, service)
		}
		if _, ok := docs[service]; !ok {
			doc, derr := fetchDiscoveryDoc(url)
			if derr != nil {
				return fmt.Errorf("emit-default-fields-schema: %s: %w", service, derr)
			}
			docs[service] = doc
			idx[service] = indexDiscoveryMethods(doc)
			fixture.GeneratedFrom[service] = url
		}
		method, ok := idx[service][discoveryMethodID(opID)]
		if !ok {
			return fmt.Errorf("emit-default-fields-schema: %s: no Discovery method %q",
				opID, discoveryMethodID(opID))
		}
		ref, _ := mapPath(method, "response", "$ref").(string)
		if ref == "" {
			return fmt.Errorf("emit-default-fields-schema: %s: Discovery method declares no response schema", opID)
		}
		mask, err := parseCuratedMask(masks[opID])
		if err != nil {
			return fmt.Errorf("emit-default-fields-schema: %s: %w", opID, err)
		}
		node, err := expandSchema(docs[service], ref, nil, mask)
		if err != nil {
			return fmt.Errorf("emit-default-fields-schema: %s: %w", opID, err)
		}
		fixture.Ops[opID] = node
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(&fixture); err != nil {
		return fmt.Errorf("emit-default-fields-schema: encode: %w", err)
	}
	if err := os.WriteFile(defaultFieldsSchemaPath, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("emit-default-fields-schema: write %s: %w", defaultFieldsSchemaPath, err)
	}

	if err := validateCuratedDefaultFields(masks, &fixture); err != nil {
		return fmt.Errorf("emit-default-fields-schema: the refreshed fixture rejects a curated mask: %w", err)
	}
	fmt.Fprintf(os.Stderr, "gen-catalog: wrote %s for %d op(s)\n", defaultFieldsSchemaPath, len(fixture.Ops))
	return nil
}

// expandSchema projects one Discovery schema into a schemaNode. It lists every
// property at the current level and recurses only where mask descends, which
// bounds the output and makes recursive schemas safe. ref is resolved against
// the document's "schemas" table; inline is the already-resolved schema when
// the property declared its object shape inline instead of by $ref.
func expandSchema(doc map[string]any, ref string, inline map[string]any, mask maskTree) (schemaNode, error) {
	schema := inline
	if ref != "" {
		resolved, ok := mapPath(doc, "schemas", ref).(map[string]any)
		if !ok {
			return schemaNode{}, fmt.Errorf("schema %q not found in the Discovery document", ref)
		}
		schema = resolved
	}
	props, _ := schema["properties"].(map[string]any)
	if len(props) == 0 {
		return schemaNode{Ref: ref}, nil
	}

	out := schemaNode{Ref: ref, Fields: map[string]schemaNode{}}
	for name, raw := range props {
		prop, _ := raw.(map[string]any)
		if prop == nil {
			out.Fields[name] = schemaNode{}
			continue
		}
		sub := mask[name]
		if len(sub) == 0 {
			out.Fields[name] = schemaNode{}
			continue
		}
		childRef, childInline := propSchema(prop)
		if childRef == "" && childInline == nil {
			out.Fields[name] = schemaNode{}
			continue
		}
		child, err := expandSchema(doc, childRef, childInline, sub)
		if err != nil {
			return schemaNode{}, fmt.Errorf("%s: %w", name, err)
		}
		out.Fields[name] = child
	}
	return out, nil
}

// propSchema unwraps one Discovery property to the object schema a mask can
// descend into: `$ref`, an array's item `$ref`, or an inline object. A mask
// selector crosses an array without naming an index, so items and objects are
// the same case here.
func propSchema(prop map[string]any) (ref string, inline map[string]any) {
	if r, ok := prop["$ref"].(string); ok && r != "" {
		return r, nil
	}
	if items, ok := prop["items"].(map[string]any); ok {
		if r, ok := items["$ref"].(string); ok && r != "" {
			return r, nil
		}
		if _, ok := items["properties"].(map[string]any); ok {
			return "", items
		}
		return "", nil
	}
	if _, ok := prop["properties"].(map[string]any); ok {
		return "", prop
	}
	return "", nil
}

// mapPath walks nested map[string]any by key and returns nil on any miss.
func mapPath(node map[string]any, keys ...string) any {
	var cur any = node
	for _, k := range keys {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur, ok = m[k]
		if !ok {
			return nil
		}
	}
	return cur
}
