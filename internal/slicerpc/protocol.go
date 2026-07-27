package slicerpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"unicode/utf8"

	"github.com/jmo/terminal-redeemer/internal/sliceprotocol"
)

const SchemaVersion uint32 = 1
const MaxRequestBytes = 1 << 20
const MaxStringBytes = 4096

var safeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]*$`)

type Verb string

const (
	VerbLiveness        Verb = "liveness"
	VerbSnapshot        Verb = "snapshot"
	VerbWorkspaceEnsure Verb = "workspace_ensure"
	VerbLaunch          Verb = "launch"
	VerbTokenQuery      Verb = "token_query"
	VerbTokenReplay     Verb = "token_replay"
)

type Request struct {
	SchemaVersion        uint32          `json:"schema_version"`
	AcceptSchemaVersions []uint32        `json:"accept_schema_versions"`
	RequestID            string          `json:"request_id"`
	Verb                 Verb            `json:"verb"`
	Payload              json.RawMessage `json:"payload,omitempty"`
}

type OutcomeStatus string

const (
	StatusOK           OutcomeStatus = "ok"
	StatusInvalid      OutcomeStatus = "invalid"
	StatusUnavailable  OutcomeStatus = "unavailable"
	StatusPending      OutcomeStatus = "pending"
	StatusDisconnected OutcomeStatus = "disconnected"
	StatusFailed       OutcomeStatus = "failed"
)

type Outcome struct {
	Status OutcomeStatus `json:"status"`
	Code   string        `json:"code,omitempty"`
}
type Response struct {
	SchemaVersion           uint32   `json:"schema_version"`
	RequestID               string   `json:"request_id,omitempty"`
	Outcome                 Outcome  `json:"outcome"`
	Result                  any      `json:"result,omitempty"`
	SupportedSchemaVersions []uint32 `json:"supported_schema_versions,omitempty"`
}

type LaunchPayload struct {
	Token         string `json:"token"`
	SessionName   string `json:"session_name,omitempty"`
	WorkspaceName string `json:"workspace_name,omitempty"`
}
type TokenPayload struct {
	Token         string `json:"token"`
	SessionName   string `json:"session_name,omitempty"`
	WorkspaceName string `json:"workspace_name,omitempty"`
}
type WorkspaceEnsurePayload struct {
	Name string `json:"name"`
}

func DecodeRequestContext(ctx context.Context, reader io.Reader) (Request, error) {
	closer, ok := reader.(io.Closer)
	if !ok {
		return Request{}, errors.New("context-bounded RPC input must be closable")
	}
	stop := context.AfterFunc(ctx, func() { _ = closer.Close() })
	defer stop()
	request, err := DecodeRequest(reader)
	if ctx.Err() != nil {
		return Request{}, ctx.Err()
	}
	return request, err
}

func DecodeRequest(reader io.Reader) (Request, error) {
	limited := io.LimitReader(reader, MaxRequestBytes+1)
	payload, err := io.ReadAll(limited)
	if err != nil {
		return Request{}, err
	}
	if len(payload) == 0 || len(payload) > MaxRequestBytes {
		return Request{}, errors.New("RPC request is empty or exceeds bound")
	}
	if !utf8.Valid(payload) {
		return Request{}, errors.New("RPC request is not valid UTF-8")
	}
	if err := sliceprotocol.RejectDuplicateKeys(payload); err != nil {
		return Request{}, err
	}
	if err := sliceprotocol.RejectUnknownFieldsExact(payload, Request{}); err != nil {
		return Request{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var request Request
	if err := decoder.Decode(&request); err != nil {
		return Request{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Request{}, errors.New("RPC request has trailing JSON")
	}
	if !safeID.MatchString(request.RequestID) || len(request.RequestID) > 128 {
		return Request{}, errors.New("invalid request_id")
	}
	if request.SchemaVersion != SchemaVersion {
		return Request{}, errors.New("unsupported schema version")
	}
	if len(request.AcceptSchemaVersions) == 0 {
		request.AcceptSchemaVersions = []uint32{request.SchemaVersion}
	}
	accepted := false
	for _, version := range request.AcceptSchemaVersions {
		if version == SchemaVersion {
			accepted = true
		}
	}
	if !accepted {
		return Request{}, errors.New("unsupported schema version")
	}
	switch request.Verb {
	case VerbLiveness, VerbSnapshot, VerbWorkspaceEnsure, VerbLaunch, VerbTokenQuery, VerbTokenReplay:
	default:
		return Request{}, fmt.Errorf("unsupported RPC verb %q", request.Verb)
	}
	return request, nil
}

func DecodePayload(raw json.RawMessage, target any) error {
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	if len(raw) > MaxRequestBytes || !utf8.Valid(raw) {
		return errors.New("invalid RPC payload")
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
		return errors.New("RPC payload has trailing JSON")
	}
	return nil
}

func ValidToken(token string) bool { return safeID.MatchString(token) && len(token) <= 128 }
func EncodeResponse(writer io.Writer, response Response) error {
	return json.NewEncoder(writer).Encode(response)
}
