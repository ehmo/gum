package auth

import (
	"errors"
	"io"
	"net/http"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// TestNewADCResolver verifies the production constructor wires the real
// environment readers. We don't probe metadata.google.internal (that's
// network); we only assert the resolver is non-nil and the field hooks
// match expectations.
func TestNewADCResolver(t *testing.T) {
	oldClient := http.DefaultClient
	t.Cleanup(func() { http.DefaultClient = oldClient })
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.Header.Get("Metadata-Flavor"); got != "Google" {
			t.Errorf("Metadata-Flavor header = %q, want Google", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(http.NoBody),
			Request:    req,
		}, nil
	})}

	r := NewADCResolver()
	if r == nil {
		t.Fatal("NewADCResolver returned nil")
	}
	if r.Env == nil {
		t.Errorf("Env hook is nil")
	}
	if r.ReadFile == nil {
		t.Errorf("ReadFile hook is nil")
	}
	if r.MetadataAvailable == nil {
		t.Errorf("MetadataAvailable hook is nil")
	}
	if !r.MetadataAvailable() {
		t.Errorf("MetadataAvailable() = false, want true from mocked metadata server")
	}

	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("metadata unavailable")
	})}
	if r.MetadataAvailable() {
		t.Errorf("MetadataAvailable() = true, want false on transport error")
	}

	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusForbidden,
			Body:       io.NopCloser(http.NoBody),
			Request:    req,
		}, nil
	})}
	if r.MetadataAvailable() {
		t.Errorf("MetadataAvailable() = true, want false on non-200 metadata response")
	}
}

func TestNewByoOAuthUsesBoundedDefaultHTTPClient(t *testing.T) {
	b := NewByoOAuth(ByoOAuthConfig{ClientID: "cid"}, &mockKeyring{data: map[string]string{}})
	if b.client == nil {
		t.Fatal("client is nil")
	}
	if b.client.Timeout != defaultAuthHTTPTimeout {
		t.Errorf("client timeout = %v, want %v", b.client.Timeout, defaultAuthHTTPTimeout)
	}
}

// TestNewLiveADCResolver smoke-tests the live-ADC constructor. The struct
// is currently empty; we just verify the constructor returns non-nil.
func TestNewLiveADCResolver(t *testing.T) {
	r := NewLiveADCResolver()
	if r == nil {
		t.Fatal("NewLiveADCResolver returned nil")
	}
}

// TestNewDefaultCompositeResolver verifies the production composite wires
// all four sub-resolvers. SA is opt-in (env-driven), so we check it's
// constructible without surfacing config errors.
func TestNewDefaultCompositeResolver(t *testing.T) {
	// Clear the SA env var so the optional wiring stays nil.
	t.Setenv("GUM_SERVICE_ACCOUNT_KEY", "")

	c := NewDefaultCompositeResolver()
	if c == nil {
		t.Fatal("NewDefaultCompositeResolver returned nil")
	}
	if c.ADC == nil {
		t.Errorf("ADC resolver is nil")
	}
	if c.APIKey == nil {
		t.Errorf("APIKey resolver is nil")
	}
	if c.GumOAuth == nil {
		t.Errorf("GumOAuth resolver is nil")
	}
	// SA is opt-in; when GUM_SERVICE_ACCOUNT_KEY is empty it remains nil.
	if c.SA != nil {
		t.Errorf("SA = %v, want nil (no GUM_SERVICE_ACCOUNT_KEY)", c.SA)
	}
}

// TestExtractSubject locks the priority order in the ADC subject extractor:
// client_email > refresh_token > client_id > raw fallback. Empty / invalid
// JSON paths also have defined behavior.
func TestExtractSubject(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want string
	}{
		{name: "empty_bytes", in: nil, want: ""},
		{name: "invalid_json_falls_back_to_raw", in: []byte("not-json"), want: "not-json"},
		{name: "client_email_wins", in: []byte(`{"client_email":"sa@x.iam","refresh_token":"r","client_id":"c"}`), want: "sa@x.iam"},
		{name: "refresh_token_next", in: []byte(`{"refresh_token":"r","client_id":"c"}`), want: "r"},
		{name: "client_id_last", in: []byte(`{"client_id":"c"}`), want: "c"},
		{name: "none_present_falls_back_to_raw", in: []byte(`{}`), want: `{}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractSubject(tc.in); got != tc.want {
				t.Errorf("extractSubject(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestExtractQuotaProject covers the env-override path, the JSON field path,
// the empty-input path, and the malformed-JSON path.
func TestExtractQuotaProject(t *testing.T) {
	t.Run("env_var_wins", func(t *testing.T) {
		t.Setenv("GOOGLE_CLOUD_QUOTA_PROJECT", "from-env")
		if got := extractQuotaProject([]byte(`{"quota_project_id":"from-json"}`)); got != "from-env" {
			t.Errorf("env should win, got %q", got)
		}
	})

	t.Run("json_field_when_env_empty", func(t *testing.T) {
		t.Setenv("GOOGLE_CLOUD_QUOTA_PROJECT", "")
		got := extractQuotaProject([]byte(`{"quota_project_id":"from-json"}`))
		if got != "from-json" {
			t.Errorf("got %q, want from-json", got)
		}
	})

	t.Run("empty_input_returns_empty", func(t *testing.T) {
		t.Setenv("GOOGLE_CLOUD_QUOTA_PROJECT", "")
		if got := extractQuotaProject(nil); got != "" {
			t.Errorf("empty input → %q, want empty", got)
		}
	})

	t.Run("malformed_json_returns_empty", func(t *testing.T) {
		t.Setenv("GOOGLE_CLOUD_QUOTA_PROJECT", "")
		if got := extractQuotaProject([]byte("not-json")); got != "" {
			t.Errorf("malformed JSON → %q, want empty", got)
		}
	})
}
