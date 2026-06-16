package registry

import (
	"crypto/rand"
	"encoding/hex"
)

// newTxID returns the 8-character lowercase hex install_txid used in the
// example at spec §8.7 line 1732. Random bytes come from crypto/rand so
// concurrent installers in different processes don't collide.
func newTxID() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand.Read on POSIX never returns an error in practice;
		// surface as a panic so a broken /dev/urandom doesn't silently
		// produce zero-valued txids.
		panic("registry: crypto/rand.Read failed: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}
