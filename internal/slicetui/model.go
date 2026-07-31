package slicetui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/jmo/terminal-redeemer/internal/slicecontroller"
)

type rowKind int

const (
	rowAll rowKind = iota
	rowWorkspace
	rowGroup
	rowSource
	rowDrop
)

type row struct {
	Kind          rowKind
	ID            string
	WorkspaceKey  string
	WorkspaceName string
	SourceID      string
	SessionID     string
	SessionName   string
	Selected      bool
	Pickup        bool
	Discoverable  bool
	Desired       bool
	Closed        bool
	Projection    slicecontroller.ProjectionStatus
	Connection    slicecontroller.ConnectionState
	Lifecycle     slicecontroller.SourceLifecycle
	Conflicts     []string
}

func rowsForState(state slicecontroller.State) []row {
	rows := []row{{Kind: rowAll, ID: "all", Selected: state.AllEligible}}

	type sourceView struct {
		tracked slicecontroller.TrackedSource
		name    string
		key     string
		live    bool
	}
	sources := make(map[string]sourceView, len(state.Sources))
	for id, tracked := range state.Sources {
		sources[id] = sourceView{tracked: tracked, key: tracked.WorkspaceKey}
	}
	if state.Inventory != nil {
		for _, source := range state.Inventory.Sources {
			view := sources[source.SourceID]
			if view.tracked.SourceID == "" {
				view.tracked = slicecontroller.TrackedSource{
					SourceID: source.SourceID, SessionID: source.Session.ID,
					SessionName: source.Session.Name, WorkspaceKey: source.Workspace.Key,
					Lifecycle: slicecontroller.SourceEligible,
				}
			}
			view.name = source.Workspace.Name
			view.key = source.Workspace.Key
			view.live = true
			sources[source.SourceID] = view
		}
	}

	groups := map[string]string{}
	for key, display := range state.SelectedWorkspaces {
		groups[key] = display
	}
	for _, source := range sources {
		if source.key == "" {
			groups[""] = "(unnamed)"
			continue
		}
		name := source.name
		if name == "" {
			name = state.SelectedWorkspaces[source.key]
		}
		if name == "" {
			name = source.key
		}
		groups[source.key] = name
	}

	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i] == "" {
			return false
		}
		if keys[j] == "" {
			return true
		}
		left, right := strings.ToLower(groups[keys[i]]), strings.ToLower(groups[keys[j]])
		if left == right {
			return keys[i] < keys[j]
		}
		return left < right
	})

	matchedSessions := map[string]bool{}
	for _, key := range keys {
		kind := rowWorkspace
		id := "workspace:" + key
		if key == "" {
			kind = rowGroup
			id = "group:unnamed-workspace"
		}
		rows = append(rows, row{Kind: kind, ID: id, WorkspaceKey: key, WorkspaceName: groups[key], Selected: state.SelectedWorkspaces[key] != ""})

		var ids []string
		for id, source := range sources {
			if source.key == key {
				ids = append(ids, id)
			}
		}
		sort.Slice(ids, func(i, j int) bool {
			a, b := sources[ids[i]], sources[ids[j]]
			if a.tracked.SessionName != b.tracked.SessionName {
				return a.tracked.SessionName < b.tracked.SessionName
			}
			return ids[i] < ids[j]
		})
		for _, id := range ids {
			view := sources[id]
			tracked := view.tracked
			_, closed := state.ClosedByUser[tracked.SessionID]
			matchedSessions[tracked.SessionID] = true
			projection := state.Projections[id]
			rows = append(rows, row{
				Kind: rowSource, ID: "source:" + id, WorkspaceKey: key, WorkspaceName: groups[key],
				SourceID: id, SessionID: tracked.SessionID, SessionName: tracked.SessionName,
				Pickup: state.Pickups[id], Discoverable: view.live, Desired: policyDesired(state, tracked, id, closed), Closed: closed,
				Projection: projection.Status, Connection: tracked.Connection, Lifecycle: tracked.Lifecycle,
				Conflicts: conflictsForSource(state, id, tracked),
			})
		}
	}

	var orphanIDs []string
	for id := range state.ClosedByUser {
		if !matchedSessions[id] {
			orphanIDs = append(orphanIDs, id)
		}
	}
	sort.Strings(orphanIDs)
	for _, id := range orphanIDs {
		drop := state.ClosedByUser[id]
		rows = append(rows, row{Kind: rowDrop, ID: "drop:" + id, SessionID: id, SessionName: drop.SessionName, Closed: true})
	}
	return rows
}

func policyDesired(state slicecontroller.State, source slicecontroller.TrackedSource, sourceID string, closed bool) bool {
	return !closed && (state.AllEligible || state.Pickups[sourceID] || state.SelectedWorkspaces[source.WorkspaceKey] != "")
}

