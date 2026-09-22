package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ehmo/gum/internal/adapters"
	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
	"github.com/ehmo/gum/internal/embed"
	"github.com/ehmo/gum/internal/lro"
	"github.com/ehmo/gum/internal/output/gain"
	"github.com/ehmo/gum/internal/output/profile"
	skillreg "github.com/ehmo/gum/internal/skills"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// convenienceOpRouting maps a convenience tool name to the canonical catalog
// op_id it routes to. Unmapped tools return CONVENIENCE_NOT_WIRED.
// Derived at init time from convenienceABITable (single source of truth).
var convenienceOpRouting = func() map[string]string {
	m := make(map[string]string, len(convenienceABITable))
	for name, row := range convenienceABITable {
		m[name] = row.OpID
	}
	return m
}()

// makeMetaToolHandler returns the right handler for a named meta-tool.
// The handler closes over the server so it can reach the dispatcher, the
// catalog snapshot, and the BM25 index. Arguments are checked against the
// tool's registered inputSchema first (see input_validation.go).
func (s *Server) makeMetaToolHandler(name string) sdkmcp.ToolHandler {
	var h sdkmcp.ToolHandler
	switch name {
	case "gum.search_apis":
		h = s.handleSearchAPIs
	case "gum.describe_op":
		h = s.handleDescribeOp
	case "gum.read":
		h = s.handleRead
	case "gum.write":
		h = s.handleWrite
	case "gum.destructive":
		h = s.handleDestructive
	case "gum.code":
		h = s.handleCode
	case "gum.poll":
		h = s.handlePoll
	case "gum.cache_stats":
		h = s.handleCacheStats
	case "gum.gain":
		h = s.handleGain
	default:
		// An unregistered name has no schema to check against, so it goes
		// straight to the UNKNOWN_TOOL envelope.
		return s.handleUnknown(name)
	}
	return validatedHandler(name, metaToolSchema(name), h)
}

// makeConvenienceHandler routes a convenience tool through the catalog by
// looking up its mapped op_id and dispatching with the appropriate risk-class
// flags. The advertised arguments are the op's own argument names (spec §4.1),
// so the only rewriting left is the body mapping: validateParams collapses every
// location=body RequestField into the reserved "body" key, and a caller cannot
// be asked to hand-assemble that key.
//
// The roster runs behind validatedHandler like the meta-tools. That was not
// possible before gum-n1gi: the schemas named `query` where the op declares `q`
// and flat `title`/`to`/`subject` where the op wants a body object, so enforcing
// them would have rejected every call that worked.
func (s *Server) makeConvenienceHandler(toolName string) sdkmcp.ToolHandler {
	h := func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		args := parseArgs(req)
		opID, ok := convenienceOpRouting[toolName]
		if !ok {
			return errorResult(fmt.Sprintf("CONVENIENCE_NOT_WIRED: %s has no catalog mapping in v0.1.0", toolName)), nil
		}
		abi := ConvenienceToolABI(toolName)
		invArgs := copyArgsWithoutControls(args, "confirmed", "confirmation_token")

		userFormat := ""
		if abi.FormatControl {
			userFormat, _ = invArgs["format"].(string)
			delete(invArgs, "format")
		}
		if err := foldConvenienceBody(abi, invArgs); err != nil {
			return errorResult(fmt.Sprintf("INVALID_ARGS: %v", err)), nil
		}

		inv := buildInvocation(opID, invArgs)
		inv.Format = userFormat
		if abi.VariantRule != "" && abi.VariantRule != "default" {
			inv.RequestedVariantID = abi.VariantRule
		}
		s.applyRiskFlagsFromCatalog(inv)

		// Confirmation controls are transport metadata; the kernel owns token
		// issuance/verification so convenience tools and gum.write share one path.
		if isWriteConfirmationTool(toolName) {
			inv.RequireWriteConfirmation = true
			confirmed, _ := args["confirmed"].(bool)
			inv.Confirmed = confirmed
			if tok, ok := args["confirmation_token"].(string); ok {
				inv.ConfirmationToken = tok
			}
		}

		return s.dispatchToolCall(ctx, req, inv)
	}
	return validatedHandler(toolName, convenienceToolSchema(toolName), h)
}

// foldConvenienceBody rewrites the advertised body arguments of one convenience
// tool into the single reserved "body" key the kernel expects. BodyArg carries
// the whole body object (drive_share's `permission`); each BodyFields entry
// becomes one field under its own name (sheets_write's `values`). gmail_send
// uses both: `message` spreads across the body, then top-level `threadId`
// overlays it.
func foldConvenienceBody(abi *ConvenienceABI, args map[string]any) error {
	if abi == nil || (abi.BodyArg == "" && len(abi.BodyFields) == 0) {
		return nil
	}

	body := map[string]any{}
	if abi.BodyArg != "" {
		if v, ok := args[abi.BodyArg]; ok {
			obj, isObj := v.(map[string]any)
			if !isObj {
				return fmt.Errorf("%s takes an object", abi.BodyArg)
			}
			for k, val := range obj {
				body[k] = val
			}
			delete(args, abi.BodyArg)
		}
	}

	for _, name := range abi.BodyFields {
		if v, ok := args[name]; ok {
			body[name] = v
			delete(args, name)
		}
	}

	if len(body) > 0 {
		args[adapters.BodyArgKey] = body
	}
	return nil
}

