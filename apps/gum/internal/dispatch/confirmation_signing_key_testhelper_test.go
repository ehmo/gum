package dispatch

import "sync"

// resetSigningKeyForTest clears the cached signing key + sync.Once so the next
// getSigningKey() reloads from disk — simulating a fresh process.
func resetSigningKeyForTest() {
	confirmationSigningKey = [32]byte{}
	confirmationSigningKeyOnce = sync.Once{}
}
