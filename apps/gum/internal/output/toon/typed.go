// Package toon — typed-null decode layer for §9.0 lossless reconstruction.
//
// DecodeTypedDocument extends DecodeTOONDocument with a Schema that maps each
// field to a concrete Go type. Empty CSV cells (wire-level null) become typed
// nil pointers (*T)(nil) rather than untyped nil, preserving the distinction
// between null and zero/empty. Quoted-empty cells ("") for string fields become
// *string("").
//
// Spec anchor: §9.0 lossless-reconstruction clause.
package toon

import (
	"fmt"
	"strconv"
	"strings"
)

// FieldType identifies the Go type to use when decoding a TOON document field.
type FieldType int

const (
	FieldTypeString FieldType = iota
	FieldTypeInt
	FieldTypeFloat
	FieldTypeBool
)

// FieldSchema pairs a field name with its declared type.
type FieldSchema struct {
	Name string
	Type FieldType
}

// Schema describes the expected types for a TOON document's fields.
// When Fields is empty (Schema{}), DecodeTypedDocument falls back to the
// default behaviour: empty cells become untyped nil (no typed pointers).
type Schema struct {
	Fields []FieldSchema
}

// TypedDocument is the result of decoding a §9.0 TOON document with a Schema.
// Rows contains one []any per data row; each cell is either:
//   - a typed nil pointer (*T)(nil) for an empty CSV cell (null)
//   - a non-nil *T for a present cell (when schema is provided)
//   - untyped nil for empty cells when no schema is provided
//   - the raw decoded value for present cells when no schema is provided
type TypedDocument struct {
	Op            string
	Variant       string
	NextPageToken string
	Count         int
	FormatVersion int
	Fields        []string
	Rows          [][]any
}

// DecodeTypedDocument parses a §9.0 TOON document and applies schema type
// information to produce typed nil pointers for null cells.
//
// Rules (when schema is non-empty and field counts match):
//   - empty CSV cell + FieldTypeString  → (*string)(nil)
//   - `""` CSV cell  + FieldTypeString  → *string("") — non-nil pointer to ""
//   - empty CSV cell + FieldTypeInt     → (*int64)(nil)
//   - non-empty cell + FieldTypeInt     → *int64(v)
//   - empty CSV cell + FieldTypeFloat   → (*float64)(nil)
//   - non-empty cell + FieldTypeFloat   → *float64(v)
//   - empty CSV cell + FieldTypeBool    → (*bool)(nil)
//   - non-empty cell + FieldTypeBool    → *bool(v)
//
// When schema.Fields is empty, the function decodes using the existing default:
// empty cells → untyped nil, present cells → raw decoded values.
//
// Returns an error containing "schema" if len(schema.Fields) != 0 and
// len(schema.Fields) != len(document fields).
func DecodeTypedDocument(data []byte, schema Schema) (*TypedDocument, error) {
	toon, err := DecodeTOONDocument(data)
	if err != nil {
		return nil, err
	}

	if len(schema.Fields) != 0 && len(schema.Fields) != len(toon.Fields) {
		return nil, fmt.Errorf("toon: schema field count (%d) does not match document field count (%d): schema mismatch",
			len(schema.Fields), len(toon.Fields))
	}

	td := &TypedDocument{
		Op:            toon.Op,
		Variant:       toon.Variant,
		NextPageToken: toon.NextPageToken,
		Count:         toon.Count,
		FormatVersion: toon.FormatVersion,
		Fields:        toon.Fields,
	}

	if len(schema.Fields) == 0 {
		td.Rows = toon.Rows
		return td, nil
	}

	fieldTypes := make([]FieldType, len(schema.Fields))
	for i, fs := range schema.Fields {
		fieldTypes[i] = fs.Type
	}

	// Re-parse the raw CSV body to obtain per-cell quoting metadata.
	// DecodeTOONDocument trims trailing newlines before parsing, which silently
	// drops all-null rows; reParseCSVRows passes the raw body to parseDocumentCSV
	// so those rows are preserved. The two paths are intentionally parallel.
	rawRows, err := reParseCSVRows(data, len(toon.Fields))
	if err != nil {
		return nil, fmt.Errorf("toon: typed decode re-parse: %w", err)
	}

	td.Rows = make([][]any, 0, len(rawRows))
	for _, rawRow := range rawRows {
		td.Rows = append(td.Rows, applySchema(rawRow, fieldTypes))
	}
	return td, nil
}

