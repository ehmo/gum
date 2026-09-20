package adapters_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ehmo/gum/internal/adapters"
	"github.com/ehmo/gum/internal/dispatch"
)

// TestUpstreamErrorCarriesRawBody proves the adapter error satisfies
// dispatch.UpstreamBodyCarrier and hands back the verbatim non-2xx body. The
// tee_mode = "failures" artifact records exactly those bytes, so an error type
// that dropped the body would reduce the artifact to gum's own envelope.
func TestUpstreamErrorCarriesRawBody(t *testing.T) {
	const body = `{"error":{"code":403,"status":"PERMISSION_DENIED","message":"caller lacks scope"}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)

	ex := adapters.NewTypedRestSDK()
	ex.AllowCredentialHostForTest(srv.URL)
	inv, rv := makeTestInvAndVariant(srv.URL)

	_, err := ex.Execute(context.Background(), inv, rv, &dispatch.Credentials{Token: "fake-bearer-token"})
	if err == nil {
		t.Fatal("Execute returned nil error; want the 403")
	}

	var carrier dispatch.UpstreamBodyCarrier
	if !errors.As(err, &carrier) {
		t.Fatalf("err %T does not implement dispatch.UpstreamBodyCarrier", err)
	}
	if got := string(carrier.UpstreamBody()); got != body {
		t.Errorf("UpstreamBody() = %q; want %q", got, body)
	}
}
