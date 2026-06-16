package genai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
)

// TestBackendKindGenAI pins spec §14 line 3334: the gen-ai backend kind is
// dispatchable via internal/adapters/genai using google.golang.org/genai.
// We stand up an httptest server that returns a canned generateContent
// response, point the SDK at it via Adapter.BaseURL + HTTPClient, and
// verify the executor returns a 200 JSON Response carrying the model's
// candidates.
func TestBackendKindGenAI(t *testing.T) {
	const cannedResponse = `{
		"candidates": [
			{
				"content": {
					"role": "model",
					"parts": [{"text": "Hello, world!"}]
				},
				"finishReason": "STOP",
				"index": 0
			}
		],
		"usageMetadata": {
			"promptTokenCount": 5,
			"candidatesTokenCount": 3,
			"totalTokenCount": 8
		},
		"modelVersion": "gemini-2.0-flash"
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Sanity-check the SDK actually reached the generateContent
		// path. The v1beta path is `/v1beta/models/<model>:generateContent`.
		if !strings.Contains(r.URL.Path, "generateContent") {
			http.Error(w, "wrong path: "+r.URL.Path, http.StatusBadRequest)
			return
		}
		// The SDK should forward the API key via the
		// x-goog-api-key header (preferred) or ?key= query arg.
		hasKey := r.Header.Get("x-goog-api-key") != "" || r.URL.Query().Get("key") != ""
		if !hasKey {
			http.Error(w, "missing key", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, cannedResponse)
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
		Args: map[string]any{
			"model":  "gemini-2.0-flash",
			"prompt": "Say hello.",
		},
	}
	creds := &dispatch.Credentials{APIKey: "AIza-fake-genai-key"}

	resp, err := adapter.Execute(context.Background(), inv, rv, creds)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d; want 200", resp.StatusCode)
	}
	if resp.Format != "json" {
		t.Errorf("Format = %q; want json", resp.Format)
	}
	var got map[string]any
	if err := json.Unmarshal(resp.Body, &got); err != nil {
		t.Fatalf("response is not JSON: %v\n%s", err, resp.Body)
	}
	candidates, _ := got["candidates"].([]any)
	if len(candidates) != 1 {
		t.Fatalf("candidates len = %d; want 1\nbody=%s", len(candidates), resp.Body)
	}
}

// TestGenAIAdapterRequiresAPIKey verifies that empty Credentials.APIKey
// surfaces a typed error rather than silently calling the SDK with no
// credentials (which would hit the live network).
func TestGenAIAdapterRequiresAPIKey(t *testing.T) {
	adapter := NewAdapter()
	_, err := adapter.Execute(context.Background(),
		&dispatch.Invocation{Args: map[string]any{"model": "gemini-2.0-flash", "prompt": "hi"}},
		&dispatch.ResolvedVariant{Variant: &catalog.Variant{Binding: &catalog.Binding{AdapterKey: "genai.models.generate_content"}}},
		&dispatch.Credentials{},
	)
	if err == nil {
		t.Fatal("expected error for missing APIKey, got nil")
	}
	if !strings.Contains(err.Error(), "API key") {
		t.Errorf("error = %q; want hint about API key", err.Error())
	}
}

// TestGenAIAdapterUnsupportedOp verifies the closed-op invariant — an
// unknown genai.<op> in the binding fails fast with a typed error so
// catalog drift can't silently call a wrong SDK method.
func TestGenAIAdapterUnsupportedOp(t *testing.T) {
	adapter := NewAdapter()
	_, err := adapter.Execute(context.Background(),
		&dispatch.Invocation{},
		&dispatch.ResolvedVariant{Variant: &catalog.Variant{Binding: &catalog.Binding{AdapterKey: "genai.live.stream"}}},
		&dispatch.Credentials{APIKey: "AIza-fake"},
	)
	if err == nil {
		t.Fatal("expected error for unsupported adapter_key, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported adapter_key") {
		t.Errorf("error = %q; want `unsupported adapter_key` hint", err.Error())
	}
}
