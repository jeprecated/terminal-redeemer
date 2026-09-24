package mirror

import (
	"context"
	"testing"

	"github.com/charmbracelet/x/term"
	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

func TestSessionGateDoesNotFlushUnreadPasteStart(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()
	fd := int(slave.Fd())
	if _, err := term.MakeRaw(uintptr(fd)); err != nil {
		t.Fatal(err)
	}
	if err := writeSessionTerminal(context.Background(), int(master.Fd()), []byte(pasteStart+"queued-offline")); err != nil {
		t.Fatal(err)
	}
	// No input reader has consumed the paste start yet. Readiness must see it
	// while draining, NOT blindly flush it away and admit the later paste tail.
	var gate sessionInputGate
	opened, err := gate.open(fd)
	if err != nil || opened || !gate.paste {
		t.Fatalf("unread paste lost its origin: opened=%v paste=%v err=%v", opened, gate.paste, err)
	}
	if err := writeSessionTerminal(context.Background(), int(master.Fd()), []byte("tail\r\n"+pasteEnd)); err != nil {
		t.Fatal(err)
	}
	opened, err = gate.open(fd)
	if err != nil || !opened || gate.current() == 0 {
		t.Fatalf("finished discard did not open: %v %v", opened, err)
	}
	var b [64]byte
	if n, err := unix.Read(fd, b[:]); n > 0 || err != unix.EAGAIN {
		t.Fatalf("backlog survived: %q %v", b[:max(n, 0)], err)
	}
	gate.close()
	if opened, err := gate.open(fd); err != nil || !opened || gate.current() != 2 {
		t.Fatalf("generation reused: %v %v %d", opened, err, gate.current())
	}
}
