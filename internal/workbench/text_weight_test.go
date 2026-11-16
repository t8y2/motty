package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

// A heavier face is wider: on Windows semibold Segoe UI is wide enough that a
// button sized from the regular-weight measurement clipped "New Host" to
// "New Hos". Widths must come from the weight the label is drawn at.
func TestToolbarButtonsAreSizedForTheirSemiboldLabel(t *testing.T) {
	var canvas gfx.Canvas
	canvas.SetMeasurer(func(value string, size, weight float32) float32 {
		return float32(len([]rune(value))) * size * (0.5 + weight)
	})
	const label = "Import SSH Config"
	semibold := canvas.TextWidthWeight(label, 12, gfx.WeightSemibold)
	if semibold <= canvas.TextWidth(label, 12) {
		t.Fatal("test measurer does not grow with weight")
	}
	if width := toolbarButtonWidth(&canvas, label, false); width < toolbarPadding*2+semibold {
		t.Fatalf("button is %.1f wide, less than padding plus its semibold label (%.1f)", width, toolbarPadding*2+semibold)
	}
}

func TestElidedTextFitsAtTheWeightItIsDrawnWith(t *testing.T) {
	var canvas gfx.Canvas
	canvas.SetMeasurer(func(value string, size, weight float32) float32 {
		return float32(len([]rune(value))) * size * (0.5 + weight)
	})
	const width = 100
	regular := elide(&canvas, "a long workspace title", 12, gfx.WeightRegular, width)
	heavy := elide(&canvas, "a long workspace title", 12, gfx.WeightSemibold, width)
	if canvas.TextWidthWeight(regular, 12, gfx.WeightRegular) > width || canvas.TextWidthWeight(heavy, 12, gfx.WeightSemibold) > width {
		t.Fatalf("elided text does not fit: %q, %q", regular, heavy)
	}
	if len([]rune(heavy)) >= len([]rune(regular)) {
		t.Fatalf("the heavier text kept as many letters (%q) as the regular one (%q)", heavy, regular)
	}
}

// elide halves its way to the longest prefix that fits. It must land where
// dropping one rune at a time lands, whatever the glyph widths and the room.
func TestElideFindsTheLongestPrefixThatFits(t *testing.T) {
	var canvas gfx.Canvas
	canvas.SetMeasurer(func(value string, size, weight float32) float32 {
		var width float32
		for _, r := range value {
			switch {
			case r >= 0x2e80:
				width += size
			case r == 'i' || r == '/':
				width += size * .3
			default:
				width += size * .55
			}
		}
		return width
	})
	linear := func(value string, width float32) string {
		if canvas.TextWidthWeight(value, 12, gfx.WeightRegular) <= width {
			return value
		}
		runes := []rune(value)
		for len(runes) > 1 {
			runes = runes[:len(runes)-1]
			if candidate := string(runes) + "…"; canvas.TextWidthWeight(candidate, 12, gfx.WeightRegular) <= width {
				return candidate
			}
		}
		return "…"
	}
	for _, value := range []string{"", "a", "/var/lib/docker/volumes/0123/_data", "目录名称很长的挂载点/data/卷", "iiiiiiiiiiiiiiiiiiiiii"} {
		for width := float32(0); width <= 260; width += 3.5 {
			if got, want := elide(&canvas, value, 12, gfx.WeightRegular, width), linear(value, width); got != want {
				t.Fatalf("elide(%q, %v) = %q, one rune at a time gives %q", value, width, got, want)
			}
		}
	}
}
