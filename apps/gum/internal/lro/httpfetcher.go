package lro

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ehmo/gum/internal/httputil"
	"github.com/ehmo/gum/internal/lro/routing"
)

// ErrUnroutable is the sentinel returned when no routing-table entry and no
// fallback template can fetch the operation. The dispatch layer maps it to
// the LRO_UNROUTABLE envelope.
var ErrUnroutable = errors.New("LRO_UNROUTABLE")

// HTTPFetcher implements Fetcher by consulting the §5.7 routing table first
// and then falling back to the two GET templates if no entry matches.
//
//  1. routing.Lookup(name) hit → GET host+SubstitutePath
//  2. routing.Lookup(name) miss → GET {LastHost}/v1/operations/{tail}
//  3. step 2 fails → GET {LastHost}/v1/{full operation_name}
//  4. step 3 fails → return ErrUnroutable
//
// LastHost is the session's most-recent successful upstream host (injected
// at construction). If empty, only step 1 (routing-table hit) is attempted.
//
// AuthInject is the callback that attaches Authorization, x-goog-quota-
// project, etc. to outbound requests. The caller owns credential acquisition;
// HTTPFetcher only knows how to GET and parse Operation JSON.
type HTTPFetcher struct {
	HTTPClient *http.Client
	LastHost   string
	AuthInject func(req *http.Request) error
}

// Fetch implements Fetcher.
func (f *HTTPFetcher) Fetch(ctx context.Context, operationName string) (*Status, error) {
	if f.HTTPClient == nil {
		// Not http.DefaultClient: it has no Timeout, so a stalled poll (TCP
		// connect or a server that accepts but never responds) holds the
		// goroutine until the OS TCP timeout even when the caller set no
		// deadline. A 30s cap matches the typedrestsdk/googleads clients
		// (review gum-vcvt).
		f.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	// Attempt 1: routing-table hit.
	if ep, _, ok := routing.Lookup(operationName); ok {
		if ep.Transport == routing.TransportGRPC {
			// v0.1.0 has no gRPC longrunning client wired; surface unroutable
			// with a specific hint so the caller can wait for v0.2.0 gRPC
			// support (gum-7po). Downgrades gracefully to fallback.
			if status, err := f.tryREST(ctx, "googleapis.com", "/v1/"+operationName); err == nil {
				return status, nil
			}
			return nil, ErrUnroutable
		}
		path := routing.SubstitutePath(ep, operationName)
		if path != "" && ep.Host != "" {
			status, err := f.tryREST(ctx, ep.Host, path)
			if err == nil {
				return status, nil
			}
			// fall through to fallback templates on miss
		}
	}

	// Attempts 2 & 3: §5.7 fallback templates against LastHost.
	if f.LastHost != "" {
		tail := operationName
		if idx := strings.LastIndex(operationName, "/"); idx >= 0 {
			tail = operationName[idx+1:]
		}
		// 2: GET {LastHost}/v1/operations/{operation_name_tail}
		if status, err := f.tryREST(ctx, f.LastHost, "/v1/operations/"+tail); err == nil {
			return status, nil
		}
		// 3: GET {LastHost}/v1/{operation_name}
		if status, err := f.tryREST(ctx, f.LastHost, "/v1/"+operationName); err == nil {
			return status, nil
		}
	}

	return nil, ErrUnroutable
}

// tryREST issues a single GET against host+path with auth injected and
// parses the response as a Google Operation. Returns ErrUnroutable on
// 404/missing-shape so the caller can advance to the next template.
func (f *HTTPFetcher) tryREST(ctx context.Context, host, path string) (*Status, error) {
	url := "https://" + host + path
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	if f.AuthInject != nil {
		if err := f.AuthInject(req); err != nil {
			return nil, err
		}
	}
	resp, err := f.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	// Operation polls return tiny JSON docs (~1 KiB). Cap at 4 MiB to defend
	// against a hostile or buggy upstream (gum-4d66).
	body, readErr := httputil.ReadCapped(resp.Body, 4<<20)
	if readErr != nil {
		return nil, fmt.Errorf("lro: read body from %s: %w", url, readErr)
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrUnroutable
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("lro: upstream %s returned %d: %s", url, resp.StatusCode, truncate(body, 200))
	}
	var op operationDoc
	if err := json.Unmarshal(body, &op); err != nil {
		return nil, ErrUnroutable
	}
	// A valid Operation MUST carry either `name` or `done` at top level. If
	// neither is present the response is not an Operation — advance to next
	// fallback rather than mis-report Done.
	if op.Name == "" && !op.hasDoneField {
		return nil, ErrUnroutable
	}
	return &Status{Done: op.Done, Result: op.toResult()}, nil
}

// operationDoc mirrors the shared Google Operation shape. We use a custom
// unmarshaller for `done` so we can distinguish "absent" from "false".
type operationDoc struct {
	Name         string          `json:"name"`
	Done         bool            `json:"done"`
	Response     json.RawMessage `json:"response,omitempty"`
	Error        json.RawMessage `json:"error,omitempty"`
	Metadata     json.RawMessage `json:"metadata,omitempty"`
	hasDoneField bool
}

func (o *operationDoc) UnmarshalJSON(data []byte) error {
	// Marker keys appear as raw JSON to detect presence.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if v, ok := raw["name"]; ok {
		_ = json.Unmarshal(v, &o.Name)
	}
	if v, ok := raw["done"]; ok {
		o.hasDoneField = true
		_ = json.Unmarshal(v, &o.Done)
	}
	o.Response = raw["response"]
	o.Error = raw["error"]
	o.Metadata = raw["metadata"]
	return nil
}

func (o *operationDoc) toResult() any {
	out := map[string]any{
		"name": o.Name,
		"done": o.Done,
	}
	if len(o.Response) > 0 {
		var v any
		_ = json.Unmarshal(o.Response, &v)
		out["response"] = v
	}
	if len(o.Error) > 0 {
		var v any
		_ = json.Unmarshal(o.Error, &v)
		out["error"] = v
	}
	if len(o.Metadata) > 0 {
		var v any
		_ = json.Unmarshal(o.Metadata, &v)
		out["metadata"] = v
	}
	return out
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}
