// Package toon — failing red-team tests for gum-np38.2 typed-null decode.
//
// These tests reference symbols that do not yet exist (Schema, FieldSchema,
// FieldType, TypedDocument, DecodeTypedDocument). They are intentionally
// non-compilable until the green phase provides the implementation.
//
// Spec anchor: §9.0 lossless-reconstruction clause —
//
//	"empty field + string type → null string;
//	 empty field + numeric type → null numeric;
//	 "" + string type → empty string."
//
// Consumers without schema MUST treat empty fields as null (not empty string).
package toon

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func testdataPath(name string) string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "testdata", name)
}

func mustReadFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(testdataPath(name))
	if err != nil {
		t.Fatalf("mustReadFixture %s: %v", name, err)
	}
	return data
}

// buildTypedNullsDoc builds a minimal §9.0 document with two columns: a
// string column and a string column where the first cell is unquoted-empty
// (null) and the second is quoted-empty (empty string).
//
//	fields: name, alias
//	row 0: ,""        — name=null, alias=""
func twoColumnNullVsEmptyDoc() []byte {
	return []byte(
		"op: test.op\n" +
			"variant: v1\n" +
			"format_version: 1\n" +
			"count: 1\n" +
			"fields: name,alias\n" +
			"\n" +
			`,""` + "\n",
	)
}

// buildNumericNullDoc returns a §9.0 document with one integer column that
// is null (unquoted-empty).
func buildNumericNullDoc() []byte {
	return []byte(
		"op: test.op\n" +
			"variant: v1\n" +
			"format_version: 1\n" +
			"count: 1\n" +
			"fields: score\n" +
			"\n" +
			"\n", // unquoted empty cell → null
	)
}

// buildFloatNullDoc returns a §9.0 document with one float column that is null.
func buildFloatNullDoc() []byte {
	return []byte(
		"op: test.op\n" +
			"variant: v1\n" +
			"format_version: 1\n" +
			"count: 1\n" +
			"fields: ratio\n" +
			"\n" +
			"\n",
	)
}

// buildBoolNullDoc returns a §9.0 document with one bool column that is null.
func buildBoolNullDoc() []byte {
	return []byte(
		"op: test.op\n" +
			"variant: v1\n" +
			"format_version: 1\n" +
			"count: 1\n" +
			"fields: active\n" +
			"\n" +
			"\n",
	)
}

// buildAllPresentDoc returns a §9.0 document with one row of all non-null cells.
func buildAllPresentDoc() []byte {
	return []byte(
		"op: test.op\n" +
			"variant: v1\n" +
			"format_version: 1\n" +
			"count: 1\n" +
			"fields: name,score,ratio,active\n" +
			"\n" +
			`alice,42,3.14,true` + "\n",
	)
}

// ---------------------------------------------------------------------------
// Test 1 — §9.0 lossless: null string vs empty string
// ---------------------------------------------------------------------------

