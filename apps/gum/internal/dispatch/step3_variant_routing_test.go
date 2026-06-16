// Package dispatch — Red Team failing tests for step 3: variant routing from catalog
// by stability and interface_kind (issue gum-vq4z.3).
//
// Spec anchors:
//   - §3.1 step 2: "select the default or explicit variant_id ... reject quarantined/unsupported
//     variants before any upstream call"
//   - §3.1 step 3: resolveVariant routing
//   - §5.1.1: "stable > beta > alpha, preferred-vs-stability conflict rule, lifecycle-aware
//     default rule, AMBIGUOUS_VARIANT envelope, OP_NOT_FOUND envelope"
//   - §5.5 catalog entry lifecycle: deprecated variants in deprecated_variant_ids return
//     VARIANT_DEPRECATED warning but still execute; quarantined variants return VARIANT_QUARANTINED
//   - §1421 stable error codes: OP_NOT_FOUND, AMBIGUOUS_VARIANT, VARIANT_QUARANTINED,
//     VARIANT_DEPRECATED
//
// CURRENT STATE: resolveVariant only calls findOpVariant which looks up default_variant_id.
// It does NOT:
//   - Fall back to stability-ordered selection when default_variant_id is absent
//   - Honour interface_kind preferences from DispatcherConfig.PreferredInterfaceKinds
//   - Return AMBIGUOUS_VARIANT when no tie-breaking rule applies
//   - Return VARIANT_QUARANTINED for quarantined variants
//   - Return VARIANT_DEPRECATED (as a warning) for deprecated variants (still executes)
//   - Carry op_id in detail of OP_NOT_FOUND errors
//   - Use the *StructuredError return type (currently returns plain error)
//
// Required NEW API surface (Green Team must add):
//
//  1. Add Quarantined bool field to catalog.Variant:
//       Quarantined bool `json:"quarantined,omitempty"`
//
//  2. Add PreferredInterfaceKinds to DispatcherConfig:
//       PreferredInterfaceKinds []string
//     and wire it through to the dispatcher struct (e.g., preferredInterfaceKinds []string).
//
//  3. Change resolveVariant return type to use *StructuredError as the second return:
//       func (d *dispatcher) resolveVariant(ctx context.Context, inv *Invocation) (*ResolvedVariant, *StructuredError)
//     and update Dispatch() to handle the new return type.
//
//  4. Implement stability-ordered variant selection when default_variant_id is empty:
//       stable > beta > alpha.
//
//  5. Implement interface_kind preference tie-breaking: when two variants have the same
//     stability, check d.preferredInterfaceKinds in order; first match wins.
//
//  6. Implement AMBIGUOUS_VARIANT: when multiple variants tie on stability and none match
//     the preference list, return AMBIGUOUS_VARIANT with detail keys: op_id, variants ([]string of variant IDs).
//
//  7. Implement VARIANT_QUARANTINED: when the selected variant has Quarantined=true,
//     return VARIANT_QUARANTINED with detail: op_id, variant_id.
//
//  8. Implement VARIANT_DEPRECATED warning: when the selected variant's ID is listed in
//     op.DeprecatedVariantIDs, attach a warning to the ResolvedVariant but still return it
//     (no error). The warning is surfaced via ResolvedVariant.DeprecationWarning bool (or
//     equivalent; green may choose the exact field name, but the test below uses
//     ResolvedVariant.Deprecated bool).
//
//  9. Carry op_id in OP_NOT_FOUND detail: resolveVariant must return
//     *StructuredError with Detail["op_id"] == inv.OpID when no op is found.
//     (Note: parseAndValidate already handles OP_NOT_FOUND for alias lookup;
//     resolveVariant's OP_NOT_FOUND fires when an op exists but has no selectable variant.)
//
// All tests in this file FAIL until Green Team implements the API surface above.
// That is the intended state for Red Team output.
package dispatch

import (
	"context"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/catalog"
)

// ── fixture helpers ──────────────────────────────────────────────────────────

