package risor_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/sandbox/risor"
)

// TestGumSearchNotWiredErrors pins the default stub for gum_search. A script
// that calls it without a host-provided implementation must fail, not silently
// receive nil and carry on with an empty search result.
func TestGumSearchNotWiredErrors(t *testing.T) {
	defer goleak.VerifyNone(t)

	_, err := risor.Run(context.Background(), `gum_search("query", 5)`, risor.Options{})
	if err == nil {
		t.Fatal("Run err = nil; want the unwired gum_search stub to fail")
	}
}

// TestGumConfirmDestructiveNotWiredErrors pins the default stub for
// gum_confirm_destructive. The failure mode this guards against is the worst
// one in the package: an unwired confirmation that returns nil would read as
// "not confirmed" to some callers and as "no error" to others.
func TestGumConfirmDestructiveNotWiredErrors(t *testing.T) {
	defer goleak.VerifyNone(t)

	_, err := risor.Run(context.Background(), `gum_confirm_destructive("op.id", {}, "delete")`, risor.Options{})
	if err == nil {
		t.Fatal("Run err = nil; want the unwired gum_confirm_destructive stub to fail")
	}
}

// TestGumPrintRejectsWrongArgCount pins the arity check. gum_print takes one
// value; two would make the printed stream ambiguous about where one printed
// value ends and the next begins.
func TestGumPrintRejectsWrongArgCount(t *testing.T) {
	defer goleak.VerifyNone(t)

	for _, source := range []string{`gum_print()`, `gum_print(1, 2)`} {
		t.Run(source, func(t *testing.T) {
			_, err := risor.Run(context.Background(), source, risor.Options{})
			if err == nil {
				t.Fatalf("Run(%s) err = nil; want the arity error", source)
			}
			if !strings.Contains(err.Error(), "want 1 argument") {
				t.Errorf("Run(%s) err = %v; want the arity error", source, err)
			}
		})
	}
}

// TestGumPrintChainsToTheCallerHook pins the chain-call contract: the sandbox
// always captures the printed bytes itself, and a caller-supplied gum_print
// still runs for its side effects. Both accepted signatures are covered,
// because a caller wiring the wrong one would otherwise get silence.
func TestGumPrintChainsToTheCallerHook(t *testing.T) {
	t.Run("func_any_any", func(t *testing.T) {
		defer goleak.VerifyNone(t)

		var seen []any
		opts := risor.Options{Globals: map[string]any{
			"gum_print": func(v any) any {
				seen = append(seen, v)
				return nil
			},
		}}
		out, err := risor.Run(context.Background(), `gum_print({"a": 1})`, opts)
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if got := string(out.Printed); got != `{"a":1}` {
			t.Errorf("Printed = %q; want the JSON form", got)
		}
		if len(seen) != 1 {
			t.Fatalf("caller hook saw %d values; want 1", len(seen))
		}
		m, ok := seen[0].(map[string]any)
		if !ok || m["a"] != 1 {
			t.Errorf("caller hook saw %#v; want map[a:1] with a Go int", seen[0])
		}
	})

	t.Run("func_string", func(t *testing.T) {
		defer goleak.VerifyNone(t)

		var seen []string
		opts := risor.Options{Globals: map[string]any{
			"gum_print": func(s string) { seen = append(seen, s) },
		}}
		out, err := risor.Run(context.Background(), `gum_print("hi")`, opts)
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if got := string(out.Printed); got != "hi" {
			t.Errorf("Printed = %q; want %q", got, "hi")
		}
		if len(seen) != 1 || seen[0] != "hi" {
			t.Errorf("caller hook saw %#v; want [\"hi\"]", seen)
		}
	})
}

