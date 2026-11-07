package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
)

// LayoutKind names how a workspace arranges its panes.
type LayoutKind uint8

const (
	// LayoutGrid is the equal-share row-major grid of NewGrid.
	LayoutGrid LayoutKind = iota + 1
	// LayoutMainRight gives the first pane the left 60% and stacks the rest on the right.
	LayoutMainRight
	// LayoutMainBottom gives the first pane the top 60% and lines the rest up below it.
	LayoutMainBottom
	// LayoutTabs shows one pane at a time. Its split tree is a one-column grid
	// that is never displayed; only the active pane is.
	LayoutTabs
	// LayoutCustom is an arbitrary split tree edited by the user.
	LayoutCustom
)

const mainShare = float32(0.6)

// ErrInvalidSpec means a LayoutSpec does not describe a layout of its panes.
var ErrInvalidSpec = errors.New("workspace: invalid layout spec")

// LayoutSpec is the persistent description of a workspace's arrangement,
// independent of which sessions fill it. Panes are addressed by position: the
// order of the pane list the spec is applied to.
type LayoutSpec struct {
	Kind LayoutKind
	// Columns is the grid width of LayoutGrid and zero for every other kind.
	Columns uint8
	// Tree is the split tree of LayoutCustom, whose leaf sessions are 1-based
	// pane positions, and empty for every other kind.
	Tree DTO
}

// GridSpec is the grid layout with the given number of columns.
func GridSpec(columns uint8) LayoutSpec { return LayoutSpec{Kind: LayoutGrid, Columns: columns} }

// Presets is every layout a user can pick without drawing a tree.
func Presets() []LayoutSpec {
	return []LayoutSpec{
		GridSpec(1), GridSpec(2), GridSpec(3),
		{Kind: LayoutMainRight}, {Kind: LayoutMainBottom}, {Kind: LayoutTabs},
	}
}

// Tabbed reports whether the layout shows a single pane at a time.
func (spec LayoutSpec) Tabbed() bool { return spec.Kind == LayoutTabs }

// GridColumns is the width keyboard navigation over the panes' cards moves by:
// the grid's own, and one for every other kind.
func (spec LayoutSpec) GridColumns() uint8 {
	if spec.Kind == LayoutGrid {
		return spec.Columns
	}
	return 1
}

// Equal reports whether two specs describe the same arrangement.
func (spec LayoutSpec) Equal(other LayoutSpec) bool {
	if spec.Kind != other.Kind || spec.Columns != other.Columns || spec.Tree.Root != other.Tree.Root ||
		len(spec.Tree.Nodes) != len(other.Tree.Nodes) {
		return false
	}
	for index := range spec.Tree.Nodes {
		if spec.Tree.Nodes[index] != other.Tree.Nodes[index] {
			return false
		}
	}
	return true
}

// Validate checks that the spec is well formed for count panes.
func (spec LayoutSpec) Validate(count int) error {
	if count < 1 || count > MaxPanes {
		return ErrInvalidPaneCount
	}
	switch spec.Kind {
	case LayoutGrid:
		if spec.Columns < MinGridColumns || spec.Columns > MaxGridColumns {
			return ErrInvalidColumns
		}
		if len(spec.Tree.Nodes) != 0 {
			return fmt.Errorf("%w: grid carries a tree", ErrInvalidSpec)
		}
	case LayoutMainRight, LayoutMainBottom, LayoutTabs:
		if spec.Columns != 0 || len(spec.Tree.Nodes) != 0 {
			return fmt.Errorf("%w: kind %d carries grid or tree data", ErrInvalidSpec, spec.Kind)
		}
	case LayoutCustom:
		if spec.Columns != 0 {
			return fmt.Errorf("%w: custom layout has columns", ErrInvalidSpec)
		}
		if err := spec.Tree.Validate(); err != nil {
			return err
		}
		if len(spec.Tree.Nodes) != count*2-1 {
			return fmt.Errorf("%w: tree has %d nodes for %d panes", ErrInvalidSpec, len(spec.Tree.Nodes), count)
		}
		// Positions are reading order, so the cards of an editor and the panes of
		// the workspace line up one to one: pre-order visits leaves 1, 2, 3...
		next := SessionID(1)
		for _, node := range spec.Tree.Nodes {
			if node.Kind != KindLeaf {
				continue
			}
			if node.Session != next {
				return fmt.Errorf("%w: leaf %d is not in reading order", ErrInvalidSpec, node.Session)
			}
			next++
		}
	default:
		return fmt.Errorf("%w: kind %d", ErrInvalidSpec, spec.Kind)
	}
	return nil
}

