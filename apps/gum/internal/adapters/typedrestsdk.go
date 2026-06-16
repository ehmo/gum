// Package adapters holds backend executors (typed-rest-sdk, code-runner, ...) per spec.md §14.
//
// Executors only — no policy. Dispatch lifecycle in internal/dispatch decides what to call.
package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/ehmo/gum/internal/dispatch"
	"github.com/ehmo/gum/internal/httputil"
)

// BodyArgKey is the reserved Invocation.Args key carrying a JSON request body
// for POST/PUT/PATCH. The value may be a map/struct (will be json-marshalled),
// a string (sent verbatim as UTF-8), or a []byte (sent verbatim). For GET/DELETE
// the key is ignored — it is never serialised into the query string.
const BodyArgKey = "body"

// UpstreamError wraps a non-2xx response from a Google API endpoint with both
// the HTTP-layer status and the Google API error envelope fields.
type UpstreamError struct {
	// HTTPStatus is the HTTP response status code (e.g. 503).
	HTTPStatus int
	// GoogleCode is the numeric Google API error code (from error.code in the JSON body).
	GoogleCode string
	// GoogleStatus is the string status token (from error.status, e.g. "UNAVAILABLE").
	GoogleStatus string
	// Message is the human-readable error message from the Google API error body.
	Message string
	// RetryAfterMillis is the parsed Retry-After header in milliseconds, or 0
	// when the upstream omitted the header. Populated by parseUpstreamError
	// when HTTPStatus == 429. Surfaced through the dispatch.RetryAfterMsCarrier
	// interface so the dispatch boundary can attach retry_after_ms to the
	// RATE_LIMITED envelope (spec §1635).
	RetryAfterMillis int64
}

func (e *UpstreamError) Error() string {
	return fmt.Sprintf("upstream error HTTP %d (%s/%s): %s", e.HTTPStatus, e.GoogleStatus, e.GoogleCode, e.Message)
}

// HTTPStatusCode satisfies dispatch.HTTPStatuser so the dispatch boundary can
// map HTTP 429 responses to ErrCodeRateLimited without importing
// internal/adapters (adapters → dispatch is the allowed direction).
func (e *UpstreamError) HTTPStatusCode() int { return e.HTTPStatus }

// RetryAfterMs satisfies dispatch.RetryAfterMsCarrier so the dispatch boundary
// can copy the Retry-After hint onto the RATE_LIMITED envelope as
// retry_after_ms (spec §1635). Returns 0 when no Retry-After was present.
func (e *UpstreamError) RetryAfterMs() int64 { return e.RetryAfterMillis }

// TypedRestSDK is the adapter for backend_kind = "typed-rest-sdk".
//
// It builds and fires HTTP requests against Google REST APIs using the HTTP binding
// from the catalog variant. It implements exponential back-off (via cenkalti/backoff)
// and honours Retry-After headers.
//
// All fields are injectable for testing:
//   - HTTPClient: swap for an httptest.Server-backed client.
//   - Clock: control "now" for token-expiry checks.
//   - SleepFn: intercept sleeps in TestTokenBucketBackoff to measure duration.
type TypedRestSDK struct {
	// HTTPClient is the HTTP client used for all upstream requests.
	// Defaults to &http.Client{Timeout: 30 * time.Second}.
	HTTPClient *http.Client
	// Clock returns the current time. Defaults to time.Now.
	Clock func() time.Time
	// SleepFn waits for d or until ctx is cancelled, returning ctx.Err() on
	// cancel and nil when the full wait elapsed. The default is a timer-based
	// wait that adds no goroutine, so a cancelled call returns immediately with
	// nothing left sleeping in the background (review gum-vcvt). Tests inject a
	// recording/cancelling function.
	SleepFn func(ctx context.Context, d time.Duration) error
	// MaxResponseBytes caps a single response body. 0 = use
	// httputil.DefaultMaxResponseBytes (64 MiB). Negative disables the cap
	// (test-only — never set in production). Per-op overrides live in the
	// catalog variant metadata "max_response_bytes" (handled by resolveCap).
	MaxResponseBytes int64
	// CredentialHostAllowlist extends the default credentialed absolute-URL
	// allowlist. Entries may be hostnames or absolute URLs. Test code uses this
	// for httptest servers; production should normally leave it empty.
	CredentialHostAllowlist []string
	// AllowInsecureCredentialURLs permits http:// credentialed URLs only for
	// explicitly allowlisted hosts. This is for local tests, not production.
	AllowInsecureCredentialURLs bool
}

