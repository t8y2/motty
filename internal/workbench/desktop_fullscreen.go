package workbench

import (
	"fmt"
	"strings"

	"github.com/vivaui/viva/gfx"
)

const desktopFullscreenChord = "Ctrl+Alt+Enter"

// desktopBarDock is where the fullscreen bar sits: one of the picture's edges, or
// free, wherever it was last dropped. While free, free says where.
type desktopBarDock uint8

const (
	desktopBarDockTop desktopBarDock = iota
	desktopBarDockBottom
	desktopBarDockLeft
	desktopBarDockRight
	desktopBarDockFree
)

// String is the value the dock is written as in the state file.
func (dock desktopBarDock) String() string {
	switch dock {
	case desktopBarDockBottom:
		return "bottom"
	case desktopBarDockLeft:
		return "left"
	case desktopBarDockRight:
		return "right"
	case desktopBarDockFree:
		return "free"
	default:
		return "top"
	}
}

// desktopBarDockFromString reads a saved dock. An absent or unknown value is the
// top edge, where the bar started.
func desktopBarDockFromString(value string) desktopBarDock {
	switch strings.TrimSpace(value) {
	case "bottom":
		return desktopBarDockBottom
	case "left":
		return desktopBarDockLeft
	case "right":
		return desktopBarDockRight
	case "free":
		return desktopBarDockFree
	default:
		return desktopBarDockTop
	}
}

// desktopBarDrag is a bar being moved with the pointer. grab is the pointer's
// offset from the bar's top-left corner, and rect is where the bar is now, in
// page coordinates. It is not a saved position: dropping the bar turns rect into
// a dock edge or a free place. A press on the exit label is pending until the
// pointer moves, so a click there still exits fullscreen rather than starting a
// drag.
type desktopBarDrag struct {
	active  bool
	pending bool
	press   gfx.Point
	grab    gfx.Point
	rect    gfx.Rect
}

// desktopFullscreenBar is the floating bar a fullscreen remote desktop shows
// over its picture. Its pin keeps the bar visible; unpinned, the bar hides and
// comes back while the pointer reaches the edge it is docked to, or the place a
// free bar was left, as the platform remote desktop client's connection bar
// does. zone is the strip or place that reveals it, and bar is the bar's own
// rect, both laid out by the last paint; over is whether the pointer is in
// either. free is the bar's centre as a fraction of the picture when it is not
// docked to an edge. dragHit and labelHit are the bar's background and its exit
// label, the two places a drag can start.
type desktopFullscreenBar struct {
	autoHide bool
	over     bool
	zone     gfx.Rect
	bar      gfx.Rect
	dock     desktopBarDock
	free     gfx.Point
	drag     desktopBarDrag
	dragHit  gfx.ID
	labelHit gfx.ID
}

// fullscreenBarRevealSlack is how far around the bar a pointer still counts as
// on it, so the bar does not flicker while it is being aimed at.
const fullscreenBarRevealSlack = float32(6)

// desktopBarRevealDepth is how far from the docked edge a pointer still counts
// as reaching for the bar. Only this strip reveals an unpinned bar: the bar's
// own rect keeps it visible once shown, but moving over the picture near the
// edge is not enough to bring it up.
const desktopBarRevealDepth = float32(10)

// desktopBarEdgeMargin is the gap between the bar and the edge it docks to.
const desktopBarEdgeMargin = float32(8)

// desktopBarSnapDistance is how close to an edge a dragged bar must be let go to
// snap flush to it; anywhere else it stays where it was dropped.
const desktopBarSnapDistance = float32(24)

// desktopBarDragDistance is how far a press on the exit label must move before
// it is a drag rather than a click.
const desktopBarDragDistance = float32(4)

// SetFullscreenControl connects the desktop action to Viva's native window.
func (state *State) SetFullscreenControl(set func(bool) error) {
	state.winChrome.setFullscreen = set
}

// SetWindowFullscreen follows native changes, including the system's exit action.
func (state *State) SetWindowFullscreen(fullscreen bool) {
	state.winChrome.fullscreen = fullscreen
	if session, ok := state.sessionByIDPointer(state.winChrome.fullscreenSession); ok && session.desk != nil {
		session.desk.focusPending = true
	}
	if !fullscreen {
		state.desktopBar.over = false
		state.desktopBar.drag = desktopBarDrag{}
		state.desktopBar.dragHit = gfx.ID{}
		state.desktopBar.labelHit = gfx.ID{}
		for i := range state.sessions {
			if view := state.sessions[i].desk; view != nil {
				view.fullscreen = false
				view.dockMenu = false
			}
		}
		state.winChrome.fullscreenSession = 0
	} else if session, ok := state.sessionByIDPointer(state.winChrome.fullscreenSession); ok && session.desk != nil {
		session.desk.fullscreen = true
	}
	if state.options.Invalidate != nil {
		state.options.Invalidate()
	}
}

