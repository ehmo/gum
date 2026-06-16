package httputil

import (
	"fmt"
	"io"
	"net/http"
	"time"
)

// LimitedTransport wraps an http.RoundTripper and caps each response body at
// MaxBytes. SDK clients that do an unbounded io.ReadAll / json.Decode on
// resp.Body (e.g. googlemaps.github.io/maps, google.golang.org/genai) would
// otherwise let a hostile or compromised upstream OOM the process; this bounds
// the in-memory size and fails the read cleanly once the cap is exceeded.
//
// Use it by wrapping the http.Client handed to the SDK:
//
//	&http.Client{Transport: &httputil.LimitedTransport{Inner: base, MaxBytes: httputil.DefaultMaxResponseBytes}}
type LimitedTransport struct {
	// Inner is the underlying RoundTripper; nil means http.DefaultTransport.
	Inner http.RoundTripper
	// MaxBytes caps each response body; <= 0 means DefaultMaxResponseBytes.
	MaxBytes int64
}

func (t *LimitedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	inner := t.Inner
	if inner == nil {
		inner = http.DefaultTransport
	}
	resp, err := inner.RoundTrip(req)
	if err != nil || resp == nil || resp.Body == nil {
		return resp, err
	}
	max := t.MaxBytes
	if max <= 0 {
		max = DefaultMaxResponseBytes
	}
	resp.Body = &limitedBody{rc: resp.Body, remaining: max}
	return resp, nil
}

// limitedBody is a ReadCloser that returns an error once more than `remaining`
// bytes have been read, rather than silently truncating (a silent truncation
// would hand the SDK a malformed body it might misparse).
type limitedBody struct {
	rc        io.ReadCloser
	remaining int64
}

func (b *limitedBody) Read(p []byte) (int, error) {
	if b.remaining <= 0 {
		return 0, fmt.Errorf("httputil: response body exceeds cap")
	}
	if int64(len(p)) > b.remaining {
		p = p[:b.remaining]
	}
	n, err := b.rc.Read(p)
	b.remaining -= int64(n)
	return n, err
}

func (b *limitedBody) Close() error { return b.rc.Close() }

// CappedClient returns an *http.Client that caps every response body at
// DefaultMaxResponseBytes, preserving base's transport and timeout (or sane
// defaults when base is nil). Hand it to SDKs that read response bodies
// unboundedly (googlemaps / genai) so a hostile upstream can't OOM the process.
func CappedClient(base *http.Client) *http.Client {
	var inner http.RoundTripper
	timeout := 30 * time.Second
	if base != nil {
		inner = base.Transport
		if base.Timeout > 0 {
			timeout = base.Timeout
		}
	}
	return &http.Client{
		Transport: &LimitedTransport{Inner: inner, MaxBytes: DefaultMaxResponseBytes},
		Timeout:   timeout,
	}
}
