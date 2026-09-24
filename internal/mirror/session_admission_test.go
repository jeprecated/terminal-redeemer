package mirror

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionSupervisorReservesBeforeSpawnReleasesAfterReadyOrReap(t *testing.T) {
	h := sessionTerminalFixture(t)
	h.control.change(func() { h.control.denySlot = true; h.control.permits = 1 })
	first := h.control.attempt(t, 1)
	awaitSession(t, func() bool { h.control.mu.Lock(); defer h.control.mu.Unlock(); return len(h.control.lost) > 0 })
	if _, err := os.Stat(filepath.Join(h.root, first+".pid")); !os.IsNotExist(err) {
		t.Fatal("spawned transport without pending capacity")
	}
	h.control.change(func() { h.control.denySlot = false; h.control.permits = 2 })
	second := h.control.attempt(t, 2)
	awaitSession(t, func() bool { h.control.mu.Lock(); defer h.control.mu.Unlock(); return h.control.slots == 1 })
	h.ready(t, second)
	h.control.change(func() {
		if h.control.slots != 0 {
			t.Error("ready transport retained pending capacity")
		}
	})
	h.mark(t, second, ".exit")
	awaitSession(t, func() bool { h.control.mu.Lock(); defer h.control.mu.Unlock(); return len(h.control.lost) >= 2 })
	h.control.change(func() { h.control.permits = 3 })
	h.control.attempt(t, 3)
	awaitSession(t, func() bool { h.control.mu.Lock(); defer h.control.mu.Unlock(); return h.control.slots == 1 })
	h.cancel()
	select {
	case <-h.done:
	case <-time.After(3 * time.Second):
		t.Fatal("pending transport not reaped")
	}
	h.control.change(func() {
		if h.control.slots != 0 {
			t.Error("reaped transport retained pending capacity")
		}
	})
}
