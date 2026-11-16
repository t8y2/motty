package workbench

import layoutcore "motty/internal/workspace"

// isGrid reports whether spec is the grid of that many columns.
func isGrid(spec layoutcore.LayoutSpec, columns uint8) bool {
	return spec.Equal(layoutcore.GridSpec(columns))
}

// columnsSpec is the grid of that many columns, valid or not.
func columnsSpec(columns uint8) layoutcore.LayoutSpec {
	return layoutcore.LayoutSpec{Kind: layoutcore.LayoutGrid, Columns: columns}
}
