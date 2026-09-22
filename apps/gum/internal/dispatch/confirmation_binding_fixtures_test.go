package dispatch

import (
	"context"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/catalog"
)

// gumCodeBindingDispatcher builds a kernel whose catalog holds the read-class
// gum.code op, the one op whose allow_* flags route to the confirmation gate.
func gumCodeBindingDispatcher(profile string) *dispatcher {
	cat := policyTestCatalog()
	cat.Ops = append(cat.Ops, catalog.Op{
		OpID:             "gum.code",
		OpSchemaVersion:  1,
		Title:            "Run code",
		Summary:          "Executes a sandboxed Risor script.",
		DefaultVariantID: "gum.code.v1.risor",
		Variants: []catalog.Variant{{
			VariantID:     "gum.code.v1.risor",
			Stability:     catalog.StabilityStable,
			InterfaceKind: catalog.InterfaceKindSDKNative,
			BackendKind:   catalog.BackendKindTypedRestSDK,
			RiskClass:     catalog.RiskClassRead,
			AuthStrategy:  catalog.AuthStrategyNone,
			Binding: &catalog.Binding{
				BindingSchemaVersion: 1,
				AdapterKey:           "code.risor",
				OperationKey:         "gum.code.risor.exec",
			},
		}},
	})
	return &dispatcher{snapshot: cat, adapters: map[string]Adapter{}, profileName: profile}
}

// codeInvocation is the elevated gum.code shape the CLI and the MCP tool both
// build. Every field here is one the matrix row says the token binds.
type codeInvocation struct {
	language         string
	source           string
	budget           int
	scope            []any
	allowWrite       bool
	allowDestructive bool
}

func (c codeInvocation) invocation(confirmed bool, token string) *Invocation {
	args := map[string]any{"language": c.language, "source": c.source}
	if c.budget > 0 {
		args["destructive_budget"] = c.budget
	}
	if c.scope != nil {
		args["destructive_scope"] = c.scope
	}
	return &Invocation{
		OpID:              "gum.code",
		Args:              args,
		AllowWrite:        c.allowWrite,
		AllowDestructive:  c.allowDestructive,
		Confirmed:         confirmed,
		ConfirmationToken: token,
		Caller:            CallerCLI,
	}
}

func elevatedCode() codeInvocation {
	return codeInvocation{
		language:         "risor",
		source:           `gum_call("drive.files.delete", {"fileId": "a"})`,
		budget:           2,
		scope:            []any{"drive.files.delete:a"},
		allowDestructive: true,
	}
}

// mintCodeToken runs the unconfirmed half of the handshake and returns the
// token the kernel issued for that exact invocation.
func mintCodeToken(t *testing.T, d *dispatcher, c codeInvocation) string {
	t.Helper()
	se := d.evaluatePolicy(context.Background(), c.invocation(false, ""))
	if se == nil || se.ErrCode != ErrCodeRequiresConfirmation {
		t.Fatalf("issue: evaluatePolicy = %v; want REQUIRES_CONFIRMATION", se)
	}
	tok, _ := se.Detail["confirmation_token"].(string)
	if tok == "" {
		t.Fatal("issue: REQUIRES_CONFIRMATION carried no confirmation_token")
	}
	return tok
}

// assertCodeRebindRejected re-presents a token against a broadened invocation
// and requires the kernel to refuse it. Every case here hands the sandbox more
// authority than the operator approved, so a pass means an approval for one
// request authorized a strictly larger one.
func assertCodeRebindRejected(t *testing.T, d *dispatcher, tok string, broadened codeInvocation) {
	t.Helper()
	se := d.evaluatePolicy(context.Background(), broadened.invocation(true, tok))
	if se == nil {
		t.Fatal("broadened re-invocation was accepted on the original token")
	}
	if se.ErrCode != ErrCodeConfirmationTokenInvalid {
		t.Fatalf("ErrCode = %q; want CONFIRMATION_TOKEN_INVALID", se.ErrCode)
	}
	if reason, _ := se.Detail["reason"].(string); reason != tokenReasonMismatch {
		t.Errorf("reason = %q; want %q", reason, tokenReasonMismatch)
	}
}

