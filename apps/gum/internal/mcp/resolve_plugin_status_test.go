package mcp

import "testing"

// TestResolvePluginStatusQuarantineWins pins resolvePluginStatus's
// `quarantined == true → return "quarantined"` arm
// (static_resources.go:273-275). Quarantine MUST take precedence over
// any other status flag so an operator can't paper over a poisoned
// plugin by toggling another field.
func TestResolvePluginStatusQuarantineWins(t *testing.T) {
	row := map[string]any{
		"quarantined": true,
		"status":      "active", // would normally win — must lose to quarantine
	}
	if got := resolvePluginStatus(row); got != "quarantined" {
		t.Errorf("got %q; want 'quarantined' (precedence)", got)
	}
}

// TestResolvePluginStatusFallthroughReturnsActive pins the trailing
// `return "active"` arm (static_resources.go:290). Reached when
// neither quarantined nor a non-empty status string is present —
// fresh-install rows that haven't recorded a status yet default
// to active per spec §13 line 3176.
func TestResolvePluginStatusFallthroughReturnsActive(t *testing.T) {
	for _, tc := range []struct {
		name string
		row  map[string]any
	}{
		{"empty_row", map[string]any{}},
		{"empty_status_field", map[string]any{"status": ""}},
		{"quarantined_false_no_status", map[string]any{"quarantined": false}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolvePluginStatus(tc.row); got != "active" {
				t.Errorf("got %q; want 'active' default", got)
			}
		})
	}
}

// TestResolvePluginStatusExplicitStatusPassthrough confirms that a
// non-empty `status` value passes through verbatim when not
// quarantined — this is the precedence-tier-2 arm.
func TestResolvePluginStatusExplicitStatusPassthrough(t *testing.T) {
	for _, want := range []string{"installed_pending_restart", "needs_configuration", "active"} {
		row := map[string]any{"status": want}
		if got := resolvePluginStatus(row); got != want {
			t.Errorf("got %q; want %q passthrough", got, want)
		}
	}
}
