// Package dispatch — spec §9.1 _expression envelope regression tests.
//
// Defect: no production path ever built the ExpressionMeta envelope for a
// single-op result. shapeResponse set StructuredContent to the raw pre-shaping
// body and emitted no envelope, so result_count, omitted_count, lossy and
// on_empty_message never reached a caller. docs/spec.md §9.1 requires the
// envelope on every shaped result and §13 requires structuredContent to
// carry the shaped value, not the upstream one.
package dispatch

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/ehmo/gum/internal/output/profile"
)

// exprFixture builds a dispatcher whose single adapter returns body verbatim.
func exprFixture(t *testing.T, body string) (string, Dispatcher) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "default")
	adapter := &funcAdapter{
		execute: func(_ context.Context, _ *Invocation, _ *ResolvedVariant, _ *Credentials) (*Response, error) {
			return &Response{Body: []byte(body), Format: "json", StatusCode: 200}, nil
		},
	}
	d := NewDispatcherWithConfig(minimalCatalog("stub"), map[string]Adapter{"stub": adapter}, DispatcherConfig{
		Tee: TeeConfig{ProfileDir: dir, RetentionHours: 24},
	})
	return dir, d
}

func dispatchShaped(t *testing.T, d Dispatcher, prof *profile.Profile, format string) *ShapedResponse {
	t.Helper()
	shaped, err := d.Dispatch(context.Background(), &Invocation{
		OpID:                   "gum.code",
		Format:                 format,
		RequestID:              "expr-meta",
		AuthSubjectFingerprint: "fp-test",
		OutputProfile:          prof,
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if shaped == nil {
		t.Fatal("Dispatch returned nil ShapedResponse")
	}
	return shaped
}

// TestExpressionEnvelopeReportsShapedCounts is the §9.1 acceptance: a lossy
// profile must disclose what it kept and what it dropped.
func TestExpressionEnvelopeReportsShapedCounts(t *testing.T) {
	t.Parallel()
	_, d := exprFixture(t, `{"messages":[{"id":"m1"},{"id":"m2"},{"id":"m3"}]}`)
	prof := &profile.Profile{
		Name:           "gmail.messages.list.v1",
		CollapseArrays: &profile.CollapseArraysSpec{MaxItems: 2},
		Recovery:       "local_artifact",
	}
	shaped := dispatchShaped(t, d, prof, "json")

	if shaped.Expression == nil {
		t.Fatal("ShapedResponse.Expression is nil; want the §9.1 envelope on every shaped result")
	}
	e := shaped.Expression
	if e.Profile != "gmail.messages.list.v1" {
		t.Errorf("Expression.Profile = %q; want %q", e.Profile, "gmail.messages.list.v1")
	}
	if e.OpID != "gum.code" {
		t.Errorf("Expression.OpID = %q; want %q", e.OpID, "gum.code")
	}
	if e.VariantID == nil || *e.VariantID != "gum.code.v1.test" {
		t.Errorf("Expression.VariantID = %v; want %q", e.VariantID, "gum.code.v1.test")
	}
	if !e.Lossy {
		t.Error("Expression.Lossy = false; want true (collapse_arrays is a lossy stage)")
	}
	if e.ResultCount != 2 {
		t.Errorf("Expression.ResultCount = %d; want 2 (rows kept after collapse)", e.ResultCount)
	}
	if e.OmittedCount != 1 {
		t.Errorf("Expression.OmittedCount = %d; want 1 (row dropped by collapse)", e.OmittedCount)
	}
	if e.OnEmptyMessage != nil {
		t.Errorf("Expression.OnEmptyMessage = %v; want nil (result set is not empty)", *e.OnEmptyMessage)
	}
	if e.FullResultPath != shaped.FullResultPath {
		t.Errorf("Expression.FullResultPath = %q; want the tee path %q", e.FullResultPath, shaped.FullResultPath)
	}
}

// TestStructuredContentCarriesShapedValue pins §13: structuredContent is the
// shaped value. It carried the raw upstream body, so an MCP client reading
// structuredContent saw every field the profile had removed.
func TestStructuredContentCarriesShapedValue(t *testing.T) {
	t.Parallel()
	_, d := exprFixture(t, `{"messages":[{"id":"m1","raw":"SECRET"}]}`)
	prof := &profile.Profile{Name: "p", DropFields: []string{"messages.raw"}}
	shaped := dispatchShaped(t, d, prof, "json")

	b, err := json.Marshal(shaped.StructuredContent)
	if err != nil {
		t.Fatalf("marshal StructuredContent: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("StructuredContent is not a JSON object: %v", err)
	}
	rows, _ := got["messages"].([]any)
	if len(rows) != 1 {
		t.Fatalf("StructuredContent.messages has %d rows; want 1: %s", len(rows), b)
	}
	row, _ := rows[0].(map[string]any)
	if _, present := row["raw"]; present {
		t.Errorf("StructuredContent kept the dropped field \"raw\"; want the shaped value: %s", b)
	}
}

// TestExpressionEnvelopeCarriesOnEmptyMessage pins §9.1 rule 2. on_empty used
// to replace the whole body with its string, which corrupted the payload and
// left on_empty_message unemitted.
func TestExpressionEnvelopeCarriesOnEmptyMessage(t *testing.T) {
	t.Parallel()
	_, d := exprFixture(t, `{"messages":[]}`)
	prof := &profile.Profile{Name: "p", OnEmpty: "No messages matched the filter."}
	shaped := dispatchShaped(t, d, prof, "json")

	if shaped.Expression == nil {
		t.Fatal("ShapedResponse.Expression is nil")
	}
	e := shaped.Expression
	if e.OnEmptyMessage == nil {
		t.Fatal("Expression.OnEmptyMessage is nil; want the profile's on_empty string")
	}
	if *e.OnEmptyMessage != "No messages matched the filter." {
		t.Errorf("Expression.OnEmptyMessage = %q; want %q", *e.OnEmptyMessage, "No messages matched the filter.")
	}
	if e.ResultCount != 0 {
		t.Errorf("Expression.ResultCount = %d; want 0", e.ResultCount)
	}
	var body map[string]any
	if err := json.Unmarshal(shaped.Body, &body); err != nil {
		t.Fatalf("shaped body is not a JSON object (%s): %v", shaped.Body, err)
	}
	if _, ok := body["messages"]; !ok {
		t.Errorf("shaped body lost its record key; want {\"messages\":[]}, got %s", shaped.Body)
	}
}

// TestExpressionEnvelopeFlagsIntentionalZero pins §9.1 discriminator 5 and its
// co-occurrence invariant: max_items=0 emits the flag together with a non-null
// on_empty_message.
func TestExpressionEnvelopeFlagsIntentionalZero(t *testing.T) {
	t.Parallel()
	_, d := exprFixture(t, `{"messages":[{"id":"m1"},{"id":"m2"}]}`)
	prof := &profile.Profile{
		Name:           "p",
		CollapseArrays: &profile.CollapseArraysSpec{MaxItems: 0},
		OnEmpty:        "Counts only; rows suppressed by profile.",
	}
	shaped := dispatchShaped(t, d, prof, "json")

	e := shaped.Expression
	if e == nil {
		t.Fatal("ShapedResponse.Expression is nil")
	}
	if e.IntentionalZeroMaxItems == nil || !*e.IntentionalZeroMaxItems {
		t.Errorf("Expression.IntentionalZeroMaxItems = %v; want true", e.IntentionalZeroMaxItems)
	}
	if e.OnEmptyMessage == nil {
		t.Error("Expression.OnEmptyMessage is nil; the flag must co-occur with a message (§13 invariant)")
	}
	if e.ResultCount != 0 || e.OmittedCount != 2 {
		t.Errorf("counts = (%d,%d); want (0,2)", e.ResultCount, e.OmittedCount)
	}
}

// TestExpressionEnvelopeOnRawIsSentinel pins §13: a raw pass-through reports
// profile "_raw" and MUST NOT claim lossy.
func TestExpressionEnvelopeOnRawIsSentinel(t *testing.T) {
	t.Parallel()
	_, d := exprFixture(t, `{"messages":[{"id":"m1"}]}`)
	shaped := dispatchShaped(t, d, &profile.Profile{Name: "p", StripNulls: true}, "raw")

	if shaped.Expression == nil {
		t.Fatal("ShapedResponse.Expression is nil for a raw pass-through")
	}
	if shaped.Expression.Profile != "_raw" {
		t.Errorf("Expression.Profile = %q; want %q", shaped.Expression.Profile, "_raw")
	}
	if shaped.Expression.Lossy {
		t.Error("Expression.Lossy = true on a raw pass-through; no shaping occurred")
	}
}
