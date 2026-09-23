// Package adapters holds backend executors (typed-rest-sdk, code-runner, ...) per spec.md §14.
//
// Executors only — no policy. Dispatch lifecycle in internal/dispatch decides what to call.
package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ehmo/gum/internal/dispatch"
	sandbox "github.com/ehmo/gum/internal/sandbox/risor"
)

// destructiveScopeEntry mirrors dispatch.DestructiveScopeEntry for internal use.
type destructiveScopeEntry struct {
	opID        string
	resourceKey string
}

// destructiveState is the per-execution budget/scope/pending-confirm tracker.
// Stack-allocated per Execute call; closures capture a pointer to it.
type destructiveState struct {
	budget         int
	scope          []destructiveScopeEntry
	pendingOpID    string
	pendingRsrcKey string
	hasPending     bool
}

// CodeRunner is the adapter for adapter_key = "code.risor".
// It delegates execution to the internal/sandbox/risor package.
type CodeRunner struct {
	// dispatcher is the kernel reference used by the gum_parallel builtin to
	// fan out per-element invocations. Nil disables gum_parallel (callers see
	// INVALID_ARGS). Wired via WithDispatcher to break the adapter↔kernel
	// construction cycle.
	dispatcher dispatch.Dispatcher

	// outputLimitBytes is the §6.1 cumulative output budget. 0 → sandbox default.
	outputLimitBytes int
}

// NewCodeRunner constructs a CodeRunner.
func NewCodeRunner() *CodeRunner {
	return &CodeRunner{}
}

// WithDispatcher sets the kernel reference used by the gum_parallel builtin.
// Returns the receiver to support fluent post-construction wiring.
func (c *CodeRunner) WithDispatcher(d dispatch.Dispatcher) *CodeRunner {
	c.dispatcher = d
	return c
}

// WithOutputLimitBytes sets the §6.1 cumulative output budget.
// 0 leaves the sandbox default in force. Returns the receiver.
func (c *CodeRunner) WithOutputLimitBytes(n int) *CodeRunner {
	c.outputLimitBytes = n
	return c
}

