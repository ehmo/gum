package profile_test

import (
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/output/profile"
)

// TestParseLineMissingEqualsSurfacesError pins Parse's `idx < 0 →
// "expected key = value"` arm (parser.go:337-339). A line that's
// neither blank, comment, nor section header, and has no '=', is a
// syntax error — without this guard a malformed line would silently
// skip and downstream code would see a missing key.
func TestParseLineMissingEqualsSurfacesError(t *testing.T) {
	_, err := profile.Parse("garbage-no-equals")
	if err == nil {
		t.Fatal("Parse(no equals)=nil err; want syntax error")
	}
	if !strings.Contains(err.Error(), "expected key = value") {
		t.Errorf("err=%q; want 'expected key = value'", err)
	}
	if !strings.Contains(err.Error(), "line 1") {
		t.Errorf("err=%q; want line number 1", err)
	}
}

// TestParseUnknownSectionHeaderSurfacesError pins Parse's
// `unknown [section] → error` arm (parser.go:333). Only [[tests]]
// is allowed; [other] should fail with a descriptive err so users
// know the section is unsupported rather than silently ignored.
func TestParseUnknownSectionHeaderSurfacesError(t *testing.T) {
	_, err := profile.Parse("[unknown_section]")
	if err == nil {
		t.Fatal("Parse([unknown])=nil err; want unknown-section error")
	}
	if !strings.Contains(err.Error(), "unknown section header") {
		t.Errorf("err=%q; want 'unknown section header'", err)
	}
}

// TestParseDefaultFormatInvalidValueSurfacesError pins Parse's
// `!validFormats[val] → invalid value` arm (parser.go:356-358).
// default_format must be one of toon|json|raw — any other quoted
// string fails so users get an immediate error rather than silently
// falling back to the zero value.
func TestParseDefaultFormatInvalidValueSurfacesError(t *testing.T) {
	_, err := profile.Parse(`default_format = "xml"`)
	if err == nil {
		t.Fatal("Parse(default_format=xml)=nil err; want invalid-format error")
	}
	if !strings.Contains(err.Error(), "default_format") {
		t.Errorf("err=%q; want 'default_format'", err)
	}
	if !strings.Contains(err.Error(), "must be toon, json, or raw") {
		t.Errorf("err=%q; want enum hint", err)
	}
}

// TestParseProjectionParseErrSurfacesWrap pins Parse's
// `parseStringArray err → "projection: ..." wrap` arm
// (parser.go:362-364). A malformed array (e.g., unbalanced brackets)
// fails parseStringArray; the wrap surfaces the failing key so
// users can locate the bad line.
func TestParseProjectionParseErrSurfacesWrap(t *testing.T) {
	_, err := profile.Parse(`projection = not-an-array`)
	if err == nil {
		t.Fatal("Parse(projection bad)=nil err; want parse-array err wrap")
	}
	if !strings.Contains(err.Error(), "projection") {
		t.Errorf("err=%q; want 'projection' key wrap", err)
	}
}

// TestParseFlattenSingletonsParseErrSurfacesWrap pins Parse's
// `parseBool err → "flatten_singletons: ..." wrap` arm
// (parser.go:368-370). A non-bool value fails parseBool; the wrap
// names the key.
func TestParseFlattenSingletonsParseErrSurfacesWrap(t *testing.T) {
	_, err := profile.Parse(`flatten_singletons = maybe`)
	if err == nil {
		t.Fatal("Parse(flatten_singletons=maybe)=nil err; want parse-bool err wrap")
	}
	if !strings.Contains(err.Error(), "flatten_singletons") {
		t.Errorf("err=%q; want 'flatten_singletons' key wrap", err)
	}
}

