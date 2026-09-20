package cache

import (
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestBBoltHotLastAccessNoRace drives the lazy last-access refresh that
// TestBBoltConcurrentCloseNoPanic never reaches. That test seeds fresh entries,
// so `now - lastAccessUnix` stays under the 60-second threshold and Get never
// takes the write branch. Backdating the field makes every Get take it, so a
// read of hotEntry.lastAccessUnix outside the lock races the write inside it.
//
// Run under -race. Without the fix the detector reports a write at
// hotEntry.lastAccessUnix concurrent with the read in Get.
func TestBBoltHotLastAccessNoRace(t *testing.T) {
	c, err := Open(BBoltConfig{Path: filepath.Join(t.TempDir(), "c.db")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = c.Close() }()

	const key = "hot"
	if err := c.Set(key, []byte("payload"), time.Hour); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Backdate past the 60-second amortization window so every Get wants to
	// refresh the field.
	c.mu.Lock()
	he, ok := c.hot[key]
	if !ok {
		c.mu.Unlock()
		t.Fatal("Set did not populate the hot tier; this test needs the hot path")
	}
	he.lastAccessUnix = time.Now().Unix() - 3600
	c.mu.Unlock()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				if _, ok := c.Get(key); !ok {
					t.Error("Get missed a live hot entry")
					return
				}
			}
		}()
	}
	wg.Wait()
}