// stripCredsOnCrossHostRedirect bounds redirects and, on a cross-host hop,
// strips the API key from both the ?key= query param and the X-Goog-Api-Key
// header. Go's net/http already drops Authorization/Cookie on cross-domain
// redirects, but it forwards query params verbatim, so a redirect off
// googleapis.com to another host would otherwise leak the key (review gum-t8x1).
func stripCredsOnCrossHostRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	if len(via) == 0 {
		return nil
	}
	if req.URL.Host == via[0].URL.Host {
		return nil
	}
	if q := req.URL.Query(); q.Has("key") {
		q.Del("key")
		req.URL.RawQuery = q.Encode()
	}
	req.Header.Del("X-Goog-Api-Key")
	return nil
}

// ctxSleep waits for d or until ctx is cancelled, using a single timer and no
// extra goroutine. Returns ctx.Err() on cancellation so the retry loop aborts
// immediately instead of leaving a goroutine sleeping up to maxRetryAfterSeconds
// (review gum-vcvt).
func ctxSleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// NewTypedRestSDK constructs a TypedRestSDK with production defaults.
func NewTypedRestSDK() *TypedRestSDK {
	return &TypedRestSDK{
		HTTPClient:       &http.Client{Timeout: 30 * time.Second, CheckRedirect: stripCredsOnCrossHostRedirect},
		Clock:            time.Now,
		SleepFn:          ctxSleep,
		MaxResponseBytes: httputil.DefaultMaxResponseBytes,
	}
}

// AllowCredentialHostForTest allows a local httptest URL to receive test
// credentials. It intentionally also enables insecure HTTP for that host.
func (t *TypedRestSDK) AllowCredentialHostForTest(rawURL string) {
	t.CredentialHostAllowlist = append(t.CredentialHostAllowlist, rawURL)
	t.AllowInsecureCredentialURLs = true
}

func (t *TypedRestSDK) resolveRequestURL(resolvedPath string, credentialed bool) (string, error) {
	if strings.HasPrefix(resolvedPath, "http://") || strings.HasPrefix(resolvedPath, "https://") {
		if credentialed {
			if err := t.validateCredentialURL(resolvedPath); err != nil {
				return "", err
			}
		}
		return resolvedPath, nil
	}
	return "https://www.googleapis.com" + resolvedPath, nil
}

func (t *TypedRestSDK) validateCredentialURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("typedrestsdk: parse credentialed URL: %w", err)
	}
	if u.User != nil {
		return fmt.Errorf("typedrestsdk: credentialed absolute URL must not include userinfo")
	}
	if u.Fragment != "" {
		return fmt.Errorf("typedrestsdk: credentialed absolute URL must not include a fragment")
	}
	if u.Scheme != "https" {
		if !t.AllowInsecureCredentialURLs || !t.urlExplicitlyAllowed(u) {
			return fmt.Errorf("typedrestsdk: credentialed absolute URL must use https")
		}
	}
	if !t.credentialURLAllowed(u) {
		return fmt.Errorf("typedrestsdk: credentialed absolute URL host %q is not allowed", u.Hostname())
	}
	return nil
}

func (t *TypedRestSDK) credentialURLAllowed(u *url.URL) bool {
	host := u.Hostname()
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	if host == "www.googleapis.com" || strings.HasSuffix(host, ".googleapis.com") {
		return true
	}
	return t.urlExplicitlyAllowed(u)
}

