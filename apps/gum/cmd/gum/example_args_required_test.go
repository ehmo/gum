package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

// TestExampleArgsCoversEveryRequiredRequestField is the catalog-wide guard for
// gum-uvw3: `gum describe` emits example_args as the shape callers paste, so an
// example that omits a field the same output marks required:true hands out a
// call that cannot succeed. Runs against the embedded catalog so a curator who
// adds a required field without a matching example fails the build.
func TestExampleArgsCoversEveryRequiredRequestField(t *testing.T) {
	snap := loadCatalog()
	if snap == nil || len(snap.Ops) == 0 {
		t.Fatal("embedded catalog did not load")
	}
	for i := range snap.Ops {
		op := &snap.Ops[i]
		example := synthesizeExampleArgs(op)
		for j := range op.RequestFields {
			f := &op.RequestFields[j]
			if !f.Required {
				continue
			}
			if _, ok := example[f.Name]; !ok {
				t.Errorf("op %s: required request field %q missing from example_args (have %v)",
					op.OpID, f.Name, sortedKeys(example))
			}
		}
	}
}

// TestExampleArgsKeysAreAcceptedArgNames guards the second half of gum-uvw3:
// every example key must name an arg the op actually accepts. The URI-template
// reserved marker ("{+resourceName}") is the concrete regression — the executor
// binds the bare name, so an example keyed "+resourceName" is unusable.
func TestExampleArgsKeysAreAcceptedArgNames(t *testing.T) {
	snap := loadCatalog()
	if snap == nil || len(snap.Ops) == 0 {
		t.Fatal("embedded catalog did not load")
	}
	for i := range snap.Ops {
		op := &snap.Ops[i]
		for key := range synthesizeExampleArgs(op) {
			if strings.HasPrefix(key, "+") {
				t.Errorf("op %s: example_args key %q keeps the URI-template reserved marker; "+
					"the executor binds the bare name", op.OpID, key)
			}
			if key == "" {
				t.Errorf("op %s: example_args has an empty key", op.OpID)
			}
		}
	}
}

// TestExampleArgsTypesMatchRequestFields verifies the placeholder for a required
// field has the JSON type the field declares. A string sigil where the schema
// wants an array makes the pasted example fail on the container's type instead
// of on the value the caller still has to supply.
func TestExampleArgsTypesMatchRequestFields(t *testing.T) {
	snap := loadCatalog()
	if snap == nil || len(snap.Ops) == 0 {
		t.Fatal("embedded catalog did not load")
	}
	for i := range snap.Ops {
		op := &snap.Ops[i]
		example := synthesizeExampleArgs(op)
		for j := range op.RequestFields {
			f := &op.RequestFields[j]
			if !f.Required {
				continue
			}
			got, ok := example[f.Name]
			if !ok {
				continue // reported by the coverage test above
			}
			if want, actual := f.Type, jsonTypeOf(got); want != "" && want != actual {
				// params_required supplies untyped names first; only complain when
				// the mismatch is structural (array/object vs scalar).
				if isComposite(want) || isComposite(actual) {
					t.Errorf("op %s: required field %q declares type %q but example_args has %q (%#v)",
						op.OpID, f.Name, want, actual, got)
				}
			}
		}
	}
}

// TestExampleArgsEnumPlaceholderIsLegal checks that a required enum field's
// placeholder is a member of the enum, not a "<name>" sigil the API rejects.
func TestExampleArgsEnumPlaceholderIsLegal(t *testing.T) {
	snap := loadCatalog()
	if snap == nil || len(snap.Ops) == 0 {
		t.Fatal("embedded catalog did not load")
	}
	for i := range snap.Ops {
		op := &snap.Ops[i]
		example := synthesizeExampleArgs(op)
		for j := range op.RequestFields {
			f := &op.RequestFields[j]
			if !f.Required || len(f.Enum) == 0 || f.Type == "array" || f.Type == "object" {
				continue
			}
			got, ok := example[f.Name].(string)
			if !ok {
				continue
			}
			if !containsString(f.Enum, got) {
				t.Errorf("op %s: required enum field %q example %q is not one of %v",
					op.OpID, f.Name, got, f.Enum)
			}
		}
	}
}

