package dispatch

import (
	"encoding/json"
	"net/http"

	"github.com/ehmo/gum/internal/cache"
	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/output/gain"
)

// http_etag.go carries the spec §10.2 HTTP/ETag revalidation cache through the
// invocation lifecycle.
//
// §10.3 (the semantic cache) answers "have I served this exact call already?"
// with no network at all, so it runs first. §10.2 answers the different
// question "has the upstream copy changed?", and it costs one conditional
// request: gum sends the stored validator as `If-None-Match` and upstream
// either returns 304 with no body or the full new representation.
//
// A 304 is not a 4xx. §2024 makes it a success that short-circuits the whole
// expression pipeline: stages 1-8 do not run, no field mask is applied, no tee
// artifact is written, and no gum://results handle is minted.

// cacheStatusETag304 is the §12.3 `cache_status` value for a revalidated read.
// §2030 names one field, `served_from_cache`, but the v1 ledger splits that
// into a string status and a boolean, so a 304 row sets both: the status
// string here and `served_from_cache: true`, because the body the caller
// receives came from the cache and not from the wire.
const cacheStatusETag304 = "etag_304"

// httpCacheable reports whether this call may revalidate through §10.2. Only
// read-class variants qualify, for the reason §10.3 excludes writes: a stored
// answer must never stand in for an effect that did not run. A resolved
// variant is required because its id is part of the key.
func (d *dispatcher) httpCacheable(rv *ResolvedVariant) bool {
	if d.httpCache == nil || rv == nil || rv.Variant == nil {
		return false
	}
	return rv.Variant.RiskClass == catalog.RiskClassRead
}

// httpCacheKey builds the §10.2 key. Both prerequisites the spec names are
// already met at every call site: §3.1 step 2 resolved the variant and
// §10.0.1 resolved the credential subject.
func (d *dispatcher) httpCacheKey(inv *Invocation, rv *ResolvedVariant, creds *Credentials) string {
	return cache.HTTPKey(
		inv.OpID,
		rv.Variant.VariantID,
		canonicalizeArgs(d.canonicalArgs(inv.Args)),
		semanticAuthFP(inv, creds),
	)
}

// notModifiedResult is the §2029 body. It is a struct rather than a map so the
// two keys marshal in the order the spec prints them.
type notModifiedResult struct {
	Unchanged bool   `json:"unchanged"`
	ETag      string `json:"etag"`
}

// serveNotModified completes a dispatch upstream answered with 304.
//
// The returned ShapedResponse carries no Expression envelope: §2029 omits
// `_expression` because no shaped output exists to describe, and exempts this
// body from outputSchema conformance for the same reason. The caller sees the
// validator and nothing else, whatever expression profile is now active
// (§2031 item 4).
func (d *dispatcher) serveNotModified(inv *Invocation, rv *ResolvedVariant, creds *Credentials, entry cache.HTTPEntry) (*ShapedResponse, error) {
	result := notModifiedResult{Unchanged: true, ETag: entry.ETag}
	body, err := json.Marshal(result)
	if err != nil {
		// Two ASCII fields cannot fail to marshal; keep the dispatch alive
		// rather than turn a saved round trip into an error.
		body = []byte(`{"unchanged":true,"etag":""}`)
	}
	shaped := &ShapedResponse{
		Body:              body,
		Format:            "json",
		StructuredContent: map[string]any{"unchanged": true, "etag": entry.ETag},
	}

	d.appendSuccessAudit(inv, rv)
	if d.gainLedger == nil {
		return shaped, nil
	}
	if err := d.gainLedger.Append(d.notModifiedGainEntry(inv, rv, creds, shaped, entry)); err != nil {
		d.log().Warn("gain ledger append failed", "op_id", inv.OpID, "err", err)
	}
	return shaped, nil
}

// notModifiedGainEntry builds the §2030 ledger row for a 304.
//
// The savings math in output/gain/report.go takes raw_tokens as the baseline
// and shaped_tokens as the actual, so the cached body is what raw_tokens must
// measure: that is the response the naive oracle would have received, which is
// what makes the row contribute positively. response_tokens is 0 because no
// response body crossed the wire, and request_tokens includes the validator
// gum put on it.
func (d *dispatcher) notModifiedGainEntry(inv *Invocation, rv *ResolvedVariant, creds *Credentials, shaped *ShapedResponse, entry cache.HTTPEntry) gain.Entry {
	cached := &Response{Body: entry.Body, Format: entry.Format}
	e := d.buildGainEntry(inv, rv, creds, shaped, cached, false, true)
	e.CacheStatus = cacheStatusETag304
	e.ServedFromCache = true
	e.ResponseTokens = 0
	e.RequestTokens += measureGainTokens([]byte("If-None-Match: " + entry.ETag))
	return e
}

// isNotModified reports whether resp is upstream's answer to a conditional
// request gum sent. A 304 with no validator in hand cannot be served, since
// there is no cached body behind it, so the caller falls through and the
// empty response surfaces as it would from any other adapter.
func isNotModified(resp *Response, validator cache.HTTPEntry) bool {
	return resp != nil && resp.StatusCode == http.StatusNotModified && validator.ETag != ""
}
