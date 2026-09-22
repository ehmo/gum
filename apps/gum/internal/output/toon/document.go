// Package toon — §9.0 two-section TOON document codec.
//
// A §9.0 TOON document has two sections separated by a blank line:
//
//	Header: YAML-style "key: value" lines
//	Body:   RFC 4180 CSV rows (no header row; column order from "fields" header)
//
// Required header keys: op, variant, count, fields, format_version (must equal 1).
// Optional header key:  next_page_token (omitted when empty).
// count=0 body emits the sentinel "{}".
// Null values are empty CSV cells; empty strings are double-quoted empty fields "".
//
// Two header extensions beyond §9.0's required set keep the document from
// dropping what the response carried.
//
// "records" names the JSON key the rows came from, when the rows were nested
// under one. §9.0's header has no slot for it, and §9.0 says reconstruction
// needs the variant's response schema, which names the array. A client reading
// a tee artifact or a plugin's output has no schema, and the key is one short
// token, so it rides the header.
//
// A response often carries scalar fields beside its record array: kind,
// resultSizeEstimate, the <field>_omitted_count that collapse_arrays writes.
// The CSV body has no column for them and dropping them would lose data the
// caller asked for, so they ride the header as extra keys after the required
// set. Their values are compact JSON, which is valid YAML flow scalar syntax,
// keeps a string with a newline or a leading space on one line, and preserves
// the string/number/bool distinction the bare required keys do not need.
package toon

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrToonVersionUnsupported is returned when a §9.0 document carries a
// format_version other than 1.
var ErrToonVersionUnsupported = errors.New("TOON_VERSION_UNSUPPORTED")

// TOONDocument is the parsed representation of a §9.0 two-section TOON document.
type TOONDocument struct {
	Op            string
	Variant       string
	Count         int
	Fields        []string
	FormatVersion int
	NextPageToken string // empty = absent
	RecordKey     string // JSON key the rows came from; empty = absent
	Extra         []DocumentHeader
	Rows          [][]any
}

// DocumentHeader is one header line beyond the §9.0 required set: a scalar the
// response carried beside its record array. Value is a JSON scalar (string,
// float64, bool, or nil).
type DocumentHeader struct {
	Key   string
	Value any
}

// requiredHeaderKeys lists the §9.0 header keys that must be present.
var requiredHeaderKeys = []string{"op", "variant", "count", "fields", "format_version"}

// reservedHeaderKeys are the header keys the document shape owns. An extra
// header may not use one: a response field named "count" would otherwise
// shadow the row count and the decoder would read the wrong number.
var reservedHeaderKeys = map[string]bool{
	"op": true, "variant": true, "count": true,
	"fields": true, "format_version": true, "next_page_token": true,
	"records": true,
}

// ErrReservedHeaderKey is returned when an extra header collides with a
// reserved key or is not a legal one-line header key.
var ErrReservedHeaderKey = errors.New("toon: extra header key is reserved or malformed")

// ValidExtraHeaderKey reports whether k may carry an extra header value.
// Callers building a document use it to decide between the header and the
// JSON fallback, rather than discovering the clash at encode time.
func ValidExtraHeaderKey(k string) bool {
	if k == "" || reservedHeaderKeys[k] {
		return false
	}
	if k != strings.TrimSpace(k) {
		return false
	}
	return !strings.ContainsAny(k, ":\n\r")
}

// DecodeTOONDocument parses a §9.0 header+body TOON document.
//
// Returns ErrToonVersionUnsupported if format_version != 1.
// Returns an error if any required header key is missing.
func DecodeTOONDocument(data []byte) (*TOONDocument, error) {
	s := string(data)

	sepIdx := strings.Index(s, "\n\n")
	if sepIdx < 0 {
		return nil, fmt.Errorf("toon: §9.0 document missing blank-line separator between header and body")
	}
	lines, err := parseDocumentHeaders(s[:sepIdx])
	if err != nil {
		return nil, err
	}
	headers := make(map[string]string, len(lines))
	for _, l := range lines {
		headers[l.key] = l.val
	}

	for _, k := range requiredHeaderKeys {
		if _, ok := headers[k]; !ok {
			return nil, fmt.Errorf("toon: §9.0 document missing required header key %q", k)
		}
	}

	fv, err := strconv.Atoi(headers["format_version"])
	if err != nil {
		return nil, fmt.Errorf("toon: format_version %q is not an integer: %w", headers["format_version"], err)
	}
	if fv != 1 {
		return nil, fmt.Errorf("%w: got format_version=%d", ErrToonVersionUnsupported, fv)
	}

	count, err := strconv.Atoi(headers["count"])
	if err != nil {
		return nil, fmt.Errorf("toon: count %q is not an integer: %w", headers["count"], err)
	}

	fields := splitFields(headers["fields"])

	doc := &TOONDocument{
		Op:            headers["op"],
		Variant:       headers["variant"],
		Count:         count,
		Fields:        fields,
		FormatVersion: fv,
		NextPageToken: headers["next_page_token"],
		RecordKey:     headers["records"],
		Extra:         extraHeaders(lines),
	}

	body := strings.TrimRight(s[sepIdx+2:], "\r\n")
	if count == 0 {
		if strings.TrimSpace(body) != "{}" {
			return nil, fmt.Errorf("toon: count=0 but body is not {}: %q", body)
		}
		return doc, nil
	}

	rawRows, err := parseDocumentCSV(body)
	if err != nil {
		return nil, fmt.Errorf("toon: body CSV parse error: %w", err)
	}
	doc.Rows = make([][]any, 0, len(rawRows))
	for _, rawRow := range rawRows {
		doc.Rows = append(doc.Rows, mapCSVRowToFields(rawRow, fields))
	}
	return doc, nil
}

