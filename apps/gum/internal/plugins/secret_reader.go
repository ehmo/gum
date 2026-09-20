package plugins

import (
	"bufio"
	"errors"
	"io"
	"os"
	"strings"
)

// secretReader reads one secret per prompt from a single stream.
//
// It keeps one bufio.Reader for the whole setup run. A reader built per prompt
// swallows every line behind the first: the first read buffers as much as the
// stream offers, and the next reader starts on what is left, which for a
// two-secret manifest is nothing.
//
// When the stream is the controlling terminal it also turns echo off around
// each read, so the secret reaches neither the screen nor the scrollback.
type secretReader struct {
	buf *bufio.Reader
	// tty is the same stream buf wraps, kept because the echo ioctls need the
	// file descriptor that bufio hides. It is nil for a pipe or a file.
	tty *os.File
}

func newSecretReader(in io.Reader) *secretReader {
	sr := &secretReader{buf: bufio.NewReader(in)}
	if f, ok := in.(*os.File); ok {
		sr.tty = f
	}
	return sr
}

// readLine returns the next line without its trailing newline. It reports
// io.EOF when the stream holds no more input.
//
// echoed says whether the typed characters reached the screen. A caller that
// gets false prints the newline the terminal did not, so the next prompt does
// not land on the same visual line.
func (s *secretReader) readLine() (line string, echoed bool, err error) {
	echoed = true
	if s.tty != nil {
		if restore, derr := disableEcho(s.tty); derr == nil {
			echoed = false
			defer restore()
		}
	}
	text, err := s.buf.ReadString('\n')
	text = strings.TrimRight(text, "\r\n")
	if err != nil && !errors.Is(err, io.EOF) {
		return "", echoed, err
	}
	if text == "" && errors.Is(err, io.EOF) {
		return "", echoed, io.EOF
	}
	return text, echoed, nil
}
