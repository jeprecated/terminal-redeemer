package resume

import (
	"context"
	"fmt"
	"time"

	"github.com/jmo/terminal-redeemer/internal/niri"
)

// WaitForNiri polls the same snapshot source used by resume until a complete
// Niri windows/workspaces payload can be queried and parsed. It is read-only
// and returns the successful payload so initial reconciliation does not issue
// a duplicate readiness query.
func WaitForNiri(ctx context.Context, source SnapshotSource, timeout, pollInterval time.Duration) ([]byte, error) {
	if source == nil {
		return nil, fmt.Errorf("Niri snapshot source is unavailable")
	}
	if timeout <= 0 || pollInterval <= 0 || pollInterval > timeout {
		return nil, fmt.Errorf("invalid Niri readiness timeout/poll interval")
	}

	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	attempts := 0
	var lastErr error
	for {
		attempts++
		raw, err := source.Snapshot(waitCtx)
		if err == nil {
			_, err = niri.ParseSnapshot(raw)
		}
		if err == nil {
			return raw, nil
		}
		lastErr = err

		timer := time.NewTimer(pollInterval)
		select {
		case <-waitCtx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil, fmt.Errorf("Niri IPC was not ready after %s (%d attempts): %w", timeout, attempts, lastErr)
		case <-timer.C:
		}
	}
}
