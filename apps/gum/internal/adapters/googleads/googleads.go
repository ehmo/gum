// Package googleads is the backend executor for catalog variants with
// backend_kind="google-ads-sdk" (spec §14): the Google Ads API Keyword Planner
// methods on googleads.googleapis.com.
//
// It is deliberately a small, self-contained REST executor rather than a
// variant of the shared typed-rest-sdk adapter, for one reason: the Google Ads
// API requires a secret `developer-token` header on every request. That secret
// is sourced server-side (OS keychain / env via the injected DevToken hook) and
// is NEVER an invocation arg, so it never enters the audit log, args_canonical,
// the semantic cache key, or the MCP tool-call context. The OAuth Bearer
// (scope https://www.googleapis.com/auth/adwords) still flows through the normal
// byo_oauth resolver as creds.Token; the customer id and (optional) manager
// login-customer-id are non-secret invocation args.
package googleads

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ehmo/gum/internal/dispatch"
)

// DefaultBaseURL is the Google Ads API host + version. Keyword planning methods
// are stable across recent versions; v24 is the current target.
const DefaultBaseURL = "https://googleads.googleapis.com/v24"

// defaultMaxResponseBytes caps a single response body. A domain (siteSeed) idea
// request can return up to 250k ideas, so the cap is generous but bounded.
const defaultMaxResponseBytes = 16 << 20 // 16 MiB

// Retry policy for the ~1-QPS-per-customer keyword-planning rate limit.
const (
	defaultMaxAttempts = 4
	defaultBackoffBase = 500 * time.Millisecond
	maxBackoff         = 30 * time.Second
)

// Input limits (Google's documented keywordSeed cap is 20; the rest is a
// defensive safety cap to fail closed on absurd input before hitting the API).
const (
	maxIdeaSeedKeywords = 20
	maxKeywords         = 10000
)

// knownMethods is the closed allow-list of keyword-planning custom methods this
// adapter serves. Anything else fails closed before a request is built.
var knownMethods = map[string]bool{
	"generateKeywordIdeas":             true,
	"generateKeywordHistoricalMetrics": true,
	"generateKeywordForecastMetrics":   true,
}

// validNetworks / validMatchTypes are the closed enums accepted from callers.
var (
	validNetworks   = map[string]bool{"GOOGLE_SEARCH": true, "GOOGLE_SEARCH_AND_PARTNERS": true}
	validMatchTypes = map[string]bool{"BROAD": true, "PHRASE": true, "EXACT": true}
)

// Adapter executes Google Ads Keyword Planner calls for catalog variants whose
// binding.adapter_key starts with "googleads.". The method to invoke is taken
// from the binding HTTP path's custom-method suffix
// (".../customers/{customerId}:generateKeywordIdeas").
type Adapter struct {
	// HTTPClient is used for the upstream call. Tests inject an
	// httptest.Server-backed client; production leaves it nil for the default.
	HTTPClient *http.Client
	// BaseURL overrides DefaultBaseURL. Tests point it at an httptest server;
	// production leaves it empty.
	BaseURL string
	// DevToken returns the Google Ads developer token, sourced server-side from
	// the keychain/env. Injected at construction so this package stays decoupled
	// from internal/auth's keychain. Returns "" when no token is configured.
	DevToken func() string
	// MaxResponseBytes overrides defaultMaxResponseBytes (0 = default).
	MaxResponseBytes int
	// MaxAttempts overrides defaultMaxAttempts for the 429/5xx retry loop
	// (0 = default; set to 1 to disable retries).
	MaxAttempts int
	// backoffBase overrides defaultBackoffBase; tests set a tiny value for speed.
	backoffBase time.Duration
	// now is injected in tests for deterministic forecast date defaults.
	now func() time.Time
}

// NewAdapter constructs a Google Ads adapter. devToken supplies the developer
// token (keychain/env); pass nil for a token-less adapter (every call then
// fails closed with an actionable error).
func NewAdapter(devToken func() string) *Adapter {
	return &Adapter{DevToken: devToken, now: time.Now}
}

// defaultHTTPClient backs the production path (a.HTTPClient nil). It carries a
// per-request timeout so a stalled googleads.googleapis.com connection cannot
// hang a goroutine indefinitely (http.DefaultClient has no Timeout). The
// retry/backoff loop still honours ctx for the inter-attempt wait.
var defaultHTTPClient = &http.Client{Timeout: 30 * time.Second}

