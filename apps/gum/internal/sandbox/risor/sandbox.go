// Package risor wraps the in-process Risor v2 runtime for gum.code (spec.md §6.3).
//
// Only Risor is supported in v0.1.0. Builtins: gum_call, gum_search,
// gum_confirm_destructive, gum_print.
package risor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	risorlib "github.com/deepnoodle-ai/risor/v2"
	"github.com/deepnoodle-ai/risor/v2/pkg/object"
)

// ErrStepLimitExceeded is returned by Run when a script executes more VM
// instructions than Options.MaxSteps allows. It is the deterministic
// CPU-exhaustion guard: the wall-clock ScriptTimeout backstops a script that
// blocks on I/O, but a tight compute loop that never yields is bounded by the
// step ceiling regardless of host speed. Callers may errors.Is against it to
// distinguish a resource-limit abort from a script syntax/runtime error.
var ErrStepLimitExceeded = errors.New("sandbox: step limit exceeded")

// DefaultMaxSteps is the instruction budget applied when Options.MaxSteps is 0.
// 100M VM steps is far above any legitimate orchestration script (gum.code
// scripts are I/O-bound on gum_call, not compute-bound) yet bounds a runaway
// loop long before the wall-clock timeout would let it pin a core.
const DefaultMaxSteps int64 = 100_000_000

// DefaultOutputLimitBytes is the spec §6.1 cumulative output budget applied
// when Options.OutputLimitBytes is 0: every gum_print plus the JSON projection
// of the script's return value is charged against this one counter.
const DefaultOutputLimitBytes = 4096

// MaxPrintBytesPerCall is the spec §6.3 ceiling on a single gum_print. The
// cumulative budget is authoritative, so this only binds when
// Options.OutputLimitBytes is raised above it.
const MaxPrintBytesPerCall = 4096

// OutputLimitError reports that the script's return value did not fit in what
// the gum_print calls left of the cumulative output budget (spec §6.1). The
// adapter maps it to the CODE_OUTPUT_LIMIT_EXCEEDED structured error; the
// printed bytes are discarded with it, because the spec returns the envelope
// in place of the whole result rather than alongside a partial one.
type OutputLimitError struct {
	LimitBytes   int
	PrintedBytes int
	ValueBytes   int
}

func (e *OutputLimitError) Error() string {
	return fmt.Sprintf("sandbox: code output limit exceeded: budget %d bytes, printed %d, return value %d",
		e.LimitBytes, e.PrintedBytes, e.ValueBytes)
}

// Budget reports what the §6.1 cumulative output budget has left at the moment
// it is asked. Run binds it before the script starts, so a global the caller
// injected can size its own output against what gum_print has already spent.
//
// It exists for the §9.0.1 batch ceiling: gum_parallel has to know the live
// remainder to decide which result elements it must truncate, and only the
// sandbox owns that counter. A Budget that was never passed to Run reports
// ok=false rather than 0, so a caller that forgot to wire one skips its
// accounting instead of truncating everything.
//
// Remaining is read from the script goroutine, the same one that runs
// gum_print, so the counter needs no lock.
type Budget struct {
	remaining func() int
}

// Remaining returns the unspent budget in bytes, and whether this Budget is
// bound to a running sandbox at all. The count never goes negative: gum_print
// clamps every write to what the budget has left, and an oversized return
// value raises OutputLimitError instead of being written.
func (b *Budget) Remaining() (int, bool) {
	if b == nil || b.remaining == nil {
		return 0, false
	}
	return b.remaining(), true
}

