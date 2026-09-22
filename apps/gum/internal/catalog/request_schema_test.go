package catalog

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// servedRefGrammar is the spec §8.2 safe served-ref grammar. Every ref
// RequestSchemaRef produces becomes a gum://schema/{ref} path segment, so a
// ref that misses the grammar is unreachable at run time.
var servedRefGrammar = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)

func TestRequestSchemaRefLowercasesTheOpID(t *testing.T) {
	cases := map[string]string{
		"":                                 "",
		"gmail.users.messages.list":        "gmail.users.messages.list.request",
		"calendar.calendarList.list":       "calendar.calendarlist.list.request",
		"gmail.users.messages.batchModify": "gmail.users.messages.batchmodify.request",
	}
	for opID, want := range cases {
		got := RequestSchemaRef(opID)
		if got != want {
			t.Errorf("RequestSchemaRef(%q) = %q, want %q", opID, got, want)
		}
		if got == "" {
			continue
		}
		if !servedRefGrammar.MatchString(got) {
			t.Errorf("RequestSchemaRef(%q) = %q, which the §8.2 served-ref grammar rejects", opID, got)
		}
		if strings.Contains(got, "..") {
			t.Errorf("RequestSchemaRef(%q) = %q, which contains a traversal segment", opID, got)
		}
	}
}

func TestRequestSchemaSkipsAnOpWithNoRequestFields(t *testing.T) {
	doc, err := RequestSchema(Op{OpID: "drive.about.get", Title: "Get Drive about"})
	if err != nil {
		t.Fatalf("RequestSchema: %v", err)
	}
	if doc != nil {
		t.Fatalf("RequestSchema returned %v for an op with no request fields; want nil", doc)
	}
}

func TestRequestSchemaCarriesEveryFieldFacet(t *testing.T) {
	op := Op{
		OpID:  "gmail.users.messages.list",
		Title: "  List Gmail messages  ",
		RequestFields: []RequestField{
			{Name: "userId", Location: "path", Type: "string", Required: true},
			{Name: "q", Type: "string", Description: "Gmail search query."},
			{Name: "labelIds", Type: "array", ItemType: "string"},
			{Name: "maxResults", Type: "integer", Default: "25"},
			{Name: "orderBy", Type: "string", Enum: []string{"asc", "desc"}},
			{Name: "after", Type: "string", Format: "date-time"},
		},
	}

	doc, err := RequestSchema(op)
	if err != nil {
		t.Fatalf("RequestSchema: %v", err)
	}

	if doc["$schema"] != JSONSchemaDraft2020 {
		t.Errorf("$schema = %v, want %s", doc["$schema"], JSONSchemaDraft2020)
	}
	if doc["$id"] != "gmail.users.messages.list.request" {
		t.Errorf("$id = %v, want gmail.users.messages.list.request", doc["$id"])
	}
	if doc["title"] != "List Gmail messages request" {
		t.Errorf("title = %v, want the trimmed op title plus \" request\"", doc["title"])
	}
	if doc["type"] != "object" {
		t.Errorf("type = %v, want object", doc["type"])
	}
	if _, ok := doc["additionalProperties"]; ok {
		t.Error("schema declares additionalProperties; gum injects control args a closed schema would reject")
	}

	required, _ := doc["required"].([]string)
	if !reflect.DeepEqual(required, []string{"userId"}) {
		t.Errorf("required = %v, want [userId]", required)
	}

	props, ok := doc["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties is %T, want map[string]any", doc["properties"])
	}
	if len(props) != len(op.RequestFields) {
		t.Fatalf("properties has %d entries, want %d", len(props), len(op.RequestFields))
	}

	want := map[string]map[string]any{
		"userId":     {"type": "string"},
		"q":          {"type": "string", "description": "Gmail search query."},
		"labelIds":   {"type": "array", "items": map[string]any{"type": "string"}},
		"maxResults": {"type": "integer", "default": int64(25)},
		"orderBy":    {"type": "string", "enum": []any{"asc", "desc"}},
		"after":      {"type": "string", "format": "date-time"},
	}
	for name, wantSub := range want {
		gotSub, ok := props[name].(map[string]any)
		if !ok {
			t.Errorf("properties[%s] is %T, want map[string]any", name, props[name])
			continue
		}
		if !reflect.DeepEqual(gotSub, wantSub) {
			t.Errorf("properties[%s] = %v, want %v", name, gotSub, wantSub)
		}
	}
}

func TestRequestSchemaOmitsRequiredWhenNoFieldIsRequired(t *testing.T) {
	doc, err := RequestSchema(Op{
		OpID:          "gmail.users.labels.list",
		Title:         "List labels",
		RequestFields: []RequestField{{Name: "pageToken", Type: "string"}},
	})
	if err != nil {
		t.Fatalf("RequestSchema: %v", err)
	}
	if _, ok := doc["required"]; ok {
		t.Errorf("schema declares required = %v with no required field", doc["required"])
	}
}

func TestRequestSchemaRejectsAnEmptyFieldName(t *testing.T) {
	_, err := RequestSchema(Op{
		OpID:          "gmail.users.messages.list",
		RequestFields: []RequestField{{Name: "", Type: "string"}},
	})
	if err == nil {
		t.Fatal("RequestSchema accepted a request field with an empty name")
	}
	if !strings.Contains(err.Error(), "gmail.users.messages.list") {
		t.Errorf("error %q does not name the op", err)
	}
}

func TestRequestSchemaPropagatesAnUndecodableDefault(t *testing.T) {
	_, err := RequestSchema(Op{
		OpID:          "gmail.users.messages.list",
		RequestFields: []RequestField{{Name: "maxResults", Type: "integer", Default: "many"}},
	})
	if err == nil {
		t.Fatal("RequestSchema accepted a default that does not decode to the declared type")
	}
	if !strings.Contains(err.Error(), "maxResults") {
		t.Errorf("error %q does not name the field", err)
	}
}