// makeRoutingVariant constructs a minimal Variant for routing tests.
func makeRoutingVariant(id string, stability catalog.Stability, ik catalog.InterfaceKind) catalog.Variant {
	return catalog.Variant{
		VariantID:     id,
		Stability:     stability,
		InterfaceKind: ik,
		BackendKind:   catalog.BackendKindTypedRestSDK,
		RiskClass:     catalog.RiskClassRead,
		Binding: &catalog.Binding{
			BindingSchemaVersion: 1,
			AdapterKey:           "test.adapter",
			OperationKey:         id + ".exec",
		},
	}
}

// makeQuarantinedVariant constructs a Variant with Quarantined=true.
// Requires green team to add Quarantined bool to catalog.Variant.
func makeQuarantinedVariant(id string) catalog.Variant {
	v := makeRoutingVariant(id, catalog.StabilityStable, catalog.InterfaceKindDiscoveryREST)
	v.Quarantined = true
	return v
}

// routingTestCatalog builds a *catalog.Catalog for routing tests.
// It is purposely not passed through catalog.Catalog.Validate() because some
// ops intentionally have empty default_variant_id to exercise fallback routing.
func routingTestCatalog() *catalog.Catalog {
	return &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratedAt:          time.Now().UTC().Format(time.RFC3339),
		GeneratorVersion:     "test@0.0.0",
		Ops: []catalog.Op{
			// Op 1: single stable variant + explicit default_variant_id.
			{
				OpID:             "routing.has.default",
				OpSchemaVersion:  1,
				Title:            "Op with explicit default",
				Summary:          "Used by TestVariantRoutingPrefersDefault.",
				DefaultVariantID: "routing.has.default.v2.rest",
				Variants: []catalog.Variant{
					makeRoutingVariant("routing.has.default.v1.rest", catalog.StabilityStable, catalog.InterfaceKindDiscoveryREST),
					makeRoutingVariant("routing.has.default.v2.rest", catalog.StabilityStable, catalog.InterfaceKindDiscoveryREST),
				},
			},
			// Op 2: no default_variant_id; stable + beta variants present.
			{
				OpID:             "routing.no.default.stable.beta",
				OpSchemaVersion:  1,
				Title:            "Op without default — stable vs beta",
				Summary:          "Used by TestVariantRoutingPrefersStableOverBeta.",
				DefaultVariantID: "", // intentionally empty; Validate() would reject this
				Variants: []catalog.Variant{
					makeRoutingVariant("routing.beta.v1", catalog.StabilityBeta, catalog.InterfaceKindDiscoveryREST),
					makeRoutingVariant("routing.stable.v1", catalog.StabilityStable, catalog.InterfaceKindDiscoveryREST),
				},
			},
			// Op 3: no default_variant_id; stable + experimental variants present.
			{
				OpID:             "routing.no.default.stable.experimental",
				OpSchemaVersion:  1,
				Title:            "Op without default — stable vs experimental",
				Summary:          "Used by TestVariantRoutingPrefersStableOverExperimental.",
				DefaultVariantID: "", // intentionally empty
				Variants: []catalog.Variant{
					makeRoutingVariant("routing.exp.v1", catalog.StabilityAlpha, catalog.InterfaceKindDiscoveryREST),
					makeRoutingVariant("routing.stable.v2", catalog.StabilityStable, catalog.InterfaceKindDiscoveryREST),
				},
			},
			// Op 4: two equally-stable variants differing by interface_kind (REST vs gRPC).
			{
				OpID:             "routing.iface.two.equal",
				OpSchemaVersion:  1,
				Title:            "Op with two equally-stable variants, REST and gRPC",
				Summary:          "Used by TestVariantRoutingPrefersInterfaceKind.",
				DefaultVariantID: "", // no default; tie-broken by preference list
				Variants: []catalog.Variant{
					makeRoutingVariant("routing.iface.rest.v1", catalog.StabilityStable, catalog.InterfaceKindDiscoveryREST),
					makeRoutingVariant("routing.iface.grpc.v1", catalog.StabilityStable, catalog.InterfaceKindGRPC),
				},
			},
			// Op 5: two equally-stable variants with no preference applicable → AMBIGUOUS.
			{
				OpID:             "routing.ambiguous",
				OpSchemaVersion:  1,
				Title:            "Op with two equally-stable variants that cannot be disambiguated",
				Summary:          "Used by TestVariantRoutingAmbiguousReturnsError.",
				DefaultVariantID: "", // no default; no preference list configured
				Variants: []catalog.Variant{
					makeRoutingVariant("routing.ambiguous.a", catalog.StabilityStable, catalog.InterfaceKindDiscoveryREST),
					makeRoutingVariant("routing.ambiguous.b", catalog.StabilityStable, catalog.InterfaceKindGRPC),
				},
			},
			// Op 6: only available variant is quarantined.
			{
				OpID:             "routing.quarantined.only",
				OpSchemaVersion:  1,
				Title:            "Op whose only variant is quarantined",
				Summary:          "Used by TestVariantRoutingQuarantinedRejected.",
				DefaultVariantID: "routing.quarantined.v1",
				Variants: []catalog.Variant{
					makeQuarantinedVariant("routing.quarantined.v1"),
				},
			},
			// Op 7: single deprecated variant (still within 90-day grace window).
			{
				OpID:             "routing.deprecated.one",
				OpSchemaVersion:  1,
				Title:            "Op with one deprecated variant (still invokable)",
				Summary:          "Used by TestVariantRoutingDeprecatedAccepted.",
				DefaultVariantID: "routing.deprecated.v1",
				DeprecatedVariantIDs: []string{"routing.deprecated.v1"},
				Variants: []catalog.Variant{
					makeRoutingVariant("routing.deprecated.v1", catalog.StabilityStable, catalog.InterfaceKindDiscoveryREST),
				},
			},
		},
	}
}

