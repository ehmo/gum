package dispatch

import (
	"testing"
	"time"
)

// TestReplayCacheSeenSweepsExpiredEntries pins the expired-entry eviction
// arm at the top of replayCache.seen (confirmation_token.go:254-255). The
// public VerifyConfirmationToken path rejects an expired token *before* it
// is ever recorded, so a stale entry never lands in the map via that route
// — leaving the `now.After(exp) → delete` sweep unexercised. Constructing
// the cache directly with a pre-expired entry, then calling seen with a
// fresh sig, forces the sweep to reclaim it.
func TestReplayCacheSeenSweepsExpiredEntries(t *testing.T) {
	rc := &replayCache{entries: map[string]time.Time{
		"stale": time.Now().Add(-time.Hour), // expired before this call
	}}

	if rc.seen("fresh", time.Now().Add(time.Hour)) {
		t.Fatal("fresh sig reported as already-seen on first call")
	}
	if _, ok := rc.entries["stale"]; ok {
		t.Error("expired entry was not swept by seen()")
	}
	if _, ok := rc.entries["fresh"]; !ok {
		t.Error("fresh sig was not recorded after sweep")
	}
}
