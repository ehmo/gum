// Package dispatch — structured error envelope (spec.md §7, §4.1, §3.1).
//
// ErrorCode constants are the stable runtime error codes defined in spec §7.
// StructuredError implements error and json.Marshaler; Detail fields are flattened
// into the top-level JSON object (never nested under a "detail" key).
package dispatch

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// ErrorCode is the stable string discriminator for structured errors (spec §7).
type ErrorCode string

// All 32 stable runtime error codes (spec §7), grouped by category.
// TestErrorCodeHeaderCountIsCurrent pins the count; TestErrorCodesMatchSpecList
// pins the set against the spec's enumerated list.
const (
	// Parse / dispatch resolution
	ErrCodeOpNotFound       ErrorCode = "OP_NOT_FOUND"
	ErrCodeInvalidArgs      ErrorCode = "INVALID_ARGS"
	ErrCodeAmbiguousVariant ErrorCode = "AMBIGUOUS_VARIANT"
	ErrCodeVariantNotFound  ErrorCode = "VARIANT_NOT_FOUND"
	ErrCodeResourceNotFound ErrorCode = "RESOURCE_NOT_FOUND"

	// Policy / risk
	ErrCodeRiskToolMismatch          ErrorCode = "RISK_TOOL_MISMATCH"
	ErrCodeRequiresConfirmation      ErrorCode = "REQUIRES_CONFIRMATION"
	ErrCodeConfirmationTokenInvalid  ErrorCode = "CONFIRMATION_TOKEN_INVALID"
	ErrCodeDestructiveBudgetExceeded ErrorCode = "DESTRUCTIVE_BUDGET_EXCEEDED"
	ErrCodeDestructiveScopeMismatch  ErrorCode = "DESTRUCTIVE_SCOPE_MISMATCH"
	ErrCodeUnsupportedCapability     ErrorCode = "UNSUPPORTED_CAPABILITY"
	ErrCodeVariantQuarantined        ErrorCode = "VARIANT_QUARANTINED"
	ErrCodeVariantDeprecated         ErrorCode = "VARIANT_DEPRECATED"

	// Auth / scope
	ErrCodeAuthRequired ErrorCode = "AUTH_REQUIRED"
	ErrCodeScopeMissing ErrorCode = "SCOPE_MISSING"
	// ErrCodeAuthSubjectMismatch is the §7 credential-resolution refusal: the
	// resolved credential's auth_subject_fingerprint is not the subject the
	// profile recorded for that auth strategy. Its string value matches
	// auth.SubjectMismatchCode, which the login-time guard raises, so one code
	// covers both entry points (bead gum-q0kd).
	ErrCodeAuthSubjectMismatch ErrorCode = "AUTH_SUBJECT_MISMATCH"

	// Transport / availability
	ErrCodeRateLimited   ErrorCode = "RATE_LIMITED"
	ErrCodeServiceDown   ErrorCode = "SERVICE_DOWN"
	ErrCodeCancelled     ErrorCode = "CANCELLED"
	ErrCodeLROTimeout    ErrorCode = "LRO_TIMEOUT"
	ErrCodeLROUnroutable ErrorCode = "LRO_UNROUTABLE"
	// ErrCodeLROUnsupportedInCode is the §6.1 code-mode refusal: an op whose
	// default variant is classified lro_return is not callable from gum.code
	// at all. It is raised by the code-mode host functions before dispatch,
	// so no upstream request is made.
	ErrCodeLROUnsupportedInCode ErrorCode = "LRO_UNSUPPORTED_IN_CODE"

	// Output / artifacts
	ErrCodeCodeOutputLimitExceeded ErrorCode = "CODE_OUTPUT_LIMIT_EXCEEDED"
	ErrCodeResultArtifactExpired   ErrorCode = "RESULT_ARTIFACT_EXPIRED"
	ErrCodeTeeSecretCorrupt        ErrorCode = "TEE_SECRET_CORRUPT"
	ErrCodeResponseTooLarge        ErrorCode = "RESPONSE_TOO_LARGE"

	// Environment / configuration
	ErrCodeProjectRootRequired ErrorCode = "PROJECT_ROOT_REQUIRED"

	// Gain ledger
	ErrCodeGainDisabled          ErrorCode = "GAIN_DISABLED"
	ErrCodeGainLedgerUnavailable ErrorCode = "GAIN_LEDGER_UNAVAILABLE"

	// CLI argument parsing
	ErrCodeCLIArgDuplicate ErrorCode = "CLI_ARG_DUPLICATE"
	ErrCodeCLIArgInvalid   ErrorCode = "CLI_ARG_INVALID"

	// Profile policy — allowlist/denylist gate (issue gum-vq4z.2).
	ErrCodePolicyDenied ErrorCode = "POLICY_DENIED"
)

