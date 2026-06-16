//go:build !windows

package registry

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
)

// acquireFileLock takes an exclusive advisory lock on lockPath (creating it
// mode-600 if absent) with the given timeout. The returned release function
// drops the lock and closes the descriptor.
//
// Implementation: syscall.Flock(LOCK_EX|LOCK_NB) in a poll loop. Spec §8.7
// step 1 mandates a 30s ceiling, but the deadline is parameterised so tests
// can use a short value.
func acquireFileLock(lockPath string, timeout time.Duration) (release func() error, err error) {
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("registry: open install lock %s: %w", lockPath, err)
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
			return nil, fmt.Errorf("registry: flock %s: %w", lockPath, err)
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, fmt.Errorf("registry: flock %s: timeout after %s: %w", lockPath, timeout, ErrLockTimeout)
		}
		time.Sleep(25 * time.Millisecond)
	}
}
