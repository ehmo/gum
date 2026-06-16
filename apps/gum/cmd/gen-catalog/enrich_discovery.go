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
		url, ok := discoveryURLForService[op.Service]
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

	if err := cat.Validate(); err != nil {
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

// discoveryURLForService maps a catalog op's service to its Discovery document.
// searchconsole is intentionally absent: its ops are hand-authored (the URL
// Inspection v1 endpoint lives outside the webmasters/v3 Discovery doc).
var discoveryURLForService = map[string]string{
	"gmail":          "https://gmail.googleapis.com/$discovery/rest?version=v1",
	"calendar":       "https://www.googleapis.com/discovery/v1/apis/calendar/v3/rest",
	"drive":          "https://www.googleapis.com/discovery/v1/apis/drive/v3/rest",
	"docs":           "https://docs.googleapis.com/$discovery/rest?version=v1",
	"sheets":         "https://sheets.googleapis.com/$discovery/rest?version=v4",
	"slides":         "https://slides.googleapis.com/$discovery/rest?version=v1",
	"tasks":          "https://tasks.googleapis.com/$discovery/rest?version=v1",
	"admin":          "https://admin.googleapis.com/$discovery/rest?version=directory_v1",
	"people":         "https://people.googleapis.com/$discovery/rest?version=v1",
	"youtube":        "https://youtube.googleapis.com/$discovery/rest?version=v3",
	"forms":          "https://forms.googleapis.com/$discovery/rest?version=v1",
	"chat":           "https://chat.googleapis.com/$discovery/rest?version=v1",
	"classroom":      "https://classroom.googleapis.com/$discovery/rest?version=v1",
	"photoslibrary":  "https://photoslibrary.googleapis.com/$discovery/rest?version=v1",
	"cloudidentity":  "https://cloudidentity.googleapis.com/$discovery/rest?version=v1",
	"script":         "https://script.googleapis.com/$discovery/rest?version=v1",
	"vault":          "https://vault.googleapis.com/$discovery/rest?version=v1",
	"meet":           "https://meet.googleapis.com/$discovery/rest?version=v2",
	"groupssettings": "https://www.googleapis.com/discovery/v1/apis/groupssettings/v1/rest",
	"indexing":       "https://indexing.googleapis.com/$discovery/rest?version=v3",
	"adminreports":   "https://admin.googleapis.com/$discovery/rest?version=reports_v1",
	"customsearch":   "https://www.googleapis.com/discovery/v1/apis/customsearch/v1/rest",
}

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
