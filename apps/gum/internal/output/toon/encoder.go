// Package toon provides the TOON (Token-Optimized Object Notation) encoder and decoder.
//
// TOON is an in-tree, ~200 LOC compact encoding designed to reduce LLM token consumption
// relative to JSON while remaining fully round-trippable to/from JSON values.
//
// Encoding rules (Phase 4):
//   - Object: key=value pairs, one per line, terminated by a blank line (or EOF).
//   - Array:  rows of comma-separated values, one row per element.
//   - Null values are encoded as the empty string on the right-hand side of "=".
//   - Strings that contain commas, double-quotes, or newlines are CSV-quoted (RFC 4180).
//   - Zero-valued integer fields are omitted when EncoderOptions.OmitZeroCounts is true.
//   - An object with no fields encodes as "{}" (empty object sentinel), not "<empty>".
//     An object that has fields keeps them even when every value is empty.
//
// TOON is NOT a general replacement for JSON; it is scoped to the output profiles
// used by gum's response shaping pipeline.
package toon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// EncoderOptions controls optional encoding behaviours.
type EncoderOptions struct {
	// OmitZeroCounts, when true, causes integer fields with value 0 to be omitted
	// from the encoded output.
	OmitZeroCounts bool
}

// bareRe matches strings that can be encoded without quotes.
var bareRe = regexp.MustCompile(`^[A-Za-z0-9_\-./]+$`)

// keywords that must be quoted even if they match bareRe.
var keywords = map[string]bool{
	"null":  true,
	"true":  true,
	"false": true,
}

// isBare returns true if s can be encoded without CSV quoting.
func isBare(s string) bool {
	if s == "" {
		return false
	}
	if keywords[s] {
		return false
	}
	if _, err := strconv.ParseFloat(s, 64); err == nil {
		return false
	}
	return bareRe.MatchString(s)
}