// Options configures a single sandbox Run.
type Options struct {
	AllowWrite       bool
	AllowDestructive bool
	// Globals are injected into the Risor environment (e.g. gum_print, gum_call,
	// gum_search, gum_confirm_destructive). Caller must inject these to enable them;
	// if absent, defaults that panic are installed.
	Globals       map[string]any
	ScriptTimeout time.Duration // 0 → default 60s wall-clock backstop
	MaxSteps      int64         // VM instruction ceiling; 0 → DefaultMaxSteps
	// OutputLimitBytes is the §6.1 cumulative output budget shared by every
	// gum_print and the script's return value; 0 → DefaultOutputLimitBytes.
	OutputLimitBytes int
	// Budget, when non-nil, is bound by Run to the live OutputLimitBytes
	// counter so injected globals can read the remainder (§9.0.1).
	Budget            *Budget
	HTTPTimeout       time.Duration // 0 → default 30s for gum_http_get
	AllowInsecureHTTP bool          // if true, gum_http_get allows http:// URLs (test only)

	// AllowPrivateEgress disables the SSRF guard that refuses to dial private,
	// loopback, and link-local addresses (gum-j1ly). Production leaves this
	// false so a plugin-declared AllowedHosts entry — whether a literal private
	// IP or a hostname that resolves into an internal range — cannot pivot the
	// sandbox into the host network (e.g. 169.254.169.254 cloud metadata). Set
	// true only to reach loopback httptest servers in tests.
	AllowPrivateEgress bool

	// AllowedHosts extends the default egress allowlist (*.googleapis.com) with
	// plugin-declared hosts (spec §11). Each entry is matched as exact lowercase
	// host name unless it begins with "*." which is a wildcard subdomain rule:
	// "*.example.com" matches "foo.example.com" and "a.b.example.com" but NOT
	// "example.com" itself.
	AllowedHosts []string
}

// Output is the result of a successful sandbox Run.
type Output struct {
	Printed []byte // all bytes written via gum_print, capped at Options.OutputLimitBytes
	Value   any    // last evaluated expression value; may be nil

	// Truncated reports that at least one gum_print lost bytes to the
	// cumulative budget or the per-call ceiling. The adapter projects it as
	// _expression._code_output_truncated (spec §6.1).
	Truncated bool
}

