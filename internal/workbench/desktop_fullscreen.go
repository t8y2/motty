package workbench

import (
	"fmt"
	"strings"
	"time"

	"github.com/vivaui/viva/gfx"
)

const desktopFullscreenChord = "Ctrl+Alt+Enter"

// desktopBarDock is where the fullscreen bar sits: one of the picture's edges, or
// free, wherever it was last dropped. The spot along that edge, or the free
// place, is desktopFullscreenBar.at.
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
// over its picture. Its pin keeps the bar visible; unpinned, the bar slides
// under the edge it is docked to, or fades into a marker where a free bar was
// left, and comes back out only while the pointer is on that marker, so no
// other part of the picture hides something that then takes its clicks. bar is
// the bar's own rect when it is out, marker the tab it leaves, and zone that
// tab with the bar, all laid out by the last paint; over is whether the pointer
// is on the marker or the bar. at is the bar's centre as a fraction of the
// picture: along the top or bottom edge it is docked to its X says where,
// along the left or right edge its Y, and a free bar takes both. dragHit and
// labelHit are the bar's background and its exit label, the two places a drag
// can start.
type desktopFullscreenBar struct {
	autoHide bool
	over     bool
	zone     gfx.Rect
	bar      gfx.Rect
	marker   gfx.Rect
	dock     desktopBarDock
	at       gfx.Point
	drag     desktopBarDrag
	dragHit  gfx.ID
	labelHit gfx.ID
}

// fullscreenBarRevealSlack is how far around the marker, and around the bar
// once it is out, a pointer still counts as on it, so the bar does not flicker
// while it is being aimed at.
const fullscreenBarRevealSlack = float32(6)

// desktopBarMarkerLength and desktopBarMarkerDepth are the tab a hidden bar
// leaves where it sits: its length along the edge, or across a free bar's
// middle, and how far it reaches from the edge. It holds the connection light.
const (
	desktopBarMarkerLength = float32(28)
	desktopBarMarkerDepth  = float32(12)
)

// desktopBarEdgeMargin is the gap between the bar and the edge it docks to.
const desktopBarEdgeMargin = float32(8)

// desktopBarSnapDistance is how close to an edge a dragged bar must be let go to
// snap flush to it; anywhere else it stays where it was dropped.
const desktopBarSnapDistance = float32(24)

// desktopBarDragDistance is how far a press on the exit label must move before
// it is a drag rather than a click.
const desktopBarDragDistance = float32(4)

// desktopBarFreeSlide is how far a free bar lifts as it gives way to its
// marker: an edge-docked bar slides out under its edge, a free one has no edge
// to go under and fades instead.
const desktopBarFreeSlide = float32(6)

// desktopBarSlideMotion is the slide of an unpinned bar out of its edge and
// back in, as the pointer reaches its marker and leaves it.
var desktopBarSlideMotion = gfx.Motion{Duration: 160 * time.Millisecond, Easing: gfx.EaseOut}

// desktopBarRevealSlot is the animated slot of the bar's drag surface: how far
// out of its marker the bar is.
const desktopBarRevealSlot = 0

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
		// An unpinned bar starts behind its marker: the pointer is on the
		// control that entered fullscreen, not on the marker.
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
// keeps the bar always visible, on hides it behind its marker until the pointer
// reaches that. The choice is this computer's and is written to the state file.
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
// picture, to the point of that edge nearest where it was, so the spot chosen
// along an edge survives a change between the top and bottom, or the left and
// right. The choice is this computer's and is written to the state file.
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
// The zone is the one the last paint laid out around the bar's marker: moving
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
	}
	// Either way the bar keeps the spot it was let go at: along the edge it
	// snapped to, or where it is, remembered as a fraction of the picture so
	// another screen size keeps it in about the same place.
	if area.Width > 0 && area.Height > 0 {
		bar.at = gfx.Point{
			X: min(max((rect.X+rect.Width/2-area.X)/area.Width, 0), 1),
			Y: min(max((rect.Y+rect.Height/2-area.Y)/area.Height, 0), 1),
		}
	}
	state.save()
}

