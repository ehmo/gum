package cache

import (
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestBBoltConcurrentCloseNoPanic exercises the use-after-close guard
// (review gum-8aqm): concurrent Get/Set racing Close must never panic on a
// closed bbolt handle. Run with -race for full coverage.
func TestBBoltConcurrentCloseNoPanic(t *testing.T) {
	c, err := Open(BBoltConfig{Path: filepath.Join(t.TempDir(), "c.db")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Seed an entry so the bbolt read path (not just hot tier) is exercised.
	if err := c.Set("seed", []byte("v"), time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				key := "k" + string(rune('a'+n))
				_ = c.Set(key, []byte("payload"), time.Minute)
				_, _ = c.Get(key)
				_, _ = c.Get("seed")
			}
		}(i)
	}

	// Close while the workers are mid-flight.
	go func() {
		time.Sleep(time.Millisecond)
		_ = c.Close()
	}()

	wg.Wait()

	// Post-close calls must be safe no-ops, not panics.
	if _, ok := c.Get("seed"); ok {
		t.Error("Get after Close returned a hit; want miss")
	}
	if err := c.Set("after", []byte("x"), time.Minute); err == nil {
		t.Error("Set after Close returned nil error; want a write error")
	}
}