// Execute satisfies dispatch.Adapter for adapter_key = "code.risor".
//
// Required inv.Args keys:
//   - "language": string — only "risor" is accepted.
//   - "source": string   — the Risor program to execute.
func (c *CodeRunner) Execute(ctx context.Context, inv *dispatch.Invocation, rv *dispatch.ResolvedVariant, creds *dispatch.Credentials) (*dispatch.Response, error) {
	args := inv.Args
	if args == nil {
		args = map[string]any{}
	}

	langVal := args["language"]
	language, _ := langVal.(string)
	if language != "risor" {
		// A fake "LANGUAGE_NOT_SUPPORTED:" text prefix used to stand in for a
		// code here. That string is not in the §7 stable set, so nothing could
		// branch on it. INVALID_ARGS with field=language is the real code. This
		// guard is defence in depth: the MCP path rejects an unknown language
		// against the registered inputSchema before dispatch (spec §4.3
		// reserved-language rejection transport).
		return nil, dispatch.NewStructuredError(dispatch.ErrCodeInvalidArgs,
			"only risor is supported").
			WithDetail("field", "language").
			WithDetail("value", language)
	}

	codeVal := args["source"]
	code, _ := codeVal.(string)
	if code == "" {
		return nil, dispatch.NewStructuredError(dispatch.ErrCodeInvalidArgs,
			"code is required").
			WithDetail("field", "code")
	}

	// Reject pragma headers before sandbox.Run so the error is a structured
	// INVALID_ARGS, not an opaque Risor parse error (spec §6.1).
	if hasPragmaHeader(code) {
		return nil, dispatch.NewStructuredError(dispatch.ErrCodeInvalidArgs,
			"script-header pragma directives are not supported").
			WithDetail("pragma", "rejected")
	}

	if err := validateDestructiveBudget(inv, args); err != nil {
		return nil, err
	}

	ds := buildDestructiveState(inv, args)

	// The §9.0.1 ceilings measure against the live §6.1 counter, which only
	// the sandbox owns. Bind an empty Budget here and hand it to Run below;
	// gum_parallel reads the remainder through it while the script runs.
	budget := &sandbox.Budget{}
	effectiveLimit := c.outputLimitBytes
	if effectiveLimit <= 0 {
		effectiveLimit = sandbox.DefaultOutputLimitBytes
	}

	globals := map[string]any{
		"gum_confirm_destructive": buildConfirmFn(inv.AllowDestructive, inv.Confirmed, ds),
		"gum_call":                buildCallFn(ctx, c.dispatcher, inv.AllowWrite, inv.AllowDestructive, inv.Confirmed, ds),
		// Variadic so both the documented two-arg form gum_search(query, k) and
		// the bare gum_search(query) work — a fixed one-arg signature made the
		// two-arg call fail at runtime in Risor, and a fixed two-arg signature
		// breaks the bare call. Search-in-code-mode is still a stub (empty).
		"gum_search": func(args ...any) any {
			return []any{}
		},
		"gum_parallel": buildParallelFn(ctx, c.dispatcher, inv.AllowWrite, inv.AllowDestructive, parallelBudget{
			remaining:   budget.Remaining,
			limitBytes:  effectiveLimit,
			concurrency: parallelMaxWorkers,
		}),
	}

	opts := sandbox.Options{
		AllowWrite:       inv.AllowWrite,
		AllowDestructive: inv.AllowDestructive,
		OutputLimitBytes: c.outputLimitBytes,
		Budget:           budget,
		Globals:          globals,
	}

	out, err := sandbox.Run(ctx, code, opts)
	if err != nil {
		var limitErr *sandbox.OutputLimitError
		if errors.As(err, &limitErr) {
			// §6.1 spells the envelope out: error_code, limit_bytes,
			// printed_bytes. The oversized value itself is never echoed back —
			// naming its size is the whole point of the refusal.
			return nil, dispatch.NewStructuredError(dispatch.ErrCodeCodeOutputLimitExceeded,
				fmt.Sprintf("gum.code output budget of %d bytes exceeded: %d bytes printed, return value adds %d",
					limitErr.LimitBytes, limitErr.PrintedBytes, limitErr.ValueBytes)).
				WithDetail("limit_bytes", limitErr.LimitBytes).
				WithDetail("printed_bytes", limitErr.PrintedBytes).
				WithRetryable(false)
		}
		return nil, err
	}

	return &dispatch.Response{
		Body:                out.Printed,
		Format:              "raw",
		StatusCode:          200,
		BytesOut:            len(out.Printed),
		CodeOutputTruncated: out.Truncated,
	}, nil
}

// Spec §6.1.1 destructive envelope bounds: a confirmed allow_destructive
// invocation must declare a budget in minDestructiveBudget..maxDestructiveBudget,
// and destructive_scope holds at most maxDestructiveScopeEntries entries.
const (
	minDestructiveBudget       = 1
	maxDestructiveBudget       = 20
	maxDestructiveScopeEntries = 20
)

// validateDestructiveBudget returns INVALID_ARGS when allow_destructive=true and
// the destructive envelope is out of bounds: destructive_budget outside
// 1..20, or more than 20 destructive_scope entries.
// budget=0 means absent (makeCodeInvocation skips 0); absent counts as invalid.
func validateDestructiveBudget(inv *dispatch.Invocation, args map[string]any) error {
	if !inv.AllowDestructive {
		return nil
	}
	budget := extractIntArg(args, "destructive_budget")
	if budget < minDestructiveBudget || budget > maxDestructiveBudget {
		return dispatch.NewStructuredError(dispatch.ErrCodeInvalidArgs,
			fmt.Sprintf("destructive_budget must be in %d..%d, got %d", minDestructiveBudget, maxDestructiveBudget, budget)).
			WithDetail("destructive_budget", budget)
	}
	// The cap has to be checked here rather than in extractScope: that helper
	// returns no error and silently skips malformed entries, so an over-cap
	// scope would have widened the destructive envelope without a word.
	if n := scopeEntryCount(args); n > maxDestructiveScopeEntries {
		return dispatch.NewStructuredError(dispatch.ErrCodeInvalidArgs,
			fmt.Sprintf("destructive_scope accepts at most %d entries, got %d", maxDestructiveScopeEntries, n)).
			WithDetail("destructive_scope_count", n)
	}
	return nil
}

