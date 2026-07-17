package events

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jmo/terminal-redeemer/internal/bootid"
	"github.com/jmo/terminal-redeemer/internal/storelock"
)

var ErrLocked = errors.New("event store is locked")

type Event struct {
	V         int            `json:"v"`
	TS        time.Time      `json:"ts"`
	Host      string         `json:"host"`
	Profile   string         `json:"profile"`
	BootID    string         `json:"boot_id,omitempty"`
	EventType string         `json:"event_type"`
	WindowKey string         `json:"window_key,omitempty"`
	Patch     map[string]any `json:"patch,omitempty"`
	State     map[string]any `json:"state,omitempty"`
	Source    string         `json:"source,omitempty"`
	StateHash string         `json:"state_hash"`
}

func (e Event) Validate() error {
	if e.V != 1 {
		return fmt.Errorf("invalid version: %d", e.V)
	}
	if e.TS.IsZero() {
		return errors.New("ts is required")
	}
	if strings.TrimSpace(e.Host) == "" {
		return errors.New("host is required")
	}
	if strings.TrimSpace(e.Profile) == "" {
		return errors.New("profile is required")
	}
	if e.BootID != "" && strings.TrimSpace(e.BootID) == "" {
		return errors.New("boot_id must not be blank")
	}
	if strings.TrimSpace(e.EventType) == "" {
		return errors.New("event_type is required")
	}
	switch e.EventType {
	case "window_patch":
		if strings.TrimSpace(e.WindowKey) == "" {
			return errors.New("window_key is required for window_patch")
		}
		if e.Patch == nil {
			return errors.New("patch is required for window_patch")
		}
	case "state_full":
		if e.State == nil {
			return errors.New("state is required for state_full")
		}
	default:
		return fmt.Errorf("unsupported event_type: %s", e.EventType)
	}
	if strings.TrimSpace(e.StateHash) == "" {
		return errors.New("state_hash is required")
	}
	return nil
}

type Store struct {
	eventsPath   string
	root         string
	bootIDSource bootid.Source
	syncFile     func(*os.File) error
}

func NewStore(root string) (*Store, error) {
	return NewStoreWithBootIDSource(root, bootid.Current)
}

func NewStoreWithBootIDSource(root string, source bootid.Source) (*Store, error) {
	if source == nil {
		source = bootid.Current
	}
	if err := os.MkdirAll(filepath.Join(root, "meta"), 0o755); err != nil {
		return nil, fmt.Errorf("create meta dir: %w", err)
	}

	eventsPath := filepath.Join(root, "events.jsonl")
	file, err := os.OpenFile(eventsPath, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create events file: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("sync events file: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close events file: %w", err)
	}
	if err := syncDir(root); err != nil {
		return nil, fmt.Errorf("sync state dir: %w", err)
	}

	return &Store{
		eventsPath:   eventsPath,
		root:         root,
		bootIDSource: source,
		syncFile:     func(file *os.File) error { return file.Sync() },
	}, nil
}

type Writer struct {
	lock         *storelock.Lock
	file         *os.File
	bootIDSource bootid.Source
	syncFile     func(*os.File) error
}

func (s *Store) AcquireWriter() (*Writer, error) {
	lock, err := storelock.Acquire(s.root)
	if errors.Is(err, storelock.ErrLocked) {
		return nil, ErrLocked
	}
	if err != nil {
		return nil, err
	}

	eventsFile, err := os.OpenFile(s.eventsPath, os.O_APPEND|os.O_RDWR, 0o600)
	if err != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("open events file: %w", err)
	}
	if err := repairTrailingRecord(eventsFile, s.syncFile); err != nil {
		_ = eventsFile.Close()
		_ = lock.Close()
		return nil, err
	}

	return &Writer{lock: lock, file: eventsFile, bootIDSource: s.bootIDSource, syncFile: s.syncFile}, nil
}

