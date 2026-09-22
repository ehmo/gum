package toon

import (
	"encoding/json"
	"sort"
	"strings"
)

// nextPageTokenKeys are the response field names that fill the §9.0
// next_page_token header. Google spells the field nextPageToken; the header
// the spec names is snake_case, so the projection renames it.
var nextPageTokenKeys = []string{"next_page_token", "nextPageToken"}

// recordArrayKeys are the record-array field names, in precedence order. A
// named key wins over a lone unnamed array so that a response carrying both a
// named record array and a second array still reports its records: the Google
// Ads keyword-history adapter adds unmatchedInputs beside results, and without
// "results" here a 243-row response reported result_count 0 with 100 rows in
// the body (gum-36zi).
var recordArrayKeys = []string{"items", "data", "messages", "results"}

// RecordArrayKey returns the key holding the record array of a shaped top-level
// object, or "" when the object has none. Named keys win; otherwise a lone
// array-valued field is the records, which covers both the stage-5 wrap and a
// service-specific key such as "files". An object with two or more unnamed
// arrays has no record array, because guessing between them would count or
// reorder the wrong one.
func RecordArrayKey(m map[string]any) string {
	for _, key := range recordArrayKeys {
		if _, ok := m[key].([]any); ok {
			return key
		}
	}
	only := ""
	found := 0
	for key, val := range m {
		if _, ok := val.([]any); !ok {
			continue
		}
		found++
		if found > 1 {
			return ""
		}
		only = key
	}
	return only
}

// DocumentFrom projects a decoded response onto a §9.0 TOON document,
// reporting ok=false when §9.0's "falls back to JSON for nested/non-uniform"
// rule applies.
//
// What maps:
//
//	[]any of objects                   → one row each, no extra headers
//	{"messages":[...], "kind":"..."}   → the array is the rows, kind is a header
//	{"id":"x","name":"y"}              → one row, the object's keys are fields
//	{}                                 → count 0, body "{}"
//
// What does not: an object graph with no record array (the nested case), an
// array whose elements are not objects, a sibling of the record array that is
// itself an object or array, a bare scalar body, and a field name the fields
// header cannot carry.
//
// A field whose value is a list or an object stays in its row as inline JSON.
// §9.4 requires that: gum.search_apis lists params_required among its TOON
// fields and its value is an array.
//
// The record array's own key rides the "records" header, so a consumer with no
// response schema still knows what the rows are.
func DocumentFrom(op, variant string, v any, omitZeroCounts bool) (TOONDocument, bool) {
	doc := TOONDocument{Op: op, Variant: variant, FormatVersion: 1}

	var records []any

	switch t := v.(type) {
	case []any:
		records = t

	case map[string]any:
		if len(t) == 0 {
			return doc, true // count 0, body "{}"
		}

		key := RecordArrayKey(t)
		if key == "" {
			// No record array: the object itself is one record, which is how a
			// single-resource GET reaches TOON without inventing a wrapper.
			// A non-scalar value here means a nested graph, so it falls back.
			fields, ok := scalarFields(t, omitZeroCounts)
			if !ok {
				return doc, false
			}
			doc.Fields = fields
			doc.Count = 1
			doc.Rows = [][]any{rowFor(t, fields)}
			return doc, true
		}

		records, _ = t[key].([]any)
		doc.RecordKey = key
		for sibling, val := range t {
			if sibling == key {
				continue
			}
			if !addSibling(&doc, sibling, val, omitZeroCounts) {
				return doc, false
			}
		}
		sort.Slice(doc.Extra, func(i, j int) bool { return doc.Extra[i].Key < doc.Extra[j].Key })

	default:
		return doc, false
	}

	fields, ok := unionFields(records)
	if !ok {
		return doc, false
	}
	doc.Fields = fields
	doc.Count = len(records)
	for _, rec := range records {
		m, _ := rec.(map[string]any)
		doc.Rows = append(doc.Rows, rowFor(m, fields))
	}
	return doc, true
}

