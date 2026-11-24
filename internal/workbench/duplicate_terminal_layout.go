package workbench

import layoutcore "motty/internal/workspace"

// duplicateTerminalUsesGrid 仅对未调整过的自动网格重新均分。
func (state *State) duplicateTerminalUsesGrid(workspace layoutcore.WorkspaceID) bool {
	live, ok := state.deck.Layout(workspace)
	if !ok {
		return false
	}
	if spec, managed := state.managedWorkspaceLayouts[workspace]; managed && spec.Kind != layoutcore.LayoutGrid &&
		!(live.Len() >= 10 && spec.Equal(duplicateTerminalGrid(live.Len()))) {
		return false
	}
	expected, err := layoutcore.NewLayout(live.Sessions(nil), duplicateTerminalGrid(live.Len()))
	return err == nil && live.Spec().Equal(expected.Spec())
}

// duplicateTerminalGrid 将四列自动布局编码为旧版本支持的自定义树。
// 工作区模板及公共 GridSpec 仍只允许一至三列。
func duplicateTerminalGrid(count int) layoutcore.LayoutSpec {
	if count < 10 {
		columns := uint8(1)
		switch {
		case count >= 5:
			columns = 3
		case count >= 2:
			columns = 2
		}
		return layoutcore.GridSpec(columns)
	}
	nodes := make([]layoutcore.NodeDTO, 0, count*2-1)
	var build func(start, count int) layoutcore.NodeID
	build = func(start, count int) layoutcore.NodeID {
		index := len(nodes)
		id := layoutcore.NodeID(index + 1)
		nodes = append(nodes, layoutcore.NodeDTO{ID: id, Kind: layoutcore.KindLeaf, Session: layoutcore.SessionID(start)})
		if count == 1 {
			return id
		}
		firstCount, axis, ratio := 1, layoutcore.Horizontal, 1/float32(count)
		if count > 4 {
			firstCount, axis, ratio = 4, layoutcore.Vertical, 1/float32((count+3)/4)
		}
		first, second := build(start, firstCount), build(start+firstCount, count-firstCount)
		nodes[index] = layoutcore.NodeDTO{ID: id, Kind: layoutcore.KindSplit, Axis: axis, Ratio: ratio, First: first, Second: second}
		return id
	}
	root := build(1, count)
	return layoutcore.LayoutSpec{Kind: layoutcore.LayoutCustom, Tree: layoutcore.DTO{Version: layoutcore.DTOVersion, Root: root, Nodes: nodes}}
}
