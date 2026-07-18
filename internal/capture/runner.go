package capture

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jmo/terminal-redeemer/internal/checkpoints"
	"github.com/jmo/terminal-redeemer/internal/diff"
	"github.com/jmo/terminal-redeemer/internal/events"
	"github.com/jmo/terminal-redeemer/internal/model"
	"github.com/jmo/terminal-redeemer/internal/snapshots"
)

type Collector interface {
	Collect(ctx context.Context) (model.State, error)
}

type EventStore interface {
	AcquireWriter() (*events.Writer, error)
}

type CheckpointStore interface {
	Read(bootID, host, profile string) (checkpoints.Checkpoint, error)
	Write(checkpoint checkpoints.Checkpoint) (string, error)
}

type SnapshotStore interface {
	Write(snapshot snapshots.Snapshot) (string, error)
}

type Config struct {
	Collector       Collector
	DiffEngine      *diff.Engine // retained for API compatibility; capture persists full states
	EventStore      EventStore
	CheckpointStore CheckpointStore
	SnapshotStore   SnapshotStore
	SnapshotEvery   int
	Host            string
	Profile         string
	Source          string
	Now             func() time.Time
	Logger          io.Writer
}

type Runner struct {
	collector       Collector
	eventStore      EventStore
	checkpointStore CheckpointStore
	snapshotStore   SnapshotStore
	snapshotEvery   int
	host            string
	profile         string
	source          string
	now             func() time.Time
	logger          io.Writer

	eventCount int
}

type Result struct {
	EventsWritten  int
	CheckpointPath string
	SnapshotPath   string
	StateHash      string
}

func NewRunner(config Config) *Runner {
	now := config.Now
	if now == nil {
		now = time.Now
	}
	logger := config.Logger
	if logger == nil {
		logger = io.Discard
	}

	return &Runner{
		collector:       config.Collector,
		eventStore:      config.EventStore,
		checkpointStore: config.CheckpointStore,
		snapshotStore:   config.SnapshotStore,
		snapshotEvery:   config.SnapshotEvery,
		host:            strings.TrimSpace(config.Host),
		profile:         strings.TrimSpace(config.Profile),
		source:          config.Source,
		now:             now,
		logger:          logger,
	}
}

// CaptureOnce always performs a complete collection. It suppresses history
// only after acquiring the repository writer lock and comparing the normalized
// result with the newest same-boot checkpoint/event evidence.
func (r *Runner) CaptureOnce(ctx context.Context) (Result, error) {
	state, err := r.collector.Collect(ctx)
	if err != nil {
		return Result{}, err
	}
	state = model.Normalize(state)
	stateHash, err := state.Hash()
	if err != nil {
		return Result{}, err
	}
	if r.checkpointStore == nil {
		rooted, ok := r.eventStore.(interface{ Root() string })
		if !ok {
			return Result{}, errors.New("rolling checkpoint store is unavailable")
		}
		store, storeErr := checkpoints.NewStore(rooted.Root())
		if storeErr != nil {
			return Result{}, storeErr
		}
		r.checkpointStore = store
	}

	writer, err := r.eventStore.AcquireWriter()
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = writer.Close() }()

	bootID, err := writer.BootID()
	if err != nil {
		return Result{}, err
	}
	now := r.now().UTC()

	records, err := writer.Records()
	if err != nil {
		return Result{}, fmt.Errorf("read event evidence: %w", err)
	}
	latestEvent, hasEvent := latestStateEvent(records, bootID, r.host, r.profile)

	rolling, rollingErr := r.checkpointStore.Read(bootID, r.host, r.profile)
	hasRolling := rollingErr == nil
	if rollingErr != nil && !errors.Is(rollingErr, checkpoints.ErrNotFound) && !errors.Is(rollingErr, checkpoints.ErrInvalid) {
		return Result{}, fmt.Errorf("read rolling checkpoint: %w", rollingErr)
	}

	evidenceHash := ""
	eventOffset := int64(0)
	if hasRolling {
		evidenceHash = rolling.StateHash
		eventOffset = rolling.EventOffset
	}
	// A durable event not represented by the published checkpoint is the
	// authoritative crash-recovery edge. Timestamp comparison also handles an
	// event log whose byte offsets changed during retention rewriting.
	if hasEvent && (!hasRolling || latestEvent.EndOffset > rolling.EventOffset || latestEvent.Event.TS.After(rolling.ObservedAt)) {
		evidenceHash = latestEvent.Event.StateHash
		eventOffset = latestEvent.EndOffset
	}

	result := Result{StateHash: stateHash}
	if evidenceHash == "" || evidenceHash != stateHash {
		eventOffset, err = writer.Append(events.Event{
			V:         1,
			TS:        now,
			Host:      r.host,
			Profile:   r.profile,
			EventType: "state_full",
			State:     stateAsMap(state),
			Source:    r.source,
			StateHash: stateHash,
		})
		if err != nil {
			return Result{}, err
		}
		result.EventsWritten = 1
		r.eventCount++
	}

	checkpointPath, err := r.checkpointStore.Write(checkpoints.Checkpoint{
		V:           checkpoints.SchemaVersion,
		BootID:      bootID,
		Host:        r.host,
		Profile:     r.profile,
		ObservedAt:  now,
		State:       state,
		StateHash:   stateHash,
		EventOffset: eventOffset,
	})
	if err != nil {
		return Result{}, fmt.Errorf("publish rolling checkpoint: %w", err)
	}
	result.CheckpointPath = checkpointPath

	// Timestamped snapshots remain a compatibility/replay optimization. Their
	// cadence counts state changes in this Runner; rolling checkpoints are the
	// process-independent resume mechanism and are refreshed on every success.
	if result.EventsWritten > 0 && snapshots.ShouldSnapshot(r.eventCount, r.snapshotEvery) && r.snapshotStore != nil {
		snapshotPath, err := r.snapshotStore.Write(snapshots.Snapshot{
			V:               1,
			CreatedAt:       now,
			Host:            r.host,
			Profile:         r.profile,
			LastEventOffset: eventOffset,
			StateHash:       stateHash,
			State:           stateAsMap(state),
		})
		if err != nil {
			return Result{}, err
		}
		result.SnapshotPath = snapshotPath
	}
	return result, nil
}

func (r *Runner) captureStateFull(ctx context.Context) (Result, error) {
	return r.CaptureOnce(ctx)
}

// captureDiff remains for existing callers but now uses the same complete,
// change-only, boot-aware transaction as one-shot capture.
func (r *Runner) captureDiff(ctx context.Context) (Result, error) {
	return r.CaptureOnce(ctx)
}

func (r *Runner) CaptureRun(ctx context.Context, ticks <-chan time.Time) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case _, ok := <-ticks:
			if !ok {
				return nil
			}
			if _, err := r.CaptureOnce(ctx); err != nil {
				_, _ = fmt.Fprintf(r.logger, "capture_once_error err=%q\n", err.Error())
			}
		}
	}
}

func latestStateEvent(records []events.Record, bootID, host, profile string) (events.Record, bool) {
	for i := len(records) - 1; i >= 0; i-- {
		event := records[i].Event
		if event.EventType == "state_full" && strings.TrimSpace(event.BootID) == bootID && event.Host == host && event.Profile == profile {
			return records[i], true
		}
	}
	return events.Record{}, false
}

func stateAsMap(state model.State) map[string]any {
	payload, err := json.Marshal(state)
	if err != nil {
		return map[string]any{}
	}
	out := map[string]any{}
	if err := json.Unmarshal(payload, &out); err != nil {
		return map[string]any{}
	}
	return out
}