func (a *Adapter) httpClient() *http.Client {
	if a.HTTPClient != nil {
		return a.HTTPClient
	}
	return defaultHTTPClient
}

func (a *Adapter) baseURL() string {
	if strings.TrimSpace(a.BaseURL) != "" {
		return strings.TrimRight(a.BaseURL, "/")
	}
	return DefaultBaseURL
}

func (a *Adapter) clock() time.Time {
	if a.now != nil {
		return a.now()
	}
	return time.Now()
}

// Execute is the dispatch.Adapter entry point.
func (a *Adapter) Execute(ctx context.Context, inv *dispatch.Invocation, rv *dispatch.ResolvedVariant, creds *dispatch.Credentials) (*dispatch.Response, error) {
	if rv == nil || rv.Variant == nil || rv.Variant.Binding == nil || rv.Variant.Binding.HTTP == nil {
		return nil, errors.New("googleads adapter: variant binding missing http block")
	}
	if creds == nil || strings.TrimSpace(creds.Token) == "" {
		return nil, errors.New("googleads adapter: missing OAuth token (run `gum login --service googleads` to authorize the adwords scope)")
	}
	devTok := ""
	if a.DevToken != nil {
		devTok = strings.TrimSpace(a.DevToken())
	}
	if devTok == "" {
		return nil, errors.New("googleads adapter: missing developer token (run `gum auth use-ads-developer-token`, or set GUM_GOOGLE_ADS_DEVELOPER_TOKEN)")
	}

	method := customMethod(rv.Variant.Binding.HTTP.Path)
	if !knownMethods[method] {
		return nil, fmt.Errorf("googleads adapter: unsupported method %q (path %q)", method, rv.Variant.Binding.HTTP.Path)
	}

	customerID, err := requireAccountID(stringArg(inv.Args, "customerId"), "customerId")
	if err != nil {
		return nil, err
	}
	loginCID, err := optionalAccountID(stringArg(inv.Args, "loginCustomerId"), "loginCustomerId")
	if err != nil {
		return nil, err
	}

	body, err := a.buildBody(method, inv.Args)
	if err != nil {
		return nil, err
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("googleads adapter: marshal request body: %w", err)
	}

	url := fmt.Sprintf("%s/customers/%s:%s", a.baseURL(), customerID, method)
	limit := a.MaxResponseBytes
	if limit <= 0 {
		limit = defaultMaxResponseBytes
	}

	// newReq rebuilds the request per attempt so the body reader is fresh on
	// retry. The Bearer + developer-token headers are set here, never logged.
	newReq := func() (*http.Request, error) {
		req, rerr := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
		if rerr != nil {
			return nil, fmt.Errorf("googleads adapter: build request: %w", rerr)
		}
		req.Header.Set("Authorization", "Bearer "+creds.Token)
		req.Header.Set("developer-token", devTok)
		req.Header.Set("Content-Type", "application/json")
		if loginCID != "" {
			req.Header.Set("login-customer-id", loginCID)
		}
		return req, nil
	}

	raw, status, hdr, err := a.doWithRetry(ctx, method, newReq, limit)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, newUpstreamError(status, raw, hdr)
	}
	return &dispatch.Response{
		Body:       raw,
		Format:     "json",
		BytesIn:    len(bodyBytes),
		BytesOut:   len(raw),
		StatusCode: status,
	}, nil
}

// doWithRetry sends the request, retrying on 429 and 5xx with exponential
// backoff that honours the upstream Retry-After. Keyword-planning is capped at
// ~1 QPS per customer, so transient 429s are expected. The body is rebuilt per
// attempt; ctx cancellation aborts the wait.
func (a *Adapter) doWithRetry(ctx context.Context, method string, newReq func() (*http.Request, error), limit int) (raw []byte, status int, hdr http.Header, err error) {
	attempts := a.MaxAttempts
	if attempts <= 0 {
		attempts = defaultMaxAttempts
	}
	for attempt := 1; attempt <= attempts; attempt++ {
		req, rerr := newReq()
		if rerr != nil {
			return nil, 0, nil, rerr
		}
		resp, derr := a.httpClient().Do(req)
		if derr != nil {
			return nil, 0, nil, fmt.Errorf("googleads adapter: %s: %w", method, derr)
		}
		body, rdErr := io.ReadAll(io.LimitReader(resp.Body, int64(limit)+1))
		_ = resp.Body.Close()
		if rdErr != nil {
			return nil, 0, nil, fmt.Errorf("googleads adapter: read response: %w", rdErr)
		}
		if len(body) > limit {
			return nil, 0, nil, fmt.Errorf("googleads adapter: response exceeds %d byte cap (use pageSize/pageToken to paginate)", limit)
		}
		status, hdr, raw = resp.StatusCode, resp.Header, body
		if !isRetryableStatus(status) || attempt == attempts {
			return raw, status, hdr, nil
		}
		select {
		case <-ctx.Done():
			return nil, 0, nil, ctx.Err()
		case <-time.After(a.backoff(attempt, hdr.Get("Retry-After"))):
		}
	}
	return raw, status, hdr, nil
}