// TestParseOmitZeroCountsParseErrSurfacesWrap pins Parse's
// `parseBool err → "omit_zero_counts: ..." wrap` arm
// (parser.go:374-376). Same as flatten_singletons but for the
// omit_zero_counts key — distinct branch, distinct wrap.
func TestParseOmitZeroCountsParseErrSurfacesWrap(t *testing.T) {
	_, err := profile.Parse(`omit_zero_counts = yes-please`)
	if err == nil {
		t.Fatal("Parse(omit_zero_counts=yes-please)=nil err; want parse-bool err wrap")
	}
	if !strings.Contains(err.Error(), "omit_zero_counts") {
		t.Errorf("err=%q; want 'omit_zero_counts' key wrap", err)
	}
}

// TestParseSortByParseErrSurfacesWrap pins Parse's `parseStringLiteral
// err → "sort_by: ..." wrap` arm (parser.go:380-382). An unquoted
// value fails parseStringLiteral; the wrap names the key.
func TestParseSortByParseErrSurfacesWrap(t *testing.T) {
	_, err := profile.Parse(`sort_by = unquoted`)
	if err == nil {
		t.Fatal("Parse(sort_by unquoted)=nil err; want parse-string err wrap")
	}
	if !strings.Contains(err.Error(), "sort_by") {
		t.Errorf("err=%q; want 'sort_by' key wrap", err)
	}
}

// TestParseLimitParseErrSurfacesWrap pins Parse's `parseInt err →
// "limit: ..." wrap` arm (parser.go:386-388). Non-integer value
// fails parseInt; wrap names the key.
func TestParseLimitParseErrSurfacesWrap(t *testing.T) {
	_, err := profile.Parse(`limit = not-an-int`)
	if err == nil {
		t.Fatal("Parse(limit=not-an-int)=nil err; want parse-int err wrap")
	}
	if !strings.Contains(err.Error(), "limit") {
		t.Errorf("err=%q; want 'limit' key wrap", err)
	}
}

// TestParseLimitNegativeSurfacesError pins Parse's `limit < 0 →
// "must be >= 0"` arm (parser.go:389-391). limit is a count; a
// negative value is semantically invalid. parseInt succeeds (it's
// a valid int) but the range check rejects it.
func TestParseLimitNegativeSurfacesError(t *testing.T) {
	_, err := profile.Parse(`limit = -1`)
	if err == nil {
		t.Fatal("Parse(limit=-1)=nil err; want negative-limit error")
	}
	if !strings.Contains(err.Error(), "must be >= 0") {
		t.Errorf("err=%q; want '>= 0' range hint", err)
	}
}

// TestParseFieldMaskModeInvalidValueSurfacesError pins Parse's
// `!validFieldMaskModes[val]` arm (parser.go:398-400). field_mask_mode
// must be upstream|dual_fetch|none; other quoted strings fail with
// the enum-listing err so operators see the valid options.
func TestParseFieldMaskModeInvalidValueSurfacesError(t *testing.T) {
	_, err := profile.Parse(`field_mask_mode = "client_only"`)
	if err == nil {
		t.Fatal("Parse(field_mask_mode=client_only)=nil err; want invalid-mode error")
	}
	if !strings.Contains(err.Error(), "field_mask_mode") {
		t.Errorf("err=%q; want 'field_mask_mode' key wrap", err)
	}
	if !strings.Contains(err.Error(), "must be upstream, dual_fetch, or none") {
		t.Errorf("err=%q; want enum hint", err)
	}
}

// TestParseStringLiteralInvalidEscapeSurfacesError pins
// parseStringLiteral's `json.Unmarshal err → "invalid string literal"`
// arm (parser.go:417-419). A quoted string with an unterminated
// escape sequence passes the outer quote-pair check but fails
// json.Unmarshal — surfaces as a clear wrap so users see "what" not
// "where in json".
func TestParseStringLiteralInvalidEscapeSurfacesError(t *testing.T) {
	// "\x" is an invalid JSON escape — passes len>=2 + quote-pair
	// check but json.Unmarshal rejects.
	_, err := profile.Parse(`default_format = "\x"`)
	if err == nil {
		t.Fatal("Parse(default_format with bad escape)=nil err; want invalid-string err")
	}
	if !strings.Contains(err.Error(), "invalid string literal") {
		t.Errorf("err=%q; want 'invalid string literal' wrap", err)
	}
}
