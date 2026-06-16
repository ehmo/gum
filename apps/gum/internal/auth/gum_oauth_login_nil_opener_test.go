package auth_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ehmo/gum/internal/auth"
)

// TestGumOAuthLoginNilBrowserOpenerUsesNoop pins Login's
// `opener := g.BrowserOpener; if opener == nil { opener = func(string) error { return nil } }`
// arm (gum_oauth.go:201-204). Reached when callers omit BrowserOpener
// — the default is a no-op so the loopback callback flow still
// reaches cb.await without panicking on a nil func. The contract is
// "don't crash on nil opener"; this test drives execution past that
// branch and then forces a pre-cancelled context so cb.await exits
// quickly via ctx.Done() rather than the 5-minute timer. If the noop
// assignment regressed (e.g. someone replaced it with a panic or
// removed the nil-guard), Login would NPE before this assertion.
func TestGumOAuthLoginNilBrowserOpenerUsesNoop(t *testing.T) {
	tokSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"access_token":"at-1"}`)
	}))
	t.Cleanup(tokSrv.Close)
	authSrv := newAuthRedirectSrv()
	t.Cleanup(authSrv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	g := &auth.GumOAuth{
		Vault:            auth.NewCredentialVault(errStoreKeyring{}),
		AuthURL:          authSrv.URL,
		TokenURL:         tokSrv.URL,
		HTTPClient:       tokSrv.Client(),
		ManifestBody:     promotedManifestFor("https://example.test/scope/a"),
		ClientIDOverride: "test-client-id",
		// BrowserOpener intentionally nil — triggers the noop default.
	}
	_, err := g.Login(ctx, []string{"https://example.test/scope/a"})
	if err == nil {
		t.Fatal("Login(nil opener, cancelled ctx)=nil err; want cb.await ctx.Err propagation")
	}
	// Must be context.Canceled — proves we passed the noop opener AND
	// reached cb.await. Any other err means a different branch tripped.
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err=%v; want errors.Is(err, context.Canceled) — proves noop opener returned nil and cb.await saw ctx.Done", err)
	}
}