// EncodeDocument encodes a decoded response as a §9.0 TOON document, or
// returns (nil, nil) when the value is not representable and the caller must
// fall back to JSON.
func EncodeDocument(op, variant string, v any, omitZeroCounts bool) ([]byte, error) {
	doc, ok := DocumentFrom(op, variant, v, omitZeroCounts)
	if !ok {
		return nil, nil
	}
	return EncodeTOONDocument(doc)
}

// addSibling routes one top-level field that sits beside the record array to
// its header slot. It reports false when the field cannot ride the header,
// which sends the whole response to the JSON fallback.
func addSibling(doc *TOONDocument, key string, val any, omitZeroCounts bool) bool {
	if !isScalar(val) {
		return false
	}
	if omitZeroCounts && isZeroNumber(val) {
		return true
	}
	for _, alias := range nextPageTokenKeys {
		if key != alias {
			continue
		}
		s, ok := val.(string)
		if !ok {
			return false
		}
		doc.NextPageToken = s
		return true
	}
	if !ValidExtraHeaderKey(key) {
		return false
	}
	doc.Extra = append(doc.Extra, DocumentHeader{Key: key, Value: val})
	return true
}

// scalarFields returns the sorted field list for an object encoded as one row,
// or ok=false when any value is an object or array.
func scalarFields(m map[string]any, omitZeroCounts bool) ([]string, bool) {
	fields := make([]string, 0, len(m))
	for k, val := range m {
		if !isScalar(val) || !validFieldName(k) {
			return nil, false
		}
		if omitZeroCounts && isZeroNumber(val) {
			continue
		}
		fields = append(fields, k)
	}
	sort.Strings(fields)
	return fields, true
}

// unionFields returns the sorted union of the record keys, or ok=false when a
// record is not an object. The union rather than the first record's keys: a
// field absent from one record is an empty cell, which §9.0 reads as null, and
// dropping the column would lose it from every record that has it.
func unionFields(records []any) ([]string, bool) {
	seen := map[string]bool{}
	for _, rec := range records {
		m, ok := rec.(map[string]any)
		if !ok {
			return nil, false
		}
		for k := range m {
			if !validFieldName(k) {
				return nil, false
			}
			seen[k] = true
		}
	}

	fields := make([]string, 0, len(seen))
	for k := range seen {
		fields = append(fields, k)
	}
	sort.Strings(fields)
	return fields, true
}

// rowFor projects one record onto the field list. A field the record does not
// carry becomes nil, which encodes as the empty cell §9.0 reads as null.
func rowFor(m map[string]any, fields []string) []any {
	row := make([]any, len(fields))
	for i, f := range fields {
		row[i] = m[f]
	}
	return row
}

// validFieldName rejects names the fields header cannot carry: splitFields
// splits on commas and the header is one line.
func validFieldName(k string) bool {
	if k == "" || k != strings.TrimSpace(k) {
		return false
	}
	return !strings.ContainsAny(k, ",\n\r")
}

// isScalar reports whether v is a JSON scalar, the only thing a header value
// can hold.
func isScalar(v any) bool {
	switch v.(type) {
	case map[string]any, []any:
		return false
	}
	return true
}

// isZeroNumber reports whether v is a numeric zero, which is what
// omit_zero_counts drops.
func isZeroNumber(v any) bool {
	switch v.(type) {
	case bool, string, nil:
		return false
	}
	f, ok := toNumber(v)
	return ok && f == 0
}

// toNumber converts a numeric value to float64. A json.Number arrives from a
// body decoded with UseNumber; a literal too large for float64 saturates
// rather than failing, which keeps the comparison total.
func toNumber(v any) (float64, bool) {
	switch vt := v.(type) {
	case float64:
		return vt, true
	case int:
		return float64(vt), true
	case int64:
		return float64(vt), true
	case int32:
		return float64(vt), true
	case json.Number:
		f, err := vt.Float64()
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}