func isRetryableStatus(code int) bool {
	return code == http.StatusTooManyRequests || (code >= 500 && code <= 599)
}

// backoff returns the wait before the next attempt: the larger of the
// Retry-After hint and an exponential base (default 500ms, 1s, 2s, …), capped
// at 30s. backoffBase is overridable in tests for speed.
func (a *Adapter) backoff(attempt int, retryAfter string) time.Duration {
	base := a.backoffBase
	if base <= 0 {
		base = defaultBackoffBase
	}
	d := base << (attempt - 1)
	if ra := parseRetryAfter(retryAfter); ra > 0 {
		if raD := time.Duration(ra) * time.Millisecond; raD > d {
			d = raD
		}
	}
	if d > maxBackoff {
		d = maxBackoff
	}
	return d
}

// customMethod extracts the custom-method suffix from a Google Ads REST path,
// e.g. ".../customers/{customerId}:generateKeywordIdeas" -> "generateKeywordIdeas".
func customMethod(path string) string {
	i := strings.LastIndex(path, ":")
	if i < 0 || i == len(path)-1 {
		return ""
	}
	return path[i+1:]
}

// buildBody assembles the JSON request body for method. When the caller passes
// a raw `body` object it is used verbatim (advanced escape hatch — required for
// fully custom forecast campaigns); otherwise the body is assembled from the
// ergonomic top-level args.
func (a *Adapter) buildBody(method string, args map[string]any) (map[string]any, error) {
	if raw, ok := rawBodyArg(args); ok {
		return raw, nil
	}
	switch method {
	case "generateKeywordIdeas":
		return a.ideasBody(args)
	case "generateKeywordHistoricalMetrics":
		return a.historicalBody(args)
	case "generateKeywordForecastMetrics":
		return a.forecastBody(args)
	default:
		return nil, fmt.Errorf("googleads adapter: unsupported method %q", method)
	}
}

func (a *Adapter) ideasBody(args map[string]any) (map[string]any, error) {
	network, err := networkArg(args)
	if err != nil {
		return nil, err
	}
	body := map[string]any{"keywordPlanNetwork": network}
	if geos := geoConstantsArg(args); len(geos) > 0 {
		body["geoTargetConstants"] = geos
	}
	if lang := languageConstantArg(args); lang != "" {
		body["language"] = lang
	}
	if boolArg(args, "includeAdultKeywords") {
		body["includeAdultKeywords"] = true
	}
	if n := intArg(args, "pageSize"); n > 0 {
		body["pageSize"] = n
	}
	if tok := stringArg(args, "pageToken"); tok != "" {
		body["pageToken"] = tok
	}

	keywords := stringSliceArg(args, "keywords")
	if len(keywords) > maxIdeaSeedKeywords {
		return nil, fmt.Errorf("googleads adapter: generateKeywordIdeas accepts at most %d seed keywords, got %d", maxIdeaSeedKeywords, len(keywords))
	}
	url := stringArg(args, "url")
	switch {
	case len(keywords) > 0 && url != "":
		body["keywordAndUrlSeed"] = map[string]any{"url": url, "keywords": keywords}
	case len(keywords) > 0:
		body["keywordSeed"] = map[string]any{"keywords": keywords}
	case url != "":
		body["urlSeed"] = map[string]any{"url": url}
	default:
		return nil, errors.New("googleads adapter: generateKeywordIdeas needs `keywords` and/or `url`")
	}
	return body, nil
}