// TestDecodeTypedStringNullVsEmpty asserts that for a two-string-column row
// where cell[0] is unquoted-empty (null) and cell[1] is quoted-empty (""),
// DecodeTypedDocument with a string schema produces:
//   - cell[0]: (*string)(nil)   — typed null pointer
//   - cell[1]: *string("")      — non-nil pointer to empty string
//
// It also asserts that the Go types are exactly *string in both cases, not
// untyped nil vs string. This exercises the §9.0 lossless reconstruction
// requirement: "null string" and "empty string" must be distinguishable after
// decode when the field type is known.
func TestDecodeTypedStringNullVsEmpty(t *testing.T) {
	schema := Schema{
		Fields: []FieldSchema{
			{Name: "name", Type: FieldTypeString},
			{Name: "alias", Type: FieldTypeString},
		},
	}
	doc, err := DecodeTypedDocument(twoColumnNullVsEmptyDoc(), schema)
	if err != nil {
		t.Fatalf("DecodeTypedDocument: %v", err)
	}
	if len(doc.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(doc.Rows))
	}
	row := doc.Rows[0]
	if len(row) != 2 {
		t.Fatalf("expected 2 cells, got %d", len(row))
	}

	// cell[0]: null string — must be a (*string)(nil), not untyped nil.
	cell0 := row[0]
	if cell0 != nil {
		// It might be a typed (*string)(nil). Check via reflect.
		rv := reflect.ValueOf(cell0)
		if rv.Kind() != reflect.Pointer {
			t.Errorf("cell[0] (null string): expected *string or (*string)(nil), got %T (%v)", cell0, cell0)
		} else if rv.Type() != reflect.TypeOf((*string)(nil)) {
			t.Errorf("cell[0] (null string): expected type *string, got %v", rv.Type())
		} else if !rv.IsNil() {
			t.Errorf("cell[0] (null string): expected nil pointer, got %v", rv.Interface())
		}
	} else {
		// Untyped nil is accepted only if the test is running before the green
		// phase converts it; however, the spec requires typed null. Report this
		// as a failure to document the expectation.
		t.Errorf("cell[0] (null string): got untyped nil; want (*string)(nil) — spec §9.0 lossless clause requires typed-null pointer")
	}

	// cell[1]: empty string — must be a *string pointing to "".
	cell1 := row[1]
	if cell1 == nil {
		t.Fatalf("cell[1] (empty string): got nil; want *string(\"\")")
	}
	rv1 := reflect.ValueOf(cell1)
	if rv1.Kind() != reflect.Pointer {
		t.Errorf("cell[1] (empty string): expected *string, got %T", cell1)
	} else if rv1.Type() != reflect.TypeOf((*string)(nil)) {
		t.Errorf("cell[1] (empty string): expected type *string, got %v", rv1.Type())
	} else if rv1.IsNil() {
		t.Errorf("cell[1] (empty string): pointer is nil; want pointer to \"\"")
	} else {
		got := rv1.Elem().String()
		if got != "" {
			t.Errorf("cell[1] (empty string): *string value = %q; want %q", got, "")
		}
	}
}

// ---------------------------------------------------------------------------
// Test 2 — §9.0 lossless: null numeric (int)
// ---------------------------------------------------------------------------

// TestDecodeTypedNumericNull asserts that an empty cell in an integer-typed
// column decodes to (*int64)(nil), NOT to int64(0) or untyped nil.
// Spec §9.0: "Null numeric: empty field (same wire form as null string)."
func TestDecodeTypedNumericNull(t *testing.T) {
	schema := Schema{
		Fields: []FieldSchema{
			{Name: "score", Type: FieldTypeInt},
		},
	}
	doc, err := DecodeTypedDocument(buildNumericNullDoc(), schema)
	if err != nil {
		t.Fatalf("DecodeTypedDocument: %v", err)
	}
	if len(doc.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(doc.Rows))
	}
	cell := doc.Rows[0][0]

	// Must not be zero int64.
	if v, ok := cell.(int64); ok {
		t.Errorf("cell (null int): got int64(%d); want (*int64)(nil) — zero is ambiguous with null", v)
		return
	}

	// Must be a typed (*int64)(nil) pointer (not untyped nil).
	if cell == nil {
		t.Errorf("cell (null int): got untyped nil; want (*int64)(nil)")
		return
	}
	rv := reflect.ValueOf(cell)
	if rv.Type() != reflect.TypeOf((*int64)(nil)) {
		t.Errorf("cell (null int): type = %v; want *int64", rv.Type())
	}
	if !rv.IsNil() {
		t.Errorf("cell (null int): pointer not nil; want nil (*int64)")
	}
}

// ---------------------------------------------------------------------------
// Test 3 — §9.0 lossless: null float
// ---------------------------------------------------------------------------

// TestDecodeTypedFloatNull asserts that an empty cell in a float-typed column
// decodes to (*float64)(nil), NOT to float64(0) or untyped nil.
func TestDecodeTypedFloatNull(t *testing.T) {
	schema := Schema{
		Fields: []FieldSchema{
			{Name: "ratio", Type: FieldTypeFloat},
		},
	}
	doc, err := DecodeTypedDocument(buildFloatNullDoc(), schema)
	if err != nil {
		t.Fatalf("DecodeTypedDocument: %v", err)
	}
	if len(doc.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(doc.Rows))
	}
	cell := doc.Rows[0][0]

	if v, ok := cell.(float64); ok {
		t.Errorf("cell (null float): got float64(%v); want (*float64)(nil)", v)
		return
	}
	if cell == nil {
		t.Errorf("cell (null float): got untyped nil; want (*float64)(nil)")
		return
	}
	rv := reflect.ValueOf(cell)
	if rv.Type() != reflect.TypeOf((*float64)(nil)) {
		t.Errorf("cell (null float): type = %v; want *float64", rv.Type())
	}
	if !rv.IsNil() {
		t.Errorf("cell (null float): pointer not nil; want nil (*float64)")
	}
}

