// Package risor_test — Phase 5.3 full Risor builtins tests (gum-fii.6.5).
//
// All tests in this file are FAILING until the green team implements the builtins
// in sandbox.go. They exercise spec.md §6.3.
package risor_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/sandbox/risor"
)

// TestGumCallBuiltinInvoked verifies that injecting a Go function as gum_call
// and calling it from a script results in the function being invoked.
func TestGumCallBuiltinInvoked(t *testing.T) {
	defer goleak.VerifyNone(t)

	var called atomic.Bool
	opts := risor.Options{
		Globals: map[string]any{
			"gum_call": func(opID any, args any) any {
				called.Store(true)
				return nil
			},
		},
	}

	_, err := risor.Run(context.Background(), `gum_call("op.id", {})`, opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !called.Load() {
		t.Error("gum_call Go function was not invoked by the script")
	}
}

// TestGumSearchBuiltinInvoked verifies that injecting a Go function as gum_search
// and calling it from a script results in the function being invoked.
func TestGumSearchBuiltinInvoked(t *testing.T) {
	defer goleak.VerifyNone(t)

	var called atomic.Bool
	opts := risor.Options{
		Globals: map[string]any{
			"gum_search": func(query any, topK any) any {
				called.Store(true)
				return nil
			},
		},
	}

	_, err := risor.Run(context.Background(), `gum_search("query", 5)`, opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !called.Load() {
		t.Error("gum_search Go function was not invoked by the script")
	}
}

// TestGumConfirmDestructiveBuiltinInvoked verifies that injecting a Go function
// as gum_confirm_destructive and calling it from a script results in the function
// being invoked.
func TestGumConfirmDestructiveBuiltinInvoked(t *testing.T) {
	defer goleak.VerifyNone(t)

	var called atomic.Bool
	opts := risor.Options{
		Globals: map[string]any{
			"gum_confirm_destructive": func(opID any, args any, purpose any) any {
				called.Store(true)
				return nil
			},
		},
	}

	_, err := risor.Run(context.Background(), `gum_confirm_destructive("op.id", {}, "delete")`, opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !called.Load() {
		t.Error("gum_confirm_destructive Go function was not invoked by the script")
	}
}

// TestGumCallNotWiredErrors verifies that calling gum_call without injection
// produces an error (the default stub panics, which the sandbox must surface as
// an error, NOT let escape the process).
func TestGumCallNotWiredErrors(t *testing.T) {
	defer goleak.VerifyNone(t)

	opts := risor.Options{} // no gum_call injection

	_, err := risor.Run(context.Background(), `gum_call("op.id", {})`, opts)
	if err == nil {
		t.Fatal("expected error when calling gum_call without injection, got nil")
	}
}

// TestGumHTTPGetHTTPSOnly verifies that gum_http_get("http://...") returns an
// error mentioning "https" (sandbox enforces HTTPS-only by default).
func TestGumHTTPGetHTTPSOnly(t *testing.T) {
	defer goleak.VerifyNone(t)

	opts := risor.Options{} // no AllowInsecureHTTP

	_, err := risor.Run(context.Background(), `gum_http_get("http://example.com/")`, opts)
	if err == nil {
		t.Fatal("expected error for http:// URL, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "https") {
		t.Errorf("error %q should mention 'https'", err.Error())
	}
}

// TestGumHTTPGetTimeout verifies that gum_http_get respects Options.HTTPTimeout.
// A slow server (200ms delay) with a short timeout (50ms) must produce a timeout
// error mentioning "deadline" or "timeout".
func TestGumHTTPGetTimeout(t *testing.T) {
	// Slow server: always sleeps 200ms before responding.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	// Close server before goleak fires so listener goroutine is gone.
	defer func() {
		srv.Close()
		goleak.VerifyNone(t)
	}()

	opts := risor.Options{
		HTTPTimeout:        50 * time.Millisecond,
		AllowInsecureHTTP:  true,                  // httptest uses http://
		AllowedHosts:       []string{"127.0.0.1"}, // httptest listens on 127.0.0.1
		AllowPrivateEgress: true,                  // reach the loopback test server past the SSRF guard
	}

	script := `gum_http_get("` + srv.URL + `/")`
	_, err := risor.Run(context.Background(), script, opts)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "deadline") && !strings.Contains(msg, "timeout") {
		t.Errorf("error %q should mention 'deadline' or 'timeout'", err.Error())
	}
}

// TestGumAllowWriteFlagVisible verifies that gum_allow_write is visible as a
// bool global in the Risor script, reflecting Options.AllowWrite.
func TestGumAllowWriteFlagVisible(t *testing.T) {
	defer goleak.VerifyNone(t)

	for _, allowWrite := range []bool{true, false} {
		allowWrite := allowWrite
		t.Run(map[bool]string{true: "true", false: "false"}[allowWrite], func(t *testing.T) {
			defer goleak.VerifyNone(t)

			opts := risor.Options{
				AllowWrite: allowWrite,
			}
			// Script reads the global and prints it via gum_print.
			// gum_print calls fmt.Sprint which renders bools as "true"/"false".
			out, err := risor.Run(context.Background(), `gum_print(gum_allow_write)`, opts)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}

			want := map[bool]string{true: "true", false: "false"}[allowWrite]
			got := strings.TrimSpace(string(out.Printed))
			if got != want {
				t.Errorf("gum_allow_write: script printed %q; want %q", got, want)
			}
		})
	}
}

// TestGumAllowDestructiveFlagVisible verifies that gum_allow_destructive is
// visible as a bool global in the Risor script, reflecting Options.AllowDestructive.
func TestGumAllowDestructiveFlagVisible(t *testing.T) {
	defer goleak.VerifyNone(t)

	for _, allowDestructive := range []bool{true, false} {
		allowDestructive := allowDestructive
		t.Run(map[bool]string{true: "true", false: "false"}[allowDestructive], func(t *testing.T) {
			defer goleak.VerifyNone(t)

			opts := risor.Options{
				AllowDestructive: allowDestructive,
			}
			out, err := risor.Run(context.Background(), `gum_print(gum_allow_destructive)`, opts)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}

			want := map[bool]string{true: "true", false: "false"}[allowDestructive]
			got := strings.TrimSpace(string(out.Printed))
			if got != want {
				t.Errorf("gum_allow_destructive: script printed %q; want %q", got, want)
			}
		})
	}
}
