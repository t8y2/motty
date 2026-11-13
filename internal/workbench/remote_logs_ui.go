package workbench

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
	"motty/internal/workbench/icons"
	layoutcore "motty/internal/workspace"
)

var (
	remoteLogHits       = tree.Feature("remote-logs")
	hitServerLogs       = tree.ID(remoteLogHits, "toggle")
	hitRemoteLogDivider = tree.ID(remoteLogHits, "divider")
	hitRemoteLogDismiss = tree.ID(remoteLogHits, "dismiss")
	hitRemoteLogName    = tree.ID(remoteLogHits, "name")
	hitRemoteLogPath    = tree.ID(remoteLogHits, "path")
)

const remoteLogRowHeight = 20

var remoteLogExpandIcon = gfx.Icon{
	ViewBox: 24, Stroke: 2, Cap: gfx.CapRound, Join: gfx.JoinRound,
	Commands: []gfx.PathCommand{
		gfx.MoveTo(9, 4), gfx.LineTo(4, 4), gfx.LineTo(4, 9),
		gfx.MoveTo(15, 4), gfx.LineTo(20, 4), gfx.LineTo(20, 9),
		gfx.MoveTo(20, 15), gfx.LineTo(20, 20), gfx.LineTo(15, 20),
		gfx.MoveTo(9, 20), gfx.LineTo(4, 20), gfx.LineTo(4, 15),
	},
}

var remoteLogRestoreIcon = gfx.Icon{
	ViewBox: 24, Stroke: 2, Cap: gfx.CapRound, Join: gfx.JoinRound,
	Commands: []gfx.PathCommand{
		gfx.MoveTo(4, 9), gfx.LineTo(9, 9), gfx.LineTo(9, 4),
		gfx.MoveTo(15, 4), gfx.LineTo(15, 9), gfx.LineTo(20, 9),
		gfx.MoveTo(20, 15), gfx.LineTo(15, 15), gfx.LineTo(15, 20),
		gfx.MoveTo(9, 20), gfx.LineTo(9, 15), gfx.LineTo(4, 15),
	},
}

func (state *State) serverLogsRailTool(session *Session, active bool) (barTool, bool) {
	if session == nil || !session.remoteLogHost() || !state.toolbarVisible(toolbarTerminal, hitServerLogs) {
		return barTool{}, false
	}
	style := iconButtonQuiet
	if active && session.remoteLogs != nil {
		style = iconButtonActive
	}
	return barTool{id: hitServerLogs, icon: iconFileClock, hint: state.messages.RemoteLogs.Title,
		style: style, disabled: !active || (session.remoteLogs == nil && !session.canViewRemoteLogs())}, true
}

func (session *Session) remoteLogHost() bool {
	return session.HostID != 0 && session.host.isSSH() && !session.host.mosh() && !session.host.InteractiveOnly && session.host.System != sshconn.SystemWindows
}

func (state *State) clickRemoteLogs(id gfx.ID) bool {
	if id != hitServerLogs {
		return false
	}
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if ok && session.remoteLogs != nil {
		state.closeRemoteLogs(session)
	} else if ok && session.canViewRemoteLogs() {
		session.remoteLogs = &remoteLogView{follow: true, height: 360, pathSplit: ui.SplitterState{Pos: 280}}
		state.fitWorkspaceTerminals(state.deck.ActiveWorkspace())
	}
	return true
}

func (state *State) closeRemoteLogs(session *Session) {
	session.stopRemoteLogs()
	session.remoteLogs = nil
	state.fitWorkspaceTerminals(state.deck.ActiveWorkspace())
}

func (state *State) workspaceRemoteLogs(id layoutcore.WorkspaceID) *remoteLogView {
	pane, _ := state.deck.Pane(id, state.deck.ActivePane(id))
	if session, ok := state.sessionByIDPointer(uint32(pane.SessionID)); ok {
		return session.remoteLogs
	}
	return nil
}

func (view *remoteLogView) extent(height float32) float32 {
	if view.expanded {
		return max(0, height-72)
	}
	return min(max(view.height, 156), max(0, height-140))
}

