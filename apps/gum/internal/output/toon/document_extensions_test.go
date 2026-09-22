package toon_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/output/toon"
)

// TestValidExtraHeaderKey pins the key rule that decides between an extra
// header and the JSON fallback. A reserved key would shadow a document field;
// a key carrying a colon or a newline would not survive one header line.
func TestValidExtraHeaderKey(t *testing.T) {
	tests := []struct {
		key  string
		want bool
	}{
		{"kind", true},
		{"resultSizeEstimate", true},
		{"messages_omitted_count", true},
		{"", false},
		{"op", false},
		{"variant", false},
		{"count", false},
		{"fields", false},
		{"format_version", false},
		{"next_page_token", false},
		{"records", false},
		{" kind", false},
		{"kind ", false},
		{"a:b", false},
		{"a\nb", false},
		{"a\rb", false},
	}
	for _, tc := range tests {
		if got := toon.ValidExtraHeaderKey(tc.key); got != tc.want {
			t.Errorf("ValidExtraHeaderKey(%q) = %v, want %v", tc.key, got, tc.want)
		}
	}
}

// TestEncodeDocumentHeaderExtensions pins the byte layout of the two §9.0
// header extensions: records names the array key the rows came from, and each
// extra key carries one scalar sibling as compact JSON.
func TestEncodeDocumentHeaderExtensions(t *testing.T) {
	doc := toon.TOONDocument{
		Op:            "gmail.users.messages.list",
		Variant:       "gmail.v1.rest.users.messages.list",
		FormatVersion: 1,
		Count:         2,
		Fields:        []string{"id", "snippet"},
		RecordKey:     "messages",
		NextPageToken: "tok-2",
		Extra: []toon.DocumentHeader{
			{Key: "kind", Value: "gmail#listMessagesResponse"},
			{Key: "resultSizeEstimate", Value: 2},
		},
		Rows: [][]any{
			{"msg001", "Hello there"},
			{"msg002", ""},
		},
	}

	got, err := toon.EncodeTOONDocument(doc)
	if err != nil {
		t.Fatalf("EncodeTOONDocument: %v", err)
	}

	want := `op: gmail.users.messages.list
variant: gmail.v1.rest.users.messages.list
format_version: 1
count: 2
fields: id,snippet
records: messages
next_page_token: tok-2
kind: "gmail#listMessagesResponse"
resultSizeEstimate: 2

msg001,Hello there
msg002,""
`
	if string(got) != want {
		t.Fatalf("encoded document mismatch:\ngot:\n%s\nwant:\n%s", got, want)
	}

	back, err := toon.DecodeTOONDocument(got)
	if err != nil {
		t.Fatalf("DecodeTOONDocument: %v", err)
	}
	if back.RecordKey != "messages" {
		t.Errorf("RecordKey = %q, want %q", back.RecordKey, "messages")
	}
	if back.NextPageToken != "tok-2" {
		t.Errorf("NextPageToken = %q, want %q", back.NextPageToken, "tok-2")
	}
	wantExtra := []toon.DocumentHeader{
		{Key: "kind", Value: "gmail#listMessagesResponse"},
		{Key: "resultSizeEstimate", Value: float64(2)},
	}
	if !reflect.DeepEqual(back.Extra, wantExtra) {
		t.Errorf("Extra = %#v, want %#v", back.Extra, wantExtra)
	}
}

// TestEncodeDocumentRejectsReservedExtraKey proves the encoder refuses rather
// than shadows: an extra header named count would overwrite the row count on
// the next decode.
func TestEncodeDocumentRejectsReservedExtraKey(t *testing.T) {
	doc := toon.TOONDocument{
		FormatVersion: 1,
		Count:         0,
		Extra:         []toon.DocumentHeader{{Key: "count", Value: 9}},
	}
	if _, err := toon.EncodeTOONDocument(doc); !errors.Is(err, toon.ErrReservedHeaderKey) {
		t.Fatalf("err = %v, want ErrReservedHeaderKey", err)
	}
}