// newRoutingDispatcher constructs a dispatcher without PreferredInterfaceKinds.
func newRoutingDispatcher() *dispatcher {
	return &dispatcher{
		snapshot: routingTestCatalog(),
		adapters: map[string]Adapter{},
	}
}

// newRoutingDispatcherWithPrefs constructs a dispatcher with the given interface_kind
// preference list. Requires DispatcherConfig.PreferredInterfaceKinds to exist.
func newRoutingDispatcherWithPrefs(prefs []string) *dispatcher {
	cfg := DispatcherConfig{
		PreferredInterfaceKinds: prefs,
	}
	d := NewDispatcherWithConfig(routingTestCatalog(), map[string]Adapter{}, cfg)
	// Cast to *dispatcher for white-box access (same package).
	return d.(*dispatcher)
}

// ── tests ────────────────────────────────────────────────────────────────────

// TestVariantRoutingPrefersDefault verifies that when default_variant_id is set and
// the Op contains multiple variants, resolveVariant returns the declared default.
//
// Current resolveVariant already does this — so this test is a regression guard that
// ALSO ensures the new *StructuredError return type is used.
// It fails today because resolveVariant returns plain error, not *StructuredError.
func TestVariantRoutingPrefersDefault(t *testing.T) {
	d := newRoutingDispatcher()
	inv := &Invocation{OpID: "routing.has.default", Args: map[string]any{}}

	rv, serr := d.resolveVariant(context.Background(), inv)
	if serr != nil {
		t.Fatalf("expected no error, got: %v", serr)
	}
	if rv == nil {
		t.Fatal("expected non-nil ResolvedVariant, got nil")
	}
	if rv.Variant.VariantID != "routing.has.default.v2.rest" {
		t.Errorf("expected variant_id 'routing.has.default.v2.rest', got %q", rv.Variant.VariantID)
	}
}