// handleSearchAPIs runs a BM25 query and returns spec §4.1 / §2129 TOON tuples.
// The response is routed through profile.Apply with the spec §2129 implicit
// profile (hardcoded, not user-overridable per spec §9.4).
// searchAPIsToolName is the op_id gum.search_apis reports in its §13 envelope.
const searchAPIsToolName = "gum.search_apis"

func (s *Server) handleSearchAPIs(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
	args := parseArgs(req)
	query := stringArg(args, "query")
	if query == "" {
		return errorResult("INVALID_ARGS: query is required"), nil
	}
	tuning := loadSearchAPIsTuning(s.profile.String(), s.log())
	k, kOK := searchAPIsK(args["k"], tuning.k)
	if !kOK {
		return errorResult(fmt.Sprintf("INVALID_ARGS: k takes an integer in %d..%d", searchAPIsKMin, searchAPIsKMax)), nil
	}

	tuples := []map[string]any{} // empty array — important for on_empty firing
	if s.snapshot != nil && len(s.snapshot.Ops) > 0 {
		idx, err := s.searchIndex()
		if err != nil {
			return errorResult(fmt.Sprintf("SEARCH_INDEX_BUILD_FAILED: %v", err)), nil
		}
		// Fetch up to 50 candidates (BM25 hard cap) so CollapseArrays.MaxItems=k
		// is the effective limiter — not the search retrieval bound. Spec §2129:
		// collapse_arrays.max_items binds k and is the authoritative truncation step.
		candidateK := k * 5
		if candidateK > 50 {
			candidateK = 50
		}
		for _, hit := range idx.Search(query, candidateK) {
			tuples = append(tuples, s.shapeSearchAPIsRow(hit))
		}
	}

	bodyJSON, err := json.Marshal(tuples)
	if err != nil {
		return errorResult(fmt.Sprintf("JSON_ENCODE_FAILED: %v", err)), nil
	}

	prof := searchAPIsProfile(k, tuning)
	out, err := profile.Apply(prof, profile.ApplyInput{
		Body:       bodyJSON,
		UserFormat: "", // spec §9.4: meta-tool profiles are not overridable
		Op:         searchAPIsToolName,
		// No catalog variant backs a meta tool, so the §9.0 variant header is
		// present and empty.
		Variant: "",
	})
	if err != nil {
		return errorResult(fmt.Sprintf("PROFILE_APPLY_FAILED: %v", err)), nil
	}

	res := &sdkmcp.CallToolResult{
		Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: string(out.Body)}},
	}

	// §9.1 rule 2: zero matches surface the profile's on_empty string. It rides
	// its own text block, because the TOON body for zero rows is "[]" and an
	// LLM reading that cannot tell a failed search from a broken index.
	if out.OnEmptyMessage != "" {
		res.Content = append(res.Content, &sdkmcp.TextContent{Text: out.OnEmptyMessage})
	}

	// §13 ToonResult. gum.search_apis registers an output schema, so it owes
	// the client a conforming structuredContent value.
	//
	// variant_id is null: a meta tool resolves no catalog variant, and naming
	// the tool there would invent one.
	res.StructuredContent = map[string]any{
		"format": "toon",
		"toon":   string(out.Body),
		"op":     searchAPIsToolName,
		"_expression": &dispatch.ExpressionMeta{
			Profile:        prof.Name,
			OpID:           searchAPIsToolName,
			VariantID:      nil,
			Lossy:          out.Lossy,
			ResultCount:    out.ResultCount,
			OmittedCount:   out.OmittedCount,
			OnEmptyMessage: onEmptyStringPtr(out.OnEmptyMessage),
		},
	}
	return res, nil
}

// onEmptyStringPtr returns nil for the empty string so _expression.on_empty_message
// stays null rather than becoming "" (§13 makes the field nullable, not optional).
func onEmptyStringPtr(msg string) *string {
	if msg == "" {
		return nil
	}
	return &msg
}

// shapeSearchAPIsRow remaps one BM25 hit to the spec §4.1 line 291 tuple:
// {api, op, summary, params_required, expected_response}.
func (s *Server) shapeSearchAPIsRow(hit embed.SearchResult) map[string]any {
	// api = first segment of op_id before the first dot.
	api := hit.OpID
	if dot := strings.IndexByte(hit.OpID, '.'); dot >= 0 {
		api = hit.OpID[:dot]
	}

	var paramsRequired []string
	expectedResponse := ""
	if op := s.findOp(hit.OpID); op != nil {
		// params_required: the NAME of each required param. ParamsRequired is a
		// [][]string of [name, type] pairs, so collect pair[0] from each — NOT
		// ParamsRequired[0], which is the first [name,type] pair and would leak
		// the type into the name list (e.g. ["userKey","string"]).
		for _, pair := range op.ParamsRequired {
			if len(pair) >= 1 {
				paramsRequired = append(paramsRequired, pair[0])
			}
		}
		// expected_response: use the default variant's OutputProfile if set.
		if v := defaultVariant(op); v != nil && v.OutputProfile != "" {
			expectedResponse = v.OutputProfile
		}
	}
	if paramsRequired == nil {
		paramsRequired = []string{}
	}

	return map[string]any{
		"api":               api,
		"op":                hit.OpID,
		"summary":           hit.Summary,
		"params_required":   paramsRequired,
		"expected_response": expectedResponse,
	}
}

