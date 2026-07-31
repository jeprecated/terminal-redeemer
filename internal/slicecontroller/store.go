package slicecontroller

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jmo/terminal-redeemer/internal/safefile"
	"github.com/jmo/terminal-redeemer/internal/sliceprotocol"
	"golang.org/x/sys/unix"
)

var (
	ErrNotInitialized     = errors.New("slice controller is not initialized")
	ErrAlreadyInitialized = errors.New("slice controller namespace already initialized")
	ErrInvalidState       = errors.New("slice controller state is invalid")
	ErrControllerLocked   = errors.New("slice controller is already running")
)

const controllerMarker = "terminal-redeemer-slice-controller-enrolled-v1\n"
const MaxControllerStateBytes = 16 << 20

type Store struct{ root, current, marker, lockPath, socketPath string }
type Lock struct{ file *os.File }

func ControlSocketPath(stateDir string) (string, error) {
	if !filepath.IsAbs(stateDir) {
		return "", errors.New("controller state directory must be absolute")
	}
	return filepath.Join(filepath.Clean(stateDir), "slice", "controller", "control.sock"), nil
}

func NewStore(stateDir string) (*Store, error) {
	socketPath, err := ControlSocketPath(stateDir)
	if err != nil {
		return nil, err
	}
	stateDir = filepath.Clean(stateDir)
	sliceDir := filepath.Join(stateDir, "slice")
	root := filepath.Join(sliceDir, "controller")
	if err := ensureStateDirectory(stateDir); err != nil {
		return nil, err
	}
	if err := ensureStateDirectory(sliceDir); err != nil {
		return nil, err
	}
	if err := ensurePrivateDirectory(root); err != nil {
		return nil, err
	}
	if err := syncDir(root); err != nil {
		return nil, err
	}
	return &Store{root: root, current: filepath.Join(root, "current.json"), marker: filepath.Join(root, "enrolled"), lockPath: filepath.Join(root, "controller.lock"), socketPath: socketPath}, nil
}
func (s *Store) Root() string       { return s.root }
func (s *Store) SocketPath() string { return s.socketPath }

func ensureStateDirectory(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(path, 0o700); err != nil {
			return err
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 || int(stat.Uid) != os.Getuid() {
		return fmt.Errorf("unsafe controller state root %s", path)
	}
	return nil
}

func ensurePrivateDirectory(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(path, 0o700); err != nil {
			return fmt.Errorf("create private directory %s: %w", filepath.Base(path), err)
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 || int(stat.Uid) != os.Getuid() {
		return fmt.Errorf("unsafe controller directory %s", path)
	}
	return nil
}

func (s *Store) Initialize(namespace Namespace) (State, error) {
	if _, err := os.Lstat(s.marker); err == nil {
		return State{}, ErrAlreadyInitialized
	} else if !errors.Is(err, os.ErrNotExist) {
		return State{}, err
	}
	if _, err := os.Lstat(s.current); err == nil {
		return State{}, ErrAlreadyInitialized
	} else if !errors.Is(err, os.ErrNotExist) {
		return State{}, err
	}
	marker, err := os.OpenFile(s.marker, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return State{}, ErrAlreadyInitialized
		}
		return State{}, err
	}
	if _, err = io.WriteString(marker, controllerMarker); err == nil {
		err = marker.Sync()
	}
	closeErr := marker.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return State{}, err
	}
	if err := syncDir(s.root); err != nil {
		return State{}, err
	}
	id, err := randomID()
	if err != nil {
		return State{}, err
	}
	state := NewState(namespace, "controller-"+id)
	state.Audit = append(state.Audit, AuditEntry{Generation: state.Generation, At: nowUTC(), Kind: "initialized"})
	if err := s.Write(state); err != nil {
		return State{}, err
	}
	return state, nil
}

func (s *Store) Read() (State, error) {
	marker, err := safefile.ReadRegular(s.marker, len(controllerMarker), 0o777, 0o600)
	if errors.Is(err, os.ErrNotExist) {
		return State{}, ErrNotInitialized
	}
	if err != nil || string(marker) != controllerMarker {
		return State{}, fmt.Errorf("%w: invalid enrollment marker", ErrInvalidState)
	}
	payload, err := safefile.ReadRegular(s.current, MaxControllerStateBytes, 0o777, 0o600)
	if errors.Is(err, os.ErrNotExist) {
		return State{}, fmt.Errorf("%w: current authority missing after enrollment", ErrInvalidState)
	}
	if err != nil {
		return State{}, fmt.Errorf("%w: read current authority: %v", ErrInvalidState, err)
	}
	if len(payload) == 0 || len(payload) > MaxControllerStateBytes {
		return State{}, fmt.Errorf("%w: state size", ErrInvalidState)
	}
	if err := sliceprotocol.RejectDuplicateKeys(payload); err != nil {
		return State{}, fmt.Errorf("%w: hostile JSON: %v", ErrInvalidState, err)
	}
	if err := sliceprotocol.RejectUnknownFieldsExact(payload, State{}); err != nil {
		return State{}, fmt.Errorf("%w: hostile fields: %v", ErrInvalidState, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var state State
	if err := decoder.Decode(&state); err != nil {
		return State{}, fmt.Errorf("%w: decode: %v", ErrInvalidState, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return State{}, fmt.Errorf("%w: trailing JSON", ErrInvalidState)
	}
	if err := state.Validate(); err != nil {
		return State{}, fmt.Errorf("%w: %v", ErrInvalidState, err)
	}
	return state, nil
}

func safeRegular(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 && info.Mode().Perm() == 0o600 && int(stat.Uid) == os.Getuid()
}
func (s *Store) Write(state State) (err error) {
	if err := state.Compact(); err != nil {
		return err
	}
	if err := state.Validate(); err != nil {
		return err
	}
	if err := ensurePrivateDirectory(s.root); err != nil {
		return err
	}
	payload, err := json.Marshal(state)
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	if len(payload) > MaxControllerStateBytes {
		return errors.New("controller state exceeds durable read limit")
	}
	tmp, err := os.CreateTemp(s.root, ".current-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = tmp.Close()
		if err != nil {
			_ = os.Remove(tmpPath)
		}
	}()
	if err = tmp.Chmod(0o600); err != nil {
		return err
	}
	if n, writeErr := tmp.Write(payload); writeErr != nil {
		return writeErr
	} else if n != len(payload) {
		return io.ErrShortWrite
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmpPath, s.current); err != nil {
		return err
	}
	if err = syncDir(s.root); err != nil {
		return err
	}
	return nil
}

func (s *Store) Acquire() (*Lock, error) {
	if err := ensurePrivateDirectory(s.root); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(s.lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if info, e := file.Stat(); e != nil || !safeRegular(info) {
		_ = file.Close()
		return nil, errors.New("unsafe controller lock")
	}
	if err = unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, ErrControllerLocked
		}
		return nil, err
	}
	return &Lock{file: file}, nil
}
func (l *Lock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	err := unix.Flock(int(l.file.Fd()), unix.LOCK_UN)
	closeErr := l.file.Close()
	l.file = nil
	return errors.Join(err, closeErr)
}
func syncDir(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
func randomID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

var nowUTC = func() time.Time { return time.Now().UTC() }