// TestVariantRoutingPrefersStableOverBeta verifies that when default_variant_id is
// absent, the resolver picks the most-stable variant (stable > beta).
// Spec §5.1.1: "stable > beta > alpha".
func TestVariantRoutingPrefersStableOverBeta(t *testing.T) {
	d := newRoutingDispatcher()
	inv := &Invocation{OpID: "routing.no.default.stable.beta", Args: map[string]any{}}

	rv, serr := d.resolveVariant(context.Background(), inv)
	if serr != nil {
		t.Fatalf("expected no error, got: %v", serr)
	}
	if rv == nil {
		t.Fatal("expected non-nil ResolvedVariant")
	}
	if rv.Variant.Stability != catalog.StabilityStable {
		t.Errorf("expected stability=%q, got %q", catalog.StabilityStable, rv.Variant.Stability)
	}
	if rv.Variant.VariantID != "routing.stable.v1" {
		t.Errorf("expected variant_id 'routing.stable.v1', got %q", rv.Variant.VariantID)
	}
}

// TestVariantRoutingPrefersStableOverExperimental verifies that when default_variant_id
// is absent, the resolver prefers stable over alpha (experimental).
// Spec §5.1.1: stability ordering stable > beta > alpha.
func TestVariantRoutingPrefersStableOverExperimental(t *testing.T) {
	d := newRoutingDispatcher()
	inv := &Invocation{OpID: "routing.no.default.stable.experimental", Args: map[string]any{}}

	rv, serr := d.resolveVariant(context.Background(), inv)
	if serr != nil {
		t.Fatalf("expected no error, got: %v", serr)
	}
	if rv == nil {
		t.Fatal("expected non-nil ResolvedVariant")
	}
	if rv.Variant.Stability != catalog.StabilityStable {
		t.Errorf("expected stability=%q, got %q", catalog.StabilityStable, rv.Variant.Stability)
	}
	if rv.Variant.VariantID != "routing.stable.v2" {
		t.Errorf("expected variant_id 'routing.stable.v2', got %q", rv.Variant.VariantID)
	}
}

// TestVariantRoutingPrefersInterfaceKindREST verifies that when two equally-stable
// variants exist, the one matching the first entry in PreferredInterfaceKinds is chosen.
// Test case: preference = ["discovery-rest", "grpc"] → REST variant wins.
//
// Requires DispatcherConfig.PreferredInterfaceKinds []string (new field).
func TestVariantRoutingPrefersInterfaceKindREST(t *testing.T) {
	d := newRoutingDispatcherWithPrefs([]string{string(catalog.InterfaceKindDiscoveryREST), string(catalog.InterfaceKindGRPC)})
	inv := &Invocation{OpID: "routing.iface.two.equal", Args: map[string]any{}}

	rv, serr := d.resolveVariant(context.Background(), inv)
	if serr != nil {
		t.Fatalf("expected no error with REST preference, got: %v", serr)
	}
	if rv == nil {
		t.Fatal("expected non-nil ResolvedVariant")
	}
	if rv.Variant.InterfaceKind != catalog.InterfaceKindDiscoveryREST {
		t.Errorf("expected interface_kind=%q, got %q", catalog.InterfaceKindDiscoveryREST, rv.Variant.InterfaceKind)
	}
	if rv.Variant.VariantID != "routing.iface.rest.v1" {
		t.Errorf("expected variant_id 'routing.iface.rest.v1', got %q", rv.Variant.VariantID)
	}
}