func (t *TypedRestSDK) urlExplicitlyAllowed(target *url.URL) bool {
	host := strings.ToLower(strings.TrimSpace(target.Hostname()))
	port := target.Port()
	for _, entry := range t.CredentialHostAllowlist {
		allowed := strings.ToLower(strings.TrimSpace(entry))
		if allowed == "" {
			continue
		}
		allowedPort := ""
		if u, err := url.Parse(allowed); err == nil && u.Hostname() != "" {
			allowed = strings.ToLower(u.Hostname())
			allowedPort = u.Port()
		} else if h, p, err := net.SplitHostPort(allowed); err == nil {
			allowed = strings.ToLower(h)
			allowedPort = p
		}
		if host == allowed && (allowedPort == "" || port == allowedPort) {
			return true
		}
	}
	return false
}

// pathParamRe matches {name} placeholders in URL path templates.
var pathParamRe = regexp.MustCompile(`\{([^}]+)\}`)

// redactURLError strips the request URL from a *url.Error. Credentialed REST
// calls can carry an API key as a `?key=` query param (spec §7 api_key), so a
// transport-level failure (DNS, connection refused, TLS) would otherwise surface
// the full URL — including the key — in the error string shown to the operator
// or forwarded to an MCP/agent client. Redacting keeps the secret out of logs
// and the model context. The wrapped Op/Err are preserved for diagnosis.
func redactURLError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		ue.URL = "[redacted]"
	}
	return err
}

// googleErrorBody is the JSON shape of Google API error responses.
type googleErrorBody struct {
	Error struct {
		Code    json.Number `json:"code"`
		Status  string      `json:"status"`
		Message string      `json:"message"`
	} `json:"error"`
}

