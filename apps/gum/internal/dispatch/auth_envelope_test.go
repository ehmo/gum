package dispatch

import (
	"errors"
	"testing"
)

// richAuthErr stands in for *auth.AuthError, which internal/dispatch cannot
// import (internal/auth imports dispatch). It carries the same seam: a method
// that hands dispatch a ready-made structured envelope.
type richAuthErr struct{ se *StructuredError }

func (e *richAuthErr) Error() string { return "auth [compound/AUTH_REQUIRED]: needs more setup" }

func (e *richAuthErr) AsStructuredError() *StructuredError { return e.se }

// TestResolveAuthKeepsRichAuthEnvelope pins spec §7: a
// non-gum_oauth auth failure must reach the caller with auth_strategy,
// missing_components and setup_command intact. Flattening it to a bare
// AUTH_REQUIRED tells the caller `gum auth login` will fix an operation that
// browser OAuth cannot fix.
func TestResolveAuthKeepsRichAuthEnvelope(t *testing.T) {
	envelope := NewStructuredError("AUTH_REQUIRED", "compound auth requires multiple components").
		WithDetail("auth_strategy", "compound").
		WithDetail("missing_components", []string{"developer_token", "customer_id"}).
		WithDetail("setup_command", "gum auth setup googleads.generateKeywordIdeas")

	r := &mockAuthResolver{err: &richAuthErr{se: envelope}}
	d := &dispatcher{auth: r}

	_, err := d.resolveAuth(t.Context(), &Invocation{OpID: "googleads.generateKeywordIdeas"}, &ResolvedVariant{})
	if err == nil {
		t.Fatal("want error, got nil")
	}

	var se *StructuredError
	if !errors.As(err, &se) {
		t.Fatalf("no structured error in chain: %v", err)
	}
	if got := se.Detail["auth_strategy"]; got != "compound" {
		t.Errorf("auth_strategy=%v want compound", got)
	}
	if se.Detail["setup_command"] != "gum auth setup googleads.generateKeywordIdeas" {
		t.Errorf("setup_command=%v want the compound setup command", se.Detail["setup_command"])
	}
	missing, _ := se.Detail["missing_components"].([]string)
	if len(missing) != 2 {
		t.Errorf("missing_components=%v want two components", se.Detail["missing_components"])
	}
}

// TestResolveAuthKeepsNonAuthRequiredCode pins that the four distinct auth
// codes stay distinct. Collapsing AUTH_KEYCHAIN_UNAVAILABLE into AUTH_REQUIRED
// sends the caller to `gum auth login` when the real fix is unlocking the
// keychain.
func TestResolveAuthKeepsNonAuthRequiredCode(t *testing.T) {
	envelope := NewStructuredError("AUTH_KEYCHAIN_UNAVAILABLE", "keychain locked").
		WithDetail("auth_strategy", "byo_oauth")

	r := &mockAuthResolver{err: &richAuthErr{se: envelope}}
	d := &dispatcher{auth: r}

	_, err := d.resolveAuth(t.Context(), &Invocation{OpID: "x"}, &ResolvedVariant{})
	if !IsStructuredError(err, "AUTH_KEYCHAIN_UNAVAILABLE") {
		t.Fatalf("code flattened: got %v, want AUTH_KEYCHAIN_UNAVAILABLE", err)
	}
}
