package auth

import (
	"errors"
	"reflect"
	"testing"
)

// TestLoadGrantKeyringErrorPropagates pins that a keyring read failure surfaces
// as (zero, false, err) rather than being silently swallowed — Acquire treats a
// read error like an absent grant for routing, but the error is still returned
// so callers can distinguish a backend fault from a clean miss.
func TestLoadGrantKeyringErrorPropagates(t *testing.T) {
	sentinel := errors.New("keyring backend down")
	b := NewByoOAuth(ByoOAuthConfig{ClientID: "c"}, errKeyring{getErr: sentinel})
	g, ok, err := b.loadGrant()
	if !errors.Is(err, sentinel) {
		t.Fatalf("loadGrant err = %v; want %v", err, sentinel)
	}
	if ok {
		t.Error("ok = true; want false on a keyring error")
	}
	if g.RefreshToken != "" || g.Scopes != nil {
		t.Errorf("grant = %+v; want zero value", g)
	}
}

// TestLoadGrantUnparseableValueTreatedAsAbsent pins that a legacy or corrupt
// (non-JSON) stored value is treated as no grant (ok=false, nil err) so the
// caller re-authorizes cleanly instead of stranding on undecodable state.
func TestLoadGrantUnparseableValueTreatedAsAbsent(t *testing.T) {
	b := NewByoOAuth(ByoOAuthConfig{ClientID: "c"}, &mockKeyring{data: map[string]string{}})
	b.kb.(*mockKeyring).data[b.keyringKey()] = "not-json{"
	g, ok, err := b.loadGrant()
	if err != nil {
		t.Fatalf("loadGrant err = %v; want nil", err)
	}
	if ok {
		t.Error("ok = true; want false for an unparseable value")
	}
	if g.RefreshToken != "" {
		t.Errorf("grant = %+v; want zero value", g)
	}
}

// TestSortedUniqueScopesDedupesAndDropsEmpties pins the union helper: duplicate
// and empty scopes collapse to a single sorted set across multiple lists.
func TestSortedUniqueScopesDedupesAndDropsEmpties(t *testing.T) {
	got := sortedUniqueScopes(
		[]string{"b", "", "a", "b"},
		[]string{"a", "c", ""},
	)
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sortedUniqueScopes = %v; want %v", got, want)
	}
}