func (state *State) remoteLogResize(event gfx.PointerEvent) gfx.EventResult {
	if state.modal != modalNone || !state.terminalPage() {
		return gfx.EventResult{}
	}
	id := state.deck.ActiveWorkspace()
	view := state.workspaceRemoteLogs(id)
	if view == nil {
		return gfx.EventResult{}
	}
	frame := state.workspaceBoundsAboveLogs(id, event.Viewport)
	if event.Kind == gfx.PointerDown && event.Target == hitRemoteLogDivider && event.Button == gfx.PointerButtonPrimary {
		view.pointerOffset = event.Y - (frame.Y + frame.Height - view.extent(frame.Height))
		view.resizing = true
	}
	if !view.resizing {
		return gfx.EventResult{}
	}
	switch event.Kind {
	case gfx.PointerMove, gfx.PointerUp:
		view.expanded = false
		view.height = min(max(frame.Y+frame.Height-event.Y+view.pointerOffset, 156), max(156, frame.Height-140))
		state.fitWorkspaceTerminals(id)
	case gfx.PointerExit:
		view.resizing = false
	}
	if event.Kind == gfx.PointerUp {
		view.resizing = false
	}
	return gfx.EventResult{Handled: true, Repaint: true}
}

func (state *State) declareRemoteLogs(c ui.C) {
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || session.remoteLogs == nil {
		return
	}
	view := session.remoteLogs
	frame := state.workspaceBoundsAboveLogs(state.deck.ActiveWorkspace(), state.viewport)
	height := view.extent(frame.Height)
	if height <= 0 || frame.Width <= 0 {
		return
	}
	m, p := state.messages.RemoteLogs, state.pal
	if view.buffer != nil {
		view.buffer.snapshot(&view.snapshot)
	}
	if view.scroll.Scroll == nil {
		view.scroll.Scroll = view.scrollLog
		view.scroll.Key = view.keyLog
		view.scroll.Pointer = view.logScrollbarPointer
	}
	c.Part("remote-logs")
	ui.Column(c).Absolute().Left(frame.X).Top(frame.Y+frame.Height-height).
		Size(frame.Width, height).Clip().Background(p.workspace).Children(func() {
		ui.Slot(c).Height(7).FillWidth().Background(p.surface).HitWith(hitRemoteLogDivider, m.Title, gfx.HitOptions{SkipFocus: true, Cursor: gfx.PointerCursorResizeVertical}).
			Draw(func(d *ui.DrawContext) {
				d.Canvas.Fill(gfx.Rect{X: d.Rect.X, Y: d.Rect.Y, Width: d.Rect.Width, Height: 1}, p.line)
				d.Canvas.Fill(gfx.Rect{X: d.Rect.X + (d.Rect.Width-26)/2, Y: d.Rect.Y + 3, Width: 26, Height: 1}, p.muted)
			})
		ui.Row(c).Height(36).PadX(10).Gap(8).Items(ui.Center).Background(p.shell).Children(func() {
			ui.Icon(c, lucideIcons[iconFileClock]).Size(16, 16)
			ui.Text(c, m.Title).Weight(gfx.WeightSemibold).Color(p.text).Shrink(0)
			ui.Text(c, session.Title).Color(p.muted).Grow(1).Ellipsis()
			label, icon := m.Expand, remoteLogExpandIcon
			if view.expanded {
				label, icon = m.Restore, remoteLogRestoreIcon
			}
			if ui.Button(c, label).Icon(icon).IconOnly().Ghost().Size(28, 28).Shrink(0).Tooltip(label).Clicked() {
				c.After(func() {
					view.expanded = !view.expanded
					state.fitWorkspaceTerminals(state.deck.ActiveWorkspace())
				})
			}
			if ui.Button(c, m.Close).Icon(lucideIcons[iconClose]).IconOnly().Ghost().Size(28, 28).Shrink(0).Tooltip(m.Close).Clicked() {
				c.After(func() { state.closeRemoteLogs(session) })
			}
		})
		ui.Slot(c).Height(1).FillWidth().Background(p.line)
		ui.Row(c).Grow(1).FillWidth().Clip().Children(func() {
			minPaths, minOutput := min(float32(150), frame.Width*.3), min(float32(260), frame.Width*.55)
			width := min(view.pathSplit.Pos, max(minPaths, frame.Width-minOutput-8))
			state.declareRemoteLogPaths(c, session, view, width)
			ui.Splitter(c, &view.pathSplit).Mins(minPaths, minOutput).Label(m.ResizePaths)
			state.declareRemoteLogOutput(c, session, view, frame.Width-width >= 470)
		})
	})
	if state.modal == modalNone && view.formMode != remoteLogFormNone {
		state.declareRemoteLogForm(c, session, view)
	}
}

