package mcp

import (
	"github.com/ehmo/gum/internal/dispatch"
	"github.com/ehmo/gum/internal/output/profile"
)

// SetSuppressLossyWarnings silences the §9.2 shadowing warning for this server.
// The `--no-warn-lossy` flag (and its `--no-warn-recovery` alias) is a root
// persistent flag, so `gum mcp` reads it before Run and injects it here; the
// package cannot reach cmd/gum state on its own.
func (s *Server) SetSuppressLossyWarnings(suppress bool) {
	s.suppressLossyWarnings = suppress
}

// warnShadowedProfile is the spec §9.2 shadowing warning at the runtime profile
// loader. It runs per tool call, after the project root is known and before
// dispatch, and never changes what is dispatched: §9.2 says the warning "fires
// for operator awareness only; it never blocks execution".
//
// Two shadow shapes reach this point, and both compare the same six
// loss-driving fields:
//
//   - A filesystem layer supplies a profile under the name the catalog variant
//     already uses, displacing the catalog-embedded body. The op is the target.
//   - An [override_bindings] entry attaches a different profile to this op or
//     to the pinned variant. The bound key is the target, which is still an op
//     or variant id, so the wire text names an operation either way.
//
// The stdio transport owns stdout, so the line goes to the log channel §9.2
// allows ("the stderr / log line"). Structured fields carry the class, target,
// and both values; msg carries the exact wire text.
func (s *Server) warnShadowedProfile(rootPath string, inv *dispatch.Invocation) {
	if s.suppressLossyWarnings || inv == nil || rootPath == "" {
		return
	}

	catalogName := s.catalogProfileNameForInvocation(inv)
	if catalogName == "" {
		return
	}
	catalogProfile, ok := profile.BuiltinLookup(catalogName)
	if !ok {
		return
	}

	target, overrideName := s.shadowTargetForInvocation(rootPath, inv, catalogName)
	if overrideName == "" {
		return
	}

	override, source, err := profile.ResolveProfile(rootPath, overrideName, profile.BuiltinLookup)
	if err != nil {
		return
	}
	if source != profile.SourceProjectLocal && source != profile.SourceUserGlobal {
		return // the catalog body won; nothing displaced it
	}

	for _, w := range profile.DetectShadowing(target, catalogProfile, override) {
		s.log().Warn(w.String(),
			"class", profile.WarnOverrideDisablesLossyStage,
			"target", w.Target,
			"field", w.Field,
			"old_value", w.OldValue,
			"new_value", w.NewValue,
		)
	}
}

// shadowTargetForInvocation reports which name the warning should name and which
// profile the override side resolves. A binding keyed on the pinned variant wins
// over one keyed on the op, matching profileNameForRequest; with no binding, the
// op is the target and the catalog profile name is what a filesystem layer may
// have displaced.
func (s *Server) shadowTargetForInvocation(rootPath string, inv *dispatch.Invocation, catalogName string) (target, overrideName string) {
	bindings, err := profile.LoadOverrideBindings(rootPath)
	if err == nil {
		if inv.RequestedVariantID != "" {
			if name, ok := bindings[inv.RequestedVariantID]; ok {
				return inv.RequestedVariantID, name
			}
		}
		if name, ok := bindings[inv.OpID]; ok {
			return inv.OpID, name
		}
	}
	return inv.OpID, catalogName
}

// catalogProfileNameForInvocation returns the output_profile the catalog names
// for this call: the pinned variant's own when the request pins one, otherwise
// the default variant's.
func (s *Server) catalogProfileNameForInvocation(inv *dispatch.Invocation) string {
	op := s.findOp(inv.OpID)
	if op == nil {
		return ""
	}
	if inv.RequestedVariantID != "" {
		for _, v := range op.Variants {
			if v.VariantID == inv.RequestedVariantID {
				return v.OutputProfile
			}
		}
	}
	v := defaultVariant(op)
	if v == nil {
		return ""
	}
	return v.OutputProfile
}