// handleDescribeOp returns the compact DescribeOpResult for the given op_id.
func (s *Server) handleDescribeOp(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
	args := parseArgs(req)
	opID := stringArg(args, "op_id")
	if opID == "" {
		return jsonErrorResult(map[string]any{
			"error_code": "INVALID_ARGS",
			"message":    "op_id is required",
		}), nil
	}
	op := s.findOp(opID)
	if op == nil {
		return jsonErrorResult(map[string]any{
			"error_code":  "OP_NOT_FOUND",
			"op_id":       opID,
			"suggestions": []string{},
		}), nil
	}
	result := buildDescribeOpResult(op, defaultMaxVariants)
	return structuredJSONResult(result), nil
}

// handleRead dispatches the inner op_id with risk_class assertion=read.
func (s *Server) handleRead(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
	return s.handleRiskTier(ctx, req, catalog.RiskClassRead)
}

// handleWrite dispatches the inner op_id with allow_write=true.
func (s *Server) handleWrite(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
	return s.handleRiskTier(ctx, req, catalog.RiskClassWrite)
}

// handleDestructive dispatches the inner op_id with a confirmation_token.
func (s *Server) handleDestructive(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
	return s.handleRiskTier(ctx, req, catalog.RiskClassDestructive)
}

func (s *Server) handleRiskTier(ctx context.Context, req *sdkmcp.CallToolRequest, want catalog.RiskClass) (*sdkmcp.CallToolResult, error) {
	args := parseArgs(req)
	opID := stringArg(args, "op_id")
	if opID == "" {
		return errorResult("INVALID_ARGS: op_id is required"), nil
	}

	// Verify op exists in catalog; return OP_NOT_FOUND with suggestions if not.
	if s.snapshot != nil {
		op := s.findOp(opID)
		if op == nil {
			suggestions := []string{}
			if idx, err := s.searchIndex(); err == nil {
				hits := idx.Search(opID, dispatch.MaxOpSuggestions)
				for _, h := range hits {
					suggestions = append(suggestions, h.OpID)
				}
			}
			return jsonErrorResult(map[string]any{
				"error_code":  "OP_NOT_FOUND",
				"op_id":       opID,
				"suggestions": suggestions,
			}), nil
		}

		// Verify the catalog op's risk_class matches the meta-tool tier. The
		// check runs against the variant the call will execute, so a pinned
		// variant_id is honoured here: gating on the default variant let a
		// caller pin a destructive variant through gum.read.
		v := gateVariant(op, stringArg(args, "variant_id"))
		if v != nil && v.RiskClass != want {
			rc := string(v.RiskClass) // already lowercase per catalog.RiskClass constants
			return jsonErrorResult(map[string]any{
				"error_code":         "RISK_TOOL_MISMATCH",
				"message":            fmt.Sprintf("%s has risk_class=%s; use gum.%s instead", opID, rc, rc),
				"op_id":              opID,
				"variant_id":         v.VariantID,
				"variant_risk_class": rc,
				"required_tool":      "gum." + rc,
			}), nil
		}
	}

	innerArgs := mapArg(args, "args")
	// Map host-control pagination / field-mask params to the canonical Google
	// query parameters. fields is already canonical; page_token/page_size arrive
	// snake_case and would be silently ignored by the REST API if forwarded
	// verbatim (it expects pageToken and pageSize|maxResults). variant_id is
	// promoted to the dispatch pin below, not an op arg.
	if v := stringArg(args, "fields"); v != "" {
		innerArgs["fields"] = v
	}
	if v := stringArg(args, "page_token"); v != "" {
		innerArgs["pageToken"] = v
	}
	// Forward page_size only when it's a POSITIVE number — mirror the CLI's
	// `pageSize > 0` guard. A zero/negative page_size has no valid Google
	// pagination meaning (it returns an empty page or a 400 depending on the
	// API); the CLI silently treats it as "unset", so the MCP path must too.
	// A fractional value is rejected here rather than forwarded: pageSize=25.5
	// used to reach the API and come back as an opaque upstream 400.
	if v, ok := args["page_size"]; ok {
		if n, isNum := numericArg(v); isNum && n > 0 {
			if n != math.Trunc(n) {
				return errorResult("INVALID_ARGS: page_size takes a positive integer"), nil
			}
			innerArgs[s.canonicalPageSizeParam(opID)] = n
		}
	}

	inv := buildInvocation(opID, innerArgs)
	// Promote the host-control variant_id from the meta-tool args to the
	// dispatch-layer pin (spec §5.1 variant override). Keep it out of the
	// op-arg map so generated REST stubs don't see a stray field.
	if vid := stringArg(args, "variant_id"); vid != "" {
		inv.RequestedVariantID = vid
		delete(inv.Args, "variant_id")
	}
	// allow_write and allow_destructive are deliberately NOT read from args.
	// None of gum.read, gum.write or gum.destructive declares them, all three
	// schemas set additionalProperties:false, and nothing validates the input
	// schema at runtime, so honouring them let `gum.read {allow_destructive:
	// true}` hand the kernel policy gate a flag the read tier must never grant.
	// The per-tier switch below is the only writer.
	if v, ok := args["confirmed"].(bool); ok {
		inv.Confirmed = v
	}
	if v, ok := args["confirmation_token"].(string); ok {
		inv.ConfirmationToken = v
	}
	inv.Format = stringArg(args, "format")
	itemCap, ok := maxItemsOverride(args["max_items"])
	if !ok {
		return errorResult(`INVALID_ARGS: max_items takes a positive integer or "all"`), nil
	}
	inv.MaxItems = itemCap

	// Set per-tier defaults so the policy gate accepts the dispatch.
	switch want {
	case catalog.RiskClassWrite:
		inv.AllowWrite = true
	case catalog.RiskClassDestructive:
		inv.AllowDestructive = true
		// Confirmed and confirmation_token must come from the caller.
	}

	return s.dispatchToolCall(ctx, req, inv)
}

