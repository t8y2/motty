package workbench

import (
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
)

// An icon-only control has no label until its tooltip appears, so resting on it
// plays the same gesture its glyph plays when chosen as a sidebar row. The
// gesture comes from iconGlyphs, not from a second table here: a Settings gear
// turns the same way in the sidebar, the dock and any compact settings control,
// and Terminal moves its prompt and cursor the same way everywhere.
//
// The trigger changes, not the drawing. Click-driven rows start on choice;
// hover-driven controls wait for a short dwell so crossing a toolbar does not
// set every glyph in motion. Once a hover gesture starts it finishes even if the
// pointer leaves, and it replays only after the pointer has gone and returned.

const (
	iconHoverDwell = 130 * time.Millisecond
	iconHoverSlot  = 1
)

// iconHover returns the transform for one icon-only control on this frame.
func (state *State) iconHover(canvas *ui.Canvas, id ui.ID, icon uint8, disabled bool) ui.IconTransform {
	if disabled {
		return ui.IconTransform{}
	}
	group, ok := iconGlyphFor(icon)
	if !ok {
		return ui.IconTransform{}
	}
	motion, ok := iconGlyphs[group]
	if !ok {
		return ui.IconTransform{}
	}
	return state.iconHoverMotion(canvas, id, group, motion)
}

// iconHoverIcon is the single entry point for a control that already holds a
// drawing, as Viva's Button and raw DrawIcon do. A kind and a drawing therefore
// reach the same gesture without a second table at the call site.
func (state *State) iconHoverIcon(canvas *ui.Canvas, id ui.ID, icon ui.Icon, disabled bool) ui.IconTransform {
	if disabled {
		return ui.IconTransform{}
	}
	motion, ok := iconMotionForDrawing(icon)
	if !ok {
		return ui.IconTransform{}
	}
	group, ok := iconGlyphForDrawing(icon)
	if !ok {
		return ui.IconTransform{}
	}
	return state.iconHoverMotion(canvas, id, group, motion)
}

// iconHoverMotion advances the gesture for one control and one glyph group.
// Switching a control to a different drawing, as Eye and EyeOff do, starts a
// new gesture instead of continuing the previous drawing's timeline.
func (state *State) iconHoverMotion(canvas *ui.Canvas, id ui.ID, group iconGlyphGroup, motion iconMotion) ui.IconTransform {
	if state.iconHovers == nil {
		state.iconHovers = make(map[ui.ID]iconHoverState, 8)
	}
	hover := state.iconHovers[id]
	if hover.group != group {
		hover = iconHoverState{group: group}
	}
	progress, playing := hover.hover.Progress(canvas, id, iconHoverSlot+uint8(group), iconHoverDwell, motion.span())
	state.iconHovers[id] = hover
	if !playing {
		return ui.IconTransform{}
	}
	return motion.at(progress)
}

// iconHoverState is the gesture of the drawing currently painted by one
// control. The group is kept beside the component state so a control that
// changes drawing cannot continue the old timeline.
type iconHoverState struct {
	group iconGlyphGroup
	hover components.IconHover
}