// encodeString encodes a string value per TOON rules.
func encodeString(s string) string {
	if s == "" {
		return `""`
	}
	if isBare(s) {
		return s
	}
	// CSV-quote: wrap in double-quotes, doubling any interior double-quotes.
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// encodeScalar encodes a non-collection Go value to its TOON representation.
func encodeScalar(v any) (string, error) {
	if v == nil {
		return "null", nil
	}
	switch val := v.(type) {
	case bool:
		if val {
			return "true", nil
		}
		return "false", nil
	case float64:
		if math.IsNaN(val) || math.IsInf(val, 0) {
			return "", fmt.Errorf("toon: cannot encode NaN/Inf float")
		}
		// Whole number within int64 range: format as an integer. Outside that
		// range int64(val) would silently clamp to MaxInt64 (e.g. 1e19 ->
		// 9223372036854775807), so fall through to FormatFloat('f') which emits
		// the full digits without an exponent or a bogus clamp.
		if val == math.Trunc(val) && val >= -9223372036854775808.0 && val < 9223372036854775808.0 {
			return strconv.FormatInt(int64(val), 10), nil
		}
		// Whole-but-huge or fractional: shortest non-scientific representation.
		return strconv.FormatFloat(val, 'f', -1, 64), nil
	case json.Number:
		// The shaping pipeline decodes upstream bodies with UseNumber so wide
		// integers keep their digits (internal/output/profile.Apply). Emit the
		// literal verbatim; quoting it would turn a number into a string.
		if val == "" {
			return "", fmt.Errorf("toon: cannot encode empty json.Number")
		}
		return string(val), nil
	case string:
		return encodeString(val), nil
	case int:
		return strconv.Itoa(val), nil
	case int64:
		return strconv.FormatInt(val, 10), nil
	case int32:
		return strconv.FormatInt(int64(val), 10), nil
	case uint64:
		return strconv.FormatUint(val, 10), nil
	case uint32:
		return strconv.FormatUint(uint64(val), 10), nil
	default:
		// Fallback to JSON for unknown scalar types.
		b, err := json.Marshal(v)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
}

// isZeroVal returns true if v is an integer or float with value 0.
func isZeroVal(v any) bool {
	switch val := v.(type) {
	case json.Number:
		// A UseNumber-decoded zero must still count as zero, or OmitZeroCounts
		// would stop dropping zero counts on every shaped body.
		f, err := val.Float64()
		return err == nil && f == 0
	case float64:
		return val == 0
	case int:
		return val == 0
	case int64:
		return val == 0
	case int32:
		return val == 0
	case uint64:
		return val == 0
	case uint32:
		return val == 0
	default:
		return false
	}
}

// Encode serialises v (which must be JSON-compatible: map, slice, or scalar) into
// TOON bytes. It is equivalent to EncodeWithOptions(v, EncoderOptions{}).
func Encode(v any) ([]byte, error) {
	return EncodeWithOptions(v, EncoderOptions{})
}

// EncodeWithOptions serialises v into TOON bytes with the given options.
func EncodeWithOptions(v any, opts EncoderOptions) ([]byte, error) {
	return encodeValue(v, opts)
}

// encodeValue is the recursive encoder.
func encodeValue(v any, opts EncoderOptions) ([]byte, error) {
	if v == nil {
		return []byte("null"), nil
	}

	switch val := v.(type) {
	case map[string]any:
		return encodeMap(val, opts)
	case []any:
		return encodeArray(val, opts)
	default:
		s, err := encodeScalar(v)
		if err != nil {
			return nil, err
		}
		return []byte(s), nil
	}
}

// encodeMap encodes a map[string]any.
func encodeMap(m map[string]any, opts EncoderOptions) ([]byte, error) {
	if len(m) == 0 {
		return []byte("{}\n"), nil
	}

	// A populated map keeps its keys even when every value is empty. Spec
	// §"Null representation (normative)" makes an empty field null and a
	// quoted empty field the empty string, and calls TOON lossless for that
	// distinction; collapsing {"error":"","status":null} to {} dropped both
	// key names and set no lossy flag. Dropping empty fields is the expression
	// pipeline's strip_nulls stage (§9.1), which the caller opts into.

	// Collect keys sorted alphabetically.
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var buf bytes.Buffer
	hasAny := false
	for _, k := range keys {
		val := m[k]
		// OmitZeroCounts: skip zero-integer/float values.
		if opts.OmitZeroCounts && isZeroVal(val) {
			continue
		}
		// Encode value. Nested maps/arrays are JSON-encoded inline (single line).
		var valStr string
		switch vt := val.(type) {
		case map[string]any:
			b, err := json.Marshal(vt)
			if err != nil {
				return nil, err
			}
			valStr = string(b)
		case []any:
			b, err := json.Marshal(vt)
			if err != nil {
				return nil, err
			}
			valStr = string(b)
		default:
			s, err := encodeScalar(val)
			if err != nil {
				return nil, err
			}
			valStr = s
		}

		keyStr := encodeString(k)
		buf.WriteString(keyStr)
		buf.WriteByte('=')
		buf.WriteString(valStr)
		buf.WriteByte('\n')
		hasAny = true
	}

	if !hasAny {
		// All values were skipped (e.g. OmitZeroCounts removed everything, or all empty).
		return []byte("{}\n"), nil
	}
	return buf.Bytes(), nil
}

// homogeneousKeys returns sorted keys if all elements are maps with the same key set,
// otherwise returns nil.
func homogeneousKeys(arr []any) []string {
	if len(arr) == 0 {
		return nil
	}
	first, ok := arr[0].(map[string]any)
	if !ok {
		return nil
	}
	// Collect sorted keys from first element.
	keys := make([]string, 0, len(first))
	for k := range first {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Build a set for comparison.
	keySet := make(map[string]bool, len(keys))
	for _, k := range keys {
		keySet[k] = true
	}

	// Check remaining elements.
	for _, elem := range arr[1:] {
		m, ok := elem.(map[string]any)
		if !ok {
			return nil
		}
		if len(m) != len(keys) {
			return nil
		}
		for k := range m {
			if !keySet[k] {
				return nil
			}
		}
	}
	return keys
}

// encodeCSVValue encodes a value for use inside a CSV row.
// For maps and arrays, it uses JSON encoding (inline). For scalars, uses TOON scalar rules.
// Returns the string and a boolean indicating if the value is complex (contains newlines after encoding).
func encodeCSVValue(v any, opts EncoderOptions) (string, bool, error) {
	if v == nil {
		// null in array context = empty string (CSV empty cell)
		return "", false, nil
	}
	switch vt := v.(type) {
	case map[string]any:
		// Encode nested map inline as JSON for CSV cells.
		b, err := json.Marshal(vt)
		if err != nil {
			return "", false, err
		}
		s := string(b)
		return encodeString(s), false, nil
	case []any:
		b, err := json.Marshal(vt)
		if err != nil {
			return "", false, err
		}
		s := string(b)
		return encodeString(s), false, nil
	default:
		s, err := encodeScalar(v)
		if err != nil {
			return "", false, err
		}
		// Check if the encoded scalar contains newlines (would need fallback).
		if strings.Contains(s, "\n") {
			return s, true, nil
		}
		return s, false, nil
	}
}

// encodeArray encodes a []any value.
func encodeArray(arr []any, opts EncoderOptions) ([]byte, error) {
	if len(arr) == 0 {
		return []byte("[]\n"), nil
	}

	keys := homogeneousKeys(arr)
	if keys == nil {
		// Heterogeneous: fall back to JSON with comment.
		b, err := json.Marshal(arr)
		if err != nil {
			return nil, err
		}
		var buf bytes.Buffer
		buf.WriteString("# heterogeneous\n")
		buf.Write(b)
		buf.WriteByte('\n')
		return buf.Bytes(), nil
	}

	// Homogeneous: header row + data rows.
	var buf bytes.Buffer

	// Header row: comma-separated bare/quoted column names.
	for i, k := range keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.WriteString(encodeString(k))
	}
	buf.WriteByte('\n')

	// Data rows.
	for _, elem := range arr {
		m := elem.(map[string]any)
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			cell, _, err := encodeCSVValue(m[k], opts)
			if err != nil {
				return nil, err
			}
			buf.WriteString(cell)
		}
		buf.WriteByte('\n')
	}

	return buf.Bytes(), nil
}

// Decode parses TOON-encoded data and returns the Go value (map[string]any,
// []any, or a scalar). The returned value has the same structure as the
// original value passed to Encode; round-tripping through JSON Marshal/Unmarshal
// on the result must produce the same structure.
func Decode(data []byte) (any, error) {
	s := string(data)
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil, nil
	}

	// Empty object sentinel.
	if s == "{}" {
		return map[string]any{}, nil
	}
	// Empty array sentinel.
	if s == "[]" {
		return []any{}, nil
	}

	// Heterogeneous array: starts with "# heterogeneous".
	if strings.HasPrefix(s, "# heterogeneous\n") {
		jsonPart := strings.TrimPrefix(s, "# heterogeneous\n")
		var v []any
		if err := json.Unmarshal([]byte(jsonPart), &v); err != nil {
			return nil, fmt.Errorf("toon: decode heterogeneous: %w", err)
		}
		return v, nil
	}

	// Headers format: starts with "# headers:".
	if strings.HasPrefix(s, "# headers:") {
		return decodeHeadersFormat(s)
	}

	// Logical, not physical, lines: a CSV-quoted value may carry a raw newline
	// (an API snippet, a description), and splitting on every "\n" would cut
	// such a value in half. The tail half then reaches the dispatch below as if
	// it were a line of its own, which used to fabricate a field when the tail
	// contained "=" and truncate the value when it did not.
	lines := splitTOONLines(s)
	firstNonBlank := ""
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			firstNonBlank = l
			break
		}
	}

	// Key=value object format: first non-blank line has an unquoted "=".
	if unquotedIndexByte(firstNonBlank, '=') >= 0 {
		return decodeKeyValue(lines)
	}

	// Array (CSV table format without # headers:): first line has no "=" but has commas.
	// OR it could be a single header row followed by data rows.
	if unquotedIndexByte(firstNonBlank, ',') >= 0 || isHeaderLine(firstNonBlank, lines) {
		return decodeCSVTable(s)
	}

	// Single scalar value.
	return decodeScalar(s)
}

