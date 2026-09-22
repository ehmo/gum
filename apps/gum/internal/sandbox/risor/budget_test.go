package risor_test

// Spec §9.0.1: gum_parallel sizes its own encoding against what the §6.1
// cumulative budget has left. Run binds that counter to a caller-supplied
// Budget; these tests pin what the caller reads back.

import (
	"context"
	"testing"

	sandbox "github.com/ehmo/gum/internal/sandbox/risor"
)

func TestBudgetReportsTheLiveRemainder(t *testing.T) {
	budget := &sandbox.Budget{}
	var seen []int

	opts := sandbox.Options{
		Budget:           budget,
		OutputLimitBytes: 100,
		Globals: map[string]any{
			"probe": func(args ...any) any {
				n, ok := budget.Remaining()
				if !ok {
					t.Error("Remaining reported unbound inside a running sandbox")
				}
				seen = append(seen, n)
				return nil
			},
		},
	}

	_, err := sandbox.Run(context.Background(), `probe()
gum_print("0123456789")
probe()`, opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(seen) != 2 {
		t.Fatalf("probe ran %d times; want 2", len(seen))
	}
	if seen[0] != 100 {
		t.Errorf("remainder before any print = %d; want the whole 100-byte budget", seen[0])
	}
	if seen[1] != 90 {
		t.Errorf("remainder after a 10-byte print = %d; want 90", seen[1])
	}
}

func TestBudgetDefaultsToTheSandboxLimit(t *testing.T) {
	budget := &sandbox.Budget{}
	got := -1

	opts := sandbox.Options{
		Budget: budget,
		Globals: map[string]any{
			"probe": func(args ...any) any {
				got, _ = budget.Remaining()
				return nil
			},
		},
	}

	if _, err := sandbox.Run(context.Background(), `probe()`, opts); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got != sandbox.DefaultOutputLimitBytes {
		t.Errorf("remainder = %d; want the %d-byte default", got, sandbox.DefaultOutputLimitBytes)
	}
}

func TestUnboundBudgetReportsNotOK(t *testing.T) {
	var nilBudget *sandbox.Budget
	if n, ok := nilBudget.Remaining(); ok || n != 0 {
		t.Errorf("nil Budget reported (%d, %v); want (0, false)", n, ok)
	}

	fresh := &sandbox.Budget{}
	if n, ok := fresh.Remaining(); ok || n != 0 {
		t.Errorf("unbound Budget reported (%d, %v); want (0, false)", n, ok)
	}
}

func TestBudgetNeverReportsANegativeRemainder(t *testing.T) {
	budget := &sandbox.Budget{}
	got := -1

	// The per-call ceiling clamps a print to the budget, so the counter
	// bottoms out at zero rather than going under it.
	opts := sandbox.Options{
		Budget:           budget,
		OutputLimitBytes: 8,
		Globals: map[string]any{
			"probe": func(args ...any) any {
				got, _ = budget.Remaining()
				return nil
			},
		},
	}

	if _, err := sandbox.Run(context.Background(), `gum_print("0123456789abcdef")
probe()`, opts); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got != 0 {
		t.Errorf("remainder after an overrun = %d; want 0", got)
	}
}