// handleCode dispatches gum.code through the kernel, which routes via the
// catalog to the code.risor adapter. The MCP layer stays thin; the adapter
// owns argument shape (language, source, etc.).
func (s *Server) handleCode(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
	args := parseArgs(req)
	invArgs := copyArgsWithoutControls(args, "confirmed", "confirmation_token")
	inv := buildInvocation("gum.code", invArgs)
	if v, ok := args["allow_write"].(bool); ok {
		inv.AllowWrite = v
	}
	if v, ok := args["allow_destructive"].(bool); ok {
		inv.AllowDestructive = v
	}
	if v, ok := args["confirmed"].(bool); ok {
		inv.Confirmed = v
	}
	if v, ok := args["confirmation_token"].(string); ok {
		inv.ConfirmationToken = v
	}
	return s.dispatchToolCall(ctx, req, inv)
}

// handlePoll implements gum.poll: drives an LRO Poller with §5.7 semantics,
// emits MCP progress notifications when _meta.progressToken is present, and
// returns the terminal Operation result or a stable LRO_TIMEOUT envelope.
func (s *Server) handlePoll(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
	args := parseArgs(req)
	operationName := stringArg(args, "operation_name")
	if operationName == "" {
		return errorResult(`{"error_code":"INVALID_ARGS","missing":["operation_name"]}`), nil
	}

	// Resolve the progress token before constructing the poller; nil means
	// "no client-side token" → no notifications, but the loop still runs.
	var progressToken any
	if req != nil && req.Params != nil {
		progressToken = req.Params.GetProgressToken()
	}

	// Send progress only to the requesting session (gum-t71g). Broadcasting
	// to s.sdkSrv.Sessions() leaks progress across concurrent stdio clients.
	onTick := func(elapsed time.Duration) {
		if progressToken == nil || req == nil || req.Session == nil {
			return
		}
		params := &sdkmcp.ProgressNotificationParams{
			ProgressToken: progressToken,
			Progress:      elapsed.Seconds(),
			Total:         600,
			Message:       operationName + ": RUNNING",
		}
		_ = req.Session.NotifyProgress(ctx, params)
	}

	factory := s.pollerFactory
	if factory == nil {
		factory = s.defaultPollerFactory
	}
	p := factory(onTick)

	result, err := p.Poll(ctx, operationName)
	if err != nil {
		var te *lro.TimeoutError
		if errors.As(err, &te) {
			// LRO_TIMEOUT is a §1527 terminal error code: the poll ended without
			// a result. Returning it with IsError=false told the agent the call
			// succeeded and handed it an envelope its outputSchema rejects. The
			// LRO_FAILED branch below already used jsonErrorResult.
			return jsonErrorResult(map[string]any{
				"error_code":     "LRO_TIMEOUT",
				"operation_name": te.OperationName,
				"resume_handle":  te.OperationName,
				"suggestion":     "Call gum.poll again with the same operation_name to resume polling.",
			}), nil
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return errorResult(`{"error_code":"CANCELLED"}`), nil
		}
		return failureResult(err), nil
	}
	// A done LRO that carries an `error` field is a FAILED operation. Google LROs
	// signal failure with done=true + error:{code,message}. Surface it as an error
	// envelope so the agent doesn't read jsonResult (IsError=false) as success and
	// proceed as if the operation completed.
	if m, ok := result.(map[string]any); ok {
		if lroErr, hasErr := m["error"]; hasErr && lroErr != nil {
			return jsonErrorResult(map[string]any{
				"error_code":     "LRO_FAILED",
				"operation_name": operationName,
				"error":          lroErr,
			}), nil
		}
	}
	res := jsonResult(result)
	if res.IsError {
		return res, nil
	}
	res.StructuredContent = pollResultEnvelope(result)
	return res, nil
}

// rawPassThroughProfile is the §13 profile name a response reports when it
// never entered the expression pipeline. It mirrors the dispatch package's
// unexported `_raw` sentinel (§2705); gum.poll returns the upstream Operation
// untouched, so no profile name would be truthful.
const rawPassThroughProfile = "_raw"

const pollToolName = "gum.poll"

// pollResultEnvelope wraps a terminal LRO Operation in the §13 RawJsonResult
// shape that gum.poll's registered outputSchema promises. The MCP text content
// stays the bare Operation JSON, which is what CLI and test callers read.
func pollResultEnvelope(result any) map[string]any {
	return map[string]any{
		"format": "json",
		"data":   result,
		"_expression": &dispatch.ExpressionMeta{
			Profile:     rawPassThroughProfile,
			OpID:        pollToolName,
			VariantID:   nil,
			Lossy:       false,
			ResultCount: 1,
		},
	}
}

// cacheStatProvider is the internal seam used by handleCacheStats to read live
// semantic cache counters without adding CacheStats to the Dispatcher interface.
// *dispatch.dispatcher satisfies this interface; noopDispatcher stubs do not,
// in which case semantic fields fall back to zero.
type cacheStatProvider interface {
	CacheStats() dispatch.CacheLayerStats
}

