// Regression tests for the defects found in the 2026-09 whole-repo review.
// Each test fails against the pre-fix source and passes after the fix in the
// same commit. The comment above each test names the file and the defect.
package dispatch

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/output/profile"
)

// confirmation_token.go: the replay marker was keyed on the caller-supplied
// signature field, and hex.DecodeString accepts upper-case digits. Re-spelling
// the last field in upper case verified as the same signature but missed both
// the filesystem marker and the in-memory map, so one approval authorized an
// unbounded number of executions.
func TestConfirmationTokenReplayIgnoresHexCase(t *testing.T) {
	// The in-memory cache, not the durable store: a case-insensitive
	// filesystem (APFS by default) folds the two marker filenames together and
	// masks the defect, while the Go map that one-process embedders and tests
	// use does not.
	params := confirmationBindingParams(5 * time.Minute)

	tok, err := IssueConfirmationToken(params)
	if err != nil {
		t.Fatalf("IssueConfirmationToken: %v", err)
	}
	if err := VerifyConfirmationToken(tok, params); err != nil {
		t.Fatalf("first verify: %v; want nil", err)
	}

	cut := strings.LastIndex(tok, ".")
	upper := tok[:cut+1] + strings.ToUpper(tok[cut+1:])
	if upper == tok {
		t.Fatal("signature field has no lower-case hex digit to re-spell")
	}

	assertTokenInvalid(t, VerifyConfirmationToken(upper, params), "replayed")
}

// confirmation_token.go: computeBindingHash omitted caller and risk_class from
// the spec §6.1.2 binding tuple, so a token approved on one presentation
// surface replayed on the other, and a token approved at one risk class
// authorized a variant re-classified upward.
func TestConfirmationTokenBindsCallerAndRiskClass(t *testing.T) {
	base := confirmationBindingParams(5 * time.Minute)
	base.Caller = "mcp"
	base.RiskClass = "destructive"

	for _, tc := range []struct {
		name  string
		mutfn func(*ConfirmationParams)
	}{
		{"caller", func(p *ConfirmationParams) { p.Caller = "cli" }},
		{"risk_class", func(p *ConfirmationParams) { p.RiskClass = "write" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			issue := base
			issue.ReplayStoreDir = t.TempDir()
			tok, err := IssueConfirmationToken(issue)
			if err != nil {
				t.Fatalf("IssueConfirmationToken: %v", err)
			}

			verify := issue
			tc.mutfn(&verify)
			assertTokenInvalid(t, VerifyConfirmationToken(tok, verify), "mismatch")
		})
	}
}

// lifecycle.go: canonicalizeArgs hand-rolled its own serializer, so it violated
// spec §10.0 Rule 1 (null-valued keys are removed before serialization). A
// caller that spelled an absent optional as null got a different cache key,
// args_canonical and confirmation-token binding than the caller who omitted it.
func TestCanonicalizeArgsDropsNullsAtEveryDepth(t *testing.T) {
	withNulls := map[string]any{
		"userId": "me",
		"q":      nil,
		"filter": map[string]any{"labelIds": nil, "unread": true},
	}
	without := map[string]any{
		"userId": "me",
		"filter": map[string]any{"unread": true},
	}

	got, want := canonicalizeArgs(withNulls), canonicalizeArgs(without)
	if got != want {
		t.Errorf("null-valued keys changed the canonical form:\n got %s\nwant %s", got, want)
	}
	if strings.Contains(got, "null") {
		t.Errorf("canonical form still carries a null: %s", got)
	}
}

// lifecycle.go: canonicalizeArgs must emit RFC 8785 JCS, which sorts object
// keys by their UTF-16 code units and escapes strings the JSON way. Array
// elements keep their nulls: dropping one would renumber the array.
func TestCanonicalizeArgsIsJCS(t *testing.T) {
	got := canonicalizeArgs(map[string]any{
		"b": []any{1, nil, "x"},
		"a": true,
		"é": "e",
	})
	const want = `{"a":true,"b":[1,null,"x"],"é":"e"}`
	if got != want {
		t.Errorf("canonicalizeArgs =\n %s\nwant %s", got, want)
	}
	var round map[string]any
	if err := json.Unmarshal([]byte(got), &round); err != nil {
		t.Errorf("canonical form is not valid JSON: %v", err)
	}
}

