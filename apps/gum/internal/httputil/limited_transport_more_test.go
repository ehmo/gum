package httputil

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// rtFunc adapts a function to http.RoundTripper.
type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func mkResp(body string) *http.Response {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}
}

// TestRoundTripPassesThroughErrorAndNilBody covers the early-return arms:
// inner error, nil response, and nil body must not be wrapped.
func TestRoundTripPassesThroughErrorAndNilBody(t *testing.T) {
	wantErr := errors.New("dial fail")
	lt := &LimitedTransport{Inner: rtFunc(func(*http.Request) (*http.Response, error) {
		return nil, wantErr
	})}
	if _, err := lt.RoundTrip(&http.Request{}); !errors.Is(err, wantErr) {
		t.Errorf("err = %v; want %v", err, wantErr)
	}

	ltNilBody := &LimitedTransport{Inner: rtFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 204, Body: nil}, nil
	})}
	resp, err := ltNilBody.RoundTrip(&http.Request{})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if resp.Body != nil {
		t.Error("nil body should be left nil, not wrapped")
	}
}

// TestRoundTripCapsBody covers the wrap + over-cap error path.
func TestRoundTripCapsBody(t *testing.T) {
	lt := &LimitedTransport{
		MaxBytes: 4,
		Inner: rtFunc(func(*http.Request) (*http.Response, error) {
			return mkResp("0123456789"), nil
		}),
	}
	resp, err := lt.RoundTrip(&http.Request{})
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	got, err := io.ReadAll(resp.Body)
	if err == nil {
		t.Errorf("expected over-cap error reading body; got %q", got)
	}
	if int64(len(got)) > 4 {
		t.Errorf("read %d bytes; want <= cap 4", len(got))
	}
	_ = resp.Body.Close()
}

// TestRoundTripDefaultMaxBytes covers the MaxBytes<=0 → DefaultMaxResponseBytes
// branch (a body under the default cap reads through unchanged).
func TestRoundTripDefaultMaxBytes(t *testing.T) {
	lt := &LimitedTransport{ // MaxBytes left 0 → default
		Inner: rtFunc(func(*http.Request) (*http.Response, error) { return mkResp("small"), nil }),
	}
	resp, err := lt.RoundTrip(&http.Request{})
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	got, err := io.ReadAll(resp.Body)
	if err != nil || string(got) != "small" {
		t.Errorf("read = %q, err=%v; want small body under default cap", got, err)
	}
	_ = resp.Body.Close()
}

// TestCappedClientDefaultsAndPreserves covers the nil-base default-timeout path
// and the base-with-custom-timeout path.
func TestCappedClientDefaultsAndPreserves(t *testing.T) {
	def := CappedClient(nil)
	if def.Timeout != 30*time.Second {
		t.Errorf("nil base timeout = %v; want 30s", def.Timeout)
	}
	if _, ok := def.Transport.(*LimitedTransport); !ok {
		t.Errorf("transport = %T; want *LimitedTransport", def.Transport)
	}

	base := &http.Client{Timeout: 5 * time.Second, Transport: rtFunc(func(*http.Request) (*http.Response, error) { return mkResp("x"), nil })}
	got := CappedClient(base)
	if got.Timeout != 5*time.Second {
		t.Errorf("preserved timeout = %v; want 5s", got.Timeout)
	}
	lt, ok := got.Transport.(*LimitedTransport)
	if !ok || lt.Inner == nil {
		t.Errorf("expected wrapped inner transport; got %#v", got.Transport)
	}
}
