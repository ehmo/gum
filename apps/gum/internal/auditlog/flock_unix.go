//go:build !windows

package auditlog

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
)

// acquireRotationLock takes an exclusive advisory lock on lockPath (creating
// it mode-600 if absent) with the given timeout. The returned release closes
// the descriptor. Spec §11 "advisory file lock" paragraph.
//
// On timeout the call returns errLockTimeout so callers can distinguish
// "another process is rotating" from a hard error.
func acquireRotationLock(lockPath string, timeout time.Duration) (release func() error, err error) {
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("auditlog: open rotation lock %s: %w", lockPath, err)
	}
	deadline := time.Now().Add(timeout)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() error {
				_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
				return f.Close()
			}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			_ = f.Close()
			return nil, fmt.Errorf("auditlog: flock %s: %w", lockPath, err)
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, errLockTimeout
		}
		time.Sleep(25 * time.Millisecond)
	}
}
