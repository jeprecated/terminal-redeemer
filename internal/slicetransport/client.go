package slicetransport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jmo/terminal-redeemer/internal/sliceprotocol"
	"github.com/jmo/terminal-redeemer/internal/slicerpc"
	"github.com/jmo/terminal-redeemer/internal/sourceinventory"
)

const MaxResponseBytes = 16 << 20

var safeDestination = regexp.MustCompile(`^[A-Za-z0-9_.:@%+\-]+$`)

type AmbiguousError struct {
	Status slicerpc.OutcomeStatus
	Err    error
}

func (e *AmbiguousError) Error() string {
	return fmt.Sprintf("%s transport outcome: %v", e.Status, e.Err)
}
func (e *AmbiguousError) Unwrap() error { return e.Err }

type Client struct {
	Command           string
	Options           []string
	Host              string
	RPCCommand        []string
	Timeout           time.Duration
	KeepaliveInterval time.Duration
	KeepaliveCount    int
	MaxAttempts       int
	InitialBackoff    time.Duration
	MaxBackoff        time.Duration
	Run               func(context.Context, string, []string, []byte) ([]byte, error)
	Sleep             func(context.Context, time.Duration) error
}

func (c Client) Call(ctx context.Context, request slicerpc.Request) (slicerpc.Response, error) {
	if err := c.validate(); err != nil {
		return slicerpc.Response{}, err
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return slicerpc.Response{}, err
	}
	payload = append(payload, '\n')
	attempts := c.MaxAttempts
	if attempts <= 0 {
		attempts = 1
	}
	retryable := request.Verb == slicerpc.VerbLiveness || request.Verb == slicerpc.VerbSnapshot || request.Verb == slicerpc.VerbTokenQuery || request.Verb == slicerpc.VerbTokenReplay
	backoff := c.InitialBackoff
	var last error
	for attempt := 1; attempt <= attempts; attempt++ {
		callCtx, cancel := context.WithTimeout(ctx, c.Timeout)
		out, callErr := c.runner()(callCtx, c.Command, c.argv(), payload)
		cancel()
		if callErr == nil {
			response, decodeErr := decodeResponse(out, request)
			if decodeErr == nil {
				return response, nil
			}
			callErr = decodeErr
		}
		last = callErr
		if !retryable || attempt == attempts {
			break
		}
		if err := c.sleeper()(ctx, backoff); err != nil {
			return slicerpc.Response{}, err
		}
		backoff *= 2
		if backoff > c.MaxBackoff {
			backoff = c.MaxBackoff
		}
	}
	status := slicerpc.StatusDisconnected
	if request.Verb == slicerpc.VerbLaunch || request.Verb == slicerpc.VerbWorkspaceEnsure {
		status = slicerpc.StatusPending
	}
	return slicerpc.Response{}, &AmbiguousError{Status: status, Err: last}
}
func (c Client) validate() error {
	if c.Timeout <= 0 || c.KeepaliveInterval <= 0 || c.KeepaliveCount < 1 || c.KeepaliveCount > 10 || c.MaxAttempts < 1 || c.MaxAttempts > 10 || c.InitialBackoff <= 0 || c.MaxBackoff <= 0 || c.InitialBackoff > c.MaxBackoff {
		return errors.New("transport timing and retry settings must be positive and bounded")
	}
	for _, value := range append(append([]string{c.Command, c.Host}, c.Options...), c.RPCCommand...) {
		if value == "" || !utf8.ValidString(value) || len(value) > 4096 || strings.ContainsAny(value, "\x00\r\n") {
			return errors.New("transport argv contains invalid value")
		}
	}
	if !safeDestination.MatchString(c.Host) || len(c.Host) > 255 {
		return errors.New("transport destination is invalid")
	}
	if len(c.RPCCommand) == 0 || len(c.RPCCommand) > 16 {
		return errors.New("remote RPC argv is invalid")
	}
	for _, value := range c.RPCCommand {
		for _, r := range value {
			if !(r == '/' || r == '-' || r == '_' || r == '.' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
				return errors.New("remote RPC argv must use shell-inert packaged tokens")
			}
		}
	}
	return nil
}
func (c Client) argv() []string {
	seconds := int(c.KeepaliveInterval / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	args := []string{"-T", "-o", "ServerAliveInterval=" + strconv.Itoa(seconds), "-o", "ServerAliveCountMax=" + strconv.Itoa(c.KeepaliveCount)}
	args = append(args, c.Options...)
	args = append(args, "--", c.Host)
	args = append(args, c.RPCCommand...)
	return args
}
func (c Client) runner() func(context.Context, string, []string, []byte) ([]byte, error) {
	if c.Run != nil {
		return c.Run
	}
	return run
}
func run(ctx context.Context, command string, args []string, input []byte) ([]byte, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Stdin = bytes.NewReader(input)
	stdout := limitedBuffer{max: MaxResponseBytes}
	stderr := limitedBuffer{max: 1 << 20}
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("remote RPC transport failed: %w", err)
	}
	return stdout.Bytes(), nil
}
func (c Client) sleeper() func(context.Context, time.Duration) error {
	if c.Sleep != nil {
		return c.Sleep
	}
	return func(ctx context.Context, d time.Duration) error {
		timer := time.NewTimer(d)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		}
	}
}
func decodeResponse(payload []byte, request slicerpc.Request) (slicerpc.Response, error) {
	if len(payload) == 0 || len(payload) > MaxResponseBytes {
		return slicpcZero(), errors.New("RPC response empty or too large")
	}
	if err := sliceprotocol.RejectDuplicateKeys(payload); err != nil {
		return slicpcZero(), err
	}
	if err := sliceprotocol.RejectUnknownFieldsExact(payload, slicerpc.Response{}); err != nil {
		return slicpcZero(), err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	var response slicerpc.Response
	if err := decoder.Decode(&response); err != nil {
		return response, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return response, errors.New("RPC response has trailing JSON")
	}
	if response.SchemaVersion != slicerpc.SchemaVersion {
		return response, errors.New("unsupported RPC response version")
	}
	if response.RequestID == "" || response.RequestID != request.RequestID {
		return response, errors.New("RPC response request_id does not match request")
	}
	if len(response.SupportedSchemaVersions) != 0 {
		return response, errors.New("correlated RPC response unexpectedly contains negotiation error fields")
	}
	if err := validateOutcome(request, response); err != nil {
		return response, err
	}
	return response, nil
}

func validateOutcome(request slicerpc.Request, response slicerpc.Response) error {
	status, code := response.Outcome.Status, response.Outcome.Code
	knownCode := map[slicerpc.OutcomeStatus]map[string]bool{
		slicerpc.StatusOK:          {"": true},
		slicerpc.StatusInvalid:     {"invalid_request": true, "invalid_payload": true, "invalid_workspace_name": true, "invalid_token": true, "invalid_launch_metadata": true, "unsupported_verb": true},
		slicerpc.StatusUnavailable: {"niri_version_unavailable": true, "snapshot_unavailable": true, "workspace_ensure_failed": true, "launch_unavailable": true, "token_store_unavailable": true, "token_not_found": true, "token_state_unavailable": true},
		slicerpc.StatusPending:     {"launch_pending": true}, slicerpc.StatusDisconnected: {"transport_disconnected": true}, slicerpc.StatusFailed: {"launch_failed": true, "launch_identity_conflict": true},
	}
	codes, ok := knownCode[status]
	if !ok || !codes[code] {
		return errors.New("RPC response has unknown status/code combination")
	}
	if status == slicerpc.StatusUnavailable || status == slicerpc.StatusInvalid || status == slicerpc.StatusDisconnected || (status == slicerpc.StatusFailed && response.Result == nil) {
		if response.Result != nil {
			return errors.New("non-result RPC outcome unexpectedly contains result")
		}
		return validateNonResult(request.Verb, status, code)
	}
	switch request.Verb {
	case slicerpc.VerbLiveness:
		if status != slicerpc.StatusOK {
			return errors.New("liveness response has invalid result status")
		}
		result, ok := response.Result.(map[string]any)
		if !ok || len(result) != 2 || result["alive"] != true {
			return errors.New("invalid liveness result")
		}
		versions, ok := result["schema_versions"].([]any)
		if !ok || len(versions) != 1 {
			return errors.New("invalid liveness schema versions")
		}
		version, ok := versions[0].(json.Number)
		if !ok || version.String() != strconv.FormatUint(uint64(slicerpc.SchemaVersion), 10) {
			return errors.New("invalid liveness schema versions")
		}
	case slicerpc.VerbSnapshot:
		if status != slicerpc.StatusOK {
			return errors.New("snapshot response has invalid result status")
		}
		payload, err := json.Marshal(response.Result)
		if err != nil {
			return err
		}
		if _, err := sliceprotocol.Decode(bytes.NewReader(payload)); err != nil {
			return fmt.Errorf("invalid snapshot result: %w", err)
		}
	case slicerpc.VerbWorkspaceEnsure:
		if status != slicerpc.StatusOK {
			return errors.New("workspace ensure response has invalid result status")
		}
		result, ok := response.Result.(map[string]any)
		if !ok || len(result) != 1 {
			return errors.New("invalid workspace ensure result")
		}
		id, ok := result["workspace_id"].(json.Number)
		if !ok {
			return errors.New("invalid workspace id")
		}
		parsed, err := strconv.ParseUint(id.String(), 10, 64)
		if err != nil || parsed == 0 {
			return errors.New("invalid workspace id")
		}
	case slicerpc.VerbLaunch, slicerpc.VerbTokenQuery, slicerpc.VerbTokenReplay:
		if status != slicerpc.StatusOK && status != slicerpc.StatusPending && status != slicerpc.StatusFailed {
			return errors.New("token response has invalid result status")
		}
		result, ok := response.Result.(map[string]any)
		if !ok || (len(result) != 3 && len(result) != 5 && len(result) != 8) {
			return errors.New("invalid token result")
		}
		token, ok := result["token"].(string)
		if !ok || !slicerpc.ValidToken(token) {
			return errors.New("invalid token result identity")
		}
		var expected slicerpc.TokenPayload
		routed := false
		if request.Verb == slicerpc.VerbLaunch {
			var launch slicerpc.LaunchPayload
			if err := slicerpc.DecodePayload(request.Payload, &launch); err != nil {
				return err
			}
			expected = slicerpc.TokenPayload{Token: launch.Token, SessionName: launch.SessionName, WorkspaceName: launch.WorkspaceName}
			routed = launch.SessionName != "" || launch.WorkspaceName != ""
		} else if err := slicerpc.DecodePayload(request.Payload, &expected); err != nil {
			return err
		} else {
			routed = expected.SessionName != "" || expected.WorkspaceName != ""
		}
		if token != expected.Token {
			return errors.New("token result does not match request")
		}
		if routed {
			if expected.SessionName != slicerpc.StableSessionName(token) {
				return errors.New("invalid routed request session")
			}
			if _, err := sliceprotocol.NormalizeWorkspaceName(expected.WorkspaceName); err != nil {
				return errors.New("invalid routed request workspace")
			}
			if status == slicerpc.StatusOK && len(result) != 8 {
				return errors.New("successful routed response is incomplete")
			}
			if status != slicerpc.StatusOK && len(result) != 5 {
				return errors.New("pending or failed routed response is incomplete")
			}
		}
		terminal, ok := result["host_terminal_id"].(string)
		if !ok || !strings.HasPrefix(terminal, "term_") || !slicerpc.ValidToken(terminal) {
			return errors.New("invalid host terminal identity")
		}
		launchStatus, ok := result["launch_status"].(string)
		if !ok {
			return errors.New("invalid launch status")
		}
		expectedStatus := map[slicerpc.OutcomeStatus]string{slicerpc.StatusOK: string(slicerpc.TokenLaunched), slicerpc.StatusPending: string(slicerpc.TokenPending), slicerpc.StatusFailed: string(slicerpc.TokenFailed)}[status]
		if launchStatus != expectedStatus {
			return errors.New("launch status does not match outcome")
		}
		if len(result) >= 5 {
			session, ok := result["session_name"].(string)
			if !ok || session != slicerpc.StableSessionName(token) {
				return errors.New("invalid session result")
			}
			workspace, ok := result["workspace_name"].(string)
			if !ok {
				return errors.New("invalid workspace result")
			}
			if _, err := sliceprotocol.NormalizeWorkspaceName(workspace); err != nil {
				return errors.New("invalid workspace result")
			}
			if routed && (session != expected.SessionName || workspace != expected.WorkspaceName) {
				return errors.New("routed metadata response mismatch")
			}
		}
		if len(result) == 8 {
			source, ok := result["source_id"].(string)
			if !ok || !strings.HasPrefix(source, "src_") || !slicerpc.ValidToken(source) {
				return errors.New("invalid source result")
			}
			epoch, ok := result["source_epoch"].(string)
			if !ok || !sliceprotocol.ValidUUID(epoch) {
				return errors.New("invalid source epoch result")
			}
			id, ok := result["runtime_window_id"].(json.Number)
			if !ok {
				return errors.New("invalid runtime window result")
			}
			parsed, err := strconv.ParseUint(id.String(), 10, 64)
			if err != nil || parsed == 0 {
				return errors.New("invalid runtime window result")
			}
			derived, deriveErr := sourceinventory.SourceID(epoch, parsed)
			if deriveErr != nil || derived != source {
				return errors.New("inconsistent routed source tuple")
			}
		}
	default:
		return errors.New("response verb is unsupported")
	}
	return nil
}
func validateNonResult(verb slicerpc.Verb, status slicerpc.OutcomeStatus, code string) error {
	if status == slicerpc.StatusDisconnected && code == "transport_disconnected" {
		switch verb {
		case slicerpc.VerbLiveness, slicerpc.VerbSnapshot, slicerpc.VerbTokenQuery, slicerpc.VerbTokenReplay:
			return nil
		default:
			return errors.New("disconnected outcome is invalid for mutation or unknown verb")
		}
	}
	allowed := map[slicerpc.Verb]map[string]bool{
		slicerpc.VerbLiveness:        {"invalid_payload": true, "niri_version_unavailable": true},
		slicerpc.VerbSnapshot:        {"invalid_payload": true, "niri_version_unavailable": true, "snapshot_unavailable": true},
		slicerpc.VerbWorkspaceEnsure: {"invalid_payload": true, "invalid_workspace_name": true, "niri_version_unavailable": true, "workspace_ensure_failed": true},
		slicerpc.VerbLaunch:          {"invalid_token": true, "invalid_launch_metadata": true, "launch_identity_conflict": true, "launch_unavailable": true, "token_state_unavailable": true},
		slicerpc.VerbTokenQuery:      {"invalid_token": true, "invalid_launch_metadata": true, "token_store_unavailable": true, "token_not_found": true, "token_state_unavailable": true},
		slicerpc.VerbTokenReplay:     {"invalid_token": true, "invalid_launch_metadata": true, "token_store_unavailable": true, "token_not_found": true, "token_state_unavailable": true},
	}
	if !allowed[verb][code] {
		return errors.New("RPC outcome code is invalid for request verb")
	}
	return nil
}
func slicpcZero() slicerpc.Response { return slicerpc.Response{} }

type limitedBuffer struct {
	bytes.Buffer
	max int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.max {
		return 0, errors.New("RPC response exceeds bound")
	}
	return b.Buffer.Write(p)
}