// TestGumHTTPGetRejectsAnUnparseableURL pins the url.Parse arm. It has to fail
// before the scheme and allowlist gates, because a URL the parser rejects has
// no trustworthy host to check the allowlist against.
func TestGumHTTPGetRejectsAnUnparseableURL(t *testing.T) {
	defer goleak.VerifyNone(t)

	// A raw control character is rejected by net/url.
	_, err := risor.Run(context.Background(), "gum_http_get(\"https://exa\\x7fmple.com/\")", risor.Options{})
	if err == nil {
		t.Fatal("Run err = nil; want the parse failure")
	}
	if !strings.Contains(err.Error(), "parse URL") {
		t.Errorf("Run err = %v; want it wrapped as \"parse URL\"", err)
	}
}

// TestGumHTTPGetFollowsARedirectToAnAllowlistedHost is the allow side of the
// redirect gate. The deny side is covered elsewhere; without this one, a
// CheckRedirect that refused everything would look identical in the tests.
func TestGumHTTPGetFollowsARedirectToAnAllowlistedHost(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"arrived":true}`))
	}))
	start := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/final", http.StatusFound)
	}))
	defer func() {
		start.Close()
		target.Close()
		goleak.VerifyNone(t)
	}()

	opts := risor.Options{
		AllowInsecureHTTP:  true,
		AllowedHosts:       []string{"127.0.0.1"},
		AllowPrivateEgress: true,
	}
	out, err := risor.Run(context.Background(), `gum_http_get("`+start.URL+`/")`, opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	m, ok := out.Value.(map[string]any)
	if !ok {
		t.Fatalf("Value = %#v; want a response map", out.Value)
	}
	if body, _ := m["body"].(string); !strings.Contains(body, "arrived") {
		t.Errorf("body = %q; want the redirect target's body", body)
	}
}

// rawHTTPListener serves one canned byte stream per connection, then hangs up.
// It exists so a test can produce a response that http.ReadResponse accepts but
// whose body cannot be read to completion.
func rawHTTPListener(t *testing.T, response []byte) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	var wg sync.WaitGroup
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = c.Write(response)
				_ = c.Close()
			}()
		}
	}()
	stop := func() {
		_ = ln.Close()
		<-done
		wg.Wait()
	}
	return ln.Addr().String(), stop
}

// TestGumHTTPGetSurfacesABodyReadFailure pins the io.ReadAll arm. The headers
// promise 100 bytes and the peer sends 5, so a silent truncation here would
// hand the script a short body that looks complete.
func TestGumHTTPGetSurfacesABodyReadFailure(t *testing.T) {
	addr, stop := rawHTTPListener(t, []byte("HTTP/1.1 200 OK\r\nContent-Length: 100\r\n\r\nshort"))
	defer func() {
		stop()
		goleak.VerifyNone(t)
	}()

	opts := risor.Options{
		AllowInsecureHTTP:  true,
		AllowedHosts:       []string{"127.0.0.1"},
		AllowPrivateEgress: true,
	}
	_, err := risor.Run(context.Background(), `gum_http_get("http://`+addr+`/")`, opts)
	if err == nil {
		t.Fatal("Run err = nil; want the truncated-body failure")
	}
	if !strings.Contains(err.Error(), "read body") {
		t.Errorf("Run err = %v; want it wrapped as \"read body\"", err)
	}
}

// TestGumHTTPGetRejectsAnOversizedBody pins the 1 MiB response cap. The read is
// limited to cap+1 bytes, so the check is that one extra byte is treated as
// "too large" instead of being dropped.
func TestGumHTTPGetRejectsAnOversizedBody(t *testing.T) {
	const maxBody = 1 << 20
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(maxBody+1))
		_, _ = w.Write(make([]byte, maxBody+1))
	}))
	defer func() {
		srv.Close()
		goleak.VerifyNone(t)
	}()

	opts := risor.Options{
		AllowInsecureHTTP:  true,
		AllowedHosts:       []string{"127.0.0.1"},
		AllowPrivateEgress: true,
	}
	_, err := risor.Run(context.Background(), `gum_http_get("`+srv.URL+`/")`, opts)
	if err == nil {
		t.Fatal("Run err = nil; want the 1 MiB cap to reject the body")
	}
	if !strings.Contains(err.Error(), "exceeds 1 MiB limit") {
		t.Errorf("Run err = %v; want the 1 MiB cap error", err)
	}
}
