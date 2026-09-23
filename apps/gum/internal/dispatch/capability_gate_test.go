package dispatch

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/catalog"
)

// capabilityCatalog returns a one-op catalog whose single variant carries the
// given execution_support and capability atoms.
func capabilityCatalog(support catalog.ExecutionSupport, caps, unsupported []string) *catalog.Catalog {
	return &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratedAt:          time.Now().UTC().Format(time.RFC3339),
		GeneratorVersion:     "test",
		Ops: []catalog.Op{
			{
				OpID:             "test.capability.op",
				OpSchemaVersion:  1,
				Title:            "Capability gate op",
				Summary:          "Exercises the spec §5.8 capability gate.",
				DefaultVariantID: "v1",
				Variants: []catalog.Variant{
					{
						VariantID:               "v1",
						Stability:               catalog.StabilityStable,
						InterfaceKind:           catalog.InterfaceKindDiscoveryREST,
						BackendKind:             catalog.BackendKindTypedRestSDK,
						RiskClass:               catalog.RiskClassRead,
						ExecutionSupport:        support,
						Capabilities:            caps,
						UnsupportedCapabilities: unsupported,
						Binding: &catalog.Binding{
							BindingSchemaVersion: 1,
							AdapterKey:           "ok",
							OperationKey:         "test.capability.op",
						},
					},
				},
			},
		},
	}
}

// TestCapabilityGateRefusesBeforeTheUpstreamRequest pins spec §5.8. A
// typed_executor_required or schema_only variant must answer
// UNSUPPORTED_CAPABILITY without reaching the adapter. Nothing enforced this
// before: such a variant resolved auth and issued a request that could not
// work.
func TestCapabilityGateRefusesBeforeTheUpstreamRequest(t *testing.T) {
	cases := []struct {
		name        string
		support     catalog.ExecutionSupport
		caps        []string
		unsupported []string
		wantBlocked []string
	}{
		{
			name:        "typed_executor_required",
			support:     catalog.ExecutionSupportTypedExecutorRequired,
			caps:        []string{"media_upload_resumable"},
			unsupported: []string{"media_upload_resumable"},
			wantBlocked: []string{"media_upload_resumable"},
		},
		{
			name:        "schema_only",
			support:     catalog.ExecutionSupportSchemaOnly,
			caps:        []string{"streaming", "websocket"},
			unsupported: []string{"streaming", "websocket"},
			wantBlocked: []string{"streaming", "websocket"},
		},
		{
			name:        "schema_only with no declared atoms still refuses",
			support:     catalog.ExecutionSupportSchemaOnly,
			wantBlocked: []string{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			adapter := &funcAdapter{execute: func(context.Context, *Invocation, *ResolvedVariant, *Credentials) (*Response, error) {
				calls++
				return &Response{Body: []byte(`{}`), Format: "json"}, nil
			}}
			d := NewDispatcher(capabilityCatalog(tc.support, tc.caps, tc.unsupported), map[string]Adapter{"ok": adapter})

			_, err := d.Dispatch(context.Background(), &Invocation{OpID: "test.capability.op", Format: "json"})
			if err == nil {
				t.Fatal("Dispatch() = nil error; want UNSUPPORTED_CAPABILITY")
			}
			var se *StructuredError
			if !errors.As(err, &se) {
				t.Fatalf("error %v is not structured", err)
			}
			if se.ErrCode != ErrCodeUnsupportedCapability {
				t.Fatalf("error_code = %s; want %s", se.ErrCode, ErrCodeUnsupportedCapability)
			}
			if calls != 0 {
				t.Errorf("adapter ran %d time(s); §5.8 refuses before any upstream request", calls)
			}
			if got := se.Detail["suggestion"]; got != unsupportedCapabilitySuggestion {
				t.Errorf("suggestion = %v; want %q", got, unsupportedCapabilitySuggestion)
			}
			blocked, ok := se.Detail["unsupported_capabilities"].([]string)
			if !ok {
				t.Fatalf("detail[unsupported_capabilities] = %T; spec §5.8 makes it the branch discriminator and it must be present", se.Detail["unsupported_capabilities"])
			}
			if len(blocked) != len(tc.wantBlocked) {
				t.Fatalf("unsupported_capabilities = %v; want %v", blocked, tc.wantBlocked)
			}
			for i := range blocked {
				if blocked[i] != tc.wantBlocked[i] {
					t.Fatalf("unsupported_capabilities = %v; want %v", blocked, tc.wantBlocked)
				}
			}
			if _, present := se.Detail["status"]; present {
				t.Error("detail carries both discriminators; spec §5.8 makes them mutually exclusive")
			}
		})
	}
}

