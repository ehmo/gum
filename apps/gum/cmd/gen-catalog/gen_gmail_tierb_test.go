// gum-ho3 acceptance: Tier B Gmail surface (labels, threads, history,
// settings) so `gum search 'gmail labels'` returns relevant ops and
// `gum.describe_op(gmail.users.labels.list)` succeeds.

package main

import (
	"strings"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/catalog"
)

// TestBuildGmailTierBOpsCoverage pins that the Tier B Gmail expansion adds
// the editorially fixed surface beyond the four dispatch-curated Tier A ops
// (gmail.users.messages.list/get/send/trash) already emitted by
// GenerateFromDiscoveries. Missing any of these would regress BM25 search
// coverage and break the "gum search 'gmail labels'" acceptance criterion.
func TestBuildGmailTierBOpsCoverage(t *testing.T) {
	want := []struct {
		opID      string
		riskClass catalog.RiskClass
	}{
		// threads
		{"gmail.users.threads.list", catalog.RiskClassRead},
		{"gmail.users.threads.get", catalog.RiskClassRead},
		{"gmail.users.threads.modify", catalog.RiskClassWrite},
		{"gmail.users.threads.trash", catalog.RiskClassDestructive},
		{"gmail.users.threads.untrash", catalog.RiskClassWrite},
		// messages (additional)
		{"gmail.users.messages.untrash", catalog.RiskClassWrite},
		{"gmail.users.messages.modify", catalog.RiskClassWrite},
		{"gmail.users.messages.batchModify", catalog.RiskClassWrite},
		{"gmail.users.messages.delete", catalog.RiskClassDestructive},
		{"gmail.users.messages.batchDelete", catalog.RiskClassDestructive},
		// labels
		{"gmail.users.labels.get", catalog.RiskClassRead},
		{"gmail.users.labels.create", catalog.RiskClassWrite},
		{"gmail.users.labels.update", catalog.RiskClassWrite},
		{"gmail.users.labels.patch", catalog.RiskClassWrite},
		{"gmail.users.labels.delete", catalog.RiskClassDestructive},
		// drafts
		{"gmail.users.drafts.list", catalog.RiskClassRead},
		{"gmail.users.drafts.get", catalog.RiskClassRead},
		{"gmail.users.drafts.update", catalog.RiskClassWrite},
		{"gmail.users.drafts.send", catalog.RiskClassWrite},
		{"gmail.users.drafts.delete", catalog.RiskClassDestructive},
		// history + profile + settings
		{"gmail.users.history.list", catalog.RiskClassRead},
		{"gmail.users.getProfile", catalog.RiskClassRead},
		{"gmail.users.settings.getVacation", catalog.RiskClassRead},
		{"gmail.users.settings.updateVacation", catalog.RiskClassWrite},
	}

	got := BuildGmailTierBOps()
	byID := make(map[string]catalog.Op, len(got))
	for _, op := range got {
		byID[op.OpID] = op
	}

	for _, w := range want {
		op, ok := byID[w.opID]
		if !ok {
			t.Errorf("op %q missing from Gmail Tier B set", w.opID)
			continue
		}
		if op.Service != "gmail" {
			t.Errorf("%s: service=%q want gmail", w.opID, op.Service)
		}
		if op.ServiceFamily != "workspace" {
			t.Errorf("%s: service_family=%q want workspace", w.opID, op.ServiceFamily)
		}
		if len(op.Variants) != 1 {
			t.Errorf("%s: variants=%d want 1", w.opID, len(op.Variants))
			continue
		}
		v := op.Variants[0]
		if v.RiskClass != w.riskClass {
			t.Errorf("%s: risk_class=%q want %q", w.opID, v.RiskClass, w.riskClass)
		}
		if v.AuthStrategy != catalog.AuthStrategyBYOOAuth {
			t.Errorf("%s: auth_strategy=%q want byo_oauth", w.opID, v.AuthStrategy)
		}
		if v.BackendKind != catalog.BackendKindTypedRestSDK {
			t.Errorf("%s: backend_kind=%q want typed-rest-sdk", w.opID, v.BackendKind)
		}
		if v.InterfaceKind != catalog.InterfaceKindDiscoveryREST {
			t.Errorf("%s: interface_kind=%q want discovery-rest", w.opID, v.InterfaceKind)
		}
		if len(v.Scopes) == 0 {
			t.Errorf("%s: no scopes set (Tier B must carry explicit OAuth scopes)", w.opID)
		}
		if v.Binding == nil || v.Binding.HTTP == nil {
			t.Errorf("%s: missing binding.http", w.opID)
			continue
		}
		if !strings.HasPrefix(v.Binding.HTTP.Path, "/gmail/v1/users/") {
			t.Errorf("%s: http.path=%q does not start with /gmail/v1/users/", w.opID, v.Binding.HTTP.Path)
		}
		if v.Binding.GoPkg != "google.golang.org/api/gmail/v1" {
			t.Errorf("%s: go_pkg=%q want google.golang.org/api/gmail/v1", w.opID, v.Binding.GoPkg)
		}
	}
}

// TestBuildGmailTierBOpsValidates ensures the Tier B-only catalog passes
// catalog.Validate.
func TestBuildGmailTierBOpsValidates(t *testing.T) {
	cat := &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratedAt:          time.Now().UTC().Format(time.RFC3339),
		GeneratorVersion:     "test",
		Ops:                  BuildGmailTierBOps(),
	}
	if err := cat.Validate(); err != nil {
		t.Fatalf("validate Gmail Tier B-only catalog: %v", err)
	}
}

// TestBuildGmailTierBOpsRejectsGUMOAuth pins that no Tier B variant uses
// gum_oauth (disabled in v0.1.0 per bd memory gum-auth-strategy-v3).
func TestBuildGmailTierBOpsRejectsGUMOAuth(t *testing.T) {
	for _, op := range BuildGmailTierBOps() {
		for _, v := range op.Variants {
			if v.AuthStrategy == catalog.AuthStrategyGUMOAuth {
				t.Errorf("op %s variant %s: gum_oauth disabled in v0.1.0", op.OpID, v.VariantID)
			}
		}
	}
}

// TestBuildGmailTierBOpsDestructiveScopes pins that destructive ops carry
// the full-mail or full-modify scope (gmail.modify is not sufficient for
// permanent deletes per Google API spec — those require https://mail.google.com/).
func TestBuildGmailTierBOpsDestructiveScopes(t *testing.T) {
	for _, op := range BuildGmailTierBOps() {
		v := op.Variants[0]
		if v.RiskClass != catalog.RiskClassDestructive {
			continue
		}
		switch op.OpID {
		case "gmail.users.messages.delete", "gmail.users.messages.batchDelete":
			// Permanent deletes require https://mail.google.com/ per Google docs.
			found := false
			for _, s := range v.Scopes {
				if s == "https://mail.google.com/" {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("%s: destructive permanent-delete op missing https://mail.google.com/ scope, got %v",
					op.OpID, v.Scopes)
			}
		}
	}
}
