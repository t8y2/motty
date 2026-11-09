package workbench

import (
	"strconv"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// A terminal pane is one ui.Page drawn into the pane's rectangle: the pane bar
// or find bar above, and the terminal below. The terminal is a ui.Control, so
// its keys, pointer, wheel and text arrive through the page's control routing
// rather than through the window's central hit dispatcher. The scrollbar is a
// part of the control; everything painted with an identity of its own (the
// find bar, the tool rail) keeps the window's dispatch.
const terminalControlPart = "terminal"

// terminalPartScrollbar is the scrollbar's part of the terminal control.
const (
	terminalPartScrollbar = iota
)

// terminalPageRoot names one session's page in the window's identity tree. Each
// pane has its own page, so the root is what keeps their controls apart.
func terminalPageRoot(sessionID uint32) string {
	return "terminal-" + strconv.FormatUint(uint64(sessionID), 10)
}

// terminalPageRootScope is where a session's page names its elements.
func terminalPageRootScope(sessionID uint32) gfx.Scope {
	return Tree().Feature(terminalPageRoot(sessionID))
}

// terminalControlHit is the local identity of a pane's terminal control, as the
// page declares it with Part("terminal") at its root. A host that has not
// painted the page can still name the control with it.
func terminalControlHit(sessionID uint32) gfx.ID {
	return terminalPageRootScope(sessionID).ID(Tree().Name(terminalControlPart))
}

// terminalControlParts is the scope of the control's parts, the identities its
// Draw declares for its own regions.
func terminalControlParts(sessionID uint32) gfx.Scope {
	return Tree().Sub(terminalPageRootScope(sessionID), Tree().Name(terminalControlPart))
}

// pageIdentity makes the page's control identity and part scope once: painting
// and event routing read them, and rebuilding the root's name would allocate.
func (t *terminal) pageIdentity(sessionID uint32) {
	if !t.controlID.IsZero() {
		return
	}
	root := terminalPageRootScope(sessionID)
	name := Tree().Name(terminalControlPart)
	t.controlID = root.ID(name)
	t.parts = Tree().Sub(root, name)
}

// terminalPageFor returns the session's page, making it and its cached
// declaration functions on first use. A session without a terminal has no page.
func (state *State) terminalPageFor(session *Session) *ui.Page {
	t := session.term
	if t == nil {
		return nil
	}
	t.pageIdentity(session.ID)
	if t.page == nil {
		sessionID := session.ID
		t.page = ui.NewPage(ui.PageConfig{Root: terminalPageRoot(sessionID), Theme: &state.theme})
		t.pageView = func(c ui.C) { state.declareTerminalPane(c, sessionID) }
		t.controlDraw = func(d *ui.DrawContext) { state.drawTerminalControl(d, sessionID) }
	}
	return t.page
}

// installTerminalHandlers gives a newly configured terminal its control input.
// The functions keep the session's identity rather than a pointer: the session
// slice moves as tabs open and close.
func (state *State) installTerminalHandlers(session *Session) {
	t := session.term
	if t == nil {
		return
	}
	sessionID := session.ID
	// A session can move to another window: its page and handlers must be the
	// new window's, naming the session's new number.
	t.page, t.pageView, t.controlDraw = nil, nil, nil
	t.controlID, t.parts, t.painted = gfx.ID{}, 0, false
	t.pageIdentity(sessionID)
	h := &t.handlers
	h.Role = gfx.RoleTerminal
	h.Key = func(event gfx.KeyEvent) gfx.EventResult {
		current, ok := state.sessionByIDPointer(sessionID)
		if !ok || current.term == nil {
			return gfx.EventResult{}
		}
		return state.terminalKey(current, event)
	}
	h.Pointer = func(event gfx.PointerEvent) gfx.EventResult {
		current, ok := state.sessionByIDPointer(sessionID)
		if !ok || current.term == nil {
			return gfx.EventResult{}
		}
		if part, ok := event.Target.In(t.parts); ok {
			if part == terminalPartScrollbar {
				return state.terminalScrollbarPointer(current, event)
			}
			return gfx.EventResult{}
		}
		if current.desk != nil {
			return state.desktopPointer(current, event)
		}
		return state.terminalPointer(current, event)
	}
	h.Scroll = func(event gfx.ScrollEvent) gfx.EventResult {
		current, ok := state.sessionByIDPointer(sessionID)
		if !ok || current.term == nil {
			return gfx.EventResult{}
		}
		if current.desk != nil {
			return state.desktopScroll(current, event)
		}
		return state.terminalScroll(current, event)
	}
	h.Text = func(context app.TextInputContext) *app.TextInput {
		current, ok := state.sessionByIDPointer(sessionID)
		if !ok || current.term == nil {
			return nil
		}
		if current.desk != nil {
			return state.desktopTextInput(current)
		}
		return state.terminalTextInputFor(current)
	}
}

// terminalPageSession finds the session whose pane page owns a local identity:
// the terminal control or one of its parts.
func (state *State) terminalPageSession(target gfx.ID) *Session {
	if target.IsZero() {
		return nil
	}
	for index := range state.sessions {
		session := &state.sessions[index]
		if session.term == nil {
			continue
		}
		session.term.pageIdentity(session.ID)
		if target == session.term.controlID {
			return session
		}
		if _, ok := target.In(session.term.parts); ok {
			return session
		}
	}
	return nil
}

// terminalPageSessionWithoutTerm is terminalPageSession for a session whose
// terminal was dropped: the page identity is still known, and the text-input
// gap reports the session it belongs to. It builds the name, so it is for the
// rare gap path, not for routing.
func (state *State) terminalPageSessionWithoutTerm(target gfx.ID) *Session {
	if target.IsZero() {
		return nil
	}
	for index := range state.sessions {
		session := &state.sessions[index]
		if session.term != nil {
			continue
		}
		if target == terminalControlHit(session.ID) {
			return session
		}
		if _, ok := target.In(terminalControlParts(session.ID)); ok {
			return session
		}
	}
	return nil
}

// panePageKey routes a key the page owns through the page, or straight to the
// control's handlers when the page has not been painted yet (a unit test that
// drives the session without a frame).
func (state *State) panePageKey(session *Session, event gfx.KeyEvent) (gfx.EventResult, bool) {
	t := session.term
	if t == nil {
		return gfx.EventResult{}, false
	}
	if t.painted {
		if page := state.terminalPageFor(session); page != nil {
			if result, ok := page.Key(event); ok {
				return result, true
			}
		}
	}
	if t.handlers.Key != nil {
		return t.handlers.Key(event), true
	}
	return gfx.EventResult{}, false
}

// panePagePointer is panePageKey for the pointer.
func (state *State) panePagePointer(session *Session, event gfx.PointerEvent) (gfx.EventResult, bool) {
	t := session.term
	if t == nil {
		return gfx.EventResult{}, false
	}
	if t.painted {
		if page := state.terminalPageFor(session); page != nil {
			if result, ok := page.Pointer(event); ok {
				return result, true
			}
		}
	}
	if t.handlers.Pointer != nil {
		return t.handlers.Pointer(event), true
	}
	return gfx.EventResult{}, false
}

// panePageScroll is panePageKey for the wheel.
func (state *State) panePageScroll(session *Session, event gfx.ScrollEvent) gfx.EventResult {
	t := session.term
	if t == nil {
		return gfx.EventResult{}
	}
	if t.painted {
		if page := state.terminalPageFor(session); page != nil {
			return page.Wheel(event)
		}
	}
	if t.handlers.Scroll != nil {
		return t.handlers.Scroll(event)
	}
	return gfx.EventResult{}
}

// declareTerminalPane is the page's view: the bar, the find bar and the
// terminal control, laid out in the pane's rectangle.
func (state *State) declareTerminalPane(c ui.C, sessionID uint32) {
	session, ok := state.sessionByIDPointer(sessionID)
	if !ok || session.term == nil {
		return
	}
	t := session.term
	f := c.Frame()
	bounds := f.Bounds()
	chrome, ok := state.terminalChromeForSession(session.ID)
	if !ok {
		chrome = terminalChrome{top: infoBarHeight}
	}
	geometry := terminalLayoutWithChrome(gfx.Size{Width: bounds.Width, Height: bounds.Height}, state.cellWidth, state.cellHeight, chrome, state.padding())
	inactive := state.paneIsInactive(session.ID)
	inSplit := state.paneInSplit(session.ID)
	barBackground, barBorder := state.pal.workspace, state.pal.line
	if state.workspaceManaged(state.deck.ActiveWorkspace()) && !inactive {
		barBackground = barBackground.Mix(state.pal.accent, .045)
		barBorder = state.activePaneRimColor()
	}
	finding := !inactive && session.term.find.open
	showingInputNotice := !inactive && !finding && state.terminalInputNoticeVisible(session)
	status, _ := state.sessionStatus(session)
	if inactive {
		state.clearTerminalRail(session)
	}
	title := session.Title
	if title == "" {
		title = session.Address
	}

	t.paintFinding, t.paintInSplit, t.paintInactive = finding, inSplit, inactive
	t.paintNoticeShown = showingInputNotice
	t.paintBarBackground = barBackground
	t.paintGeometry = geometry
	t.paintFind = findBarSlots{}
	t.paintSpinner, t.paintLight, t.paintNotice = ui.Box{}, ui.Box{}, ui.Box{}

	// The notice strip is part of the top chrome whenever the layout reserved
	// it, so the control starts below it even on a pane that does not paint it.
	noticeReserve := chrome.top
	if inSplit || finding {
		noticeReserve -= infoBarHeight
	}
	noticeReserved := noticeReserve > 0

	f.Column().Fill().PadSides(windowBarHeight, 0, 0, 0).Children(func() {
		if inSplit || finding {
			f.Stack().Height(infoBarHeight).Shrink(0).Background(barBackground).Children(func() {
				if finding {
					t.paintFind = state.declareFindBar(f, session)
				} else {
					t.paintSpinner, t.paintLight = state.declarePaneBar(f, bounds.Width, title, status, session, !inactive)
				}
				f.Row().FillWidth().Height(1).Self(ui.End).Background(barBorder)
			})
		}
		f.Column().Grow(1).FillHeight().Children(func() {
			switch {
			case showingInputNotice:
				t.paintNotice = f.Slot().Height(inputNoticeHeight).FillWidth().Shrink(0)
			case noticeReserved:
				f.Slot().Height(inputNoticeHeight).FillWidth().Shrink(0).Background(state.ground(state.termPal.terminal))
			}
			c.Part(terminalControlPart)
			label := t.accessibleName(state.messages.LabelTerminalFmt, session.Address)
			terminal := ui.Control(c, label, &t.handlers)
			if session.ID == state.activeSessionID {
				terminal.AutoFocus()
			}
			terminal.Grow(1).FillWidth().Background(state.ground(state.termPal.terminal)).Draw(t.controlDraw)
		})
	})
	t.painted = true
}

// drawTerminalControl paints the grid inside the control's rectangle.
func (state *State) drawTerminalControl(d *ui.DrawContext, sessionID uint32) {
	session, ok := state.sessionByIDPointer(sessionID)
	if !ok || session.term == nil {
		return
	}
	geometry := terminalGeometryForArea(d.Rect, state.cellWidth, state.cellHeight, state.padding())
	state.paintGridControl(d.Canvas, session, geometry, d.State.Focused)
}
