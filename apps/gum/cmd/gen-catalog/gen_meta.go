package main

import "github.com/ehmo/gum/internal/catalog"

// BuildMetaOps emits the "meta" service family: gum's own ops that are not
// backed by a Google REST endpoint but by an in-process adapter. Today this is
// the single gum.code op — the flagship sandbox-execution verb wired to the
// Risor runner (adapter_key=code.risor).
//
// The op MUST be appended to the generated catalog so that both the `gum code`
// CLI verb and the gum.code MCP tool resolve in the shipped binary. The
// dispatcher's findOp looks the op up in the embedded catalog snapshot; if it
// is absent every invocation returns OP_NOT_FOUND (the gum-7ras P0). It is
// emitted here rather than parsed from a discovery doc because it has no
// upstream HTTP binding.
//
// auth_strategy is "none": the meta-op carries no upstream credential. Any
// catalog op the Risor script calls via gum_call is re-dispatched through a
// fresh lifecycle that resolves THAT op's own auth_strategy, so credentials are
// never bypassed — they are resolved per sub-call, not at the meta layer.
func BuildMetaOps() []catalog.Op {
	return []catalog.Op{
		{
			OpID:            "gum.code",
			OpSchemaVersion: 1,
			Title:           "Execute code in sandbox",
			Summary:         "Executes a Risor snippet that may call catalog ops via gum_call.",
			ParamsRequired: [][]string{
				{"language", "string"},
				{"source", "string"},
			},
			ParamsOptional: [][]string{
				{"allow_write", "bool"},
				{"allow_destructive", "bool"},
				{"destructive_budget", "int"},
				{"destructive_scope", "string"},
				{"confirmed", "bool"},
				{"confirmation_token", "string"},
			},
			ServiceFamily:    "meta",
			Service:          "meta",
			DefaultVariantID: "gum.code.v1.risor",
			Variants: []catalog.Variant{
				{
					VariantID:            "gum.code.v1.risor",
					VariantSchemaVersion: 1,
					Version:              "v1",
					Stability:            catalog.StabilityStable,
					InterfaceKind:        catalog.InterfaceKindSDKNative,
					BackendKind:          catalog.BackendKindTypedRestSDK,
					Preferred:            true,
					RiskClass:            catalog.RiskClassRead,
					AuthStrategy:         catalog.AuthStrategyNone,
					Capabilities:         []string{"code_execution"},
					DefaultFormat:        "json",
					Binding: &catalog.Binding{
						BindingSchemaVersion: 1,
						AdapterKey:           "code.risor",
						OperationKey:         "gum.code.risor.exec",
					},
				},
			},
		},
	}
}