func (a *Adapter) historicalBody(args map[string]any) (map[string]any, error) {
	keywords := stringSliceArg(args, "keywords")
	if len(keywords) == 0 {
		return nil, errors.New("googleads adapter: generateKeywordHistoricalMetrics needs `keywords`")
	}
	if len(keywords) > maxKeywords {
		return nil, fmt.Errorf("googleads adapter: generateKeywordHistoricalMetrics accepts at most %d keywords, got %d", maxKeywords, len(keywords))
	}
	network, err := networkArg(args)
	if err != nil {
		return nil, err
	}
	body := map[string]any{
		"keywords":           keywords,
		"keywordPlanNetwork": network,
	}
	if geos := geoConstantsArg(args); len(geos) > 0 {
		body["geoTargetConstants"] = geos
	}
	if lang := languageConstantArg(args); lang != "" {
		body["language"] = lang
	}
	return body, nil
}

func (a *Adapter) forecastBody(args map[string]any) (map[string]any, error) {
	keywords := stringSliceArg(args, "keywords")
	if len(keywords) == 0 {
		return nil, errors.New("googleads adapter: generateKeywordForecastMetrics needs `keywords` (or pass a raw `body`)")
	}
	if len(keywords) > maxKeywords {
		return nil, fmt.Errorf("googleads adapter: generateKeywordForecastMetrics accepts at most %d keywords, got %d", maxKeywords, len(keywords))
	}
	maxCPC := int64Arg(args, "maxCpcMicros")
	if maxCPC <= 0 {
		maxCPC = 1_000_000 // $1.00 default
	}
	matchType := strings.ToUpper(strings.TrimSpace(stringArg(args, "matchType")))
	if matchType == "" {
		matchType = "BROAD"
	} else if !validMatchTypes[matchType] {
		return nil, fmt.Errorf("googleads adapter: matchType %q invalid (want BROAD, PHRASE, or EXACT)", matchType)
	}
	// ForecastAdGroup.keywords is a list of KeywordInfo ({text, matchType}); the
	// per-ad-group/per-keyword bid is NOT set here — the bid comes from the
	// campaign-level manualCpcBiddingStrategy (see Google's official example).
	kwInfos := make([]map[string]any, 0, len(keywords))
	for _, kw := range keywords {
		kwInfos = append(kwInfos, map[string]any{"text": kw, "matchType": matchType})
	}
	campaign := map[string]any{
		"biddingStrategy": map[string]any{
			"manualCpcBiddingStrategy": map[string]any{"maxCpcBidMicros": maxCPC},
		},
		"adGroups": []map[string]any{
			{"keywords": kwInfos},
		},
	}
	if geos := geoConstantsArg(args); len(geos) > 0 {
		campaign["geoTargetConstants"] = geos
	}
	if lang := languageConstantArg(args); lang != "" {
		campaign["languageConstants"] = []string{lang}
	}

	// forecastPeriod is a DateRange carrying startDate/endDate directly (no
	// nested dateRange wrapper). Both default to a future 1..30 day window.
	start := stringArg(args, "forecastStartDate")
	end := stringArg(args, "forecastEndDate")
	if start == "" || end == "" {
		now := a.clock()
		start = now.AddDate(0, 0, 1).Format("2006-01-02")
		end = now.AddDate(0, 0, 30).Format("2006-01-02")
	} else {
		st, serr := time.Parse("2006-01-02", start)
		if serr != nil {
			return nil, fmt.Errorf("googleads adapter: forecastStartDate %q must be YYYY-MM-DD", start)
		}
		et, eerr := time.Parse("2006-01-02", end)
		if eerr != nil {
			return nil, fmt.Errorf("googleads adapter: forecastEndDate %q must be YYYY-MM-DD", end)
		}
		if et.Before(st) {
			return nil, fmt.Errorf("googleads adapter: forecastEndDate %q is before forecastStartDate %q", end, start)
		}
	}
	return map[string]any{
		"campaign":       campaign,
		"forecastPeriod": map[string]any{"startDate": start, "endDate": end},
	}, nil
}

// ── arg coercion helpers ─────────────────────────────────────────────────────

func rawBodyArg(args map[string]any) (map[string]any, bool) {
	v, ok := args["body"]
	if !ok {
		return nil, false
	}
	switch b := v.(type) {
	case map[string]any:
		if len(b) == 0 {
			return nil, false
		}
		return b, true
	case string:
		s := strings.TrimSpace(b)
		if s == "" {
			return nil, false
		}
		var m map[string]any
		if json.Unmarshal([]byte(s), &m) == nil && len(m) > 0 {
			return m, true
		}
	}
	return nil, false
}

