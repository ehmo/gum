package auth_test

import (
	"errors"
	"testing"

	"github.com/ehmo/gum/internal/auth"
	"github.com/ehmo/gum/internal/catalog"
)

// TestResolvePluginManagedMapping pins strategyFromCatalog's
// `catalog.AuthStrategyPluginManaged → StrategyPluginManaged, nil` arm
// (strategy.go:231-232). All other catalog→Strategy mappings are
// exercised by existing tests; this one fills the last enum cell.
func TestResolvePluginManagedMapping(t *testing.T) {
	t.Parallel()
	v := &catalog.Variant{AuthStrategy: catalog.AuthStrategyPluginManaged}
	got, err := auth.Resolve(t.Context(), v)
	if err != nil {
		t.Fatalf("Resolve(plugin_managed) err=%v; want nil", err)
	}
	if got != auth.StrategyPluginManaged {
		t.Errorf("Resolve(plugin_managed) = %v; want StrategyPluginManaged", got)
	}
}

// TestAcquireUnknownStrategyReturnsErrUnknown pins Acquire's
// `default → return 0, ErrUnknownStrategy` arm (strategy.go:290-291).
// Passing an out-of-range Strategy value (Strategy(255), beyond every
// defined constant) MUST surface ErrUnknownStrategy rather than panic
// or default-case any of the existing AUTH_* error codes.
func TestAcquireUnknownStrategyReturnsErrUnknown(t *testing.T) {
	t.Parallel()
	_, err := auth.Acquire(t.Context(), auth.Strategy(255), nil)
	if err == nil {
		t.Fatal("Acquire(Strategy(255)) err=nil; want ErrUnknownStrategy")
	}
	if !errors.Is(err, auth.ErrUnknownStrategy) {
		t.Errorf("err=%v; want errors.Is(err, ErrUnknownStrategy)", err)
	}
}
