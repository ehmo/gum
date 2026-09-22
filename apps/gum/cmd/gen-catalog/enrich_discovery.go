package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ehmo/gum/internal/catalog"
)

// enrichRequestFields populates RequestFields across the whole REST catalog:
// it applies the hand-authored map first (plugins, meta, verified Tier A +
// Search Console), then derives fields from Discovery for every REST op still
// missing them, and rewrites catalog.json + .sha256 in lockstep. The hand-map
// wins for the ops it covers (re-applied each run); the Discovery pass only
// fills ops with no fields, so data is never clobbered by Discovery.
func enrichRequestFields(catalogPath string) error {
	data, err := os.ReadFile(catalogPath)
	if err != nil {
		return fmt.Errorf("enrich-request-fields: read %s: %w", catalogPath, err)
	}
	var cat catalog.Catalog
	if err := json.Unmarshal(data, &cat); err != nil {
		return fmt.Errorf("enrich-request-fields: parse %s: %w", catalogPath, err)
	}

	// 1. Hand-authored map wins for the ops it covers.
	handMap := tierARequestFields()
	for i := range cat.Ops {
		if rf, ok := handMap[cat.Ops[i].OpID]; ok {
			cat.Ops[i].RequestFields = rf
		}
	}

	// 2. Discovery-derive the rest, fetching each service's doc once.
	docCache := map[string]map[string]any{}
	idxCache := map[string]map[string]map[string]any{}
	enriched := 0
	var skipped []string
	for i := range cat.Ops {
		op := &cat.Ops[i]
		if len(op.RequestFields) > 0 {
			continue
		}
		url, ok := discoveryURLFor(op.Service)
		if !ok {
			skipped = append(skipped, op.OpID)
			continue
		}
		if _, ok := docCache[op.Service]; !ok {
			doc, derr := fetchDiscoveryDoc(url)
			if derr != nil {
				return fmt.Errorf("enrich-request-fields: %s: %w", op.Service, derr)
			}
			docCache[op.Service] = doc
			idxCache[op.Service] = indexDiscoveryMethods(doc)
		}
		method, ok := idxCache[op.Service][discoveryMethodID(op.OpID)]
		if !ok {
			skipped = append(skipped, op.OpID)
			continue
		}
		op.RequestFields = deriveRequestFields(docCache[op.Service], method)
		if len(op.RequestFields) > 0 {
			enriched++ // count only ops that actually gained fields
		}
	}

	// 3. Classify lro_return from the same Discovery methods. The pass runs over
	// every op, including the ones step 2 skipped because the hand-map already
	// gave them RequestFields: the classification is independent of where the
	// fields came from.
	classified := 0
	for i := range cat.Ops {
		op := &cat.Ops[i]
		url, ok := discoveryURLFor(op.Service)
		if !ok {
			continue
		}
		if _, ok := docCache[op.Service]; !ok {
			doc, derr := fetchDiscoveryDoc(url)
			if derr != nil {
				return fmt.Errorf("enrich-request-fields: %s: %w", op.Service, derr)
			}
			docCache[op.Service] = doc
			idxCache[op.Service] = indexDiscoveryMethods(doc)
		}
		method, ok := idxCache[op.Service][discoveryMethodID(op.OpID)]
		if !ok {
			continue
		}
		if !methodReturnsLRO(docCache[op.Service], method) {
			continue
		}
		classified += stampLROReturn(op)
	}

	if err := validateGeneratedCatalog(&cat); err != nil {
		return fmt.Errorf("enrich-request-fields: validate catalog: %w", err)
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(&cat); err != nil {
		return fmt.Errorf("enrich-request-fields: encode catalog: %w", err)
	}
	if err := os.WriteFile(catalogPath, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("enrich-request-fields: write %s: %w", catalogPath, err)
	}
	sum := sha256.Sum256(buf.Bytes())
	checksumLine := fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), filepath.Base(catalogPath))
	if err := os.WriteFile(catalogPath+".sha256", []byte(checksumLine), 0o644); err != nil {
		return fmt.Errorf("enrich-request-fields: write %s.sha256: %w", catalogPath, err)
	}

	fmt.Fprintf(os.Stderr, "gen-catalog: enriched %d REST op(s) from Discovery; %d op(s) left to the hand-map/no-params: %v\n", enriched, len(skipped), skipped)
	fmt.Fprintf(os.Stderr, "gen-catalog: classified %d REST variant(s) lro_return\n", classified)
	return nil
}

