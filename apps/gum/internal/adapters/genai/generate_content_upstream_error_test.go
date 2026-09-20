package genai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
)

// TestGenerateContentUpstreamErrorWraps pins genai.go:107-109 — the
// `client.Models.GenerateContent err` arm. An upstream rejection must carry
// the `genai.models.generate_content` prefix, otherwise a Gemini 400 reaches
// the operator as a bare SDK string with no hint of which adapter produced
// it.
func TestGenerateContentUpstreamErrorWraps(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":400,"message":"model not found","status":"INVALID_ARGUMENT"}}`))
	}))
	defer srv.Close()

	adapter := &Adapter{BaseURL: srv.URL, HTTPClient: srv.Client()}
	rv := &dispatch.ResolvedVariant{
		Variant: &catalog.Variant{
			BackendKind: catalog.BackendKindGenAI,
			Binding:     &catalog.Binding{AdapterKey: "genai.models.generate_content"},
		},
		AdapterKey: "genai.models.generate_content",
	}
	inv := &dispatch.Invocation{
		OpID: "gemini.models.generate_content",
		Args: map[string]any{"model": "gemini-2.0-flash", "prompt": "Say hello."},
	}

	_, err := adapter.Execute(context.Background(), inv, rv, &dispatch.Credentials{APIKey: "AIza-fake-genai-key"})
	if err == nil {
		t.Fatal("Execute against a 400 upstream: nil err")
	}
	if !strings.Contains(err.Error(), "genai.models.generate_content:") {
		t.Errorf("err=%q; want a 'genai.models.generate_content:' prefix", err)
	}
	if !strings.Contains(err.Error(), "model not found") {
		t.Errorf("err=%q; want the upstream message preserved", err)
	}
}