// isHeaderLine checks if the first line looks like a CSV header followed by data rows.
func isHeaderLine(firstLine string, lines []string) bool {
	if len(lines) < 2 {
		return false
	}
	// If there are multiple lines and no "=" in first line, treat as CSV table.
	return unquotedIndexByte(firstLine, '=') < 0 && len(lines) >= 2
}

// splitTOONLines splits a TOON document into logical lines, treating a newline
// inside a CSV-quoted field as data rather than a line break. A quoted span
// opens only at a field start (line start, or just after "=" or ","), which is
// where the encoder puts its quotes, so a stray quote inside an unquoted token
// cannot swallow the rest of the document.
func splitTOONLines(s string) []string {
	var lines []string
	var cur strings.Builder
	inQuotes := false
	atFieldStart := true

	for i := 0; i < len(s); i++ {
		ch := s[i]

		switch {
		case inQuotes && ch == '"':
			if i+1 < len(s) && s[i+1] == '"' {
				// Doubled "" is an escaped quote; keep both bytes so
				// unquoteCSV still sees the escape.
				cur.WriteString(`""`)
				i++
				continue
			}
			inQuotes = false
			cur.WriteByte(ch)
		case !inQuotes && ch == '"' && atFieldStart:
			inQuotes = true
			cur.WriteByte(ch)
		case !inQuotes && ch == '\n':
			lines = append(lines, cur.String())
			cur.Reset()
			atFieldStart = true
			continue
		default:
			cur.WriteByte(ch)
		}

		atFieldStart = !inQuotes && (ch == '=' || ch == ',')
	}

	return append(lines, cur.String())
}