// NewLayout arranges sessions, in position order, as spec describes.
func NewLayout(sessions []SessionID, spec LayoutSpec) (Layout, error) {
	if err := spec.Validate(len(sessions)); err != nil {
		return Layout{}, err
	}
	switch spec.Kind {
	case LayoutGrid:
		return NewGrid(sessions, int(spec.Columns))
	case LayoutTabs:
		return NewGrid(sessions, 1)
	case LayoutCustom:
		dto := DTO{Version: spec.Tree.Version, Root: spec.Tree.Root, Nodes: append([]NodeDTO(nil), spec.Tree.Nodes...)}
		for index := range dto.Nodes {
			if dto.Nodes[index].Kind == KindLeaf {
				dto.Nodes[index].Session = sessions[dto.Nodes[index].Session-1]
			}
		}
		return Restore(dto)
	}
	if err := checkSessions(sessions); err != nil {
		return Layout{}, err
	}
	tree := buildMain(sessions, spec.Kind)
	nodes := make([]NodeDTO, 0, len(sessions)*2-1)
	appendGridDTO(&nodes, tree)
	return Restore(DTO{Version: DTOVersion, Root: 1, Nodes: nodes})
}

// Spec expresses the layout as a custom spec. Its leaves are numbered by
// position in reading order, whatever sessions they hold now.
func (layout *Layout) Spec() LayoutSpec {
	dto := layout.DTO()
	next := SessionID(1)
	for index := range dto.Nodes {
		if dto.Nodes[index].Kind == KindLeaf {
			dto.Nodes[index].Session = next
			next++
		}
	}
	return LayoutSpec{Kind: LayoutCustom, Tree: dto}
}

func buildMain(sessions []SessionID, kind LayoutKind) *gridNode {
	first := &gridNode{session: sessions[0]}
	if len(sessions) == 1 {
		return first
	}
	rest := sessions[1:]
	if kind == LayoutMainRight {
		return &gridNode{axis: Horizontal, ratio: mainShare, first: first, second: buildStack(rest)}
	}
	return &gridNode{axis: Vertical, ratio: mainShare, first: first, second: buildGridRow(rest)}
}

func buildStack(sessions []SessionID) *gridNode {
	if len(sessions) == 1 {
		return &gridNode{session: sessions[0]}
	}
	return &gridNode{
		axis: Vertical, ratio: 1 / float32(len(sessions)),
		first: &gridNode{session: sessions[0]}, second: buildStack(sessions[1:]),
	}
}

var kindNames = [...]string{LayoutGrid: "grid", LayoutMainRight: "main-right", LayoutMainBottom: "main-bottom", LayoutTabs: "tabs", LayoutCustom: "custom"}

type specJSON struct {
	Kind    string `json:"kind"`
	Columns uint8  `json:"columns,omitempty"`
	Tree    *DTO   `json:"tree,omitempty"`
}

// MarshalJSON writes the kind by name, so a stored layout stays readable and
// survives the reordering of the constants.
func (spec LayoutSpec) MarshalJSON() ([]byte, error) {
	if spec.Kind < LayoutGrid || spec.Kind > LayoutCustom {
		return nil, fmt.Errorf("%w: kind %d", ErrInvalidSpec, spec.Kind)
	}
	out := specJSON{Kind: kindNames[spec.Kind], Columns: spec.Columns}
	if spec.Kind == LayoutCustom {
		out.Tree = &spec.Tree
	}
	return json.Marshal(out)
}