// lifecycle.go: semanticFields concatenated Projection and KeepFields into one
// sorted list, so projection=[a] keep=[b] and projection=[b] keep=[a] produced
// the same cache key. The cache stores the upstream body masked to the
// projection, so a warm call under one profile was served the other's body.
func TestSemanticFieldsKeepsProjectionAndKeepDistinct(t *testing.T) {
	one := semanticFields(&Invocation{OutputProfile: &profile.Profile{
		Projection: []string{"a"},
		KeepFields: []string{"b"},
	}})
	two := semanticFields(&Invocation{OutputProfile: &profile.Profile{
		Projection: []string{"b"},
		KeepFields: []string{"a"},
	}})
	if one == two {
		t.Errorf("both profiles key to %q; projection and keep_fields must not collapse", one)
	}
}

// lifecycle.go: an adapter that returned (nil, nil) — the obvious early return
// for an empty result set — reached shapeResponse, which dereferenced
// resp.Format after executeAdapter's recover window had closed. That killed a
// long-running `gum mcp --stdio` session instead of failing the one call.
func TestExecuteAdapterRejectsNilResponseWithNilError(t *testing.T) {
	d := &dispatcher{adapters: map[string]Adapter{"nilnil": nilNilAdapter{}}}
	rv := &ResolvedVariant{AdapterKey: "nilnil", Variant: &catalog.Variant{}}

	resp, err := d.executeAdapter(t.Context(), &Invocation{OpID: "x"}, rv, nil)
	if resp != nil {
		t.Errorf("resp = %+v; want nil", resp)
	}
	if !IsStructuredError(err, ErrCodeServiceDown) {
		t.Fatalf("err = %v; want SERVICE_DOWN", err)
	}
}

// lifecycle.go: shapeResponse is reachable with a nil response from callers
// other than executeAdapter, so it carries its own guard.
func TestShapeResponseNilResponseIsStructured(t *testing.T) {
	d := &dispatcher{}
	_, err := d.shapeResponse(t.Context(), &Invocation{OpID: "x"}, nil, nil)
	if !IsStructuredError(err, ErrCodeServiceDown) {
		t.Fatalf("err = %v; want SERVICE_DOWN", err)
	}
}

// lifecycle.go: AnnotateResponse is adapter-owned code that runs after
// executeAdapter's recover has already returned, so a panic in it terminated
// the process. The annotation is additive by contract, so the upstream body is
// served instead.
func TestAnnotateResponseContainsAnnotatorPanic(t *testing.T) {
	body := []byte(`{"id":"1"}`)
	d := &dispatcher{adapters: map[string]Adapter{"boom": panicAnnotatingAdapter{}}}
	rv := &ResolvedVariant{AdapterKey: "boom", Variant: &catalog.Variant{}}

	got := d.annotateResponse(&Invocation{OpID: "x"}, rv, body)
	if string(got) != string(body) {
		t.Errorf("annotateResponse = %s; want the upstream body %s", got, body)
	}
}

type nilNilAdapter struct{}

func (nilNilAdapter) Execute(_ context.Context, _ *Invocation, _ *ResolvedVariant, _ *Credentials) (*Response, error) {
	return nil, nil
}

type panicAnnotatingAdapter struct{}

func (panicAnnotatingAdapter) Execute(_ context.Context, _ *Invocation, _ *ResolvedVariant, _ *Credentials) (*Response, error) {
	return &Response{Body: []byte(`{}`)}, nil
}

func (panicAnnotatingAdapter) AnnotateResponse(*Invocation, *ResolvedVariant, []byte) []byte {
	panic("annotator exploded")
}