// TestEncodeDocumentExtraValueUnmarshalable covers the marshal failure arm of
// the extra-header loop.
func TestEncodeDocumentExtraValueUnmarshalable(t *testing.T) {
	doc := toon.TOONDocument{
		FormatVersion: 1,
		Count:         0,
		Extra:         []toon.DocumentHeader{{Key: "kind", Value: make(chan int)}},
	}
	_, err := toon.EncodeTOONDocument(doc)
	if err == nil {
		t.Fatal("want an error for a value json.Marshal cannot encode")
	}
	if !errContains(err, `extra header "kind"`) {
		t.Fatalf("err = %v, want it to name the offending key", err)
	}
}

// TestEncodeDocumentCellNonScalar covers §9.4's params_required: a cell whose
// value is an array carries compact JSON inside one CSV string.
func TestEncodeDocumentCellNonScalar(t *testing.T) {
	doc := toon.TOONDocument{
		Op:            "gum.search_apis",
		FormatVersion: 1,
		Count:         1,
		Fields:        []string{"op", "params_required"},
		Rows:          [][]any{{"gmail.users.messages.list", []any{"userId", "q"}}},
	}

	got, err := toon.EncodeTOONDocument(doc)
	if err != nil {
		t.Fatalf("EncodeTOONDocument: %v", err)
	}
	want := `op: gum.search_apis
variant: 
format_version: 1
count: 1
fields: op,params_required

gmail.users.messages.list,"[""userId"",""q""]"
`
	if string(got) != want {
		t.Fatalf("encoded document mismatch:\ngot:\n%s\nwant:\n%s", got, want)
	}

	back, err := toon.DecodeTOONDocument(got)
	if err != nil {
		t.Fatalf("DecodeTOONDocument: %v", err)
	}
	// The cell comes back as JSON text, not as a []any. §9.0 covers this as
	// the lossy case; the test pins it so a future change is deliberate.
	if back.Rows[0][1] != `["userId","q"]` {
		t.Fatalf("cell = %#v, want the JSON text", back.Rows[0][1])
	}
}

// TestEncodeDocumentCellUnmarshalable covers the error arm of the cell JSON
// fallback.
func TestEncodeDocumentCellUnmarshalable(t *testing.T) {
	doc := toon.TOONDocument{
		FormatVersion: 1,
		Count:         1,
		Fields:        []string{"c"},
		Rows:          [][]any{{make(chan int)}},
	}
	_, err := toon.EncodeTOONDocument(doc)
	if err == nil {
		t.Fatal("want an error for a cell json.Marshal cannot encode")
	}
	if !errContains(err, "encode cell") {
		t.Fatalf("err = %v, want it to say which cell failed", err)
	}
}

// TestDecodeDocumentExtraHeaderRawFallback proves a hand-written document
// keeps a bare scalar. A plugin spells kind the way the §9.0 example spells
// op and variant, with no quotes, and json.Unmarshal rejects that.
func TestDecodeDocumentExtraHeaderRawFallback(t *testing.T) {
	src := []byte(`op: plugin.things.list
variant: plugin.v1.things.list
format_version: 1
count: 1
fields: id
records: things
kind: things#list

thing-1
`)
	doc, err := toon.DecodeTOONDocument(src)
	if err != nil {
		t.Fatalf("DecodeTOONDocument: %v", err)
	}
	if doc.RecordKey != "things" {
		t.Errorf("RecordKey = %q, want %q", doc.RecordKey, "things")
	}
	want := []toon.DocumentHeader{{Key: "kind", Value: "things#list"}}
	if !reflect.DeepEqual(doc.Extra, want) {
		t.Fatalf("Extra = %#v, want %#v", doc.Extra, want)
	}
}

func errContains(err error, needle string) bool {
	return err != nil && strings.Contains(err.Error(), needle)
}
