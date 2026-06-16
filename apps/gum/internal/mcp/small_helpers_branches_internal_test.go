package mcp

import "testing"

// TestJCSCanonicaliseBytesInvalidJSONSurfacesError pins the
// `json.Unmarshal err → return nil, err` arm. Schema resources fed
// through this helper come from on-disk files; a corrupt or partially
// written file must surface as an error, not be silently re-marshaled
// to "null" which would then JCS-canonicalise to an empty schema and
// silently shadow the broken file in the schema resource cache.
func TestJCSCanonicaliseBytesInvalidJSONSurfacesError(t *testing.T) {
	out, err := jcsCanonicaliseBytes([]byte("not { valid } json"))
	if err == nil {
		t.Fatalf("jcsCanonicaliseBytes(garbage)=nil err; want json.SyntaxError surface\nout=%s", out)
	}
	if out != nil {
		t.Errorf("out=%q; want nil on decode-fail", out)
	}
}

// TestMapFromRowNilReceiverReturnsNil pins the `row == nil → return
// nil` arm. The plugin_resource pipeline calls this helper with the
// "lock_row" / "registry_row" of a plugin entry, and either can be
// nil mid-load — the helper MUST tolerate that without panicking on
// row[key] indexing.
func TestMapFromRowNilReceiverReturnsNil(t *testing.T) {
	if got := mapFromRow(nil, "package"); got != nil {
		t.Errorf("mapFromRow(nil, _)=%v; want nil", got)
	}
}

// TestDecodeSchemaRefMalformedPercentReturnsRaw pins the
// `url.PathUnescape err → return raw` arm. A malformed percent
// sequence (e.g. `%ZZ`) must NOT crash the resource resolver — the
// helper returns the raw input so the downstream grammar check trips
// instead, yielding a clean "invalid ref" error to the operator.
func TestDecodeSchemaRefMalformedPercentReturnsRaw(t *testing.T) {
	raw := "schema/%ZZ.json"
	if got := decodeSchemaRef(raw); got != raw {
		t.Errorf("decodeSchemaRef(%q)=%q; want raw passthrough on PathUnescape error", raw, got)
	}
}
