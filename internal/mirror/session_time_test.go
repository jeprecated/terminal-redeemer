package mirror

import (
	"context"
	"errors"
	"testing"
	"time"
)

type wallExpiredContext struct {
	context.Context
	deadline time.Time
}

func (c *wallExpiredContext) Deadline() (time.Time, bool) { return c.deadline, true }
func TestSessionContextRejectsWallExpiredEvidenceBeforeTimerFires(t *testing.T) {
	ctx := &wallExpiredContext{Context: context.Background(), deadline: time.Now().Add(-time.Second)}
	if ctx.Err() != nil {
		t.Fatal("fixture must model a timer not yet fired")
	}
	if !errors.Is(sessionContextError(ctx), context.DeadlineExceeded) {
		t.Fatal("accepted buffered pre-suspend evidence")
	}
	if _, err := AcquireSessionInventory(ctx, nil, RemoteConfig{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("late evidence entered planner: %v", err)
	}
}
