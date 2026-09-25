package procmeta

import (
	"context"
	"fmt"
)

// DescendantProcess is argv evidence with the process/parent identity retained
// for IPC peer matching and ancestor suppression. ObserveDescendantProcesses
// returns no partial candidates when any part of the bounded tree is uncertain.
type DescendantProcess struct {
	PID, ParentPID int
	StartTime      string
	Args           []string
}

func ObserveDescendantProcesses(ctx context.Context, procRoot string, rootPID int) ([]DescendantProcess, error) {
	return observeDescendantProcesses(ctx, procRoot, rootPID, nil)
}

func observeDescendantProcesses(ctx context.Context, procRoot string, rootPID int, afterSnapshot func()) ([]DescendantProcess, error) {
	if rootPID <= 0 {
		return nil, fmt.Errorf("invalid process root")
	}
	root := normalizedProcRoot(procRoot)
	table, children, complete, err := processTable(ctx, root, rootPID)
	if err != nil {
		return nil, err
	}
	if !complete {
		return nil, fmt.Errorf("process tree observation incomplete")
	}
	if afterSnapshot != nil {
		afterSnapshot()
	}
	verify := func(pid int) error {
		identity, ok := table[pid]
		if !ok {
			return fmt.Errorf("process missing from observation")
		}
		current, err := readProcessIdentity(root, pid)
		if err != nil || current != identity {
			return fmt.Errorf("process identity changed during observation")
		}
		return nil
	}
	if err := verify(rootPID); err != nil {
		return nil, err
	}
	queue := append([]int(nil), children[rootPID]...)
	seen := map[int]bool{}
	var result []DescendantProcess
	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		pid := queue[0]
		queue = queue[1:]
		if seen[pid] {
			return nil, fmt.Errorf("process tree contains repeated identity")
		}
		seen[pid] = true
		if err := verify(pid); err != nil {
			return nil, err
		}
		args, err := readProcArgs(root, pid)
		if err != nil {
			return nil, err
		}
		if err := verify(pid); err != nil {
			return nil, err
		}
		result = append(result, DescendantProcess{PID: pid, ParentPID: table[pid].parentPID, StartTime: table[pid].startTime, Args: args})
		queue = append(queue, children[pid]...)
	}
	// Recheck already-read ancestors as well as the last descendant. A later
	// unreadable/reparented process must not leak earlier callback matches.
	for pid := range table {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := verify(pid); err != nil {
			return nil, err
		}
	}
	return result, nil
}