// scopeEntryCount reports how many raw destructive_scope entries the caller
// sent, before extractScope drops the ones it cannot read.
func scopeEntryCount(args map[string]any) int {
	list, ok := args["destructive_scope"].([]any)
	if !ok {
		return 0
	}
	return len(list)
}

// buildDestructiveState populates a fresh destructiveState from inv/args.
func buildDestructiveState(inv *dispatch.Invocation, args map[string]any) *destructiveState {
	ds := &destructiveState{}
	if inv.AllowDestructive {
		ds.budget = extractIntArg(args, "destructive_budget")
		ds.scope = extractScope(args)
	}
	return ds
}

// buildConfirmFn returns the gum_confirm_destructive closure for this execution.
func buildConfirmFn(allowDestructive, confirmed bool, ds *destructiveState) func(...any) (any, error) {
	return func(fnArgs ...any) (any, error) {
		if !allowDestructive {
			return nil, dispatch.NewStructuredError(dispatch.ErrCodeRequiresConfirmation,
				"allow_destructive is false; call gum_confirm_destructive is not permitted")
		}
		if !confirmed {
			return nil, dispatch.NewStructuredError(dispatch.ErrCodeRequiresConfirmation,
				"allow_destructive requires confirmed=true on the enclosing gum.code invocation")
		}
		opID := ""
		if len(fnArgs) > 0 {
			opID, _ = fnArgs[0].(string)
		}
		rsrcKey := ""
		if len(fnArgs) > 1 {
			rsrcKey, _ = fnArgs[1].(string)
		}
		ds.pendingOpID = opID
		ds.pendingRsrcKey = rsrcKey
		ds.hasPending = true
		return true, nil
	}
}

// buildCallFn returns the gum_call closure for this execution.
//
// The dispatcher is probed first with no elevated risk flags. That lets the
// policy kernel identify the op's risk class without executing write or
// destructive ops; only then does gum_call retry with the specific capability
// the script invocation granted. Destructive retries also pass through gum's
// normal confirmation-token machinery after the local in-script confirmation
// and budget/scope gates have succeeded.
func buildCallFn(parentCtx context.Context, disp dispatch.Dispatcher, allowWrite, allowDestructive, confirmed bool, ds *destructiveState) func(...any) (any, error) {
	return func(fnArgs ...any) (any, error) {
		el, err := parseCallInput(fnArgs)
		if err != nil {
			return nil, err
		}
		if disp == nil {
			if allowDestructive {
				if err := consumeDestructiveCallGate(ds, el.OpID); err != nil {
					return nil, err
				}
			}
			return nil, dispatch.NewStructuredError(dispatch.ErrCodeUnsupportedCapability,
				"gum_call is not wired in this execution context (no dispatcher reference)").
				WithDetail("capability", "gum_call")
		}
		if err := refuseLRO(disp, el.OpID); err != nil {
			return nil, err
		}

		result, err := dispatchCallOnce(parentCtx, disp, el, false, false, false, "")
		if err == nil {
			return result, nil
		}

		requiredTool := requiredToolFromRiskMismatch(err)
		switch requiredTool {
		case "gum.write":
			if !allowWrite {
				return nil, err
			}
			if !confirmed {
				return nil, dispatch.NewStructuredError(dispatch.ErrCodeRequiresConfirmation,
					"allow_write requires confirmed=true on the enclosing gum.code invocation").
					WithDetail("op_id", el.OpID)
			}
			result, writeErr := dispatchCallOnce(parentCtx, disp, el, true, false, false, "")
			token := confirmationTokenFromRequiresConfirmation(writeErr)
			if token == "" {
				return result, writeErr
			}
			return dispatchCallOnce(parentCtx, disp, el, true, false, true, token)
		case "gum.destructive":
			if !allowDestructive {
				return nil, err
			}
			if !confirmed {
				return nil, dispatch.NewStructuredError(dispatch.ErrCodeRequiresConfirmation,
					"allow_destructive requires confirmed=true on the enclosing gum.code invocation").
					WithDetail("op_id", el.OpID)
			}
			if err := consumeDestructiveCallGate(ds, el.OpID); err != nil {
				return nil, err
			}
			_, firstErr := dispatchCallOnce(parentCtx, disp, el, false, true, false, "")
			token := confirmationTokenFromRequiresConfirmation(firstErr)
			if token == "" {
				if firstErr != nil {
					return nil, firstErr
				}
				return nil, dispatch.NewStructuredError(dispatch.ErrCodeRequiresConfirmation,
					fmt.Sprintf("destructive call to %q did not return a confirmation token", el.OpID)).
					WithDetail("op_id", el.OpID)
			}
			return dispatchCallOnce(parentCtx, disp, el, false, true, true, token)
		default:
			return nil, err
		}
	}
}

