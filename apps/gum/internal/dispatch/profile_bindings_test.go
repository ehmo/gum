package dispatch_test

// profile_bindings_test.go — gum-b3sm: spec §9.2 [override_bindings] at
// dispatch time. Before this change the table had no runtime reader, so a
// project-local binding changed nothing.

import (
	"context"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/dispatch"
	"github.com/ehmo/gum/internal/output/profile"
)

// bindingLookup resolves the two profiles used below: the catalog default,
// which keeps "text", and the bound one, which keeps "drop" instead. The two
// keep sets are disjoint, so the shaped body names which profile ran.
func bindingLookup(name string) (*profile.Profile, bool) {
	switch name {
	case "test.compact":
		return &profile.Profile{Name: name, DefaultFormat: "json", KeepFields: []string{"results.text"}}, true
	case "bound.profile":
		return &profile.Profile{Name: name, DefaultFormat: "json", KeepFields: []string{"results.drop"}}, true
	}
	return nil, false
}

func dispatchWithBindings(t *testing.T, bindings map[string]string) string {
	t.Helper()
	disp := dispatch.NewDispatcherWithConfig(profileTestCatalog(),
		map[string]dispatch.Adapter{"fake": fakeJSONAdapter{verboseBody}},
		dispatch.DispatcherConfig{
			ProfileLookup:   bindingLookup,
			ProfileBindings: func() map[string]string { return bindings },
		},
	)
	res, err := disp.Dispatch(context.Background(), &dispatch.Invocation{OpID: "fake.op", Format: "json"})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	return string(res.Body)
}

// TestOverrideBindingByOpIDWins: a binding keyed on op_id replaces the profile
// the variant names.
func TestOverrideBindingByOpIDWins(t *testing.T) {
	body := dispatchWithBindings(t, map[string]string{"fake.op": "bound.profile"})
	if !strings.Contains(body, "gone") {
		t.Errorf("bound profile did not run; body=%s", body)
	}
	if strings.Contains(body, "shoes") {
		t.Errorf("catalog profile still ran; body=%s", body)
	}
}

// TestOverrideBindingByVariantIDBeatsOpID: both keys bind the same call, and
// the variant_id is the more specific of the two.
func TestOverrideBindingByVariantIDBeatsOpID(t *testing.T) {
	body := dispatchWithBindings(t, map[string]string{
		"fake.op": "test.compact",
		"fake.v1": "bound.profile",
	})
	if !strings.Contains(body, "gone") {
		t.Errorf("variant_id binding lost to op_id binding; body=%s", body)
	}
}

// TestOverrideBindingIgnoresOtherTargets is the control: a table that binds
// some other op leaves this call on its catalog profile.
func TestOverrideBindingIgnoresOtherTargets(t *testing.T) {
	body := dispatchWithBindings(t, map[string]string{"other.op": "bound.profile"})
	if !strings.Contains(body, "shoes") {
		t.Errorf("catalog profile did not run; body=%s", body)
	}
	if strings.Contains(body, "gone") {
		t.Errorf("an unrelated binding applied; body=%s", body)
	}
}

// TestOverrideBindingAttachesProfileToUnprofiledVariant is the case the table
// exists for: the variant names no output_profile, and the binding gives it
// one without a catalog rebuild.
func TestOverrideBindingAttachesProfileToUnprofiledVariant(t *testing.T) {
	cat := profileTestCatalog()
	cat.Ops[0].Variants[0].OutputProfile = ""
	disp := dispatch.NewDispatcherWithConfig(cat,
		map[string]dispatch.Adapter{"fake": fakeJSONAdapter{verboseBody}},
		dispatch.DispatcherConfig{
			ProfileLookup:   bindingLookup,
			ProfileBindings: func() map[string]string { return map[string]string{"fake.op": "bound.profile"} },
		},
	)
	res, err := disp.Dispatch(context.Background(), &dispatch.Invocation{OpID: "fake.op", Format: "json"})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if strings.Contains(string(res.Body), "shoes") {
		t.Errorf("binding did not shape an unprofiled variant; body=%s", res.Body)
	}
}

// TestPresetOutputProfileBeatsBinding: the presentation layer resolved the
// filesystem layers itself (MCP does this under the client's project root), so
// the kernel must not overwrite what it set.
func TestPresetOutputProfileBeatsBinding(t *testing.T) {
	disp := dispatch.NewDispatcherWithConfig(profileTestCatalog(),
		map[string]dispatch.Adapter{"fake": fakeJSONAdapter{verboseBody}},
		dispatch.DispatcherConfig{
			ProfileLookup:   bindingLookup,
			ProfileBindings: func() map[string]string { return map[string]string{"fake.op": "bound.profile"} },
		},
	)
	preset := &profile.Profile{Name: "preset", DefaultFormat: "json", KeepFields: []string{"results.text"}}
	res, err := disp.Dispatch(context.Background(), &dispatch.Invocation{OpID: "fake.op", Format: "json", OutputProfile: preset})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !strings.Contains(string(res.Body), "shoes") {
		t.Errorf("preset profile was overwritten by a binding; body=%s", res.Body)
	}
}
