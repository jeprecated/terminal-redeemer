package mirror

import (
	"fmt"
	"testing"
	"time"

	"github.com/jmo/terminal-redeemer/internal/zellijlive"
)

func recoveryRequest(n int) SessionControlRequest {
	name := fmt.Sprintf("session-%d", n)
	return SessionControlRequest{Token: fmt.Sprintf("%032x", n+1), Client: fmt.Sprintf("%032x", n+101), Attempt: fmt.Sprintf("%032x", n+201), Session: name, SessionID: zellijlive.SessionID("boot", name, 1, uint64(n+1)), State: string(sessionOffline)}
}
func recoveryInventory(now time.Time, rs ...SessionControlRequest) SessionInventory {
	inv := SessionInventory{GeneratedAt: now, ActiveSessions: []string{}, SessionIDs: map[string]string{}}
	for _, r := range rs {
		inv.ActiveSessions = append(inv.ActiveSessions, r.Session)
		inv.SessionIDs[r.Session] = r.SessionID
	}
	return inv
}
func TestSessionRecoveryThirtyShareCheckBackoffAndRetry(t *testing.T) {
	s := newSessionRecovery()
	now := time.Unix(10000, 0)
	requests := make([]SessionControlRequest, 30)
	for i := range requests {
		requests[i] = recoveryRequest(i)
		if _, err := s.exchange(requests[i], now); err != nil {
			t.Fatal(err)
		}
	}
	seq, ok := s.beginProbe(now)
	if !ok {
		t.Fatal("no probe")
	}
	for _, r := range requests {
		r.Retry = true
		_, _ = s.exchange(r, now)
		if _, ok := s.beginProbe(now); ok {
			t.Fatal("overlapping probe")
		}
	}
	for i, delay := range []time.Duration{1, 2, 4, 8, 16, 30, 30} {
		s.finishProbe(seq, now, SessionInventory{}, fmt.Errorf("offline"))
		if got := s.nextProbe.Sub(now); got != delay*time.Second {
			t.Fatalf("failure %d delay %s", i, got)
		}
		now = s.nextProbe
		seq, ok = s.beginProbe(now)
		if !ok {
			t.Fatal("retry exhausted")
		}
	}
	s.finishProbe(seq, now, SessionInventory{}, fmt.Errorf("offline"))
	for _, r := range requests {
		r.Retry = true
		_, _ = s.exchange(r, now)
	}
	if _, ok := s.beginProbe(now); ok {
		t.Fatal("manual retry violated minimum spacing")
	}
	if _, ok := s.beginProbe(now.Add(time.Second)); !ok {
		t.Fatal("manual requests did not coalesce")
	}
}
func TestSessionRecoveryAdmissionFairAndStaleEventsHarmless(t *testing.T) {
	s := newSessionRecovery()
	now := time.Unix(10000, 0)
	var rs []SessionControlRequest
	for i := 0; i < 5; i++ {
		r := recoveryRequest(i)
		rs = append(rs, r)
		_, _ = s.exchange(r, now)
	}
	seq, _ := s.beginProbe(now)
	s.finishProbe(seq, now, recoveryInventory(now, rs...), nil)
	pending := func() int {
		n := 0
		for _, m := range s.members {
			if m.active != "" && !m.ready {
				n++
			}
		}
		return n
	}
	if pending() != 2 {
		t.Fatalf("pending=%d", pending())
	}
	bad := rs[0]
	bad.Event = "lost"
	_, _ = s.exchange(bad, now)
	if !s.available || pending() != 2 {
		t.Fatal("one bad attachment disabled healthy admissions")
	}
	if s.members[rs[2].Client].active == "" {
		t.Fatal("waiting member starved")
	}
	r := rs[1]
	r.Event = "ready"
	r.State = string(sessionReady)
	_, _ = s.exchange(r, now)
	if pending() != 2 || s.members[rs[3].Client].active == "" {
		t.Fatal("ready attachment retained a slot")
	}
	bad.Event = ""
	bad.Attempt = fmt.Sprintf("%032x", 999)
	reply, _ := s.exchange(bad, now)
	if reply.Grant != nil {
		t.Fatal("bad session bypassed backoff")
	}
	// An old readiness event cannot confirm its replacement attempt.
	stale := rs[0]
	stale.Event = "ready"
	stale.State = string(sessionReady)
	reply, _ = s.exchange(stale, now.Add(time.Second))
	if !reply.Reset || s.members[bad.Client].ready {
		t.Fatal("stale readiness accepted")
	}
}
func TestSessionRecoveryExpiryWaitsForReapAndRejectsReplay(t *testing.T) {
	s := newSessionRecovery()
	now := time.Unix(10000, 0)
	a, b, c := recoveryRequest(0), recoveryRequest(1), recoveryRequest(2)
	for _, r := range []SessionControlRequest{a, b, c} {
		_, _ = s.exchange(r, now)
	}
	seq, _ := s.beginProbe(now)
	s.finishProbe(seq, now, recoveryInventory(now, a, b, c), nil)
	late := now.Add(16 * time.Second)
	a.Event = "ready"
	a.State = string(sessionReady)
	reply, _ := s.exchange(a, late)
	if reply.CancelAttempt != a.Attempt || s.members[c.Client].active != "" {
		t.Fatal("expired child released capacity before reap")
	}
	a.Event = "lost"
	a.State = string(sessionOffline)
	_, _ = s.exchange(a, late)
	if s.members[c.Client].active == "" {
		t.Fatal("reaped slot not released")
	}
	a.Event = ""
	reply, _ = s.exchange(a, late.Add(time.Second))
	if !reply.Reset || reply.Grant != nil {
		t.Fatal("retired grant replayed")
	}
}
func TestSessionRecoveryOnlyFreshCompleteScopedEvidenceEnds(t *testing.T) {
	s := newSessionRecovery()
	now := time.Unix(10000, 0)
	a, b := recoveryRequest(0), recoveryRequest(1)
	_, _ = s.exchange(a, now)
	seq, _ := s.beginProbe(now)
	_, _ = s.exchange(b, now) // Joined during this observation.
	s.finishProbe(seq, now, recoveryInventory(now), nil)
	if !s.members[a.Client].ended || s.members[b.Client].ended {
		t.Fatal("observation escaped its membership scope")
	}
	now = now.Add(time.Second)
	seq, _ = s.beginProbe(now)
	s.finishProbe(seq-1, now, recoveryInventory(now), nil)
	if !s.checking || s.members[b.Client].ended {
		t.Fatal("stale completion accepted")
	}
	s.finishProbe(seq, now, SessionInventory{}, nil)
	if s.members[b.Client].ended {
		t.Fatal("incomplete inventory ended member")
	}
	now = now.Add(time.Second)
	seq, _ = s.beginProbe(now)
	s.finishProbe(seq, now, recoveryInventory(time.Unix(10000, 0)), nil)
	if s.members[b.Client].ended {
		t.Fatal("old timestamp ended member")
	}
	now = s.nextProbe
	seq, _ = s.beginProbe(now)
	s.finishProbe(seq, now, recoveryInventory(now), nil)
	if !s.members[b.Client].ended {
		t.Fatal("fresh authoritative absence ignored")
	}
}
func TestSessionRecoveryRestartKeepsReadyButResetsConnecting(t *testing.T) {
	s := newSessionRecovery()
	now := time.Unix(10000, 0)
	ready, connecting := recoveryRequest(0), recoveryRequest(1)
	ready.State = string(sessionReady)
	connecting.State = string(sessionConnecting)
	a, _ := s.exchange(ready, now)
	b, _ := s.exchange(connecting, now)
	if a.Reset || a.Grant != nil || !b.Reset || b.Grant != nil {
		t.Fatal("restart mishandled live transport evidence")
	}
	changed := ready
	changed.Session = "unrelated"
	if _, err := s.exchange(changed, now); err == nil {
		t.Fatal("identity changed")
	}
	duplicate := recoveryRequest(2)
	duplicate.Token = ready.Token
	if _, err := s.exchange(duplicate, now); err == nil {
		t.Fatal("unrelated client reused token")
	}
}
