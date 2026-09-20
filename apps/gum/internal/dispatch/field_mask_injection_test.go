// field_mask_injection_test.go — spec §9.1 stage 1, upstream projection.
//
// gum-tthf: the profile DSL declares field_mask as stage 1, but the parser had
// no case for it and no dispatch step put it on the wire, so stage 1 never ran.
// The only mask gum sends upstream is the universal Google `fields` query
// parameter, so injection means writing Args["fields"] before the executor.
package dispatch_test

import (
	"context"
	"testing"

	"github.com/ehmo/gum/internal/dispatch"
	"github.com/ehmo/gum/internal/output/profile"
)

// argRecorder returns upstreamBody and keeps the Args the kernel handed it.
type argRecorder struct{ args map[string]any }

func (a *argRecorder) Execute(_ context.Context, inv *dispatch.Invocation, _ *dispatch.ResolvedVariant, _ *dispatch.Credentials) (*dispatch.Response, error) {
	a.args = map[string]any{}
	for k, v := range inv.Args {
		a.args[k] = v
	}
	return &dispatch.Response{
		Body:       []byte(upstreamBody),
		Format:     "json",
		StatusCode: 200,
		BytesOut:   len(upstreamBody),
	}, nil
}

// dispatchWithMask runs one call and returns the fields arg the executor saw.
func dispatchWithMask(t *testing.T, opID string, inv *dispatch.Invocation) (string, bool) {
	t.Helper()
	adapterKey := opID + ".adapter"
	rec := &argRecorder{}
	disp := dispatch.NewDispatcherWithConfig(
		minimalCatalogFor(opID, adapterKey),
		map[string]dispatch.Adapter{adapterKey: rec},
		dispatch.DispatcherConfig{},
	)
	inv.OpID = opID
	inv.Format = "json"
	if _, err := disp.Dispatch(context.Background(), inv); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	got, ok := rec.args["fields"].(string)
	return got, ok
}

// TestProfileFieldMaskReachesUpstream is the stage-1 positive case.
func TestProfileFieldMaskReachesUpstream(t *testing.T) {
	got, ok := dispatchWithMask(t, "test.mask.inject", &dispatch.Invocation{
		Args:          map[string]any{},
		OutputProfile: &profile.Profile{FieldMask: "results(text,metrics/avg)"},
	})
	if !ok || got != "results(text,metrics/avg)" {
		t.Errorf("upstream fields = %q (present=%v); want the profile's field_mask", got, ok)
	}
}

// TestCallerFieldsBeatsTheProfileMask: an explicit caller mask is a narrower
// instruction than the profile default, so the profile must not overwrite it.
func TestCallerFieldsBeatsTheProfileMask(t *testing.T) {
	got, _ := dispatchWithMask(t, "test.mask.caller", &dispatch.Invocation{
		Args:          map[string]any{"fields": "results(text)"},
		OutputProfile: &profile.Profile{FieldMask: "results(text,metrics/avg)"},
	})
	if got != "results(text)" {
		t.Errorf("upstream fields = %q; want the caller's own mask", got)
	}
}

// TestFieldMaskModeNoneSkipsInjection: field_mask_mode="none" is the documented
// "host-side shaping only" setting, so it must leave the wire unmasked.
func TestFieldMaskModeNoneSkipsInjection(t *testing.T) {
	_, ok := dispatchWithMask(t, "test.mask.none", &dispatch.Invocation{
		Args: map[string]any{},
		OutputProfile: &profile.Profile{
			FieldMask:     "results(text)",
			FieldMaskMode: profile.FieldMaskModeNone,
		},
	})
	if ok {
		t.Error("upstream carried a fields arg; field_mask_mode=none skips stage 1")
	}
}

// TestSuppressFieldMaskSkipsInjection: --no-field-mask deletes a caller mask
// already, and it has to stop the profile from putting one back.
func TestSuppressFieldMaskSkipsInjection(t *testing.T) {
	_, ok := dispatchWithMask(t, "test.mask.suppress", &dispatch.Invocation{
		Args:              map[string]any{},
		SuppressFieldMask: true,
		OutputProfile:     &profile.Profile{FieldMask: "results(text)"},
	})
	if ok {
		t.Error("upstream carried a fields arg; --no-field-mask skips stage 1")
	}
}

