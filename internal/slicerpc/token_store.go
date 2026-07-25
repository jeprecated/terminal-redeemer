package slicerpc

import (
	"bytes"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/jmo/terminal-redeemer/internal/safefile"
	"github.com/jmo/terminal-redeemer/internal/sliceprotocol"
)

var ErrTokenNotFound = errors.New("slice launch token not found")
var ErrTokenInvalid = errors.New("slice launch token state invalid")

const MaxTokenRecords = 4096
const MaxTokenRecordBytes = 64 << 10

type TokenStatus string

const (
	TokenPending  TokenStatus = "pending"
	TokenLaunched TokenStatus = "launched"
	TokenFailed   TokenStatus = "failed"
)

type TokenRecord struct {
	StorageVersion         uint32      `json:"storage_version"`
	Token                  string      `json:"token"`
	HostTerminalID         string      `json:"host_terminal_id"`
	Status                 TokenStatus `json:"status"`
	SessionName            string      `json:"session_name,omitempty"`
	WorkspaceName          string      `json:"workspace_name,omitempty"`
	Stage                  string      `json:"stage,omitempty"`
	PreparedSocketPath     string      `json:"prepared_socket_path,omitempty"`
	PreparedMarkerDevice   uint64      `json:"prepared_marker_device,omitempty"`
	PreparedMarkerInode    uint64      `json:"prepared_marker_inode,omitempty"`
	PreparedSocketDevice   uint64      `json:"prepared_socket_device,omitempty"`
	PreparedSocketInode    uint64      `json:"prepared_socket_inode,omitempty"`
	KittyPID               int         `json:"kitty_pid,omitempty"`
	NiriWindowID           uint64      `json:"niri_window_id,omitempty"`
	SourceID               string      `json:"source_id,omitempty"`
	SourceEpoch            string      `json:"source_epoch,omitempty"`
	TransactionEpoch       string      `json:"transaction_epoch,omitempty"`
	TransactionFingerprint string      `json:"transaction_fingerprint,omitempty"`
	CreatedAt              time.Time   `json:"created_at"`
	UpdatedAt              time.Time   `json:"updated_at"`
}

func (r TokenRecord) Validate() error {
	if r.StorageVersion != 1 || !ValidToken(r.Token) || !safeID.MatchString(r.HostTerminalID) || len(r.HostTerminalID) > 128 || r.CreatedAt.IsZero() || r.UpdatedAt.IsZero() {
		return ErrTokenInvalid
	}
	switch r.Status {
	case TokenPending, TokenLaunched, TokenFailed:
	default:
		return ErrTokenInvalid
	}
	if r.SessionName != "" {
		if !safeID.MatchString(r.SessionName) || len(r.SessionName) > 64 || !utf8.ValidString(r.WorkspaceName) || r.WorkspaceName == "" || len(r.WorkspaceName) > 255 || strings.ContainsAny(r.WorkspaceName, "\x00\r\n") {
			return ErrTokenInvalid
		}
		switch r.Stage {
		case "pending", "session_starting", "session_created", "socket_planned", "kitty_prepared", "kitty_starting", "kitty_started", "placed", "proof_committed", "committed":
		default:
			return ErrTokenInvalid
		}
		planned := r.PreparedSocketPath != "" || r.PreparedSocketDevice != 0 || r.PreparedSocketInode != 0
		if planned != (r.PreparedSocketPath != "" && utf8.ValidString(r.PreparedSocketPath) && filepath.IsAbs(r.PreparedSocketPath) && filepath.Clean(r.PreparedSocketPath) == r.PreparedSocketPath && len(r.PreparedSocketPath) <= 4096 && !strings.ContainsAny(r.PreparedSocketPath, "\x00\r\n") && r.PreparedSocketDevice != 0 && r.PreparedSocketInode != 0) {
			return ErrTokenInvalid
		}
		marked := r.PreparedMarkerDevice != 0 || r.PreparedMarkerInode != 0
		if marked != (r.PreparedMarkerDevice != 0 && r.PreparedMarkerInode != 0) {
			return ErrTokenInvalid
		}
		requiresPlan := r.Stage == "socket_planned" || r.Stage == "kitty_prepared" || r.Stage == "kitty_starting" || r.Stage == "kitty_started" || r.Stage == "placed" || r.Stage == "proof_committed" || r.Stage == "committed"
		requiresMarker := r.Stage == "kitty_prepared" || r.Stage == "kitty_starting" || r.Stage == "kitty_started" || r.Stage == "placed" || r.Stage == "proof_committed" || r.Stage == "committed"
		if requiresPlan != planned || requiresMarker != marked {
			return ErrTokenInvalid
		}
		if (r.Stage == "kitty_started" || r.Stage == "placed" || r.Stage == "proof_committed" || r.Stage == "committed") && (r.NiriWindowID == 0 || r.KittyPID <= 0) {
			return ErrTokenInvalid
		}
		if r.SourceID != "" && !safeID.MatchString(r.SourceID) {
			return ErrTokenInvalid
		}
		if r.SourceEpoch != "" && !safeID.MatchString(r.SourceEpoch) {
			return ErrTokenInvalid
		}
		if r.TransactionEpoch != "" && !safeID.MatchString(r.TransactionEpoch) {
			return ErrTokenInvalid
		}
		if (r.TransactionEpoch == "") != (r.TransactionFingerprint == "") {
			return ErrTokenInvalid
		}
		if r.TransactionFingerprint != "" {
			decoded, err := hex.DecodeString(r.TransactionFingerprint)
			if err != nil || len(decoded) != 32 {
				return ErrTokenInvalid
			}
		}
		if (r.Stage == "proof_committed" || r.Stage == "committed") && (r.SourceID == "" || r.SourceEpoch == "") {
			return ErrTokenInvalid
		}
		if r.Status == TokenLaunched && (r.Stage != "committed" || r.SourceID == "" || r.SourceEpoch == "" || r.NiriWindowID == 0 || r.KittyPID <= 0) {
			return ErrTokenInvalid
		}
	}
	return nil
}