func (state *State) fullscreenDesktop() *Session {
	if !state.winChrome.fullscreen || state.modal != modalNone || state.activeSection != sectionTerminal {
		return nil
	}
	session, ok := state.sessionByIDPointer(state.winChrome.fullscreenSession)
	if !ok || session.ID != state.activeSessionID || session.desk == nil || session.desk.ended {
		return nil
	}
	return session
}

func (state *State) toggleDesktopFullscreen(session *Session) {
	chrome := &state.winChrome
	if chrome.setFullscreen == nil {
		state.showActionError(state.messages.Desktop.Fullscreen, state.messages.Desktop.FullscreenUnavailable)
		return
	}
	enter := !chrome.fullscreen && chrome.fullscreenSession == 0
	if enter {
		if session == nil || session.desk == nil || session.desk.ended {
			return
		}
		chrome.fullscreenSession = session.ID
		// An unpinned bar starts hidden: the pointer is on the control that
		// entered fullscreen, not at the top edge the bar answers to.
		state.desktopBar.over = false
	}
	if selected, ok := state.sessionByIDPointer(chrome.fullscreenSession); ok && selected.desk != nil {
		if !enter {
			// The menu and a drag belong to the fullscreen bar, so they go with it.
			selected.desk.dockMenu = false
			state.desktopBar.drag = desktopBarDrag{}
		}
		selected.desk.releaseInput()
	}
	if err := chrome.setFullscreen(enter); err != nil {
		if enter {
			chrome.fullscreenSession = 0
		}
		state.showActionFailure(state.messages.Desktop.Fullscreen, err, fmt.Sprintf(state.messages.Desktop.FullscreenFailedFmt, err))
	}
}

func (state *State) desktopFullscreenKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if event.Key != gfx.KeyEnter || event.Modifiers != gfx.ModifierControl|gfx.ModifierAlt {
		return gfx.EventResult{}, false
	}
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if state.winChrome.fullscreenSession == 0 &&
		(!ok || session.desk == nil || state.activeSection != sectionTerminal || state.modal != modalNone) {
		return gfx.EventResult{}, false
	}
	if !event.Release && !event.Repeat {
		state.toggleDesktopFullscreen(session)
	}
	return gfx.EventResult{Handled: true, Repaint: true}, true
}

func (state *State) syncDesktopFullscreen() {
	if state.winChrome.fullscreen && state.winChrome.fullscreenSession != 0 && state.fullscreenDesktop() == nil {
		state.toggleDesktopFullscreen(nil)
	}
}

// setDesktopFullscreenBarAutoHide turns the fullscreen bar's pin off or on: off
// keeps the bar always visible, on hides it until the pointer reaches the top
// edge. The choice is this computer's and is written to the state file.
func (state *State) setDesktopFullscreenBarAutoHide(autoHide bool) bool {
	bar := &state.desktopBar
	if bar.autoHide == autoHide {
		return false
	}
	bar.autoHide = autoHide
	if autoHide {
		// The pin was just pressed, so the pointer is on the bar: keep it out
		// until the pointer leaves the reveal band.
		bar.over = true
	}
	state.save()
	return true
}

// setDesktopFullscreenBarDock moves the fullscreen bar to another edge of the
// picture. The choice is this computer's and is written to the state file.
func (state *State) setDesktopFullscreenBarDock(dock desktopBarDock) bool {
	bar := &state.desktopBar
	if bar.dock == dock {
		return false
	}
	bar.dock = dock
	// The choice was made from the bar's own menu, so the pointer is on the bar:
	// keep it visible until the pointer leaves the new edge or the bar.
	bar.over = true
	state.save()
	return true
}

// trackDesktopFullscreenBar follows the pointer for an unpinned fullscreen bar.
// The strip is the one the last paint laid out along the docked edge: moving
// into it shows the bar, and the bar's own rect keeps it shown while it is being
// used. Leaving both hides it again. A drag that captured the pointer keeps the
// bar as it was, so panning the picture cannot make it appear, and an open dock
// menu keeps it visible.
func (state *State) trackDesktopFullscreenBar(event gfx.PointerEvent) bool {
	bar := &state.desktopBar
	if !bar.autoHide || event.Captured || bar.drag.active {
		return false
	}
	session := state.fullscreenDesktop()
	if session == nil || session.desk.dockMenu {
		return false
	}
	over := bar.over
	switch event.Kind {
	case gfx.PointerDown, gfx.PointerMove:
		over = pointInRect(event.X, event.Y, bar.zone)
		if bar.over {
			over = over || pointInRect(event.X, event.Y, inflateRect(bar.bar, fullscreenBarRevealSlack))
		}
	case gfx.PointerExit:
		over = false
	}
	changed := over != bar.over
	bar.over = over
	return changed
}

// inflateRect grows r by by on every side.
func inflateRect(r gfx.Rect, by float32) gfx.Rect {
	return gfx.Rect{X: r.X - by, Y: r.Y - by, Width: r.Width + 2*by, Height: r.Height + 2*by}
}