func (state *State) declareRemoteLogPaths(c ui.C, session *Session, view *remoteLogView, width float32) {
	m, p := state.messages.RemoteLogs, state.pal
	host, saved := state.hostByID(session.HostID)
	paths := host.RemoteLogPaths
	selected := slices.IndexFunc(paths, func(path remoteLogPath) bool { return path.Path == view.selected.Path })
	if !view.paths.Changed() {
		view.paths.Select(selected)
	}
	if selected < 0 && view.selected != (remoteLogPath{}) {
		c.After(func() { session.clearRemoteLog() })
	}
	connected := session.canViewRemoteLogs() && view.cancel != nil && view.snapshot.ready && !view.snapshot.done
	ui.Column(c).Width(width).FillHeight().Pad(8).Gap(5).Clip().Background(p.shell).Children(func() {
		ui.Row(c).Height(32).Items(ui.Center).Gap(4).Children(func() {
			ui.Text(c, m.Paths).Weight(gfx.WeightSemibold).Color(p.text).Grow(1).Ellipsis()
			if ui.Button(c, m.Add).Icon(icons.Plus).IconOnly().Secondary().Size(28, 28).Shrink(0).
				Tooltip(m.Add).Disabled(!saved || !session.canViewRemoteLogs()).Clicked() {
				c.After(func() { view.editPath(remoteLogFormAdd, remoteLogPath{}) })
			}
		})
		if len(paths) == 0 {
			ui.Text(c, m.NoPaths).Color(p.muted).FillWidth()
			return
		}
		var menuAnchor ui.Box
		ui.List(c, &view.paths, len(paths), func(row ui.ListRow) {
			path := paths[row.Index]
			live := row.Index == selected && connected
			label := path.Name + " " + path.Path
			if live {
				label += " · " + m.Live
			}
			row.Element.Height(48).PadX(8).Label(label).Radius(6)
			if row.Index == selected {
				row.Element.Background(p.surfaceSoft)
			}
			row.Element.Children(func() {
				ui.Row(row.C).Fill().Items(ui.Center).Gap(8).Children(func() {
					ui.Icon(row.C, icons.File).Size(16, 16).Shrink(0).Color(p.muted)
					ui.Column(row.C).Grow(1).MinWidth(0).Gap(2).Children(func() {
						ui.Text(row.C, path.Name).Weight(gfx.WeightMedium).Color(p.text).Ellipsis()
						ui.Text(row.C, path.Path).Mono().FontSize(metrics.FontSizeS).Color(p.muted).Ellipsis()
					})
					if live {
						ui.Slot(row.C).Size(7, 7).Shrink(0).Radius(3.5).Background(p.success)
					}
					label := fmt.Sprintf(m.PathActionsFmt, path.Name)
					more := ui.Button(row.C.Key(path.Path), label).Icon(icons.Ellipsis).IconOnly().Ghost().
						Size(26, 26).Shrink(0).Tooltip(label).Disabled(!session.canViewRemoteLogs())
					if more.Clicked() {
						view.pathMenu = !view.pathMenu || view.pathMenuFor != path
						view.pathMenuFor = path
					}
					if view.pathMenu && view.pathMenuFor == path {
						menuAnchor = more
					}
				})
			})
		}).Label(m.Paths).Grow(1).FillWidth()
		if view.paths.Changed() {
			index := view.paths.Selected()
			if index >= 0 && index < len(paths) && index != selected {
				path := paths[index]
				c.After(func() { state.followRemoteLog(session, view, path) })
			}
		}
		if !menuAnchor.Valid() || state.modal != modalNone || !session.canViewRemoteLogs() {
			view.pathMenu = false
			return
		}
		path := view.pathMenuFor
		ui.Menu(c, menuAnchor, &view.pathMenu, func(menu *ui.MenuItems) {
			if menu.Item(m.Edit).Icon(icons.SquarePen).Clicked() {
				c.After(func() { view.editPath(remoteLogFormEdit, path) })
			}
			if menu.Item(m.Remove).Icon(icons.Trash2).Destructive().Clicked() {
				c.After(func() { view.editPath(remoteLogFormDelete, path) })
			}
		})
	})
}