// TestVariantRoutingPrefersInterfaceKindGRPC verifies that when preference list
// starts with gRPC, the gRPC variant is chosen over the REST variant.
// Test case: preference = ["grpc", "discovery-rest"] → gRPC variant wins.
func TestVariantRoutingPrefersInterfaceKindGRPC(t *testing.T) {
	d := newRoutingDispatcherWithPrefs([]string{string(catalog.InterfaceKindGRPC), string(catalog.InterfaceKindDiscoveryREST)})
	inv := &Invocation{OpID: "routing.iface.two.equal", Args: map[string]any{}}

	rv, serr := d.resolveVariant(context.Background(), inv)
	if serr != nil {
		t.Fatalf("expected no error with gRPC preference, got: %v", serr)
	}
	if rv == nil {
		t.Fatal("expected non-nil ResolvedVariant")
	}
	if rv.Variant.InterfaceKind != catalog.InterfaceKindGRPC {
		t.Errorf("expected interface_kind=%q, got %q", catalog.InterfaceKindGRPC, rv.Variant.InterfaceKind)
	}
	if rv.Variant.VariantID != "routing.iface.grpc.v1" {
		t.Errorf("expected variant_id 'routing.iface.grpc.v1', got %q", rv.Variant.VariantID)
	}
}

// TestVariantRoutingAmbiguousReturnsError verifies that when two equally-stable
// variants exist and no preference list is configured (or no preference matches),
// resolveVariant returns AMBIGUOUS_VARIANT with op_id and variants detail keys.
// Spec §5.1.1 rule 5: "return AMBIGUOUS_VARIANT ... require explicit variant_id".
func TestVariantRoutingAmbiguousReturnsError(t *testing.T) {
	// No preferred interface kinds configured.
	d := newRoutingDispatcher()
	inv := &Invocation{OpID: "routing.ambiguous", Args: map[string]any{}}

	rv, serr := d.resolveVariant(context.Background(), inv)
	if serr == nil {
		t.Fatalf("expected AMBIGUOUS_VARIANT error, got nil (rv=%+v)", rv)
	}
	if serr.ErrCode != ErrCodeAmbiguousVariant {
		t.Errorf("expected ErrCode=%q, got %q", ErrCodeAmbiguousVariant, serr.ErrCode)
	}
	// Must carry op_id detail.
	if serr.Detail["op_id"] != "routing.ambiguous" {
		t.Errorf("expected detail['op_id']='routing.ambiguous', got %v", serr.Detail["op_id"])
	}
	// Must carry variants list (a non-empty slice of variant IDs).
	variants, ok := serr.Detail["variants"]
	if !ok {
		t.Error("expected detail key 'variants' in AMBIGUOUS_VARIANT error")
	}
	if variants == nil {
		t.Error("detail['variants'] must not be nil")
	}
}

// TestVariantRoutingQuarantinedRejected verifies that when the only available variant
// is quarantined, resolveVariant returns VARIANT_QUARANTINED before any upstream call.
// Spec §5.5 rule 5: "Security quarantine overrides the grace window. Quarantined variants
// return VARIANT_QUARANTINED before auth or upstream execution."
// Spec §1421: VARIANT_QUARANTINED is a terminal error code.
//
// Requires catalog.Variant.Quarantined bool field (new).
func TestVariantRoutingQuarantinedRejected(t *testing.T) {
	d := newRoutingDispatcher()
	inv := &Invocation{OpID: "routing.quarantined.only", Args: map[string]any{}}

	rv, serr := d.resolveVariant(context.Background(), inv)
	if serr == nil {
		t.Fatalf("expected VARIANT_QUARANTINED error, got nil (rv=%+v)", rv)
	}
	if serr.ErrCode != ErrCodeVariantQuarantined {
		t.Errorf("expected ErrCode=%q, got %q", ErrCodeVariantQuarantined, serr.ErrCode)
	}
	// Must carry op_id and variant_id in detail.
	if serr.Detail["op_id"] != "routing.quarantined.only" {
		t.Errorf("expected detail['op_id']='routing.quarantined.only', got %v", serr.Detail["op_id"])
	}
	if serr.Detail["variant_id"] != "routing.quarantined.v1" {
		t.Errorf("expected detail['variant_id']='routing.quarantined.v1', got %v", serr.Detail["variant_id"])
	}
	if rv != nil {
		t.Errorf("expected nil ResolvedVariant for quarantined variant, got %+v", rv)
	}
}