// desktopBarPointer moves the fullscreen bar while it is dragged: a primary press
// on the bar's own surface takes the pointer at once, and one on its exit label
// takes it only after the pointer moves, so a click there still exits. The bar
// follows the pointer inside the picture, and letting go snaps it to a nearby
// edge or leaves it where it was.
func (state *State) desktopBarPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	bar := &state.desktopBar
	session := state.fullscreenDesktop()
	if session == nil {
		bar.drag = desktopBarDrag{}
		bar.dragHit, bar.labelHit = gfx.ID{}, gfx.ID{}
		return gfx.EventResult{}, false
	}
	area := session.desk.area
	switch event.Kind {
	case gfx.PointerDown:
		if event.Button != gfx.PointerButtonPrimary || bar.dragHit.IsZero() {
			return gfx.EventResult{}, false
		}
		onLabel := !bar.labelHit.IsZero() && event.Target == bar.labelHit
		if event.Target != bar.dragHit && !onLabel {
			return gfx.EventResult{}, false
		}
		bar.drag = desktopBarDrag{
			pending: true,
			press:   gfx.Point{X: event.X, Y: event.Y},
			grab:    gfx.Point{X: event.X - bar.bar.X, Y: event.Y - bar.bar.Y},
			rect:    bar.bar,
		}
		bar.over = true
		if onLabel {
			// A click on the label exits fullscreen: the drag starts only when
			// the pointer has moved.
			return gfx.EventResult{}, false
		}
		bar.drag.active = true
		return gfx.EventResult{Handled: true, Repaint: true}, true
	case gfx.PointerMove:
		if bar.drag.active {
			state.moveDesktopFullscreenBar(event, area)
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
		if !bar.drag.pending {
			return gfx.EventResult{}, false
		}
		dx, dy := event.X-bar.drag.press.X, event.Y-bar.drag.press.Y
		if dx*dx+dy*dy < desktopBarDragDistance*desktopBarDragDistance {
			return gfx.EventResult{}, false
		}
		bar.drag.active, bar.drag.pending = true, false
		state.moveDesktopFullscreenBar(event, area)
		return gfx.EventResult{Handled: true, Repaint: true}, true
	case gfx.PointerUp:
		if bar.drag.active {
			if event.Button != gfx.PointerButtonPrimary {
				return gfx.EventResult{}, false
			}
			state.dropDesktopFullscreenBar(area)
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
		if bar.drag.pending {
			// A press on the label that never moved: its click goes ahead.
			bar.drag = desktopBarDrag{}
		}
		return gfx.EventResult{}, false
	case gfx.PointerExit:
		if bar.drag.active {
			// Letting the pointer leave the window finishes the drag where it is,
			// so a release another window takes cannot leave the bar following the
			// pointer the next time it comes back.
			state.dropDesktopFullscreenBar(area)
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
		bar.drag = desktopBarDrag{}
		return gfx.EventResult{}, false
	}
	return gfx.EventResult{}, false
}

// moveDesktopFullscreenBar follows the pointer with the bar, kept inside area.
func (state *State) moveDesktopFullscreenBar(event gfx.PointerEvent, area gfx.Rect) {
	bar := &state.desktopBar
	width, height := bar.bar.Width, bar.bar.Height
	x := event.X - bar.drag.grab.X
	y := event.Y - bar.drag.grab.Y
	x = min(max(x, area.X), max(area.X+area.Width-width, area.X))
	y = min(max(y, area.Y), max(area.Y+area.Height-height, area.Y))
	bar.drag.rect = gfx.Rect{X: x, Y: y, Width: width, Height: height}
	bar.over = true
}

// dropDesktopFullscreenBar ends a drag: an edge within reach snaps the bar to it,
// anything else leaves it where it was let go, remembered as a fraction of the
// picture so another screen size keeps it in about the same place.
func (state *State) dropDesktopFullscreenBar(area gfx.Rect) {
	bar := &state.desktopBar
	rect := bar.drag.rect
	bar.drag = desktopBarDrag{}
	bar.over = true
	best := desktopBarDockTop
	bestDistance := rect.Y - area.Y
	for _, candidate := range [...]struct {
		dock     desktopBarDock
		distance float32
	}{
		{desktopBarDockBottom, area.Y + area.Height - (rect.Y + rect.Height)},
		{desktopBarDockLeft, rect.X - area.X},
		{desktopBarDockRight, area.X + area.Width - (rect.X + rect.Width)},
	} {
		if candidate.distance < bestDistance {
			best, bestDistance = candidate.dock, candidate.distance
		}
	}
	if bestDistance <= desktopBarSnapDistance {
		bar.dock = best
	} else {
		bar.dock = desktopBarDockFree
		if area.Width > 0 && area.Height > 0 {
			bar.free = gfx.Point{
				X: (rect.X + rect.Width/2 - area.X) / area.Width,
				Y: (rect.Y + rect.Height/2 - area.Y) / area.Height,
			}
		}
	}
	state.save()
}
