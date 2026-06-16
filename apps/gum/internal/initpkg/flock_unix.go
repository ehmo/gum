//go:build !windows

package initpkg

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
)

// acquireSettingsLock takes an exclusive advisory flock on lockPath with the
// given timeout. Mirrors internal/plugins/registry/flock_unix.go so the
// settings.json patch is serialised against concurrent `gum init` and
// `gum plugin install` invocations (spec §12.2 normative paragraph).
func acquireSettingsLock(lockPath string, timeout time.Duration) (release func() error, err error) {
	if err := os.MkdirAll(parentDir(lockPath), 0o755); err != nil {
		return nil, fmt.Errorf("initpkg: mkdir for lock %s: %w", lockPath, err)
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("initpkg: open settings lock %s: %w", lockPath, err)
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
			return nil, fmt.Errorf("initpkg: flock %s: %w", lockPath, err)
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, fmt.Errorf("initpkg: flock %s: timeout after %s", lockPath, timeout)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func parentDir(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[:i]
		}
	}
	return "."
}