// ---------------------------------------------------------------------------
// Test 4 — §9.0 lossless: null bool
// ---------------------------------------------------------------------------

// TestDecodeTypedBoolNull asserts that an empty cell in a bool-typed column
// decodes to (*bool)(nil), NOT to false or untyped nil.
func TestDecodeTypedBoolNull(t *testing.T) {
	schema := Schema{
		Fields: []FieldSchema{
			{Name: "active", Type: FieldTypeBool},
		},
	}
	doc, err := DecodeTypedDocument(buildBoolNullDoc(), schema)
	if err != nil {
		t.Fatalf("DecodeTypedDocument: %v", err)
	}
	if len(doc.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(doc.Rows))
	}
	cell := doc.Rows[0][0]

	if v, ok := cell.(bool); ok {
		t.Errorf("cell (null bool): got bool(%v); want (*bool)(nil)", v)
		return
	}
	if cell == nil {
		t.Errorf("cell (null bool): got untyped nil; want (*bool)(nil)")
		return
	}
	rv := reflect.ValueOf(cell)
	if rv.Type() != reflect.TypeOf((*bool)(nil)) {
		t.Errorf("cell (null bool): type = %v; want *bool", rv.Type())
	}
	if !rv.IsNil() {
		t.Errorf("cell (null bool): pointer not nil; want nil (*bool)")
	}
}

// ---------------------------------------------------------------------------
// Test 5 — all present: non-null typed cells
// ---------------------------------------------------------------------------

// TestDecodeTypedAllPresent asserts that when no cells are null, every cell in
// a row is a non-nil typed pointer (or the non-nil typed value itself — the
// green phase chooses). We use reflect.TypeOf to avoid depending on the exact
// boxing strategy, so this test compiles against either *T or T value cells.
func TestDecodeTypedAllPresent(t *testing.T) {
	schema := Schema{
		Fields: []FieldSchema{
			{Name: "name", Type: FieldTypeString},
			{Name: "score", Type: FieldTypeInt},
			{Name: "ratio", Type: FieldTypeFloat},
			{Name: "active", Type: FieldTypeBool},
		},
	}
	doc, err := DecodeTypedDocument(buildAllPresentDoc(), schema)
	if err != nil {
		t.Fatalf("DecodeTypedDocument: %v", err)
	}
	if len(doc.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(doc.Rows))
	}
	row := doc.Rows[0]
	if len(row) != 4 {
		t.Fatalf("expected 4 cells, got %d", len(row))
	}

	for i, cell := range row {
		if cell == nil {
			t.Errorf("cell[%d]: got nil; want non-nil typed value", i)
			continue
		}
		rv := reflect.ValueOf(cell)
		// If it's a pointer, it must not be nil.
		if rv.Kind() == reflect.Pointer && rv.IsNil() {
			t.Errorf("cell[%d]: got nil pointer of type %v; want non-nil", i, rv.Type())
		}
	}

	// Spot-check expected values regardless of boxing (dereference pointers).
	checkStringCell := func(idx int, want string) {
		t.Helper()
		cell := row[idx]
		rv := reflect.ValueOf(cell)
		var got string
		switch rv.Kind() {
		case reflect.String:
			got = rv.String()
		case reflect.Pointer:
			if rv.Elem().Kind() == reflect.String {
				got = rv.Elem().String()
			} else {
				t.Errorf("cell[%d]: unexpected ptr-to-%v", idx, rv.Elem().Kind())
				return
			}
		default:
			t.Errorf("cell[%d]: unexpected kind %v for string field", idx, rv.Kind())
			return
		}
		if got != want {
			t.Errorf("cell[%d]: got %q; want %q", idx, got, want)
		}
	}
	checkStringCell(0, "alice")
}

// ---------------------------------------------------------------------------
// Test 6 — round-trip with typed-nulls fixture
// ---------------------------------------------------------------------------

