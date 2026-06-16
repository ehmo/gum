//go:build unix

package fsatomic

import (
	"os"
	"syscall"
)

// OpenNoFollow opens path read-only with O_NOFOLLOW, so a symlink swapped in
// for a file that a prior stat reported as regular is rejected at open time
// rather than silently followed. Closes install-time and key-load TOCTOU
// substitution vectors (review gum-t8x1).
func OpenNoFollow(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
}