type TokenStore struct {
	stateDir      string
	sliceDir      string
	root          string
	marker        string
	link          func(string, string) error
	remove        func(string) error
	syncFile      func(*os.File) error
	syncDirectory func(string) error
}

func NewTokenStore(stateDir string) (*TokenStore, error) {
	if strings.TrimSpace(stateDir) == "" {
		return nil, errors.New("state directory is required")
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, err
	}
	sliceDir := filepath.Join(stateDir, "slice")
	root := filepath.Join(sliceDir, "rpc-tokens")
	marker := filepath.Join(sliceDir, "rpc-tokens.enrolled")
	if err := ensureTokenDirectory(stateDir, false); err != nil {
		return nil, fmt.Errorf("validate token state directory: %w", err)
	}
	if err := createPrivateDirectory(sliceDir); err != nil {
		return nil, fmt.Errorf("create token slice directory: %w", err)
	}
	_, markerErr := os.Lstat(marker)
	markerExists := markerErr == nil
	if markerErr != nil && !errors.Is(markerErr, os.ErrNotExist) {
		return nil, markerErr
	}
	_, rootErr := os.Lstat(root)
	rootExists := rootErr == nil
	if rootErr != nil && !errors.Is(rootErr, os.ErrNotExist) {
		return nil, rootErr
	}
	if markerExists && !rootExists {
		return nil, errors.New("token journal missing after enrollment")
	}
	if rootExists && !markerExists {
		return nil, errors.New("unmarked token journal authority")
	}
	if !markerExists {
		file, err := os.OpenFile(marker, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return nil, err
		}
		if _, err = file.WriteString("terminal-redeemer-rpc-tokens-v1\n"); err == nil {
			err = file.Sync()
		}
		closeErr := file.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			return nil, err
		}
		if err = syncDir(sliceDir); err != nil {
			return nil, err
		}
	}
	if err := createPrivateDirectory(root); err != nil {
		return nil, fmt.Errorf("create token root: %w", err)
	}
	store := &TokenStore{stateDir: stateDir, sliceDir: sliceDir, root: root, marker: marker, link: os.Link, remove: os.Remove, syncFile: func(file *os.File) error { return file.Sync() }, syncDirectory: syncDir}
	if err := store.verifyHierarchy(); err != nil {
		return nil, err
	}
	for _, directory := range []string{stateDir, sliceDir, root} {
		if err := store.syncDirectory(directory); err != nil {
			return nil, err
		}
	}
	return store, nil
}
func (s *TokenStore) Root() string { return s.root }
func (s *TokenStore) LockToken(token string) (*os.File, error) {
	if _, err := s.path(token); err != nil {
		return nil, err
	}
	lockPath := filepath.Join(s.root, "transaction.lock")
	// The transaction lock must never cross an exec boundary. Routed creation
	// starts Zellij/Kitty while holding it; without CLOEXEC a daemonized child can
	// retain the flock after the responsible RPC process crashes and permanently
	// block same-token recovery.
	fd, err := syscall.Open(lockPath, syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), lockPath)
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, ErrTokenInvalid
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || int(stat.Uid) != os.Getuid() {
		file.Close()
		return nil, ErrTokenInvalid
	}
	if err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}
