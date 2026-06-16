// Package genai is the backend executor for catalog variants with
// backend_kind="gen-ai" (spec §14 line 3334). It wraps
// google.golang.org/genai so the dispatcher can call Gemini's
// generateContent (and follow-on Live / Caches / Files operations) without
// going through the discovery-REST or raw-HTTP paths — generativelanguage
// is NOT in the discovery doc set per docs/research/deep-research/01.
//
// v0.1.0 wires the `genai.models.generate_content` adapter_key as the
// canary surface; follow-ons (Live, Caches, embeddings) add new cases to
// the switch in Execute.
package genai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	gg "google.golang.org/genai"

	"github.com/ehmo/gum/internal/dispatch"
	"github.com/ehmo/gum/internal/httputil"
)

// Adapter executes Gemini API calls for catalog variants whose
// binding.adapter_key matches `genai.<service>.<method>`.
type Adapter struct {
	// HTTPClient is forwarded into ClientConfig.HTTPClient. Tests inject
	// an httptest.Server-backed client; production leaves it nil so the
	// SDK builds its own.
	HTTPClient *http.Client
	// BaseURL, when non-empty, overrides the Gemini API base URL via
	// HTTPOptions.BaseURL. Required for offline tests; production leaves
	// it empty so the SDK targets generativelanguage.googleapis.com.
	BaseURL string
}

// NewAdapter constructs a genai adapter with production defaults.
func NewAdapter() *Adapter { return &Adapter{} }

// Execute is the dispatch.Adapter entry point. It pulls the API key from
// creds.APIKey (Gemini auth is api_key per spec §7), builds a genai
// Client, then routes by binding.adapter_key.
func (a *Adapter) Execute(ctx context.Context, inv *dispatch.Invocation, rv *dispatch.ResolvedVariant, creds *dispatch.Credentials) (*dispatch.Response, error) {
	if creds == nil || creds.APIKey == "" {
		return nil, errors.New("genai adapter: missing API key (run `gum auth use-api-key`)")
	}
	cfg := &gg.ClientConfig{
		APIKey:  creds.APIKey,
		Backend: gg.BackendGeminiAPI,
		// Response-size-capped client: the genai SDK reads bodies with an
		// unbounded io.ReadAll, so cap them to avoid an upstream OOM.
		HTTPClient: httputil.CappedClient(a.HTTPClient),
	}
	if a.BaseURL != "" {
		cfg.HTTPOptions = gg.HTTPOptions{BaseURL: a.BaseURL}
	}
	client, err := gg.NewClient(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("genai adapter: NewClient: %w", err)
	}

	switch genaiOp(rv) {
	case "models.generate_content":
		return executeGenerateContent(ctx, client, inv)
	default:
		return nil, fmt.Errorf("genai adapter: unsupported adapter_key %q (v0.1.0 wires `genai.models.generate_content` only)", binding(rv))
	}
}

func binding(rv *dispatch.ResolvedVariant) string {
	if rv == nil || rv.Variant == nil || rv.Variant.Binding == nil {
		return ""
	}
	return rv.Variant.Binding.AdapterKey
}

// genaiOp extracts the operation discriminator from `genai.<service>.<method>`.
func genaiOp(rv *dispatch.ResolvedVariant) string {
	key := binding(rv)
	const prefix = "genai."
	if len(key) > len(prefix) && key[:len(prefix)] == prefix {
		return key[len(prefix):]
	}
	return ""
}

// executeGenerateContent marshals inv.Args into a Gemini generateContent
// call. v0.1.0 expects:
//   - args["model"]: string, e.g. "gemini-2.0-flash"
//   - args["prompt"]: string, single-turn user prompt
//
// The full Content/Part shape is delegated to the catalog generator for
// follow-on releases; this minimal arg surface is what the canary needs.
func executeGenerateContent(ctx context.Context, client *gg.Client, inv *dispatch.Invocation) (*dispatch.Response, error) {
	model := stringArg(inv.Args, "model")
	if model == "" {
		return nil, errors.New("genai.models.generate_content: `model` is required (e.g. \"gemini-2.0-flash\")")
	}
	prompt := stringArg(inv.Args, "prompt")
	if prompt == "" {
		return nil, errors.New("genai.models.generate_content: `prompt` is required")
	}
	contents := []*gg.Content{gg.NewContentFromText(prompt, gg.RoleUser)}
	resp, err := client.Models.GenerateContent(ctx, model, contents, nil)
	if err != nil {
		return nil, fmt.Errorf("genai.models.generate_content: %w", err)
	}
	body, err := json.Marshal(resp)
	if err != nil {
		return nil, fmt.Errorf("genai.models.generate_content: marshal response: %w", err)
	}
	return &dispatch.Response{
		Body:       body,
		Format:     "json",
		BytesOut:   len(body),
		StatusCode: http.StatusOK,
	}, nil
}

// stringArg pulls a string by key, tolerating absent / non-string entries.
func stringArg(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

// Compile-time check that Adapter satisfies dispatch.Adapter.
var _ dispatch.Adapter = (*Adapter)(nil)
