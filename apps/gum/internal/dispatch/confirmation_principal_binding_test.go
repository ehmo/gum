package dispatch_test

import (
	"context"
	"testing"

	"github.com/ehmo/gum/internal/dispatch"
)

// switchableAuth is a mutable stub principal. Flipping fp between two Dispatch
// calls on ONE dispatcher isolates the resolved principal as the only variable:
// the confirmation HMAC key is per-dispatcher, so a second dispatcher would
// reject the token for key reasons and hide what is under test.
type switchableAuth struct{ fp string }

func (s *switchableAuth) ResolveAuth(_ context.Context, _ *dispatch.Invocation, _ *dispatch.ResolvedVariant) (*dispatch.Credentials, error) {
	return &dispatch.Credentials{Token: "tok", SubjectFingerprint: s.fp}, nil
}

// TestConfirmationTokenIsProfileScopedNotPrincipalScoped pins what the §6.1.2
// binding tuple actually covers. A destructive token issued while principal-A is
// active still verifies once the active credential is principal-B, because
// confirmation runs in step 2 and credentials do not resolve until step 4, so no
// principal is known at issue or at verify.
//
// ConfirmationParams used to carry an AuthFingerprint field for this. Nothing on
// the CLI or MCP path ever populated it, so it hashed an empty string on every
// call and read as a guarantee gum does not make; gum-b7hq removed it. The real
// wrong-account defence belongs at credential resolution, and that is where it
// lives: checkAuthSubject refuses with AUTH_SUBJECT_MISMATCH in step 4 when the
// profile recorded a subject for the strategy (gum-q0kd). This dispatcher wires
// no expectation, so the token path is the only variable under test here.
func TestConfirmationTokenIsProfileScopedNotPrincipalScoped(t *testing.T) {
	args := map[string]any{"userId": "me", "id": "msg001"}
	adapter := &confirmCountingAdapter{}
	who := &switchableAuth{fp: "principal-A"}
	disp := dispatch.NewDispatcherWithConfig(
		destructiveCatalog(),
		map[string]dispatch.Adapter{"test.counting": adapter},
		dispatch.DispatcherConfig{Auth: who},
	)

	tok := firstContactToken(t, disp, "gmail.users.messages.trash", args)

	who.fp = "principal-B"
	if _, err := disp.Dispatch(context.Background(), &dispatch.Invocation{
		OpID:              "gmail.users.messages.trash",
		Args:              args,
		Format:            "json",
		RequestID:         "principal-switched",
		Confirmed:         true,
		ConfirmationToken: tok,
	}); err != nil {
		t.Fatalf("dispatch after principal switch: %v", err)
	}
	if adapter.calls != 1 {
		t.Fatalf("adapter calls = %d; want 1", adapter.calls)
	}
}
