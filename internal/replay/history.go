package replay

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/jmo/terminal-redeemer/internal/events"
	"github.com/jmo/terminal-redeemer/internal/model"
)

// Checkpoint is a complete captured state. BootID is empty for legacy
// checkpoints, which remain useful to explicit historical restore callers.
type Checkpoint struct {
	CapturedAt time.Time
	Host       string
	Profile    string
	BootID     string
	State      model.State
}

// ListCheckpoints returns complete state_full records in capture order.
// Incremental window patches are deliberately excluded: implicit resume must
// only select a capture that represents a successfully completed query.
func ListCheckpoints(root string) ([]Checkpoint, error) {
	recorded, err := ListEvents(root, nil, nil)
	if err != nil {
		return nil, err
	}

	out := make([]Checkpoint, 0, len(recorded))
	for _, event := range recorded {
		if event.EventType != "state_full" {
			continue
		}
		out = append(out, Checkpoint{
			CapturedAt: event.TS,
			Host:       event.Host,
			Profile:    event.Profile,
			BootID:     event.BootID,
			State:      model.Normalize(decodeEventState(event.State)),
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CapturedAt.Before(out[j].CapturedAt) })
	return out, nil
}

func ListEvents(root string, from *time.Time, to *time.Time) ([]events.Event, error) {
	path := filepath.Join(root, "events.jsonl")
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open events file: %w", err)
	}
	defer func() {
		_ = f.Close()
	}()

	decoded, _, err := events.ReadLog(f)
	if err != nil {
		return nil, fmt.Errorf("read event log: %w", err)
	}
	out := make([]events.Event, 0, len(decoded))
	for _, event := range decoded {
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
