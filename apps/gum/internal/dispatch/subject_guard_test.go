package dispatch_test

// Dispatch-path wrong-account guard (bead gum-q0kd).
//
// Spec §7 credential resolution, item 1, admits a profile's active keychain
// credential "only if its auth_subject_fingerprint matches the selected
// profile's expected subject when one is recorded". The login guard in
// internal/auth covers the interactive path, but a credential can also be
// swapped under a profile without a login: an edited keychain entry, a shared
// keyring, or a legacy grant whose refresh-token-derived fingerprint moved.
// Every cache entry, tee artifact, recovery URI and gain-ledger row is keyed on
// the fingerprint (§10.0.1), so the wrong account does not merely read the
// wrong mailbox; it also writes rows the profile can never reach again.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
)

// strategyCatalog is minimalCatalogFor plus an auth_strategy, which is the
// dimension the expectation map is keyed on.
func strategyCatalog(opID, adapterKey string, strategy catalog.AuthStrategy) *catalog.Catalog {
	snap := &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratedAt:          time.Now().UTC().Format(time.RFC3339),
		GeneratorVersion:     "test",
		Ops: []catalog.Op{
			{
				OpID:             opID,
				OpSchemaVersion:  1,
				Title:            "Test op",
				Summary:          "Test op for the subject guard.",
				DefaultVariantID: "v1",
				Variants: []catalog.Variant{
					{
						VariantID:     "v1",
						Stability:     catalog.StabilityStable,
						InterfaceKind: catalog.InterfaceKindDiscoveryREST,
						BackendKind:   catalog.BackendKindTypedRestSDK,
						RiskClass:     catalog.RiskClassRead,
						AuthStrategy:  strategy,
						Binding: &catalog.Binding{
							BindingSchemaVersion: 1,
							AdapterKey:           adapterKey,
							OperationKey:         opID,
						},
					},
				},
			},
		},
	}
	return snap
}

func TestDispatchRefusesACredentialFromAnotherAccount(t *testing.T) {
	const opID = "test.subject.refuse"
	const adapterKey = "test.adapter"
	adapter := &countingAdapter{}
	sink := &recordingAuditSink{}

	disp := dispatch.NewDispatcherWithConfig(
		strategyCatalog(opID, adapterKey, catalog.AuthStrategyBYOOAuth),
		map[string]dispatch.Adapter{adapterKey: adapter},
		dispatch.DispatcherConfig{
			Auth:                 stubAuth{fp: "principal-B"},
			ExpectedAuthSubjects: map[string]string{"byo_oauth": "principal-A"},
			Audit:                sink,
		},
	)

	_, err := disp.Dispatch(context.Background(), authedInv(opID))
	if err == nil {
		t.Fatal("Dispatch succeeded with a credential for a different account; it must refuse")
	}

	var se *dispatch.StructuredError
	if !errors.As(err, &se) {
		t.Fatalf("Dispatch error = %v (%T), want *dispatch.StructuredError", err, err)
	}
	if se.ErrCode != dispatch.ErrCodeAuthSubjectMismatch {
		t.Errorf("error_code = %q, want %q", se.ErrCode, dispatch.ErrCodeAuthSubjectMismatch)
	}
	if got := se.Detail["auth_strategy"]; got != "byo_oauth" {
		t.Errorf("detail auth_strategy = %v, want byo_oauth", got)
	}
	if got := se.Detail["expected_subject_fingerprint"]; got != "principal-A" {
		t.Errorf("detail expected_subject_fingerprint = %v, want principal-A", got)
	}
	if got := se.Detail["resolved_subject_fingerprint"]; got != "principal-B" {
		t.Errorf("detail resolved_subject_fingerprint = %v, want principal-B", got)
	}

	if got := adapter.calls.Load(); got != 0 {
		t.Errorf("adapter.calls = %d, want 0 — the refusal must land before the executor", got)
	}

	// §10.0.1: "When the active profile's credential subject changes, GUM MUST
	// emit a credential_subject_changed audit event."
	sawEvent := false
	for _, entry := range sink.entries {
		if entry["event"] == "credential_subject_changed" {
			sawEvent = true
			if entry["op_id"] != opID {
				t.Errorf("audit op_id = %v, want %q", entry["op_id"], opID)
			}
			if entry["auth_strategy"] != "byo_oauth" {
				t.Errorf("audit auth_strategy = %v, want byo_oauth", entry["auth_strategy"])
			}
		}
	}
	if !sawEvent {
		t.Errorf("no credential_subject_changed audit entry; got %d entries: %v", len(sink.entries), sink.entries)
	}
}