// lroRefusalMessage is the §6.1 message the LRO refusal carries verbatim. The
// wording names the two surfaces that do support an LRO, so the script author
// knows where to move the call.
const lroRefusalMessage = "long-running operations are not callable from gum.code; " +
	"run the op through the CLI or the matching gum.read / gum.write / " +
	"gum.destructive tool, then poll it with gum.poll"

// refuseLRO is the §6.1 pre-dispatch gate shared by the code-mode host
// functions: an op whose default variant is classified `lro_return` is not
// callable from gum.code.
//
// The gate lives here rather than in the kernel because the restriction is a
// property of code mode; the same op called through the CLI or its risk-class
// MCP tool still runs. A dispatcher that does not answer LROClassifier leaves
// the gate open, which keeps mock dispatchers working and matches how
// gum_parallel treats a missing ServiceFamilyResolver.
//
// Poll-cycle support inside gum.code is not built: a
// request-scoped Risor execution cannot safely drive the host-side polling
// state machine.
func refuseLRO(disp dispatch.Dispatcher, opID string) error {
	c, ok := disp.(dispatch.LROClassifier)
	if !ok || !c.ReturnsLRO(opID) {
		return nil
	}
	return dispatch.NewStructuredError(dispatch.ErrCodeLROUnsupportedInCode, lroRefusalMessage).
		WithDetail("op_id", opID)
}

func parseCallInput(fnArgs []any) (parallelElement, error) {
	if len(fnArgs) == 0 {
		return parallelElement{}, dispatch.NewStructuredError(dispatch.ErrCodeInvalidArgs,
			"gum_call: expected op_id and optional args")
	}
	opID, _ := fnArgs[0].(string)
	if opID == "" {
		return parallelElement{}, dispatch.NewStructuredError(dispatch.ErrCodeInvalidArgs,
			fmt.Sprintf("gum_call: op_id must be a string, got %T", fnArgs[0]))
	}
	el := parallelElement{OpID: opID, Args: map[string]any{}}
	if len(fnArgs) > 1 {
		args, ok := fnArgs[1].(map[string]any)
		if !ok {
			return parallelElement{}, dispatch.NewStructuredError(dispatch.ErrCodeInvalidArgs,
				fmt.Sprintf("gum_call: args must be a map, got %T", fnArgs[1]))
		}
		el.Args = args
	}
	if len(fnArgs) > 2 {
		variantID, _ := fnArgs[2].(string)
		if variantID == "" {
			return parallelElement{}, dispatch.NewStructuredError(dispatch.ErrCodeInvalidArgs,
				fmt.Sprintf("gum_call: variant_id must be a string, got %T", fnArgs[2]))
		}
		el.VariantID = variantID
	}
	return el, nil
}