// TestConfirmationTokenBinding is the docs/test-matrix.md row-20 proof for
// §6.1.2. It carries the row's whole fixture set (a) to (f) and the clause that
// gum.code tokens bind language, source, allow_write, allow_destructive,
// destructive_budget and the canonical destructive_scope.
//
// The (e) cases are the sharp ones: they re-present a valid token against an
// invocation that asks for more than the operator approved. Each must be
// refused, or one y answer escalates into unlimited authority.
func TestConfirmationTokenBinding(t *testing.T) {
	t.Run("a_replay_within_ttl", func(t *testing.T) {
		ResetReplayCacheForTest()
		p := confirmationBindingParams(5 * time.Minute)
		tok, err := IssueConfirmationToken(p)
		if err != nil {
			t.Fatalf("IssueConfirmationToken: %v", err)
		}
		if err := VerifyConfirmationToken(tok, p); err != nil {
			t.Fatalf("first use: %v; want nil", err)
		}
		assertTokenInvalid(t, VerifyConfirmationToken(tok, p), tokenReasonReplayed)
	})

	t.Run("b_replay_after_ttl", func(t *testing.T) {
		ResetReplayCacheForTest()
		const ttl = 20 * time.Millisecond
		p := confirmationBindingParams(ttl)
		issued := time.Now()
		tok, err := IssueConfirmationToken(p)
		if err != nil {
			t.Fatalf("IssueConfirmationToken: %v", err)
		}
		// Wait on the clock, not on Verify: a successful Verify would record a
		// replay marker and the next call would say "replayed" before the TTL
		// ever elapsed, hiding the expiry arm this fixture exists to prove.
		time.Sleep(time.Until(issued.Add(2 * ttl)))
		assertTokenInvalid(t, VerifyConfirmationToken(tok, p), tokenReasonExpired)
	})

	// (c) The signing key is per-user and persistent, so a token minted by one
	// gum process verifies in the next: that is what makes a two-step CLI
	// confirm work at all. The durable replay marker, not a key mismatch, is
	// what stops the second process from reusing a spent token.
	t.Run("c_cross_process_replay", func(t *testing.T) {
		p := confirmationBindingParams(5 * time.Minute)
		p.ReplayStoreDir = t.TempDir()
		p.RequireDurableReplay = true
		tok, err := IssueConfirmationToken(p)
		if err != nil {
			t.Fatalf("IssueConfirmationToken: %v", err)
		}
		if err := VerifyConfirmationToken(tok, p); err != nil {
			t.Fatalf("first process: %v; want nil", err)
		}
		ResetReplayCacheForTest() // the second process starts with an empty heap
		assertTokenInvalid(t, VerifyConfirmationToken(tok, p), tokenReasonReplayed)
	})

	t.Run("d_cross_profile_replay", func(t *testing.T) {
		ResetReplayCacheForTest()
		p := confirmationBindingParams(5 * time.Minute)
		p.ProfileName = "profile-a"
		tok, err := IssueConfirmationToken(p)
		if err != nil {
			t.Fatalf("IssueConfirmationToken: %v", err)
		}
		other := p
		other.ProfileName = "profile-b"
		assertTokenInvalid(t, VerifyConfirmationToken(tok, other), tokenReasonMismatch)
	})

	t.Run("e_code_budget_increase", func(t *testing.T) {
		d := gumCodeBindingDispatcher("")
		base := elevatedCode()
		tok := mintCodeToken(t, d, base)
		raised := base
		raised.budget = 20
		assertCodeRebindRejected(t, d, tok, raised)
	})

	t.Run("e_code_scope_broadened", func(t *testing.T) {
		d := gumCodeBindingDispatcher("")
		base := elevatedCode()
		tok := mintCodeToken(t, d, base)
		wider := base
		wider.scope = []any{"drive.files.delete:a", "gmail.users.messages.delete"}
		assertCodeRebindRejected(t, d, tok, wider)
	})

	// The approved request was destructive-only. Adding allow_write hands the
	// sandbox a second capability gate (code_risor.go buildCallFn) that the
	// operator never saw, without changing the args or the purpose.
	t.Run("e_code_allow_write_added", func(t *testing.T) {
		d := gumCodeBindingDispatcher("")
		base := elevatedCode()
		tok := mintCodeToken(t, d, base)
		wider := base
		wider.allowWrite = true
		assertCodeRebindRejected(t, d, tok, wider)
	})

	// The mirror of the case above, and the reason the binding is scoped to the
	// gum.code purposes. On the destructive tier the same two flags only say
	// which tool the caller used, and policy.go grants allow_destructive
	// implicitly once a token is presented, so the confirming call legitimately
	// arrives with the flag cleared. Binding them there would refuse the
	// handshake the spec prescribes.
	t.Run("destructive_tier_ignores_capability_flags", func(t *testing.T) {
		ResetReplayCacheForTest()
		p := confirmationBindingParams(5 * time.Minute)
		p.AllowDestructive = true
		tok, err := IssueConfirmationToken(p)
		if err != nil {
			t.Fatalf("IssueConfirmationToken: %v", err)
		}
		confirming := p
		confirming.AllowDestructive = false
		if verr := VerifyConfirmationToken(tok, confirming); verr != nil {
			t.Fatalf("destructive confirm after the implicit grant was refused: %v", verr)
		}
	})

	t.Run("code_binds_language", func(t *testing.T) {
		d := gumCodeBindingDispatcher("")
		base := elevatedCode()
		tok := mintCodeToken(t, d, base)
		other := base
		other.language = "starlark"
		assertCodeRebindRejected(t, d, tok, other)
	})

	// Row 21: the submitted source is re-hashed at re-invocation, so a token
	// approved for one script cannot run a different one.
	t.Run("code_binds_source", func(t *testing.T) {
		d := gumCodeBindingDispatcher("")
		base := elevatedCode()
		tok := mintCodeToken(t, d, base)
		other := base
		other.source = `gum_call("drive.files.delete", {"fileId": "EVERYTHING"})`
		assertCodeRebindRejected(t, d, tok, other)
	})

	t.Run("code_exact_request_is_accepted", func(t *testing.T) {
		d := gumCodeBindingDispatcher("")
		base := elevatedCode()
		tok := mintCodeToken(t, d, base)
		if se := d.evaluatePolicy(context.Background(), base.invocation(true, tok)); se != nil {
			t.Fatalf("the approved request was refused on its own token: %v", se)
		}
	})

	t.Run("f_write_token_cannot_confirm_destructive", func(t *testing.T) {
		ResetReplayCacheForTest()
		p := confirmationBindingParams(5 * time.Minute)
		p.Purpose = ConfirmationPurposeWrite
		tok, err := IssueConfirmationToken(p)
		if err != nil {
			t.Fatalf("IssueConfirmationToken: %v", err)
		}
		destructive := p
		destructive.Purpose = ConfirmationPurposeDestructive
		assertTokenInvalid(t, VerifyConfirmationToken(tok, destructive), tokenReasonMismatch)
	})

	t.Run("f_write_token_cannot_confirm_another_variant", func(t *testing.T) {
		ResetReplayCacheForTest()
		p := confirmationBindingParams(5 * time.Minute)
		p.Purpose = ConfirmationPurposeWrite
		tok, err := IssueConfirmationToken(p)
		if err != nil {
			t.Fatalf("IssueConfirmationToken: %v", err)
		}
		other := p
		other.VariantID = "gmail.v1.rest.users.messages.modify"
		assertTokenInvalid(t, VerifyConfirmationToken(tok, other), tokenReasonMismatch)
	})
}