func (w *Writer) Append(event Event) (int64, error) {
	id, err := w.bootIDSource()
	if err != nil {
		return 0, err
	}
	event.BootID = id
	if err := event.Validate(); err != nil {
		return 0, err
	}

	payload, err := json.Marshal(event)
	if err != nil {
		return 0, fmt.Errorf("marshal event: %w", err)
	}
	payload = append(payload, '\n')

	written, err := w.file.Write(payload)
	if err != nil {
		return 0, fmt.Errorf("append event: %w", err)
	}
	if written != len(payload) {
		return 0, io.ErrShortWrite
	}
	if err := w.syncFile(w.file); err != nil {
		return 0, fmt.Errorf("sync appended event: %w", err)
	}

	offset, err := w.file.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, fmt.Errorf("seek current: %w", err)
	}

	return offset, nil
}

func (w *Writer) Close() error {
	if w == nil {
		return nil
	}
	var fileErr error
	if w.file != nil {
		fileErr = w.file.Close()
		w.file = nil
	}
	lockErr := w.lock.Close()
	w.lock = nil
	return errors.Join(fileErr, lockErr)
}

func (s *Store) ReadSince(cursor int64) ([]Event, int64, error) {
	f, err := os.Open(s.eventsPath)
	if err != nil {
		return nil, cursor, fmt.Errorf("open events file: %w", err)
	}
	defer func() {
		_ = f.Close()
	}()

	if _, err := f.Seek(cursor, io.SeekStart); err != nil {
		return nil, cursor, fmt.Errorf("seek to cursor: %w", err)
	}

	out, consumed, err := ReadLog(f)
	if err != nil {
		return nil, cursor, err
	}
	return out, cursor + consumed, nil
}

// ReadLog decodes complete events from r. A malformed final record is ignored
// and excluded from consumed so a later read can retry it after recovery. Any
// malformed record followed by more data is corruption and returns an error.
func ReadLog(r io.Reader) ([]Event, int64, error) {
	reader := bufio.NewReader(r)
	out := make([]Event, 0)
	var consumed int64
	lineNumber := 0

	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) == 0 && errors.Is(readErr, io.EOF) {
			return out, consumed, nil
		}
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return nil, consumed, fmt.Errorf("read events: %w", readErr)
		}
		lineNumber++

		payload := line
		if len(payload) > 0 && payload[len(payload)-1] == '\n' {
			payload = payload[:len(payload)-1]
		}
		var event Event
		decodeErr := json.Unmarshal(payload, &event)
		if decodeErr == nil {
			decodeErr = event.Validate()
		}
		if decodeErr != nil {
			_, peekErr := reader.Peek(1)
			if errors.Is(peekErr, io.EOF) {
				return out, consumed, nil
			}
			if peekErr != nil {
				return nil, consumed, fmt.Errorf("inspect events after line %d: %w", lineNumber, peekErr)
			}
			return nil, consumed, fmt.Errorf("invalid event at line %d: %w", lineNumber, decodeErr)
		}

		out = append(out, event)
		consumed += int64(len(line))
		if errors.Is(readErr, io.EOF) {
			return out, consumed, nil
		}
	}
}

func repairTrailingRecord(file *os.File, syncFile func(*os.File) error) error {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("seek event log for recovery: %w", err)
	}
	_, consumed, err := ReadLog(file)
	if err != nil {
		return fmt.Errorf("validate event log before append: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat event log: %w", err)
	}
	if consumed < info.Size() {
		if err := file.Truncate(consumed); err != nil {
			return fmt.Errorf("truncate malformed trailing event: %w", err)
		}
		if err := syncFile(file); err != nil {
			return fmt.Errorf("sync repaired event log: %w", err)
		}
		info, err = file.Stat()
		if err != nil {
			return fmt.Errorf("stat repaired event log: %w", err)
		}
	}
	if info.Size() > 0 {
		last := []byte{0}
		if _, err := file.ReadAt(last, info.Size()-1); err != nil {
			return fmt.Errorf("read event log terminator: %w", err)
		}
		if last[0] != '\n' {
			written, err := file.Write([]byte{'\n'})
			if err != nil {
				return fmt.Errorf("terminate final event: %w", err)
			}
			if written != 1 {
				return io.ErrShortWrite
			}
			if err := syncFile(file); err != nil {
				return fmt.Errorf("sync event terminator: %w", err)
			}
		}
	}
	return nil
}

func syncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	return dir.Sync()
}
