//go:build linux || darwin

package plugins

import (
	"bytes"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// TestDisableEchoOnRealTerminal exercises the ioctl path against a live
// pseudo-terminal: echo is on, disableEcho clears it, and the returned call
// puts the terminal back exactly as it was.
func TestDisableEchoOnRealTerminal(t *testing.T) {
	_, slave := openPTY(t)
	fd := int(slave.Fd())

	before, err := unix.IoctlGetTermios(fd, ioctlReadTermios)
	if err != nil {
		t.Fatalf("read termios: %v", err)
	}
	if before.Lflag&unix.ECHO == 0 {
		t.Fatal("fresh pty has ECHO off; the test proves nothing")
	}

	restore, err := disableEcho(slave)
	if err != nil {
		t.Fatalf("disableEcho: %v", err)
	}

	during, err := unix.IoctlGetTermios(fd, ioctlReadTermios)
	if err != nil {
		t.Fatalf("read termios while quiet: %v", err)
	}
	if during.Lflag&unix.ECHO != 0 {
		t.Error("ECHO still set; the typed secret would reach the screen")
	}
	if during.Lflag&unix.ICANON == 0 {
		t.Error("ICANON cleared; the prompt would not read a line")
	}

	restore()

	after, err := unix.IoctlGetTermios(fd, ioctlReadTermios)
	if err != nil {
		t.Fatalf("read termios after restore: %v", err)
	}
	if after.Lflag != before.Lflag || after.Iflag != before.Iflag {
		t.Errorf("restore left lflag=%#x iflag=%#x; want %#x %#x", after.Lflag, after.Iflag, before.Lflag, before.Iflag)
	}
}

// TestSecretReaderOnTerminalSuppressesEcho proves the caller contract on a real
// terminal: readLine reports echoed=false, so `gum plugin setup` knows it owes
// the newline the terminal did not print.
func TestSecretReaderOnTerminalSuppressesEcho(t *testing.T) {
	master, slave := openPTY(t)
	if _, err := master.WriteString("hunter2\n"); err != nil {
		t.Fatalf("write to master: %v", err)
	}

	line, echoed, err := newSecretReader(slave).readLine()
	if err != nil {
		t.Fatalf("readLine: %v", err)
	}
	if line != "hunter2" {
		t.Errorf("line=%q; want hunter2", line)
	}
	if echoed {
		t.Error("echoed=true on a terminal; the secret reached the scrollback")
	}
}

// TestPromptAndStoreClosesLineWhenEchoOff covers the newline the prompt owes
// the terminal. With echo off the user's Enter never reaches the screen, so
// without this the second prompt would land on top of the first.
func TestPromptAndStoreClosesLineWhenEchoOff(t *testing.T) {
	master, slave := openPTY(t)
	if _, err := master.WriteString("s3cret\n"); err != nil {
		t.Fatalf("write to master: %v", err)
	}

	var out bytes.Buffer
	kr := newFakeKeyring()
	opts := SetupOptions{Profile: "prof", Keyring: kr, Out: &out}
	d := CredentialDescriptor{Alias: "session", Env: "PLUG_SESSION", Kind: "session", DisplayName: "Session"}
	if err := promptAndStore(opts, newSecretReader(slave), "p", d); err != nil {
		t.Fatalf("promptAndStore: %v", err)
	}

	if got := out.String(); !strings.HasSuffix(got, "\n") {
		t.Errorf("prompt output %q does not end the line", got)
	}
	if got := kr.store[PluginCredentialKey("prof", "p", "session")]; got != "s3cret" {
		t.Errorf("keychain=%q; want s3cret", got)
	}
}