// countingCodeAdapter stands in for the Risor sandbox. Its only job is to
// record whether the script ever reached execution.
type countingCodeAdapter struct{ calls int }

func (c *countingCodeAdapter) Execute(_ context.Context, _ *Invocation, _ *ResolvedVariant, _ *Credentials) (*Response, error) {
	c.calls++
	return &Response{Body: []byte(`{"ok":true}`)}, nil
}

// TestConfirmationTokenSourceRehash is the docs/test-matrix.md row-21 proof.
// The dispatcher re-derives the args hash, and therefore the source hash, from
// the args submitted at re-invocation. A token minted for one script body is
// refused when a different body arrives under it, so the swap cannot happen
// between the operator's approval and the run.
//
// The counting adapter is the load-bearing part: the refusal has to land in
// step 2, before the sandbox sees the substituted script.
func TestConfirmationTokenSourceRehash(t *testing.T) {
	ResetReplayCacheForTest()
	adapter := &countingCodeAdapter{}
	d := gumCodeBindingDispatcher("")
	d.adapters = map[string]Adapter{"code.risor": adapter}

	approved := elevatedCode()
	tok := mintCodeToken(t, d, approved)

	swapped := approved
	swapped.source = `gum_call("drive.files.delete", {"fileId": "EVERYTHING"})`
	_, err := d.Dispatch(context.Background(), swapped.invocation(true, tok))
	assertTokenInvalid(t, err, tokenReasonMismatch)
	if adapter.calls != 0 {
		t.Fatalf("adapter calls = %d; want 0: the swapped script reached the sandbox", adapter.calls)
	}

	// Control: the approved body on the same token does reach the sandbox, so
	// the refusal above is the source swap and not a dead dispatch path.
	if _, err := d.Dispatch(context.Background(), approved.invocation(true, tok)); err != nil {
		t.Fatalf("approved body on its own token: %v", err)
	}
	if adapter.calls != 1 {
		t.Fatalf("adapter calls = %d; want 1", adapter.calls)
	}
}