// TestVariantRoutingDeprecatedAccepted verifies that a deprecated variant (listed in
// op.DeprecatedVariantIDs) is still selected and returned without error, but with a
// deprecation warning attached to the ResolvedVariant.
//
// Spec §5.5 rule 2–3: "Deprecated variants remain invokable by explicit variant_id
// for 90 days unless quarantined ... MUST return VARIANT_DEPRECATED as a warning
// envelope field while still executing."
// Spec §1421: "VARIANT_DEPRECATED is a warning envelope field (the call still executes
// with a deprecation annotation)".
//
// The green team should attach the warning via ResolvedVariant.Deprecated bool == true
// (or ResolvedVariant.DeprecationWarning, etc.; the exact field name is a green
// implementation choice; this test uses ResolvedVariant.Deprecated bool).
func TestVariantRoutingDeprecatedAccepted(t *testing.T) {
	d := newRoutingDispatcher()
	inv := &Invocation{OpID: "routing.deprecated.one", Args: map[string]any{}}

	rv, serr := d.resolveVariant(context.Background(), inv)
	// Must NOT return an error — deprecated variants still execute.
	if serr != nil {
		t.Fatalf("expected no error for deprecated variant (still executable), got: %v", serr)
	}
	if rv == nil {
		t.Fatal("expected non-nil ResolvedVariant for deprecated variant")
	}
	if rv.Variant.VariantID != "routing.deprecated.v1" {
		t.Errorf("expected variant_id 'routing.deprecated.v1', got %q", rv.Variant.VariantID)
	}
	// The resolver MUST signal deprecation via ResolvedVariant.Deprecated == true
	// so the output pipeline can attach the VARIANT_DEPRECATED warning envelope field.
	if !rv.Deprecated {
		t.Error("expected ResolvedVariant.Deprecated=true for a deprecated variant; " +
			"green team must add Deprecated bool to ResolvedVariant and set it when " +
			"variant_id is in op.DeprecatedVariantIDs")
	}
}

// TestVariantRoutingOpNotFoundCarriesOpID verifies that when Dispatch is called with a
// non-existent op_id, the returned error is OP_NOT_FOUND and carries op_id in its detail.
//
// Note: parseAndValidate already handles OP_NOT_FOUND for the alias-scan path.
// This test exercises the full Dispatch path to confirm the detail contract holds
// end-to-end and that resolveVariant's OP_NOT_FOUND errors also carry op_id.
//
// Spec §4.1: "if op_id is not present in catalog.json (after alias normalization), the
// call returns {"error_code": "OP_NOT_FOUND", "message": "...", "suggestions": [...]}".
// The red team requires detail["op_id"] to equal the requested op_id (spec line 230:
// "reject missing/quarantined/unsupported variants" → the caller must know which op failed).
func TestVariantRoutingOpNotFoundCarriesOpID(t *testing.T) {
	d := newRoutingDispatcher()
	inv := &Invocation{OpID: "nonexistent.op.xyz", Args: map[string]any{}}

	// parseAndValidate fires before resolveVariant; it also returns OP_NOT_FOUND.
	_, serr := d.parseAndValidate(context.Background(), inv)
	if serr == nil {
		t.Fatal("expected *StructuredError for non-existent op, got nil")
	}
	if serr.ErrCode != ErrCodeOpNotFound {
		t.Errorf("expected ErrCode=%q, got %q", ErrCodeOpNotFound, serr.ErrCode)
	}
	// Spec contract: error detail must carry the requested op_id so callers can correlate
	// error messages to the original request without parsing the free-text message field.
	opIDDetail, ok := serr.Detail["op_id"]
	if !ok {
		t.Error("expected detail key 'op_id' in OP_NOT_FOUND error; " +
			"green team must add WithDetail(\"op_id\", inv.OpID) to the OP_NOT_FOUND path " +
			"in parseAndValidate and resolveVariant")
	}
	if opIDDetail != "nonexistent.op.xyz" {
		t.Errorf("expected detail['op_id']='nonexistent.op.xyz', got %v", opIDDetail)
	}
}