// Execute satisfies dispatch.Adapter for backend_kind = "typed-rest-sdk".
//
// It reads the HTTP binding from rv.Variant.Binding, substitutes path parameters
// from inv.Args, appends query parameters, sets the Authorization header from
// creds.Token, and fires the request. On 5xx it retries with exponential back-off
// up to 3 retries (cenkalti/backoff). On 429 with Retry-After it sleeps (via SleepFn)
// before a single additional retry.
func (t *TypedRestSDK) Execute(ctx context.Context, inv *dispatch.Invocation, rv *dispatch.ResolvedVariant, creds *dispatch.Credentials) (*dispatch.Response, error) {
	if rv.Variant == nil || rv.Variant.Binding == nil || rv.Variant.Binding.HTTP == nil {
		return nil, fmt.Errorf("typedrestsdk: missing HTTP binding for op %s", inv.OpID)
	}

	method := rv.Variant.Binding.HTTP.Method
	pathTemplate := rv.Variant.Binding.HTTP.Path

	// Identify which args are path params. A leading '+' marks RFC 6570 reserved
	// expansion (e.g. {+resourceName}), where the value may contain unescaped
	// '/' — strip it to recover the bare arg name.
	pathParamNames := map[string]bool{}
	for _, m := range pathParamRe.FindAllStringSubmatch(pathTemplate, -1) {
		pathParamNames[strings.TrimPrefix(m[1], "+")] = true
	}

	// Substitute path params.
	resolvedPath := pathParamRe.ReplaceAllStringFunc(pathTemplate, func(placeholder string) string {
		name := placeholder[1 : len(placeholder)-1] // strip { and }
		reserved := strings.HasPrefix(name, "+")
		name = strings.TrimPrefix(name, "+")
		if val, ok := inv.Args[name]; ok {
			s := fmt.Sprintf("%v", val)
			if reserved {
				// {+name}: reserved expansion — preserve '/' and other reserved
				// chars (e.g. resourceName "people/c123", orgUnitPath "/Eng/BE").
				// Neutralise the few characters that would break out of the path
				// into the query/fragment or inject a header: a resource-name arg
				// must not be able to drop the field mask or smuggle params.
				s = strings.NewReplacer("\r", "", "\n", "", "?", "%3F", "#", "%23").Replace(s)
				return s
			}
			return url.PathEscape(s)
		}
		return placeholder // leave unresolved if not provided
	})

	// Build query string from remaining (non-path) args. The reserved BodyArgKey
	// never participates in the query string. Slice values ([]string, []any)
	// emit one ?k=v pair per element (repeat style — Google's REST APIs use
	// this for `fields`, `q`, label filters, etc., spec gum-fo59).
	// HeaderParams (binding) route specific args to HTTP headers instead of the
	// query string — e.g. fieldMask → X-Goog-FieldMask for Places (New)/Routes.
	headerParams := rv.Variant.Binding.HTTP.HeaderParams
	headerArgs := map[string]string{}
	queryVals := url.Values{}
	for k, v := range inv.Args {
		if pathParamNames[k] || k == BodyArgKey {
			continue
		}
		if hn, ok := headerParams[k]; ok {
			headerArgs[hn] = fmt.Sprintf("%v", v)
			continue
		}
		appendQueryValue(queryVals, k, v)
	}
	// auth_strategy=api_key: also pass the key as the universal ?key= query
	// param alongside the X-Goog-Api-Key header. Newer Google APIs read the
	// header; classic services (e.g. Maps geocoding/timezone) read only the
	// query param. Setting both is harmless — Google accepts either.
	if creds != nil && creds.APIKey != "" {
		queryVals.Set("key", creds.APIKey)
	}

	rawURL, err := t.resolveRequestURL(resolvedPath, creds != nil && (creds.Token != "" || creds.APIKey != "" || creds.QuotaProjectID != ""))
	if err != nil {
		return nil, err
	}
	if len(queryVals) > 0 {
		rawURL += "?" + queryVals.Encode()
	}

	// Marshal the request body once if present and the method supports it.
	// Per HTTP semantics we only send bodies for POST/PUT/PATCH.
	upperMethod := strings.ToUpper(method)
	bodyBytes, bodyErr := marshalBody(inv.Args[BodyArgKey], upperMethod)
	if bodyErr != nil {
		return nil, fmt.Errorf("typedrestsdk: marshal request body: %w", bodyErr)
	}

	// doRequest performs a single HTTP attempt and returns the full response.
	doRequest := func() (body []byte, status int, headers http.Header, err error) {
		if ctx.Err() != nil {
			return nil, 0, nil, ctx.Err()
		}
		var bodyReader io.Reader
		if bodyBytes != nil {
			bodyReader = bytes.NewReader(bodyBytes)
		}
		req, reqErr := http.NewRequestWithContext(ctx, method, rawURL, bodyReader)
		if reqErr != nil {
			return nil, 0, nil, reqErr
		}
		if bodyBytes != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		// Binding-routed request headers (e.g. X-Goog-FieldMask). Set before auth
		// so an op can never clobber the Authorization/api-key headers below.
		for hn, hv := range headerArgs {
			req.Header.Set(hn, hv)
		}
		if creds != nil && creds.Token != "" {
			req.Header.Set("Authorization", "Bearer "+creds.Token)
		}
		if creds != nil && creds.APIKey != "" {
			// Spec §7 auth_strategy=api_key: Google's universal API-key
			// header. The dispatcher guarantees Token is empty in this
			// branch (api_key vs Bearer are mutually exclusive on a single
			// request) but we set both fields independently so a future
			// adapter that needs to layer both can do so without changing
			// this code path.
			req.Header.Set("X-Goog-Api-Key", creds.APIKey)
		}
		if creds != nil && creds.QuotaProjectID != "" {
			// Google APIs that bill against user ADC need the quota project
			// attribution header. Setting this is harmless for APIs that
			// don't require it.
			req.Header.Set("X-Goog-User-Project", creds.QuotaProjectID)
		}
		resp, doErr := t.HTTPClient.Do(req)
		if doErr != nil {
			return nil, 0, nil, redactURLError(doErr)
		}
		defer func() { _ = resp.Body.Close() }()
		b, readErr := httputil.ReadCapped(resp.Body, t.MaxResponseBytes)
		return b, resp.StatusCode, resp.Header, readErr
	}

	// parseUpstreamError builds an UpstreamError from a non-2xx response.
	// headers may be nil; when present, Retry-After is parsed (status 429
	// or 503) and surfaced as RetryAfterMillis so the dispatch boundary can
	// attach retry_after_ms to the RATE_LIMITED envelope (spec §1635).
	parseUpstreamError := func(status int, body []byte, headers http.Header) *UpstreamError {
		ue := &UpstreamError{HTTPStatus: status}
		var eb googleErrorBody
		if jsonErr := json.Unmarshal(body, &eb); jsonErr == nil {
			ue.GoogleCode = eb.Error.Code.String()
			ue.GoogleStatus = eb.Error.Status
			ue.Message = eb.Error.Message
		}
		if headers != nil {
			if secs := retryAfterSeconds(headers); secs > 0 {
				ue.RetryAfterMillis = int64(secs) * 1000
			}
		}
		return ue
	}

	// Retry loop for 5xx using cenkalti/backoff (max 3 retries).
	bo := backoff.WithContext(backoff.WithMaxRetries(backoff.NewExponentialBackOff(), 3), ctx)

	var finalBody []byte
	var finalStatus int
	var lastUpstreamErr *UpstreamError

	operation := func() error {
		if ctx.Err() != nil {
			return backoff.Permanent(ctx.Err())
		}

		body, status, headers, err := doRequest()
		if err != nil {
			if ctx.Err() != nil {
				return backoff.Permanent(ctx.Err())
			}
			// Response-body cap breach is a permanent error — there's no
			// "retry smaller" semantic for an oversized upstream payload.
			// Surface as RESPONSE_TOO_LARGE so the dispatch boundary emits
			// the structured envelope (spec gum-4d66).
			if errors.Is(err, httputil.ErrResponseTooLarge) {
				return backoff.Permanent(dispatch.NewStructuredError(
					dispatch.ErrCodeResponseTooLarge,
					fmt.Sprintf("upstream response exceeded body cap for op %s", inv.OpID)).
					WithDetail("op_id", inv.OpID).
					WithDetail("cap_bytes", t.MaxResponseBytes).
					WithRetryable(false))
			}
			return err // retryable network error
		}

		if status >= 200 && status < 300 {
			finalBody = body
			finalStatus = status
			return nil // success
		}

		if status == 429 {
			// Handle Retry-After: sleep then retry once more. SleepFn is
			// context-aware (default: timer + select on ctx) so a cancelled or
			// deadlined call returns immediately with no goroutine left sleeping
			// up to maxRetryAfterSeconds in the background (review gum-vcvt).
			if secs := retryAfterSeconds(headers); secs > 0 {
				if err := t.SleepFn(ctx, time.Duration(secs)*time.Second); err != nil {
					return backoff.Permanent(err)
				}
			}
			// Retry once more immediately within this attempt.
			body2, status2, headers2, err2 := doRequest()
			if err2 != nil {
				if ctx.Err() != nil {
					return backoff.Permanent(ctx.Err())
				}
				return err2
			}
			if status2 >= 200 && status2 < 300 {
				finalBody = body2
				finalStatus = status2
				return nil
			}
			// Still failing after retry — return as permanent 4xx-like (don't 5xx-retry 429).
			// Prefer headers2 (final attempt). Fall back to the first-attempt headers
			// when the second attempt didn't return a Retry-After hint.
			lastUpstreamErr = parseUpstreamError(status2, body2, headers2)
			if lastUpstreamErr.RetryAfterMillis == 0 && status2 == 429 {
				if secs := retryAfterSeconds(headers); secs > 0 {
					lastUpstreamErr.RetryAfterMillis = int64(secs) * 1000
				}
			}
			// A *5xx* after the Retry-After'd 429 is a transient server error, not a
			// terminal rate-limit: let the outer backoff loop retry it for idempotent
			// methods (mirrors the direct-5xx arm below). A repeat 429 — or any other
			// 4xx — is terminal here; we already honored its one Retry-After shot.
			if status2 >= 500 && isIdempotentMethod(upperMethod) {
				return lastUpstreamErr // retryable
			}
			return backoff.Permanent(lastUpstreamErr)
		}

		if status >= 500 {
			lastUpstreamErr = parseUpstreamError(status, body, headers)
			if !isIdempotentMethod(upperMethod) {
				// A 5xx on a non-idempotent method (POST/PATCH) may have already
				// taken effect server-side before the failure (e.g. Gmail accepted
				// the send, then the connection dropped). Retrying would duplicate
				// the write/send, so surface the error to the caller instead.
				return backoff.Permanent(lastUpstreamErr)
			}
			return lastUpstreamErr // retryable 5xx (idempotent method)
		}

		// 4xx (other): not retryable.
		lastUpstreamErr = parseUpstreamError(status, body, headers)
		return backoff.Permanent(lastUpstreamErr)
	}

	if err := backoff.Retry(operation, bo); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if lastUpstreamErr != nil {
			return nil, lastUpstreamErr
		}
		return nil, err
	}

	return &dispatch.Response{
		Body:       finalBody,
		Format:     "json",
		StatusCode: finalStatus,
		BytesIn:    0,
		BytesOut:   len(finalBody),
	}, nil
}

