package main

import (
	"testing"

	"github.com/spf13/cobra"
)

// TestCompleteFieldsForOpUnknownOpID exercises the loop-exhaustion branch:
// when the supplied op_id doesn't match any op in the embedded catalog,
// the function falls through to the trailing return — exactly the same
// "no suggestions" directive as the empty-args case.
func TestCompleteFieldsForOpUnknownOpID(t *testing.T) {
	if loadCatalog() == nil {
		t.Skip("embedded catalog unavailable")
	}
	results, directive := completeFieldsForOp(nil, []string{"definitely.not.an.op"}, "")
	if results != nil {
		t.Errorf("got %v; want nil for unknown op_id", results)
	}
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("directive=%d; want ShellCompDirectiveNoFileComp", directive)
	}
}

// TestCompleteFieldsForOpKnownOpEmptyDefaultFields covers the inner
// "v.DefaultFields == ''" branch. The embedded catalog's stable op
// gmail.users.messages.list resolves a default variant, but that variant
// currently has no default_fields, so the helper returns nil — which is
// exactly the safety net for ops that haven't published a default mask.
func TestCompleteFieldsForOpKnownOpEmptyDefaultFields(t *testing.T) {
	snap := loadCatalog()
	if snap == nil {
		t.Skip("embedded catalog unavailable")
	}
	// Find any op whose default variant has no DefaultFields; the embedded
	// catalog has zero default_fields entries so the first op works.
	var opID string
	for i := range snap.Ops {
		v := defaultVariant(&snap.Ops[i])
		if v != nil && v.DefaultFields == "" {
			opID = snap.Ops[i].OpID
			break
		}
	}
	if opID == "" {
		t.Skip("no op with empty default_fields in embedded catalog")
	}
	results, directive := completeFieldsForOp(nil, []string{opID}, "")
	if results != nil {
		t.Errorf("got %v; want nil for op without default_fields", results)
	}
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("directive=%d", directive)
	}
}
