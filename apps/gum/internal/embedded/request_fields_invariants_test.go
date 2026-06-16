package embedded_test

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/embedded"
)

// pathPlaceholderRe matches {name} segments in an HTTP path template.
var pathPlaceholderRe = regexp.MustCompile(`\{([^}]+)\}`)

// opsWithoutRequestFields are the catalog ops that legitimately carry no
// RequestFields: two have no request parameters at all, and gum.code is a meta
// op invoked via `gum code <file>`, not `gum call`. Any OTHER op missing
// RequestFields is a regression (the convenient-CLI layer would be dark for it).
var opsWithoutRequestFields = map[string]bool{
	"searchconsole.sites.list":          true, // GET /sites — no parameters
	"calendar.colors.get":               true, // GET /colors — no parameters
	"drive.about.get":                   true, // GET /about — no path/query params (only the system `fields`)
	"people.connections.list":           true, // GET /people/me/connections — open-schema read (personFields etc. pass through); resourceName baked into the path
	"people.contactGroups.create":       true, // POST /contactGroups — body-only contact group
	"classroom.courses.create":          true, // POST /courses — body-only course resource
	"photoslibrary.albums.create":       true, // POST /albums — body-only album
	"photoslibrary.mediaItems.search":   true, // POST /mediaItems:search — body-only filter, no path/query params
	"script.projects.create":            true, // POST /projects — body-only (title)
	"vault.matters.create":              true, // POST /matters — body-only matter
	"meet.spaces.create":                true, // POST /spaces — body-only (config), no path/query params
	"indexing.urlNotifications.publish": true, // POST /urlNotifications:publish — body-only (url, type)
	"gum.code":                          true, // meta op; uses `gum code`, not `gum call`
}

var validRequestFieldTypes = map[string]bool{
	"string": true, "integer": true, "number": true, "boolean": true,
	"array": true, "object": true, "": true,
}

func loadEmbeddedCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	var cat catalog.Catalog
	if err := json.Unmarshal(embedded.CatalogJSON, &cat); err != nil {
		t.Fatalf("unmarshal embedded catalog: %v", err)
	}
	if len(cat.Ops) == 0 {
		t.Fatal("embedded catalog has no ops")
	}
	return &cat
}

// TestRequestFieldsInvariantsForEveryOp validates the RequestFields of EVERY
// catalog op: valid names/locations/types, no duplicates, path fields required,
// enums non-empty, and — for HTTP ops — that path-location fields exactly match
// the URL template's {placeholders} (so a path value always substitutes and
// never leaks to the query string).
func TestRequestFieldsInvariantsForEveryOp(t *testing.T) {
	cat := loadEmbeddedCatalog(t)
	for i := range cat.Ops {
		op := &cat.Ops[i]
		t.Run(op.OpID, func(t *testing.T) {
			seen := map[string]bool{}
			var pathFields []string
			for _, f := range op.RequestFields {
				if f.Name == "" {
					t.Errorf("%s: a RequestField has an empty name", op.OpID)
				}
				if seen[f.Name] {
					t.Errorf("%s: duplicate RequestField name %q", op.OpID, f.Name)
				}
				seen[f.Name] = true

				switch f.Location {
				case catalog.RequestFieldPath, catalog.RequestFieldQuery, catalog.RequestFieldBody, catalog.RequestFieldArg, catalog.RequestFieldHeader:
				default:
					t.Errorf("%s: field %q has invalid location %q", op.OpID, f.Name, f.Location)
				}
				if !validRequestFieldTypes[f.Type] {
					t.Errorf("%s: field %q has invalid type %q", op.OpID, f.Name, f.Type)
				}
				for _, e := range f.Enum {
					if e == "" {
						t.Errorf("%s: field %q has an empty enum value", op.OpID, f.Name)
					}
				}
				if f.Location == catalog.RequestFieldPath {
					if !f.Required {
						t.Errorf("%s: path field %q must be Required", op.OpID, f.Name)
					}
					pathFields = append(pathFields, f.Name)
				}
			}

			// HTTP ops: path-location fields must exactly match the URL template
			// placeholders (both directions).
			dv := defaultVariant(op)
			if dv != nil && dv.Binding != nil && dv.Binding.HTTP != nil {
				var placeholders []string
				for _, m := range pathPlaceholderRe.FindAllStringSubmatch(dv.Binding.HTTP.Path, -1) {
					// Strip the RFC 6570 reserved-expansion marker ({+name}) so the
					// placeholder matches the bare path-field name "name".
					placeholders = append(placeholders, strings.TrimPrefix(m[1], "+"))
				}
				// Only enforce the match when the op declares RequestFields;
				// a param-less op (no RequestFields) with a bare path is fine.
				if len(op.RequestFields) > 0 || len(placeholders) > 0 {
					sort.Strings(placeholders)
					sort.Strings(pathFields)
					if !equalStrings(placeholders, pathFields) {
						t.Errorf("%s: path fields %v do not match URL placeholders %v (path=%s)",
							op.OpID, pathFields, placeholders, dv.Binding.HTTP.Path)
					}
				}
			}
		})
	}
}

// TestEveryParamBearingOpHasRequestFields pins 100% coverage: every op must
// carry RequestFields except the documented param-less/meta allowlist. A new op
// that ships without them (or a removed field set) fails here.
func TestEveryParamBearingOpHasRequestFields(t *testing.T) {
	cat := loadEmbeddedCatalog(t)
	for i := range cat.Ops {
		op := &cat.Ops[i]
		has := len(op.RequestFields) > 0
		allowEmpty := opsWithoutRequestFields[op.OpID]
		if !has && !allowEmpty {
			t.Errorf("%s has no RequestFields and is not in the documented param-less allowlist — populate it (gen-catalog --enrich-request-fields) or add it to opsWithoutRequestFields with justification", op.OpID)
		}
		if has && allowEmpty {
			t.Errorf("%s is in the param-less allowlist but now HAS RequestFields — remove it from the allowlist", op.OpID)
		}
	}
}

func defaultVariant(op *catalog.Op) *catalog.Variant {
	for i := range op.Variants {
		if op.Variants[i].VariantID == op.DefaultVariantID {
			return &op.Variants[i]
		}
	}
	if len(op.Variants) > 0 {
		return &op.Variants[0]
	}
	return nil
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
