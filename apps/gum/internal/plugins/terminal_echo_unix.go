//go:build linux || darwin

package plugins

import (
	"os"

	"golang.org/x/sys/unix"
)

// disableEcho turns terminal echo off on f and returns the call that restores
// the previous terminal state. It returns an error when f is not a terminal,
// which is the normal case for `gum plugin setup < secrets.txt`; the caller
// then reads the stream unchanged.
func disableEcho(f *os.File) (func(), error) {
	fd := int(f.Fd())
	prev, err := unix.IoctlGetTermios(fd, ioctlReadTermios)
	if err != nil {
		return nil, err
	}
	quiet := quietTermios(*prev)
	if err := unix.IoctlSetTermios(fd, ioctlWriteTermios, &quiet); err != nil {
		return nil, err
	}
	return func() { _ = unix.IoctlSetTermios(fd, ioctlWriteTermios, prev) }, nil
}

// quietTermios is the terminal state a secret prompt needs and nothing more:
// echo off, canonical line editing and signals left on so Ctrl-C still reaches
// the process, and CR mapped to NL so a terminal that sends CR ends the line.
// Every other flag is carried over from prev, because the caller restores prev
// and must not have to undo edits this function never intended.
func quietTermios(prev unix.Termios) unix.Termios {
	quiet := prev
	quiet.Lflag &^= unix.ECHO
	quiet.Lflag |= unix.ICANON | unix.ISIG
	quiet.Iflag |= unix.ICRNL
	return quiet
}
