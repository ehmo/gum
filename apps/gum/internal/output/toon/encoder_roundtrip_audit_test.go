package toon

import (
	"reflect"
	"testing"
)

// TestDecodeKeyValueQuotedStringLiteral is the audit regression: a map string
// value that the encoder CSV-quotes (a keyword, empty, or special-char string)
// must round-trip as that string, not be mis-decoded. Before the fix
// decodeKeyValue ran the raw quoted text through decodeCSVCell, which never
// unquoted — so `status="null"` decoded to the string `"null"` (literal quotes)
// or, for keywords, the unquote was simply missing.
//
func TestDecodeKeyValueQuotedStringLiteral(t *testing.T) {
	in := map[string]any{
		"status": "null",         // looks like the null keyword
		"flag":   "true",         // looks like a bool
		"off":    "false",        // looks like a bool
		"number": "123",          // looks like a number
		"name":   "ok",           // ordinary bare string
		"empty":  "",             // empty string (must not become null)
		"csv":    "a,b",          // comma forces CSV-quoting
		"quoted": `he said "hi"`, // interior quotes (doubled by encoder)
	}
	enc, err := Encode(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	out, err := Decode(enc)
	if err != nil {
		t.Fatalf("Decode: %v\nencoded:\n%s", err, enc)
	}
	got, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("Decode returned %T; want map", out)
	}
	for k, want := range in {
		if !reflect.DeepEqual(got[k], want) {
			t.Errorf("round-trip %q = %#v (%T); want %#v (string)", k, got[k], got[k], want)
		}
	}
}

func TestDecodeCSVTableQuotedStringLiterals(t *testing.T) {
	enc, err := Encode([]any{map[string]any{
		"status": "null",
		"flag":   "true",
		"number": "123",
		"empty":  "",
	}})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	out, err := Decode(enc)
	if err != nil {
		t.Fatalf("Decode: %v\nencoded:\n%s", err, enc)
	}
	rows, ok := out.([]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("Decode returned %#v; want one row", out)
	}
	row, ok := rows[0].(map[string]any)
	if !ok {
		t.Fatalf("row is %T", rows[0])
	}
	for _, key := range []string{"status", "flag", "number", "empty"} {
		if got, ok := row[key].(string); !ok {
			t.Errorf("%s = %#v (%T), want string", key, row[key], row[key])
		} else if got != map[string]string{"status": "null", "flag": "true", "number": "123", "empty": ""}[key] {
			t.Errorf("%s = %q", key, got)
		}
	}
}