// UnmarshalJSON reads MarshalJSON's form. Whether the spec fits a particular
// pane count is Validate's question.
func (spec *LayoutSpec) UnmarshalJSON(data []byte) error {
	var in specJSON
	if err := json.Unmarshal(data, &in); err != nil {
		return err
	}
	for kind := LayoutGrid; kind <= LayoutCustom; kind++ {
		if kindNames[kind] != in.Kind {
			continue
		}
		next := LayoutSpec{Kind: kind, Columns: in.Columns}
		if in.Tree != nil {
			next.Tree = *in.Tree
		}
		*spec = next
		return nil
	}
	return fmt.Errorf("%w: kind %q", ErrInvalidSpec, in.Kind)
}

// Retain drops the panes at positions outside keep from a custom layout whose
// hosts are partly gone, closing the gaps and renumbering the rest in reading
// order. Presets and grids need no change, since they are independent of how
// many panes remain.
func (spec LayoutSpec) Retain(keep []int, total int) (LayoutSpec, error) {
	if err := spec.Validate(total); err != nil {
		return LayoutSpec{}, err
	}
	if spec.Kind != LayoutCustom || len(keep) == total {
		return spec, nil
	}
	if len(keep) == 0 {
		return LayoutSpec{}, ErrInvalidPaneCount
	}
	layout, err := NewLayout(sessionIDs(total), spec)
	if err != nil {
		return LayoutSpec{}, err
	}
	kept := make(map[int]struct{}, len(keep))
	for _, position := range keep {
		if position < 1 || position > total {
			return LayoutSpec{}, ErrInvalidSpec
		}
		kept[position] = struct{}{}
	}
	for position := 1; position <= total; position++ {
		if _, ok := kept[position]; !ok {
			if err := layout.Remove(SessionID(position)); err != nil {
				return LayoutSpec{}, err
			}
		}
	}
	return layout.Spec(), nil
}

func sessionIDs(count int) []SessionID {
	sessions := make([]SessionID, count)
	for index := range sessions {
		sessions[index] = SessionID(index + 1)
	}
	return sessions
}

// SameShape reports whether two layouts divide the plane the same way: the
// same tree of axes with the same sessions in the same places. Divider ratios
// are the user's to drag and do not count.
func (layout *Layout) SameShape(other *Layout) bool {
	a, b := layout.DTO(), other.DTO()
	if len(a.Nodes) != len(b.Nodes) {
		return false
	}
	for index := range a.Nodes {
		x, y := a.Nodes[index], b.Nodes[index]
		if x.Kind != y.Kind || x.Axis != y.Axis || x.Session != y.Session {
			return false
		}
	}
	return true
}

// MovePosition rearranges the layout of count panes by docking the pane at
// position from beside the pane at position to. The result is a custom spec
// numbered in the new reading order, and order lists the 1-based position each
// of those places was filled from, so the panes can be put in step with it.
func (spec LayoutSpec) MovePosition(count, from, to int, axis Axis, placement Placement) (LayoutSpec, []int, error) {
	layout, err := NewLayout(sessionIDs(count), spec)
	if err != nil {
		return LayoutSpec{}, nil, err
	}
	if from < 1 || from > count || to < 1 || to > count {
		return LayoutSpec{}, nil, ErrInvalidSpec
	}
	if err := layout.Move(SessionID(from), SessionID(to), axis, 0.5, placement); err != nil {
		return LayoutSpec{}, nil, err
	}
	order := make([]int, 0, count)
	for _, session := range layout.Sessions(nil) {
		order = append(order, int(session))
	}
	return layout.Spec(), order, nil
}
