package plugins

import "golang.org/x/sys/unix"

// The termios get/set ioctl pair on Linux. Darwin spells the same pair
// TIOCGETA/TIOCSETA, which is the only platform difference in disableEcho.
const (
	ioctlReadTermios  = unix.TCGETS
	ioctlWriteTermios = unix.TCSETS
)
