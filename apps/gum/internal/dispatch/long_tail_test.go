// Package dispatch — spec §5.7 long-tail dispatcher tests (issue gum-fmi).
//
// The §5.7 read-only allowlist escape hatch lets raw-http / discovery-rest
// read variants pass through unknown query parameters with a
// `_validation_warnings` envelope entry instead of an INVALID_ARGS error.
// This test file pins the four behavioural axes the kernel must honour:
//
//  1. Default reject — without a ProfilePolicy entry, an unknown key is
//     surfaced as INVALID_ARGS (parsed.ValidationWarnings stays empty).
//  2. Allowlist pass-through — a key listed in
//     ProfilePolicy.UnknownReadParamsAllowlist[op_id] is waived with a
//     "_validation_warnings"-shaped string; the parsedInvocation carries
//     the warning so downstream steps can fold it into the response.
//  3. Write/destructive ignored — the allowlist must NOT fire when the
//     caller asserts allow_write or allow_destructive on the invocation,
//     even if the op_id is keyed in the allowlist map.
//  4. Strict mode disables — ProfilePolicy.StrictValidation=true short-
//     circuits the allowlist, restoring the default INVALID_ARGS path.
//
// All four cases share the same catalog (an admin.directory.users.list-
// like op with backend_kind=raw-http + risk=read) so the gate inputs only
// vary by ProfilePolicy / Invocation flags.
package dispatch

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/catalog"
)

// longTailTestCatalog builds an inline catalog mirroring the §5.7 surface
// (admin.directory.users.list backed by raw-http, read-class). The default
// variant's backend_kind+risk_class drive applyReadOnlyAllowlist's eligibility
// check, so this fixture must keep both fields aligned with the spec.
func longTailTestCatalog() *catalog.Catalog {
	return &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratedAt:          time.Now().UTC().Format(time.RFC3339),
		GeneratorVersion:     "test@0.0.0",
		Ops: []catalog.Op{
			{
				OpID:             "admin.directory.users.list",
				OpSchemaVersion:  1,
				Title:            "List directory users",
				Summary:          "Long-tail raw-http read op for §5.7 tests.",
				Service:          "admin",
				ServiceFamily:    "workspace",
				ParamsRequired:   [][]string{{"customer", "string"}},
				ParamsOptional:   [][]string{{"maxResults", "integer"}},
				DefaultVariantID: "admin.directory_v1.rawhttp.users.list",
				Variants: []catalog.Variant{
					{
						VariantID:            "admin.directory_v1.rawhttp.users.list",
						VariantSchemaVersion: 1,
						Version:              "v1",
						Stability:            catalog.StabilityStable,
						InterfaceKind:        catalog.InterfaceKindDiscoveryREST,
						BackendKind:          catalog.BackendKindRawHTTP,
						RiskClass:            catalog.RiskClassRead,
						AuthStrategy:         catalog.AuthStrategyBYOOAuth,
						Binding: &catalog.Binding{
							BindingSchemaVersion: 1,
							AdapterKey:           "rest.raw-http",
							OperationKey:         "admin.directory.users.list",
							HTTP: &catalog.HTTPBinding{
								Method: "GET",
								Path:   "/admin/directory/v1/users",
							},
						},
					},
				},
			},
		},
	}
}

// newLongTailDispatcher wires the long-tail catalog with a caller-supplied
// ProfilePolicy. Other dispatcher seams (adapters, auth, cache) stay nil — the
// test only exercises step-1 parseAndValidate, which doesn't touch them.
func newLongTailDispatcher(pol ProfilePolicy) *dispatcher {
	return &dispatcher{
		snapshot:      longTailTestCatalog(),
		adapters:      map[string]Adapter{},
		profilePolicy: pol,
	}
}

