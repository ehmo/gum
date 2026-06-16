package googleads

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// upstreamError carries a non-2xx Google Ads response. It implements the
// dispatch.HTTPStatuser and dispatch.RetryAfterMsCarrier interfaces (by method
// name, without importing dispatch) so the dispatch boundary maps a 429 to
// RATE_LIMITED with the retry hint and a 5xx to SERVICE_DOWN.
type upstreamError struct {
	status     int
	googleCode int
	message    string
	retryMs    int64
}

func (e *upstreamError) Error() string {
	if e.message != "" {
		return fmt.Sprintf("googleads upstream error HTTP %d: %s", e.status, e.message)
	}
	return fmt.Sprintf("googleads upstream error HTTP %d", e.status)
}

// HTTPStatusCode satisfies dispatch.HTTPStatuser.
func (e *upstreamError) HTTPStatusCode() int { return e.status }

// RetryAfterMs satisfies dispatch.RetryAfterMsCarrier (0 when absent).
func (e *upstreamError) RetryAfterMs() int64 { return e.retryMs }

// newUpstreamError parses the Google Ads JSON error envelope and Retry-After
// header into an *upstreamError.
func newUpstreamError(status int, body []byte, headers http.Header) *upstreamError {
	e := &upstreamError{status: status}
	var env struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Status  string `json:"status"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &env) == nil {
		e.googleCode = env.Error.Code
		e.message = strings.TrimSpace(env.Error.Message)
	}
	if e.message == "" && len(body) > 0 {
		// Fall back to a truncated raw body so the error is never empty.
		const maxRaw = 512
		raw := strings.TrimSpace(string(body))
		if len(raw) > maxRaw {
			raw = raw[:maxRaw]
		}
		e.message = raw
	}
	e.retryMs = parseRetryAfter(headers.Get("Retry-After"))
	return e
}

// parseRetryAfter converts a Retry-After header (delta-seconds or HTTP-date)
// into milliseconds. Returns 0 when absent or unparseable.
func parseRetryAfter(v string) int64 {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs < 0 {
			return 0
		}
		return int64(secs) * 1000
	}
	if t, err := http.ParseTime(v); err == nil {
		d := time.Until(t)
		if d <= 0 {
			return 0
		}
		return d.Milliseconds()
	}
	return 0
}
