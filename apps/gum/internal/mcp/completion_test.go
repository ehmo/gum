package mcp_test

import (
	"sort"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/help/topics"
)

// TestMCPCompletions is the bead-named acceptance for gum-vok: completion/
// complete MUST be wired and the help-topic source MUST return the embedded
// topic names filtered by case-insensitive prefix.
func TestMCPCompletions(t *testing.T) {
	defer goleak.VerifyNone(t)

	ctx, cs, _, cleanup := connectResourceClient(t)
	defer cleanup()

	// Empty prefix → all eight topics in sorted order.
	all := topics.Names()
	sort.Strings(all)
	res, err := cs.Complete(ctx, &sdkmcp.CompleteParams{
		Ref:      &sdkmcp.CompleteReference{Type: "ref/resource", URI: "gum://help/{topic}"},
		Argument: sdkmcp.CompleteParamsArgument{Name: "topic", Value: ""},
	})
	if err != nil {
		t.Fatalf("Complete(empty): %v", err)
	}
	if !equalStringSlices(res.Completion.Values, all) {
		t.Errorf("Complete(empty).Values = %v; want %v", res.Completion.Values, all)
	}
	if res.Completion.Total != len(all) {
		t.Errorf("Complete(empty).Total = %d; want %d", res.Completion.Total, len(all))
	}
	if res.Completion.HasMore {
		t.Errorf("Complete(empty).HasMore = true; embedded roster is below the 50-value cap")
	}

	// Prefix "a" → only "auth" (the embedded topic set has exactly one match).
	res, err = cs.Complete(ctx, &sdkmcp.CompleteParams{
		Ref:      &sdkmcp.CompleteReference{Type: "ref/resource", URI: "gum://help/{topic}"},
		Argument: sdkmcp.CompleteParamsArgument{Name: "topic", Value: "a"},
	})
	if err != nil {
		t.Fatalf("Complete(prefix=a): %v", err)
	}
	want := []string{"auth"}
	if !equalStringSlices(res.Completion.Values, want) {
		t.Errorf("Complete(prefix=a).Values = %v; want %v", res.Completion.Values, want)
	}

	// Case-insensitive: "AU" still matches "auth".
	res, err = cs.Complete(ctx, &sdkmcp.CompleteParams{
		Ref:      &sdkmcp.CompleteReference{Type: "ref/resource", URI: "gum://help/{topic}"},
		Argument: sdkmcp.CompleteParamsArgument{Name: "topic", Value: "AU"},
	})
	if err != nil {
		t.Fatalf("Complete(prefix=AU): %v", err)
	}
	if !equalStringSlices(res.Completion.Values, want) {
		t.Errorf("Complete(prefix=AU).Values = %v; want %v", res.Completion.Values, want)
	}

	// Unmatched prefix → empty Values, never nil.
	res, err = cs.Complete(ctx, &sdkmcp.CompleteParams{
		Ref:      &sdkmcp.CompleteReference{Type: "ref/resource", URI: "gum://help/{topic}"},
		Argument: sdkmcp.CompleteParamsArgument{Name: "topic", Value: "zzzz"},
	})
	if err != nil {
		t.Fatalf("Complete(prefix=zzzz): %v", err)
	}
	if res.Completion.Values == nil {
		t.Error("Complete(no-match).Values = nil; want non-nil empty slice (clients must never see JSON null)")
	}
	if len(res.Completion.Values) != 0 {
		t.Errorf("Complete(no-match).Values = %v; want empty", res.Completion.Values)
	}

	// Unknown resource template → empty, no error.
	res, err = cs.Complete(ctx, &sdkmcp.CompleteParams{
		Ref:      &sdkmcp.CompleteReference{Type: "ref/resource", URI: "gum://schema/{ref}"},
		Argument: sdkmcp.CompleteParamsArgument{Name: "ref", Value: "anything"},
	})
	if err != nil {
		t.Fatalf("Complete(unknown-template): %v", err)
	}
	if len(res.Completion.Values) != 0 {
		t.Errorf("Complete(unknown-template).Values = %v; want empty (v0.2.0 will wire schema completions)", res.Completion.Values)
	}

	// ref/prompt → always empty for the v0.1.0 zero-argument roster.
	res, err = cs.Complete(ctx, &sdkmcp.CompleteParams{
		Ref:      &sdkmcp.CompleteReference{Type: "ref/prompt", Name: "gum.summarize_workspace_for_today"},
		Argument: sdkmcp.CompleteParamsArgument{Name: "anything", Value: "x"},
	})
	if err != nil {
		t.Fatalf("Complete(ref/prompt): %v", err)
	}
	if len(res.Completion.Values) != 0 {
		t.Errorf("Complete(ref/prompt).Values = %v; want empty (zero-argument roster)", res.Completion.Values)
	}
}

// TestMCPCompletionLatency is the bead-named latency floor for gum-vok.
// Spec §13 budgets P95=100ms / P99=250ms for completion/complete. The
// in-process transport eliminates network jitter, so a 100-sample sweep
// against the embedded help-topic source is a faithful floor. The cross-
// process latency canary (gum-tsu) tightens this once the bench harness
// lands.
func TestMCPCompletionLatency(t *testing.T) {
	defer goleak.VerifyNone(t)

	ctx, cs, _, cleanup := connectResourceClient(t)
	defer cleanup()

	const samples = 100
	durations := make([]time.Duration, 0, samples)
	for i := 0; i < samples; i++ {
		start := time.Now()
		_, err := cs.Complete(ctx, &sdkmcp.CompleteParams{
			Ref:      &sdkmcp.CompleteReference{Type: "ref/resource", URI: "gum://help/{topic}"},
			Argument: sdkmcp.CompleteParamsArgument{Name: "topic", Value: "a"},
		})
		if err != nil {
			t.Fatalf("Complete sample %d: %v", i, err)
		}
		durations = append(durations, time.Since(start))
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p95 := durations[(samples*95)/100]
	p99 := durations[(samples*99)/100]
	if p95 > 100*time.Millisecond {
		t.Errorf("P95 latency = %s; spec §13 budget is 100ms", p95)
	}
	if p99 > 250*time.Millisecond {
		t.Errorf("P99 latency = %s; spec §13 budget is 250ms", p99)
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
