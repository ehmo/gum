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

// Execute satisfies dispatch.Adapter for adapter_key = "code.risor".
//
// Required inv.Args keys:
//   - "language": string — only "risor" is accepted in v0.1.0.
//   - "source": string   — the Risor program to execute.
func (c *CodeRunner) Execute(ctx context.Context, inv *dispatch.Invocation, rv *dispatch.ResolvedVariant, creds *dispatch.Credentials) (*dispatch.Response, error) {
	args := inv.Args
	if args == nil {
		args = map[string]any{}
	}

	langVal := args["language"]
	language, _ := langVal.(string)
	if language != "risor" {
		return nil, fmt.Errorf("LANGUAGE_NOT_SUPPORTED: only risor v0.1.0")
	}

	codeVal := args["source"]
	code, _ := codeVal.(string)
	if code == "" {
		return nil, fmt.Errorf("INVALID_ARGS: code is required")
	}

	// Reject pragma headers before sandbox.Run so the error is a structured
	// INVALID_ARGS, not an opaque Risor parse error (spec §6.1 line 1110).
	if hasPragmaHeader(code) {
		return nil, dispatch.NewStructuredError(dispatch.ErrCodeInvalidArgs,
			"script-header pragma directives are not supported in v0.1.0 (deferred to v0.3.0)").
			WithDetail("pragma", "rejected")
	}

	if err := validateDestructiveBudget(inv, args); err != nil {
		return nil, err
	}

	ds := buildDestructiveState(inv, args)

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
		"gum_parallel": buildParallelFn(ctx, c.dispatcher, inv.AllowWrite, inv.AllowDestructive),
	}

	opts := sandbox.Options{
		AllowWrite:       inv.AllowWrite,
		AllowDestructive: inv.AllowDestructive,
		Globals:          globals,
	}

	out, err := sandbox.Run(ctx, code, opts)
	if err != nil {
		return nil, err
	}

	return &dispatch.Response{
		Body:       out.Printed,
		Format:     "raw",
		StatusCode: 200,
		BytesOut:   len(out.Printed),
	}, nil
}

// validateDestructiveBudget returns INVALID_ARGS when allow_destructive=true
// and destructive_budget is outside 1..20.
// budget=0 means absent (makeCodeInvocation skips 0); absent counts as invalid.
func validateDestructiveBudget(inv *dispatch.Invocation, args map[string]any) error {
	if !inv.AllowDestructive {
		return nil
	}
	budget := extractIntArg(args, "destructive_budget")
	if budget < 1 || budget > 20 {
		return dispatch.NewStructuredError(dispatch.ErrCodeInvalidArgs,
			fmt.Sprintf("destructive_budget must be in 1..20, got %d", budget)).
			WithDetail("destructive_budget", budget)
	}
	return nil
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
		// Consume the one-shot pending slot before returning error (spec §6.1 line 1117).
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