// handleCacheStats returns the spec §3003 CacheStatsResult envelope.
func (s *Server) handleCacheStats(_ context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
	var sem dispatch.CacheLayerStats
	if csp, ok := s.disp.(cacheStatProvider); ok {
		sem = csp.CacheStats()
	}
	return structuredJSONResult(cacheStatsEnvelope(sem, s.auditBroken(), clientSupportsPromptCache(req))), nil
}

// auditBroken returns true when the audit.broken sentinel file exists at
// <XDG_DATA_HOME or $HOME/.local/share>/gum/<profile>/audit.broken. Spec §2333-2336.
func (s *Server) auditBroken() bool {
	dir, err := s.profile.DataDir()
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(dir, "audit.broken"))
	return err == nil
}

// cacheStatsEnvelope builds the spec §3003 CacheStatsResult map.
// semantic is live; http stays a v0.1.0 stub (zeros). prompt.supported
// reflects the connected client's prompt-cache capability per §10.1
// (true when the client looks Anthropic-backed). hits_estimate stays nil
// because GUM has no provider-side observability surface yet.
// audit_broken reflects sentinel-file presence per §2335.
func cacheStatsEnvelope(sem dispatch.CacheLayerStats, auditBroken, promptSupported bool) map[string]any {
	return map[string]any{
		"semantic": map[string]any{
			"hits":      sem.Hits,
			"misses":    sem.Misses,
			"evictions": sem.Evictions,
			"entries":   sem.Entries,
			"bytes":     sem.Bytes,
		},
		"http": map[string]any{
			"hits":    int64(0),
			"misses":  int64(0),
			"entries": int64(0),
			"bytes":   int64(0),
		},
		"prompt": map[string]any{
			"supported":     promptSupported,
			"hits_estimate": nil,
		},
		"audit_broken": auditBroken,
	}
}

// handleGain returns the spec §2793 GainResult envelope.
func (s *Server) handleGain(_ context.Context, _ *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
	// Spec §2570 + §2689: GAIN_DISABLED terminal error. The documented opt-out
	// is `gum config set gain.enabled=false`, so read the profile config, not
	// just the env override.
	if !gain.Enabled(s.profile) {
		return jsonErrorResult(map[string]any{"error_code": "GAIN_DISABLED"}), nil
	}
	// Spec §2541: GAIN_LEDGER_UNAVAILABLE terminal error.
	// The ledger is per profile (§12.3), so an MCP server bound to --profile
	// work must not report the default profile's savings.
	path, err := gain.DefaultPath(s.profile)
	if err != nil {
		return jsonErrorResult(map[string]any{
			"error_code": "GAIN_LEDGER_UNAVAILABLE",
			"hint":       "Enable server-side gain ledger storage or configure telemetry export for product analytics.",
		}), nil
	}
	ledger, err := gain.NewLedger(path)
	if err != nil {
		return jsonErrorResult(map[string]any{
			"error_code": "GAIN_LEDGER_UNAVAILABLE",
			"hint":       "Enable server-side gain ledger storage or configure telemetry export for product analytics.",
		}), nil
	}
	defer func() { _ = ledger.Close() }()
	return structuredJSONResult(gainSuccessEnvelope(ledger.Stats(), gain.Sessions(ledger.Select(gain.Filter{})))), nil
}

// gainSuccessEnvelope builds the spec §2793 GainResult map from ledger stats.
// baseline_tokens is the naive raw-token total (TotalTokensIn) and
// actual_tokens is the shaped total, so savings_pct is the real reduction.
//
// savings_pct and the three §12.3 savings-accounting fields answer different
// questions and routinely disagree:
//
//   - savings_pct covers the whole window, estimated baselines included.
//   - end_to_end_savings is the release-gated figure: fixture-backed entries
//     only, gum_parallel envelope overhead charged against the savings.
//   - per_op_shaping_savings is the same calls with the envelope removed. It
//     always reads at least as high, and §12.3 forbids gating on it.
//   - batch_envelope_overhead is the gap between them, in tokens.
//
// end_to_end_savings and per_op_shaping_savings are null, not 0, when the
// window holds no fixture-backed entry. No reproducible evidence is a
// different claim from no savings.
func gainSuccessEnvelope(stats gain.Stats, sessions []gain.SessionRow) map[string]any {
	// The schema reads an absent array as "this mode was not selected", so
	// summary mode emits [] rather than null for an empty ledger.
	if sessions == nil {
		sessions = []gain.SessionRow{}
	}
	savingsTokens := stats.TotalTokensSaved
	// The ledger DOES track the baseline (sum of raw tokens). Report it directly
	// rather than approximating baseline as savings — the old approximation made
	// actual_tokens always 0 and savings_pct always 100%, misleading any agent
	// or human inspecting its own token efficiency.
	baselineTokens := stats.TotalTokensIn
	actualTokens := baselineTokens - savingsTokens

	var savingsPct any
	if baselineTokens > 0 {
		savingsPct = float64(savingsTokens) / float64(baselineTokens) * 100
	}

	return map[string]any{
		"mode":                    "summary",
		"window":                  "last-30-sessions",
		"baseline_tokens":         baselineTokens,
		"actual_tokens":           actualTokens,
		"savings_tokens":          savingsTokens,
		"savings_pct":             savingsPct,
		"end_to_end_savings":      stats.Release.Pct(),
		"per_op_shaping_savings":  stats.ReleaseInner.Pct(),
		"batch_envelope_overhead": stats.BatchEnvelopeTokens,
		"tokenizer":               "cl100k_base",
		// Summary mode carries one aggregate per session, and an empty
		// ledger emits [] rather than dropping the key (spec §2818-2833).
		"sessions": sessions,
	}
}