// TestSynthesizeExampleArgsHistoricalMetrics pins the exact repro from gum-uvw3.
func TestSynthesizeExampleArgsHistoricalMetrics(t *testing.T) {
	snap := loadCatalog()
	if snap == nil {
		t.Fatal("embedded catalog did not load")
	}
	const opID = "googleads.keywordPlanIdeas.generateKeywordHistoricalMetrics"
	var op *catalog.Op
	for i := range snap.Ops {
		if snap.Ops[i].OpID == opID {
			op = &snap.Ops[i]
			break
		}
	}
	if op == nil {
		t.Fatalf("op %s not in catalog", opID)
	}
	got := synthesizeExampleArgs(op)
	if _, ok := got["customerId"]; !ok {
		t.Errorf("example_args missing customerId: %v", sortedKeys(got))
	}
	kw, ok := got["keywords"]
	if !ok {
		t.Fatalf("example_args missing keywords (the gum-uvw3 repro): %v", sortedKeys(got))
	}
	arr, ok := kw.([]any)
	if !ok || len(arr) == 0 {
		t.Fatalf("keywords placeholder is %#v, want a non-empty array", kw)
	}
	if _, ok := arr[0].(string); !ok {
		t.Errorf("keywords[0] is %#v, want a string (item_type string)", arr[0])
	}
}

// TestPathTemplateParamsStripsReservedMarker covers the helper directly, including
// the degenerate forms the catalog does not currently contain.
func TestPathTemplateParamsStripsReservedMarker(t *testing.T) {
	cases := []struct {
		name string
		path string
		want []string
	}{
		{"reserved marker", "https://people.googleapis.com/v1/{+resourceName}", []string{"resourceName"}},
		{"plain placeholder", "/gmail/v1/users/{userId}/messages", []string{"userId"}},
		{"mixed", "/v1/{+parent}/children/{childId}", []string{"parent", "childId"}},
		{"no placeholders", "/v1/spaces", nil},
		{"unterminated brace", "/v1/{oops", nil},
		{"marker only", "/v1/{+}", nil},
		{"empty placeholder", "/v1/{}", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := pathTemplateParams(tc.path)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}

// TestExampleValueForFieldTypes exercises the typed-placeholder table directly,
// including item_type shapes the embedded catalog does not exercise.
func TestExampleValueForFieldTypes(t *testing.T) {
	cases := []struct {
		name  string
		field catalog.RequestField
		want  string // JSON encoding of the expected placeholder
	}{
		{"string", catalog.RequestField{Name: "title", Type: "string"}, `"<title>"`},
		{"enum wins over sigil", catalog.RequestField{Name: "role", Type: "string", Enum: []string{"READER", "WRITER"}}, `"READER"`},
		{"integer", catalog.RequestField{Name: "depth", Type: "integer"}, `1`},
		{"integer page size heuristic", catalog.RequestField{Name: "pageSize", Type: "integer"}, `10`},
		{"boolean", catalog.RequestField{Name: "force", Type: "boolean"}, `false`},
		{"object", catalog.RequestField{Name: "start", Type: "object"}, `{}`},
		{"array of string", catalog.RequestField{Name: "ranges", Type: "array", ItemType: "string"}, `["<ranges>"]`},
		{"array of object", catalog.RequestField{Name: "requests", Type: "array", ItemType: "object"}, `[{}]`},
		{"array of array", catalog.RequestField{Name: "values", Type: "array", ItemType: "array"}, `[[]]`},
		{"array of integer", catalog.RequestField{Name: "ids", Type: "array", ItemType: "integer"}, `[1]`},
		{"array of boolean", catalog.RequestField{Name: "flags", Type: "array", ItemType: "boolean"}, `[false]`},
		{"array with enum items", catalog.RequestField{Name: "parts", Type: "array", Enum: []string{"snippet"}}, `["snippet"]`},
		{"unknown type falls back to sigil", catalog.RequestField{Name: "weird", Type: "quaternion"}, `"<weird>"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := encodeJSONNoEscape(exampleValueForField(&tc.field))
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}

// TestSynthesizeExampleArgsParamsRequiredWins verifies precedence: a name already
// supplied by params_required is not overwritten by the request_fields pass.
func TestSynthesizeExampleArgsParamsRequiredWins(t *testing.T) {
	op := &catalog.Op{
		OpID:           "test.op",
		ParamsRequired: [][]string{{"userId"}},
		RequestFields: []catalog.RequestField{
			{Name: "userId", Type: "array", ItemType: "string", Required: true},
		},
	}
	got := synthesizeExampleArgs(op)
	if got["userId"] != "me" {
		t.Fatalf("params_required placeholder was overwritten: got %#v, want \"me\"", got["userId"])
	}
}

// encodeJSONNoEscape marshals v without json's HTML escaping so the "<name>"
// placeholder sigils stay readable in test expectations.
func encodeJSONNoEscape(v any) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return strings.TrimRight(buf.String(), "\n"), nil
}

func jsonTypeOf(v any) string {
	switch v.(type) {
	case []any:
		return "array"
	case map[string]any:
		return "object"
	case string:
		return "string"
	case bool:
		return "boolean"
	case int, int64, float64:
		return "integer"
	}
	return ""
}

func isComposite(t string) bool { return t == "array" || t == "object" }

func containsString(hay []string, needle string) bool {
	for _, s := range hay {
		if s == needle {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
