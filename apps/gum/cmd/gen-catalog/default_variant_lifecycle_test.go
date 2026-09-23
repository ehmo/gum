package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/embedded"
)

// docs/spec.md §5.1, lifecycle-aware default rule (normative):
// "default_variant_id MUST select an active, non-quarantined, non-removed
// variant... cmd/gen-catalog fails with DEFAULT_VARIANT_INVALID when the
// selected default violates this lifecycle rule." The §7 build-time error
// table lists the code with phase "Build".
//
// The removed half is already enforced: Op.Validate rejects a default that
// names no variant in variants[], which is what removal looks like in the
// snapshot. The quarantined and deprecated halves had no producer at all.

// lifecycleOp builds one op whose default is defaultID.
func lifecycleOp(defaultID string, deprecated []string, variants ...catalog.Variant) catalog.Op {
	return catalog.Op{
		OpID:                 "demo.op",
		OpSchemaVersion:      1,
		Title:                "Demo",
		Summary:              "Demo op for the lifecycle gate.",
		DefaultVariantID:     defaultID,
		DeprecatedVariantIDs: deprecated,
		Variants:             variants,
	}
}

// lifecycleVariant builds a minimal executable variant.
func lifecycleVariant(id string) catalog.Variant {
	return catalog.Variant{
		VariantID:            id,
		VariantSchemaVersion: 1,
		Stability:            catalog.StabilityStable,
		InterfaceKind:        catalog.InterfaceKindDiscoveryREST,
		BackendKind:          catalog.BackendKindTypedRestSDK,
		RiskClass:            catalog.RiskClassRead,
		Binding: &catalog.Binding{
			BindingSchemaVersion: 1,
			AdapterKey:           "rest",
			OperationKey:         "demo.op",
		},
	}
}

func TestDefaultVariantLifecycleRejectsQuarantinedDefault(t *testing.T) {
	dead := lifecycleVariant("demo.v1.dead")
	dead.Quarantined = true

	cat := &catalog.Catalog{Ops: []catalog.Op{
		lifecycleOp("demo.v1.dead", nil, dead, lifecycleVariant("demo.v1.live")),
	}}

	err := validateDefaultVariantLifecycle(cat)
	if !errors.Is(err, errDefaultVariantInvalid) {
		t.Fatalf("validateDefaultVariantLifecycle = %v; want errDefaultVariantInvalid", err)
	}
	if !strings.Contains(err.Error(), "demo.v1.dead") {
		t.Errorf("error %q does not name the offending variant", err)
	}
	if !strings.Contains(err.Error(), "quarantined") {
		t.Errorf("error %q does not say which half of the rule broke", err)
	}
}

// "Deprecated variants are excluded from default selection when any
// non-deprecated executable variant exists for the op."
func TestDefaultVariantLifecycleRejectsDeprecatedDefaultWithLiveSibling(t *testing.T) {
	cat := &catalog.Catalog{Ops: []catalog.Op{
		lifecycleOp("demo.v1.old", []string{"demo.v1.old"},
			lifecycleVariant("demo.v1.old"), lifecycleVariant("demo.v2.new")),
	}}

	err := validateDefaultVariantLifecycle(cat)
	if !errors.Is(err, errDefaultVariantInvalid) {
		t.Fatalf("validateDefaultVariantLifecycle = %v; want errDefaultVariantInvalid", err)
	}
	if !strings.Contains(err.Error(), "demo.v2.new") {
		t.Errorf("error %q does not name the live alternative the generator should have picked", err)
	}
}

// "If every executable variant for an op is deprecated but still inside its
// 90-day grace window, the default may point at the least-risk deprecated
// variant." The gate must not fire when there is nothing else to pick.
func TestDefaultVariantLifecycleAllowsDeprecatedDefaultWhenAllAreDeprecated(t *testing.T) {
	cat := &catalog.Catalog{Ops: []catalog.Op{
		lifecycleOp("demo.v1.old", []string{"demo.v1.old", "demo.v2.old"},
			lifecycleVariant("demo.v1.old"), lifecycleVariant("demo.v2.old")),
	}}

	if err := validateDefaultVariantLifecycle(cat); err != nil {
		t.Fatalf("validateDefaultVariantLifecycle = %v; the grace-window clause allows this", err)
	}
}