// TestNoProfileMaskLeavesArgsAlone keeps the default path unchanged: no shipped
// profile declares field_mask, so nothing new goes on the wire for them.
func TestNoProfileMaskLeavesArgsAlone(t *testing.T) {
	_, ok := dispatchWithMask(t, "test.mask.absent", &dispatch.Invocation{
		Args:          map[string]any{},
		OutputProfile: &profile.Profile{KeepFields: []string{"results.text"}},
	})
	if ok {
		t.Error("upstream carried a fields arg for a profile with no field_mask")
	}
}

// dispatchWithVariantDefaultFields runs one call against a variant that
// declares default_fields and returns the fields arg the executor saw.
func dispatchWithVariantDefaultFields(t *testing.T, opID, defaultFields string, inv *dispatch.Invocation) (string, bool) {
	t.Helper()
	adapterKey := opID + ".adapter"
	snap := minimalCatalogFor(opID, adapterKey)
	snap.Ops[0].Variants[0].DefaultFields = defaultFields
	rec := &argRecorder{}
	disp := dispatch.NewDispatcherWithConfig(
		snap,
		map[string]dispatch.Adapter{adapterKey: rec},
		dispatch.DispatcherConfig{},
	)
	inv.OpID = opID
	inv.Format = "json"
	if _, err := disp.Dispatch(context.Background(), inv); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	got, ok := rec.args["fields"].(string)
	return got, ok
}

// TestVariantDefaultFieldsIsTheProfileMaskDefault pins gum-jnxs. The §9.1 DSL
// field table defaults `field_mask` to the variant's `default_fields`, and
// docs/expression-profile-dsl.md repeats it. No dispatch step read the variant
// field, so a profile that omitted the key sent no upstream mask, which is
// what field_mask_mode="none" means on the wire.
func TestVariantDefaultFieldsIsTheProfileMaskDefault(t *testing.T) {
	got, ok := dispatchWithVariantDefaultFields(t, "test.mask.default", "nextPageToken,messages(id,threadId)",
		&dispatch.Invocation{
			Args:          map[string]any{},
			OutputProfile: &profile.Profile{KeepFields: []string{"messages.id"}},
		})
	if !ok || got != "nextPageToken,messages(id,threadId)" {
		t.Errorf("upstream fields = %q (present=%v); want the variant's default_fields", got, ok)
	}
}

// TestProfileFieldMaskBeatsVariantDefaultFields: default_fields is the default
// for the DSL key, so a profile that states field_mask overrides it.
func TestProfileFieldMaskBeatsVariantDefaultFields(t *testing.T) {
	got, _ := dispatchWithVariantDefaultFields(t, "test.mask.override", "messages(id,threadId)",
		&dispatch.Invocation{
			Args:          map[string]any{},
			OutputProfile: &profile.Profile{FieldMask: "messages(id)"},
		})
	if got != "messages(id)" {
		t.Errorf("upstream fields = %q; want the profile's own field_mask", got)
	}
}

// TestVariantDefaultFieldsHonorsTheStageOneVetoes: the default mask is still a
// stage-1 injection, so the three vetoes that stop a profile mask stop it too.
func TestVariantDefaultFieldsHonorsTheStageOneVetoes(t *testing.T) {
	cases := []struct {
		name string
		inv  *dispatch.Invocation
		want string
	}{
		{
			name: "caller fields wins",
			inv: &dispatch.Invocation{
				Args:          map[string]any{"fields": "messages(id)"},
				OutputProfile: &profile.Profile{},
			},
			want: "messages(id)",
		},
		{
			name: "field_mask_mode none",
			inv: &dispatch.Invocation{
				Args:          map[string]any{},
				OutputProfile: &profile.Profile{FieldMaskMode: profile.FieldMaskModeNone},
			},
			want: "",
		},
		{
			name: "--no-field-mask",
			inv: &dispatch.Invocation{
				Args:              map[string]any{},
				SuppressFieldMask: true,
				OutputProfile:     &profile.Profile{},
			},
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := dispatchWithVariantDefaultFields(t, "test.mask.veto."+tc.name, "messages(id,threadId)", tc.inv)
			if got != tc.want {
				t.Errorf("upstream fields = %q; want %q", got, tc.want)
			}
		})
	}
}
