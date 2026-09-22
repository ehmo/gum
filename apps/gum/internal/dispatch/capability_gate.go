package dispatch

import (
	"slices"

	"github.com/ehmo/gum/internal/catalog"
)

// unsupportedCapabilitySuggestion is the spec §927 suggestion string, quoted
// verbatim so the envelope matches the contract text.
const unsupportedCapabilitySuggestion = "Use another variant or wait for a typed executor."

// blockedCapabilities returns the atoms a variant declares that no executor
// runs, sorted for a stable envelope. It reads `unsupported_capabilities` when
// the catalog declares it, because that list is the curator's statement of
// which atoms block; the enum fallback covers a variant that named a class in
// `capabilities[]` but left the list empty.
func blockedCapabilities(v *catalog.Variant) []string {
	if v == nil {
		return nil
	}
	atoms := slices.Clone(v.UnsupportedCapabilities)
	for _, atom := range v.Capabilities {
		if catalog.CapabilityExecutable(atom) {
			continue
		}
		if !slices.Contains(atoms, atom) {
			atoms = append(atoms, atom)
		}
	}
	slices.Sort(atoms)
	return atoms
}

// capabilityGate enforces the spec §927 pre-request refusal.
//
// A `typed_executor_required` or `schema_only` variant is searchable and
// describable but not invokable: every declared atom is blocked. Nothing
// enforced that before this gate, so such a variant resolved auth, consumed a
// rate-limit token, and issued an upstream request that could not work, then
// failed with whatever the adapter happened to return.
//
// It runs after routing and before auth, so the refusal costs no credential
// resolution and no upstream call, which is what "before any upstream request"
// requires.
func capabilityGate(inv *Invocation, rv *ResolvedVariant) *StructuredError {
	if rv == nil || rv.Variant == nil {
		return nil
	}
	support := rv.Variant.ExecutionSupport
	if support == "" {
		support = catalog.ExecutionSupportFull
	}
	if support != catalog.ExecutionSupportTypedExecutorRequired && support != catalog.ExecutionSupportSchemaOnly {
		return nil
	}

	blocked := blockedCapabilities(rv.Variant)
	err := NewStructuredError(ErrCodeUnsupportedCapability,
		"variant is not invokable: execution_support="+string(support)).
		WithDetail("suggestion", unsupportedCapabilitySuggestion).
		WithDetail("op_id", opIDOf(inv)).
		WithDetail("variant_id", rv.Variant.VariantID).
		WithDetail("execution_support", string(support))
	// §940 makes `unsupported_capabilities` the discriminator for this branch,
	// and a discriminator is identified by presence. An empty slice still
	// encodes as `[]`, so the key is always there for this cause.
	if blocked == nil {
		blocked = []string{}
	}
	return err.WithDetail("unsupported_capabilities", blocked)
}

// partialCapabilityWarning returns the atoms to name in the §932 warning
// envelope field, or nil when the variant is not `partial`.
//
// §932 keeps a `partial` variant invokable and requires gum.read, gum.write
// and gum.destructive to say which atoms did not run. The field rides in
// `_expression`, which §13 leaves open for `_`-prefixed diagnostics; the three
// result shapes are closed, so a new top-level key would fail the registered
// outputSchema.
func partialCapabilityWarning(v *catalog.Variant) []string {
	if v == nil || v.ExecutionSupport != catalog.ExecutionSupportPartial {
		return nil
	}
	blocked := blockedCapabilities(v)
	if len(blocked) == 0 {
		return nil
	}
	return blocked
}
