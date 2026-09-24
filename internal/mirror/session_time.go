package mirror

import (
	"context"
	"time"
)

// Go's monotonic clock can stop during suspend. A buffered reply arriving on
// resume must not become fresh evidence merely because ctx.Err still reports
// nil. Check the wall deadline as well, without its monotonic component.
func sessionContextError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); ok && !time.Now().UTC().Before(deadline.UTC()) {
		return context.DeadlineExceeded
	}
	return nil
}