// unquotedIndexByte returns the index of the first b in s that sits outside a
// CSV-quoted field, or -1. It is what separates a real key/value delimiter from
// one that is part of a value, e.g. the "=" in {"a=b": v}, which encodes as
// `"a=b"=v`.
func unquotedIndexByte(s string, b byte) int {
	inQuotes := false
	atFieldStart := true

	for i := 0; i < len(s); i++ {
		ch := s[i]

		if inQuotes {
			if ch == '"' {
				if i+1 < len(s) && s[i+1] == '"' {
					i++
					continue
				}
				inQuotes = false
			}
			atFieldStart = false
			continue
		}

		if ch == '"' && atFieldStart {
			inQuotes = true
			atFieldStart = false
			continue
		}
		if ch == b {
			return i
		}

		atFieldStart = ch == '=' || ch == ','
	}

	return -1
}

// decodeHeadersFormat decodes the "# headers: ..." format.
func decodeHeadersFormat(s string) (any, error) {
	// Strip the "# headers:" prefix line and parse the rest as CSV.
	idx := strings.IndexByte(s, '\n')
	if idx < 0 {
		return []any{}, nil
	}
	headerLine := strings.TrimSpace(strings.TrimPrefix(s[:idx], "# headers:"))
	rest := s[idx+1:]

	headers := parseCSVRow(headerLine)
	rows, err := parseDocumentCSV(rest)
	if err != nil {
		return nil, err
	}

	result := make([]any, 0, len(rows))
	for _, row := range rows {
		if len(row) == 0 {
			continue
		}
		m := make(map[string]any, len(headers))
		for i, h := range headers {
			if i < len(row) {
				m[h] = decodeCSVCellWithQuoted(row[i])
			} else {
				m[h] = nil
			}
		}
		result = append(result, m)
	}
	return result, nil
}

// decodeCSVTable decodes a CSV table (header row + data rows, no "# headers:" prefix).
// Uses parseDocumentCSV to handle multi-line quoted fields correctly.
func decodeCSVTable(s string) (any, error) {
	rows, err := parseDocumentCSV(s)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return []any{}, nil
	}
	headers := make([]string, len(rows[0]))
	for i, h := range rows[0] {
		headers[i] = h.value
	}
	result := make([]any, 0, len(rows)-1)
	for _, row := range rows[1:] {
		if len(row) == 0 {
			continue
		}
		m := make(map[string]any, len(headers))
		for i, h := range headers {
			if i < len(row) {
				m[h] = decodeCSVCellWithQuoted(row[i])
			} else {
				m[h] = nil
			}
		}
		result = append(result, m)
	}
	return result, nil
}

// decodeCSVCell converts a CSV cell string to an appropriate Go type.
// An empty cell → nil (null), a quoted empty string `""` → "" (empty string).
func decodeCSVCell(s string) any {
	if s == "" {
		// Empty cell from CSV = null.
		return nil
	}
	// Try JSON keywords.
	switch s {
	case "null":
		return nil
	case "true":
		return true
	case "false":
		return false
	}
	// Try numeric.
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	// Try JSON object/array (for nested values encoded as JSON).
	if len(s) > 0 && (s[0] == '{' || s[0] == '[') {
		var v any
		if err := json.Unmarshal([]byte(s), &v); err == nil {
			return v
		}
	}
	return s
}