func UnlockToken(file *os.File) {
	if file == nil {
		return
	}
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	_ = file.Close()
}
func createPrivateDirectory(path string) error {
	err := os.Mkdir(path, 0o700)
	if err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	return ensureTokenDirectory(path, true)
}
func ensureTokenDirectory(path string, private bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || int(stat.Uid) != os.Getuid() {
		return errors.New("unsafe token state directory")
	}
	if private {
		if info.Mode().Perm() != 0o700 {
			return errors.New("token state directory must have mode 0700")
		}
	} else if info.Mode().Perm()&0o022 != 0 {
		return errors.New("state directory is group/world writable")
	}
	return nil
}
func (s *TokenStore) verifyHierarchy() error {
	if err := ensureTokenDirectory(s.stateDir, false); err != nil {
		return err
	}
	if err := ensureTokenDirectory(s.sliceDir, true); err != nil {
		return err
	}
	payload, err := safefile.ReadRegular(s.marker, len("terminal-redeemer-rpc-tokens-v1\n"), 0o777, 0o600)
	if err != nil || string(payload) != "terminal-redeemer-rpc-tokens-v1\n" {
		return ErrTokenInvalid
	}
	return ensureTokenDirectory(s.root, true)
}
func (s *TokenStore) path(token string) (string, error) {
	if !ValidToken(token) {
		return "", errors.New("invalid idempotency token")
	}
	sum := sha256.Sum256([]byte("terminal-redeemer/rpc-token/v1\x00" + token))
	return filepath.Join(s.root, base64.RawURLEncoding.EncodeToString(sum[:])+".json"), nil
}
func StableTerminalID(sourceHostID, token string) string {
	sum := sha256.Sum256([]byte("terminal-redeemer/host-terminal/v1\x00" + sourceHostID + "\x00" + token))
	return "term_" + base64.RawURLEncoding.EncodeToString(sum[:])
}
func StableSessionName(token string) string {
	sum := sha256.Sum256([]byte("terminal-redeemer/routed-zellij/v1\x00" + token))
	return "tr-" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:20]))
}
func (s *TokenStore) CreatePending(sourceHostID, token string, now time.Time) (TokenRecord, bool, error) {
	return s.CreatePendingLaunch(sourceHostID, token, "", "", now)
}
func (s *TokenStore) CreatePendingLaunch(sourceHostID, token, sessionName, workspaceName string, now time.Time) (TokenRecord, bool, error) {
	return s.CreatePendingRouted(sourceHostID, "", "", token, sessionName, workspaceName, now)
}
func (s *TokenStore) CreatePendingRouted(sourceHostID, transactionEpoch, transactionFingerprint, token, sessionName, workspaceName string, now time.Time) (TokenRecord, bool, error) {
	if err := s.verifyHierarchy(); err != nil {
		return TokenRecord{}, false, err
	}
	lock, err := s.LockToken(token)
	if err != nil {
		return TokenRecord{}, false, err
	}
	defer UnlockToken(lock)
	path, err := s.path(token)
	if err != nil {
		return TokenRecord{}, false, err
	}
	entries, readErr := os.ReadDir(s.root)
	if readErr != nil {
		return TokenRecord{}, false, readErr
	}
	records := 0
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			records++
		}
	}
	if records >= MaxTokenRecords {
		if _, statErr := os.Lstat(path); errors.Is(statErr, os.ErrNotExist) {
			return TokenRecord{}, false, errors.New("slice launch token capacity exhausted; maintenance required")
		}
	}
	if existing, readErr := s.Read(token); readErr == nil {
		return existing, false, nil
	} else if !errors.Is(readErr, ErrTokenNotFound) {
		return TokenRecord{}, false, readErr
	}
	record := TokenRecord{StorageVersion: 1, Token: token, HostTerminalID: StableTerminalID(sourceHostID, token), Status: TokenPending, SessionName: sessionName, WorkspaceName: workspaceName, TransactionEpoch: transactionEpoch, TransactionFingerprint: transactionFingerprint, CreatedAt: now.UTC(), UpdatedAt: now.UTC()}
	if sessionName != "" {
		record.Stage = "pending"
	}
	if err := record.Validate(); err != nil {
		return TokenRecord{}, false, err
	}
	payload, _ := json.Marshal(record)
	payload = append(payload, '\n')
	if len(payload) > MaxTokenRecordBytes {
		return TokenRecord{}, false, ErrTokenInvalid
	}
	tmp, err := os.CreateTemp(s.root, ".pending-*.tmp")
	if err != nil {
		return TokenRecord{}, false, err
	}
	tmpPath := tmp.Name()
	defer func() { _ = tmp.Close(); _ = s.remove(tmpPath) }()
	if err := tmp.Chmod(0o600); err != nil {
		return TokenRecord{}, false, err
	}
	if _, err := tmp.Write(payload); err != nil {
		return TokenRecord{}, false, err
	}
	if err := s.syncFile(tmp); err != nil {
		return TokenRecord{}, false, err
	}
	if err := tmp.Close(); err != nil {
		return TokenRecord{}, false, err
	}
	if err := s.verifyHierarchy(); err != nil {
		return TokenRecord{}, false, err
	}
	if err := s.link(tmpPath, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			existing, readErr := s.Read(token)
			return existing, false, readErr
		}
		return TokenRecord{}, false, err
	}
	if err := s.syncDirectory(s.root); err != nil {
		return TokenRecord{}, false, err
	}
	if err := s.remove(tmpPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return TokenRecord{}, false, err
	}
	if err := s.syncDirectory(s.root); err != nil {
		return TokenRecord{}, false, err
	}
	return record, true, nil
}
func (s *TokenStore) Read(token string) (TokenRecord, error) {
	if err := s.verifyHierarchy(); err != nil {
		return TokenRecord{}, err
	}
	path, err := s.path(token)
	if err != nil {
		return TokenRecord{}, err
	}
	payload, err := safefile.ReadRegular(path, MaxTokenRecordBytes, 0o077, 0)
	if errors.Is(err, os.ErrNotExist) {
		return TokenRecord{}, ErrTokenNotFound
	}
	if err != nil {
		return TokenRecord{}, ErrTokenInvalid
	}
	if len(payload) == 0 || len(payload) > MaxTokenRecordBytes {
		return TokenRecord{}, ErrTokenInvalid
	}
	if err := sliceprotocol.RejectDuplicateKeys(payload); err != nil {
		return TokenRecord{}, ErrTokenInvalid
	}
	if err := sliceprotocol.RejectUnknownFieldsExact(payload, TokenRecord{}); err != nil {
		return TokenRecord{}, ErrTokenInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var record TokenRecord
	if err := decoder.Decode(&record); err != nil {
		return TokenRecord{}, ErrTokenInvalid
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return TokenRecord{}, ErrTokenInvalid
	}
	if err := record.Validate(); err != nil {
		return TokenRecord{}, err
	}
	if record.Token != token {
		return TokenRecord{}, ErrTokenInvalid
	}
	return record, nil
}
func (s *TokenStore) Update(record TokenRecord) (err error) {
	if err := s.verifyHierarchy(); err != nil {
		return err
	}
	if err := record.Validate(); err != nil {
		return err
	}
	path, err := s.path(record.Token)
	if err != nil {
		return err
	}
	if _, err := s.Read(record.Token); err != nil {
		return err
	}
	payload, _ := json.Marshal(record)
	payload = append(payload, '\n')
	if len(payload) > MaxTokenRecordBytes {
		return ErrTokenInvalid
	}
	tmp, err := os.CreateTemp(s.root, ".token-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = tmp.Close()
		if err != nil {
			_ = s.remove(tmpPath)
		}
	}()
	if err = tmp.Chmod(0o600); err != nil {
		return err
	}
	if _, err = tmp.Write(payload); err != nil {
		return err
	}
	if err = s.syncFile(tmp); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = s.verifyHierarchy(); err != nil {
		return err
	}
	if err = os.Rename(tmpPath, path); err != nil {
		return err
	}
	if err = s.syncDirectory(s.root); err != nil {
		return err
	}
	return nil
}
func syncDir(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open state directory: %w", err)
	}
	defer f.Close()
	return f.Sync()
}