// Run compiles and executes source in a Risor sandbox with the given options.
// The context deadline / cancellation is propagated into the Risor VM.
func Run(ctx context.Context, source string, opts Options) (*Output, error) {
	if opts.ScriptTimeout <= 0 {
		opts.ScriptTimeout = 60 * time.Second
	}
	// Use <= 0, not == 0: Risor silently SKIPS its step cap for a non-positive
	// value, so a negative MaxSteps would remove the instruction ceiling and
	// leave only the wall-clock timeout. Treat any non-positive value as "unset"
	// (applied to all three limits for consistency).
	if opts.MaxSteps <= 0 {
		opts.MaxSteps = DefaultMaxSteps
	}
	// <= 0 rather than == 0 for the same reason as MaxSteps: a negative budget
	// would otherwise read as "no ceiling" instead of "unset".
	if opts.OutputLimitBytes <= 0 {
		opts.OutputLimitBytes = DefaultOutputLimitBytes
	}

	childCtx, cancel := context.WithTimeout(ctx, opts.ScriptTimeout)
	defer cancel()

	var buf bytes.Buffer
	outputLimit := opts.OutputLimitBytes
	truncated := false

	if opts.Budget != nil {
		opts.Budget.remaining = func() int { return outputLimit - buf.Len() }
	}

	// Build globals map from caller-provided globals.
	globals := make(map[string]any)
	for k, v := range opts.Globals {
		globals[k] = v
	}

	// Extract caller-provided gum_print (if any) to chain-call it.
	callerGumPrint := globals["gum_print"]

	// Always inject the sandbox's gum_print that writes to buf.
	// If the caller provided their own gum_print, call it too (for side effects).
	//
	// This is a native Risor builtin rather than a func(any) any global for two
	// reasons. Risor converts a Go `any` parameter through reflect.ValueOf, and
	// a nil argument yields the zero Value, so `gum_print(nil)` aborted the whole
	// script with "reflect: Call using zero Value argument". And the object form
	// keeps the Risor value intact until printValue decides how to encode it.
	globals["gum_print"] = object.NewBuiltin("gum_print", func(_ context.Context, args ...object.Object) (object.Object, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("gum_print: want 1 argument, got %d", len(args))
		}
		value := risorObjectToGo(args[0])
		b := []byte(printValue(value))
		// Two ceilings apply at once: what this call may print and what the
		// cumulative budget has left. The tighter one wins.
		allowed := MaxPrintBytesPerCall
		if remaining := outputLimit - buf.Len(); remaining < allowed {
			allowed = remaining
		}
		if len(b) > allowed {
			kept := TruncateToUTF8Boundary(b, allowed)
			// An empty print against an exhausted budget drops nothing, so it
			// must not raise the flag.
			if len(kept) < len(b) {
				truncated = true
			}
			b = kept
		}
		buf.Write(b)
		// Also invoke the caller's version if one was provided.
		if callerGumPrint != nil {
			if fn, ok := callerGumPrint.(func(any) any); ok {
				fn(value)
			} else if fn, ok := callerGumPrint.(func(string)); ok {
				fn(fmt.Sprint(value))
			}
		}
		return object.Nil, nil
	})

	// Install default panic stubs for caller-injectable builtins if not provided.
	if _, ok := globals["gum_call"]; !ok {
		globals["gum_call"] = func(opID any, args any) any {
			panic("not implemented: gum_call not wired by caller")
		}
	}
	if _, ok := globals["gum_search"]; !ok {
		globals["gum_search"] = func(query any, topK any) any {
			panic("not implemented: gum_search not wired by caller")
		}
	}
	if _, ok := globals["gum_confirm_destructive"]; !ok {
		globals["gum_confirm_destructive"] = func(opID any, args any, purpose any) any {
			panic("not implemented: gum_confirm_destructive not wired by caller")
		}
	}
	httpTimeout := opts.HTTPTimeout
	if httpTimeout == 0 {
		httpTimeout = 30 * time.Second
	}
	allowInsecureHTTP := opts.AllowInsecureHTTP
	// Build the effective egress allowlist: spec-mandated default *.googleapis.com
	// plus any plugin-declared hosts from Options.AllowedHosts (spec §11). Callers
	// that need to reach loopback or other hosts (e.g. test httptest servers)
	// must add them explicitly via AllowedHosts.
	egressAllowlist := append([]string{"*.googleapis.com"}, opts.AllowedHosts...)

	if _, ok := globals["gum_http_get"]; !ok {
		globals["gum_http_get"] = func(rawURL any) (any, error) {
			urlStr := fmt.Sprint(rawURL)

			// Step 1: Parse URL.
			parsed, err := url.Parse(urlStr)
			if err != nil {
				return nil, fmt.Errorf("gum_http_get: parse URL: %w", err)
			}

			// Step 2: Scheme check.
			scheme := strings.ToLower(parsed.Scheme)
			switch scheme {
			case "https":
				// always allowed
			case "http":
				if !allowInsecureHTTP {
					return nil, fmt.Errorf("gum_http_get: insecure http:// URL rejected; use https instead")
				}
			default:
				return nil, fmt.Errorf("gum_http_get: unsupported URL scheme %q; only https (or http with AllowInsecureHTTP) is permitted", scheme)
			}

			// Step 3: Host allowlist check.
			host := strings.ToLower(parsed.Hostname())
			if !matchHostAllowlist(host, egressAllowlist) {
				return nil, fmt.Errorf("gum_http_get: EGRESS_HOST_DENIED: host %q is not in the egress allowlist (default: *.googleapis.com; extend via plugin-declared hosts)", host)
			}

			// Step 4: Build and execute request.
			// Use a synchronous RoundTripper that dials in the caller's goroutine
			// (never spawns background goroutines) to avoid goleak false-positives.
			// http.Transport intentionally detaches its dial goroutines from the
			// request context via context.WithoutCancel (see transport.go getConn).
			// Our syncDialTransport dials synchronously within the request context.
			reqCtx, reqCancel := context.WithTimeout(childCtx, httpTimeout)
			defer reqCancel()
			transport := &syncDialTransport{allowPrivateEgress: opts.AllowPrivateEgress}
			client := &http.Client{
				Transport: transport,
				CheckRedirect: func(req *http.Request, _ []*http.Request) error {
					redirectHost := strings.ToLower(req.URL.Hostname())
					if !matchHostAllowlist(redirectHost, egressAllowlist) {
						return fmt.Errorf("gum_http_get: EGRESS_HOST_DENIED: redirect host %q is not in the egress allowlist (default: *.googleapis.com; extend via plugin-declared hosts)", redirectHost)
					}
					return nil
				},
			}
			req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, urlStr, nil)
			if err != nil {
				return nil, fmt.Errorf("gum_http_get: build request: %w", err)
			}
			resp, err := client.Do(req)
			if err != nil {
				return nil, fmt.Errorf("gum_http_get: request: %w", err)
			}
			defer func() { _ = resp.Body.Close() }()

			const maxBody = 1 << 20 // 1 MiB
			lr := io.LimitReader(resp.Body, maxBody+1)
			body, err := io.ReadAll(lr)
			if err != nil {
				return nil, fmt.Errorf("gum_http_get: read body: %w", err)
			}
			if len(body) > maxBody {
				return nil, fmt.Errorf("gum_http_get: response body exceeds 1 MiB limit")
			}

			headers := make(map[string]any, len(resp.Header))
			for k, vs := range resp.Header {
				if len(vs) > 0 {
					headers[k] = vs[0]
				}
			}

			return map[string]any{
				"status_code": resp.StatusCode,
				"body":        string(body),
				"headers":     headers,
			}, nil
		}
	}

	// Propagate AllowWrite / AllowDestructive as script-visible bool globals.
	globals["gum_allow_write"] = opts.AllowWrite
	globals["gum_allow_destructive"] = opts.AllowDestructive

	rawResult, err := risorlib.Eval(childCtx, source,
		risorlib.WithEnv(globals),
		risorlib.WithRawResult(),
		risorlib.WithMaxSteps(opts.MaxSteps),
	)
	if err != nil {
		// Distinguish cancellation/deadline from script errors.
		if childCtx.Err() != nil {
			return nil, fmt.Errorf("sandbox: script %w", childCtx.Err())
		}
		// Surface the step-limit abort as a typed sentinel so callers can
		// errors.Is it (CPU-exhaustion guard) rather than parse the message.
		if errors.Is(err, risorlib.ErrStepLimitExceeded) {
			return nil, fmt.Errorf("%w (max_steps=%d)", ErrStepLimitExceeded, opts.MaxSteps)
		}
		return nil, fmt.Errorf("sandbox: risor eval: %w", err)
	}

	// Convert the Risor object to a Go value. We use risorObjectToGo instead
	// of object.Interface() so that Risor Int objects are returned as Go int
	// (preserving the original type from Go function returns) rather than int64.
	var value any
	if obj, ok := rawResult.(object.Object); ok {
		value = risorObjectToGo(obj)
	}

	// The return value is charged against whatever the prints left. §6.1 makes
	// the envelope replace the result rather than accompany it, so this is an
	// error return and not a flag on Output.
	valueBytes := 0
	if value != nil {
		valueBytes = len(printValue(value))
	}
	if buf.Len()+valueBytes > outputLimit {
		return nil, &OutputLimitError{
			LimitBytes:   outputLimit,
			PrintedBytes: buf.Len(),
			ValueBytes:   valueBytes,
		}
	}

	return &Output{
		Printed:   buf.Bytes(),
		Value:     value,
		Truncated: truncated,
	}, nil
}