// A non-deprecated sibling that cannot execute is not an alternative. §5.8
// says typed_executor_required and schema_only execute no declared atom
// through generic dispatch, so promoting one would trade a deprecation warning
// for UNSUPPORTED_CAPABILITY on every call.
func TestDefaultVariantLifecycleIgnoresNonExecutableSiblings(t *testing.T) {
	for _, support := range []catalog.ExecutionSupport{
		catalog.ExecutionSupportTypedExecutorRequired,
		catalog.ExecutionSupportSchemaOnly,
	} {
		t.Run(string(support), func(t *testing.T) {
			sibling := lifecycleVariant("demo.v2.meta")
			sibling.ExecutionSupport = support

			cat := &catalog.Catalog{Ops: []catalog.Op{
				lifecycleOp("demo.v1.old", []string{"demo.v1.old"},
					lifecycleVariant("demo.v1.old"), sibling),
			}}

			if err := validateDefaultVariantLifecycle(cat); err != nil {
				t.Fatalf("validateDefaultVariantLifecycle = %v; %s executes nothing, so it is no alternative", err, support)
			}
		})
	}
}

// A quarantined sibling is likewise no alternative, but a quarantined default
// is still invalid: the rule is absolute, with no grace clause.
func TestDefaultVariantLifecycleRejectsQuarantinedDefaultWithNoAlternative(t *testing.T) {
	only := lifecycleVariant("demo.v1.dead")
	only.Quarantined = true

	cat := &catalog.Catalog{Ops: []catalog.Op{lifecycleOp("demo.v1.dead", nil, only)}}

	if err := validateDefaultVariantLifecycle(cat); !errors.Is(err, errDefaultVariantInvalid) {
		t.Fatalf("validateDefaultVariantLifecycle = %v; a quarantined default is never allowed", err)
	}
}

func TestDefaultVariantLifecycleAcceptsAHealthyDefault(t *testing.T) {
	cat := &catalog.Catalog{Ops: []catalog.Op{
		lifecycleOp("demo.v1.live", []string{"demo.v1.old"},
			lifecycleVariant("demo.v1.live"), lifecycleVariant("demo.v1.old")),
	}}

	if err := validateDefaultVariantLifecycle(cat); err != nil {
		t.Fatalf("validateDefaultVariantLifecycle = %v; want nil", err)
	}
}

// One run must show the whole list, matching validateVariantProfiles.
func TestDefaultVariantLifecycleReportsEveryViolation(t *testing.T) {
	first := lifecycleVariant("a.v1")
	first.Quarantined = true
	second := lifecycleVariant("b.v1")
	second.Quarantined = true

	opA := lifecycleOp("a.v1", nil, first)
	opA.OpID = "demo.a"
	opB := lifecycleOp("b.v1", nil, second)
	opB.OpID = "demo.b"

	err := validateDefaultVariantLifecycle(&catalog.Catalog{Ops: []catalog.Op{opA, opB}})
	if err == nil {
		t.Fatal("validateDefaultVariantLifecycle = nil; both ops are invalid")
	}
	for _, want := range []string{"demo.a", "demo.b"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q omits op %s; the gate must report every violation in one run", err, want)
		}
	}
}

