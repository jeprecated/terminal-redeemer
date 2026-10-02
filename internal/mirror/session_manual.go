package mirror

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type manualSessionControl struct {
	mutex        sync.Mutex
	attempt      string
	deadline     time.Time
	started      bool
	retryPending bool
}

func (control *manualSessionControl) Exchange(ctx context.Context, request SessionControlRequest) (SessionControlReply, error) {
	control.mutex.Lock()
	defer control.mutex.Unlock()
	if err := ctx.Err(); err != nil {
		return SessionControlReply{}, err
	}
	if !validAttachmentAttempt(request.Attempt) {
		return SessionControlReply{}, fmt.Errorf("invalid attachment attempt")
	}
	if request.Event == "lost" && request.Attempt == control.attempt {
		control.attempt = ""
	}
	if request.Event == "ready" && request.Attempt == control.attempt {
		control.retryPending = false
	}
	if request.Retry && request.State != "ready" {
		control.retryPending = true
	}
	if request.Event == "" && control.attempt == "" && (!control.started || control.retryPending) {
		control.started = true
		control.retryPending = false
		control.attempt = request.Attempt
		control.deadline = time.Now().Add(20 * time.Second)
	}
	if request.Event == "" && request.Attempt == control.attempt {
		return SessionControlReply{Grant: &SessionGrant{Attempt: control.attempt, Deadline: control.deadline}}, nil
	}
	return SessionControlReply{Reason: "Connection closed; press Enter to reconnect to this exact session"}, nil
}

func RunManualSessionSupervisor(ctx context.Context, cfg SessionSupervisorConfig) error {
	cfg.Control = &manualSessionControl{}
	cfg.Local = nil
	return RunSessionSupervisor(ctx, cfg)
}
