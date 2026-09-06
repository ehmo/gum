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
	failures   []string
	retryMs    int64
}

func (e *upstreamError) Error() string {
	msg := fmt.Sprintf("googleads upstream error HTTP %d", e.status)
	if e.message != "" {
		msg += ": " + e.message
	}
	if len(e.failures) > 0 {
		msg += " (" + strings.Join(e.failures, "; ") + ")"
	}
	return msg
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
			Code    int             `json:"code"`
			Message string          `json:"message"`
			Status  string          `json:"status"`
			Details []googleAdsFail `json:"details"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &env) == nil {
		e.googleCode = env.Error.Code
		e.message = strings.TrimSpace(env.Error.Message)
		e.failures = summarizeFailures(env.Error.Details)
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

// googleAdsFail is the GoogleAdsFailure entry Google puts in error.details.
// The top-level error.message for a rejected mutate is always the generic
// "Request contains an invalid argument."; the field path and reason that
// identify which of the operations failed live only in here.
type googleAdsFail struct {
	Errors []googleAdsFailError `json:"errors"`
}

type googleAdsFailError struct {
	Message  string `json:"message"`
	Location struct {
		FieldPathElements []fieldPathElement `json:"fieldPathElements"`
	} `json:"location"`
}

type fieldPathElement struct {
	FieldName string `json:"fieldName"`
	Index     *int   `json:"index"`
}

// maxReportedFailures caps how many per-operation errors reach the message.
// A partial-failure batch can carry one error per operation, and the whole
// error string ends up in a tool result.
const maxReportedFailures = 5

// summarizeFailures renders each GoogleAdsFailure error as "<field path>: <message>".
func summarizeFailures(details []googleAdsFail) []string {
	var out []string
	total := 0

	for _, d := range details {
		for _, err := range d.Errors {
			line := failureLine(err)
			if line == "" {
				continue
			}

			total++
			if len(out) < maxReportedFailures {
				out = append(out, line)
			}
		}
	}

	if total > len(out) {
		out = append(out, fmt.Sprintf("and %d more", total-len(out)))
	}

	return out
}

func failureLine(err googleAdsFailError) string {
	path := fieldPath(err.Location.FieldPathElements)
	msg := strings.TrimSpace(err.Message)

	switch {
	case path != "" && msg != "":
		return path + ": " + msg
	case path != "":
		return path
	default:
		return msg
	}
}

// fieldPath joins the elements into "operations[3].create.ad.headlines[0].text".
func fieldPath(elements []fieldPathElement) string {
	var b strings.Builder

	for _, el := range elements {
		if el.FieldName == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('.')
		}

		b.WriteString(el.FieldName)
		if el.Index != nil {
			b.WriteString("[" + strconv.Itoa(*el.Index) + "]")
		}
	}

	return b.String()
}
