package auth

import (
	"reflect"
	"testing"
)

// TestExpandGrantedScopesAddsSubsumedGmailMetadata pins gum-yn22: a standard
// login grants gmail.readonly (gmail.metadata is pruned from the token), so the
// allowlist fed to the exact-match policy gate must be expanded to include the
// subsumed gmail.metadata, or gmail.users.history.list / getProfile stay broken.
func TestExpandGrantedScopesAddsSubsumedGmailMetadata(t *testing.T) {
	got := ExpandGrantedScopes([]string{scopeGmailReadonly})
	if !contains(got, scopeGmailMetadata) {
		t.Errorf("ExpandGrantedScopes(%v) = %v; want it to add %s", []string{scopeGmailReadonly}, got, scopeGmailMetadata)
	}
	if !contains(got, scopeGmailReadonly) {
		t.Errorf("ExpandGrantedScopes must preserve the original scopes: %v", got)
	}
	// No broad gmail scope → nothing added (don't fabricate a grant).
	bare := ExpandGrantedScopes([]string{"https://www.googleapis.com/auth/calendar.readonly"})
	if contains(bare, scopeGmailMetadata) {
		t.Errorf("must not add gmail.metadata without a covering gmail scope: %v", bare)
	}
	// Idempotent when metadata is already present.
	already := ExpandGrantedScopes([]string{scopeGmailReadonly, scopeGmailMetadata})
	n := 0
	for _, s := range already {
		if s == scopeGmailMetadata {
			n++
		}
	}
	if n != 1 {
		t.Errorf("must not duplicate gmail.metadata: %v", already)
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// TestPruneLoginScopesDropsPoisonousGmailMetadata pins the live-sweep fix:
// gmail.metadata is dropped from a login union when a full-read gmail scope is
// present (it would otherwise block messages.get?format=full), but kept when it
// is the only gmail read scope, and unrelated scopes are untouched.
func TestPruneLoginScopesDropsPoisonousGmailMetadata(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "metadata dropped when readonly present",
			in:   []string{scopeGmailMetadata, scopeGmailReadonly, "https://www.googleapis.com/auth/calendar"},
			want: []string{scopeGmailReadonly, "https://www.googleapis.com/auth/calendar"},
		},
		{
			name: "metadata dropped when mail.google.com present",
			in:   []string{scopeGmailFull, scopeGmailMetadata},
			want: []string{scopeGmailFull},
		},
		{
			name: "metadata kept when no broader gmail scope",
			in:   []string{scopeGmailMetadata, "https://www.googleapis.com/auth/calendar.readonly"},
			want: []string{scopeGmailMetadata, "https://www.googleapis.com/auth/calendar.readonly"},
		},
		{
			name: "no gmail.metadata at all is a no-op",
			in:   []string{scopeGmailReadonly, scopeGmailModify},
			want: []string{scopeGmailReadonly, scopeGmailModify},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := PruneLoginScopes(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("PruneLoginScopes(%v) = %v; want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestScopesSatisfiedSubsumesGmailMetadata pins that an op declaring
// gmail.metadata resolves against a token that holds a broader gmail scope but
// NOT gmail.metadata (the state after PruneLoginScopes), while an unrelated
// missing scope is still unsatisfied.
func TestScopesSatisfiedSubsumesGmailMetadata(t *testing.T) {
	granted := []string{scopeGmailReadonly, "https://www.googleapis.com/auth/calendar"}
	if !scopesSatisfied(granted, []string{scopeGmailMetadata}) {
		t.Error("gmail.metadata requirement not satisfied by gmail.readonly grant; want satisfied (subsumption)")
	}
	if scopesSatisfied(granted, []string{"https://www.googleapis.com/auth/drive"}) {
		t.Error("a genuinely missing scope (drive) was reported satisfied; subsumption must not over-grant")
	}
	// Exact match still works.
	if !scopesSatisfied([]string{scopeGmailMetadata}, []string{scopeGmailMetadata}) {
		t.Error("exact gmail.metadata grant should satisfy gmail.metadata requirement")
	}
}
