//go:build !unix

package fsatomic

import "os"

// OpenNoFollow falls back to a plain open on unsupported non-Unix platforms.
// Release targets use the Unix implementation with O_NOFOLLOW.
func OpenNoFollow(path string) (*os.File, error) {
	return os.Open(path)
}