// enrich_discovery.go derives RequestField descriptors for REST catalog ops
// directly from the authoritative Google API Discovery documents, so the
// convenient-CLI input layer (typed flags, --skeleton, enum/type validation,
// wizard) covers the whole catalog without hand-maintaining every op.
//
// It is an OFFLINE enrichment pass (gen-catalog --enrich-request-fields): it
// reads the existing catalog.json, applies the hand-authored map first (which
// owns plugin/meta ops and the already-verified Tier A + Search Console set),
// then for every REST op STILL lacking RequestFields it fetches the matching
// Discovery method and derives them. Existing RequestFields are never
// overwritten — the curated/verified data wins.

// discoveryMethodID maps a gum op_id to its Discovery method id. They match for
// most services; some APIs name their methods differently from gum's
// service.resource.method op_ids:
//   - admin's Directory API uses the "directory." prefix.
//   - People's discovery ids already match gum's people.people.* /
//     people.contactGroups.* op_ids directly. (people.connections.list is
//     intentionally NOT mapped: its binding hardcodes /people/me/connections,
//     so it stays an open-schema read rather than enriching a resourceName path
//     param that the binding doesn't carry.)
func discoveryMethodID(opID string) string {
	if rest, ok := strings.CutPrefix(opID, "admin.directory."); ok {
		return "directory." + rest
	}
	// Groups Settings names its Discovery methods camelCased (groupsSettings.*);
	// gum's service is the lowercase "groupssettings".
	if rest, ok := strings.CutPrefix(opID, "groupssettings."); ok {
		return "groupsSettings." + rest
	}
	// Admin Reports lives under service "adminreports" but its Discovery method
	// ids use the "reports." prefix.
	if rest, ok := strings.CutPrefix(opID, "adminreports."); ok {
		return "reports." + rest
	}
	// Custom Search's Discovery method ids use the "search." prefix
	// (search.cse.list) rather than the apiName "customsearch".
	if rest, ok := strings.CutPrefix(opID, "customsearch."); ok {
		return "search." + rest
	}
	return opID
}

// indexDiscoveryMethods walks a Discovery doc's resource tree and returns every
// method keyed by its id.
func indexDiscoveryMethods(disc map[string]any) map[string]map[string]any {
	out := map[string]map[string]any{}
	var walk func(node map[string]any)
	walk = func(node map[string]any) {
		if methods, ok := node["methods"].(map[string]any); ok {
			for _, m := range methods {
				if mm, ok := m.(map[string]any); ok {
					if id, ok := mm["id"].(string); ok {
						out[id] = mm
					}
				}
			}
		}
		if res, ok := node["resources"].(map[string]any); ok {
			for _, r := range res {
				if rr, ok := r.(map[string]any); ok {
					walk(rr)
				}
			}
		}
	}
	walk(disc)
	return out
}

