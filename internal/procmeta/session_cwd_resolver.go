package procmeta

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type SessionCWDResolver interface {
	Resolve(session string) (string, error)
}

type ZellijSessionCWDResolver struct {
	ProcRoot string
}

func NewZellijSessionCWDResolver(procRoot string) ZellijSessionCWDResolver {
	return ZellijSessionCWDResolver{ProcRoot: procRoot}
}

func (r ZellijSessionCWDResolver) Resolve(session string) (string, error) {
	session = strings.TrimSpace(session)
	if session == "" {
		return "", nil
	}

	root := strings.TrimSpace(r.ProcRoot)
	if root == "" {
		root = "/proc"
	}

	table, err := scanSessionProcesses(root)
	if err != nil {
		return "", err
	}
	return table.resolve(root, session), nil
}

// SnapshotSessionCWDResolver scans the process table once, on first use, and
// resolves every session from that one observation. Use it for a single
// capture; ZellijSessionCWDResolver rescans on every call.
type SnapshotSessionCWDResolver struct {
	ProcRoot string
	table    *sessionProcessTable
	err      error
}

func (r *SnapshotSessionCWDResolver) Resolve(session string) (string, error) {
	session = strings.TrimSpace(session)
	if session == "" {
		return "", nil
	}
	root := strings.TrimSpace(r.ProcRoot)
	if root == "" {
		root = "/proc"
	}
	if r.table == nil && r.err == nil {
		r.table, r.err = scanSessionProcesses(root)
	}
	if r.err != nil {
		return "", r.err
	}
	return r.table.resolve(root, session), nil
}

type sessionProcessMeta struct {
	pid     int
	ppid    int
	comm    string
	cmdline string
}

type sessionProcessTable struct {
	metas    map[int]sessionProcessMeta
	children map[int][]int
}

func scanSessionProcesses(root string) (*sessionProcessTable, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}

	metas := make(map[int]sessionProcessMeta, len(entries))
	children := make(map[int][]int, len(entries))

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}

		statPayload, err := os.ReadFile(filepath.Join(root, entry.Name(), "stat"))
		if err != nil {
			continue
		}
		ppid, err := parseParentPIDFromStat(string(statPayload))
		if err != nil {
			continue
		}

		comm := readComm(root, pid)
		cmdline := ""
		if payload, err := os.ReadFile(filepath.Join(root, entry.Name(), "cmdline")); err == nil {
			cmdline = strings.Join(parseNullSeparated(payload), " ")
		}

		meta := sessionProcessMeta{pid: pid, ppid: ppid, comm: comm, cmdline: cmdline}
		metas[pid] = meta
		children[ppid] = append(children[ppid], pid)
	}

	return &sessionProcessTable{metas: metas, children: children}, nil
}

func (t *sessionProcessTable) resolve(root string, session string) string {
	metas, children := t.metas, t.children
	servers := make([]int, 0, 4)
	for pid, meta := range metas {
		if isZellijServerForSession(meta.comm, meta.cmdline, session) {
			servers = append(servers, pid)
		}
	}

	if len(servers) == 0 {
		return ""
	}

	home, _ := os.UserHomeDir()
	best, bestScore := "", -1
	for _, serverPID := range servers {
		candidates := bfsChildren(children, serverPID, 4)
		for _, c := range candidates {
			cwd, err := os.Readlink(filepath.Join(root, strconv.Itoa(c.pid), "cwd"))
			if err != nil || strings.TrimSpace(cwd) == "" {
				continue
			}
			score := scoreSessionCWDCandidate(c.depth, metas[c.pid].comm, cwd, home)
			if score > bestScore {
				bestScore = score
				best = cwd
			}
		}
	}

	if bestScore < 0 {
		return ""
	}

	return best
}

func isZellijServerForSession(comm string, cmdline string, session string) bool {
	needle := "/" + strings.TrimSpace(session)
	return strings.EqualFold(comm, "zellij") && strings.Contains(cmdline, "--server") && strings.Contains(cmdline, needle)
}

func scoreSessionCWDCandidate(depth int, comm string, cwd string, home string) int {
	score := depth * 10
	if isInteractiveComm(comm) {
		score += 50
	}
	if home != "" && cwd != home {
		score += 20
	}
	return score
}

type childCandidate struct {
	pid   int
	depth int
}

func bfsChildren(children map[int][]int, rootPID int, maxDepth int) []childCandidate {
	queue := []childCandidate{{pid: rootPID, depth: 0}}
	out := make([]childCandidate, 0, 16)
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		if curr.depth >= maxDepth {
			continue
		}
		for _, child := range children[curr.pid] {
			next := childCandidate{pid: child, depth: curr.depth + 1}
			out = append(out, next)
			queue = append(queue, next)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].depth != out[j].depth {
			return out[i].depth < out[j].depth
		}
		return out[i].pid < out[j].pid
	})
	return out
}
