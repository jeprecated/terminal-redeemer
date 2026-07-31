package slicecontroller

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"regexp"
	"sync"
	"syscall"
	"time"

	"github.com/jmo/terminal-redeemer/internal/sliceprotocol"
)

type ControlVerb string

const (
	VerbStatus              ControlVerb = "status"
	VerbWorkspaceAdd        ControlVerb = "workspace_add"
	VerbWorkspaceRemove     ControlVerb = "workspace_remove"
	VerbAllEnable           ControlVerb = "all_enable"
	VerbAllDisable          ControlVerb = "all_disable"
	VerbPickup              ControlVerb = "pickup"
	VerbPickupRemove        ControlVerb = "pickup_remove"
	VerbDrop                ControlVerb = "drop"
	VerbClose               ControlVerb = "close"
	VerbReopen              ControlVerb = "reopen"
	VerbUndo                ControlVerb = "undo"
	VerbReconnect           ControlVerb = "reconnect"
	VerbLaunchHandoff       ControlVerb = "launch_handoff"
	VerbAttachmentConnected ControlVerb = "attachment_connected"
	VerbAttachmentLost      ControlVerb = "attachment_lost"
)

var safeRequestID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

type ControlRequest struct {
	SchemaVersion int             `json:"schema_version"`
	RequestID     string          `json:"request_id"`
	Verb          ControlVerb     `json:"verb"`
	Payload       json.RawMessage `json:"payload,omitempty"`
}
type ControlOutcome struct {
	Status string `json:"status"`
	Code   string `json:"code,omitempty"`
}
type ControlResponse struct {
	SchemaVersion int            `json:"schema_version"`
	RequestID     string         `json:"request_id,omitempty"`
	Outcome       ControlOutcome `json:"outcome"`
	State         *State         `json:"state,omitempty"`
	Effects       []Effect       `json:"effects,omitempty"`
}
type SourcePayload struct {
	SourceID string `json:"source_id"`
}
type ClosePayload struct {
	SourceID      string `json:"source_id"`
	FocusRequired bool   `json:"focus_required,omitempty"`
}
type WorkspacePayload struct {
	Name string `json:"name"`
}

type ControlHandler struct {
	Engine    *Engine
	Execute   func(context.Context, []Effect) error
	Serialize *sync.Mutex
}

