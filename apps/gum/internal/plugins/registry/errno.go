package registry

import (
	"errors"
	"syscall"
)

// errENOTSUP and errEINVAL are aliased into errors.Is wrappers so the
// fsync fallback in isFsyncUnsupported can match syscall errno values without
// pulling syscall into every transaction.go consumer.
var (
	errENOTSUP error = syscall.ENOTSUP
	errEINVAL  error = syscall.EINVAL
)

// errnoName renders the syscall errno behind err for the §8.7
// `fsync_not_supported` warning. Errors carrying no errno report "unknown".
func errnoName(err error) string {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return "unknown"
	}
	switch errno {
	case syscall.EINVAL:
		return "EINVAL"
	case syscall.ENOTSUP:
		return "ENOTSUP"
	case syscall.ENOSYS:
		return "ENOSYS"
	}
	return errno.Error()
}
