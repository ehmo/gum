package plugins_test

import (
	"testing"

	"github.com/ehmo/gum/internal/plugins"
)

// TestPluginErrorCodeMapping covers docs/test-matrix.md line 101 and
// spec §8 lines 1635-1641: plugin-local error codes map deterministically to
// stable GUM codes, retry fields are preserved per row, and the source code
// is forwarded to audit metadata.
func TestPluginErrorCodeMapping(t *testing.T) {
	t.Parallel()

	t.Run("code projection table", func(t *testing.T) {
		t.Parallel()
		cases := map[string]string{
			"RATE_LIMIT":    "RATE_LIMITED",
			"AUTH_EXPIRED":  "AUTH_REQUIRED",
			"PARSE_FAILURE": "SERVICE_DOWN",
			"SERVICE_DOWN":  "SERVICE_DOWN",
			"INVALID_INPUT": "INVALID_ARGS",
			"WHATEVER":      "SERVICE_DOWN", // unknown → SERVICE_DOWN
			"":              "SERVICE_DOWN", // empty → SERVICE_DOWN
		}
		for in, want := range cases {
			if got := plugins.MapPluginErrorCode(in); got != want {
				t.Errorf("MapPluginErrorCode(%q) = %q; want %q", in, got, want)
			}
		}
	})

	t.Run("RATE_LIMIT preserves retryable and positive retry_after_ms", func(t *testing.T) {
		t.Parallel()
		got := plugins.MapPluginError(plugins.PluginError{
			Code: "RATE_LIMIT", Retryable: true, RetryAfterMS: 5000, Message: "quota",
		})
		if got.Code != "RATE_LIMITED" || !got.Retryable || got.RetryAfterMS != 5000 || got.SourceErrorCode != "RATE_LIMIT" {
			t.Fatalf("RATE_LIMIT mapping: got %+v", got)
		}
	})

	t.Run("RATE_LIMIT drops non-positive retry_after_ms", func(t *testing.T) {
		t.Parallel()
		got := plugins.MapPluginError(plugins.PluginError{
			Code: "RATE_LIMIT", Retryable: true, RetryAfterMS: 0,
		})
		if got.RetryAfterMS != 0 {
			t.Errorf("non-positive retry_after_ms should not be preserved; got %d", got.RetryAfterMS)
		}
	})

	t.Run("AUTH_EXPIRED forces retryable false", func(t *testing.T) {
		t.Parallel()
		got := plugins.MapPluginError(plugins.PluginError{
			Code: "AUTH_EXPIRED", Retryable: true, RetryAfterMS: 1000,
		})
		if got.Code != "AUTH_REQUIRED" || got.Retryable {
			t.Errorf("AUTH_EXPIRED must force retryable=false; got %+v", got)
		}
		if got.RetryAfterMS != 0 {
			t.Errorf("AUTH_EXPIRED must drop retry_after_ms; got %d", got.RetryAfterMS)
		}
	})

	t.Run("PARSE_FAILURE preserves plugin-asserted retryable", func(t *testing.T) {
		t.Parallel()
		got := plugins.MapPluginError(plugins.PluginError{Code: "PARSE_FAILURE", Retryable: true})
		if got.Code != "SERVICE_DOWN" || !got.Retryable {
			t.Errorf("PARSE_FAILURE mapping: got %+v", got)
		}
	})

	t.Run("SERVICE_DOWN preserves retry fields", func(t *testing.T) {
		t.Parallel()
		got := plugins.MapPluginError(plugins.PluginError{
			Code: "SERVICE_DOWN", Retryable: true, RetryAfterMS: 250,
		})
		if got.Code != "SERVICE_DOWN" || !got.Retryable || got.RetryAfterMS != 250 {
			t.Errorf("SERVICE_DOWN mapping: got %+v", got)
		}
	})

	t.Run("INVALID_INPUT forces retryable false", func(t *testing.T) {
		t.Parallel()
		got := plugins.MapPluginError(plugins.PluginError{
			Code: "INVALID_INPUT", Retryable: true, RetryAfterMS: 1000,
		})
		if got.Code != "INVALID_ARGS" || got.Retryable {
			t.Errorf("INVALID_INPUT must force retryable=false; got %+v", got)
		}
		if got.RetryAfterMS != 0 {
			t.Errorf("INVALID_INPUT must drop retry_after_ms; got %d", got.RetryAfterMS)
		}
	})

	t.Run("unknown code maps to SERVICE_DOWN with retryable false and preserved source", func(t *testing.T) {
		t.Parallel()
		got := plugins.MapPluginError(plugins.PluginError{
			Code: "SOMETHING_WEIRD", Retryable: true, RetryAfterMS: 9999, Message: "boom",
		})
		if got.Code != "SERVICE_DOWN" || got.Retryable || got.RetryAfterMS != 0 {
			t.Errorf("unknown code mapping: got %+v", got)
		}
		if got.SourceErrorCode != "SOMETHING_WEIRD" {
			t.Errorf("source_error_code must be preserved verbatim; got %q", got.SourceErrorCode)
		}
		if got.Message != "boom" {
			t.Errorf("message should be forwarded; got %q", got.Message)
		}
	})
}
