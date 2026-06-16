package main

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/ehmo/gum/internal/catalog"
)

// completeOpIDByRisk returns a cobra completion function that proposes op_ids
// from the embedded catalog whose default variant matches the given risk class.
// Pass an empty string to allow any risk class (used for `gum describe`).
func completeOpIDByRisk(riskClass string) func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		snap := loadCatalog()
		if snap == nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		var out []string
		want := catalog.RiskClass(riskClass)
		for i := range snap.Ops {
			op := &snap.Ops[i]
			if riskClass != "" {
				v := defaultVariant(op)
				if v == nil || v.RiskClass != want {
					continue
				}
			}
			if toComplete == "" || strings.HasPrefix(op.OpID, toComplete) {
				out = append(out, op.OpID)
			}
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	}
}

// defaultVariant returns the variant whose VariantID matches op.DefaultVariantID,
// or the first variant if no match is found.
func defaultVariant(op *catalog.Op) *catalog.Variant {
	for i := range op.Variants {
		if op.Variants[i].VariantID == op.DefaultVariantID {
			return &op.Variants[i]
		}
	}
	if len(op.Variants) > 0 {
		return &op.Variants[0]
	}
	return nil
}
