// Package jcs implements RFC 8785 JSON Canonicalization Scheme (JCS).
// It provides Marshal, which serializes any Go value to a canonical,
// deterministic JSON byte sequence suitable for hashing and signing.
package jcs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"unicode/utf16"
)

// ErrJCSUnsupportedType is returned when the input contains a type that
// cannot be represented in JSON (chan, func, complex, unsafe.Pointer, etc.).
var ErrJCSUnsupportedType = errors.New("jcs: unsupported type")

// ErrJCSInvalidNumber is returned when the input contains a floating-point
// value that has no JSON representation (NaN, +Inf, -Inf).
var ErrJCSInvalidNumber = errors.New("jcs: invalid number (NaN or Inf)")

// Marshal serializes v to a canonical JSON byte slice according to RFC 8785.
//
// Key ordering: object keys are sorted by UTF-16 code unit sequence (§3.2.3).
// Array order: preserved exactly as-is (§3.2.2).
// Numbers: shortest IEEE 754 representation; integer-valued floats have no
// decimal point.
// Strings: U+0000–U+001F encoded as \uXXXX (lowercase hex); " and \ escaped;
// all other code points passed through as UTF-8.
func Marshal(v any) ([]byte, error) {
	// Phase 1: pre-validation — reject unsupported types and invalid numbers
	// via reflection before attempting json.Marshal, so callers receive
	// sentinel errors rather than stdlib error strings.
	if err := validateValue(reflect.ValueOf(v)); err != nil {
		return nil, err
	}

	// Phase 2: normalization — round-trip through json.Marshal + Decode with
	// UseNumber to produce a generic tree that preserves numeric precision.
	tree, err := normalizeToTree(v)
	if err != nil {
		return nil, err
	}

	// Phase 3: canonical emission — walk the generic tree and write JCS output.
	var buf bytes.Buffer
	if err := emitCanonical(&buf, tree); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// validateValue walks v via reflection and returns an error for any value that
// is unsupported (chan, func, complex, unsafe.Pointer) or an invalid float
// (NaN, Inf). It descends into maps, slices, arrays, structs, and pointers.
func validateValue(v reflect.Value) error {
	if !v.IsValid() {
		return nil
	}
	// Unwrap pointer/interface chains before inspecting the concrete kind.
	for v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}

	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return fmt.Errorf("%w: %s", ErrJCSUnsupportedType, v.Type())
	case reflect.Complex64, reflect.Complex128:
		return fmt.Errorf("%w: %s", ErrJCSUnsupportedType, v.Type())
	case reflect.Float32, reflect.Float64:
		f := v.Float()
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return fmt.Errorf("%w: %v", ErrJCSInvalidNumber, f)
		}
	case reflect.Map:
		if v.IsNil() {
			return nil
		}
		for _, k := range v.MapKeys() {
			if err := validateValue(k); err != nil {
				return err
			}
			if err := validateValue(v.MapIndex(k)); err != nil {
				return err
			}
		}
	case reflect.Slice:
		if v.IsNil() {
			return nil
		}
		for i := range v.Len() {
			if err := validateValue(v.Index(i)); err != nil {
				return err
			}
		}
	case reflect.Array:
		for i := range v.Len() {
			if err := validateValue(v.Index(i)); err != nil {
				return err
			}
		}
	case reflect.Struct:
		for i := range v.NumField() {
			if err := validateValue(v.Field(i)); err != nil {
				return err
			}
		}
	}
	return nil
}

// normalizeToTree serializes v with the stdlib encoder (preserving struct tags,
// omitempty, etc.), then decodes back into a generic any tree with UseNumber so
// that numeric strings are not rounded or re-typed by float64 parsing.
func normalizeToTree(v any) (any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("jcs: json.Marshal: %w", err)
	}
	var tree any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&tree); err != nil {
		return nil, fmt.Errorf("jcs: json.Decode: %w", err)
	}
	return tree, nil
}

