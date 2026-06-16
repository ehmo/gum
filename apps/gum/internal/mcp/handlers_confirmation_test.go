// Spec gum-3j3g: the REQUIRES_CONFIRMATION envelope returned by
// gum.destructive MUST carry a freshly issued, server-signed
// confirmation_token. The caller echoes it back on the follow-up call;
// the policy gate then verifies signature, binding, and replay before
// admitting the dispatch.
//
// TDD red (gum-46uq): handleRiskTier currently passes through to
// dispatch.evaluatePolicy, which emits REQUIRES_CONFIRMATION as
// {error_code, message, op_id, risk_class} — no confirmation_token field.
// Tampered tokens that match the loose tokenFormatRe regex are accepted
// (policy.go:126). All three subtests fail until gum-3j3g lands.
//
// The dispatcher used here is dispatch.NewDispatcher with a minimal
// destructive op so the real policy gate runs.

package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// destructiveTestCatalog builds a minimal catalog with one destructive op
// whose default variant is wired to a stub adapter key. The policy gate
// fires before adapter resolution for the cases this test exercises.
func destructiveTestCatalog(opID string) *catalog.Catalog {
	variantID := opID + ".v1.test"
	return &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratedAt:          "2026-01-01T00:00:00Z",
		GeneratorVersion:     "test",
		Ops: []catalog.Op{
			{
				OpID:             opID,
				OpSchemaVersion:  1,
				Title:            opID,
				Summary:          "test destructive op for " + opID,
				DefaultVariantID: variantID,
				Variants: []catalog.Variant{
					{
						VariantID:     variantID,
						Stability:     catalog.StabilityStable,
						InterfaceKind: catalog.InterfaceKindDiscoveryREST,
						BackendKind:   catalog.BackendKindDiscoveryREST,
						RiskClass:     catalog.RiskClassDestructive,
						Binding: &catalog.Binding{
							AdapterKey: "rest.typed-rest-sdk",
						},
					},
				},
			},
		},
	}
}

// makeDestructiveRequest mirrors makeReadRequest but targets gum.destructive.
func makeDestructiveRequest(args map[string]any) *sdkmcp.CallToolRequest {
	raw, _ := json.Marshal(args)
	return &sdkmcp.CallToolRequest{
		Params: &sdkmcp.CallToolParamsRaw{
			Name:      "gum.destructive",
			Arguments: raw,
		},
	}
}

// TestDestructiveRequiresConfirmationCarriesToken: first call without
// confirmed/token MUST return REQUIRES_CONFIRMATION with a non-empty
// confirmation_token field the caller can echo back.
func TestDestructiveRequiresConfirmationCarriesToken(t *testing.T) {
	const opID = "drive.files.delete"
	snap := destructiveTestCatalog(opID)
	disp := dispatch.NewDispatcher(snap, map[string]dispatch.Adapter{})
	srv := NewServerWithCatalog(disp, snap)

	req := makeDestructiveRequest(map[string]any{
		"op_id": opID,
		"args":  map[string]any{"fileId": "F1"},
	})
	res, err := srv.handleDestructive(context.Background(), req)
	if err != nil {
		t.Fatalf("handleDestructive returned unexpected Go error: %v", err)
	}
	m := parseErrorResult(t, res)

	if code, _ := m["error_code"].(string); code != "REQUIRES_CONFIRMATION" {
		t.Errorf("error_code=%q; want REQUIRES_CONFIRMATION", code)
	}
	tok, _ := m["confirmation_token"].(string)
	if tok == "" {
		t.Error("REQUIRES_CONFIRMATION envelope missing non-empty confirmation_token field (spec gum-3j3g)")
	}
}

// TestDestructiveConfirmedWithoutTokenIsInvalidMissing: confirmed=true
// but no token MUST surface CONFIRMATION_TOKEN_INVALID with reason="missing".
func TestDestructiveConfirmedWithoutTokenIsInvalidMissing(t *testing.T) {
	const opID = "drive.files.delete"
	snap := destructiveTestCatalog(opID)
	disp := dispatch.NewDispatcher(snap, map[string]dispatch.Adapter{})
	srv := NewServerWithCatalog(disp, snap)

	req := makeDestructiveRequest(map[string]any{
		"op_id":     opID,
		"args":      map[string]any{"fileId": "F1"},
		"confirmed": true,
		// confirmation_token omitted on purpose.
	})
	res, err := srv.handleDestructive(context.Background(), req)
	if err != nil {
		t.Fatalf("handleDestructive returned unexpected Go error: %v", err)
	}
	m := parseErrorResult(t, res)

	if code, _ := m["error_code"].(string); code != "CONFIRMATION_TOKEN_INVALID" {
		t.Errorf("error_code=%q; want CONFIRMATION_TOKEN_INVALID (spec §6.1.2)", code)
	}
	if reason, _ := m["reason"].(string); reason != "missing" {
		t.Errorf("reason=%q; want \"missing\" (confirmed=true with no token)", reason)
	}
}

// TestDestructiveTamperedTokenIsInvalidMismatch: a well-formed token whose
// signature byte has been flipped MUST surface CONFIRMATION_TOKEN_INVALID
// with reason="mismatch". The current loose-regex gate (policy.go:126)
// admits well-formed tampered tokens — this is the security bug.
func TestDestructiveTamperedTokenIsInvalidMismatch(t *testing.T) {
	const opID = "drive.files.delete"
	snap := destructiveTestCatalog(opID)
	disp := dispatch.NewDispatcher(snap, map[string]dispatch.Adapter{})
	srv := NewServerWithCatalog(disp, snap)

	// Issue a real token bound to the same params the handler will derive,
	// then flip a hex digit in the trailing signature segment.
	good, err := dispatch.IssueConfirmationToken(dispatch.ConfirmationParams{
		OpID:      opID,
		VariantID: opID + ".v1.test",
		ArgsHash:  "ignored-the-handler-will-compute-its-own",
		Purpose:   dispatch.ConfirmationPurposeDestructive,
		TTL:       dispatch.DefaultDestructiveTokenTTL,
	})
	if err != nil {
		t.Fatalf("IssueConfirmationToken: %v", err)
	}
	parts := strings.Split(good, ".")
	if len(parts) < 6 {
		t.Fatalf("issued token has %d parts; want >=6", len(parts))
	}
	sig := parts[len(parts)-1]
	// Flip the first hex digit (0↔1, otherwise xor 1).
	first := sig[0]
	var swap byte
	switch first {
	case '0':
		swap = '1'
	case '1':
		swap = '0'
	default:
		swap = first ^ 1
	}
	parts[len(parts)-1] = string(swap) + sig[1:]
	tampered := strings.Join(parts, ".")

	req := makeDestructiveRequest(map[string]any{
		"op_id":              opID,
		"args":               map[string]any{"fileId": "F1"},
		"confirmed":          true,
		"confirmation_token": tampered,
	})
	res, err := srv.handleDestructive(context.Background(), req)
	if err != nil {
		t.Fatalf("handleDestructive returned unexpected Go error: %v", err)
	}
	m := parseErrorResult(t, res)

	if code, _ := m["error_code"].(string); code != "CONFIRMATION_TOKEN_INVALID" {
		t.Errorf("error_code=%q; want CONFIRMATION_TOKEN_INVALID (tampered signature)", code)
	}
	if reason, _ := m["reason"].(string); reason != "mismatch" {
		t.Errorf("reason=%q; want \"mismatch\" (tampered signature)", reason)
	}
}
