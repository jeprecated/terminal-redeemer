package sourceinventory

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/jmo/terminal-redeemer/internal/safefile"
	"github.com/jmo/terminal-redeemer/internal/sliceprotocol"
)

var ErrStateNotFound = errors.New("source inventory state not found")
var ErrStateInvalid = errors.New("source inventory state invalid")
var ErrNamespaceUsed = errors.New("source inventory namespace was already enrolled")

const enrollmentMarkerContents = "terminal-redeemer-source-inventory-enrolled-v1\n"
const MaxStateBytes = 16 << 20

type Store struct {
	root       string
	path       string
	markerPath string
	syncFile   func(*os.File) error
	syncDir    func(string) error
	rename     func(string, string) error
}

func NewStore(stateDir string) (*Store, error) {
	sliceDir := filepath.Join(stateDir, "slice")
	root := filepath.Join(sliceDir, "source-inventory")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create source inventory state directory: %w", err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return nil, fmt.Errorf("secure source inventory state directory: %w", err)
	}
	for _, directory := range []string{stateDir, sliceDir, root} {
		if err := syncDirectory(directory); err != nil {
			return nil, fmt.Errorf("sync source inventory directory: %w", err)
		}
	}
	return &Store{root: root, path: filepath.Join(root, "current.json"), markerPath: filepath.Join(root, "enrolled"), syncFile: func(file *os.File) error { return file.Sync() }, syncDir: syncDirectory, rename: os.Rename}, nil
}

func (store *Store) Root() string       { return store.root }
func (store *Store) Path() string       { return store.path }
func (store *Store) MarkerPath() string { return store.markerPath }

func (store *Store) EnrollmentMarkerPresent() (bool, error) {
	payload, err := safefile.ReadRegular(store.markerPath, len(enrollmentMarkerContents), 0o077, 0)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("%w: unsafe or oversized enrollment marker: %v", ErrStateInvalid, err)
	}
	if string(payload) != enrollmentMarkerContents {
		return false, fmt.Errorf("%w: invalid enrollment marker", ErrStateInvalid)
	}
	return true, nil
}

func (store *Store) CreateEnrollmentMarker() (err error) {
	file, err := os.OpenFile(store.markerPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return ErrNamespaceUsed
	}
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	written, err := io.WriteString(file, enrollmentMarkerContents)
	if err != nil {
		return err
	}
	if written != len(enrollmentMarkerContents) {
		return io.ErrShortWrite
	}
	if err := store.syncFile(file); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := store.syncDir(store.root); err != nil {
		return err
	}
	return nil
}

func (store *Store) Read() (State, error) {
	payload, err := safefile.ReadRegular(store.path, MaxStateBytes, 0o077, 0)
	if errors.Is(err, os.ErrNotExist) {
		return State{}, ErrStateNotFound
	}
	if err != nil {
		return State{}, fmt.Errorf("%w: unsafe or oversized state file: %v", ErrStateInvalid, err)
	}
	if len(payload) == 0 || len(payload) > MaxStateBytes {
		return State{}, fmt.Errorf("%w: state size", ErrStateInvalid)
	}
	if err := sliceprotocol.RejectDuplicateKeys(payload); err != nil {
		return State{}, fmt.Errorf("%w: hostile JSON: %v", ErrStateInvalid, err)
	}
	if err := sliceprotocol.RejectUnknownFieldsExact(payload, State{}); err != nil {
		return State{}, fmt.Errorf("%w: hostile fields: %v", ErrStateInvalid, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var state State
	if err := decoder.Decode(&state); err != nil {
		return State{}, fmt.Errorf("%w: decode: %v", ErrStateInvalid, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return State{}, fmt.Errorf("%w: trailing JSON", ErrStateInvalid)
	}
	if err := state.Validate(); err != nil {
		return State{}, fmt.Errorf("%w: %v", ErrStateInvalid, err)
	}
	return state, nil
}

func (store *Store) Write(state State) (err error) {
	if err := state.Validate(); err != nil {
		return fmt.Errorf("validate source inventory state: %w", err)
	}
	payload, err := json.Marshal(state)
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	if len(payload) > MaxStateBytes {
		return fmt.Errorf("source inventory state exceeds durable read limit")
	}
	tmp, err := os.CreateTemp(store.root, ".current-*.tmp")
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
	if err := tmp.Chmod(0o600); err != nil {
		return err
	}
	written, err := tmp.Write(payload)
	if err != nil {
		return err
	}
	if written != len(payload) {
		return io.ErrShortWrite
	}
	if err := store.syncFile(tmp); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := store.rename(tmpPath, store.path); err != nil {
		return err
	}
	if err := store.syncDir(store.root); err != nil {
		return err
	}
	return nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
