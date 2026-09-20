package plugins

import (
	"errors"
	"io"
	"strings"
	"testing"
)

// TestSecretReaderLineSemantics pins what one prompt consumes. The old
// bufio.Scanner per prompt is gone, so these cases now run against a shared
// bufio.Reader and must keep the same line handling: one line per call, the
// trailing newline stripped, a CRLF line stripped of both bytes, a final line
// without a newline still returned, and a spent stream reported as io.EOF so
// promptAndStore can say "no input provided".
func TestSecretReaderLineSemantics(t *testing.T) {
	t.Parallel()
	sr := newSecretReader(strings.NewReader("first\r\nsecond\n\nlast-no-newline"))

	for _, want := range []string{"first", "second", "", "last-no-newline"} {
		got, echoed, err := sr.readLine()
		if err != nil {
			t.Fatalf("readLine(want %q): %v", want, err)
		}
		if got != want {
			t.Errorf("readLine()=%q; want %q", got, want)
		}
		if !echoed {
			t.Error("echoed=false for a strings.Reader; only a terminal suppresses echo")
		}
	}

	if _, _, err := sr.readLine(); !errors.Is(err, io.EOF) {
		t.Errorf("readLine(spent) err=%v; want io.EOF", err)
	}
}

// errReader fails every read with a sentinel so the non-EOF arm of readLine
// is reachable without a real broken file descriptor.
type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }

// TestSecretReaderPropagatesReadError proves a broken stream surfaces as
// itself, not as "no input provided". The two cases send the operator to
// different places: one is a typo, the other is a broken pipe.
func TestSecretReaderPropagatesReadError(t *testing.T) {
	t.Parallel()
	boom := errors.New("pipe closed")
	_, _, err := newSecretReader(errReader{err: boom}).readLine()
	if !errors.Is(err, boom) {
		t.Fatalf("readLine err=%v; want wraps %v", err, boom)
	}
}
