package registry

import "syscall"

// errENOTSUP and errEINVAL are aliased into errors.Is wrappers so the
// fsync fallback in isFsyncUnsupported can match syscall errno values without
// pulling syscall into every transaction.go consumer.
var (
	errENOTSUP error = syscall.ENOTSUP
	errEINVAL  error = syscall.EINVAL
)