// StructuredError is the canonical error type for the dispatch kernel.
//
// JSON marshalling rules (spec §7, §4.1):
//   - Key order: error_code, message, <detail keys alphabetically>, retryable (if set).
//   - Detail fields are emitted at the top level — the literal key "detail" never appears.
//   - "retryable" is only emitted when WithRetryable has been called (even for false).
//
// The exported field is named ErrCode (not Code) to match pre-existing callers
// in context_propagation_test.go; the JSON key is still "error_code".
type StructuredError struct {
	ErrCode      ErrorCode      `json:"error_code"`
	Message      string         `json:"message"`
	Detail       map[string]any `json:"-"`
	Retryable    bool           `json:"retryable,omitempty"`
	retryableSet bool           // tracks whether WithRetryable was explicitly called
	// auditWritten marks a failure whose §11 audit row the producing stage
	// already appended. The adapter-panic path writes its own row carrying
	// panic:true (spec §3.1 step 7) before the error unwinds, so without the
	// flag dispatcher.Dispatch would add a second row for the same call.
	auditWritten bool
}

// Error implements the error interface.
// Format "<CODE>: <message>" mirrors the JSON error_code + message pair (spec §7).
func (e *StructuredError) Error() string {
	return fmt.Sprintf("%s: %s", string(e.ErrCode), e.Message)
}

// writeJSONKey appends ,"key":value to buf (leading comma always included).
// Both key and value must be already JSON-encoded bytes.
// The caller is responsible for omitting the comma before the very first field.
func writeJSONKey(buf *bytes.Buffer, key []byte, value []byte) {
	buf.WriteByte(',')
	buf.Write(key)
	buf.WriteByte(':')
	buf.Write(value)
}

// MarshalJSON produces a deterministic JSON object (spec §7, §4.1).
// Key order is enforced manually because encoding/json randomises map iteration:
// error_code → message → detail keys (alpha) → retryable (only when explicitly set).
func (e *StructuredError) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')

	// Only Detail carries caller-supplied values, so it is the only member
	// whose encoding can fail. Marshalling a string or a bool is total.
	codeVal, _ := json.Marshal(string(e.ErrCode))
	buf.WriteString(`"error_code":`)
	buf.Write(codeVal)

	msgVal, _ := json.Marshal(e.Message)
	keyMsg, _ := json.Marshal("message")
	writeJSONKey(&buf, keyMsg, msgVal)

	if len(e.Detail) > 0 {
		keys := make([]string, 0, len(e.Detail))
		for k := range e.Detail {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			keyBytes, _ := json.Marshal(k)
			valBytes, err := json.Marshal(e.Detail[k])
			if err != nil {
				return nil, err
			}
			writeJSONKey(&buf, keyBytes, valBytes)
		}
	}

	// Emit "retryable" only when explicitly set via WithRetryable (spec §3.1 SERVICE_DOWN).
	if e.retryableSet {
		retryableVal, _ := json.Marshal(e.Retryable)
		keyRetryable, _ := json.Marshal("retryable")
		writeJSONKey(&buf, keyRetryable, retryableVal)
	}

	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// NewStructuredError constructs a StructuredError with the given code and message.
func NewStructuredError(code ErrorCode, message string) *StructuredError {
	return &StructuredError{
		ErrCode: code,
		Message: message,
	}
}

// WithDetail adds a key-value pair to the detail map (fluent builder).
// The key is emitted at the top level of the JSON object (spec §4.1).
func (e *StructuredError) WithDetail(key string, value any) *StructuredError {
	if e.Detail == nil {
		e.Detail = make(map[string]any)
	}
	e.Detail[key] = value
	return e
}

// WithRetryable sets the retryable flag and marks it as explicitly set,
// so it will be emitted in JSON output even when false (spec §3.1).
func (e *StructuredError) WithRetryable(b bool) *StructuredError {
	e.Retryable = b
	e.retryableSet = true
	return e
}

