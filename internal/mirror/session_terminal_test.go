package mirror

import (
	"context"
	"testing"
	"time"

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

func TestSessionGateReleasesLoneEscape(t *testing.T) {
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
	if err := writeSessionTerminal(context.Background(), int(master.Fd()), []byte("\x1b")); err != nil {
		t.Fatal(err)
	}
	// Offline Escape cannot keep readiness closed; it is discarded, not held.
	var gate sessionInputGate
	if opened, err := gate.open(fd); err != nil || opened {
		t.Fatalf("fresh prefix admitted readiness: %v %v", opened, err)
	}
	time.Sleep(sessionPrefixTimeout)
	if opened, err := gate.open(fd); err != nil || !opened || len(gate.prefix) != 0 {
		t.Fatalf("lone Escape blocked readiness: %v %v", opened, err)
	}
	// While ready, a lone Escape is released alone with the current origin.
	gate.mu.Lock()
	items := gate.decode([]byte("\x1b"))
	gate.mu.Unlock()
	if len(items) != 0 {
		t.Fatalf("escape released before timeout: %+v", items)
	}
	if early := gate.flushPrefix(time.Now()); early != nil {
		t.Fatalf("escape released early: %+v", early)
	}
	items = gate.flushPrefix(time.Now().Add(sessionPrefixTimeout))
	if len(items) != 1 || string(items[0].data) != "\x1b" || items[0].generation != gate.current() || items[0].paste {
		t.Fatalf("escape not released with its origin: %+v", items)
	}
}
