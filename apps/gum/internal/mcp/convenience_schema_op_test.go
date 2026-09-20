package mcp

// gum-n1gi: the 18 Tier A convenience tools advertised MCP input schemas whose
// property names the dispatch kernel rejects as unknown. gmail_search declared
// `query` where gmail.users.messages.list declares `q`; docs_create declared a
// flat `title` plus a string `body` where the op wants the body object.
// Nothing mapped one to the other, so a caller that obeyed the advertised
// schema got INVALID_ARGS, and the schemas could not be enforced.

import (
	"encoding/json"
	"sort"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
	"github.com/ehmo/gum/internal/embedded"
)

// convenienceControlKeys are transport controls the handler consumes and strips
// before it builds the invocation, so the kernel never sees them.
var convenienceControlKeys = map[string]struct{}{
	"confirmed":          {},
	"confirmation_token": {},
}

func embeddedCatalogForTest(t *testing.T) *catalog.Catalog {
	t.Helper()
	if len(embedded.CatalogJSON) == 0 {
		t.Skip("binary embeds no catalog")
	}
	var c catalog.Catalog
	if err := json.Unmarshal(embedded.CatalogJSON, &c); err != nil {
		t.Fatalf("unmarshal embedded catalog: %v", err)
	}
	return &c
}

func opByIDInCatalog(c *catalog.Catalog, id string) *catalog.Op {
	for i := range c.Ops {
		if c.Ops[i].OpID == id {
			return &c.Ops[i]
		}
	}
	return nil
}

