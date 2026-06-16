// Spec gum-4d66: typed-REST executor MUST cap response body size. A
// 128 MiB upstream payload MUST surface as RESPONSE_TOO_LARGE without
// allocating proportional heap.
//
// TDD red (gum-46uq): TypedRestSDK currently calls io.ReadAll(resp.Body)
// with no cap (typedrestsdk.go:201). This test fails until gum-4d66 lands.

package adapters_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/adapters"
	"github.com/ehmo/gum/internal/dispatch"
)

// TestTypedRestSDKResponseBodyCapped serves a 128 MiB payload and asserts
// the executor returns a RESPONSE_TOO_LARGE-shaped error without doubling
// the process heap.
func TestTypedRestSDKResponseBodyCapped(t *testing.T) {
	verifyNoLeaks(t)

	// Pick a target larger than any realistic Google API response. 128 MiB is
	// the canonical "oversized" probe in the spec test matrix; the executor
	// must reject it before it ever lands in memory.
	const bodySize = 128 << 20

	// Baseline heap. Force a GC so the measurement isn't polluted by prior
	// allocations.
	runtime.GC()
	var pre runtime.MemStats
	runtime.ReadMemStats(&pre)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		buf := make([]byte, 1<<20) // stream 1 MiB at a time so the *server* doesn't allocate 128 MiB
		for i := 0; i < bodySize>>20; i++ {
			if _, err := w.Write(buf); err != nil {
				return
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}))
	t.Cleanup(srv.Close)

	executor := adapters.NewTypedRestSDK()
	inv, rv := makeTestInvAndVariant(srv.URL)

	_, err := executor.Execute(context.Background(), inv, rv, nil)
	if err == nil {
		t.Fatal("executor returned nil error for 128 MiB upstream body; want RESPONSE_TOO_LARGE")
	}
	// Accept either a wrapped StructuredError with ErrCodeResponseTooLarge or
	// an error whose message contains "RESPONSE_TOO_LARGE" — both shapes are
	// compatible with the spec §1421 envelope.
	var se *dispatch.StructuredError
	if (!errors.As(err, &se) || string(se.ErrCode) != "RESPONSE_TOO_LARGE") &&
		!strings.Contains(err.Error(), "RESPONSE_TOO_LARGE") {
		t.Errorf("executor err = %v; want RESPONSE_TOO_LARGE", err)
	}

	// Heap must not have grown by ~128 MiB. Allow generous slack (32 MiB) for
	// transient allocations from net/http, TLS, etc.
	runtime.GC()
	var post runtime.MemStats
	runtime.ReadMemStats(&post)
	diff := int64(post.HeapAlloc) - int64(pre.HeapAlloc)
	if diff > 32<<20 {
		t.Errorf("heap grew by %d bytes after capped 128 MiB read; want < 32 MiB — likely no cap is being enforced", diff)
	}
}
