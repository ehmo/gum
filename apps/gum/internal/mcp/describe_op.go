package mcp

import (
	"slices"

	"github.com/ehmo/gum/internal/catalog"
)

const defaultMaxVariants = 5

type describeOpVariant struct {
	VariantID        string   `json:"variant_id"`
	Stability        string   `json:"stability"`
	InterfaceKind    string   `json:"interface_kind,omitempty"`
	RiskClass        string   `json:"risk_class,omitempty"`
	Scopes           []string `json:"scopes,omitempty"`
	Deprecated       bool     `json:"deprecated,omitempty"`
	ExecutionSupport string   `json:"execution_support,omitempty"`
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
			ExecutionSupport: v.ExecutionSupport,
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
		r.Scopes = defVar.Scopes
		r.OutputProfile = defVar.OutputProfile
		r.ExecutionSupport = defVar.ExecutionSupport
		if defVar.RiskOverride {
			r.RiskOverride = true
			r.RiskOverrideReason = defVar.RiskOverrideReason
		}
	}
	return r
}
