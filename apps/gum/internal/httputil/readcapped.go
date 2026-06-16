// Package httputil shares HTTP read primitives across the three dispatch
// HTTP fetchers (typed-REST adapter, OAuth token exchange, LRO polling).
// Spec gum-4d66: every io.ReadAll(resp.Body) site must enforce a heap cap
// to defend against runaway / hostile upstreams. The sandbox path keeps its
// own (tighter) 1 MiB cap — see internal/sandbox/risor/sandbox.go.
package httputil

import (
	"errors"
	"fmt"
	"io"
)

// DefaultMaxResponseBytes is the per-call body cap for dispatch HTTP fetchers.
// 64 MiB covers Gmail thread fetches, Calendar bulk lists, and BigQuery row
// pages; legitimate ops are well under 10 MiB. Per-op overrides live in the
// catalog variant metadata under "max_response_bytes" for known-large ops
// (e.g. drive.files.get with alt=media).
const DefaultMaxResponseBytes int64 = 64 << 20

// ErrResponseTooLarge is returned by ReadCapped when the upstream body
// exceeds the cap. Adapters wrap this in a dispatch.StructuredError with
// ErrCodeResponseTooLarge.
var ErrResponseTooLarge = errors.New("RESPONSE_TOO_LARGE: response body exceeds cap")

// ReadCapped reads up to max bytes from r. If r yields more than max bytes
// it returns ErrResponseTooLarge without buffering the overflow. max <= 0
// disables the cap (callers should normally pass DefaultMaxResponseBytes or
// a per-op override).
func ReadCapped(r io.Reader, max int64) ([]byte, error) {
	if max <= 0 {
		return io.ReadAll(r)
	}
	// max+1 sentinel: if io.ReadAll returns max+1 bytes the body was at
	// least max+1, so it exceeded the cap.
	b, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("%w (cap=%d bytes)", ErrResponseTooLarge, max)
	}
	return b, nil
}