// handleUnknown is the safety net for unregistered meta-tools.
func (s *Server) handleUnknown(name string) sdkmcp.ToolHandler {
	return func(_ context.Context, _ *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		return errorResult(fmt.Sprintf("META_TOOL_NOT_IMPLEMENTED: %s", name)), nil
	}
}

func (s *Server) handleSkillsList(_ context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
	if len(req.Params.Arguments) > 0 && strings.TrimSpace(string(req.Params.Arguments)) != "{}" {
		return errorResult("INVALID_ARGS: skills_list takes no arguments"), nil
	}
	return structuredJSONResult(map[string]any{"skills": skillreg.DefaultRegistry().List()}), nil
}

type skillsGetArgs struct {
	Name     string `json:"name"`
	Version  string `json:"version,omitempty"`
	MaxBytes int    `json:"max_bytes,omitempty"`
}

func (s *Server) handleSkillsGet(_ context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
	var args skillsGetArgs
	if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
		return errorResult("INVALID_ARGS: " + err.Error()), nil
	}
	if !skillreg.ValidName(args.Name) {
		return errorResult("INVALID_ARGS: name is required and must match " + skillreg.NamePattern), nil
	}
	if !skillreg.ValidVersionSelector(args.Version) {
		return errorResult("INVALID_ARGS: version must match " + skillreg.VersionPattern), nil
	}
	if args.MaxBytes < 0 {
		return errorResult("INVALID_ARGS: max_bytes must be >= 0"), nil
	}
	skill, err := skillreg.DefaultRegistry().Resolve(args.Name, args.Version)
	if err != nil {
		if errors.Is(err, skillreg.ErrUnknownSkill) {
			return errorResult("UNKNOWN_SKILL: " + args.Name), nil
		}
		if errors.Is(err, skillreg.ErrUnknownVersion) {
			return errorResult("UNKNOWN_SKILL_VERSION: " + args.Name + "@" + args.Version), nil
		}
		return failureResult(err), nil
	}
	truncated := false
	if args.MaxBytes > 0 && len([]byte(skill.Body)) > args.MaxBytes {
		body := []byte(skill.Body)
		skill.Body = string(body[:args.MaxBytes])
		truncated = true
	}
	// No structuredContent: skills_get registers no outputSchema, so the body
	// ships once, in the text content. See registerSkillTools.
	return jsonResult(map[string]any{"skill": skill, "truncated": truncated}), nil
}

// --- helpers ---

func (s *Server) findOp(opID string) *catalog.Op {
	if s.snapshot == nil {
		return nil
	}
	for i := range s.snapshot.Ops {
		if s.snapshot.Ops[i].OpID == opID {
			return &s.snapshot.Ops[i]
		}
	}
	return nil
}

// canonicalPageSizeParam returns the query parameter the op uses for page size.
// Google REST APIs are split: most use "maxResults" (Gmail, Calendar, Tasks),
// the newer ones use "pageSize" (Drive). The host-control page_size param is
// mapped to whichever the op declares, defaulting to "pageSize".
func (s *Server) canonicalPageSizeParam(opID string) string {
	op := s.findOp(opID)
	if op == nil {
		return "pageSize"
	}
	hasField := func(name string) bool {
		for _, f := range op.RequestFields {
			if f.Name == name {
				return true
			}
		}
		return false
	}
	if hasField("pageSize") {
		return "pageSize"
	}
	if hasField("maxResults") {
		return "maxResults"
	}
	return "pageSize"
}

// gateVariant returns the variant the risk gate must evaluate: the pinned one
// when variant_id names an active variant, otherwise the op default. It mirrors
// the kernel's policyVariant so the tool-routing check and the policy gate
// agree on which variant a call will execute.
//
// A pin naming an unknown or quarantined variant returns nil, which skips the
// gate. That is safe: the kernel rejects those with VARIANT_NOT_FOUND or
// VARIANT_QUARANTINED before any execution.
func gateVariant(op *catalog.Op, pinnedID string) *catalog.Variant {
	if pinnedID == "" {
		return defaultVariant(op)
	}
	for i := range op.Variants {
		if op.Variants[i].VariantID != pinnedID {
			continue
		}
		if op.Variants[i].Quarantined {
			return nil
		}
		return &op.Variants[i]
	}
	return nil
}

func defaultVariant(op *catalog.Op) *catalog.Variant {
	for i := range op.Variants {
		if op.Variants[i].VariantID == op.DefaultVariantID {
			return &op.Variants[i]
		}
	}
	return nil
}

func (s *Server) applyRiskFlagsFromCatalog(inv *dispatch.Invocation) {
	op := s.findOp(inv.OpID)
	if op == nil {
		return
	}
	v := defaultVariant(op)
	if v == nil {
		return
	}
	switch v.RiskClass {
	case catalog.RiskClassWrite:
		inv.AllowWrite = true
	case catalog.RiskClassDestructive:
		inv.AllowDestructive = true
	}
}