// headerLine is one parsed "key: value" header, kept in file order so extra
// headers decode in the order they were written.
type headerLine struct {
	key string
	val string
}

// parseDocumentHeaders parses the header section into ordered key/value pairs.
// Lines are "key: value"; blank lines and CRLF are tolerated.
func parseDocumentHeaders(section string) ([]headerLine, error) {
	var lines []headerLine
	for _, line := range strings.Split(section, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		colonIdx := strings.IndexByte(line, ':')
		if colonIdx < 0 {
			return nil, fmt.Errorf("toon: malformed header line %q", line)
		}
		lines = append(lines, headerLine{
			key: strings.TrimSpace(line[:colonIdx]),
			val: strings.TrimSpace(line[colonIdx+1:]),
		})
	}
	return lines, nil
}

// extraHeaders returns the non-reserved header lines as typed values.
//
// A value that is not valid JSON is kept as the raw string rather than
// dropped: a hand-written document from a plugin may spell a scalar the way
// the spec example spells op and variant, bare.
func extraHeaders(lines []headerLine) []DocumentHeader {
	var out []DocumentHeader
	for _, l := range lines {
		if reservedHeaderKeys[l.key] {
			continue
		}
		var v any
		if err := json.Unmarshal([]byte(l.val), &v); err != nil {
			v = l.val
		}
		out = append(out, DocumentHeader{Key: l.key, Value: v})
	}
	return out
}

// splitFields splits the bare comma-separated fields header value into names.
// No RFC 4180 quoting is expected in the fields list.
func splitFields(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	for i, p := range parts {
		parts[i] = strings.TrimSpace(p)
	}
	return parts
}

// mapCSVRowToFields converts a parsed CSV row (with quoting metadata) to a
// typed []any, aligned to the given field list. Unquoted-empty → nil (null);
// quoted-empty → "" (empty string); otherwise typed via decodeDocumentCell.
func mapCSVRowToFields(row []csvCell, fields []string) []any {
	out := make([]any, len(fields))
	for i := range fields {
		if i >= len(row) {
			out[i] = nil
			continue
		}
		cell := row[i]
		switch {
		case cell.quoted:
			out[i] = cell.value
		case cell.value == "":
			out[i] = nil
		default:
			out[i] = decodeDocumentCell(cell.value)
		}
	}
	return out
}

// csvCell holds a parsed CSV cell with quoting metadata.
// quoted distinguishes "" (empty string) from an unquoted empty cell (null).
type csvCell struct {
	value  string
	quoted bool
}

// parseDocumentCSV parses a CSV body (no header row) into rows of csvCell,
// tracking per-cell quoting so callers can distinguish null from empty string.
// Handles RFC 4180 multi-line quoted fields and CRLF line endings.
func parseDocumentCSV(s string) ([][]csvCell, error) {
	var rows [][]csvCell
	var currentRow []csvCell
	var field strings.Builder
	inQuotes := false
	wasQuoted := false
	i, n := 0, len(s)

	flushCell := func() {
		currentRow = append(currentRow, csvCell{value: field.String(), quoted: wasQuoted})
		field.Reset()
		wasQuoted = false
	}
	flushRow := func() {
		if len(currentRow) > 0 {
			rows = append(rows, currentRow)
			currentRow = nil
		}
	}

	for i < n {
		ch := s[i]
		if inQuotes {
			if ch == '"' {
				if i+1 < n && s[i+1] == '"' {
					field.WriteByte('"')
					i += 2
				} else {
					inQuotes = false
					i++
				}
			} else {
				field.WriteByte(ch)
				i++
			}
			continue
		}
		switch ch {
		case '"':
			inQuotes = true
			wasQuoted = true
		case ',':
			flushCell()
		case '\n':
			flushCell()
			flushRow()
		case '\r':
			// skip CR in CRLF
		default:
			field.WriteByte(ch)
		}
		i++
	}
	// Flush any trailing field/row not terminated by newline.
	if field.Len() > 0 || wasQuoted || len(currentRow) > 0 {
		flushCell()
		flushRow()
	}
	return rows, nil
}