// IsStructuredError reports whether err (or any error in its chain) is a
// *StructuredError with the given code. Uses errors.As for unwrapping.
func IsStructuredError(err error, code ErrorCode) bool {
	var se *StructuredError
	if errors.As(err, &se) {
		return se.ErrCode == code
	}
	return false
}

// ErrRateLimited is the kernel-level sentinel for rate-limit conditions
// (spec §3.1 + §7). The internal/auth.ErrRateLimited bucket sentinel
// wraps this so dispatch can detect rate-limit errors flowing up from the
// token-bucket layer without an import cycle (auth → dispatch is the
// allowed direction). Adapter 429 responses are detected separately via
// the HTTPStatuser interface below; both paths converge on
// ErrCodeRateLimited via mapRateLimited at the dispatch boundary.
var ErrRateLimited = errors.New("RATE_LIMITED")

// HTTPStatuser is implemented by adapter errors that carry an HTTP status
// code. The dispatch boundary uses this to detect upstream 429 responses
// without importing internal/adapters (which would create a cycle, since
// adapters import dispatch). adapters.UpstreamError implements this.
type HTTPStatuser interface {
	HTTPStatusCode() int
}

// UpstreamBodyCarrier is implemented by adapter errors that retain the raw
// non-2xx response body. The dispatch boundary uses it to artifact the real
// upstream payload when tee_mode = "failures" fires, without importing
// internal/adapters. adapters.UpstreamError implements this.
type UpstreamBodyCarrier interface {
	UpstreamBody() []byte
}

// StructuredErrorCarrier is implemented by errors that already know their own
// spec §7 envelope. internal/auth implements it on *auth.AuthError: dispatch
// cannot name that type (internal/auth imports dispatch), and flattening the
// error to a bare AUTH_REQUIRED would drop auth_strategy, missing_components
// and setup_command, which spec §7 makes mandatory for every
// non-gum_oauth auth failure.
//
// A carrier that returns nil is treated as having no envelope, so the caller
// falls back to its own wrapping.
type StructuredErrorCarrier interface {
	AsStructuredError() *StructuredError
}

// RetryAfterMsCarrier is implemented by adapter errors that captured a
// Retry-After hint from the upstream response. Optional companion to
// HTTPStatuser: when present and positive, mapRateLimited surfaces the
// value as the retry_after_ms detail on the RATE_LIMITED error envelope
// (spec §7).
type RetryAfterMsCarrier interface {
	RetryAfterMs() int64
}

// mapRateLimited converts a raw bucket or adapter error into the canonical
// ErrCodeRateLimited structured error (spec §7). It is the
// single dispatch-boundary translator for both paths:
//
//   - errors.Is(err, ErrRateLimited) — token-bucket sentinel (auth path)
//   - errors.As(err, &h) && h.HTTPStatusCode() == 429 — upstream 429 (adapter path)
//
// Any other error is returned unchanged.
func mapRateLimited(err error) error {
	if err == nil {
		return nil
	}
	// Map an upstream HTTP status error to a structured code so the agent gets a
	// machine-readable error_code/retryable instead of an opaque "upstream error
	// HTTP NNN..." string. 429 takes priority (it carries the retry_after_ms
	// hint); a 5xx that survived the adapter's backoff is SERVICE_DOWN.
	var hs HTTPStatuser
	if errors.As(err, &hs) {
		switch code := hs.HTTPStatusCode(); {
		case code == 429:
			se := NewStructuredError(ErrCodeRateLimited, "upstream rate-limited (HTTP 429)").
				WithRetryable(true)
			var rac RetryAfterMsCarrier
			if errors.As(err, &rac) {
				if ms := rac.RetryAfterMs(); ms > 0 {
					se = se.WithDetail("retry_after_ms", ms)
				}
			}
			return se
		case code >= 500 && code <= 599:
			// Retries already exhausted upstream; mark non-retryable so the agent
			// doesn't hot-loop, but the SERVICE_DOWN code tells it the failure is
			// server-side (safe to try again later).
			return NewStructuredError(ErrCodeServiceDown,
				fmt.Sprintf("upstream service error (HTTP %d)", code)).
				WithDetail("http_status", code).
				WithRetryable(false)
		}
	}
	// An adapter error that already knows its own §7 envelope keeps it. This
	// runs after the status switch so a 429 stays RATE_LIMITED even when the
	// body also named a policy reason: a rate limit clears on its own, a
	// policy gap does not.
	var sec StructuredErrorCarrier
	if errors.As(err, &sec) {
		if se := sec.AsStructuredError(); se != nil {
			return se
		}
	}
	if errors.Is(err, ErrRateLimited) {
		return NewStructuredError(ErrCodeRateLimited, "local token-bucket rate-limited").
			WithRetryable(true)
	}
	return err
}

