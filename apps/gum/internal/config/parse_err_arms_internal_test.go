package config

import (
	"errors"
	"strings"
	"testing"
)

// TestParseCommentLineIgnored pins parse's `strings.HasPrefix(line, "#")`
// short-circuit (config.go:140-141). Comment lines MUST be skipped
// without triggering the missing-equals err — otherwise users can't
// annotate their config.toml.
func TestParseCommentLineIgnored(t *testing.T) {
	src := "# this is a comment\nconfig_schema_version = 1\n"
	c, _, err := parse("default", src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if c.SchemaVersion != 1 {
		t.Errorf("SchemaVersion=%d; want 1", c.SchemaVersion)
	}
}

// TestParseLineMissingEqualsReturnsError pins parse's
// `idx < 0 → 'expected key = value'` arm (config.go:144-146). A line
// without '=' is malformed; surfacing line N lets users fix the file
// rather than getting silent acceptance.
func TestParseLineMissingEqualsReturnsError(t *testing.T) {
	src := "not-a-key-value-pair\n"
	_, _, err := parse("default", src)
	if err == nil {
		t.Fatalf("parse(no-equals) err=nil; want missing-equals err")
	}
	if !strings.Contains(err.Error(), "expected key = value") {
		t.Errorf("err=%v; want 'expected key = value' message", err)
	}
}

// TestParseEmptyKeyReturnsError pins parse's `key == "" → 'empty key'`
// arm (config.go:149-151). A line like "= value" has no key — reject
// upfront rather than storing under an empty key.
func TestParseEmptyKeyReturnsError(t *testing.T) {
	src := "= dangling-value\n"
	_, _, err := parse("default", src)
	if err == nil {
		t.Fatalf("parse(empty key) err=nil; want empty-key err")
	}
	if !strings.Contains(err.Error(), "empty key") {
		t.Errorf("err=%v; want 'empty key' message", err)
	}
}

// TestParseBadSchemaVersionReturnsError pins parse's
// `Sscanf err → 'config_schema_version must be an integer'` arm
// (config.go:156-158). A non-integer schema version means the file is
// corrupted; reject so migration logic doesn't read a bogus version.
func TestParseBadSchemaVersionReturnsError(t *testing.T) {
	src := "config_schema_version = not-a-number\n"
	_, _, err := parse("default", src)
	if err == nil {
		t.Fatalf("parse(bad schema_version) err=nil; want integer err")
	}
	if !strings.Contains(err.Error(), "must be an integer") {
		t.Errorf("err=%v; want 'must be an integer' message", err)
	}
}

// TestParseFutureSchemaVersionReturnsErrSchemaUnsupported pins parse's
// `v > CurrentSchemaVersion → ErrSchemaUnsupported` arm. Surfacing the
// typed error lets the CLI prompt the user to upgrade rather than
// silently accepting a future-format file.
func TestParseFutureSchemaVersionReturnsErrSchemaUnsupported(t *testing.T) {
	src := "config_schema_version = 999\n"
	_, _, err := parse("default", src)
	if err == nil {
		t.Fatalf("parse(future version) err=nil; want ErrSchemaUnsupported")
	}
	var schemaErr *ErrSchemaUnsupported
	if !errors.As(err, &schemaErr) {
		t.Errorf("err type=%T; want *ErrSchemaUnsupported", err)
	}
}

// TestParseUnquoteValueErrorWrapsWithLine pins parse's
// `unquoteValue err → "config: line N: %w"` wrap arm (config.go:168-170).
// An empty value (no characters after '=') triggers unquoteValue's
// "empty value" err; the wrap names the line for debuggability.
func TestParseUnquoteValueErrorWrapsWithLine(t *testing.T) {
	src := "output.profile.default =\n" // empty value after =
	_, _, err := parse("default", src)
	if err == nil {
		t.Fatalf("parse(empty value) err=nil; want unquoteValue err")
	}
	if !strings.Contains(err.Error(), "line 1") {
		t.Errorf("err=%v; want 'line 1' in wrap", err)
	}
	if !strings.Contains(err.Error(), "empty value") {
		t.Errorf("err=%v; want underlying 'empty value' chained", err)
	}
}