func schemaPropertyNames(t *testing.T, raw json.RawMessage) []string {
	t.Helper()
	var doc struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}
	names := make([]string, 0, len(doc.Properties))
	for name := range doc.Properties {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortedConvenienceNames() []string {
	names := make([]string, 0, len(convenienceABITable))
	for name := range convenienceABITable {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func opDeclaresArg(op *catalog.Op, name string) bool {
	for _, pair := range op.ParamsRequired {
		if len(pair) == 2 && pair[0] == name {
			return true
		}
	}
	for _, pair := range op.ParamsOptional {
		if len(pair) == 2 && pair[0] == name {
			return true
		}
	}
	for _, f := range op.RequestFields {
		if f.Location != catalog.RequestFieldBody && f.Name == name {
			return true
		}
	}
	return false
}

// TestConvenienceSchemaPropertiesReachTheKernel is the gate that lets
// makeConvenienceHandler sit behind validatedHandler: every property a
// convenience tool advertises must be a key its backing op accepts, either
// directly or through a declared body mapping.
func TestConvenienceSchemaPropertiesReachTheKernel(t *testing.T) {
	c := embeddedCatalogForTest(t)
	for _, toolName := range sortedConvenienceNames() {
		abi := ConvenienceToolABI(toolName)
		t.Run(toolName, func(t *testing.T) {
			op := opByIDInCatalog(c, abi.OpID)
			if op == nil {
				t.Skipf("op %s absent from this catalog build", abi.OpID)
			}
			allowed := dispatch.AllowedArgKeys(op)
			if allowed == nil {
				return // open schema: the op accepts every key
			}
			bodyMapped := map[string]struct{}{}
			if abi.BodyArg != "" {
				bodyMapped[abi.BodyArg] = struct{}{}
			}
			for _, f := range abi.BodyFields {
				bodyMapped[f] = struct{}{}
			}
			for _, prop := range schemaPropertyNames(t, convenienceToolSchema(toolName)) {
				if _, isControl := convenienceControlKeys[prop]; isControl {
					continue
				}
				if prop == "format" && abi.FormatControl {
					continue
				}
				if _, isBody := bodyMapped[prop]; isBody {
					continue
				}
				if _, ok := allowed[prop]; !ok {
					t.Errorf("schema advertises %q, which %s rejects as unknown", prop, abi.OpID)
				}
			}
		})
	}
}

// TestConvenienceBodyArgsMapToRealBodyFields: a tool whose op carries
// body-located fields must declare how the caller supplies them, a tool whose
// op carries none must not invent a body mapping, and every named BodyFields
// entry must be a body field the op really declares.
func TestConvenienceBodyArgsMapToRealBodyFields(t *testing.T) {
	c := embeddedCatalogForTest(t)
	for _, toolName := range sortedConvenienceNames() {
		abi := ConvenienceToolABI(toolName)
		t.Run(toolName, func(t *testing.T) {
			op := opByIDInCatalog(c, abi.OpID)
			if op == nil {
				t.Skipf("op %s absent from this catalog build", abi.OpID)
			}
			hasBodyFields := false
			for _, f := range op.RequestFields {
				if f.Location == catalog.RequestFieldBody {
					hasBodyFields = true
					break
				}
			}
			declaresMapping := abi.BodyArg != "" || len(abi.BodyFields) > 0
			switch {
			case hasBodyFields && !declaresMapping:
				t.Errorf("%s carries body fields but %s declares no body mapping", abi.OpID, toolName)
			case !hasBodyFields && declaresMapping:
				t.Errorf("%s carries no body fields but %s declares a body mapping", abi.OpID, toolName)
			}
			for _, name := range abi.BodyFields {
				found := false
				for _, f := range op.RequestFields {
					if f.Location == catalog.RequestFieldBody && f.Name == name {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("%s maps body field %q, which %s does not declare", toolName, name, abi.OpID)
				}
			}
			// A declared body mapping must also be advertised, or the caller has
			// no documented way to reach the body at all.
			props := map[string]struct{}{}
			for _, p := range schemaPropertyNames(t, convenienceToolSchema(toolName)) {
				props[p] = struct{}{}
			}
			if abi.BodyArg != "" {
				if _, ok := props[abi.BodyArg]; !ok {
					t.Errorf("%s maps body arg %q that its schema does not advertise", toolName, abi.BodyArg)
				}
			}
			for _, name := range abi.BodyFields {
				if _, ok := props[name]; !ok {
					t.Errorf("%s maps body field %q that its schema does not advertise", toolName, name)
				}
			}
		})
	}
}

// TestConvenienceRequiredPathParamsAreAdvertised: the kernel rejects a missing
// required path parameter before any upstream call, so a schema that omits one,
// or marks it optional, advertises a call that cannot succeed.
func TestConvenienceRequiredPathParamsAreAdvertised(t *testing.T) {
	c := embeddedCatalogForTest(t)
	for _, toolName := range sortedConvenienceNames() {
		abi := ConvenienceToolABI(toolName)
		t.Run(toolName, func(t *testing.T) {
			op := opByIDInCatalog(c, abi.OpID)
			if op == nil {
				t.Skipf("op %s absent from this catalog build", abi.OpID)
			}
			raw := convenienceToolSchema(toolName)
			props := map[string]struct{}{}
			for _, p := range schemaPropertyNames(t, raw) {
				props[p] = struct{}{}
			}
			required := map[string]struct{}{}
			for _, r := range schemaRequired(raw) {
				required[r] = struct{}{}
			}
			for _, f := range op.RequestFields {
				if f.Location != catalog.RequestFieldPath || !f.Required {
					continue
				}
				if _, ok := props[f.Name]; !ok {
					t.Errorf("%s requires path param %q; the schema does not advertise it", abi.OpID, f.Name)
					continue
				}
				if _, ok := required[f.Name]; !ok {
					t.Errorf("path param %q is structurally required; the schema lists it as optional", f.Name)
				}
			}
		})
	}
}

// TestConvenienceFormatControlMatchesFormats pins when a tool owns the `format`
// key. Spec §4.1 adds the §9 output-format control only when a row lists more
// than one format, and gmail_get_message cannot take it: its op declares its own
// `format` request field (full/minimal/raw/metadata), and one key cannot mean
// both the wire encoding and the Gmail payload shape.
func TestConvenienceFormatControlMatchesFormats(t *testing.T) {
	c := embeddedCatalogForTest(t)
	for _, toolName := range sortedConvenienceNames() {
		abi := ConvenienceToolABI(toolName)
		t.Run(toolName, func(t *testing.T) {
			op := opByIDInCatalog(c, abi.OpID)
			if op == nil {
				t.Skipf("op %s absent from this catalog build", abi.OpID)
			}
			want := len(abi.Formats) > 1 && !opDeclaresArg(op, "format")
			if abi.FormatControl != want {
				t.Errorf("FormatControl = %v; want %v (formats=%v, op declares format=%v)",
					abi.FormatControl, want, abi.Formats, opDeclaresArg(op, "format"))
			}
			props := map[string]struct{}{}
			for _, p := range schemaPropertyNames(t, convenienceToolSchema(toolName)) {
				props[p] = struct{}{}
			}
			_, advertises := props["format"]
			if abi.FormatControl && !advertises {
				t.Errorf("%s takes the format control but does not advertise `format`", toolName)
			}
		})
	}
}