func decodeCSVCellWithQuoted(cell csvCell) any {
	if cell.quoted {
		if len(cell.value) > 0 && (cell.value[0] == '{' || cell.value[0] == '[') {
			return decodeCSVCell(cell.value)
		}
		return cell.value
	}
	return decodeCSVCell(cell.value)
}

// decodeKeyValue decodes key=value lines into a map[string]any.
func decodeKeyValue(lines []string) (any, error) {
	m := make(map[string]any)
	for _, line := range lines {
		if line == "" {
			continue
		}
		idx := keyValueSplit(line)
		if idx < 0 {
			continue
		}
		key := unquoteString(line[:idx])
		valStr := line[idx+1:]
		m[key] = decodeKeyValueCell(valStr)
	}
	return m, nil
}

// decodeKeyValueCell decodes a map value that still carries its delimiters. A
// double-quoted value is a STRING LITERAL — the encoder quotes a string only to
// stop it parsing back as a keyword/number/empty (e.g. the string "null" is
// emitted as `"null"`), so it must be unquoted WITHOUT re-interpretation.
// Otherwise it's a bare token handled by decodeCSVCell (keyword/number/JSON).
// (decodeCSVCell can't do this itself: in the CSV-table path it receives cells
// already stripped of their quotes by parseDocumentCSV, so it can't tell a
// quoted "null" from the bare keyword — the CSV-table round-trip is tracked
// separately.)
func decodeKeyValueCell(s string) any {
	if len(s) >= 2 && s[0] == '"' {
		return unquoteCSV(s)
	}
	return decodeCSVCell(s)
}

// keyValueSplit returns the index of the '=' separating a TOON key from its
// value. A quoted key (CSV-quoted, interior quotes doubled as "") may itself
// contain '=' — e.g. {"a=b": v} encodes as `"a=b"=v` — so the split must skip
// any '=' inside the leading quoted key and use the '=' after the closing quote.
func keyValueSplit(line string) int {
	if len(line) == 0 || line[0] != '"' {
		return strings.IndexByte(line, '=')
	}
	i := 1
	for i < len(line) {
		if line[i] == '"' {
			if i+1 < len(line) && line[i+1] == '"' {
				i += 2 // doubled "" is an escaped quote, not the close
				continue
			}
			i++ // step past the closing quote
			break
		}
		i++
	}
	if i < len(line) && line[i] == '=' {
		return i
	}
	if eq := strings.IndexByte(line[i:], '='); eq >= 0 {
		return i + eq
	}
	return -1
}

// decodeScalar decodes a single scalar TOON value.
func decodeScalar(s string) (any, error) {
	switch s {
	case "null":
		return nil, nil
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	// Quoted string.
	if len(s) >= 2 && s[0] == '"' {
		return unquoteCSV(s), nil
	}
	// Numeric.
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f, nil
	}
	// Bare string.
	return s, nil
}

// unquoteString unquotes a TOON string (for map keys).
func unquoteString(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return unquoteCSV(s)
	}
	return s
}

// unquoteCSV unquotes a CSV-quoted string (surrounding double-quotes, interior "" → ").
func unquoteCSV(s string) string {
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return s
	}
	inner := s[1 : len(s)-1]
	return strings.ReplaceAll(inner, `""`, `"`)
}

// parseCSVRow parses a single CSV row, handling double-quote escaping and
// newlines within quoted fields. This is a simplified RFC 4180 parser.
func parseCSVRow(line string) []string {
	var fields []string
	var field strings.Builder
	inQuotes := false
	i := 0
	for i < len(line) {
		ch := line[i]
		if inQuotes {
			if ch == '"' {
				// Check for escaped quote (doubled).
				if i+1 < len(line) && line[i+1] == '"' {
					field.WriteByte('"')
					i += 2
					continue
				}
				// Closing quote.
				inQuotes = false
				i++
				continue
			}
			field.WriteByte(ch)
			i++
		} else {
			if ch == '"' {
				inQuotes = true
				i++
				continue
			}
			if ch == ',' {
				fields = append(fields, field.String())
				field.Reset()
				i++
				continue
			}
			field.WriteByte(ch)
			i++
		}
	}
	fields = append(fields, field.String())
	return fields
}