func consumeDestructiveCallGate(ds *destructiveState, opID string) error {
	// Step 1: require a matching pending confirmation.
	if !ds.hasPending {
		return dispatch.NewStructuredError(dispatch.ErrCodeRequiresConfirmation,
			fmt.Sprintf("destructive call to %q requires gum_confirm_destructive first", opID)).
			WithDetail("op_id", opID)
	}
	if ds.pendingOpID != opID {
		// Consume the one-shot pending slot before returning error (spec §6.1).
		ds.hasPending = false
		return dispatch.NewStructuredError(dispatch.ErrCodeRequiresConfirmation,
			fmt.Sprintf("gum_confirm_destructive op_id %q does not match gum_call op_id %q", ds.pendingOpID, opID)).
			WithDetail("op_id", opID).
			WithDetail("confirmed_op_id", ds.pendingOpID)
	}
	pendingRsrcKey := ds.pendingRsrcKey
	ds.hasPending = false

	// Step 2: budget gate.
	if ds.budget <= 0 {
		return dispatch.NewStructuredError(dispatch.ErrCodeDestructiveBudgetExceeded,
			fmt.Sprintf("destructive budget exhausted (0 remaining) for op %q", opID)).
			WithDetail("op_id", opID).
			WithDetail("destructive_budget", 0)
	}

	// Step 3: scope gate (only when scope is non-empty).
	if len(ds.scope) > 0 {
		if !scopeMatches(ds.scope, opID, pendingRsrcKey) {
			return dispatch.NewStructuredError(dispatch.ErrCodeDestructiveScopeMismatch,
				fmt.Sprintf("call to %q with resource %q is outside the allowed destructive_scope", opID, pendingRsrcKey)).
				WithDetail("op_id", opID).
				WithDetail("resource_key", pendingRsrcKey)
		}
	}

	// Step 4: commit.
	ds.budget--
	return nil
}

func dispatchCallOnce(ctx context.Context, disp dispatch.Dispatcher, el parallelElement, allowWrite, allowDestructive, confirmed bool, token string) (any, error) {
	inv := &dispatch.Invocation{
		OpID:               el.OpID,
		Args:               el.Args,
		RequestedVariantID: el.VariantID,
		AllowWrite:         allowWrite,
		AllowDestructive:   allowDestructive,
		Confirmed:          confirmed,
		ConfirmationToken:  token,
		Caller:             dispatch.CallerRisor,
	}
	shaped, err := disp.Dispatch(ctx, inv)
	if err != nil {
		return nil, err
	}
	return callResultValue(shaped), nil
}

func callResultValue(shaped *dispatch.ShapedResponse) any {
	if shaped == nil {
		return nil
	}
	if shaped.StructuredContent != nil {
		return shaped.StructuredContent
	}
	if len(shaped.Body) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(shaped.Body, &v); err == nil {
		return v
	}
	return string(shaped.Body)
}

func requiredToolFromRiskMismatch(err error) string {
	var se *dispatch.StructuredError
	if !errors.As(err, &se) || se.ErrCode != dispatch.ErrCodeRiskToolMismatch {
		return ""
	}
	required, _ := se.Detail["required_tool"].(string)
	return required
}

func confirmationTokenFromRequiresConfirmation(err error) string {
	var se *dispatch.StructuredError
	if !errors.As(err, &se) || se.ErrCode != dispatch.ErrCodeRequiresConfirmation {
		return ""
	}
	token, _ := se.Detail["confirmation_token"].(string)
	return token
}

// scopeMatches returns true if (opID, rsrcKey) matches any entry in scope.
// An entry with an empty resourceKey matches any resource for that op_id.
func scopeMatches(scope []destructiveScopeEntry, opID, rsrcKey string) bool {
	for _, e := range scope {
		if e.opID != opID {
			continue
		}
		if e.resourceKey == "" || e.resourceKey == rsrcKey {
			return true
		}
	}
	return false
}

// hasPragmaHeader reports whether the first non-blank line of source is a
// pragma directive: optional-whitespace + "//" + optional-whitespace + "pragma:" ...
// (case-insensitive).
func hasPragmaHeader(source string) bool {
	for _, line := range strings.Split(source, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		if strings.HasPrefix(lower, "//") {
			rest := strings.TrimSpace(strings.TrimPrefix(lower, "//"))
			if strings.HasPrefix(rest, "pragma:") {
				return true
			}
		}
		break
	}
	return false
}

// extractIntArg reads an integer from args[key], handling both int and float64
// (JSON numbers unmarshal as float64).
func extractIntArg(args map[string]any, key string) int {
	v, ok := args[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

// extractScope reads destructive_scope from args into []destructiveScopeEntry.
func extractScope(args map[string]any) []destructiveScopeEntry {
	raw, ok := args["destructive_scope"]
	if !ok {
		return nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]destructiveScopeEntry, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		opID, _ := m["op_id"].(string)
		rsrcKey, _ := m["resource_key"].(string)
		out = append(out, destructiveScopeEntry{opID: opID, resourceKey: rsrcKey})
	}
	return out
}
