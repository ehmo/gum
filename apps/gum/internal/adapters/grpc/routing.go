package grpc

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/ehmo/gum/internal/catalog"
)

// routingParamsHeader is the canonical gRPC metadata key for dynamic routing
// parameters per AIP-4222.
const routingParamsHeader = "x-goog-request-params"

// routingHeaderValue builds the x-goog-request-params value for a variant from
// its binding's routing_headers list and the invocation args, per the
// catalog-abi.md `routing_headers` invariant.
//
// Rule 2's runtime half: a listed field that is absent or empty contributes
// nothing. A header parameter with an empty value would tell the router the
// field was set to "", which routes differently from an unset field. Rule 3's
// runtime half: parameters keep declaration order, so the emitted value is
// byte-stable for a given arg set.
//
// An empty return means the caller emits no header at all.
func routingHeaderValue(binding *catalog.Binding, args map[string]any) string {
	if binding == nil || len(binding.RoutingHeaders) == 0 {
		return ""
	}

	var params []string
	for _, path := range binding.RoutingHeaders {
		value, ok := routingArgValue(args, path)
		if !ok {
			continue
		}
		params = append(params, url.QueryEscape(path)+"="+url.QueryEscape(value))
	}

	return strings.Join(params, "&")
}

// routingArgValue resolves a dotted field path against the invocation args and
// renders it as a header parameter value. It reports false when the path is
// absent, when a segment is not an object, or when the value is empty.
func routingArgValue(args map[string]any, path string) (string, bool) {
	var cur any = args
	for _, segment := range strings.Split(path, ".") {
		obj, ok := cur.(map[string]any)
		if !ok {
			return "", false
		}
		cur, ok = obj[segment]
		if !ok {
			return "", false
		}
	}
	return scalarRoutingValue(cur)
}

// scalarRoutingValue renders one resolved arg as a routing parameter value.
// Only scalars route: a composite value has no single canonical rendering, and
// AIP-4222 parameters are flat strings.
func scalarRoutingValue(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		return t, t != ""
	case bool:
		return strconv.FormatBool(t), true
	case int:
		return strconv.Itoa(t), true
	case int64:
		return strconv.FormatInt(t, 10), true
	case float64:
		// JSON numbers decode to float64; render whole values without a
		// trailing ".0" so a page size reads as "50", not "50.0".
		return strconv.FormatFloat(t, 'f', -1, 64), true
	}
	return "", false
}