func (h ControlHandler) Handle(ctx context.Context, request ControlRequest) ControlResponse {
	if h.Serialize != nil {
		h.Serialize.Lock()
		defer h.Serialize.Unlock()
	}
	response := ControlResponse{SchemaVersion: SchemaVersion, RequestID: request.RequestID}
	fail := func(code string) ControlResponse {
		response.Outcome = ControlOutcome{Status: "error", Code: code}
		return response
	}
	if h.Engine == nil {
		return fail("controller_unavailable")
	}
	var state State
	var effects []Effect
	var focusedRollback *FocusedCloseRollback
	var err error
	switch request.Verb {
	case VerbStatus:
		var p struct{}
		err = decodeControlPayload(request.Payload, &p)
		if err == nil {
			state, err = h.Engine.Status()
		}
	case VerbWorkspaceAdd, VerbWorkspaceRemove:
		var p WorkspacePayload
		err = decodeControlPayload(request.Payload, &p)
		if err == nil {
			state, effects, err = h.Engine.SelectWorkspace(p.Name, request.Verb == VerbWorkspaceAdd)
		}
	case VerbAllEnable, VerbAllDisable:
		var p struct{}
		err = decodeControlPayload(request.Payload, &p)
		if err == nil {
			state, effects, err = h.Engine.SelectAll(request.Verb == VerbAllEnable)
		}
	case VerbPickup, VerbPickupRemove, VerbDrop, VerbReopen, VerbReconnect:
		var p SourcePayload
		err = decodeControlPayload(request.Payload, &p)
		if err == nil && !safeName(p.SourceID) {
			err = errors.New("invalid source")
		}
		if err == nil {
			switch request.Verb {
			case VerbPickup, VerbPickupRemove:
				state, effects, err = h.Engine.Pickup(p.SourceID, request.Verb == VerbPickup)
			case VerbDrop:
				state, effects, err = h.Engine.Close(p.SourceID)
			case VerbReopen:
				state, effects, err = h.Engine.Reopen(p.SourceID)
			case VerbReconnect:
				state, effects, err = h.Engine.Reconnect(p.SourceID)
			}
		}
	case VerbClose:
		var p ClosePayload
		err = decodeControlPayload(request.Payload, &p)
		if err == nil && !safeName(p.SourceID) {
			err = errors.New("invalid source")
		}
		if err == nil {
			if p.FocusRequired {
				if h.Serialize == nil {
					err = errors.New("focused close requires serialized control execution")
				} else {
					state, effects, focusedRollback, err = h.Engine.CloseFocused(p.SourceID)
				}
			} else {
				state, effects, err = h.Engine.Close(p.SourceID)
			}
		}
	case VerbUndo:
		var p struct{}
		err = decodeControlPayload(request.Payload, &p)
		if err == nil {
			state, effects, err = h.Engine.Undo()
		}
	case VerbLaunchHandoff:
		var p LaunchHandoff
		err = decodeControlPayload(request.Payload, &p)
		matched := false
		if err == nil && p.Status == "launched" {
			current, statusErr := h.Engine.Status()
			if statusErr != nil {
				err = statusErr
			} else {
				matched = launchHandoffMatches(current, p)
				if !matched {
					p.Status = "launch_pending"
				}
			}
		}
		if err == nil {
			state, err = h.Engine.SetLaunchHandoff(p)
		}
		if err == nil && matched {
			if source, ok := state.Sources[p.SourceID]; ok && source.Connection == ConnectionDisconnected {
				state, effects, err = h.Engine.Reconnect(p.SourceID)
			}
		}
	case VerbAttachmentConnected:
		var p SourcePayload
		err = decodeControlPayload(request.Payload, &p)
		if err == nil {
			state, err = h.Engine.AttachmentConnected(p.SourceID)
		}
	case VerbAttachmentLost:
		var p SourcePayload
		err = decodeControlPayload(request.Payload, &p)
		if err == nil {
			state, effects, err = h.Engine.AttachmentLost(p.SourceID)
		}
	default:
		return fail("unsupported_verb")
	}
	if err != nil {
		return fail("request_failed")
	}
	if focusedRollback != nil && h.Execute == nil {
		if _, rollbackErr := h.Engine.RollbackFocusedClose(focusedRollback); rollbackErr != nil {
			return fail("focused_close_rollback_failed")
		}
		return fail("effect_failed")
	}
	if len(effects) > 0 && h.Execute != nil {
		if err = h.Execute(ctx, effects); err != nil {
			if focusedRollback != nil {
				if _, rollbackErr := h.Engine.RollbackFocusedClose(focusedRollback); rollbackErr != nil {
					return fail("focused_close_rollback_failed")
				}
			}
			return fail("effect_failed")
		}
	}
	response.Outcome = ControlOutcome{Status: "ok"}
	response.State = &state
	response.Effects = effects
	return response
}

func launchHandoffMatches(state State, h LaunchHandoff) bool {
	if state.Inventory == nil || state.Inventory.SourceEpoch != h.SourceEpoch || h.SourceID == "" || h.RuntimeWindowID == 0 {
		return false
	}
	for _, source := range state.Inventory.Sources {
		if source.SourceID == h.SourceID && source.RuntimeWindowID == h.RuntimeWindowID && source.Session.Name == h.SessionName && source.Workspace.Name == h.WorkspaceName {
			return true
		}
	}
	return false
}

