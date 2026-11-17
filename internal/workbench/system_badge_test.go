package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/sshconn"
)

// TestBadgesAndTheIconListAgree keeps the badges and the dialog's list together:
// a system with a badge the list omits can never be chosen by hand, and a system
// the list offers without a badge shows the generic server glyph.
func TestBadgesAndTheIconListAgree(t *testing.T) {
	offered := make(map[sshconn.RemoteSystem]bool, len(hostSystemChoices))
	for _, choice := range hostSystemChoices {
		offered[choice.system] = true
		if _, ok := distroBadges[choice.system]; !ok {
			switch choice.system {
			case sshconn.SystemUbuntu, sshconn.SystemLinux, sshconn.SystemWindows, sshconn.SystemDarwin:
				// These draw their badge from the generic case in systemBadgeArt.
			default:
				t.Errorf("the icon list offers %v, which has no badge", choice.system)
			}
		}
	}
	for system := range distroBadges {
		if !offered[system] {
			t.Errorf("%v has a badge but the icon list does not offer it", system)
		}
	}
}

// TestEveryOfferedBadgeComposes guards the composition step: a badge whose glyph
// has no path commands would render as an empty square. The glyph is checked layer
// by layer, because an icon that names fill colors keeps its geometry in them and
// a composition that only reads the top level loses the mark.
func TestEveryOfferedBadgeComposes(t *testing.T) {
	for _, choice := range hostSystemChoices {
		if _, ok := distroBadges[choice.system]; !ok {
			continue
		}
		for _, dark := range []bool{true, false} {
			icon, ok := systemBadgeIcon(choice.system, dark)
			if !ok {
				t.Fatalf("%v has no badge (dark=%v)", choice.system, dark)
			}
			if len(icon.Layers) == 0 {
				t.Errorf("%v composes an empty badge (dark=%v)", choice.system, dark)
			}
			// Layer 0 is the square the badge sits on; the rest are the glyph.
			for index, layer := range icon.Layers[1:] {
				if len(layer.Commands) == 0 {
					t.Errorf("%v: glyph layer %d has no path commands (dark=%v)", choice.system, index, dark)
				}
			}
		}
	}
}

// TestBadgeGlyphsAreCentered guards the scaling step. A glyph is drawn from the
// top-left of its view box, so artwork that does not fill the box (a wordmark, a
// mark with padding) hugs an edge instead of sitting in the middle. The tolerance
// is two and a half badge units out of 24: enough for artwork that is itself a
// little lopsided, as the Linux glyph is.
func TestBadgeGlyphsAreCentered(t *testing.T) {
	const tolerance = 2.5
	for _, choice := range hostSystemChoices {
		art, ok := systemBadgeArt(choice.system, true)
		if !ok {
			continue
		}
		for _, glyph := range art.glyphs {
			icon := glyph.icon
			minX, minY, maxX, maxY := glyphBounds(icon)
			height := icon.ViewBox
			if icon.ViewHeight > 0 {
				height = icon.ViewHeight
			}
			scale := (badgeIconView - 2*badgeIconView*glyph.inset) / icon.ViewBox
			offX := absFloat((minX+maxX)/2-icon.ViewBox/2) * scale
			offY := absFloat((minY+maxY)/2-height/2) * scale
			if offX > tolerance || offY > tolerance {
				t.Errorf("%v: glyph sits %.1f, %.1f badge units off centre", choice.system, offX, offY)
			}
		}
	}
}

// glyphBounds is the extent of a glyph's geometry, in its own view-box units.
func glyphBounds(icon gfx.Icon) (minX, minY, maxX, maxY float32) {
	minX, minY, maxX, maxY = 0, 0, 0, 0
	first := true
	for _, command := range glyphCommands(icon) {
		if command.Kind == gfx.PathCommandClose {
			continue
		}
		points := []gfx.Point{command.Point}
		if command.Kind == gfx.PathCommandQuad || command.Kind == gfx.PathCommandCubic {
			points = append(points, command.Control1)
		}
		if command.Kind == gfx.PathCommandCubic {
			points = append(points, command.Control2)
		}
		for _, point := range points {
			if first {
				minX, minY, maxX, maxY = point.X, point.Y, point.X, point.Y
				first = false
				continue
			}
			minX, minY = min(minX, point.X), min(minY, point.Y)
			maxX, maxY = max(maxX, point.X), max(maxY, point.Y)
		}
	}
	return minX, minY, maxX, maxY
}

func absFloat(value float32) float32 {
	if value < 0 {
		return -value
	}
	return value
}
