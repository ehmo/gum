package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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
// catalog snapshot, and the BM25 index.
func (s *Server) makeMetaToolHandler(name string) sdkmcp.ToolHandler {
	switch name {
	case "gum.search_apis":
		return s.handleSearchAPIs
	case "gum.describe_op":
		return s.handleDescribeOp
	case "gum.read":
		return s.handleRead
	case "gum.write":
		return s.handleWrite
	case "gum.destructive":
		return s.handleDestructive
	case "gum.code":
		return s.handleCode
	case "gum.poll":
		return s.handlePoll
	case "gum.cache_stats":
		return s.handleCacheStats
	case "gum.gain":
		return s.handleGain
	}
	return s.handleUnknown(name)
}

// makeConvenienceHandler routes a convenience tool through the catalog by
// looking up its mapped op_id and dispatching with the appropriate risk-class
// flags.
func (s *Server) makeConvenienceHandler(toolName string) sdkmcp.ToolHandler {
	return func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		args := parseArgs(req)
		opID, ok := convenienceOpRouting[toolName]
		if !ok {
			return errorResult(fmt.Sprintf("CONVENIENCE_NOT_WIRED: %s has no catalog mapping in v0.1.0", toolName)), nil
		}
		invArgs := copyArgsWithoutControls(args, "confirmed", "confirmation_token")
		inv := buildInvocation(opID, invArgs)
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
}

// handleSearchAPIs runs a BM25 query and returns spec §4.1 / §2129 TOON tuples.
// The response is routed through profile.Apply with the spec §2129 implicit
// profile (hardcoded, not user-overridable per spec §9.4).
func (s *Server) handleSearchAPIs(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
	args := parseArgs(req)
	query := stringArg(args, "query")
	if query == "" {
		return errorResult("INVALID_ARGS: query is required"), nil
	}
	tuning := loadSearchAPIsTuning(s.profile.String())
	k := intArg(args, "k", tuning.k)

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

	out, err := profile.Apply(searchAPIsProfile(k, tuning), profile.ApplyInput{
		Body:       bodyJSON,
		UserFormat: "", // spec §9.4: meta-tool profiles are not overridable
	})
	if err != nil {
		return errorResult(fmt.Sprintf("PROFILE_APPLY_FAILED: %v", err)), nil
	}

	return &sdkmcp.CallToolResult{
		Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: string(out.Body)}},
	}, nil
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
	return jsonResult(result), nil
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
				hits := idx.Search(opID, 5)
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

		// Verify the catalog op's risk_class matches the meta-tool tier.
		v := defaultVariant(op)
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
	if v, ok := args["page_size"]; ok {
		if n, isNum := numericArg(v); isNum && n > 0 {
			innerArgs[s.canonicalPageSizeParam(opID)] = v
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
	// Inherit per-tier flags from the outer args.
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
	inv.Format = stringArg(args, "format")

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
			return jsonResult(map[string]any{
				"error_code":     "LRO_TIMEOUT",
				"operation_name": te.OperationName,
				"resume_handle":  te.OperationName,
				"suggestion":     "Call gum.poll again with the same operation_name to resume polling.",
			}), nil
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return errorResult(`{"error_code":"CANCELLED"}`), nil
		}
		return errorResult(err.Error()), nil
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
	return jsonResult(result), nil
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
	var session *sdkmcp.ServerSession
	if req != nil {
		session = req.Session
	}
	return jsonResult(cacheStatsEnvelope(sem, s.auditBroken(), clientSupportsPromptCache(session))), nil
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
	// Spec §2570: GAIN_DISABLED terminal error.
	if os.Getenv("GUM_GAIN_DISABLED") == "1" {
		return jsonErrorResult(map[string]any{"error_code": "GAIN_DISABLED"}), nil
	}
	// Spec §2541: GAIN_LEDGER_UNAVAILABLE terminal error.
	ledger, err := gain.NewLedger("")
	if err != nil {
		return jsonErrorResult(map[string]any{
			"error_code": "GAIN_LEDGER_UNAVAILABLE",
			"hint":       "Enable server-side gain ledger storage or configure telemetry export for product analytics.",
		}), nil
	}
	defer func() { _ = ledger.Close() }()
	return jsonResult(gainSuccessEnvelope(ledger.Stats())), nil
}

// gainSuccessEnvelope builds the 9-key spec §2793 GainResult map from ledger
// stats. baseline_tokens is the naive raw-token total (TotalTokensIn) and
// actual_tokens is the shaped total, so savings_pct is the real reduction.
func gainSuccessEnvelope(stats gain.Stats) map[string]any {
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
		"end_to_end_savings":      savingsPct, // v0.1.0: same as savings_pct
		"batch_envelope_overhead": int64(0),
		"tokenizer":               "cl100k_base",
		"sessions":                []any{}, // summary-mode: empty array (spec §2818-2833)
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
	return jsonResult(map[string]any{"skills": skillreg.DefaultRegistry().List()}), nil
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
		return errorResult(err.Error()), nil
	}
	truncated := false
	if args.MaxBytes > 0 && len([]byte(skill.Body)) > args.MaxBytes {
		body := []byte(skill.Body)
		skill.Body = string(body[:args.MaxBytes])
		truncated = true
	}
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
// req may be nil in unit tests that bypass the SDK transport; in that case
// project-local resolution is skipped and dispatch proceeds with the
// catalog-default profile.
func (s *Server) dispatchToolCall(ctx context.Context, req *sdkmcp.CallToolRequest, inv *dispatch.Invocation) (*sdkmcp.CallToolResult, error) {
	if req != nil && req.Session != nil {
		metaGumRoot := stringFromMeta(req, "gumRoot")
		rootPath, projErr := s.ResolveProjectRootForRequest(ctx, req.Session, metaGumRoot)
		if projErr != nil {
			return jsonErrorResult(projectRootRequiredEnvelope(projErr)), nil
		}
		if profName := s.profileNameForOp(inv.OpID); profName != "" {
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
		// Render structured errors as JSON envelopes (spec §1421) so the
		// MCP caller sees a parseable object with error_code, message, and
		// flattened detail fields (e.g. confirmation_token, reason).
		var se *dispatch.StructuredError
		if errors.As(err, &se) {
			return jsonErrorResult(se), nil
		}
		return errorResult(err.Error()), nil
	}
	res := &sdkmcp.CallToolResult{
		Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: string(shaped.Body)}},
	}
	if shaped.StructuredContent != nil {
		res.StructuredContent = shaped.StructuredContent
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