// decodeDocumentCell converts an unquoted non-empty CSV cell string to a typed
// Go value. Callers have already handled empty/null; this handles typed scalars.
func decodeDocumentCell(s string) any {
	switch s {
	case "null":
		return nil
	case "true":
		return true
	case "false":
		return false
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	return s
}

// EncodeTOONDocument encodes a TOONDocument to §9.0 wire format.
//
// Header keys are emitted in canonical order:
// op, variant, format_version, count, fields[, records][, next_page_token][, extra...]
// followed by a blank line, then the CSV body.
// Extra headers keep the order the caller built them in, and their values are
// compact JSON.
// count=0 emits the body sentinel "{}".
// nil cell values encode as empty CSV cells; empty-string values encode as "".
func EncodeTOONDocument(doc TOONDocument) ([]byte, error) {
	var buf bytes.Buffer

	writeHeader := func(key, val string) {
		buf.WriteString(key)
		buf.WriteString(": ")
		buf.WriteString(val)
		buf.WriteByte('\n')
	}

	writeHeader("op", doc.Op)
	writeHeader("variant", doc.Variant)
	writeHeader("format_version", strconv.Itoa(doc.FormatVersion))
	writeHeader("count", strconv.Itoa(doc.Count))
	writeHeader("fields", strings.Join(doc.Fields, ","))
	if doc.RecordKey != "" {
		writeHeader("records", doc.RecordKey)
	}
	if doc.NextPageToken != "" {
		writeHeader("next_page_token", doc.NextPageToken)
	}
	for _, h := range doc.Extra {
		if !ValidExtraHeaderKey(h.Key) {
			return nil, fmt.Errorf("%w: %q", ErrReservedHeaderKey, h.Key)
		}
		val, err := json.Marshal(h.Value)
		if err != nil {
			return nil, fmt.Errorf("toon: extra header %q: %w", h.Key, err)
		}
		writeHeader(h.Key, string(val))
	}
	buf.WriteByte('\n') // blank-line separator

	if doc.Count == 0 {
		buf.WriteString("{}\n")
		return buf.Bytes(), nil
	}

	for _, row := range doc.Rows {
		for j := range doc.Fields {
			if j > 0 {
				buf.WriteByte(',')
			}
			if j < len(row) {
				cell, err := encodeDocumentCell(row[j])
				if err != nil {
					return nil, err
				}
				buf.WriteString(cell)
			}
			// Columns beyond len(row) are implicitly null (trailing comma).
		}
		buf.WriteByte('\n')
	}

	return buf.Bytes(), nil
}

// encodeDocumentCell encodes a single cell value for the §9.0 CSV body.
//
//   - nil        → empty cell (null)
//   - ""         → `""` (quoted empty, preserves empty-string distinction)
//   - string     → RFC 4180 quoted if needed, otherwise bare
//   - bool/float → canonical string representation
//   - anything else → compact JSON in a quoted cell
//
// The JSON arm carries a field whose value is a list or an object, which §9.4
// requires: gum.search_apis names params_required as a TOON field and its
// value is an array. A CSV cell has no type, so such a cell decodes back as
// the JSON text rather than as the structure. That is the lossy case §9.0
// already covers, and the alternative — refusing the whole response — would
// send every list-valued op to the JSON fallback.
func encodeDocumentCell(v any) (string, error) {
	if v == nil {
		return "", nil
	}
	switch val := v.(type) {
	case string:
		return encodeDocumentString(val), nil
	case bool:
		if val {
			return "true", nil
		}
		return "false", nil
	case json.Number:
		// Verbatim, for the same reason as encodeScalar: the default branch
		// below would quote the digits as a string cell.
		return string(val), nil
	case float64:
		if val == float64(int64(val)) {
			return strconv.FormatInt(int64(val), 10), nil
		}
		return strconv.FormatFloat(val, 'f', -1, 64), nil
	case int:
		return strconv.Itoa(val), nil
	case int64:
		return strconv.FormatInt(val, 10), nil
	case int32:
		return strconv.FormatInt(int64(val), 10), nil
	default:
		b, err := json.Marshal(val)
		if err != nil {
			return "", fmt.Errorf("toon: encode cell %T: %w", val, err)
		}
		return encodeDocumentString(string(b)), nil
	}
}

// encodeDocumentString encodes a string for a §9.0 CSV cell.
// Empty → `""` to preserve the null/empty-string distinction.
// Strings containing ,  "  \n  \r are RFC 4180 quoted.
func encodeDocumentString(s string) string {
	if s == "" {
		return `""`
	}
	if strings.ContainsAny(s, ",\"\n\r") {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}