// reParseCSVRows extracts the CSV body from a raw §9.0 document and returns
// the per-cell quoting metadata needed for typed decode.
//
// DecodeTOONDocument trims trailing newlines before parsing, silently dropping
// all-null rows. This function passes the raw body directly to parseDocumentCSV
// so those rows are preserved. A body of "\n" produces one empty row (all-null),
// while the "{}" sentinel (count=0) produces nil.
func reParseCSVRows(data []byte, _ int) ([][]csvCell, error) {
	s := string(data)

	sepIdx := strings.Index(s, "\n\n")
	if sepIdx < 0 {
		return nil, fmt.Errorf("toon: missing blank-line separator")
	}

	body := s[sepIdx+2:]

	// Detect the count=0 sentinel without consuming the body for row parsing.
	if strings.TrimRight(body, "\r\n") == "{}" {
		return nil, nil
	}
	if len(body) == 0 {
		return nil, nil
	}

	return parseDocumentCSV(body)
}

// applySchema converts a raw CSV row (with quoting metadata) to a typed []any.
// An unquoted empty cell is null for all types; a quoted empty cell is a
// non-null empty string for FieldTypeString.
func applySchema(row []csvCell, fieldTypes []FieldType) []any {
	out := make([]any, len(fieldTypes))
	for i, ft := range fieldTypes {
		var cell csvCell
		if i < len(row) {
			cell = row[i]
		}
		isNull := !cell.quoted && cell.value == ""

		switch ft {
		case FieldTypeString:
			if isNull {
				out[i] = (*string)(nil)
			} else {
				v := cell.value
				out[i] = &v
			}
		case FieldTypeInt:
			out[i] = parseTypedIntCell(cell.value, isNull)
		case FieldTypeFloat:
			out[i] = parseTypedFloatCell(cell.value, isNull)
		case FieldTypeBool:
			if isNull {
				out[i] = (*bool)(nil)
			} else {
				v := cell.value == "true"
				out[i] = &v
			}
		}
	}
	return out
}

// parseTypedIntCell converts an unquoted non-empty CSV cell to *int64.
// Returns (*int64)(nil) for null or unparseable input.
func parseTypedIntCell(value string, isNull bool) any {
	if isNull {
		return (*int64)(nil)
	}
	v, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return (*int64)(nil)
	}
	return &v
}

// parseTypedFloatCell converts an unquoted non-empty CSV cell to *float64.
// Returns (*float64)(nil) for null or unparseable input.
func parseTypedFloatCell(value string, isNull bool) any {
	if isNull {
		return (*float64)(nil)
	}
	v, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return (*float64)(nil)
	}
	return &v
}

// ToTOONDocument converts a TypedDocument back to a TOONDocument suitable for
// re-encoding with EncodeTOONDocument, preserving the null vs empty-string
// distinction required by the §9.0 lossless round-trip clause.
//
// Conversion rules:
//   - (*string)(nil)  → nil  (encodes as empty CSV cell)
//   - *string("")     → ""   (encodes as `""`)
//   - *string("foo")  → "foo"
//   - (*int64)(nil)   → nil
//   - *int64(v)       → int64(v)
//   - (*float64)(nil) → nil
//   - *float64(v)     → float64(v)
//   - (*bool)(nil)    → nil
//   - *bool(v)        → bool(v)
//   - untyped nil     → nil
//   - other values    → passed through unchanged
func (d *TypedDocument) ToTOONDocument() (*TOONDocument, error) {
	rows := make([][]any, len(d.Rows))
	for i, srcRow := range d.Rows {
		row := make([]any, len(srcRow))
		for j, cell := range srcRow {
			row[j] = typedCellToValue(cell)
		}
		rows[i] = row
	}
	return &TOONDocument{
		Op:            d.Op,
		Variant:       d.Variant,
		NextPageToken: d.NextPageToken,
		Count:         d.Count,
		FormatVersion: d.FormatVersion,
		Fields:        d.Fields,
		Rows:          rows,
	}, nil
}

// typedCellToValue dereferences a typed pointer cell back to a plain Go value
// for use in a TOONDocument that EncodeTOONDocument can encode. A nil pointer
// of any supported type becomes untyped nil (encodes as an empty CSV cell).
func typedCellToValue(cell any) any {
	if cell == nil {
		return nil
	}
	switch v := cell.(type) {
	case *string:
		if v == nil {
			return nil
		}
		return *v
	case *int64:
		if v == nil {
			return nil
		}
		return *v
	case *float64:
		if v == nil {
			return nil
		}
		return *v
	case *bool:
		if v == nil {
			return nil
		}
		return *v
	default:
		return cell
	}
}
