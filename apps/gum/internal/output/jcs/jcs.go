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
	"strings"
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
// Strings: U+0008, U+0009, U+000A, U+000C and U+000D take \b, \t, \n, \f and
// \r; every other code point in U+0000–U+001F takes \uXXXX with lowercase hex;
// " and \ are escaped; all other code points pass through as UTF-8 (§3.2.2.2).
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
// Integer tokens that fit int64 or uint64 are emitted from the token itself, so
// an id above 2^53 keeps every digit. RFC 8785 would route that token through a
// float64 first and emit 18446744073709552000 for MaxUint64; gum keeps the
// exact digits instead, which TestJCSCanonicalLargeUint64 pins and spec §10.0
// records as the one deliberate departure.
//
// Every other value goes through es6Number, which implements the ECMAScript
// Number::toString algorithm that RFC 8785 §3.2.2.3 requires.
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

	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrJCSInvalidNumber, s)
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return "", fmt.Errorf("%w: %s", ErrJCSInvalidNumber, s)
	}

	return es6Number(f), nil
}

// es6Number formats f with the ECMAScript Number::toString algorithm, which
// RFC 8785 §3.2.2.3 adopts by reference. strconv's 'g' verb used to stand in for
// it and disagreed on three whole classes of value: it moved to an exponent at
// 1e7 where ES6 still writes digits, padded the exponent to two digits where
// ES6 pads nothing, and wrote 1e-06 where ES6 writes 0.000001. Hashes computed
// by any other RFC 8785 implementation therefore never matched gum's for those
// values (bead gum-xvy9).
//
// The rule numbers below are the steps of ECMA-262 Number::toString. They are
// stated in terms of s, k and n: s is the shortest decimal digit string that
// round-trips f, k is its length, and n is the position of the decimal point,
// so that f = 0.s × 10^n.
func es6Number(f float64) string {
	if f == 0 {
		return "0" // RFC 8785 §3.2.2.3 serializes negative zero as 0.
	}
	if f < 0 {
		return "-" + es6Number(-f)
	}

	// Go's shortest 'e' form carries both quantities already: "d.dddde±XX"
	// holds the digits of s around a point that can be deleted, and its
	// exponent is n-1.
	return es6FromShortest(strconv.FormatFloat(f, 'e', -1, 64))
}

// es6FromShortest applies rules 1 to 5 to Go's shortest 'e' form of a positive
// finite float. It is split out from es6Number so the malformed-input fallback
// is reachable from a test: every float64 es6Number can pass in produces the
// "d.dddde±XX" shape, so the fallback exists only to keep the function total if
// a future toolchain changes that format.
func es6FromShortest(shortest string) string {
	mantissa, exponent, found := strings.Cut(shortest, "e")
	if !found {
		return shortest
	}
	digits := strings.Replace(mantissa, ".", "", 1)
	exp, err := strconv.Atoi(exponent)
	if err != nil {
		return shortest
	}

	k := len(digits)
	n := exp + 1

	switch {
	case k <= n && n <= 21: // rule 1: digits, then the zeros that follow them
		return digits + strings.Repeat("0", n-k)
	case 0 < n && n <= 21: // rule 2: the point falls inside the digits
		return digits[:n] + "." + digits[n:]
	case -6 < n && n <= 0: // rule 3: a leading zero, then -n zeros
		return "0." + strings.Repeat("0", -n) + digits
	}

	// Rules 4 and 5: an exponent, signed and never zero-padded. Rule 4 is the
	// single-digit case, which carries no decimal point.
	sign := "+"
	e := n - 1
	if e < 0 {
		sign = "-"
		e = -e
	}
	if k == 1 {
		return digits + "e" + sign + strconv.Itoa(e)
	}

	return digits[:1] + "." + digits[1:] + "e" + sign + strconv.Itoa(e)
}

// shortEscapes are the five control characters RFC 8785 §3.2.2.2 requires to be
// written as a two-character escape. Every other code point below U+0020 takes
// the \uXXXX form. The set is closed: U+000B has no short escape in JSON and so
// is written \u000b, even though ECMAScript source accepts \v.
//
// emitString used to write \u0008 \u0009 \u000a \u000c \u000d for these five,
// and its doc comment asserted that RFC 8785 demanded it. The rule reads the
// other way, and three of the six upstream vectors caught it (bead gum-gq9q).
var shortEscapes = map[rune]string{
	'\b': `\b`, // U+0008
	'\t': `\t`, // U+0009
	'\n': `\n`, // U+000A
	'\f': `\f`, // U+000C
	'\r': `\r`, // U+000D
}

// emitString writes a JCS-canonical JSON string to buf.
//
// RFC 8785 §3.2.2.2: a code point in U+0000–U+001F takes \uXXXX with lowercase
// hex unless it is one of the five in shortEscapes, which take \b, \t, \n, \f or
// \r. U+0022 and U+005C take \" and \\. Everything else is written as-is in
// UTF-8, including code points encoding/json would escape for HTML safety.
func emitString(buf *bytes.Buffer, s string) {
	buf.WriteByte('"')
	for _, r := range s {
		if esc, ok := shortEscapes[r]; ok {
			buf.WriteString(esc)

			continue
		}

		switch {
		case r == '"':
			buf.WriteString(`\"`)
		case r == '\\':
			buf.WriteString(`\\`)
		case r < 0x20:
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
