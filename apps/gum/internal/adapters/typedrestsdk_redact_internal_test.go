package adapters

import (
	"errors"
	"net/url"
	"strings"
	"testing"
)

// TestRedactURLError pins the secret-leak fix: a transport error whose URL
// carries an API key (?key=...) must not surface the key, while the underlying
// cause survives and non-url errors pass through unchanged.
func TestRedactURLError(t *testing.T) {
	ue := &url.Error{Op: "Get", URL: "https://maps.googleapis.com/x?key=SECRET-KEY-123", Err: errors.New("dial tcp: i/o timeout")}
	got := redactURLError(ue).Error()
	if strings.Contains(got, "SECRET-KEY-123") {
		t.Errorf("api key leaked into error string: %q", got)
	}
	if !strings.Contains(got, "[redacted]") {
		t.Errorf("want [redacted] URL marker, got %q", got)
	}
	if !strings.Contains(got, "timeout") {
		t.Errorf("underlying cause should survive redaction, got %q", got)
	}
	plain := errors.New("boom")
	if redactURLError(plain) != plain {
		t.Error("non-url error must pass through unchanged")
	}
}
