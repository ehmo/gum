package dispatch

import (
	"errors"
	"testing"
)

func TestScopeLoadFailureAndRetry(t *testing.T) {
	calls := 0
	d := newPolicyDispatcher(ProfilePolicy{LoadScopes: func() ([]string, error) {
		calls++
		if calls == 1 {
			return nil, &richAuthErr{se: NewStructuredError("AUTH_KEYCHAIN_UNAVAILABLE", "keychain timed out").WithRetryable(true)}
		}
		return []string{"https://www.googleapis.com/auth/drive"}, nil
	}})
	inv := &Invocation{OpID: "test.scoped.op", AllowWrite: true}
	if se := d.evaluatePolicy(t.Context(), inv); se == nil || se.ErrCode != "AUTH_KEYCHAIN_UNAVAILABLE" || !se.Retryable {
		t.Fatalf("first call = %v; want retryable Keychain failure", se)
	}
	if se := d.evaluatePolicy(t.Context(), inv); se != nil {
		t.Fatalf("retry = %v; want readable grant to pass", se)
	}
	if calls != 2 {
		t.Fatalf("scope reads = %d; want 2", calls)
	}
}

func TestScopeLoadSkipsUnscopedAndDeniedCalls(t *testing.T) {
	d := newPolicyDispatcher(ProfilePolicy{
		DenyOps:    []string{"test.scoped.op"},
		LoadScopes: func() ([]string, error) { t.Fatal("unexpected scope read"); return nil, nil },
	})
	if se := d.evaluatePolicy(t.Context(), &Invocation{OpID: "test.read.op"}); se != nil {
		t.Fatal(se)
	}
	if se := d.evaluatePolicy(t.Context(), &Invocation{OpID: "test.scoped.op", AllowWrite: true}); se == nil || se.ErrCode != ErrCodePolicyDenied {
		t.Fatalf("denied op = %v; want POLICY_DENIED", se)
	}
}

func TestScopeLoadUnknownErrorFailsClosed(t *testing.T) {
	for _, err := range []error{errors.New("decode failed"), &richAuthErr{}} {
		d := newPolicyDispatcher(ProfilePolicy{LoadScopes: func() ([]string, error) { return nil, err }})
		if se := d.evaluatePolicy(t.Context(), &Invocation{OpID: "test.scoped.op", AllowWrite: true}); se == nil || se.ErrCode != ErrCodeAuthRequired {
			t.Fatalf("scope error = %v; want AUTH_REQUIRED", se)
		}
	}
}
