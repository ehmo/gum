package httputil

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

// TestReadCapped covers:
//   - Body under cap → returned verbatim.
//   - Body equal to cap → returned verbatim (boundary case).
//   - Body over cap → ErrResponseTooLarge with the cap value in the message.
//   - max <= 0 → cap disabled, full body returned.
//   - Reader error → surfaced unwrapped (we don't mask transport errors).
func TestReadCapped(t *testing.T) {
	t.Run("under_cap", func(t *testing.T) {
		body := []byte("hello world")
		got, err := ReadCapped(bytes.NewReader(body), 1024)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if !bytes.Equal(got, body) {
			t.Errorf("got %q, want %q", got, body)
		}
	})

	t.Run("equal_to_cap", func(t *testing.T) {
		body := bytes.Repeat([]byte("x"), 10)
		got, err := ReadCapped(bytes.NewReader(body), 10)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if len(got) != 10 {
			t.Errorf("len = %d, want 10", len(got))
		}
	})

	t.Run("over_cap_returns_RESPONSE_TOO_LARGE", func(t *testing.T) {
		body := bytes.Repeat([]byte("x"), 100)
		_, err := ReadCapped(bytes.NewReader(body), 10)
		if err == nil {
			t.Fatal("err = nil, want ErrResponseTooLarge")
		}
		if !errors.Is(err, ErrResponseTooLarge) {
			t.Errorf("errors.Is(err, ErrResponseTooLarge) = false; err=%v", err)
		}
		if !strings.Contains(err.Error(), "cap=10") {
			t.Errorf("err message = %q, want cap=10 reference", err)
		}
	})

	t.Run("zero_cap_disables_limit", func(t *testing.T) {
		body := bytes.Repeat([]byte("y"), 5000)
		got, err := ReadCapped(bytes.NewReader(body), 0)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if len(got) != 5000 {
			t.Errorf("len = %d, want 5000 (no cap)", len(got))
		}
	})

	t.Run("negative_cap_disables_limit", func(t *testing.T) {
		body := []byte("anything")
		got, err := ReadCapped(bytes.NewReader(body), -1)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if !bytes.Equal(got, body) {
			t.Errorf("got %q, want %q", got, body)
		}
	})

	t.Run("reader_error_surfaces", func(t *testing.T) {
		sentinel := errors.New("transport boom")
		r := &errReader{err: sentinel}
		_, err := ReadCapped(r, 100)
		if !errors.Is(err, sentinel) {
			t.Errorf("err = %v, want wrap of sentinel", err)
		}
	})
}

type errReader struct{ err error }

func (e *errReader) Read([]byte) (int, error) { return 0, e.err }

var _ io.Reader = (*errReader)(nil)
