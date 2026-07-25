package slicelaunch

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/jmo/terminal-redeemer/internal/safefile"
	"github.com/jmo/terminal-redeemer/internal/sliceprotocol"
	"github.com/jmo/terminal-redeemer/internal/slicerpc"
	"github.com/jmo/terminal-redeemer/internal/sourceinventory"
)

const (
	StorageVersion = 1
	MaxIntentBytes = 64 << 10
	MaxIntentFiles = 1024
)

var safeToken = regexp.MustCompile(`^[a-f0-9]{64}$`)
var safeSession = regexp.MustCompile(`^tr-[a-z2-7]{32}$`)

var ErrIntentNotFound = errors.New("slice launch intent not found")

type IntentStatus string

const (
	IntentPending      IntentStatus = "pending"
	IntentLaunched     IntentStatus = "launched"
	IntentDisconnected IntentStatus = "disconnected"
	IntentFailed       IntentStatus = "failed"
)

type Mode struct {
	StorageVersion int       `json:"storage_version"`
	Enabled        bool      `json:"enabled"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type Intent struct {
	StorageVersion  int          `json:"storage_version"`
	Token           string       `json:"token"`
	SessionName     string       `json:"session_name"`
	WorkspaceName   string       `json:"workspace_name"`
	Status          IntentStatus `json:"status"`
	HostTerminalID  string       `json:"host_terminal_id,omitempty"`
	SourceID        string       `json:"source_id,omitempty"`
	SourceEpoch     string       `json:"source_epoch,omitempty"`
	RuntimeWindowID uint64       `json:"runtime_window_id,omitempty"`
	Attempt         int          `json:"attempt"`
	CreatedAt       time.Time    `json:"created_at"`
	UpdatedAt       time.Time    `json:"updated_at"`
	RetryExpiresAt  time.Time    `json:"retry_expires_at"`
}

func (i Intent) Validate() error {
	if i.StorageVersion != StorageVersion || !safeToken.MatchString(i.Token) || !safeSession.MatchString(i.SessionName) || i.SessionName != SessionName(i.Token) || !utf8.ValidString(i.WorkspaceName) || strings.TrimSpace(i.WorkspaceName) == "" || len(i.WorkspaceName) > 255 || strings.ContainsAny(i.WorkspaceName, "\x00\r\n") || i.CreatedAt.IsZero() || i.UpdatedAt.IsZero() || i.RetryExpiresAt.IsZero() || !i.RetryExpiresAt.After(i.CreatedAt) || i.Attempt < 0 || i.Attempt > 100 {
		return errors.New("invalid routed launch intent")
	}
	switch i.Status {
	case IntentPending, IntentLaunched, IntentDisconnected, IntentFailed:
	default:
		return errors.New("invalid routed launch status")
	}
	for _, v := range []string{i.HostTerminalID, i.SourceID, i.SourceEpoch} {
		if !utf8.ValidString(v) || len(v) > 128 || strings.ContainsAny(v, "\x00\r\n/") {
			return errors.New("invalid routed launch identity")
		}
	}
	if i.Status == IntentLaunched && (i.HostTerminalID == "" || i.SourceID == "" || i.SourceEpoch == "" || i.RuntimeWindowID == 0) {
		return errors.New("launched intent lacks committed identity")
	}
	present := 0
	if i.SourceID != "" {
		present++
	}
	if i.SourceEpoch != "" {
		present++
	}
	if i.RuntimeWindowID != 0 {
		present++
	}
	if present != 0 && present != 3 {
		return errors.New("incomplete routed source tuple")
	}
	if present == 3 {
		derived, err := sourceinventory.SourceID(i.SourceEpoch, i.RuntimeWindowID)
		if err != nil || derived != i.SourceID {
			return errors.New("invalid routed source tuple")
		}
	}
	return nil
}

func SessionName(token string) string { return slicerpc.StableSessionName(token) }

func NewToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

type Store struct {
	stateDir, sliceDir, root, intents, marker string
	fresh                                     bool
}

func NewStore(stateDir string) (*Store, error) {
	if strings.TrimSpace(stateDir) == "" {
		return nil, errors.New("state directory required")
	}
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		return nil, err
	}
	stateInfo, err := os.Lstat(stateDir)
	if err != nil {
		return nil, err
	}
	stateSys, ok := stateInfo.Sys().(*syscall.Stat_t)
	if !ok || !stateInfo.IsDir() || stateInfo.Mode()&os.ModeSymlink != 0 || stateInfo.Mode().Perm()&0022 != 0 || int(stateSys.Uid) != os.Getuid() {
		return nil, errors.New("unsafe routed launch state root")
	}
	sliceDir := filepath.Join(stateDir, "slice")
	root := filepath.Join(sliceDir, "launch")
	marker := filepath.Join(sliceDir, "launch.enrolled")
	_, rootErr := os.Lstat(root)
	rootExisted := rootErr == nil
	if rootErr != nil && !errors.Is(rootErr, os.ErrNotExist) {
		return nil, rootErr
	}
	_, markerErr := os.Lstat(marker)
	markerExisted := markerErr == nil
	if markerErr != nil && !errors.Is(markerErr, os.ErrNotExist) {
		return nil, markerErr
	}
	if markerExisted && !rootExisted {
		return nil, errors.New("routed launch authority missing after enrollment")
	}
	if rootExisted && !markerExisted {
		return nil, errors.New("unmarked routed launch authority")
	}
	for _, d := range []string{sliceDir, root, filepath.Join(root, "intents")} {
		if err := mkdirPrivate(d); err != nil {
			return nil, err
		}
	}
	if !markerExisted {
		if err := writeAtomic(marker, map[string]any{"storage_version": StorageVersion, "enrolled": true}); err != nil {
			return nil, err
		}
	}
	store := &Store{stateDir: stateDir, sliceDir: sliceDir, root: root, intents: filepath.Join(root, "intents"), marker: marker, fresh: !markerExisted}
	if err := store.verify(); err != nil {
		return nil, err
	}
	return store, nil
}
func mkdirPrivate(path string) error {
	if err := os.Mkdir(path, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	return checkPrivate(path)
}
func checkPrivate(path string) error {
	st, err := os.Lstat(path)
	if err != nil {
		return err
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 || st.Mode().Perm() != 0700 || int(sys.Uid) != os.Getuid() {
		return errors.New("unsafe routed launch state directory")
	}
	return nil
}
func (s *Store) verify() error {
	info, err := os.Lstat(s.stateDir)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0022 != 0 || int(stat.Uid) != os.Getuid() {
		return errors.New("unsafe routed launch state root")
	}
	for _, path := range []string{s.sliceDir, s.root, s.intents} {
		if err := checkPrivate(path); err != nil {
			return err
		}
	}
	var marker struct {
		StorageVersion int  `json:"storage_version"`
		Enrolled       bool `json:"enrolled"`
	}
	if err := decodeFile(s.marker, &marker); err != nil || marker.StorageVersion != StorageVersion || !marker.Enrolled {
		return errors.New("invalid routed launch enrollment")
	}
	return nil
}
func (s *Store) modePath() string { return filepath.Join(s.root, "mode.json") }
func (s *Store) lock(token string) (*os.File, error) {
	if _, err := s.intentPath(token); err != nil {
		return nil, err
	}
	return s.lockGlobal()
}
func (s *Store) lockGlobal() (*os.File, error) {
	if err := s.verify(); err != nil {
		return nil, err
	}
	lockPath := filepath.Join(s.root, "intent.lock")
	fd, err := syscall.Open(lockPath, syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), lockPath)
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || int(stat.Uid) != os.Getuid() {
		file.Close()
		return nil, errors.New("unsafe routed launch lock")
	}
	if err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}
func unlock(file *os.File) {
	if file == nil {
		return
	}
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	_ = file.Close()
}
func (s *Store) intentPath(token string) (string, error) {
	if !safeToken.MatchString(token) {
		return "", errors.New("invalid token")
	}
	sum := sha256.Sum256([]byte(token))
	return filepath.Join(s.intents, hex.EncodeToString(sum[:])+".json"), nil
}
func writeAtomic(path string, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".write-*.tmp")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err != nil {
		return err
	}
	if _, err = f.Write(payload); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
func writeExclusive(path string, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".intent-*.tmp")
	if err != nil {
		return err
	}
	name := f.Name()
	defer func() { _ = f.Close(); _ = os.Remove(name) }()
	if err = f.Chmod(0600); err != nil {
		return err
	}
	if _, err = f.Write(payload); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Link(name, path); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
func decodeFile(path string, dst any) error {
	payload, err := safefile.ReadRegular(path, MaxIntentBytes, 0o777, 0o600)
	if err != nil {
		return err
	}
	if len(payload) == 0 || len(payload) > MaxIntentBytes {
		return errors.New("invalid routed launch state size")
	}
	if err := sliceprotocol.RejectDuplicateKeys(payload); err != nil {
		return fmt.Errorf("invalid routed launch state JSON: %w", err)
	}
	if err := sliceprotocol.RejectUnknownFieldsExact(payload, dst); err != nil {
		return fmt.Errorf("invalid routed launch state fields: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.DisallowUnknownFields()
	if err = dec.Decode(dst); err != nil {
		return err
	}
	var trailing any
	if err = dec.Decode(&trailing); err != io.EOF {
		return errors.New("trailing routed launch state")
	}
	return nil
}
func (s *Store) Mode(defaultEnabled bool) (Mode, error) {
	if err := s.verify(); err != nil {
		return Mode{}, err
	}
	var m Mode
	err := decodeFile(s.modePath(), &m)
	if errors.Is(err, os.ErrNotExist) {
		if !s.fresh {
			return Mode{}, errors.New("leech mode authority missing after enrollment")
		}
		m = Mode{StorageVersion: StorageVersion, Enabled: defaultEnabled, UpdatedAt: time.Now().UTC()}
		err = writeAtomic(s.modePath(), m)
		if err == nil {
			s.fresh = false
		}
	}
	if err != nil {
		return Mode{}, err
	}
	if m.StorageVersion != StorageVersion || m.UpdatedAt.IsZero() {
		return Mode{}, errors.New("invalid leech mode state")
	}
	return m, nil
}
func (s *Store) SetMode(enabled bool) (Mode, error) {
	if err := s.verify(); err != nil {
		return Mode{}, err
	}
	if _, err := os.Lstat(s.modePath()); errors.Is(err, os.ErrNotExist) && !s.fresh {
		return Mode{}, errors.New("leech mode authority missing after enrollment")
	}
	m := Mode{StorageVersion: StorageVersion, Enabled: enabled, UpdatedAt: time.Now().UTC()}
	err := writeAtomic(s.modePath(), m)
	if err == nil {
		s.fresh = false
	}
	return m, err
}
func (s *Store) compact() error {
	if err := s.verify(); err != nil {
		return err
	}
	entries, err := os.ReadDir(s.intents)
	if err != nil {
		return err
	}
	type old struct {
		path string
		at   time.Time
	}
	terminal := []old{}
	active := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		var i Intent
		if err := decodeFile(filepath.Join(s.intents, entry.Name()), &i); err != nil {
			return err
		}
		if err := i.Validate(); err != nil {
			return err
		}
		if i.Status == IntentPending || i.Status == IntentDisconnected {
			active++
		} else {
			terminal = append(terminal, old{filepath.Join(s.intents, entry.Name()), i.UpdatedAt})
		}
	}
	if active >= MaxIntentFiles {
		return errors.New("non-prunable routed launch intents exceed bound")
	}
	sort.Slice(terminal, func(a, b int) bool {
		if terminal[a].at.Equal(terminal[b].at) {
			return terminal[a].path < terminal[b].path
		}
		return terminal[a].at.Before(terminal[b].at)
	})
	remove := active + len(terminal) - (MaxIntentFiles - 1)
	removed := false
	for remove > 0 && len(terminal) > 0 {
		if err := os.Remove(terminal[0].path); err != nil {
			return err
		}
		removed = true
		terminal = terminal[1:]
		remove--
	}
	if removed {
		dir, err := os.Open(s.intents)
		if err != nil {
			return err
		}
		syncErr := dir.Sync()
		_ = dir.Close()
		if syncErr != nil {
			return syncErr
		}
	}
	if remove > 0 {
		return errors.New("routed launch intent capacity exhausted")
	}
	return nil
}
func (s *Store) Create(workspace string, retryWindow time.Duration, now time.Time) (Intent, error) {
	if err := s.compact(); err != nil {
		return Intent{}, err
	}
	if retryWindow <= 0 {
		return Intent{}, errors.New("retry window must be positive")
	}
	token, err := NewToken()
	if err != nil {
		return Intent{}, err
	}
	i := Intent{StorageVersion: StorageVersion, Token: token, SessionName: SessionName(token), WorkspaceName: strings.TrimSpace(workspace), Status: IntentPending, CreatedAt: now.UTC(), UpdatedAt: now.UTC(), RetryExpiresAt: now.UTC().Add(retryWindow)}
	if err = i.Validate(); err != nil {
		return Intent{}, err
	}
	path, _ := s.intentPath(token)
	return i, writeExclusive(path, i)
}
func (s *Store) Read(token string) (Intent, error) {
	if err := s.verify(); err != nil {
		return Intent{}, err
	}
	path, err := s.intentPath(token)
	if err != nil {
		return Intent{}, err
	}
	var i Intent
	err = decodeFile(path, &i)
	if errors.Is(err, os.ErrNotExist) {
		return Intent{}, ErrIntentNotFound
	}
	if err != nil {
		return Intent{}, err
	}
	if err = i.Validate(); err != nil {
		return Intent{}, err
	}
	if i.Token != token {
		return Intent{}, errors.New("intent token mismatch")
	}
	return i, nil
}
func (s *Store) Write(i Intent) error {
	if err := s.verify(); err != nil {
		return err
	}
	if err := i.Validate(); err != nil {
		return err
	}
	path, err := s.intentPath(i.Token)
	if err != nil {
		return err
	}
	if _, err = s.Read(i.Token); err != nil {
		return err
	}
	return writeAtomic(path, i)
}

type Remote interface {
	Call(context.Context, slicerpc.Request) (slicerpc.Response, error)
}
type Workspace interface {
	Current(context.Context) (string, error)
}
type Selection interface{ Selected(string) (bool, error) }
type LocalLauncher interface{ Launch(context.Context) error }
type Handoff interface {
	Send(context.Context, Intent) error
}

type Result struct {
	Routed bool    `json:"routed"`
	Local  bool    `json:"local"`
	Intent *Intent `json:"intent,omitempty"`
	Code   string  `json:"code"`
}
type Router struct {
	Store                                   *Store
	DefaultEnabled                          bool
	Workspace                               Workspace
	Selection                               Selection
	Local                                   LocalLauncher
	Remote                                  Remote
	Handoff                                 Handoff
	RetryWindow, InitialBackoff, MaxBackoff time.Duration
	MaxAttempts                             int
	Now                                     func() time.Time
	Sleep                                   func(context.Context, time.Duration) error
}

func (r Router) now() time.Time {
	if r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}
func (r Router) Route(ctx context.Context) (Result, error) {
	m, err := r.Store.Mode(r.DefaultEnabled)
	if err != nil {
		return Result{}, err
	}
	if !m.Enabled {
		if r.Local == nil {
			return Result{}, errors.New("local launcher unavailable")
		}
		if err := r.Local.Launch(ctx); err != nil {
			return Result{}, err
		}
		return Result{Local: true, Code: "local"}, nil
	}
	workspace, err := r.Workspace.Current(ctx)
	if err != nil {
		return Result{}, err
	}
	selected, err := r.Selection.Selected(workspace)
	if err != nil {
		return Result{}, err
	}
	if !selected {
		if r.Local == nil {
			return Result{}, errors.New("local launcher unavailable")
		}
		if err := r.Local.Launch(ctx); err != nil {
			return Result{}, err
		}
		return Result{Local: true, Code: "local"}, nil
	}
	lock, err := r.Store.lockGlobal()
	if err != nil {
		return Result{}, err
	}
	defer unlock(lock)
	i, err := r.Store.Create(workspace, r.RetryWindow, r.now())
	if err != nil {
		return Result{}, err
	}
	if r.Handoff != nil {
		if err := r.Handoff.Send(ctx, i); err != nil {
			return Result{Routed: true, Intent: &i, Code: "controller_unavailable"}, err
		}
	}
	return r.drive(ctx, i, true)
}
func (r Router) Reconnect(ctx context.Context, token string) (Result, error) {
	lock, err := r.Store.lock(token)
	if err != nil {
		return Result{}, err
	}
	defer unlock(lock)
	i, err := r.Store.Read(token)
	if err != nil {
		return Result{}, err
	}
	if i.Status == IntentLaunched {
		if r.Handoff == nil {
			return Result{Routed: true, Intent: &i, Code: "launched"}, nil
		}
		if err := r.Handoff.Send(ctx, i); err != nil {
			return Result{Routed: true, Intent: &i, Code: "controller_unavailable"}, err
		}
		return Result{Routed: true, Intent: &i, Code: "launched"}, nil
	}
	if i.Status != IntentPending && i.Status != IntentDisconnected {
		return Result{}, errors.New("intent is not reconnectable")
	}
	// A pending intent is an interrupted in-budget attempt. Resuming it after a
	// process/store reconstruction must retain its absolute deadline and consumed
	// attempts. An explicitly disconnected intent is the operator boundary that
	// starts a new retry budget.
	if i.Status == IntentDisconnected {
		i.Status = IntentPending
		i.Attempt = 0
		i.UpdatedAt = r.now()
		i.RetryExpiresAt = i.UpdatedAt.Add(r.RetryWindow)
		if err = r.Store.Write(i); err != nil {
			return Result{}, err
		}
	}
	if r.Handoff != nil {
		if err := r.Handoff.Send(ctx, i); err != nil {
			return Result{Routed: true, Intent: &i, Code: "controller_unavailable"}, err
		}
	}
	return r.drive(ctx, i, false)
}
func (r Router) drive(ctx context.Context, i Intent, first bool) (Result, error) {
	expired := func() bool { return !r.now().Before(i.RetryExpiresAt) }
	if expired() {
		return r.finish(i, IntentDisconnected, "disconnected", errors.New("routed launch retry exhausted"))
	}
	if r.Remote == nil {
		return Result{}, errors.New("remote transport unavailable")
	}
	maxAttempts := r.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	attempts := maxAttempts - i.Attempt
	if attempts < 0 {
		attempts = 0
	}
	delay := r.InitialBackoff
	if delay <= 0 {
		delay = time.Millisecond
	}
	max := r.MaxBackoff
	if max < delay {
		max = delay
	}
	for n := 0; n < attempts; n++ {
		// RetryExpiresAt is durable authority, not a post-attempt accounting hint.
		// A reconstructed pending intent and every post-backoff iteration must
		// remain inside its absolute window.
		if expired() {
			return r.finish(i, IntentDisconnected, "disconnected", errors.New("routed launch retry exhausted"))
		}
		if err := ctx.Err(); err != nil {
			return r.finish(i, IntentPending, "cancelled", err)
		}
		verb := slicerpc.VerbTokenReplay
		if first && n == 0 {
			verb = slicerpc.VerbLaunch
		}
		payload := any(slicerpc.TokenPayload{Token: i.Token, SessionName: i.SessionName, WorkspaceName: i.WorkspaceName})
		if verb == slicerpc.VerbLaunch {
			payload = slicerpc.LaunchPayload{Token: i.Token, SessionName: i.SessionName, WorkspaceName: i.WorkspaceName}
		}
		raw, _ := json.Marshal(payload)
		req := slicerpc.Request{SchemaVersion: slicerpc.SchemaVersion, AcceptSchemaVersions: []uint32{slicerpc.SchemaVersion}, RequestID: fmt.Sprintf("route-%d-%d", r.now().UnixNano(), n), Verb: verb, Payload: raw}
		// Recheck immediately before the only remote side effect. Request
		// construction and injected clocks must not open a post-deadline call.
		if expired() {
			return r.finish(i, IntentDisconnected, "disconnected", errors.New("routed launch retry exhausted"))
		}
		resp, err := r.Remote.Call(ctx, req)
		i.Attempt++
		i.UpdatedAt = r.now()
		if err == nil {
			applyResponse(&i, resp)
			if (resp.Outcome.Status == slicerpc.StatusUnavailable && (resp.Outcome.Code == "token_not_found" || (verb == slicerpc.VerbLaunch && resp.Outcome.Code == "launch_unavailable"))) ||
				(resp.Outcome.Status == slicerpc.StatusInvalid && resp.Outcome.Code == "invalid_launch_metadata") ||
				(resp.Outcome.Status == slicerpc.StatusFailed && resp.Outcome.Code == "launch_identity_conflict") {
				i.Status = IntentFailed
			}
			if writeErr := r.Store.Write(i); writeErr != nil {
				return Result{Routed: true, Intent: &i, Code: "intent_persist_failed"}, writeErr
			}
			if r.Handoff != nil {
				if handoffErr := r.Handoff.Send(ctx, i); handoffErr != nil {
					return Result{Routed: true, Intent: &i, Code: "controller_unavailable"}, handoffErr
				}
			}
			if i.Status == IntentLaunched {
				return Result{Routed: true, Intent: &i, Code: "launched"}, nil
			}
			if i.Status == IntentFailed {
				return Result{Routed: true, Intent: &i, Code: "failed"}, errors.New("host definitively did not create launch")
			}
		} else {
			if writeErr := r.Store.Write(i); writeErr != nil {
				return Result{Routed: true, Intent: &i, Code: "intent_persist_failed"}, writeErr
			}
		}
		if !r.now().Before(i.RetryExpiresAt) || n+1 >= attempts {
			break
		}
		sleep := r.Sleep
		if sleep == nil {
			sleep = func(ctx context.Context, d time.Duration) error {
				t := time.NewTimer(d)
				defer t.Stop()
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-t.C:
					return nil
				}
			}
		}
		if err := sleep(ctx, delay); err != nil {
			return r.finish(i, IntentPending, "cancelled", err)
		}
		delay *= 2
		if delay > max {
			delay = max
		}
	}
	return r.finish(i, IntentDisconnected, "disconnected", errors.New("routed launch retry exhausted"))
}
func (r Router) finish(i Intent, status IntentStatus, code string, cause error) (Result, error) {
	i.Status = status
	i.UpdatedAt = r.now()
	if err := r.Store.Write(i); err != nil {
		return Result{Routed: true, Intent: &i, Code: "intent_persist_failed"}, err
	}
	if r.Handoff != nil {
		_ = r.Handoff.Send(context.Background(), i)
	}
	return Result{Routed: true, Intent: &i, Code: code}, cause
}
func applyResponse(i *Intent, r slicerpc.Response) {
	if m, ok := r.Result.(map[string]any); ok {
		if v, ok := m["host_terminal_id"].(string); ok {
			i.HostTerminalID = v
		}
		sourceID, _ := m["source_id"].(string)
		epoch, _ := m["source_epoch"].(string)
		runtimeID := uint64(0)
		switch v := m["runtime_window_id"].(type) {
		case json.Number:
			runtimeID, _ = strconv.ParseUint(v.String(), 10, 64)
		case float64:
			if v > 0 {
				runtimeID = uint64(v)
			}
		case uint64:
			runtimeID = v
		}
		if sourceID != "" && epoch != "" && runtimeID != 0 {
			derived, err := sourceinventory.SourceID(epoch, runtimeID)
			if err == nil && derived == sourceID {
				i.SourceID = sourceID
				i.SourceEpoch = epoch
				i.RuntimeWindowID = runtimeID
			}
		}
	}
	switch r.Outcome.Status {
	case slicerpc.StatusOK:
		if i.HostTerminalID != "" && i.SourceID != "" && i.SourceEpoch != "" && i.RuntimeWindowID != 0 {
			i.Status = IntentLaunched
		} else {
			i.Status = IntentPending
		}
	case slicerpc.StatusFailed:
		i.Status = IntentFailed
	default:
		i.Status = IntentPending
	}
}
