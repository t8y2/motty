package workbench

import (
	"testing"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

func TestPlainTextOffsetAtUsesWideRuneBoundaries(t *testing.T) {
	var canvas gfx.Canvas
	var view aiPlainTextView
	view.layoutMono("中文", 8, aiMaxOutput, 10, 16)
	view.canvas = &canvas
	view.rect = gfx.Rect{Width: 40, Height: 16}

	offset, ok := view.OffsetAt(gfx.Point{X: 15})
	if !ok || offset != len("中") {
		t.Fatalf("offset inside the first wide rune = %d, %v; want %d", offset, ok, len("中"))
	}
	if got := view.SelectedText(components.MarkdownSelection{Focus: offset}); got != "中" {
		t.Fatalf("selected %q, want the whole first rune", got)
	}
	offset, ok = view.OffsetAt(gfx.Point{X: 25})
	if !ok || offset != len("中") {
		t.Fatalf("offset near the second rune's start = %d, %v; want %d", offset, ok, len("中"))
	}
	offset, ok = view.OffsetAt(gfx.Point{X: 35})
	if !ok || offset != len("中文") {
		t.Fatalf("offset near the second rune's end = %d, %v; want %d", offset, ok, len("中文"))
	}
}
