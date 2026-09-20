package auth

import (
	"context"
	"sync"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
)

// TestResolveAuthGumOAuthConcurrentIsRaceFree pins gum-n3x2: one
// CompositeResolver is shared by the dispatcher across concurrent invocations
// (gum_parallel, the MCP server), so the gum_oauth arm must not write
// c.GumOAuth on the read path. Run under -race, the old lazy assignment was a
// data race between two goroutines resolving the same strategy.
func TestResolveAuthGumOAuthConcurrentIsRaceFree(t *testing.T) {
	c := &CompositeResolver{} // GumOAuth deliberately unwired
	rv := &dispatch.ResolvedVariant{
		Variant: &catalog.Variant{AuthStrategy: catalog.AuthStrategyGUMOAuth},
	}

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// The manifest gate rejects every unpromoted scope set, so this
			// returns a typed error without touching the keychain or network.
			_, _ = c.ResolveAuth(context.Background(), &dispatch.Invocation{}, rv)
		}()
	}
	wg.Wait()

	if c.GumOAuth != nil {
		t.Errorf("GumOAuth=%T after resolving; the read path must not populate the field", c.GumOAuth)
	}
}
