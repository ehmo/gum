package catalog

import (
	"fmt"
	"strings"
)

// JSONSchemaDraft2020 is the `$schema` value every derived first-party request
// schema declares. Spec §8.2 serves the store as JSON Schema 2020-12.
const JSONSchemaDraft2020 = "https://json-schema.org/draft/2020-12/schema"

// requestRefSuffix distinguishes an op's request schema from the response
// schema that shares its op_id stem.
const requestRefSuffix = ".request"

// RequestSchemaRef returns the `gum://schema/{ref}` ref for op's request
// schema: the lowercased op_id plus ".request".
//
// The op_id cannot be used verbatim. 58 of the shipped op_ids carry a
// camelCase resource segment (calendar.calendarList.list,
// gmail.users.messages.batchModify), and the §8.2 served-ref grammar
// ^[a-z0-9][a-z0-9._-]{0,127}$ has no uppercase letters. Lowercasing is
// collision-free across the catalog and keeps the ref readable.
func RequestSchemaRef(opID string) string {
	if opID == "" {
		return ""
	}
	return strings.ToLower(opID) + requestRefSuffix
}

// RequestSchema derives the JSON Schema 2020-12 document describing op's
// request parameters from its RequestFields. It returns (nil, nil) for an op
// that declares none: 13 of the shipped ops carry no RequestFields, and an
// empty-properties schema for one of those would claim the op takes no
// arguments, which is false for most of them.
//
// The document deliberately omits `additionalProperties`. RequestFields covers
// the upstream API's own parameters; the gum invocation layer adds control args
// (fields, format, page_size) that a closed schema would declare invalid.
func RequestSchema(op Op) (map[string]any, error) {
	if len(op.RequestFields) == 0 {
		return nil, nil
	}

	props := make(map[string]any, len(op.RequestFields))
	var required []string

	for _, f := range op.RequestFields {
		if f.Name == "" {
			return nil, fmt.Errorf("op %s: request field with empty name", op.OpID)
		}
		sub, err := requestFieldSchema(f)
		if err != nil {
			return nil, fmt.Errorf("op %s: %w", op.OpID, err)
		}
		props[f.Name] = sub

		if f.Required {
			required = append(required, f.Name)
		}
	}

	doc := map[string]any{
		"$schema":    JSONSchemaDraft2020,
		"$id":        RequestSchemaRef(op.OpID),
		"title":      strings.TrimSpace(op.Title) + " request",
		"type":       "object",
		"properties": props,
	}
	if len(required) > 0 {
		doc["required"] = required
	}

	return doc, nil
}

// requestFieldSchema maps one RequestField onto its JSON Schema subschema. The
// field's Location is not carried: it describes how gum routes the value to the
// upstream transport, not what a valid value looks like.
func requestFieldSchema(f RequestField) (map[string]any, error) {
	sub := map[string]any{}

	if f.Type != "" {
		sub["type"] = f.Type
	}
	if f.Format != "" {
		sub["format"] = f.Format
	}
	if f.Description != "" {
		sub["description"] = f.Description
	}

	if len(f.Enum) > 0 {
		vals := make([]any, len(f.Enum))
		for i, e := range f.Enum {
			vals[i] = e
		}
		sub["enum"] = vals
	}

	if f.Type == "array" && f.ItemType != "" {
		sub["items"] = map[string]any{"type": f.ItemType}
	}

	if f.HasDefault() {
		v, err := f.DefaultValue()
		if err != nil {
			return nil, err
		}
		sub["default"] = v
	}

	return sub, nil
}