func (s *Server) searchIndex() (*embed.Index, error) {
	// The MCP server is goroutine-per-session, so concurrent gum.search_apis /
	// OP_NOT_FOUND-suggestion / completion calls race on the lazy build. sync.Once
	// makes the build happen exactly once and the result visible to all readers.
	s.bm25Once.Do(func() {
		s.bm25, s.bm25Err = embed.Build(s.snapshot)
	})
	return s.bm25, s.bm25Err
}

// dispatchToolCall is the unified Tier A request entry point. It implements
// the spec §9.2 contract: extract `_meta.gumRoot`, resolve the project root
// via the per-session roots cache (single-root or multi-root selection rule),
// surface PROJECT_ROOT_REQUIRED on §9.2 violation, then resolve the active
// output profile (project-local → user-global → catalog-embedded) using the
// catalog variant's `output_profile` name and feed it into the invocation
// before dispatching.
//
// When the session's roots are not cached yet, the call returns an
// InputRequests result instead of dispatching: MCP 2026-07-28 (SEP-2322)
// forbids a server-initiated roots/list mid-request, so the client fulfils
// the request and the SDK retries this handler with the reply attached.
//
// req may be nil in unit tests that bypass the SDK transport; in that case
// project-local resolution is skipped and dispatch proceeds with the
// catalog-default profile.
func (s *Server) dispatchToolCall(ctx context.Context, req *sdkmcp.CallToolRequest, inv *dispatch.Invocation) (*sdkmcp.CallToolResult, error) {
	if req != nil && req.Session != nil {
		metaGumRoot := stringFromMeta(req, "gumRoot")
		rootPath, needRoots, projErr := s.ResolveProjectRootForRequest(req, metaGumRoot)
		if needRoots {
			return &sdkmcp.CallToolResult{InputRequests: rootsInputRequest()}, nil
		}
		if projErr != nil {
			return jsonErrorResult(projectRootRequiredEnvelope(projErr)), nil
		}
		if profName := s.profileNameForRequest(rootPath, inv); profName != "" {
			if p, _, err := profile.ResolveProfile(rootPath, profName, nil); err == nil {
				inv.OutputProfile = p
			}
		}
	}
	return s.dispatchAndShape(ctx, inv)
}

// stringFromMeta safely extracts a string-typed key from req.Params.Meta.
// Returns "" when the request, params, meta map, or key is missing.
func stringFromMeta(req *sdkmcp.CallToolRequest, key string) string {
	if req == nil || req.Params == nil || req.Params.Meta == nil {
		return ""
	}
	if v, ok := req.Params.Meta[key].(string); ok {
		return v
	}
	return ""
}

// profileNameForRequest picks the profile name for one tool call under the
// client's project root. A §9.2 [override_bindings] entry beats the catalog
// default, the pinned variant_id beating the op_id: that is what lets a project
// attach a profile to an op whose catalog variant names none.
func (s *Server) profileNameForRequest(rootPath string, inv *dispatch.Invocation) string {
	bindings, err := profile.LoadOverrideBindings(rootPath)
	if err == nil {
		if inv.RequestedVariantID != "" {
			if name, ok := bindings[inv.RequestedVariantID]; ok {
				return name
			}
		}
		if name, ok := bindings[inv.OpID]; ok {
			return name
		}
	}
	return s.profileNameForOp(inv.OpID)
}

// profileNameForOp returns the catalog default variant's output_profile name
// for the given op, or "" when the op or variant is unknown.
func (s *Server) profileNameForOp(opID string) string {
	op := s.findOp(opID)
	if op == nil {
		return ""
	}
	v := defaultVariant(op)
	if v == nil {
		return ""
	}
	return v.OutputProfile
}

func (s *Server) dispatchAndShape(ctx context.Context, inv *dispatch.Invocation) (*sdkmcp.CallToolResult, error) {
	shaped, err := s.disp.Dispatch(ctx, inv)
	if err != nil {
		return failureResult(err), nil
	}
	res := &sdkmcp.CallToolResult{
		Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: string(shaped.Body)}},
	}
	// Spec §13: structuredContent is the ToonResult / SingleObjectResult /
	// RawJsonResult envelope, which carries the `_expression` metadata block
	// alongside the payload.
	if wrapped := tierAResult(shaped); wrapped != nil {
		res.StructuredContent = wrapped
	}
	// A profile whitelist removes fields with no marker in the shaped text. Name
	// them in their own text block so the caller can tell an absent field from a
	// field the upstream API never returned (gum-bpx0). The block names the
	// recovery artifact when tee wrote one.
	notice := profile.ShapingNotice(profile.NoticeInput{
		DroppedPaths:    shaped.DroppedPaths,
		CollapsedArrays: shaped.CollapsedArrays,
		DedupedRows:     shaped.DedupedRows,
		LimitedRows:     shaped.LimitedRows,
		RawHint:         `format: "raw"`,
		AnnotationPaths: shaped.AnnotationPaths,
		MaxItemsHint:    `max_items: "all"`,
		FullResultPath:  shaped.FullResultPath,
		OnEmptyMessage:  onEmptyMessageOf(shaped),
	})
	if msg := notice; msg != "" {
		res.Content = append(res.Content, &sdkmcp.TextContent{Text: msg})
	}
	// Spec §9.0 lines 1845-1847: when the active profile uses
	// recovery=resource_link and tee fired, the dispatch layer populates
	// shaped.FullResultResource with the gum://results/<hash> URI. We mirror
	// it as a resource_link content block so MCP clients can fetch the full
	// pre-projection payload. Exactly one block per response; the
	// _expression.full_result_resource field in StructuredContent points to
	// the same URI (set upstream in lifecycle.go).
	if shaped.FullResultResource != "" {
		res.Content = append(res.Content, &sdkmcp.ResourceLink{
			URI:         shaped.FullResultResource,
			Name:        "full_result",
			MIMEType:    "application/json",
			Description: recoveryResourceLinkDescription(inv.OpID),
			Size:        shaped.FullResultSize,
		})
	}
	return res, nil
}

