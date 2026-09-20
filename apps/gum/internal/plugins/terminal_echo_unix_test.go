//go:build linux || darwin

package plugins

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// TestQuietTermiosClearsEchoOnly proves the state a secret prompt installs.
// ECHO must go off, canonical mode and signals must stay on so Ctrl-C still
// reaches the process, CR must map to NL, and every unrelated flag must survive
// unchanged: the caller restores the saved state and must not have to undo an
// edit this transform never meant to make.
func TestQuietTermiosClearsEchoOnly(t *testing.T) {
	t.Parallel()
	var prev unix.Termios
	prev.Lflag = unix.ECHO | unix.ECHOE | unix.ISIG | unix.ICANON | unix.IEXTEN
	prev.Iflag = unix.IXON | unix.BRKINT
	prev.Oflag = unix.OPOST
	prev.Cflag = unix.CREAD

	got := quietTermios(prev)

	if got.Lflag&unix.ECHO != 0 {
		t.Error("ECHO still set; the secret would reach the screen")
	}
	// Termios flag fields are uint32 on Linux and uint64 on Darwin, so the
	// kept flags are listed as closures rather than as a map of widths.
	kept := []struct {
		name string
		set  func() bool
	}{
		{"ICANON", func() bool { return got.Lflag&unix.ICANON != 0 }},
		{"ISIG", func() bool { return got.Lflag&unix.ISIG != 0 }},
		{"IEXTEN", func() bool { return got.Lflag&unix.IEXTEN != 0 }},
		{"ECHOE", func() bool { return got.Lflag&unix.ECHOE != 0 }},
	}
	for _, k := range kept {
		if !k.set() {
			t.Errorf("%s cleared; quietTermios must touch ECHO only", k.name)
		}
	}
	if got.Iflag&unix.ICRNL == 0 {
		t.Error("ICRNL not set; a terminal that sends CR would never end the line")
	}
	if got.Iflag&unix.IXON == 0 || got.Iflag&unix.BRKINT == 0 {
		t.Errorf("Iflag=%x dropped a flag it carried in", got.Iflag)
	}
	if got.Oflag != prev.Oflag || got.Cflag != prev.Cflag {
		t.Errorf("Oflag/Cflag changed: %x/%x want %x/%x", got.Oflag, got.Cflag, prev.Oflag, prev.Cflag)
	}
}

// TestDisableEchoRejectsNonTerminal proves the fallback that keeps
// `gum plugin setup < secrets.txt` working: a regular file is not a terminal,
// disableEcho says so, and the caller reads the stream unchanged.
func TestDisableEchoRejectsNonTerminal(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "secrets")
	if err := os.WriteFile(path, []byte("s3cret\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = f.Close() }()

	restore, err := disableEcho(f)
	if err == nil {
		restore()
		t.Fatal("disableEcho(regular file) err=nil; want ENOTTY")
	}

	line, echoed, err := newSecretReader(f).readLine()
	if err != nil {
		t.Fatalf("readLine: %v", err)
	}
	if line != "s3cret" {
		t.Errorf("line=%q; want s3cret", line)
	}
	if !echoed {
		t.Error("echoed=false for a file; the caller would print a stray newline")
	}
}
