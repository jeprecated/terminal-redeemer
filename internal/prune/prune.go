package prune

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/jmo/terminal-redeemer/internal/checkpoints"
	"github.com/jmo/terminal-redeemer/internal/events"
	"github.com/jmo/terminal-redeemer/internal/storelock"
)

var ErrActiveWriter = errors.New("active writer lock present")

type Runner struct {
	root string
	days int
	now  func() time.Time
}

type Summary struct {
	EventsPruned      int
	CheckpointsPruned int
	SnapshotsPruned   int
}

func NewRunner(root string, days int, now func() time.Time) *Runner {
	if now == nil {
		now = time.Now
	}
	return &Runner{root: root, days: days, now: now}
}

func (r *Runner) Run() (Summary, error) {
	lock, err := storelock.Acquire(r.root)
	if errors.Is(err, storelock.ErrLocked) {
		return Summary{}, ErrActiveWriter
	}
	if err != nil {
		return Summary{}, fmt.Errorf("acquire prune lock: %w", err)
	}
	defer func() { _ = lock.Close() }()

	cutoff := r.now().UTC().AddDate(0, 0, -r.days)
	eventsPruned, err := r.pruneEvents(cutoff)
	if err != nil {
		return Summary{}, err
	}
	checkpointsPruned, err := checkpoints.Prune(r.root, cutoff)
	if err != nil {
		return Summary{}, err
	}
	snapshotsPruned, err := r.pruneSnapshots(cutoff)
	if err != nil {
		return Summary{}, err
	}

	return Summary{EventsPruned: eventsPruned, CheckpointsPruned: checkpointsPruned, SnapshotsPruned: snapshotsPruned}, nil
}

func (r *Runner) pruneEvents(cutoff time.Time) (int, error) {
	eventsPath := filepath.Join(r.root, "events.jsonl")
	f, err := os.Open(eventsPath)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("open events: %w", err)
	}
	defer func() {
		_ = f.Close()
	}()

	decoded, _, err := events.ReadLog(f)
	if err != nil {
		return 0, fmt.Errorf("read events: %w", err)
	}

	kept := make([]events.Event, 0, len(decoded))
	var anchor *events.Event
	for _, event := range decoded {
		if event.TS.Before(cutoff) {
			e := event
			anchor = &e
			continue
		}
		kept = append(kept, event)
	}
	if anchor != nil {
		kept = append([]events.Event{*anchor}, kept...)
	}

	if err := rewriteEvents(eventsPath, kept); err != nil {
		return 0, err
	}

	return max(0, len(decoded)-len(kept)), nil
}

func (r *Runner) pruneSnapshots(cutoff time.Time) (int, error) {
	dir := filepath.Join(r.root, "snapshots")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read snapshots dir: %w", err)
	}

	type snapshotFile struct {
		path string
		ts   time.Time
	}
	all := make([]snapshotFile, 0)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		base := entry.Name()[:len(entry.Name())-len(filepath.Ext(entry.Name()))]
		unix, parseErr := strconv.ParseInt(base, 10, 64)
		if parseErr != nil {
			continue
		}
		all = append(all, snapshotFile{path: filepath.Join(dir, entry.Name()), ts: time.Unix(unix, 0).UTC()})
	}

	if len(all) == 0 {
		return 0, nil
	}

	sort.Slice(all, func(i, j int) bool { return all[i].ts.Before(all[j].ts) })

	keep := map[string]struct{}{all[len(all)-1].path: {}}
	for i := len(all) - 1; i >= 0; i-- {
		if !all[i].ts.After(cutoff) {
			keep[all[i].path] = struct{}{}
			break
		}
	}

	pruned := 0
	for _, snap := range all {
		if _, ok := keep[snap.path]; ok {
			continue
		}
		if err := os.Remove(snap.path); err != nil {
			return 0, err
		}
		pruned++
	}

	if pruned > 0 {
		if err := syncDir(dir); err != nil {
			return 0, fmt.Errorf("sync snapshots dir: %w", err)
		}
	}
	return pruned, nil
}

func rewriteEvents(path string, kept []events.Event) (err error) {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".events-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() {
		_ = f.Close()
		if err != nil {
			_ = os.Remove(tmp)
		}
	}()

	for _, event := range kept {
		payload, marshalErr := json.Marshal(event)
		if marshalErr != nil {
			return marshalErr
		}
		payload = append(payload, '\n')
		written, writeErr := f.Write(payload)
		if writeErr != nil {
			return writeErr
		}
		if written != len(payload) {
			return io.ErrShortWrite
		}
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	if err := syncDir(dir); err != nil {
		return err
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

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