// TestLongTailUnknownArgHandling pins the four §5.7 allowlist axes.
//
// Acceptance for gum-fmi: this test must cover (a) default reject, (b)
// allowlist warning pass-through, (c) write/destructive bypass, and (d)
// strict-mode disable in a single table-driven harness so a regression in
// any one axis surfaces as a focused subtest failure.
func TestLongTailUnknownArgHandling(t *testing.T) {
	type want struct {
		wantErr       bool                  // expect *StructuredError (vs nil)
		wantUnknown   []string              // INVALID_ARGS detail['unknown'] when wantErr
		wantWarnings  []string              // parsed.ValidationWarnings when !wantErr
	}

	cases := []struct {
		name   string
		policy ProfilePolicy
		inv    *Invocation
		want   want
	}{
		{
			name:   "default-reject: no allowlist entry → INVALID_ARGS",
			policy: ProfilePolicy{},
			inv: &Invocation{
				OpID: "admin.directory.users.list",
				Args: map[string]any{
					"customer":    "my_customer",
					"viewType":    "admin_view", // unknown — not in params_*
				},
			},
			want: want{wantErr: true, wantUnknown: []string{"viewType"}},
		},
		{
			name: "allowlist-warning: keyed unknown waived with pass-through",
			policy: ProfilePolicy{
				UnknownReadParamsAllowlist: map[string][]string{
					"admin.directory.users.list": {"viewType", "projection"},
				},
			},
			inv: &Invocation{
				OpID: "admin.directory.users.list",
				Args: map[string]any{
					"customer": "my_customer",
					"viewType": "admin_view",
				},
			},
			want: want{
				wantErr:      false,
				wantWarnings: []string{"viewType"},
			},
		},
		{
			name: "write-bypasses-allowlist: AllowWrite=true disables the gate",
			policy: ProfilePolicy{
				UnknownReadParamsAllowlist: map[string][]string{
					"admin.directory.users.list": {"viewType"},
				},
			},
			inv: &Invocation{
				OpID: "admin.directory.users.list",
				Args: map[string]any{
					"customer": "my_customer",
					"viewType": "admin_view",
				},
				AllowWrite: true,
			},
			want: want{wantErr: true, wantUnknown: []string{"viewType"}},
		},
		{
			name: "destructive-bypasses-allowlist: AllowDestructive=true disables",
			policy: ProfilePolicy{
				UnknownReadParamsAllowlist: map[string][]string{
					"admin.directory.users.list": {"viewType"},
				},
			},
			inv: &Invocation{
				OpID: "admin.directory.users.list",
				Args: map[string]any{
					"customer": "my_customer",
					"viewType": "admin_view",
				},
				AllowDestructive: true,
			},
			want: want{wantErr: true, wantUnknown: []string{"viewType"}},
		},
		{
			name: "strict-validation: allowlist disabled even when keyed",
			policy: ProfilePolicy{
				StrictValidation: true,
				UnknownReadParamsAllowlist: map[string][]string{
					"admin.directory.users.list": {"viewType"},
				},
			},
			inv: &Invocation{
				OpID: "admin.directory.users.list",
				Args: map[string]any{
					"customer": "my_customer",
					"viewType": "admin_view",
				},
			},
			want: want{wantErr: true, wantUnknown: []string{"viewType"}},
		},
		{
			name: "partial-coverage: one unknown waived, one rejected",
			policy: ProfilePolicy{
				UnknownReadParamsAllowlist: map[string][]string{
					"admin.directory.users.list": {"viewType"},
				},
			},
			inv: &Invocation{
				OpID: "admin.directory.users.list",
				Args: map[string]any{
					"customer":   "my_customer",
					"viewType":   "admin_view", // waived
					"orderBy":    "email",       // not in allowlist → reject
				},
			},
			want: want{wantErr: true, wantUnknown: []string{"orderBy"}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := newLongTailDispatcher(tc.policy)
			parsed, serr := d.parseAndValidate(context.Background(), tc.inv)

			if tc.want.wantErr {
				if serr == nil {
					t.Fatalf("expected INVALID_ARGS, got nil error (parsed=%+v)", parsed)
				}
				if serr.ErrCode != ErrCodeInvalidArgs {
					t.Fatalf("ErrCode = %q, want %q", serr.ErrCode, ErrCodeInvalidArgs)
				}
				unknown, _ := serr.Detail["unknown"].([]string)
				if !stringSliceEqual(unknown, tc.want.wantUnknown) {
					t.Errorf("detail['unknown'] = %v, want %v", unknown, tc.want.wantUnknown)
				}
				return
			}

			if serr != nil {
				t.Fatalf("expected nil error, got %v", serr)
			}
			if parsed == nil {
				t.Fatalf("expected parsedInvocation, got nil")
			}
			if len(parsed.ValidationWarnings) != len(tc.want.wantWarnings) {
				t.Fatalf("ValidationWarnings count = %d (%v), want %d (%v)",
					len(parsed.ValidationWarnings), parsed.ValidationWarnings,
					len(tc.want.wantWarnings), tc.want.wantWarnings)
			}
			// Verify each waived key surfaces in the warning string.
			joined := strings.Join(parsed.ValidationWarnings, " | ")
			for _, key := range tc.want.wantWarnings {
				if !strings.Contains(joined, key) {
					t.Errorf("ValidationWarnings = %q, missing waived key %q", joined, key)
				}
			}
			if !strings.Contains(joined, "read-only allowlist") {
				t.Errorf("ValidationWarnings = %q, missing 'read-only allowlist' marker", joined)
			}
		})
	}
}

// stringSliceEqual compares two slices by element identity, treating nil and
// empty as equal. Order-sensitive — the kernel keeps params_* declaration
// order so we don't need a set comparison here.
func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
