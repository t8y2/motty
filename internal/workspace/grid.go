package workspace

import "errors"

const (
	MinGridColumns = 1
	MaxGridColumns = 3
)

var (
	ErrInvalidPaneCount = errors.New("workspace: invalid pane count")
	ErrInvalidColumns   = errors.New("workspace: invalid grid columns")
)

// NewGrid creates an equal-share layout in row-major order. The final row
// occupies the full width, matching how the panes read from left to right.
func NewGrid(sessions []SessionID, columns int) (Layout, error) {
	if len(sessions) == 0 || len(sessions) > MaxPanes {
		return Layout{}, ErrInvalidPaneCount
	}
	if columns < MinGridColumns || columns > MaxGridColumns {
		return Layout{}, ErrInvalidColumns
	}
	if err := checkSessions(sessions); err != nil {
		return Layout{}, err
	}
	tree := buildGridRows(sessions, columns)
	nodes := make([]NodeDTO, 0, len(sessions)*2-1)
	appendGridDTO(&nodes, tree)
	return Restore(DTO{Version: DTOVersion, Root: 1, Nodes: nodes})
}

func checkSessions(sessions []SessionID) error {
	seen := make(map[SessionID]struct{}, len(sessions))
	for _, session := range sessions {
		if session == 0 {
			return ErrInvalidSession
		}
		if _, exists := seen[session]; exists {
			return ErrSessionExists
		}
		seen[session] = struct{}{}
	}
	return nil
}

type gridNode struct {
	session SessionID
	axis    Axis
	ratio   float32
	first   *gridNode
	second  *gridNode
}

func buildGridRows(sessions []SessionID, columns int) *gridNode {
	rows := (len(sessions) + columns - 1) / columns
	var build func(start, count int) *gridNode
	build = func(start, count int) *gridNode {
		end := min(start+columns, len(sessions))
		row := buildGridRow(sessions[start:end])
		if len(sessions)-end == 0 {
			return row
		}
		return &gridNode{
			axis: Vertical, ratio: 1 / float32(count),
			first: row, second: build(end, count-1),
		}
	}
	return build(0, rows)
}

func buildGridRow(sessions []SessionID) *gridNode {
	if len(sessions) == 1 {
		return &gridNode{session: sessions[0]}
	}
	return &gridNode{
		axis: Horizontal, ratio: 1 / float32(len(sessions)),
		first:  &gridNode{session: sessions[0]},
		second: buildGridRow(sessions[1:]),
	}
}

func appendGridDTO(nodes *[]NodeDTO, node *gridNode) NodeID {
	id := NodeID(len(*nodes) + 1)
	nodesIndex := len(*nodes)
	*nodes = append(*nodes, NodeDTO{ID: id, Kind: KindLeaf, Session: node.session})
	if node.first == nil {
		return id
	}
	first := appendGridDTO(nodes, node.first)
	second := appendGridDTO(nodes, node.second)
	(*nodes)[nodesIndex] = NodeDTO{
		ID: id, Kind: KindSplit, Axis: node.axis, Ratio: node.ratio,
		First: first, Second: second,
	}
	return id
}
