package plugins

import (
	"fmt"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

// openPTY returns the two ends of a fresh pseudo-terminal. Darwin has no
// posix_openpt wrapper in x/sys/unix, so the sequence is spelled out: open the
// multiplexer, grant and unlock the slave, then name it from the master's
// device minor, which the darwin pty driver keeps in step with /dev/ttysNNN.
// The master end is not a terminal on darwin, so only the slave answers the
// termios ioctls disableEcho issues. Every failure skips: a machine that hands
// out no pty is not a reason to fail the suite.
func openPTY(t *testing.T) (master, slave *os.File) {
	t.Helper()
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("open /dev/ptmx: %v", err)
	}
	t.Cleanup(func() { _ = m.Close() })

	fd := int(m.Fd())
	if err := unix.IoctlSetInt(fd, unix.TIOCPTYGRANT, 0); err != nil {
		t.Skipf("TIOCPTYGRANT: %v", err)
	}
	if err := unix.IoctlSetInt(fd, unix.TIOCPTYUNLK, 0); err != nil {
		t.Skipf("TIOCPTYUNLK: %v", err)
	}

	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		t.Skipf("fstat /dev/ptmx: %v", err)
	}

	name := fmt.Sprintf("/dev/ttys%03d", unix.Minor(uint64(st.Rdev)))
	s, err := os.OpenFile(name, os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("open %s: %v", name, err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return m, s
}