// deriveRequestFields builds RequestFields for one Discovery method from its
// path and query PARAMETERS only. Request-body fields are intentionally NOT
// derived here: the older Google Discovery docs (gmail, calendar, …) do not
// reliably flag output-only properties (readOnly is unset and descriptions are
// inconsistent), so deriving body flags would surface confusing/harmful
// output fields (id, created, htmlLink, messagesTotal, …). Body input therefore
// comes from the curated hand-map (request_fields_data.go) for the common write
// ops, with the §12.0 body:=json grammar as the always-available fallback for
// the rest. Parameters, by contrast, are unambiguously input and safe to derive.
func deriveRequestFields(disc map[string]any, method map[string]any) []catalog.RequestField {
	_ = disc
	var pathF, queryF []catalog.RequestField

	if params, ok := method["parameters"].(map[string]any); ok {
		for name, raw := range params {
			p, _ := raw.(map[string]any)
			if p == nil {
				continue
			}
			loc, _ := p["location"].(string)
			rf := catalog.RequestField{
				Name:        name,
				Type:        discoveryType(p),
				ItemType:    discoveryItemType(p),
				Enum:        toStringSlice(p["enum"]),
				Description: clip(asString(p["description"])),
			}
			if loc == "path" {
				rf.Location = catalog.RequestFieldPath
				rf.Required = true
				pathF = append(pathF, rf)
			} else {
				rf.Location = catalog.RequestFieldQuery
				rf.Required, _ = p["required"].(bool)
				queryF = append(queryF, rf)
			}
		}
	}

	sortFields(pathF)
	sortFields(queryF)
	return append(pathF, queryF...)
}

// discoveryType maps a Discovery property/parameter to a gum field type. A $ref
// (nested message) is an object; everything else uses the declared JSON type.
func discoveryType(p map[string]any) string {
	if _, ok := p["$ref"].(string); ok {
		return "object"
	}
	if t, ok := p["type"].(string); ok {
		return t
	}
	return "string"
}

// discoveryItemType returns the element type for an array property.
func discoveryItemType(p map[string]any) string {
	if t, _ := p["type"].(string); t != "array" {
		return ""
	}
	items, ok := p["items"].(map[string]any)
	if !ok {
		return ""
	}
	return discoveryType(items)
}

func toStringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func asString(v any) string { s, _ := v.(string); return s }

// clip trims a description to a single short line for the catalog.
func clip(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) > 120 {
		s = s[:119] + "…"
	}
	return s
}

func sortFields(f []catalog.RequestField) {
	sort.Slice(f, func(i, j int) bool { return f[i].Name < f[j].Name })
}

// fetchDiscoveryDoc fetches and parses a Discovery document.
func fetchDiscoveryDoc(url string) (map[string]any, error) {
	rc, err := httpGet(url)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse discovery %s: %w", url, err)
	}
	return doc, nil
}

// methodReturnsLRO reports whether a Discovery method answers with a
// google.longrunning.Operation rather than the finished resource.
//
// The test is the shape of the referenced schema, not its name. Discovery
// spells the type "Operation" in every document gum reads today, but other
// Google APIs spell it "GoogleLongrunningOperation", and a name match on
// "Operation" would also catch ListOperationsResponse, which is an ordinary
// list payload. Every longrunning.Operation carries both `done` and `error`;
// no list response does.
func methodReturnsLRO(disc map[string]any, method map[string]any) bool {
	resp, ok := method["response"].(map[string]any)
	if !ok {
		return false
	}
	ref, _ := resp["$ref"].(string)
	if ref == "" {
		return false
	}

	schemas, ok := disc["schemas"].(map[string]any)
	if !ok {
		return false
	}
	schema, ok := schemas[ref].(map[string]any)
	if !ok {
		return false
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		return false
	}

	_, hasDone := props["done"]
	_, hasError := props["error"]
	return hasDone && hasError
}

// stampLROReturn appends the lro_return capability atom to every REST variant
// of op that does not already declare it, and returns how many it changed.
//
// Only REST-backed variants are stamped: the evidence is a Discovery document,
// which describes the REST surface. A gRPC SDK or plugin variant of the same op
// may well answer differently, and its own generator owns that call.
func stampLROReturn(op *catalog.Op) int {
	changed := 0
	for i := range op.Variants {
		v := &op.Variants[i]
		switch v.BackendKind {
		case catalog.BackendKindTypedRestSDK, catalog.BackendKindDiscoveryREST, catalog.BackendKindRawHTTP:
		default:
			continue
		}
		if v.ReturnsLRO() {
			continue
		}
		v.Capabilities = append(v.Capabilities, catalog.CapabilityLROReturn)
		changed++
	}
	return changed
}
