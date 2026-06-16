// Spec §14.1 rule 4: every dispatch event log entry MUST carry the closed
// enum `event`, `op_id`, `variant_id_resolved`, `risk_class`, `caller`, and
// `duration_ms`. This file owns the two closed enums (DispatchEvent and
// Caller) so callers compile-fail on typos rather than emitting drifting
// free-form strings.

package dispatch

// DispatchEvent is the closed enum of names emitted at the `event` key of
// every dispatch slog entry. The set matches the 9-step lifecycle in
// §3.1: parse → policy → routing → cache → auth → token bucket → executor →
// shape → return. Each constant matches the string used by the legacy `step`
// key so historical log consumers continue parsing without reinterpretation.
type DispatchEvent string

const (
	EventParseAndValidate DispatchEvent = "parse_and_validate"
	EventEvaluatePolicy   DispatchEvent = "evaluate_policy"
	EventResolveVariant   DispatchEvent = "resolve_variant"
	EventCacheCheck       DispatchEvent = "cache_check"
	EventResolveAuth      DispatchEvent = "resolve_auth"
	EventTokenBucket      DispatchEvent = "token_bucket"
	EventExecuteAdapter   DispatchEvent = "execute_adapter"
	EventShapeResponse    DispatchEvent = "shape_response"
	EventRecordAndReturn  DispatchEvent = "record_and_return"
)

// Caller is the closed enum stamped on Invocation.Caller by the presentation
// layer (cli, mcp) or the in-process call site (risor sandbox, plugin
// subprocess). When unset, the logger emits the empty string — operators can
// filter for `caller=""` to find code paths that forgot to populate it.
type Caller string

const (
	CallerCLI    Caller = "cli"
	CallerMCP    Caller = "mcp"
	CallerRisor  Caller = "risor"
	CallerPlugin Caller = "plugin"
)
