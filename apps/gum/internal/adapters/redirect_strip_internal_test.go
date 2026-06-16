package adapters

import (
	"net/http"
	"testing"
)

// TestStripCredsOnCrossHostRedirect pins the API-key leak guard (review
// gum-t8x1): on a same-host redirect the key is preserved; on a cross-host
// redirect the ?key= query param and X-Goog-Api-Key header are stripped; and
// redirect chains are bounded.
func TestStripCredsOnCrossHostRedirect(t *testing.T) {
	mkReq := func(rawurl string) *http.Request {
		r, err := http.NewRequest(http.MethodGet, rawurl, nil)
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		r.Header.Set("X-Goog-Api-Key", "SECRET")
		return r
	}
	origin := mkReq("https://www.googleapis.com/gmail/v1/users/me?key=SECRET")

	t.Run("same host keeps key", func(t *testing.T) {
		req := mkReq("https://www.googleapis.com/other?key=SECRET")
		if err := stripCredsOnCrossHostRedirect(req, []*http.Request{origin}); err != nil {
			t.Fatalf("err: %v", err)
		}
		if req.URL.Query().Get("key") != "SECRET" {
			t.Error("same-host redirect should keep the key")
		}
		if req.Header.Get("X-Goog-Api-Key") == "" {
			t.Error("same-host redirect should keep the api-key header")
		}
	})

	t.Run("cross host strips key", func(t *testing.T) {
		req := mkReq("https://evil.example.com/grab?key=SECRET")
		if err := stripCredsOnCrossHostRedirect(req, []*http.Request{origin}); err != nil {
			t.Fatalf("err: %v", err)
		}
		if req.URL.Query().Has("key") {
			t.Errorf("cross-host redirect must strip ?key=; got %q", req.URL.RawQuery)
		}
		if req.Header.Get("X-Goog-Api-Key") != "" {
			t.Error("cross-host redirect must strip the X-Goog-Api-Key header")
		}
	})

	t.Run("too many redirects", func(t *testing.T) {
		via := make([]*http.Request, 10)
		for i := range via {
			via[i] = origin
		}
		if err := stripCredsOnCrossHostRedirect(mkReq("https://x/y"), via); err == nil {
			t.Error("expected error after 10 redirects")
		}
	})
}
