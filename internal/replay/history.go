package replay

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jmo/terminal-redeemer/internal/events"
)

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