// structuredErrorFromEnvelope reconstructs a StructuredError from a JSON error
// envelope an adapter packed into its Response.Body (the plugin path returns
// {error_code, retryable, retry_after_ms, ...} alongside a non-nil error).
// Returns nil when the body is not such an envelope, so the caller can fall
// back to the opaque error. This keeps a plugin's mapped error_code / retry
// hints from being discarded when the dispatch lifecycle sees the (resp, err)
// pair and would otherwise drop resp.
func structuredErrorFromEnvelope(body []byte) *StructuredError {
	if len(body) == 0 {
		return nil
	}
	var env struct {
		ErrorCode    string `json:"error_code"`
		Message      string `json:"message"`
		Retryable    *bool  `json:"retryable"`
		RetryAfterMs int64  `json:"retry_after_ms"`
	}
	if err := json.Unmarshal(body, &env); err != nil || env.ErrorCode == "" {
		return nil
	}
	msg := env.Message
	if msg == "" {
		msg = env.ErrorCode
	}
	se := NewStructuredError(ErrorCode(env.ErrorCode), msg)
	if env.Retryable != nil {
		se = se.WithRetryable(*env.Retryable)
	}
	if env.RetryAfterMs > 0 {
		se = se.WithDetail("retry_after_ms", env.RetryAfterMs)
	}
	return se
}

// wrappedKernelError attaches the original failure to a §7 envelope so both
// consumer paths keep working: errors.As finds the *StructuredError for the
// envelope, and errors.Is still reaches the cause for sentinel matching. It
// mirrors cancelledError, which solves the same problem for context errors.
type wrappedKernelError struct {
	*StructuredError
	cause error
}

// Unwrap returns the original error so errors.Is keeps resolving sentinels.
func (w *wrappedKernelError) Unwrap() error { return w.cause }

// As resolves *StructuredError targets. Returning false for any other target
// lets errors.As continue down the chain to the cause, which is what keeps the
// HTTPStatuser and UpstreamBodyCarrier lookups working through a wrap.
func (w *wrappedKernelError) As(target any) bool {
	if t, ok := target.(**StructuredError); ok {
		*t = w.StructuredError
		return true
	}
	return false
}

// structuredErrorCode returns the §7 error code carried by err, or "" when
// the chain holds no *StructuredError. Callers use it to label a failure
// without duplicating the errors.As dance.
func structuredErrorCode(err error) ErrorCode {
	var se *StructuredError
	if !errors.As(err, &se) || se == nil {
		return ""
	}
	return se.ErrCode
}

// dispatchAudited reports whether an earlier stage already appended the §11
// audit row for this failure.
func dispatchAudited(err error) bool {
	var se *StructuredError
	if !errors.As(err, &se) || se == nil {
		return false
	}
	return se.auditWritten
}

// wrapKernelError gives a non-structured kernel failure a §7 envelope.
//
// A structured error passes through untouched: this is a fallback, not a
// rewrite. Anything else becomes SERVICE_DOWN with retryable=false, which is
// the code spec §3.1 step 7 already assigns to an internal failure the caller
// cannot classify. The stable code set has no INTERNAL_ERROR.
//
// The original message is kept verbatim. The message was already reaching the
// caller as free text before the envelope existed, so preserving it adds a code
// without widening what is disclosed.
func wrapKernelError(inv *Invocation, err error) error {
	if err == nil {
		return nil
	}
	var se *StructuredError
	if errors.As(err, &se) {
		return err
	}
	wrapped := NewStructuredError(ErrCodeServiceDown, err.Error()).WithRetryable(false)
	if inv != nil && inv.OpID != "" {
		wrapped = wrapped.WithDetail("op_id", inv.OpID)
	}
	return &wrappedKernelError{StructuredError: wrapped, cause: err}
}