// emitCanonical writes the JCS canonical form of tree to buf.
// tree must be one of: nil, bool, json.Number, string, []any, map[string]any.
func emitCanonical(buf *bytes.Buffer, tree any) error {
	switch v := tree.(type) {
	case nil:
		buf.WriteString("null")
	case bool:
		if v {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case json.Number:
		s, err := canonicalNumber(v)
		if err != nil {
			return err
		}
		buf.WriteString(s)
	case string:
		emitString(buf, v)
	case []any:
		buf.WriteByte('[')
		for i, elem := range v {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := emitCanonical(buf, elem); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sortKeysUTF16(keys)
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			emitString(buf, k)
			buf.WriteByte(':')
			if err := emitCanonical(buf, v[k]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	default:
		return fmt.Errorf("%w: unexpected type %T in canonical tree", ErrJCSUnsupportedType, tree)
	}
	return nil
}

// canonicalNumber converts a json.Number to its JCS canonical string.
//
// Integer-valued tokens (no decimal point or exponent) are emitted without a
// decimal point. Floats use strconv 'g' with -1 precision, which produces the
// shortest IEEE 754 round-trip string and matches ECMAScript Number::toString
// for the range of values that json.Marshal generates (RFC 8785 §3.2.2.3).
func canonicalNumber(n json.Number) (string, error) {
	s := n.String()

	// Fast path: if the token is a plain integer literal, format it directly
	// without a float parse round-trip.
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return strconv.FormatInt(i, 10), nil
	}
	if u, err := strconv.ParseUint(s, 10, 64); err == nil {
		return strconv.FormatUint(u, 10), nil
	}

	// Float path: parse then re-emit as shortest-round-trip decimal.
	// 'g' with prec=-1 drops trailing zeros and omits the decimal point for
	// integer-valued floats, matching ECMAScript output for typical values.
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrJCSInvalidNumber, s)
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return "", fmt.Errorf("%w: %s", ErrJCSInvalidNumber, s)
	}
	return string(strconv.AppendFloat(nil, f, 'g', -1, 64)), nil
}

// emitString writes a JCS-canonical JSON string to buf.
//
// RFC 8785 §3.2.2.2 is stricter than encoding/json: all U+0000–U+001F control
// characters (including \b, \f, \n, \r, \t) must use \uXXXX with lowercase hex
// rather than their short escape sequences.
func emitString(buf *bytes.Buffer, s string) {
	buf.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			buf.WriteString(`\"`)
		case r == '\\':
			buf.WriteString(`\\`)
		case r < 0x20: // U+0000–U+001F: always \uXXXX, never \n \t etc.
			buf.WriteString(`\u`)
			buf.WriteByte('0')
			buf.WriteByte('0')
			buf.WriteByte(hexDigit(byte(r >> 4)))
			buf.WriteByte(hexDigit(byte(r & 0x0f)))
		default:
			buf.WriteRune(r)
		}
	}
	buf.WriteByte('"')
}

// hexDigit returns the lowercase ASCII hex character for n (0–15).
func hexDigit(n byte) byte {
	if n < 10 {
		return '0' + n
	}
	return 'a' + n - 10
}

// sortKeysUTF16 sorts keys in UTF-16 code unit order as required by RFC 8785 §3.2.3.
func sortKeysUTF16(keys []string) {
	sort.Slice(keys, func(i, j int) bool {
		return utf16Less(keys[i], keys[j])
	})
}

// utf16Less returns true if a sorts before b in UTF-16 code unit order.
func utf16Less(a, b string) bool {
	ua := utf16.Encode([]rune(a))
	ub := utf16.Encode([]rune(b))
	for i := 0; i < len(ua) && i < len(ub); i++ {
		if ua[i] != ub[i] {
			return ua[i] < ub[i]
		}
	}
	return len(ua) < len(ub)
}
