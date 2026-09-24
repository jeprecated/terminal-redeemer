package mirror

import (
	"fmt"
	"time"

	"github.com/jmo/terminal-redeemer/internal/zellijlive"
)

// This model is serialized by the runtime server. Time and observations are
// supplied explicitly; no network IO, window policy or durable state lives here.
type sessionRecovery struct {
	members                     map[string]*recoveryMember
	order                       []string
	sessions                    map[string]*sessionRetry
	available, checking, wanted bool
	reason                      string
	nextProbe, lastProbe        time.Time
	failures                    int
	sequence                    uint64
	observing                   map[string]*recoveryMember
	inventory                   SessionInventory
}
type recoveryMember struct {
	identity                  SessionControlRequest
	proposed, active, retired string
	deadline                  time.Time
	ready, ended, reset       bool
}
type sessionRetry struct {
	failures int
	at       time.Time
}

func newSessionRecovery() *sessionRecovery {
	return &sessionRecovery{members: map[string]*recoveryMember{}, sessions: map[string]*sessionRetry{}, reason: "Waiting for host observation"}
}
func recoveryDelay(failures int) time.Duration {
	return min(30*time.Second, time.Second*time.Duration(1<<min(failures, 5)))
}
func (s *sessionRecovery) requestProbe(now time.Time) {
	now = now.UTC()
	if s.checking {
		return
	}
	s.wanted = true
	if !s.checking {
		due := now
		if floor := s.lastProbe.Add(time.Second); due.Before(floor) {
			due = floor
		}
		if s.nextProbe.IsZero() || due.Before(s.nextProbe) {
			s.nextProbe = due
		}
	}
}
func (s *sessionRecovery) beginProbe(now time.Time) (uint64, bool) {
	now = now.UTC()
	if s.checking || len(s.members) == 0 || now.Before(s.nextProbe) {
		return 0, false
	}
	waiting := false
	for _, m := range s.members {
		if !m.ready && !m.ended {
			waiting = true
			break
		}
	}
	if !s.wanted && !waiting {
		return 0, false
	}
	s.checking = true
	s.wanted = false
	s.lastProbe = now
	s.sequence++
	s.observing = make(map[string]*recoveryMember, len(s.members))
	for id, m := range s.members {
		s.observing[id] = m
	}
	return s.sequence, true
}
func (s *sessionRecovery) finishProbe(sequence uint64, now time.Time, inventory SessionInventory, err error) {
	now = now.UTC()
	if !s.checking || sequence != s.sequence {
		return
	}
	if err == nil {
		err = inventory.validate()
	}
	if err == nil && !s.inventory.GeneratedAt.IsZero() && !inventory.GeneratedAt.After(s.inventory.GeneratedAt) {
		err = fmt.Errorf("stale session inventory")
	}
	s.checking = false
	if err != nil {
		s.available = false
		s.reason = "Host observation unavailable: " + err.Error()
		s.nextProbe = now.Add(recoveryDelay(s.failures))
		s.failures = min(s.failures+1, 6)
		s.observing = nil
		return
	}
	s.available = true
	s.failures = 0
	s.reason = "Waiting for attachment admission"
	s.inventory = inventory
	s.nextProbe = now.Add(30 * time.Second)
	for id, observed := range s.observing {
		// A member joining during the probe cannot be ended by an observation
		// that may precede its session's creation. Positive presence is reusable.
		if m := s.members[id]; m == observed && inventory.SessionIDs[m.identity.Session] != m.identity.SessionID {
			m.ended = true
			m.active = ""
			m.ready = false
		}
	}
	s.observing = nil
	for _, m := range s.members {
		if !m.ended && inventory.SessionIDs[m.identity.Session] != m.identity.SessionID {
			s.requestProbe(now)
			break
		}
	}
	s.admit(now)
}
func validRecoveryRequest(r SessionControlRequest) bool {
	if !validAttachmentAttempt(r.Client) || !correlationTokenPattern.MatchString(r.Token) || !validAttachmentAttempt(r.Attempt) || !validSessionID(r.SessionID) || !zellijlive.SafeSessionName(r.Session) {
		return false
	}
	switch r.State {
	case string(sessionOffline), string(sessionChecking), string(sessionConnecting), string(sessionReady):
	default:
		return false
	}
	return r.Event == "" || r.Event == "ready" || r.Event == "lost"
}
func (s *sessionRecovery) exchange(r SessionControlRequest, now time.Time) (SessionControlReply, error) {
	now = now.UTC()
	if !validRecoveryRequest(r) {
		return SessionControlReply{}, fmt.Errorf("invalid recovery identity or state")
	}
	m := s.members[r.Client]
	if m == nil {
		for _, other := range s.members {
			if other.identity.Token == r.Token {
				return SessionControlReply{}, fmt.Errorf("projection token already registered")
			}
		}
		m = &recoveryMember{identity: r, proposed: r.Attempt}
		s.members[r.Client] = m
		s.order = append(s.order, r.Client)
		if s.sessions[r.SessionID] == nil {
			s.sessions[r.SessionID] = &sessionRetry{}
		}
		// A restarted coordinator must not grant alongside an unaccounted child.
		// Healthy helpers can retain their already-verified transport instead.
		if r.State == string(sessionReady) {
			m.ready = true
			m.active = r.Attempt
		}
		if r.State == string(sessionConnecting) || r.Event != "" && r.State != string(sessionReady) {
			m.reset = true
			m.retired = r.Attempt
		}
		s.requestProbe(now)
	} else if m.identity.Token != r.Token || m.identity.Session != r.Session || m.identity.SessionID != r.SessionID {
		return SessionControlReply{}, fmt.Errorf("recovery client identity changed")
	}
	if r.Retry {
		s.requestProbe(now)
	}
	s.expire(now)
	if r.Event != "" {
		switch {
		case r.Event == "lost" && r.Attempt == m.retired:
			// Acknowledge replay or the reap following a coordinator restart.
			m.reset = false
		case r.Attempt != m.active || m.active == "":
			return s.reply(m, now, true), nil
		case r.Event == "ready":
			if !m.ready && !now.Before(m.deadline) {
				return s.reply(m, now, true), nil
			}
			m.ready = true
			s.sessions[r.SessionID].failures = 0
			s.sessions[r.SessionID].at = time.Time{}
		case r.Event == "lost":
			s.fail(m, now)
			s.requestProbe(now)
		}
	} else if m.active == "" && !m.reset && r.Attempt != m.retired {
		m.proposed = r.Attempt
	}
	if r.State == string(sessionConnecting) && m.active != r.Attempt {
		m.reset = true
	}
	s.admit(now)
	return s.reply(m, now, m.reset || r.Attempt == m.retired), nil
}
func (s *sessionRecovery) fail(m *recoveryMember, now time.Time) {
	m.retired = m.active
	m.active = ""
	m.ready = false
	m.reset = false
	retry := s.sessions[m.identity.SessionID]
	retry.at = now.Add(recoveryDelay(retry.failures))
	retry.failures = min(retry.failures+1, 6)
}
func (s *sessionRecovery) expire(now time.Time) {
	now = now.UTC()
	for _, m := range s.members {
		if m.active != "" && !m.ready && !now.Before(m.deadline) {
			m.reset = true
		}
	}
}
func (s *sessionRecovery) admit(now time.Time) {
	s.expire(now)
	if !s.available {
		return
	}
	pending := 0
	busy := map[string]bool{}
	for _, m := range s.members {
		if m.active != "" && !m.ready {
			pending++
			busy[m.identity.SessionID] = true
		}
	}
	// Rotate each admitted member to the tail; bad sessions retain their own
	// backoff and cannot monopolize the two not-yet-ready slots.
	n := len(s.order)
	for i := 0; i < n && pending < 2; i++ {
		id := s.order[0]
		s.order = s.order[1:]
		s.order = append(s.order, id)
		m := s.members[id]
		retry := s.sessions[m.identity.SessionID]
		if m.ended || m.reset || m.active != "" || m.proposed == m.retired || busy[m.identity.SessionID] || now.Before(retry.at) || s.inventory.SessionIDs[m.identity.Session] != m.identity.SessionID {
			continue
		}
		m.active = m.proposed
		m.deadline = now.Add(15 * time.Second)
		pending++
		busy[m.identity.SessionID] = true
	}
}
func (s *sessionRecovery) reply(m *recoveryMember, now time.Time, reset bool) SessionControlReply {
	reply := SessionControlReply{Checking: s.checking, Reason: s.reason, Ended: m.ended, Reset: reset}
	if m.reset && m.active != "" && !m.ready {
		reply.CancelAttempt = m.active
	}
	if !s.available || s.checking {
		reply.RetryAt = s.nextProbe
	}
	if retry := s.sessions[m.identity.SessionID]; retry != nil && now.Before(retry.at) {
		reply.RetryAt = retry.at
		reply.Reason = "Attachment failed; waiting for session backoff"
	}
	if !reset && !m.ended && m.active != "" && !m.ready {
		reply.Grant = &SessionGrant{m.active, m.deadline}
	}
	return reply
}
func (s *sessionRecovery) remove(client string) {
	m := s.members[client]
	if m == nil {
		return
	}
	delete(s.members, client)
	for i, id := range s.order {
		if id == client {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
	for _, other := range s.members {
		if other.identity.SessionID == m.identity.SessionID {
			return
		}
	}
	delete(s.sessions, m.identity.SessionID)
}
