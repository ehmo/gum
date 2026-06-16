package dispatch_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
	"github.com/ehmo/gum/internal/output/profile"
)

// fakeJSONAdapter is a minimal Adapter that returns a fixed JSON body, used to
// exercise step-8 expression-profile application without a live backend.
type fakeJSONAdapter struct{ body string }

func (f fakeJSONAdapter) Execute(_ context.Context, _ *dispatch.Invocation, _ *dispatch.ResolvedVariant, _ *dispatch.Credentials) (*dispatch.Response, error) {
	return &dispatch.Response{Body: []byte(f.body), Format: "json", StatusCode: 200}, nil
}

func profileTestCatalog() *catalog.Catalog {
	return &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratedAt:          "2026-06-05T00:00:00Z",
		GeneratorVersion:     "test",
		Ops: []catalog.Op{{
			OpID:             "fake.op",
			OpSchemaVersion:  1,
			Title:            "Fake",
			Summary:          "fake op for profile-apply test",
			DefaultVariantID: "fake.v1",
			Variants: []catalog.Variant{{
				VariantID:            "fake.v1",
				VariantSchemaVersion: 1,
				Stability:            catalog.StabilityStable,
				InterfaceKind:        catalog.InterfaceKindDiscoveryREST,
				BackendKind:          catalog.BackendKindRawHTTP,
				RiskClass:            catalog.RiskClassRead,
				AuthStrategy:         catalog.AuthStrategyNone,
				OutputProfile:        "test.compact",
				Binding: &catalog.Binding{
					BindingSchemaVersion: 1,
					AdapterKey:           "fake",
					OperationKey:         "fake.op",
					HTTP:                 &catalog.HTTPBinding{Method: "GET", Path: "https://x.googleapis.com/y"},
				},
			}},
		}},
	}
}

const verboseBody = `{"results":[{"text":"shoes","drop":"gone"}]}`

// TestDispatchAppliesCatalogEmbeddedProfile proves the kernel resolves a
// variant's output_profile via the injected ProfileLookup and applies it at
// step 8 (the keep_fields projection drops "drop").
func TestDispatchAppliesCatalogEmbeddedProfile(t *testing.T) {
	cat := profileTestCatalog()
	if err := cat.Validate(); err != nil {
		t.Fatalf("catalog invalid: %v", err)
	}
	lookup := func(name string) (*profile.Profile, bool) {
		if name == "test.compact" {
			return &profile.Profile{Name: "test.compact", DefaultFormat: "json", KeepFields: []string{"results.text"}}, true
		}
		return nil, false
	}
	disp := dispatch.NewDispatcherWithConfig(cat,
		map[string]dispatch.Adapter{"fake": fakeJSONAdapter{verboseBody}},
		dispatch.DispatcherConfig{ProfileLookup: lookup},
	)
	res, err := disp.Dispatch(context.Background(), &dispatch.Invocation{OpID: "fake.op", Format: "json"})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	body := string(res.Body)
	if !strings.Contains(body, "shoes") {
		t.Errorf("kept field missing from shaped body: %s", body)
	}
	if strings.Contains(body, "gone") {
		t.Errorf("keep_fields profile should have dropped \"drop\"; body=%s", body)
	}
}

// TestDispatchNoProfileLookupIsUnshaped is the negative control: with no
// ProfileLookup wired, the same op falls back to default shaping and the body
// is unprojected (still carries "gone"). Guards backward compatibility for the
// ~all ops whose output_profile name has no embedded definition.
func TestDispatchNoProfileLookupIsUnshaped(t *testing.T) {
	cat := profileTestCatalog()
	disp := dispatch.NewDispatcherWithConfig(cat,
		map[string]dispatch.Adapter{"fake": fakeJSONAdapter{verboseBody}},
		dispatch.DispatcherConfig{}, // no ProfileLookup
	)
	res, err := disp.Dispatch(context.Background(), &dispatch.Invocation{OpID: "fake.op", Format: "json"})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !strings.Contains(string(res.Body), "gone") {
		t.Errorf("without a ProfileLookup the body must be unshaped; body=%s", res.Body)
	}
}
