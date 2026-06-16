package plugins

// PluginError is the host-side rendering of a plugin-local error envelope.
// Spec §8 line 1625 fixes the wire shape; the fields preserved here are the
// ones spec §8 line 1631 ("Plugin error-code mapping") allows the host to
// forward into the stable GUM envelope after mapping.
type PluginError struct {
	Code         string // plugin-local error_code, e.g. "RATE_LIMIT"
	Message      string // raw "error" text from the plugin envelope
	Retryable    bool   // plugin-asserted retryability
	RetryAfterMS int    // milliseconds; preserved when positive
}

// MappedError is the stable GUM-side envelope produced by MapPluginError.
// `SourceErrorCode` is the audit/error-metadata field spec §8 line 1635-1639
// requires the host to preserve so that observability can correlate the
// upstream plugin code with the stable runtime code shown to callers.
type MappedError struct {
	Code            string // stable GUM error_code (RATE_LIMITED, AUTH_REQUIRED, …)
	Message         string // sanitized message forwarded to the caller
	Retryable       bool   // resolved per the §8 mapping table
	RetryAfterMS    int    // 0 unless preserved per the §8 mapping rules
	SourceErrorCode string // original plugin-local code (always populated)
}

// MapPluginErrorCode is the pure-string projection of the §8 mapping table.
// It powers MapPluginError but is exposed for unit tests and observability
// hooks that need only the stable-code projection without retry semantics.
//
// Unknown codes map to SERVICE_DOWN per spec §8 line 1641.
func MapPluginErrorCode(pluginCode string) string {
	switch pluginCode {
	case "RATE_LIMIT":
		return "RATE_LIMITED"
	case "AUTH_EXPIRED":
		return "AUTH_REQUIRED"
	case "PARSE_FAILURE":
		return "SERVICE_DOWN"
	case "SERVICE_DOWN":
		return "SERVICE_DOWN"
	case "INVALID_INPUT":
		return "INVALID_ARGS"
	default:
		return "SERVICE_DOWN"
	}
}

// MapPluginError applies the full spec §8 mapping table — code projection
// plus per-row retry/retry_after_ms field rules — to a plugin error envelope.
// The returned MappedError is what the host injects into the stable GUM
// envelope before forwarding to the caller and to the audit log.
//
// Field rules implemented (spec §8 lines 1635-1641):
//   - RATE_LIMIT    → preserve retryable + retry_after_ms (positive only)
//   - AUTH_EXPIRED  → force retryable=false; drop retry_after_ms
//   - PARSE_FAILURE → preserve retryable from envelope; drop retry_after_ms
//   - SERVICE_DOWN  → preserve retryable + retry_after_ms
//   - INVALID_INPUT → force retryable=false; drop retry_after_ms
//   - unknown       → force retryable=false; drop retry_after_ms; map → SERVICE_DOWN
func MapPluginError(in PluginError) MappedError {
	out := MappedError{
		Code:            MapPluginErrorCode(in.Code),
		Message:         in.Message,
		SourceErrorCode: in.Code,
	}
	switch in.Code {
	case "RATE_LIMIT":
		out.Retryable = in.Retryable
		if in.RetryAfterMS > 0 {
			out.RetryAfterMS = in.RetryAfterMS
		}
	case "AUTH_EXPIRED":
		out.Retryable = false
	case "PARSE_FAILURE":
		out.Retryable = in.Retryable
	case "SERVICE_DOWN":
		out.Retryable = in.Retryable
		if in.RetryAfterMS > 0 {
			out.RetryAfterMS = in.RetryAfterMS
		}
	case "INVALID_INPUT":
		out.Retryable = false
	default:
		out.Retryable = false
	}
	return out
}