// The gate is worthless if a generator path can write a snapshot without it.
// validateGeneratedCatalog is the single choke point every path calls.
func TestValidateGeneratedCatalogRunsTheLifecycleGate(t *testing.T) {
	dead := lifecycleVariant("demo.v1.dead")
	dead.Quarantined = true

	cat := &catalog.Catalog{
		CatalogSchemaVersion: catalog.SupportedCatalogSchemaVersions[0],
		GeneratedAt:          time.Now().UTC().Format(time.RFC3339),
		GeneratorVersion:     "test",
		Ops:                  []catalog.Op{lifecycleOp("demo.v1.dead", nil, dead, lifecycleVariant("demo.v1.live"))},
	}

	if err := validateGeneratedCatalog(cat); !errors.Is(err, errDefaultVariantInvalid) {
		t.Fatalf("validateGeneratedCatalog = %v; want errDefaultVariantInvalid", err)
	}
}

func TestEmbeddedCatalogPassesTheLifecycleGate(t *testing.T) {
	var cat catalog.Catalog
	if err := json.Unmarshal(embedded.CatalogJSON, &cat); err != nil {
		t.Fatalf("decode embedded catalog: %v", err)
	}
	if len(cat.Ops) == 0 {
		t.Fatal("embedded catalog has no ops; the gate would pass vacuously")
	}

	if err := validateDefaultVariantLifecycle(&cat); err != nil {
		t.Fatalf("shipped catalog fails the lifecycle gate: %v", err)
	}
}

// TestDefaultVariantLifecycleSelection is the proof artifact docs/test-matrix.md
// row 50 names: "Default variant never selects removed/quarantined/deprecated
// variants when an active executable alternative exists."
//
// It replaces the internal/testmatrix shim of the same name, which asserted
// only that some symbol starting "TestDefaultVariant" existed somewhere in the
// repo. Each of the row's three lifecycle states is driven here against a
// catalog that does carry an active executable alternative.
func TestDefaultVariantLifecycleSelection(t *testing.T) {
	live := lifecycleVariant("demo.v2.live")

	cases := []struct {
		state    string
		build    func() catalog.Op
		rejected bool
		// owner names the gate that produces the failure, so a future reader
		// knows where to look when the case regresses.
		owner string
	}{
		{
			state: "removed",
			build: func() catalog.Op {
				// A removed variant is simply absent from variants[]. The
				// default still names it, which is the dangling reference
				// Op.Validate rejects.
				return lifecycleOp("demo.v1.gone", nil, live)
			},
			rejected: true,
			owner:    "Op.Validate/ErrDanglingDefaultVariantID",
		},
		{
			state: "quarantined",
			build: func() catalog.Op {
				dead := lifecycleVariant("demo.v1.dead")
				dead.Quarantined = true
				return lifecycleOp("demo.v1.dead", nil, dead, live)
			},
			rejected: true,
			owner:    "validateDefaultVariantLifecycle",
		},
		{
			state: "deprecated",
			build: func() catalog.Op {
				return lifecycleOp("demo.v1.old", []string{"demo.v1.old"},
					lifecycleVariant("demo.v1.old"), live)
			},
			rejected: true,
			owner:    "validateDefaultVariantLifecycle",
		},
		{
			state: "active",
			build: func() catalog.Op {
				return lifecycleOp("demo.v2.live", nil, live, lifecycleVariant("demo.v1.old"))
			},
			rejected: false,
			owner:    "validateDefaultVariantLifecycle",
		},
	}

	for _, tc := range cases {
		t.Run(tc.state, func(t *testing.T) {
			cat := &catalog.Catalog{
				CatalogSchemaVersion: catalog.SupportedCatalogSchemaVersions[0],
				GeneratedAt:          time.Now().UTC().Format(time.RFC3339),
				GeneratorVersion:     "test",
				Ops:                  []catalog.Op{tc.build()},
			}

			err := validateGeneratedCatalog(cat)
			if tc.rejected && err == nil {
				t.Fatalf("validateGeneratedCatalog = nil; a %s default must be rejected by %s", tc.state, tc.owner)
			}
			if !tc.rejected && err != nil {
				t.Fatalf("validateGeneratedCatalog = %v; an active executable default is valid", err)
			}
		})
	}
}
