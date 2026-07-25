package niriipc

import "encoding/json"

type State struct {
	Outputs    map[string]Output `json:"outputs"`
	Workspaces []Workspace       `json:"workspaces"`
	Windows    []Window          `json:"windows"`
}

type Output struct {
	Name    string  `json:"name"`
	Logical Logical `json:"logical"`
}

type Logical struct {
	X         int     `json:"x"`
	Y         int     `json:"y"`
	Width     int     `json:"width"`
	Height    int     `json:"height"`
	Scale     float64 `json:"scale"`
	Transform string  `json:"transform"`
}

type Workspace struct {
	ID        uint64  `json:"id"`
	Index     int     `json:"idx"`
	Name      *string `json:"name"`
	Output    *string `json:"output"`
	IsActive  bool    `json:"is_active"`
	IsFocused bool    `json:"is_focused"`
}

type Window struct {
	ID          uint64  `json:"id"`
	Title       string  `json:"title"`
	AppID       string  `json:"app_id"`
	PID         int     `json:"pid"`
	WorkspaceID *uint64 `json:"workspace_id"`
	IsFocused   bool    `json:"is_focused"`
	IsFloating  bool    `json:"is_floating"`
	Layout      Layout  `json:"layout"`
}

type Layout struct {
	Position   []int     `json:"pos_in_scrolling_layout"`
	TileSize   []float64 `json:"tile_size"`
	WindowSize []int     `json:"window_size"`
}

type eventEnvelope map[string]json.RawMessage