// onEmptyMessageOf returns the profile's on_empty string when shaping left an
// empty result set. A client that reads only the text blocks needs it there
// too: structuredContent carries it as _expression.on_empty_message, but the
// text block would otherwise show a bare empty list.
func onEmptyMessageOf(shaped *dispatch.ShapedResponse) string {
	if shaped == nil || shaped.Expression == nil || shaped.Expression.OnEmptyMessage == nil {
		return ""
	}
	return *shaped.Expression.OnEmptyMessage
}

// recoveryResourceLinkDescription returns the short hint surfaced on the
// resource_link content block. Spec §9.0 line 1847 caps it at 120 chars.
func recoveryResourceLinkDescription(opID string) string {
	desc := "Full pre-projection result for " + opID
	if len(desc) > 120 {
		desc = desc[:117] + "..."
	}
	return desc
}

func buildInvocation(opID string, args map[string]any) *dispatch.Invocation {
	return &dispatch.Invocation{
		OpID:   opID,
		Args:   args,
		Caller: dispatch.CallerMCP,
	}
}

func copyArgsWithoutControls(args map[string]any, controls ...string) map[string]any {
	out := make(map[string]any, len(args))
	for k, v := range args {
		out[k] = v
	}
	for _, k := range controls {
		delete(out, k)
	}
	return out
}

func parseArgs(req *sdkmcp.CallToolRequest) map[string]any {
	args := map[string]any{}
	if len(req.Params.Arguments) > 0 {
		_ = json.Unmarshal(req.Params.Arguments, &args)
	}
	return args
}

// searchAPIsK parses the gum.search_apis k argument. The registered schema
// declares integer/minimum 1/maximum 20, but this SDK does not validate tool
// input against the schema, so the handler has to. k reached
// CollapseArraysSpec.MaxItems unchecked and a negative value panicked the
// stdio server on a slice bound (spec §3.1: a handler fault must not end the
// session).
func searchAPIsK(raw any, def int) (int, bool) {
	if raw == nil {
		return def, true
	}
	n, isNum := numericArg(raw)
	if !isNum || n != math.Trunc(n) {
		return 0, false
	}
	if n < searchAPIsKMin || n > searchAPIsKMax {
		return 0, false
	}
	return int(n), true
}

func intArg(args map[string]any, key string, def int) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return int(n)
		}
	}
	return def
}

func mapArg(args map[string]any, key string) map[string]any {
	if v, ok := args[key].(map[string]any); ok {
		return v
	}
	return map[string]any{}
}

func errorResult(msg string) *sdkmcp.CallToolResult {
	return &sdkmcp.CallToolResult{
		IsError: true,
		Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: msg}},
	}
}

// failureResult is the one error path for a failure the handler could not
// classify itself.
//
// A structured error renders as its own JSON envelope (spec §1421), so the
// caller sees error_code, message, and the flattened detail fields
// (confirmation_token, reason, scope). Anything else used to fall through to
// free text with IsError=true and no code at all, which left the agent a bare
// sentence to parse. Such an error now gets the SERVICE_DOWN envelope spec §3.1
// step 7 already assigns to an internal failure the caller cannot classify.
func failureResult(err error) *sdkmcp.CallToolResult {
	var se *dispatch.StructuredError
	if errors.As(err, &se) {
		return jsonErrorResult(se)
	}
	return jsonErrorResult(map[string]any{
		"error_code": string(dispatch.ErrCodeServiceDown),
		"message":    err.Error(),
		"retryable":  false,
	})
}

// jsonErrorResult marshals v to JSON and returns it as an error result.
// Use instead of errorResult(string(mustJSON(v))) to avoid the marshal/cast duplication.
func jsonErrorResult(v any) *sdkmcp.CallToolResult {
	b, err := json.Marshal(v)
	if err != nil {
		return errorResult(fmt.Sprintf("JSON_ENCODE_FAILED: %v", err))
	}
	return errorResult(string(b))
}

func jsonResult(v any) *sdkmcp.CallToolResult {
	b, err := json.Marshal(v)
	if err != nil {
		return errorResult(fmt.Sprintf("JSON_ENCODE_FAILED: %v", err))
	}
	return &sdkmcp.CallToolResult{
		Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: string(b)}},
	}
}

// structuredJSONResult is jsonResult plus the structuredContent the tool's
// registered outputSchema promises. A tool that advertises an outputSchema and
// returns text only violates spec §3175; the pinned go-sdk's low-level AddTool
// leaves that validation to the caller and catches nothing, so the pairing is
// enforced by TestEveryRegisteredToolPairsOutputSchemaWithStructuredContent.
//
// Content stays byte-identical to jsonResult: v is marshalled once for the
// text body and handed to the SDK unchanged for structuredContent.
func structuredJSONResult(v any) *sdkmcp.CallToolResult {
	res := jsonResult(v)
	if res.IsError {
		return res
	}
	res.StructuredContent = v
	return res
}
