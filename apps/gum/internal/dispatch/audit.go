// Package dispatch — audit helpers and panic-recovery utilities (spec.md §3.1 step 7, line 235).
package dispatch

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strings"

	"github.com/ehmo/gum/internal/catalog"
)

// auditSink is an internal interface for receiving audit log entries from the
// dispatch kernel (spec §3.1 step 7). The test package's AuditSink interface
// is structurally identical; any value satisfying Append(map[string]any) works.
type auditSink interface {
	Append(entry map[string]any)
}

// NewDispatcherWithAudit constructs the dispatch kernel with an optional audit
// sink. Panic recovery appends an entry with panic:true to the sink on each
// adapter panic (spec §3.1 step 7, line 235).
func NewDispatcherWithAudit(snapshot *catalog.Catalog, adapters map[string]Adapter, sink interface{ Append(entry map[string]any) }) Dispatcher {
	return &dispatcher{
		snapshot:  snapshot,
		adapters:  adapters,
		auditSink: sink,
	}
}

// runtimeAddr matches hex addresses embedded in stack-trace frame lines
// (e.g. "0x1ab" in "/path/file.go:42 +0x1ab"). Compiled once at package
// init to avoid repeated regexp compilation per panic event.
var runtimeAddr = regexp.MustCompile(`0x[0-9a-fA-F]+`)

// sanitizeStackForLog is the §11 layer-2 sanitizer for stack traces.
// WHY: raw debug.Stack() output contains absolute file-system paths (which
// may expose workspace layout or user home directory) and memory addresses
// (which are useless for debugging but add noise). We keep only the base
// filename + line number and function/package names so that "runtime" is
// still visible in the output (required for test assertions and diagnosis)
// while filesystem paths and pointer values are removed.
func sanitizeStackForLog(raw string) string {
	lines := strings.Split(raw, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		// File lines look like: "\t/some/absolute/path/file.go:42 +0x1ab"
		// We want to keep "file.go:42" and strip the path and the hex offset.
		trimmed := strings.TrimLeft(line, "\t ")
		if strings.HasPrefix(trimmed, "/") || (len(trimmed) > 2 && trimmed[1] == ':' && trimmed[0] >= 'A') {
			// Likely an absolute path line — keep only the base name + line number.
			// Strip the trailing " +0x..." part first.
			spaceIdx := strings.Index(trimmed, " ")
			if spaceIdx != -1 {
				trimmed = trimmed[:spaceIdx]
			}
			trimmed = filepath.Base(trimmed)
		}
		// Strip any remaining hex addresses.
		trimmed = runtimeAddr.ReplaceAllString(trimmed, "")
		out = append(out, trimmed)
	}
	result := strings.Join(out, "\n")
	return result
}

// panicAuditEntry builds the audit-log map for a recovered adapter panic.
// Required keys per spec §3.1 step 7 (line 235): panic, op_id, variant_id,
// args_hash, risk_class. canonicalArgs is the dispatcher-normalized args
// (spec §10.0 Rule 4 datetime normalization when enabled); the hash MUST
// match the cache key and tee hash to satisfy "all reference args_canonical"
// (spec §10.0 paragraph 1).
func panicAuditEntry(inv *Invocation, rv *ResolvedVariant, canonicalArgs map[string]any) map[string]any {
	return map[string]any{
		"panic":      true,
		"op_id":      inv.OpID,
		"variant_id": rv.Variant.VariantID,
		"args_hash":  argsHashHex(canonicalArgs),
		"client_id":  callerToClientID(inv.Caller),
		"risk_class": string(rv.Variant.RiskClass),
	}
}

// successAuditEntry builds the normative §11 audit-log map for a successful
// dispatch. The on-disk writer (internal/auditlog) stamps `v`, `ts`, and key
// order; this helper supplies the per-invocation payload. dual_fetch is
// emitted when the active expression profile selects field_mask_mode=
// "dual_fetch" (spec §9.1 + §11). shaping_bypassed and sanitizer_bypassed
// remain deferred to follow-on beads.
func successAuditEntry(inv *Invocation, rv *ResolvedVariant, canonicalArgs map[string]any) map[string]any {
	variantID := ""
	riskClass := ""
	riskOverride := false
	var riskOverrideReason any // nil → null in the JSONL emission
	if rv != nil && rv.Variant != nil {
		variantID = rv.Variant.VariantID
		riskClass = string(rv.Variant.RiskClass)
		riskOverride = rv.Variant.RiskOverride
		if rv.Variant.RiskOverrideReason != "" {
			riskOverrideReason = rv.Variant.RiskOverrideReason
		}
	}
	entry := map[string]any{
		"op_id":         inv.OpID,
		"variant_id":    variantID,
		"args_hash":     argsHashHex(canonicalArgs),
		"client_id":     callerToClientID(inv.Caller),
		"risk_class":    riskClass,
		"risk_override": riskOverride,
	}
	if riskOverride && riskOverrideReason != nil {
		entry["risk_override_reason"] = riskOverrideReason
	}
	if inv.OutputProfile != nil && inv.OutputProfile.FieldMaskMode == "dual_fetch" {
		entry["dual_fetch"] = true
	}
	return entry
}

// argsHashHex returns the SHA-256 hex of the canonical args JSON. Spec §11
// requires `args_hash` to be the digest, not the canonical string itself.
func argsHashHex(args map[string]any) string {
	canonical := canonicalizeArgs(args)
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}

// callerToClientID maps the dispatch Caller enum to the §11 audit-log
// `client_id` field. The MCP layer surfaces "mcp" rather than the connected
// client's Implementation.Name in v0.1.0; threading that through is gum-gv9a
// scope.
func callerToClientID(c Caller) string {
	if c == "" {
		return "unknown"
	}
	return string(c)
}

// recoverAdapterPanic is the named recover helper for executeAdapter's deferred
// block. It converts any adapter panic into a SERVICE_DOWN StructuredError,
// logs the sanitized stack at ERROR level, and optionally appends an audit
// entry. The resp and err double-pointers let the deferred call write back into
// executeAdapter's named return values without a wrapper closure.
func (d *dispatcher) recoverAdapterPanic(inv *Invocation, rv *ResolvedVariant, resp **Response, err *error) {
	r := recover()
	if r == nil {
		return
	}

	stack := debug.Stack()
	sanitizedStack := sanitizeStackForLog(string(stack))

	slog.Error("adapter panic",
		"op_id", inv.OpID,
		"variant_id", rv.Variant.VariantID,
		"request_id", inv.RequestID,
		"panic_value", fmt.Sprintf("%v", r),
		"stack", sanitizedStack,
	)

	if d.auditSink != nil {
		d.auditSink.Append(panicAuditEntry(inv, rv, d.canonicalArgs(inv.Args)))
	}

	*err = NewStructuredError(ErrCodeServiceDown, "internal error; see audit log").WithRetryable(false)
	*resp = nil
}