// marshalBody serialises the reserved body argument into bytes that can be sent
// as the HTTP request body. Returns nil bytes (no error) when the caller did not
// provide a body or when the method does not support a body.
//
// appendQueryValue adds key/v to vals under repeat style: slice values emit
// one ?key=elem pair per element rather than the Go default "[a b]" rendering.
// Scalars fall through to fmt.Sprintf("%v"). Spec gum-fo59.
func appendQueryValue(vals url.Values, key string, v any) {
	switch s := v.(type) {
	case nil:
		return
	case []string:
		for _, elem := range s {
			vals.Add(key, elem)
		}
	case []any:
		for _, elem := range s {
			vals.Add(key, formatQueryScalar(elem))
		}
	default:
		vals.Add(key, formatQueryScalar(v))
	}
}

// formatQueryScalar renders a scalar arg as a query-string value. JSON numbers
// arrive as float64; FormatFloat with the 'f' verb avoids scientific notation,
// so a large integer like 1500000000 stays "1500000000" instead of becoming
// "1.5e+09" (which Google REST APIs reject with 400). Non-float values keep the
// default formatting (int64, bool, string all render correctly).
func formatQueryScalar(v any) string {
	if f, ok := v.(float64); ok {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	return fmt.Sprintf("%v", v)
}

// isIdempotentMethod reports whether retrying a 5xx for this HTTP method is safe
// (no risk of a duplicate side effect). Per RFC 7231 §4.2.2 GET, HEAD, PUT,
// DELETE, OPTIONS, and TRACE are idempotent; POST and PATCH are not.
func isIdempotentMethod(m string) bool {
	switch m {
	case "GET", "HEAD", "PUT", "DELETE", "OPTIONS", "TRACE":
		return true
	}
	return false
}

// Accepted shapes:
//   - nil           → no body
//   - []byte        → sent verbatim
//   - string        → sent verbatim as UTF-8
//   - map/struct/…  → json.Marshal
func marshalBody(raw any, method string) ([]byte, error) {
	if raw == nil {
		return nil, nil
	}
	switch method {
	case "POST", "PUT", "PATCH":
		// continue
	default:
		return nil, nil
	}
	switch v := raw.(type) {
	case []byte:
		if len(v) == 0 {
			return nil, nil
		}
		return v, nil
	case string:
		if v == "" {
			return nil, nil
		}
		return []byte(v), nil
	default:
		return json.Marshal(v)
	}
}

// maxRetryAfterSeconds is the spec-mandated upper bound on Retry-After
// (gum-4pfi). A misbehaving upstream can otherwise wedge the executor for
// arbitrarily long; 5 minutes is the longest realistic backoff we honour
// inline before surfacing to the caller.
const maxRetryAfterSeconds = 300

// retryAfterSeconds parses the Retry-After header value in either form
// permitted by RFC 7231 §7.1.3: delta-seconds (e.g. "30") or HTTP-date
// (e.g. "Wed, 21 Oct 2015 07:28:00 GMT"). Past dates resolve to 0. All
// values are clamped to maxRetryAfterSeconds.
func retryAfterSeconds(h http.Header) int {
	ra := strings.TrimSpace(h.Get("Retry-After"))
	if ra == "" {
		return 0
	}
	if n, err := strconv.Atoi(ra); err == nil {
		return clampRetryAfter(n)
	}
	if t, err := http.ParseTime(ra); err == nil {
		delta := int(time.Until(t).Seconds())
		if delta < 0 {
			return 0
		}
		return clampRetryAfter(delta)
	}
	return 0
}

func clampRetryAfter(n int) int {
	if n < 0 {
		return 0
	}
	if n > maxRetryAfterSeconds {
		return maxRetryAfterSeconds
	}
	return n
}
