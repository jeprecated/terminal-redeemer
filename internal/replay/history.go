package replay

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/jmo/terminal-redeemer/internal/checkpoints"
	"github.com/jmo/terminal-redeemer/internal/events"
	"github.com/jmo/terminal-redeemer/internal/model"
)

// Checkpoint is a complete captured state. BootID is empty for legacy
// checkpoints, which remain useful to explicit historical restore callers.
type Checkpoint struct {
	CapturedAt         time.Time
	Host               string
	Profile            string
	BootID             string
	State              model.State
	StateHash          string
	DurableEventOffset int64
}

// ListCheckpoints returns complete state_full event records in capture order.
// Incremental window patches are deliberately excluded: implicit resume must
// only select a capture that represents a successfully completed query.
func ListCheckpoints(root string) ([]Checkpoint, error) {
	recorded, err := listEventRecords(root)
	if err != nil {
		return nil, err
	}

	out := make([]Checkpoint, 0, len(recorded))
	for _, record := range recorded {
		event := record.Event
		if event.EventType != "state_full" {
			continue
		}
		out = append(out, Checkpoint{
			CapturedAt:         event.TS,
			Host:               event.Host,
			Profile:            event.Profile,
			BootID:             event.BootID,
			State:              model.Normalize(decodeEventState(event.State)),
			StateHash:          event.StateHash,
			DurableEventOffset: record.EndOffset,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CapturedAt.Before(out[j].CapturedAt) })
	return out, nil
}

// ListResumeCheckpoints merges valid rolling checkpoints with boot-aware
// state_full event fallback. For each boot/host/profile identity, a durable
// event newer than the checkpoint's referenced offset wins; otherwise the
// rolling observed_at supplies freshness even when state stayed unchanged.
// Malformed rolling files are ignored here so event history remains usable;
// doctor reports those files separately.
func ListResumeCheckpoints(root string) ([]Checkpoint, error) {
	eventCheckpoints, err := ListCheckpoints(root)
	if err != nil {
		return nil, err
	}
	rolling, _, err := checkpoints.List(root)
	if err != nil {
		return nil, err
	}

	type key struct{ bootID, host, profile string }
	selected := make(map[key]Checkpoint)
	for _, checkpoint := range eventCheckpoints {
		if checkpoint.BootID == "" {
			continue
		}
		identity := key{checkpoint.BootID, checkpoint.Host, checkpoint.Profile}
		current, ok := selected[identity]
		if !ok || checkpoint.DurableEventOffset > current.DurableEventOffset || checkpoint.CapturedAt.After(current.CapturedAt) {
			selected[identity] = checkpoint
		}
	}
	for _, checkpoint := range rolling {
		identity := key{checkpoint.BootID, checkpoint.Host, checkpoint.Profile}
		current, hasEvent := selected[identity]
		if hasEvent && (current.DurableEventOffset > checkpoint.EventOffset || current.CapturedAt.After(checkpoint.ObservedAt)) {
			continue
		}
		selected[identity] = Checkpoint{
			CapturedAt:         checkpoint.ObservedAt,
			Host:               checkpoint.Host,
			Profile:            checkpoint.Profile,
			BootID:             checkpoint.BootID,
			State:              model.Normalize(checkpoint.State),
			StateHash:          checkpoint.StateHash,
			DurableEventOffset: checkpoint.EventOffset,
		}
	}

	out := make([]Checkpoint, 0, len(selected))
	for _, checkpoint := range selected {
		out = append(out, checkpoint)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CapturedAt.Equal(out[j].CapturedAt) {
			return out[i].CapturedAt.Before(out[j].CapturedAt)
		}
		if out[i].Host != out[j].Host {
			return out[i].Host < out[j].Host
		}
		if out[i].Profile != out[j].Profile {
			return out[i].Profile < out[j].Profile
		}
		return out[i].BootID < out[j].BootID
	})
	return out, nil
}

func ListEvents(root string, from *time.Time, to *time.Time) ([]events.Event, error) {
	records, err := listEventRecords(root)
	if err != nil {
		return nil, err
	}
	out := make([]events.Event, 0, len(records))
	for _, record := range records {
		event := record.Event
		if from != nil && event.TS.Before(*from) {
			continue
		}
		if to != nil && event.TS.After(*to) {
			continue
		}
		out = append(out, event)
	}
	return out, nil
}

func listEventRecords(root string) ([]events.Record, error) {
	path := filepath.Join(root, "events.jsonl")
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open events file: %w", err)
	}
	defer func() { _ = f.Close() }()

	records, _, err := events.ReadLogRecords(f)
	if err != nil {
		return nil, fmt.Errorf("read event log: %w", err)
	}
	return records, nil
}
