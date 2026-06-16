package config

import (
	"strings"
	"testing"
)

// TestUnquoteValueSingleQuoteCharRejected pins unquoteValue's
// `len(raw) < 2 → unterminated` arm (config.go:194-196). A single-char
// value matching a quote (raw = `"`) is structurally meaningful — both
// HasPrefix and HasSuffix match the same single rune — but it has no
// closing quote. The guard MUST reject rather than slice raw[1:0].
func TestUnquoteValueSingleQuoteCharRejected(t *testing.T) {
	t.Parallel()
	if _, err := unquoteValue(`"`); err == nil || !strings.Contains(err.Error(), "unterminated") {
		t.Fatalf(`unquoteValue("\"") err=%v; want "unterminated quoted value"`, err)
	}
	if _, err := unquoteValue(`'`); err == nil || !strings.Contains(err.Error(), "unterminated") {
		t.Fatalf(`unquoteValue("'") err=%v; want "unterminated quoted value"`, err)
	}
}

// TestParseScannerErrorSurfaces pins parse's
// `sc.Err() != nil → return "config: scan: %w"` arm (config.go:182-184).
// bufio.Scanner returns bufio.ErrTooLong when a single line exceeds
// MaxScanTokenSize (default 64 KiB). A 128 KiB unbroken line trips
// that limit; the wrap MUST carry the "config: scan:" prefix.
func TestParseScannerErrorSurfaces(t *testing.T) {
	t.Parallel()
	// Build a value that's 128 KiB long with no newline so the scanner's
	// single-line buffer overflows before yielding a Scan().
	huge := strings.Repeat("x", 128*1024)
	src := "key = " + huge
	_, _, err := parse("test-profile", src)
	if err == nil {
		t.Fatal("parse(huge-line) err=nil; want bufio.ErrTooLong wrap")
	}
	if !strings.Contains(err.Error(), "config: scan:") {
		t.Errorf("err=%q; want 'config: scan:' prefix", err.Error())
	}
}