func (state *State) declareRemoteLogOutput(c ui.C, session *Session, view *remoteLogView, wide bool) {
	m, p := state.messages.RemoteLogs, state.pal
	ui.Column(c).Grow(1).MinWidth(0).FillHeight().Clip().Children(func() {
		ui.Row(c).Height(39).PadX(8).Gap(6).Items(ui.Center).Children(func() {
			if wide {
				ui.Text(c, view.selected.Path).Mono().Color(p.text).Grow(1).Ellipsis()
			}
			ui.TextField(c, &view.query, m.Search, m.Search).Icon(lucideIcons[iconSearch]).Height(28).MinWidth(70).Grow(1)
			state.declareRemoteLogAutoScroll(c, view)
		})
		ui.Slot(c).Height(1).FillWidth().Background(p.line)
		ui.Control(c, m.Title, &view.scroll).Grow(1).FillWidth().Clip().Background(p.terminal).
			Draw(func(d *ui.DrawContext) { state.drawRemoteLogOutput(d, view) })
		state.declareRemoteLogStatus(c, session, view)
	})
}

func (state *State) declareRemoteLogAutoScroll(c ui.C, view *remoteLogView) {
	label := state.messages.RemoteLogs.AutoScroll
	width := c.Canvas().TextWidthWeight(label, metrics.FontSizeM, gfx.WeightMedium)
	theme := state.switchTheme()
	// The track and thumb keep Viva's switch geometry, scaled by the theme's
	// density, so the control here matches ui.Switch elsewhere.
	trackWidth, trackHeight := components.SwitchExtent(theme, false)
	scale := theme.Metrics.ControlHeight / 32
	thumbSize := trackHeight - 2*scale
	ui.SwitchBase(c, &view.follow, label).
		Size(trackWidth+8+width, max(28, trackHeight+10)).Shrink(0).HitOutset(0, 0, 0, 0).
		Disabled(view.buffer == nil).Draw(func(d *ui.DrawContext) {
		track := gfx.Rect{X: d.Rect.X, Y: d.Rect.Y + (d.Rect.Height-trackHeight)/2, Width: trackWidth, Height: trackHeight}
		progress := float32(0)
		if d.State.Checked {
			progress = 1
		}
		look := components.SwitchRecipe(theme, d.State, progress)
		if d.State.FocusVisible {
			d.Canvas.RoundedOutline(gfx.Rect{X: track.X - 2, Y: track.Y - 2, Width: trackWidth + 4, Height: trackHeight + 4}, trackHeight/2+2, 2, look.Ring)
		}
		d.Canvas.Rounded(track, trackHeight/2, look.Track)
		inset := 1 * scale
		d.Canvas.Rounded(gfx.Rect{X: track.X + inset + progress*(trackWidth-thumbSize-2*inset), Y: track.Y + (trackHeight-thumbSize)/2, Width: thumbSize, Height: thumbSize}, thumbSize/2, look.Thumb)
		color := state.pal.text
		if d.State.Disabled {
			color = state.pal.muted
		}
		d.Canvas.Label(label, gfx.Rect{X: track.X + trackWidth + 8, Y: d.Rect.Y, Width: width, Height: d.Rect.Height},
			gfx.TextStyle{Size: metrics.FontSizeM, Weight: gfx.WeightMedium, Color: color})
	})
}
func (state *State) declareRemoteLogStatus(c ui.C, session *Session, view *remoteLogView) {
	m, p := state.messages.RemoteLogs, state.pal
	status, color, retry := "", p.muted, false
	switch {
	case !session.canViewRemoteLogs():
		status, color = m.Disconnected, p.warning
	case view.buffer == nil:
		return
	case view.snapshot.done || view.cancel == nil:
		status, color, retry = m.Stopped, p.warning, true
		if view.snapshot.err != nil {
			status += " · " + view.snapshot.err.Error()
			if !view.snapshot.ready && len(view.snapshot.lines) != 0 {
				status = view.snapshot.lines[0]
			}
		}
	case !view.snapshot.ready:
		status = m.Connecting
	default:
		return
	}
	ui.Slot(c).Height(1).FillWidth().Background(p.line)
	ui.Row(c).Height(30).PadX(8).Gap(6).Items(ui.Center).Children(func() {
		ui.Text(c, status).Color(color).Grow(1).Ellipsis()
		if retry && ui.Button(c, m.Retry).Ghost().Shrink(0).Clicked() {
			c.After(func() { state.followRemoteLog(session, view, view.selected) })
		}
	})
}