// TestCapabilityGateLetsFullAndPartialThrough pins the other half of §5.8.
// "full" and "partial" variants stay invokable; refusing them would turn a
// documented warning into an outright failure.
func TestCapabilityGateLetsFullAndPartialThrough(t *testing.T) {
	cases := []struct {
		name        string
		support     catalog.ExecutionSupport
		caps        []string
		unsupported []string
	}{
		{name: "absent resolves to full"},
		{name: "full", support: catalog.ExecutionSupportFull, caps: []string{"json_response"}},
		{
			name:        "partial",
			support:     catalog.ExecutionSupportPartial,
			caps:        []string{"json_response", "media_upload_simple"},
			unsupported: []string{"media_upload_simple"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := NewDispatcher(capabilityCatalog(tc.support, tc.caps, tc.unsupported),
				map[string]Adapter{"ok": jsonAdapter(`{"a":1}`)})
			if _, err := d.Dispatch(context.Background(), &Invocation{OpID: "test.capability.op", Format: "json"}); err != nil {
				t.Fatalf("Dispatch() = %v; want success", err)
			}
		})
	}
}

// TestPartialVariantWarnsInTheExpressionEnvelope pins spec §5.8. A "partial"
// variant executes, so the caller gets data and must also be told which atoms
// did not run. The field rides in _expression because the three §13 result
// shapes are closed.
func TestPartialVariantWarnsInTheExpressionEnvelope(t *testing.T) {
	cat := capabilityCatalog(catalog.ExecutionSupportPartial,
		[]string{"json_response", "media_download", "media_upload_simple"},
		[]string{"media_download", "media_upload_simple"})
	d := NewDispatcher(cat, map[string]Adapter{"ok": jsonAdapter(`{"a":1}`)})

	shaped, err := d.Dispatch(context.Background(), &Invocation{OpID: "test.capability.op", Format: "json"})
	if err != nil {
		t.Fatalf("Dispatch() = %v; want success", err)
	}
	if shaped.Expression == nil {
		t.Fatal("no _expression envelope on a partial variant")
	}
	got := shaped.Expression.UnsupportedCapabilities
	want := []string{"media_download", "media_upload_simple"}
	if len(got) != len(want) {
		t.Fatalf("_unsupported_capabilities = %v; want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("_unsupported_capabilities = %v; want %v", got, want)
		}
	}
	if fields := shaped.Expression.Fields(); fields["_unsupported_capabilities"] == nil {
		t.Error("Fields() drops _unsupported_capabilities; the wire envelope would omit the §5.8 warning")
	}
}

// TestFullVariantCarriesNoCapabilityWarning keeps the §5.8 field off every
// other execution_support. A warning on a fully executable variant names atoms
// that did run, which is worse than no warning.
func TestFullVariantCarriesNoCapabilityWarning(t *testing.T) {
	cat := capabilityCatalog(catalog.ExecutionSupportFull, []string{"json_response"}, nil)
	d := NewDispatcher(cat, map[string]Adapter{"ok": jsonAdapter(`{"a":1}`)})

	shaped, err := d.Dispatch(context.Background(), &Invocation{OpID: "test.capability.op", Format: "json"})
	if err != nil {
		t.Fatalf("Dispatch() = %v; want success", err)
	}
	if got := shaped.Expression.UnsupportedCapabilities; len(got) != 0 {
		t.Errorf("_unsupported_capabilities = %v; want none on a full variant", got)
	}
	if _, present := shaped.Expression.Fields()["_unsupported_capabilities"]; present {
		t.Error("Fields() emits _unsupported_capabilities on a full variant")
	}
}
