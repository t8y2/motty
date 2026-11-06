package workbench

import (
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
)

var hitNotePreviewThumb = tree.ID(notesHits, "preview-thumb")

// Each area owns its activity clock; scrolling a picker must not light the tree.
type noteScrollbar struct {
	offset   float32
	since    time.Duration
	lit      bool
	activity bool
}

func (bar *noteScrollbar) fade(c *ui.Canvas, id ui.ID, offset float32, active, scrollable bool) float32 {
	if !scrollable {
		*bar = noteScrollbar{offset: offset}
		return 0
	}
	now := c.Now()
	if active || bar.activity || offset != bar.offset {
		bar.offset, bar.since, bar.lit = offset, now, true
	}
	bar.activity = false
	target := float32(0)
	if bar.lit {
		if remaining := terminalBarLinger - (now - bar.since); active || remaining > 0 {
			target = 1
			if !active {
				c.RequestFrameIn(remaining)
			}
		} else {
			bar.lit = false
		}
	}
	return c.Animate(id, 0, target, ui.DefaultMotion())
}

func (v *noteViewState) resetPopupScrollbar() {
	v.popupBar = noteScrollbar{}
	v.tagPopupDragging = false
}

func (s *State) noteScrollHover(bounds ui.Rect) bool {
	v := &s.noteContent().view
	return v.pointerValid && pointInRect(v.pointer.X, v.pointer.Y, bounds)
}

func (s *State) noteScrollRegions() uint8 {
	v := &s.noteContent().view
	if !s.noteLibraryView() || !s.notesVisible() || s.noteContent().confirm != "" || v.menu != "" {
		return 0
	}
	if v.popup != "" {
		if s.noteScrollHover(v.tagPopupArea.Geometry().Viewport) {
			return 4
		}
		return 0
	}
	var regions uint8
	if s.noteScrollHover(v.listArea.Geometry().Viewport) {
		regions |= 1
	}
	if !v.drawer && s.noteScrollHover(s.noteContent().previewBounds) {
		regions |= 2
	}
	return regions
}

// The local theme belongs only to the scroll area's bars, not its contents.
func (s *State) noteHoverScrollArea(c *ui.Canvas, bounds ui.Rect, content ui.Size, offset float32, viewport, thumb ui.ID, label string, bar *noteScrollbar, dragging, hovered bool) components.ScrollArea {
	theme := s.Theme()
	area := components.BeginScrollArea(nil, theme, bounds, content).Offset(0, offset).HoverScrollbars(true)
	geometry := area.Geometry()
	fade := bar.fade(c, thumb, geometry.OffsetY, dragging || hovered || c.Pressed() == thumb, geometry.MaxOffsetY > 0)
	theme.Border.Alpha *= fade
	theme.MutedForeground.Alpha *= fade
	return components.BeginScrollArea(c, theme, bounds, content).Offset(0, offset).
		Viewport(viewport, label).VerticalScrollbar(thumb, label, "").HoverScrollbars(fade > 0)
}

func (s *State) notePreviewScrollbarPointer(e ui.PointerEvent) ui.EventResult {
	v := &s.noteContent().view
	geo := v.previewArea.Geometry()
	if e.Kind == ui.PointerDown && e.Target == hitNotePreviewThumb && (e.Button == ui.PointerButtonPrimary || e.Button == ui.PointerButtonNone) && geo.MaxOffsetY > 0 {
		v.previewDragging = true
		v.previewGrab = geo.VerticalThumb.Height / 2
		if e.Y >= geo.VerticalThumb.Y && e.Y < geo.VerticalThumb.Y+geo.VerticalThumb.Height {
			v.previewGrab = e.Y - geo.VerticalThumb.Y
		}
	}
	if !v.previewDragging {
		return ui.EventResult{}
	}
	switch e.Kind {
	case ui.PointerMove, ui.PointerDown, ui.PointerUp:
		s.noteScrollDriver(true)
		s.noteContent().previewScroll = geo.VerticalOffsetForThumb(e.Y - v.previewGrab)
		if e.Kind == ui.PointerUp {
			v.previewDragging = false
		}
	default:
		v.previewDragging = false
	}
	return ui.EventResult{Handled: true, Repaint: true}
}
