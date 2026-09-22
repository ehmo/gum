package mcp

import (
	"slices"

	"github.com/ehmo/gum/internal/catalog"
)

const defaultMaxVariants = 5

// executionSupportFull is the §918 value for an op whose declared atoms are all
// executable. The catalog ABI leaves `execution_support` omitempty and
// `gen-catalog -apply-capabilities` writes it only on the curated variants that
// cannot run one of their atoms, so most variants arrive empty. §13 makes the
// field required at both levels and closes its enum, so an empty string must
// resolve to the value that describes those variants: they execute.
const executionSupportFull = string(catalog.ExecutionSupportFull)

// executionSupport resolves a catalog variant's declared execution support.
func executionSupport(declared catalog.ExecutionSupport) string {
	if declared == "" {
		return executionSupportFull
	}
	return string(declared)
}

// capabilityClassWarnings renders one line per atom the op's default variant
// declares and cannot run.
func capabilityClassWarnings(atoms []string) []string {
	if len(atoms) == 0 {
		return nil
	}
	out := make([]string, 0, len(atoms))
	for _, atom := range atoms {
		out = append(out, atom+" is cataloged but not executable in this release")
	}
	return out
}

type describeOpVariant struct {
	VariantID        string   `json:"variant_id"`
	Stability        string   `json:"stability"`
	InterfaceKind    string   `json:"interface_kind,omitempty"`
	RiskClass        string   `json:"risk_class,omitempty"`
	Scopes           []string `json:"scopes,omitempty"`
	Deprecated       bool     `json:"deprecated,omitempty"`
	ExecutionSupport string   `json:"execution_support"`
}

type describeOpResult struct {
	OpID                 string              `json:"op_id"`
	Title                string              `json:"title"`
	Summary              string              `json:"summary"`
	DefaultVariantID     string              `json:"default_variant_id"`
	Variants             []describeOpVariant `json:"variants"`
	VariantsTotal        int                 `json:"variants_total"`
	VariantsOmittedCount int                 `json:"variants_omitted_count"`
	RiskClass            string              `json:"risk_class"`
	Scopes               []string            `json:"scopes"`
	OutputProfile        string              `json:"output_profile,omitempty"`
	ExecutionSupport     string              `json:"execution_support"`
	SchemaRefs           map[string]string   `json:"schema_refs"`
	RiskOverride         bool                `json:"risk_override,omitempty"`
	RiskOverrideReason   string              `json:"risk_override_reason,omitempty"`

	// CapabilityClassWarnings renders the blocking atoms as prose. §955 makes
	// describe_op surface a new atom here as well as in execution_support, so
	// a caller who reads the answer rather than the discriminator still learns
	// the limit. Omitted when the default variant blocks nothing.
	CapabilityClassWarnings []string `json:"capability_class_warnings,omitempty"`

	// UnsupportedCapabilities is a pointer so the three states stay distinct:
	// absent (execution_support "full", which §13 forbids it on), present and
	// empty, and present with entries. A plain slice cannot express "present
	// and empty" through encoding/json.
	UnsupportedCapabilities *[]string `json:"unsupported_capabilities,omitempty"`
}

func buildDescribeOpResult(op *catalog.Op, maxVariants int) describeOpResult {
	// Use the shared defaultVariant helper; fall back to first variant defensively.
	defVar := defaultVariant(op)
	if defVar == nil && len(op.Variants) > 0 {
		defVar = &op.Variants[0]
	}

	// Build compact projection of each variant, truncated to maxVariants.
	total := len(op.Variants)
	n := total
	if maxVariants > 0 && n > maxVariants {
		n = maxVariants
	}
	variants := make([]describeOpVariant, n)
	for i := 0; i < n; i++ {
		v := op.Variants[i]
		variants[i] = describeOpVariant{
			VariantID:        v.VariantID,
			Stability:        string(v.Stability),
			InterfaceKind:    string(v.InterfaceKind),
			RiskClass:        string(v.RiskClass),
			Scopes:           v.Scopes,
			Deprecated:       slices.Contains(op.DeprecatedVariantIDs, v.VariantID),
			ExecutionSupport: executionSupport(v.ExecutionSupport),
		}
	}

	// schema_refs is always present as an object (even when empty) so callers
	// can unconditionally key into it without nil-checking.
	schemaRefs := map[string]string{}
	if defVar != nil && defVar.Binding != nil {
		if defVar.Binding.RequestRef != "" {
			schemaRefs["input"] = defVar.Binding.RequestRef
		}
		if defVar.Binding.ResponseRef != "" {
			schemaRefs["output"] = defVar.Binding.ResponseRef
		}
	}

	r := describeOpResult{
		OpID:                 op.OpID,
		Title:                op.Title,
		Summary:              op.Summary,
		DefaultVariantID:     op.DefaultVariantID,
		Variants:             variants,
		VariantsTotal:        total,
		VariantsOmittedCount: total - n,
		SchemaRefs:           schemaRefs,
	}
	if defVar != nil {
		r.RiskClass = string(defVar.RiskClass)
		// §13 types `scopes` as an array. A nil slice marshals to null, which
		// fails that type for the 22 catalog variants that declare no scopes.
		r.Scopes = defVar.Scopes
		if r.Scopes == nil {
			r.Scopes = []string{}
		}
		r.OutputProfile = defVar.OutputProfile
		r.ExecutionSupport = executionSupport(defVar.ExecutionSupport)
		if r.ExecutionSupport != executionSupportFull {
			// §13 requires the list on every non-full branch and the variant
			// declares it. Op.Validate already checked the §925 binding, so
			// the only work left is the nil-to-empty conversion §13 needs: the
			// field is required here, and a nil slice marshals to null.
			unsupported := slices.Clone(defVar.UnsupportedCapabilities)
			if unsupported == nil {
				unsupported = []string{}
			}
			r.UnsupportedCapabilities = &unsupported
			r.CapabilityClassWarnings = capabilityClassWarnings(unsupported)
		}
		if defVar.RiskOverride {
			r.RiskOverride = true
			r.RiskOverrideReason = defVar.RiskOverrideReason
		}
	}
	return r
}