func DecodeControlRequest(reader io.Reader) (ControlRequest, error) {
	payload, err := io.ReadAll(io.LimitReader(reader, MaxControlBytes+1))
	if err != nil {
		return ControlRequest{}, err
	}
	if len(payload) == 0 || len(payload) > MaxControlBytes {
		return ControlRequest{}, errors.New("control request empty or too large")
	}
	if err := sliceprotocol.RejectDuplicateKeys(payload); err != nil {
		return ControlRequest{}, err
	}
	if err := sliceprotocol.RejectUnknownFieldsExact(payload, ControlRequest{}); err != nil {
		return ControlRequest{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var request ControlRequest
	if err := decoder.Decode(&request); err != nil {
		return request, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return request, errors.New("trailing control JSON")
	}
	if request.SchemaVersion != SchemaVersion || !safeRequestID.MatchString(request.RequestID) {
		return request, errors.New("invalid control envelope")
	}
	return request, nil
}
func decodeControlPayload(raw json.RawMessage, target any) error {
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	if err := sliceprotocol.RejectDuplicateKeys(raw); err != nil {
		return err
	}
	if err := sliceprotocol.RejectUnknownFieldsExact(raw, target); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return errors.New("trailing payload JSON")
	}
	return nil
}

// ServeControl holds a private Unix socket until ctx cancellation. The caller
// holds the controller store lock for the full lifetime.
func ServeControl(ctx context.Context, path string, timeout time.Duration, handler ControlHandler) error {
	if timeout <= 0 {
		return errors.New("control timeout must be positive")
	}
	if len(path) > 103 {
		return errors.New("control socket path exceeds Unix budget")
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return errors.New("refusing to replace non-socket control path")
		}
		if stat, ok := info.Sys().(*syscall.Stat_t); !ok || int(stat.Uid) != os.Getuid() {
			return errors.New("unsafe existing control socket")
		}
		_ = os.Remove(path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	defer listener.Close()
	defer os.Remove(path)
	if err := os.Chmod(path, 0o600); err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { _ = listener.Close() })
	defer stop()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go serveControlConn(ctx, conn, timeout, handler)
	}
}
func serveControlConn(ctx context.Context, conn net.Conn, timeout time.Duration, handler ControlHandler) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	request, err := DecodeControlRequest(bufio.NewReader(conn))
	var response ControlResponse
	if err != nil {
		response = ControlResponse{SchemaVersion: SchemaVersion, Outcome: ControlOutcome{Status: "error", Code: "invalid_request"}}
	} else {
		requestCtx, cancel := context.WithTimeout(ctx, timeout)
		response = handler.Handle(requestCtx, request)
		cancel()
	}
	payload, marshalErr := json.Marshal(response)
	if marshalErr != nil || len(payload) > MaxControlResponseBytes {
		payload, _ = json.Marshal(ControlResponse{SchemaVersion: SchemaVersion, RequestID: request.RequestID, Outcome: ControlOutcome{Status: "error", Code: "response_too_large"}})
	}
	payload = append(payload, '\n')
	_, _ = conn.Write(payload)
}

func CallControl(ctx context.Context, path string, timeout time.Duration, request ControlRequest) (ControlResponse, error) {
	if timeout <= 0 {
		return ControlResponse{}, errors.New("control timeout must be positive")
	}
	request.SchemaVersion = SchemaVersion
	if !safeRequestID.MatchString(request.RequestID) {
		return ControlResponse{}, errors.New("invalid request id")
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return ControlResponse{}, err
	}
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "unix", path)
	if err != nil {
		return ControlResponse{}, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if _, err = conn.Write(payload); err != nil {
		return ControlResponse{}, err
	}
	if unix, ok := conn.(*net.UnixConn); ok {
		_ = unix.CloseWrite()
	}
	raw, err := io.ReadAll(io.LimitReader(conn, MaxControlResponseBytes+1))
	if err != nil {
		return ControlResponse{}, err
	}
	return decodeControlResponse(raw, request.RequestID)
}

func decodeControlResponse(raw []byte, requestID string) (ControlResponse, error) {
	if len(raw) == 0 || len(raw) > MaxControlResponseBytes {
		return ControlResponse{}, errors.New("control response empty or too large")
	}
	if err := sliceprotocol.RejectDuplicateKeys(raw); err != nil {
		return ControlResponse{}, err
	}
	if err := sliceprotocol.RejectUnknownFieldsExact(raw, ControlResponse{}); err != nil {
		return ControlResponse{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var response ControlResponse
	if err := decoder.Decode(&response); err != nil {
		return response, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return response, errors.New("trailing control response JSON")
	}
	if response.SchemaVersion != SchemaVersion || response.RequestID != requestID {
		return response, fmt.Errorf("mismatched control response")
	}
	if response.Outcome.Status != "ok" && response.Outcome.Status != "error" {
		return response, errors.New("invalid control outcome")
	}
	if response.State != nil {
		if err := response.State.Validate(); err != nil {
			return response, fmt.Errorf("invalid control response state: %w", err)
		}
	}
	return response, nil
}

func NewControlRequest(verb ControlVerb, payload any) ControlRequest {
	id := fmt.Sprintf("req-%d", time.Now().UnixNano())
	raw, _ := json.Marshal(payload)
	return ControlRequest{SchemaVersion: SchemaVersion, RequestID: id, Verb: verb, Payload: raw}
}
