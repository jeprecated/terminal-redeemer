package zellijlive

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const maxProcessNodes = 256
const maxProcessDepth = 8
const maxProcessMetadataBytes = 64 << 10

var kittyProcessBasenames = map[string]struct{}{"kitty": {}, "kitty.bin": {}}
var zellijProcessBasenames = map[string]struct{}{"zellij": {}}

var ErrProcessObservationIncomplete = errors.New("process observation incomplete")

type ProcObserver struct{ ProcRoot string }

func (observer ProcObserver) Observe(ctx context.Context, pid int) (ProcessEvidence, error) {
	if pid <= 0 {
		return ProcessEvidence{}, fmt.Errorf("invalid Kitty PID")
	}
	root := observer.ProcRoot
	if root == "" {
		root = "/proc"
	}
	if err := ctx.Err(); err != nil {
		return ProcessEvidence{}, err
	}
	verified, err := verifyKitty(root, pid)
	if err != nil {
		return ProcessEvidence{}, processIncomplete(pid, "verify root Kitty metadata", err)
	}
	type node struct{ pid, parent, depth int }
	queue := []node{{pid: pid}}
	seen := map[int]struct{}{}
	candidates := map[string]struct{}{}
	for len(queue) > 0 && len(seen) < maxProcessNodes {
		if err := ctx.Err(); err != nil {
			return ProcessEvidence{}, err
		}
		current := queue[0]
		queue = queue[1:]
		if _, ok := seen[current.pid]; ok {
			return ProcessEvidence{}, processIncomplete(current.pid, "traverse children", errors.New("duplicate or cyclic child edge"))
		}
		seen[current.pid] = struct{}{}
		parentPID, err := readProcessParent(root, current.pid)
		if err != nil {
			return ProcessEvidence{}, processIncomplete(current.pid, "read process identity", err)
		}
		if current.parent > 0 && parentPID != current.parent {
			return ProcessEvidence{}, processIncomplete(current.pid, "validate parent edge", fmt.Errorf("parent changed from %d to %d", current.parent, parentPID))
		}
		children, err := readProcessChildren(root, current.pid)
		if err != nil {
			return ProcessEvidence{}, processIncomplete(current.pid, "read children edge", err)
		}
		if current.pid != pid {
			args, argsErr := readNull(filepath.Join(root, strconv.Itoa(current.pid), "cmdline"))
			envValues, envErr := readNull(filepath.Join(root, strconv.Itoa(current.pid), "environ"))
			if argsErr != nil || envErr != nil {
				return ProcessEvidence{}, processIncomplete(current.pid, "read descendant metadata", errors.Join(argsErr, envErr))
			}
			for _, name := range candidatesFrom(args, envValues) {
				if name != "" {
					candidates[name] = struct{}{}
				}
			}
		}
		if current.depth >= maxProcessDepth {
			if len(children) > 0 {
				return ProcessEvidence{}, processIncomplete(current.pid, "traverse children", errors.New("depth bound exceeded"))
			}
			continue
		}
		for _, child := range children {
			queue = append(queue, node{pid: child, parent: current.pid, depth: current.depth + 1})
		}
	}
	if len(queue) > 0 {
		return ProcessEvidence{}, processIncomplete(pid, "traverse children", errors.New("node bound exceeded"))
	}
	out := make([]string, 0, len(candidates))
	for candidate := range candidates {
		out = append(out, candidate)
	}
	sort.Strings(out)
	return ProcessEvidence{KittyVerified: verified, Candidates: out}, nil
}

func verifyKitty(root string, pid int) (bool, error) {
	processDir := filepath.Join(root, strconv.Itoa(pid))
	if _, err := os.Stat(processDir); err != nil {
		return false, fmt.Errorf("inspect Kitty process: %w", err)
	}
	executable, executableErr := os.Readlink(filepath.Join(processDir, "exe"))
	if executableErr == nil {
		basename := strings.ToLower(filepath.Base(executable))
		if basename == "" || basename == "." || basename == string(filepath.Separator) {
			return false, fmt.Errorf("invalid Kitty executable basename")
		}
		if _, ok := kittyProcessBasenames[basename]; ok {
			return true, nil
		}
	}
	payload, commErr := os.ReadFile(filepath.Join(processDir, "comm"))
	if commErr == nil {
		if !utf8.Valid(payload) {
			return false, fmt.Errorf("Kitty comm is not valid UTF-8")
		}
		comm := strings.ToLower(strings.TrimSpace(string(payload)))
		if comm == "" || strings.ContainsAny(comm, "/\\\x00") {
			return false, fmt.Errorf("invalid Kitty comm basename")
		}
		if _, ok := kittyProcessBasenames[comm]; ok {
			return true, nil
		}
	}
	if executableErr != nil && commErr != nil {
		return false, fmt.Errorf("read Kitty executable and comm: %w", errors.Join(executableErr, commErr))
	}
	return false, nil
}