// matchHostAllowlist reports whether host (lowercase, no port) matches any
// entry in the allowlist. Entries are exact matches unless they begin with "*."
// in which case they match one or more subdomain labels but NOT the bare apex.
//
// Rules:
//   - All comparisons are case-insensitive (caller should lowercase host).
//   - "example.com" matches only "example.com".
//   - "*.example.com" matches "foo.example.com" and "a.b.example.com" but NOT "example.com".
//   - Empty, "*", or "*." entries never match anything.
func matchHostAllowlist(host string, allowlist []string) bool {
	h := strings.ToLower(host)
	for _, entry := range allowlist {
		e := strings.ToLower(entry)
		if e == "" || e == "*" || e == "*." {
			continue
		}
		if strings.HasPrefix(e, "*.") {
			// Wildcard: strip "*." and require host to end with ".<suffix>"
			// with at least one character before the dot.
			// e != "*." is guaranteed by the guard above, so the suffix is
			// always non-empty here.
			suffix := e[2:] // e.g. "example.com"
			// host must be "<something>.<suffix>" where <something> is non-empty.
			required := "." + suffix
			if strings.HasSuffix(h, required) && len(h) > len(required) {
				return true
			}
		} else {
			// Exact match.
			if h == e {
				return true
			}
		}
	}
	return false
}