func conflictsForSource(state slicecontroller.State, sourceID string, source slicecontroller.TrackedSource) []string {
	conflicts := []string{}
	if source.Conflict != "" {
		conflicts = append(conflicts, "tracked="+source.Conflict)
	}
	if conflict := state.Spatial[sourceID].Conflict; conflict != "" {
		conflicts = append(conflicts, "spatial="+conflict)
	}
	if conflict := state.PendingCleanups[sourceID].Conflict; conflict != "" {
		conflicts = append(conflicts, "cleanup="+conflict)
	}
	var successorConflicts []string
	for oldID, gate := range state.SuccessorGates {
		if gate.Conflict != "" && (oldID == sourceID || gate.SessionID == source.SessionID) {
			successorConflicts = append(successorConflicts, gate.Conflict)
		}
	}
	sort.Strings(successorConflicts)
	for i, conflict := range successorConflicts {
		if i == 0 || conflict != successorConflicts[i-1] {
			conflicts = append(conflicts, "successor="+conflict)
		}
	}
	for oldID, record := range state.Lineage {
		if record.Status == "conflict" && (oldID == sourceID || record.SuccessorSourceID == sourceID || record.SessionID == source.SessionID) {
			conflicts = append(conflicts, "lineage=conflict")
			break
		}
	}
	return conflicts
}

func (r row) label() string {
	switch r.Kind {
	case rowAll:
		return fmt.Sprintf("%s all eligible sources", mark(r.Selected))
	case rowWorkspace:
		return fmt.Sprintf("%s workspace %s", mark(r.Selected), r.WorkspaceName)
	case rowGroup:
		return "[-] workspace (unnamed)"
	case rowDrop:
		return fmt.Sprintf("    session %s [closed] [source absent]", displaySession(r))
	case rowSource:
		return fmt.Sprintf("    session %s %s", displaySession(r), bracket(r.badges()))
	default:
		return ""
	}
}

// renderLines keeps controller facts at the left edge and spends only the
// remaining terminal cells on operator-controlled names. Badge groups wrap as
// whole facts when needed; names are the only content intentionally truncated.
func (r row) renderLines(width int) []string {
	switch r.Kind {
	case rowWorkspace:
		return []string{fitNamedLine(fmt.Sprintf("%s workspace ", mark(r.Selected)), r.WorkspaceName, width)}
	case rowDrop:
		return []string{fitNamedLine("    [closed] [source absent] session ", displaySession(r), width)}
	case rowSource:
		if width <= 0 {
			return []string{r.label()}
		}
		const indent = "    "
		lines := []string{}
		current := indent
		for _, badge := range r.badges() {
			token := "[" + badge + "]"
			separator := ""
			if current != indent {
				separator = " "
			}
			if current != indent && ansi.StringWidth(current+separator+token) > width {
				lines = append(lines, current)
				current = indent
				separator = ""
			}
			available := width - ansi.StringWidth(indent)
			if available > 0 {
				current += separator + clipCells(token, available)
			}
		}
		namePrefix := current + " session "
		if ansi.StringWidth(namePrefix) >= width && current != indent {
			lines = append(lines, current)
			namePrefix = indent + "session "
		}
		lines = append(lines, fitNamedLine(namePrefix, displaySession(r), width))
		return lines
	default:
		return []string{clipCells(r.label(), width)}
	}
}

func (r row) badges() []string {
	badges := []string{}
	if r.Discoverable {
		badges = append(badges, "discoverable")
	} else {
		badges = append(badges, "not-discoverable")
	}
	if r.Desired {
		badges = append(badges, "desired")
	}
	if r.Pickup {
		badges = append(badges, "pickup")
	}
	if r.Closed {
		badges = append(badges, "closed")
	}
	if r.Projection != "" {
		badges = append(badges, "projection="+string(r.Projection))
	} else {
		badges = append(badges, "projection=none")
	}
	if r.Connection != "" {
		badges = append(badges, "connection="+string(r.Connection))
	}
	if r.Lifecycle != "" {
		badges = append(badges, "lifecycle="+string(r.Lifecycle))
	}
	for _, conflict := range r.Conflicts {
		badges = append(badges, "conflict:"+conflict)
	}
	return badges
}

func fitNamedLine(fixed, name string, width int) string {
	if width <= 0 {
		return fixed + name
	}
	fixedWidth := ansi.StringWidth(fixed)
	if fixedWidth >= width {
		return ansi.Truncate(fixed, width, "")
	}
	return fixed + ansi.Truncate(name, width-fixedWidth, "…")
}

func clipCells(value string, width int) string {
	if width <= 0 {
		return value
	}
	return ansi.Truncate(value, width, "")
}

func displaySession(r row) string {
	if r.SessionName != "" {
		return r.SessionName
	}
	if r.SessionID != "" {
		return r.SessionID
	}
	return r.SourceID
}

func mark(selected bool) string {
	if selected {
		return "[x]"
	}
	return "[ ]"
}

func bracket(values []string) string {
	parts := make([]string, len(values))
	for i, value := range values {
		parts[i] = "[" + value + "]"
	}
	return strings.Join(parts, " ")
}

func workspacePayload(r row) slicecontroller.WorkspacePayload {
	return slicecontroller.WorkspacePayload{Name: r.WorkspaceName}
}

func sourcePayload(r row) slicecontroller.SourcePayload {
	return slicecontroller.SourcePayload{SourceID: r.SourceID}
}
