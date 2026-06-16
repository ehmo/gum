package mcp

import (
	"reflect"
	"testing"
)

// TestCredentialAliasNamesNilStateRowReturnsNil pins the
// `stateRow == nil → return nil` arm
// (inactive_plugin_resource.go:163-165). A row that wasn't found
// (nil map) must produce nil — not panic via map index.
func TestCredentialAliasNamesNilStateRowReturnsNil(t *testing.T) {
	t.Parallel()
	if got := credentialAliasNames(nil); got != nil {
		t.Errorf("got=%v; want nil", got)
	}
}

// TestCredentialAliasNamesSkipsNonMapDescriptor pins the inner
// `!ok → continue` arm (inactive_plugin_resource.go:170-171). A raw
// non-map descriptor in the slice must be skipped, while valid map
// rows still surface their alias.
func TestCredentialAliasNamesSkipsNonMapDescriptor(t *testing.T) {
	t.Parallel()
	row := map[string]any{
		"credential_descriptors": []any{
			"not-a-map",
			map[string]any{"alias": "good"},
			map[string]any{"alias": ""}, // empty alias dropped by line 173
		},
	}
	got := credentialAliasNames(row)
	want := []string{"good"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got=%v; want %v", got, want)
	}
}