func candidatesFrom(args, environ []string) []string {
	if len(args) == 0 {
		return nil
	}
	if _, ok := zellijProcessBasenames[strings.ToLower(filepath.Base(args[0]))]; !ok {
		return nil
	}
	values := make(map[string]struct{})
	for _, item := range environ {
		if strings.HasPrefix(item, "ZELLIJ_SESSION_NAME=") {
			if candidate := strings.TrimSpace(strings.TrimPrefix(item, "ZELLIJ_SESSION_NAME=")); candidate != "" {
				values[candidate] = struct{}{}
			}
		}
	}
	for i := 1; i < len(args); i++ {
		if args[i] == "attach" {
			for j := i + 1; j < len(args); j++ {
				if args[j] == "--" {
					continue
				}
				if !strings.HasPrefix(args[j], "-") {
					if candidate := strings.TrimSpace(args[j]); candidate != "" {
						values[candidate] = struct{}{}
					}
					break
				}
			}
		}
		if (args[i] == "--session" || args[i] == "-s") && i+1 < len(args) {
			if candidate := strings.TrimSpace(args[i+1]); candidate != "" {
				values[candidate] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func readNull(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	payload, err := io.ReadAll(io.LimitReader(file, maxProcessMetadataBytes+1))
	if err != nil {
		return nil, err
	}
	if len(payload) > maxProcessMetadataBytes {
		return nil, fmt.Errorf("process metadata exceeds bound")
	}
	if !utf8.Valid(payload) {
		return nil, fmt.Errorf("process metadata is not valid UTF-8")
	}
	parts := bytes.Split(payload, []byte{0})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(string(part)); value != "" {
			out = append(out, value)
		}
	}
	return out, nil
}

func readProcessParent(root string, pid int) (int, error) {
	path := filepath.Join(root, strconv.Itoa(pid), "stat")
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	payload, err := io.ReadAll(io.LimitReader(file, maxProcessMetadataBytes+1))
	if err != nil {
		return 0, err
	}
	if len(payload) > maxProcessMetadataBytes {
		return 0, fmt.Errorf("process stat exceeds bound")
	}
	text := string(payload)
	end := strings.LastIndex(text, ")")
	if end < 0 || end+1 >= len(text) {
		return 0, fmt.Errorf("malformed process stat")
	}
	fields := strings.Fields(text[end+1:])
	if len(fields) < 2 {
		return 0, fmt.Errorf("malformed process stat fields")
	}
	parent, err := strconv.Atoi(fields[1])
	if err != nil || parent < 0 {
		return 0, fmt.Errorf("invalid process parent PID")
	}
	return parent, nil
}

func readProcessChildren(root string, pid int) ([]int, error) {
	taskRoot := filepath.Join(root, strconv.Itoa(pid), "task")
	tasks, err := os.ReadDir(taskRoot)
	if err != nil {
		return nil, err
	}
	if len(tasks) > maxProcessNodes {
		return nil, errors.New("task bound exceeded")
	}
	seen := map[int]struct{}{}
	for _, task := range tasks {
		if !task.IsDir() {
			continue
		}
		if _, err := strconv.Atoi(task.Name()); err != nil {
			return nil, fmt.Errorf("invalid task ID %q", task.Name())
		}
		file, err := os.Open(filepath.Join(taskRoot, task.Name(), "children"))
		if err != nil {
			return nil, err
		}
		payload, readErr := io.ReadAll(io.LimitReader(file, maxProcessMetadataBytes+1))
		closeErr := file.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if len(payload) > maxProcessMetadataBytes {
			return nil, fmt.Errorf("children edge exceeds bound")
		}
		taskSeen := map[int]struct{}{}
		for _, field := range strings.Fields(string(payload)) {
			child, err := strconv.Atoi(field)
			if err != nil || child <= 0 {
				return nil, fmt.Errorf("invalid child PID %q", field)
			}
			if _, duplicate := taskSeen[child]; duplicate {
				return nil, fmt.Errorf("duplicate child PID %d", child)
			}
			taskSeen[child] = struct{}{}
			seen[child] = struct{}{}
		}
	}
	children := make([]int, 0, len(seen))
	for child := range seen {
		children = append(children, child)
	}
	sort.Ints(children)
	return children, nil
}

func processIncomplete(pid int, operation string, err error) error {
	return fmt.Errorf("%w: pid %d %s: %v", ErrProcessObservationIncomplete, pid, operation, err)
}