func stringArg(args map[string]any, key string) string {
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func boolArg(args map[string]any, key string) bool {
	switch v := args[key].(type) {
	case bool:
		return v
	case string:
		b, _ := strconv.ParseBool(strings.TrimSpace(v))
		return b
	}
	return false
}

func intArg(args map[string]any, key string) int {
	return int(int64Arg(args, key))
}

func int64Arg(args map[string]any, key string) int64 {
	switch v := args[key].(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	case json.Number:
		n, _ := v.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return n
	}
	return 0
}

// stringSliceArg accepts a JSON array (from MCP) or a comma-separated string
// (CLI convenience), returning trimmed non-empty elements.
func stringSliceArg(args map[string]any, key string) []string {
	v, ok := args[key]
	if !ok {
		return nil
	}
	var out []string
	switch t := v.(type) {
	case []string:
		out = append(out, t...)
	case []any:
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
	case string:
		out = strings.Split(t, ",")
	}
	res := make([]string, 0, len(out))
	for _, s := range out {
		if s = strings.TrimSpace(s); s != "" {
			res = append(res, s)
		}
	}
	return res
}

// networkArg returns the keywordPlanNetwork enum, defaulting to GOOGLE_SEARCH,
// and fails closed on an unrecognized value.
func networkArg(args map[string]any) (string, error) {
	n := strings.ToUpper(strings.TrimSpace(stringArg(args, "keywordPlanNetwork")))
	if n == "" {
		return "GOOGLE_SEARCH", nil
	}
	if !validNetworks[n] {
		return "", fmt.Errorf("googleads adapter: keywordPlanNetwork %q invalid (want GOOGLE_SEARCH or GOOGLE_SEARCH_AND_PARTNERS)", n)
	}
	return n, nil
}

// geoConstantsArg normalizes geo targets to resource names. Accepts bare
// criterion ids ("2840"), full resource names ("geoTargetConstants/2840"), an
// array, or CSV. Empty input yields nil (Ads then defaults to all locations).
func geoConstantsArg(args map[string]any) []string {
	raw := stringSliceArg(args, "geoTargetConstants")
	out := make([]string, 0, len(raw))
	for _, g := range raw {
		if strings.Contains(g, "/") {
			out = append(out, g)
		} else {
			out = append(out, "geoTargetConstants/"+g)
		}
	}
	return out
}

// languageConstantArg normalizes a single language to a resource name. Accepts
// a bare id ("1000") or a full resource name ("languageConstants/1000").
func languageConstantArg(args map[string]any) string {
	l := stringArg(args, "language")
	if l == "" {
		return ""
	}
	if strings.Contains(l, "/") {
		return l
	}
	return "languageConstants/" + l
}

// requireAccountID validates a mandatory 10-digit account id (dashes/spaces
// allowed), attributing errors to the named field.
func requireAccountID(s, field string) (string, error) {
	id, err := normalizeCustomerID(s)
	if err != nil {
		return "", fmt.Errorf("googleads adapter: `%s` must be 10 digits (dashes allowed)", field)
	}
	if id == "" {
		return "", fmt.Errorf("googleads adapter: `%s` is required (the 10-digit account id)", field)
	}
	if len(id) != 10 {
		return "", fmt.Errorf("googleads adapter: `%s` must be exactly 10 digits", field)
	}
	return id, nil
}

// optionalAccountID validates an optional 10-digit account id. Empty input
// returns ("", nil); a present-but-malformed id is an error (not silently
// dropped) so a typo in login-customer-id surfaces instead of a confusing
// upstream permission error.
func optionalAccountID(s, field string) (string, error) {
	id, err := normalizeCustomerID(s)
	if err != nil {
		return "", fmt.Errorf("googleads adapter: `%s` must be 10 digits (dashes allowed)", field)
	}
	if id == "" {
		return "", nil
	}
	if len(id) != 10 {
		return "", fmt.Errorf("googleads adapter: `%s` must be exactly 10 digits", field)
	}
	return id, nil
}

// normalizeCustomerID strips dashes/spaces and verifies the result is all
// digits. Empty input returns ("", nil) so optional ids (login-customer-id)
// can be skipped; a non-empty malformed id returns an error.
func normalizeCustomerID(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	var b strings.Builder
	for _, r := range s {
		if r == '-' || r == ' ' {
			continue
		}
		if r < '0' || r > '9' {
			return "", fmt.Errorf("googleads adapter: customer id %q must be 10 digits (dashes allowed)", s)
		}
		b.WriteRune(r)
	}
	id := b.String()
	if id == "" {
		return "", fmt.Errorf("googleads adapter: customer id %q has no digits", s)
	}
	return id, nil
}