// TestDecodeTypedRoundTripRespectsFixtureTypes decodes the typed-nulls.toon
// fixture with a schema that marks "id" and "snippet" as strings and
// "size_estimate" as int, then re-encodes and compares with the original bytes.
//
// The fixture has:
//   - row 0: id=msg001, snippet=null (unquoted-empty), size_estimate=42
//   - row 1: id=msg002, snippet="" (quoted-empty), size_estimate=null (unquoted-empty)
//   - row 2: id=msg003, snippet=hello, size_estimate=7
//
// This exercises the lossless round-trip clause: after typed decode, a
// re-encode MUST reproduce the same wire bytes (null cells stay null; empty
// string cells stay quoted-empty).
func TestDecodeTypedRoundTripRespectsFixtureTypes(t *testing.T) {
	fixtureData := mustReadFixture(t, "typed-nulls.toon")

	schema := Schema{
		Fields: []FieldSchema{
			{Name: "id", Type: FieldTypeString},
			{Name: "snippet", Type: FieldTypeString},
			{Name: "size_estimate", Type: FieldTypeInt},
		},
	}

	typedDoc, err := DecodeTypedDocument(fixtureData, schema)
	if err != nil {
		t.Fatalf("DecodeTypedDocument: %v", err)
	}

	// Convert TypedDocument back to TOONDocument for re-encoding.
	// The green phase must provide a method or conversion to enable this.
	// We call the conversion method TypedDocumentToTOON (expected by green).
	toonDoc, err := typedDoc.ToTOONDocument()
	if err != nil {
		t.Fatalf("TypedDocument.ToTOONDocument: %v", err)
	}

	reEncoded, err := EncodeTOONDocument(*toonDoc)
	if err != nil {
		t.Fatalf("EncodeTOONDocument: %v", err)
	}

	if !bytes.Equal(fixtureData, reEncoded) {
		t.Errorf("round-trip mismatch\noriginal:\n%s\nre-encoded:\n%s",
			fixtureData, reEncoded)
	}
}

// ---------------------------------------------------------------------------
// Test 7 — schema mismatch returns error containing "schema"
// ---------------------------------------------------------------------------

// TestDecodeTypedSchemaMismatchErrors asserts that when the schema declares N
// fields but the document header "fields:" lists a different number, Decode
// returns a non-nil error whose message contains the word "schema".
//
// Spec §9.0 lossless-reconstruction: the schema must align with the document
// fields; a mismatch is an unrecoverable decode error.
func TestDecodeTypedSchemaMismatchErrors(t *testing.T) {
	// Document declares 3 fields: id, snippet, subject.
	docData := []byte(
		"op: test.op\n" +
			"variant: v1\n" +
			"format_version: 1\n" +
			"count: 1\n" +
			"fields: id,snippet,subject\n" +
			"\n" +
			"msg001,,Hello\n",
	)

	// Schema declares 4 fields — mismatch.
	schema := Schema{
		Fields: []FieldSchema{
			{Name: "id", Type: FieldTypeString},
			{Name: "snippet", Type: FieldTypeString},
			{Name: "subject", Type: FieldTypeString},
			{Name: "extra", Type: FieldTypeString}, // one too many
		},
	}

	_, err := DecodeTypedDocument(docData, schema)
	if err == nil {
		t.Fatal("expected non-nil error for schema/fields count mismatch, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "schema") {
		t.Errorf("error %q does not contain the word \"schema\"", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Test 8 — no schema: empty cells treated as untyped nil (spec default)
// ---------------------------------------------------------------------------

// TestDecodeTypedWithoutSchemaTreatsEmptyAsNull calls DecodeTypedDocument with
// an empty Schema{} (no declared field types). Empty cells must yield untyped
// nil — the existing default behaviour described in spec §9.0:
// "Consumers without schema access MUST treat empty fields as null (not empty
// string)."
//
// This documents the "lossy but safe" consumer path.
func TestDecodeTypedWithoutSchemaTreatsEmptyAsNull(t *testing.T) {
	docData := []byte(
		"op: test.op\n" +
			"variant: v1\n" +
			"format_version: 1\n" +
			"count: 1\n" +
			"fields: id,snippet\n" +
			"\n" +
			"msg001,\n", // snippet cell is unquoted-empty
	)

	doc, err := DecodeTypedDocument(docData, Schema{})
	if err != nil {
		t.Fatalf("DecodeTypedDocument (no schema): %v", err)
	}
	if len(doc.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(doc.Rows))
	}
	row := doc.Rows[0]
	if len(row) < 2 {
		t.Fatalf("expected at least 2 cells, got %d", len(row))
	}

	// snippet cell (index 1) must be nil — not "" and not a pointer.
	snippetCell := row[1]
	if snippetCell != nil {
		t.Errorf("cell[1] (no schema, empty field): got %T(%v); want untyped nil", snippetCell, snippetCell)
	}
}