func (view *remoteLogView) editPath(mode remoteLogForm, path remoteLogPath) {
	view.formMode, view.formOriginal, view.formError = mode, path, ""
	view.name.SetText(path.Name)
	view.path.SetText(path.Path)
}

func (state *State) declareRemoteLogForm(c ui.C, session *Session, view *remoteLogView) {
	m := state.messages.RemoteLogs
	title := m.Add
	if view.formMode == remoteLogFormEdit {
		title = m.Edit
	}
	deleting := view.formMode == remoteLogFormDelete
	bodyHeight := float32(145)
	focus := hitRemoteLogName
	if deleting {
		title, bodyHeight, focus = m.Remove, 56, gfx.ID{}
	}
	bodyHeight = withError(bodyHeight, view.formError)
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: title, Width: 460, Height: bodyHeight + 112, Layer: dialogLayer,
		Scope: dialogScope, CloseID: hitRemoteLogDismiss, InitialFocusID: focus,
	})
	dialog.Body(func() {
		ui.Column(c).Fill().Gap(6).Children(func() {
			if deleting {
				ui.Text(c, fmt.Sprintf(m.RemovePrompt, view.formOriginal.Path)).FillWidth().Ellipsis()
			} else {
				ui.Text(c, m.Name)
				ui.TextFieldID(c, hitRemoteLogName, &view.name, m.Name, "").Height(32)
				ui.Text(c, m.Path)
				ui.TextFieldID(c, hitRemoteLogPath, &view.path, m.Path, "/var/log/app.log").Height(32)
			}
			if view.formError != "" {
				ui.Text(c, view.formError).Color(state.pal.danger).FillWidth()
			}
		})
	})
	dialog.Footer(func() {
		if ui.Button(c, state.messages.Cancel).Secondary().Clicked() {
			c.After(func() { view.formMode = remoteLogFormNone })
		}
		if ui.Button(c, title).Clicked() {
			c.After(func() {
				path := remoteLogPath{Name: strings.TrimSpace(view.name.Text()), Path: view.path.Text()}
				if err := state.saveRemoteLogPath(session, view.formOriginal, path, deleting); err != nil {
					view.formError = err.Error()
					return
				}
				if view.formOriginal != (remoteLogPath{}) && view.selected.Path == view.formOriginal.Path {
					if deleting {
						session.clearRemoteLog()
					} else if path.Path != view.selected.Path {
						state.followRemoteLog(session, view, path)
					} else {
						view.selected = path
					}
				}
				view.formMode = remoteLogFormNone
			})
		}
	})
	if dialog.Dismissed() {
		view.formMode = remoteLogFormNone
	}
}

func (state *State) drawRemoteLogOutput(d *ui.DrawContext, view *remoteLogView) {
	p, m := state.pal, state.messages.RemoteLogs
	box := d.Rect
	view.visibleRows = max(1, int(box.Height/remoteLogRowHeight))
	view.updateMatches()
	last := view.lastTop()
	if view.follow {
		view.top = last
	}
	view.top = min(max(view.top, view.snapshot.first), last)
	area := components.BeginScrollArea(d.Canvas, state.Theme(), box, gfx.Size{
		Width: box.Width, Height: float32(view.outputRows()) * remoteLogRowHeight,
	}).Offset(0, float32(view.top-view.snapshot.first)*remoteLogRowHeight).
		ScrollbarSize(7).VerticalScrollbar(view.scroll.Parts().At(0), m.Title, "")
	view.scrollbar = area.BeginContent()
	style := gfx.TextStyle{Size: metrics.FontSizeM, Color: p.termFG, Mono: true}
	start := int(view.top - view.snapshot.first)
	drawn := 0
	for row := start; row < view.outputRows() && drawn < view.visibleRows; row++ {
		index := row
		if view.matchQuery != "" {
			index = view.matches[row]
		}
		line := view.snapshot.lines[index]
		y := box.Y + float32(drawn)*remoteLogRowHeight
		number := gfx.Rect{X: box.X + 6, Y: y, Width: 55, Height: remoteLogRowHeight}
		style.Color = p.muted
		d.Canvas.LabelAligned(strconv.FormatInt(view.snapshot.first+int64(index)+1, 10), number, style, gfx.AlignRight)
		style.Color = p.termFG
		switch {
		case strings.Contains(line, "ERROR") || strings.Contains(line, "FATAL"):
			style.Color = p.danger
		case strings.Contains(line, "WARN"):
			style.Color = p.warning
		}
		d.Canvas.Label(line, gfx.Rect{X: box.X + 72, Y: y, Width: max(0, box.Width-84), Height: remoteLogRowHeight}, style)
		drawn++
	}
	if view.buffer == nil || view.matchQuery != "" && drawn == 0 {
		label := m.SelectPath
		if view.matchQuery != "" {
			label = m.NoMatches
		}
		style.Color = p.muted
		d.Canvas.LabelCenter(label, box, style)
	}
	area.EndContent()
}

