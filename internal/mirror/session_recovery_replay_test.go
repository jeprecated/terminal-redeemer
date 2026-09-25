package mirror

import (
	"fmt"
	"testing"
	"time"
)

func TestSessionRecoveryRetiredConnectingLossCannotResetReplacement(t *testing.T) {
	s := newSessionRecovery()
	now := time.Unix(10000, 0)
	r := recoveryRequest(0)
	_, _ = s.exchange(r, now)
	seq, _ := s.beginProbe(now)
	s.finishProbe(seq, now, recoveryInventory(now, r), nil)
	old := r
	old.Event = "lost"
	_, _ = s.exchange(old, now)
	r.Attempt = fmt.Sprintf("%032x", 999)
	reply, _ := s.exchange(r, now.Add(time.Second))
	if reply.Grant == nil {
		t.Fatal("replacement not admitted")
	}
	old.State = string(sessionConnecting)
	_, _ = s.exchange(old, now.Add(time.Second))
	if s.members[r.Client].reset {
		t.Fatal("stale connecting loss revoked replacement")
	}
	ancient := old
	ancient.Attempt = fmt.Sprintf("%032x", 998)
	ancient.Event = ""
	reply, _ = s.exchange(ancient, now.Add(time.Second))
	if s.members[r.Client].reset || reply.CancelAttempt != "" || reply.Grant != nil {
		t.Fatal("ancient connecting status affected live attempt")
	}
	s.expire(now.Add(20 * time.Second))
	// Revocation stays sticky even if wall time moves back before the deadline.
	_, _ = s.exchange(old, now.Add(time.Second))
	if !s.members[r.Client].reset {
		t.Fatal("stale loss cleared replacement revocation")
	}
	r.State = string(sessionReady)
	r.Event = "ready"
	reply, _ = s.exchange(r, now.Add(2*time.Second))
	if reply.CancelAttempt != r.Attempt || s.members[r.Client].ready {
		t.Fatal("clock rollback revived an expired grant")
	}
}
