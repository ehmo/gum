package plugins

import "golang.org/x/sys/unix"

// The termios get/set ioctl pair on Darwin. See terminal_echo_linux.go.
const (
	ioctlReadTermios  = unix.TIOCGETA
	ioctlWriteTermios = unix.TIOCSETA
)