func (view *remoteLogView) lastTop() int64 {
	return view.snapshot.first + int64(max(0, view.outputRows()-view.visibleRows))
}

func (view *remoteLogView) scrollLog(event gfx.ScrollEvent) gfx.EventResult {
	delta := event.StepY(remoteLogRowHeight) + view.scrollRemainder
	lines := int64(delta / remoteLogRowHeight)
	view.scrollRemainder = delta - float32(lines)*remoteLogRowHeight
	return view.moveLog(lines)
}

func (view *remoteLogView) keyLog(event gfx.KeyEvent) gfx.EventResult {
	switch event.Key {
	case gfx.KeyUp:
		return view.moveLog(-1)
	case gfx.KeyDown:
		return view.moveLog(1)
	case gfx.KeyPageUp:
		return view.moveLog(-int64(view.visibleRows))
	case gfx.KeyPageDown:
		return view.moveLog(int64(view.visibleRows))
	case gfx.KeyHome:
		return view.moveLog(view.snapshot.first - view.top)
	case gfx.KeyEnd:
		return view.moveLog(view.lastTop() - view.top)
	default:
		return gfx.EventResult{}
	}
}

func (view *remoteLogView) moveLog(lines int64) gfx.EventResult {
	if lines == 0 || view.buffer == nil {
		return gfx.EventResult{Handled: true}
	}
	view.top = min(max(view.top+lines, view.snapshot.first), view.lastTop())
	view.follow = view.top == view.lastTop()
	return gfx.EventResult{Handled: true, Repaint: true}
}

func (view *remoteLogView) updateMatches() {
	query := strings.ToLower(strings.TrimSpace(view.query.Text()))
	if query == view.matchQuery && view.matchRevision == view.snapshot.revision {
		return
	}
	if query != view.matchQuery {
		view.top = view.snapshot.first
	}
	view.matchQuery, view.matchRevision = query, view.snapshot.revision
	view.matches = view.matches[:0]
	if query == "" {
		return
	}
	for index, line := range view.snapshot.lines {
		if strings.Contains(strings.ToLower(line), query) {
			view.matches = append(view.matches, index)
		}
	}
}

func (view *remoteLogView) outputRows() int {
	if view.matchQuery != "" {
		return len(view.matches)
	}
	return len(view.snapshot.lines)
}

func (view *remoteLogView) logScrollbarPointer(event gfx.PointerEvent) gfx.EventResult {
	if event.Kind == gfx.PointerDown && event.Target == view.scroll.Parts().At(0) && event.Button == gfx.PointerButtonPrimary {
		view.scrollDragging = true
		view.scrollGrab = event.Y - view.scrollbar.VerticalThumb.Y
	}
	if !view.scrollDragging {
		return gfx.EventResult{}
	}
	switch event.Kind {
	case gfx.PointerMove, gfx.PointerUp:
		offset := view.scrollbar.VerticalOffsetForThumb(event.Y - view.scrollGrab)
		top := view.snapshot.first + int64(offset/remoteLogRowHeight+.5)
		if event.Kind == gfx.PointerUp {
			view.scrollDragging = false
		}
		return view.moveLog(top - view.top)
	case gfx.PointerExit:
		view.scrollDragging = false
	}
	return gfx.EventResult{Handled: true}
}
