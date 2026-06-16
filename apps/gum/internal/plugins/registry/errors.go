package registry

import "errors"

// ErrLockTimeout is returned by WriteTransaction when the 30s flock budget
// elapses before plugins.install.lock can be acquired. Spec §8.7 step 1.
var ErrLockTimeout = errors.New("registry: plugins.install.lock timeout")
