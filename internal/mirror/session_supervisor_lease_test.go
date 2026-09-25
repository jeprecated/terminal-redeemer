package mirror

import (
	"context"
	"sync"
	"testing"
	"time"
)

// modelSessionControl drives the real recovery model in-process. The first
// grant reply is dropped as if it arrived after suspend, and the clock jumps
// past that grant's deadline.
type modelSessionControl struct {
	mu      sync.Mutex
	model   *sessionRecovery
	offset  time.Duration
	dropped bool
	grants  []string
}

func (c *modelSessionControl) Exchange(ctx context.Context, r SessionControlRequest) (SessionControlReply, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now().Add(c.offset)
	if seq, ok := c.model.beginProbe(now); ok {
		c.model.finishProbe(seq, now, recoveryInventory(now, r), nil)
	}
	reply, err := c.model.exchange(r, now)
	if err != nil || reply.Grant == nil || reply.Reset {
		return reply, err
	}
	if len(c.grants) == 0 || c.grants[len(c.grants)-1] != reply.Grant.Attempt {
		c.grants = append(c.grants, reply.Grant.Attempt)
	}
	if !c.dropped {
		c.dropped = true
		c.offset += 20 * time.Second
		return SessionControlReply{}, context.DeadlineExceeded
	}
	return reply, nil
}

func TestSessionSupervisorReleasesGrantWhoseReplyWasLost(t *testing.T) {
	control := &modelSessionControl{model: newSessionRecovery()}
	h := sessionTerminalFixtureWith(t, control)
	var first, second string
	awaitSession(t, func() bool {
		control.mu.Lock()
		defer control.mu.Unlock()
		if len(control.grants) >= 2 {
			first, second = control.grants[0], control.grants[1]
			return true
		}
		return false
	})
	if second == first {
		t.Fatal("lost grant was reissued instead of released")
	}
	h.mark(t, second, ".ready")
	awaitSession(t, func() bool {
		control.mu.Lock()
		defer control.mu.Unlock()
		for _, m := range control.model.members {
			return m.ready && m.active == second
		}
		return false
	})
	h.write(t, "fresh\n")
	awaitSession(t, func() bool { return h.received(second) == "fresh\n" })
}
