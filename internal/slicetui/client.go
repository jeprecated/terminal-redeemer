package slicetui

import (
	"context"
	"time"

	"github.com/jmo/terminal-redeemer/internal/slicecontroller"
)

// Client is the sole slice-management seam. Implementations issue controller
// control requests; the TUI never reads or mutates durable authority directly.
type Client interface {
	Call(context.Context, slicecontroller.ControlVerb, any) (slicecontroller.ControlResponse, error)
}

type SocketClient struct {
	Path    string
	Timeout time.Duration
}

func (c SocketClient) Call(ctx context.Context, verb slicecontroller.ControlVerb, payload any) (slicecontroller.ControlResponse, error) {
	return slicecontroller.CallControl(ctx, c.Path, c.Timeout, slicecontroller.NewControlRequest(verb, payload))
}