func TestDispatchAcceptsTheExpectedSubject(t *testing.T) {
	const opID = "test.subject.accept"
	const adapterKey = "test.adapter"
	adapter := &countingAdapter{}

	disp := dispatch.NewDispatcherWithConfig(
		strategyCatalog(opID, adapterKey, catalog.AuthStrategyBYOOAuth),
		map[string]dispatch.Adapter{adapterKey: adapter},
		dispatch.DispatcherConfig{
			Auth:                 stubAuth{fp: "principal-A"},
			ExpectedAuthSubjects: map[string]string{"byo_oauth": "principal-A"},
		},
	)

	if _, err := disp.Dispatch(context.Background(), authedInv(opID)); err != nil {
		t.Fatalf("Dispatch with the expected subject: %v", err)
	}
	if got := adapter.calls.Load(); got != 1 {
		t.Errorf("adapter.calls = %d, want 1", got)
	}
}

// TestDispatchKeepsSubjectExpectationsPerStrategy pins the reason the
// expectation is a map and not one string: the same human on the same Google
// account fingerprints differently under byo_oauth than under gum_oauth,
// because the namespace prefix differs. A flat expectation would refuse every
// mixed-strategy profile.
func TestDispatchKeepsSubjectExpectationsPerStrategy(t *testing.T) {
	const opID = "test.subject.perstrategy"
	const adapterKey = "test.adapter"
	adapter := &countingAdapter{}

	disp := dispatch.NewDispatcherWithConfig(
		strategyCatalog(opID, adapterKey, catalog.AuthStrategyGUMOAuth),
		map[string]dispatch.Adapter{adapterKey: adapter},
		dispatch.DispatcherConfig{
			Auth:                 stubAuth{fp: "managed-fingerprint"},
			ExpectedAuthSubjects: map[string]string{"byo_oauth": "principal-A"},
		},
	)

	if _, err := disp.Dispatch(context.Background(), authedInv(opID)); err != nil {
		t.Fatalf("Dispatch under an unrecorded strategy: %v", err)
	}
	if got := adapter.calls.Load(); got != 1 {
		t.Errorf("adapter.calls = %d, want 1 — no expectation is recorded for gum_oauth", got)
	}
}

// TestDispatchIgnoresAnEmptyFingerprint keeps unauthenticated and
// strategy-none ops working: ResolveAuth may return credentials with no
// subject at all, and there is nothing to compare.
func TestDispatchIgnoresAnEmptyFingerprint(t *testing.T) {
	const opID = "test.subject.empty"
	const adapterKey = "test.adapter"
	adapter := &countingAdapter{}

	disp := dispatch.NewDispatcherWithConfig(
		strategyCatalog(opID, adapterKey, catalog.AuthStrategyBYOOAuth),
		map[string]dispatch.Adapter{adapterKey: adapter},
		dispatch.DispatcherConfig{
			Auth:                 stubAuth{fp: ""},
			ExpectedAuthSubjects: map[string]string{"byo_oauth": "principal-A"},
		},
	)

	if _, err := disp.Dispatch(context.Background(), authedInv(opID)); err != nil {
		t.Fatalf("Dispatch with no resolved subject: %v", err)
	}
	if got := adapter.calls.Load(); got != 1 {
		t.Errorf("adapter.calls = %d, want 1", got)
	}
}
