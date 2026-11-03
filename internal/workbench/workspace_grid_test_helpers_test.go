package workbench

import (
	"math"
	"testing"

	layoutcore "motty/internal/workspace"
)

type gridRect struct {
	x      float32
	y      float32
	width  float32
	height float32
}

func assertGridRects(t *testing.T, layout *layoutcore.Layout, want []gridRect) {
	t.Helper()
	got := appendGridRects(nil, layout, layout.Root(), gridRect{width: 1, height: 1})
	if len(got) != len(want) {
		t.Fatalf("rectangles = %v, want %v", got, want)
	}
	for index := range want {
		if math.Abs(float64(got[index].x-want[index].x)) > 1e-6 ||
			math.Abs(float64(got[index].y-want[index].y)) > 1e-6 ||
			math.Abs(float64(got[index].width-want[index].width)) > 1e-6 ||
			math.Abs(float64(got[index].height-want[index].height)) > 1e-6 {
			t.Fatalf("rectangle %d = %+v, want %+v (all %v)", index, got[index], want[index], got)
		}
	}
}

func appendGridRects(dst []gridRect, layout *layoutcore.Layout, id layoutcore.NodeID, bounds gridRect) []gridRect {
	node, ok := layout.Node(id)
	if !ok {
		return dst
	}
	if node.Kind == layoutcore.KindLeaf {
		return append(dst, bounds)
	}
	first := bounds
	second := bounds
	if node.Axis == layoutcore.Horizontal {
		first.width *= node.Ratio
		second.x += first.width
		second.width -= first.width
	} else {
		first.height *= node.Ratio
		second.y += first.height
		second.height -= first.height
	}
	dst = appendGridRects(dst, layout, node.First, first)
	return appendGridRects(dst, layout, node.Second, second)
}