// printValue renders one gum_print argument for the response body.
//
// A string prints verbatim, so a script that assembles its own text is not
// handed back a quoted copy of it. Everything else is JSON: the printed stream
// is the gum.code response body, which the MCP layer hands to a client as the
// §13 `data` member, and Go's default rendering of a map ("map[a:1 b:x]") is
// not parseable by anything. The canonical §6.3 line
// `gum_print(gum_parallel([...]))` produced exactly that dump.
//
// HTML escaping is off because this is a response body, not an HTML document;
// with it on, a printed URL query string came back full of \u0026. A value JSON
// cannot encode falls back to the Go rendering rather than losing the print.
func printValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return fmt.Sprint(v)
	}
	return strings.TrimSuffix(buf.String(), "\n")
}

// risorObjectToGo converts a Risor object to a native Go value. It mirrors
// Risor's default Interface() conversion (Map→map[string]any, List→[]any,
// String→string, Bool→bool, NilType→nil) but maps Risor Int to Go int
// (not int64) so that callers can assert .(int) on numeric results such as
// HTTP status codes returned by gum_http_get.
//
// For types with no Go equivalent (closures, modules, etc.), it falls back to
// the Risor object's Inspect() string representation, matching Risor's default.
func risorObjectToGo(obj object.Object) any {
	if obj == nil {
		return nil
	}
	switch v := obj.(type) {
	case *object.NilType:
		return nil
	case *object.Int:
		return int(v.Interface().(int64)) // preserve as int, not int64
	case *object.Float:
		return v.Interface()
	case *object.String:
		return v.Interface()
	case *object.Bool:
		return v.Interface()
	case *object.Map:
		goMap := make(map[string]any)
		for k, val := range v.Value() {
			goMap[k] = risorObjectToGo(val)
		}
		return goMap
	case *object.List:
		vals := v.Value()
		items := make([]any, len(vals))
		for i, val := range vals {
			items[i] = risorObjectToGo(val)
		}
		return items
	default:
		// For closures, modules, and other types without a Go equivalent,
		// match Risor's fallback: use the string representation.
		iface := obj.Interface()
		if iface == nil {
			return obj.Inspect()
		}
		return iface
	}
}

// TruncateToUTF8Boundary truncates b to at most maxBytes, always ending on a
// valid UTF-8 rune boundary. The result is guaranteed to be valid UTF-8.
func TruncateToUTF8Boundary(b []byte, maxBytes int) []byte {
	if maxBytes <= 0 {
		return b[:0]
	}
	if len(b) <= maxBytes {
		return b
	}
	// Walk back from maxBytes until we find a valid rune start.
	i := maxBytes
	for i > 0 && !utf8.RuneStart(b[i]) {
		i--
	}
	return b[:i]
}
