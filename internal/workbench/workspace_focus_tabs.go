package workbench

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	layoutcore "motty/internal/workspace"
	"strconv"
)

var hitWorkspaceFocusTab = tree.Sub(shellHits, tree.Name("pane-focus-tab"))

type workspaceFocusTabLabels struct {
	title, status, label, tooltip string
}

type workspaceFocusTabs struct {
	ghostDraw func(*ui.DrawContext)
	drag      workspaceFocusDrag
	bounds    [layoutcore.MaxPanes]gfx.Rect
	scroll    *ui.ScrollState
	labels    [layoutcore.MaxPanes]workspaceFocusTabLabels
	workspace layoutcore.WorkspaceID
	pane      layoutcore.PaneID
	width     float32
}

func (state *State) declareWorkspaceFocusTabs(c ui.C, content gfx.Rect) {
	id := state.deck.ActiveWorkspace()
	if state.focusTabs.drag.pane != 0 && !state.workspaceFocusDragValid() {
		state.focusTabs.drag = workspaceFocusDrag{}
	}
	if !state.terminalPage() || !state.deck.PaneMaximized(id) {
		return
	}
	frame := state.workspaceFocusTabsBounds(id, state.viewport)
	active := state.deck.ActivePane(id)
	changed := state.focusTabs.workspace != id || state.focusTabs.pane != active || state.focusTabs.width != frame.Width
	state.focusTabs.workspace, state.focusTabs.pane, state.focusTabs.width = id, active, frame.Width
	c.Part("pane-focus-tabs")
	ui.Stack(c).Absolute().Left(frame.X).Top(frame.Y).Size(frame.Width, frame.Height).Background(state.pal.workspace).Children(func() {
		ui.Slot(c).Absolute().Left(0).Top(0).Size(frame.Width, 1).Background(state.pal.line)
		area := ui.Scroll(c).Fill().Sideways().Scrollbars()
		state.focusTabs.scroll = area.ScrollState()
		state.scrollWorkspaceFocusDrag()
		if changed || area.ScrollState().ContentWidth == 0 {
			for index, pane := range state.panes {
				if pane.ID != active {
					continue
				}
				left, right := float32(index)*workspacePaneTabWidth, float32(index+1)*workspacePaneTabWidth
				scroll := area.ScrollState()
				if left < scroll.OffsetX {
					scroll.ScrollToX(left)
				}
				if right > scroll.OffsetX+frame.Width {
					scroll.ScrollToX(right - frame.Width)
				}
			}
		}
		area.Children(func() {
			ui.Row(c).Width(float32(len(state.panes)) * workspacePaneTabWidth).Height(workspacePaneTabsHeight).Children(func() {
				for index, pane := range state.panes {
					state.declareWorkspaceFocusTab(c, index, pane)
				}
			})
		})
	})
}

func (state *State) declareWorkspaceFocusTab(c ui.C, index int, pane Pane) {
	title, status := state.messages.LabelNewTab, ""
	if session, ok := state.sessionByIDPointer(pane.SessionID); ok {
		title = session.Title
		status, _ = state.sessionStatus(session)
	}
	cached := &state.focusTabs.labels[index]
	if cached.label == "" || cached.title != title || cached.status != status {
		*cached = workspaceFocusTabLabels{title: title, status: status, label: strconv.Itoa(index+1) + " " + title, tooltip: title + "  " + status}
	}
	label := cached.label
	fill := state.pal.workspace
	if pane.Active || state.focusTabs.drag.active && state.focusTabs.drag.pane == pane.ID {
		fill = withAlpha(state.pal.accent, .12)
	}
	ui.Row(c).Width(workspacePaneTabWidth).Height(workspacePaneTabsHeight-4).MarginSides(2, 0, 2, 0).
		PadX(10).Gap(6).Items(ui.Center).Background(fill).Radius(4).
		Hover(ui.Look{}.Background(withAlpha(state.pal.accent, .08))).
		HitWith(hitWorkspaceFocusTab.At(int(pane.ID)), label, gfx.HitOptions{Role: gfx.RoleTab, Selected: pane.Active}).
		Tooltip(cached.tooltip).Draw(func(d *ui.DrawContext) {
		state.drawWorkspaceFocusTabDrag(d, index)
	}).Children(func() {
		ui.Text(c, strconv.Itoa(index+1)).FontSize(metrics.FontSizeS).Color(state.pal.muted).Shrink(0)
		if pane.Active {
			ui.Text(c, "●").FontSize(9).Color(state.pal.success).Shrink(0)
		}
		ui.Text(c, title).FontSize(metrics.FontSizeS).Color(state.pal.text).Grow(1).MinWidth(0).Ellipsis()
	})
}

func (state *State) selectWorkspaceFocusTab(pane layoutcore.PaneID, focusContent bool) {
	id := state.deck.ActiveWorkspace()
	previous := state.deck.ActivePane(id)
	if !state.deck.PaneMaximized(id) || !state.activatePane(id, pane) {
		return
	}
	if focusContent {
		state.workspaceHeaderFocusPending = state.activePaneContentFocus()
	}
	if previous != pane {
		state.save()
	}
}