// rect is where the bar of this size sits when it is out: against the edge it
// is docked to, centred at its spot along that edge, or centred on a free
// bar's place; either way kept inside area. The spot is the bar's centre as a
// fraction of the picture; one never chosen, or saved before the bar could be
// placed along an edge, reads as the middle of the edge.
func (bar *desktopFullscreenBar) rect(area gfx.Rect, width, height float32) gfx.Rect {
	centre := gfx.Point{X: area.X + min(max(bar.at.X, 0), 1)*area.Width, Y: area.Y + min(max(bar.at.Y, 0), 1)*area.Height}
	switch bar.dock {
	case desktopBarDockTop, desktopBarDockBottom:
		centre.X = area.X + placedFraction(bar.at.X)*area.Width
	case desktopBarDockLeft, desktopBarDockRight:
		centre.Y = area.Y + placedFraction(bar.at.Y)*area.Height
	}
	x := min(max(centre.X-width/2, area.X), max(area.X+area.Width-width, area.X))
	y := min(max(centre.Y-height/2, area.Y), max(area.Y+area.Height-height, area.Y))
	x = min(max(x, area.X+desktopBarEdgeMargin), max(area.X+area.Width-width-desktopBarEdgeMargin, area.X+desktopBarEdgeMargin))
	y = min(max(y, area.Y+desktopBarEdgeMargin), max(area.Y+area.Height-height-desktopBarEdgeMargin, area.Y+desktopBarEdgeMargin))
	switch bar.dock {
	case desktopBarDockBottom:
		y = area.Y + area.Height - height - desktopBarEdgeMargin
	case desktopBarDockLeft:
		x = area.X + desktopBarEdgeMargin
	case desktopBarDockRight:
		x = area.X + area.Width - width - desktopBarEdgeMargin
	case desktopBarDockTop:
		y = area.Y + desktopBarEdgeMargin
	}
	return gfx.Rect{X: x, Y: y, Width: width, Height: height}
}

// placedFraction is a saved spot along an edge: a bar is never let go with its
// centre on the edge itself, so zero can only be a file that predates placing.
func placedFraction(v float32) float32 {
	if v <= 0 || v > 1 {
		return 0.5
	}
	return v
}

// markerRect is the tab the bar leaves when hidden: at its spot on the edge it
// is docked to, or across the middle of a free bar.
func (bar *desktopFullscreenBar) markerRect(rest gfx.Rect, area gfx.Rect) gfx.Rect {
	length, depth := desktopBarMarkerLength, desktopBarMarkerDepth
	centre := gfx.Point{X: rest.X + rest.Width/2, Y: rest.Y + rest.Height/2}
	switch bar.dock {
	case desktopBarDockLeft, desktopBarDockRight:
		y := min(max(centre.Y-length/2, area.Y), max(area.Y+area.Height-length, area.Y))
		if bar.dock == desktopBarDockLeft {
			return gfx.Rect{X: area.X, Y: y, Width: depth, Height: length}
		}
		return gfx.Rect{X: area.X + area.Width - depth, Y: y, Width: depth, Height: length}
	}
	x := min(max(centre.X-length/2, area.X), max(area.X+area.Width-length, area.X))
	switch bar.dock {
	case desktopBarDockBottom:
		return gfx.Rect{X: x, Y: area.Y + area.Height - depth, Width: length, Height: depth}
	case desktopBarDockFree:
		return gfx.Rect{X: x, Y: centre.Y - depth/2, Width: length, Height: depth}
	default:
		return gfx.Rect{X: x, Y: area.Y, Width: length, Height: depth}
	}
}

// slide is the vector from the bar's rest to where it hides: under its edge,
// or lifted off a free bar's marker.
func (bar *desktopFullscreenBar) slide(rest gfx.Rect, area gfx.Rect) gfx.Point {
	switch bar.dock {
	case desktopBarDockBottom:
		return gfx.Point{Y: area.Y + area.Height - rest.Y}
	case desktopBarDockLeft:
		return gfx.Point{X: rest.X + rest.Width - area.X}
	case desktopBarDockRight:
		return gfx.Point{X: -(area.X + area.Width - rest.X)}
	case desktopBarDockFree:
		return gfx.Point{Y: -desktopBarFreeSlide}
	default:
		return gfx.Point{Y: -(rest.Y + rest.Height - area.Y)}
	}
}
