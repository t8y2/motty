package workbench

import (
	"strconv"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// A terminal pane is declared inside the workbench page: the workspace names a
// container per pane, and the pane view puts the bar or find bar above the
// terminal. The terminal is a ui.Control of that page, so its keys, pointer,
// wheel and text arrive through the page's control routing. The scrollbar is a
// part of the control; everything painted with an identity of its own (the find
// bar, the tool rail) keeps the window's dispatch.
//
// The pane containers are named by Part, not keyed: a Part name is the word the
// program gives it, so the identities the page makes can be rebuilt from the
// tree without a paint (the tests that drive a State without painting rely on
// that), while still being one stable name per session in the workbench page.
const terminalControlPart = "terminal"

// The pane's containers: the workspace part holds the visible panes, a pane
// root holds one session's pane, and the control lives under the pane and body
// parts. Parts name them, so a bar appearing or leaving does not move the
// control.
const (
	terminalWorkspacePart = "workspace"
	terminalPanePart      = "pane"
	terminalBodyPart      = "body"
)

// terminalPartScrollbar is the scrollbar's part of the terminal control, and
// terminalPartGhostHint the accept-suggestion badge beside an inline suggestion.
const (
	terminalPartScrollbar = iota
	terminalPartGhostHint
)

// terminalPageRoot names one session's pane container in the window's identity
// tree. The session number is unique in the window, so it keeps the panes apart.
func terminalPageRoot(sessionID uint32) string {
	return "terminal-" + strconv.FormatUint(uint64(sessionID), 10)
}

// terminalWorkspaceScope is where the shell page declares the panes.
func terminalWorkspaceScope() gfx.Scope {
	return Tree().Sub(Tree().Feature(shellPageRoot), Tree().Name(terminalWorkspacePart))
}

// terminalPageRootScope is the scope of a session's pane container.
func terminalPageRootScope(sessionID uint32) gfx.Scope {
	return Tree().Sub(terminalWorkspaceScope(), Tree().Name(terminalPageRoot(sessionID)))
}

// terminalPaneScope is the pane container the session's bar and body live in.
func terminalPaneScope(sessionID uint32) gfx.Scope {
	return Tree().Sub(terminalPageRootScope(sessionID), Tree().Name(terminalPanePart))
}

// terminalBodyScope is the scope the terminal control is named in: the pane's
// body column.
func terminalBodyScope(sessionID uint32) gfx.Scope {
	return Tree().Sub(terminalPaneScope(sessionID), Tree().Name(terminalBodyPart))
}

// terminalControlHit is the identity of a pane's terminal control, as the page
// declares it with Part("terminal") in its body. A host that has not painted the
// page can still name the control with it.
func terminalControlHit(sessionID uint32) gfx.ID {
	return terminalBodyScope(sessionID).ID(Tree().Name(terminalControlPart))
}

// terminalControlParts is the scope of the control's parts, the identities its
// Draw declares for its own regions.
func terminalControlParts(sessionID uint32) gfx.Scope {
	return Tree().Sub(terminalBodyScope(sessionID), Tree().Name(terminalControlPart))
}

// pageIdentity makes the control identity and part scope once: painting and
// event routing read them, and rebuilding the name would allocate. The name is
// a pure function of the page's parts, so it is the same before and after a
// paint.
func (t *terminal) pageIdentity(sessionID uint32) {
	if !t.controlID.IsZero() {
		return
	}
	t.controlID = terminalControlHit(sessionID)
	t.parts = terminalControlParts(sessionID)
}

// installTerminalHandlers gives a newly configured terminal its control input
// and its Draw function. The functions keep the session's identity rather than
// a pointer: the session slice moves as tabs open and close.
func (state *State) installTerminalHandlers(session *Session) {
	t := session.term
	if t == nil {
		return
	}
	sessionID := session.ID
	// A session can move to another window: its handlers and identity must be
	// the new window's, naming the session's new number.
	t.controlDraw = func(d *ui.DrawContext) { state.drawTerminalControl(d, sessionID) }
	t.controlID, t.parts = gfx.ID{}, 0
	t.pageIdentity(sessionID)
	h := &t.handlers
	h.Role = gfx.RoleTerminal
	// The terminal's input is the application's own routing: a key the
	// application acts on (a shortcut, the completion list, the key that
	// activates a pane) is handled before the terminal encodes it, and the
	// terminal's encoding is the last step. Declaring the window's handlers as
	// the control's keeps that order now that the page routes the control: the
	// page offers the event to this dispatcher, which reaches the terminal at the
	// same point the workbench always did.
	h.Key = state.Key
	h.Pointer = state.Pointer
	h.Scroll = state.Scroll
	h.Text = func(context app.TextInputContext) *app.TextInput {
		current, ok := state.sessionByIDPointer(sessionID)
		if !ok || current.term == nil {
			return nil
		}
		return state.terminalControlText(current, context)
	}
}

// terminalControlPointer is the terminal control's own pointer handling, after
// the window's pointer routing has made this pane the active one: a part of the
// control, a remote desktop, or the terminal grid.
func (state *State) terminalControlPointer(session *Session, event gfx.PointerEvent) gfx.EventResult {
	if session.term == nil {
		return gfx.EventResult{}
	}
	if part, ok := event.Target.In(session.term.parts); ok {
		if part == terminalPartScrollbar {
			return state.terminalScrollbarPointer(session, event)
		}
		if part == terminalPartGhostHint {
			return state.terminalGhostHintPointer(session, event)
		}
		return gfx.EventResult{}
	}
	if session.desk != nil {
		return state.desktopPointer(session, event)
	}
	return state.terminalPointer(session, event)
}

// terminalControlScroll is terminalControlPointer for the wheel.
func (state *State) terminalControlScroll(session *Session, event gfx.ScrollEvent) gfx.EventResult {
	if session.term == nil {
		return gfx.EventResult{}
	}
	if session.desk != nil {
		return state.desktopScroll(session, event)
	}
	return state.terminalScroll(session, event)
}

// terminalControlText is terminalControlPointer for the platform's text service.
func (state *State) terminalControlText(session *Session, context app.TextInputContext) *app.TextInput {
	if session.term == nil {
		return nil
	}
	if session.desk != nil {
		return state.desktopTextInput(session)
	}
	return state.terminalTextInputFor(session)
}

// terminalPageSession finds the session whose terminal control or control part
// an identity names.
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

// terminalSessionWithoutTerm is terminalPageSession for a session whose
// terminal was dropped: the identity is still known, and the text-input gap
// reports the session it belongs to. It builds the name, so it is for the rare
// gap path, not for routing.
func (state *State) terminalSessionWithoutTerm(target gfx.ID) *Session {
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

// terminalPageKey routes a key the terminal control owns to the terminal's own
// encoding. The window's handlers reach this point in their established order;
// the page's control routing calls the same application chain, so a key acts
// here once either way.
func (state *State) terminalPageKey(session *Session, event gfx.KeyEvent) (gfx.EventResult, bool) {
	if session.term == nil {
		return gfx.EventResult{}, false
	}
	return state.terminalKey(session, event), true
}

// terminalPagePointer is terminalPageKey for the pointer.
func (state *State) terminalPagePointer(session *Session, event gfx.PointerEvent) (gfx.EventResult, bool) {
	if session.term == nil {
		return gfx.EventResult{}, false
	}
	return state.terminalControlPointer(session, event), true
}

// terminalPageScroll is terminalPageKey for the wheel.
func (state *State) terminalPageScroll(session *Session, event gfx.ScrollEvent) gfx.EventResult {
	if session.term == nil {
		return gfx.EventResult{}
	}
	return state.terminalControlScroll(session, event)
}

// terminalPageText is terminalPageKey for the platform's text service.
func (state *State) terminalPageText(session *Session, context app.TextInputContext) *app.TextInput {
	if session.term == nil {
		return nil
	}
	return state.terminalControlText(session, context)
}

// declareTerminalPane is the pane view: the bar, the find bar and the terminal
// control, laid out in the pane's rectangle. bounds is the pane as the window
// paints it; the page is one window-sized tree now, so the view keeps window
// coordinates rather than a pane-local canvas.
func (state *State) declareTerminalPane(c ui.C, session *Session, bounds gfx.Rect) {
	t := session.term
	if t == nil {
		return
	}
	chrome, ok := state.terminalChromeForSession(session.ID)
	if !ok {
		chrome = terminalChrome{top: infoBarHeight}
	}
	geometry := terminalGeometryForArea(gfx.Rect{
		X:      bounds.X,
		Y:      bounds.Y + chrome.top,
		Width:  bounds.Width,
		Height: max(bounds.Height-chrome.top, 0),
	}, state.cellWidth, state.cellHeight, state.padding())
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
	t.paintCommandNoticeShown = state.workspaceCommandNoticeInWorkspace(session) && (inSplit || !finding)
	t.paintCommandNotice = ui.Box{}
	t.paintBarBackground = barBackground
	t.paintGeometry = geometry
	t.paintFind = findBarSlots{}
	t.paintSpinner, t.paintLight, t.paintNotice = ui.Box{}, ui.Box{}, ui.Box{}

	// Each notice reserves its own strip across a split, including panes that
	// do not own the notice, so changing focus does not resize the terminal.
	noticeReserved := state.terminalInputNoticeVisibleInWorkspace(session) && (inSplit || !finding)

	c.Part(terminalPanePart)
	ui.Column(c).Fill().Children(func() {
		if inSplit || finding {
			ui.Stack(c).Height(infoBarHeight).Shrink(0).Background(barBackground).Children(func() {
				if finding {
					t.paintFind = state.declareFindBar(c, session)
				} else {
					t.paintSpinner, t.paintLight = state.declarePaneBar(c, bounds.Width, title, status, session, !inactive)
				}
				ui.Row(c).FillWidth().Height(1).Self(ui.End).Background(barBorder)
			})
		}
		c.Part(terminalBodyPart)
		ui.Column(c).Grow(1).FillHeight().Children(func() {
			if t.paintCommandNoticeShown {
				t.paintCommandNotice = ui.Slot(c).Height(inputNoticeHeight).FillWidth().Shrink(0)
			}
			switch {
			case showingInputNotice:
				t.paintNotice = ui.Slot(c).Height(inputNoticeHeight).FillWidth().Shrink(0)
			case noticeReserved:
				ui.Slot(c).Height(inputNoticeHeight).FillWidth().Shrink(0).Background(state.ground(state.termPal.terminal))
			}
			c.Part(terminalControlPart)
			label := t.accessibleName(state.messages.LabelTerminalFmt, session.Address)
			terminal := ui.Control(c, label, &t.handlers)
			if session.ID == state.activeSessionID && !t.find.open {
				terminal.AutoFocus()
			}
			terminal.Grow(1).FillWidth().Background(state.ground(state.termPal.terminal)).Draw(t.controlDraw)
		})
	})
}

// drawTerminalControl paints the grid inside the control's rectangle.
func (state *State) drawTerminalControl(d *ui.DrawContext, sessionID uint32) {
	session, ok := state.sessionByIDPointer(sessionID)
	if !ok || session.term == nil {
		return
	}
	geometry := terminalGeometryForArea(d.Rect, state.cellWidth, state.cellHeight, state.padding())
	state.paintGridControl(d.Canvas, session, geometry, terminalGridFocused(d.Canvas, session, d.State.Focused))
}

// terminalGridFocused reports whether the keyboard is on the terminal control
// or on one of its parts. The scrollbar and the accept-suggestion badge are
// parts; pressing one focuses it, and the grid is still what the user is typing
// at, so the ghost suggestion and its badge keep showing.
func terminalGridFocused(canvas *gfx.Canvas, session *Session, control bool) bool {
	if control {
		return true
	}
	if session == nil || session.term == nil {
		return false
	}
	_, ok := canvas.Focused().In(session.term.parts)
	return ok
}
