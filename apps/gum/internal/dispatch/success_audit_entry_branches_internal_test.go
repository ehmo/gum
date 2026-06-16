package dispatch

import (
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

// TestSuccessAuditEntryRiskOverrideReasonIsSurfaced pins the
// `RiskOverride && RiskOverrideReason != "" → entry["risk_override_reason"]`
// arm. Spec §11 requires the audit-log to record WHY a risk override was
// granted (so a reviewer can post-hoc justify a destructive call), not
// just the boolean — emitting the bool without the reason would hide
// the operator's intent in the log.
func TestSuccessAuditEntryRiskOverrideReasonIsSurfaced(t *testing.T) {
	inv := &Invocation{OpID: "demo.op"}
	rv := &ResolvedVariant{
		Variant: &catalog.Variant{
			VariantID:          "demo.v1",
			RiskClass:          catalog.RiskClass("destructive"),
			RiskOverride:       true,
			RiskOverrideReason: "incident response: revoke leaked key",
		},
	}

	entry := successAuditEntry(inv, rv, map[string]any{})

	if got, _ := entry["risk_override"].(bool); !got {
		t.Errorf("risk_override=%v; want true", entry["risk_override"])
	}
	if got, _ := entry["risk_override_reason"].(string); got != "incident response: revoke leaked key" {
		t.Errorf("risk_override_reason=%q; want incident response: revoke leaked key", entry["risk_override_reason"])
	}
}

// TestSuccessAuditEntryEmptyReasonNotEmitted pins the negative
// complement: when RiskOverride=true but Reason is empty, the
// `risk_override_reason` key MUST be absent from the entry (not emitted
// as ""). This keeps the JSONL output free of misleading empty-string
// reasons that downstream review tools would otherwise count as
// "reason provided".
func TestSuccessAuditEntryEmptyReasonNotEmitted(t *testing.T) {
	inv := &Invocation{OpID: "demo.op"}
	rv := &ResolvedVariant{
		Variant: &catalog.Variant{
			VariantID:    "demo.v1",
			RiskOverride: true,
			// RiskOverrideReason intentionally left empty.
		},
	}

	entry := successAuditEntry(inv, rv, map[string]any{})

	if _, ok := entry["risk_override_reason"]; ok {
		t.Errorf("risk_override_reason key present despite empty reason: %v", entry["risk_override_reason"])
	}
}
