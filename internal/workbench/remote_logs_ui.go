package workbench

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"motty/internal/sshconn"
	"motty/internal/workbench/icons"
	layoutcore "motty/internal/workspace"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

var (
	remoteLogHits       = tree.Feature("remote-logs")
	hitServerLogs       = tree.ID(remoteLogHits, "toggle")
	hitRemoteLogDivider = tree.ID(remoteLogHits, "divider")
	hitRemoteLogDismiss = tree.ID(remoteLogHits, "dismiss")
	hitRemoteLogSudo    = tree.ID(remoteLogHits, "sudo")
	hitRemoteLogName    = tree.ID(remoteLogHits, "name")
	hitRemoteLogPath    = tree.ID(remoteLogHits, "path")
	hitRemoteLogOutput  = tree.ID(remoteLogHits, "output")
	hitRemoteLogBar     = tree.ID(remoteLogHits, "output-bar")
)

const (
	remoteLogRowHeight = 20
	// The line-number gutter and its right edge: the text starts at
	// remoteLogTextLeft and has the rest of the width.
	remoteLogTextLeft  = 72
	remoteLogTextInset = 84
)

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
			state.declareRemoteLogSudo(c, session)
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
			// The output is what the splitter leaves of the row. The thickness is
			// the one the last layout used, so the wrapped height the declaration
			// measures is the paint's own.
			outputWidth := float32(0)
			if thickness := view.pathSplit.GetThickness(); thickness > 0 {
				outputWidth = frame.Width - width - thickness
			}
			state.declareRemoteLogOutput(c, session, view, outputWidth, frame.Width-width >= 470)
		})
	})
	if state.modal == modalNone && view.formMode != remoteLogFormNone {
		state.declareRemoteLogForm(c, session, view)
	}
}

// declareRemoteLogSudo is the log panel's form of the tools' privilege switch:
// the same lock, wording and password dialog, sharing the session's flag, so a
// log the account itself cannot read opens as root once it is on.
func (state *State) declareRemoteLogSudo(c ui.C, session *Session) {
	m := state.messages
	label, style := m.SudoCommandsEnable, iconButtonQuiet
	if session.sudoCommands {
		label, style = m.SudoCommandsDisable, iconButtonActive
	}
	button := state.declareIconButton(c, hitRemoteLogSudo, iconLock, label, !session.canViewRemoteLogs(), style).
		Size(28, 28)
	if button.Clicked() {
		c.After(func() { state.toggleRemoteLogSudo(session) })
	}
}

func (state *State) toggleRemoteLogSudo(session *Session) {
	if session.sudoCommands {
		// The switch restarts the stream: it belongs to the privilege it
		// started with.
		state.setSudoCommands(session, "", false)
		return
	}
	if !sudoCommandsAvailable(session) {
		state.showActionError(state.messages.SudoCommandsEnable, errSudoCommandUnavailable.Error())
		return
	}
	state.sudoSFTP = sudoSFTPDialog{sessionID: session.ID, commands: true, logs: true}
	state.modal = modalSudoSFTP
}

// restartRemoteLog reopens the selected path after the privilege changed: the
// stream already running belongs to the previous privilege and would keep
// failing, or keep reading as root after it was turned off.
func (state *State) restartRemoteLog(session *Session, view *remoteLogView) {
	if session.remoteLogs != view || view.selected.Path == "" {
		return
	}
	state.followRemoteLog(session, view, view.selected)
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
			row.Element.Height(48).PadX(8).Label(label).Radius(cornerPx(6))
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

func (state *State) declareRemoteLogOutput(c ui.C, session *Session, view *remoteLogView, width float32, wide bool) {
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
		view.updateMatches()
		// The declaration sizes the content from the output's own width, so the
		// scroll range is right on the frame the first lines arrive. The paint
		// repeats the call with the width it actually got.
		cols, _ := remoteLogColumns(c.Canvas(), width)
		view.refreshRows(cols)
		scroll := ui.Scroll(c).Grow(1).FillWidth().Clip().Background(p.terminal).
			AlwaysScrollbars().ScrollbarSize(7).ScrollbarIDs(hitRemoteLogBar, gfx.ID{}).
			ScrollbarLabels(m.Title, "").
			HitWith(hitRemoteLogOutput, m.Title, gfx.HitOptions{Role: gfx.RoleGroup})
		if area := scroll.ScrollState(); area != nil {
			if view.resetScroll && !view.follow {
				area.ScrollTo(0)
			}
			view.resetScroll = false
			if view.follow != view.lastFollow {
				view.lastFollow = view.follow
				if view.follow {
					area.ScrollToEnd()
				}
			}
			if area.Offset != view.lastOffset {
				// The wheel or a drag moved the log: following follows only
				// while the view is still at its end, as the old log's switch
				// showed.
				view.follow = area.AtEnd()
				view.lastFollow = view.follow
				view.lastOffset = area.Offset
			}
			view.scroll = area
		}
		scroll = scroll.Follow(view.follow)
		scroll.Children(func() {
			height := max(float32(view.outputRows())*remoteLogRowHeight, view.viewport, 1)
			ui.Slot(c).FillWidth().Height(height).
				Draw(func(d *ui.DrawContext) { state.drawRemoteLogOutput(d, view) })
		})
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
	view.path = *text.NewEditor(path.Path)
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
				ui.TextField(c, &view.name, m.Name, "", ui.WithID(hitRemoteLogName)).Height(32)
				ui.Text(c, m.Path)
				ui.Slot(c).FillWidth().Height(32).Draw(func(d *ui.DrawContext) {
					value := view.path.String()
					focused := d.Canvas.Focused() == hitRemoteLogPath
					// Let the native editor scroll with the caret instead of clipping a long path at its start.
					components.PaintInput(d.Canvas, *d.Theme, d.Rect, components.InputSnapshot{
						ID: hitRemoteLogPath, Label: m.Path, Value: value, Placeholder: "/var/log/app.log",
						Focused: focused, PlatformEditing: focused && value != "",
					})
				})
			}
			if view.formError != "" {
				ui.TextSelectionArea(c, &view.formErrorSelection).FillWidth().Children(func() {
					ui.Text(c, view.formError).Color(state.pal.danger).FillWidth()
				})
			}
		})
	})
	dialog.Footer(func() {
		if ui.Button(c, state.messages.Cancel).Secondary().Clicked() {
			c.After(func() { view.formMode = remoteLogFormNone })
		}
		if ui.Button(c, title).Clicked() {
			c.After(func() {
				path := remoteLogPath{Name: strings.TrimSpace(view.name.Text()), Path: view.path.String()}
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
	area := view.scroll
	view.viewport = area.Viewport
	style := gfx.TextStyle{Size: metrics.FontSizeM, Color: p.termFG, Mono: true}
	cols, cellWidth := remoteLogColumns(d.Canvas, d.Rect.Width)
	view.refreshRows(cols)
	view.textOrigin = gfx.Point{X: d.Rect.X + remoteLogTextLeft, Y: d.Rect.Y}
	view.cellWidth = cellWidth
	// The content child is laid out at the area's scroll offset; the viewport
	// is where the area itself is.
	box := gfx.Rect{X: d.Rect.X, Y: d.Rect.Y + area.Offset, Width: d.Rect.Width, Height: area.Viewport}
	visibleRows := max(1, int(area.Viewport/remoteLogRowHeight))
	start := int(area.Offset / remoteLogRowHeight)
	if start < 0 {
		start = 0
	}
	d.Canvas.Clip(box)
	drawn := 0
	// One row past the viewport paints a partially scrolled row; the clip trims it.
	for row := start; row < view.outputRows() && drawn <= visibleRows; row++ {
		item := view.rows[row]
		line := view.snapshot.lines[item.line]
		y := d.Rect.Y + float32(row)*remoteLogRowHeight
		if item.start == 0 {
			number := gfx.Rect{X: box.X + 6, Y: y, Width: 55, Height: remoteLogRowHeight}
			numberStyle := style
			numberStyle.Color = p.muted
			d.Canvas.LabelAligned(strconv.FormatInt(view.snapshot.first+int64(item.line)+1, 10), number, numberStyle, gfx.AlignRight)
		}
		style.Color = p.termFG
		switch {
		case strings.Contains(line, "ERROR") || strings.Contains(line, "FATAL"):
			style.Color = p.danger
		case strings.Contains(line, "WARN"):
			style.Color = p.warning
		}
		d.Canvas.Label(line[item.start:item.end], gfx.Rect{X: box.X + remoteLogTextLeft, Y: y, Width: max(0, box.Width-remoteLogTextInset), Height: remoteLogRowHeight}, style)
		drawn++
	}
	// The selection paints over the text as the assistant's does: a quarter of
	// the text colour leaves the selected row readable.
	if selectStart, selectEnd, ok := view.selectionRange(); ok {
		color := withAlpha(p.text, 0.25)
		for row := start; row < view.outputRows() && row < start+visibleRows+1; row++ {
			item := view.rows[row]
			begin := view.lineStarts[item.line] + int64(item.start)
			end := view.lineStarts[item.line] + int64(item.end)
			lo, hi := max(selectStart, begin), min(selectEnd, end)
			if lo >= hi {
				continue
			}
			line := view.snapshot.lines[item.line]
			from, to := item.start+int32(lo-begin), item.start+int32(hi-begin)
			x := box.X + remoteLogTextLeft + float32(remoteLogCells(line[item.start:from]))*view.cellWidth
			width := float32(remoteLogCells(line[from:to])) * view.cellWidth
			d.Canvas.Fill(gfx.Rect{X: x, Y: d.Rect.Y + float32(row)*remoteLogRowHeight, Width: width, Height: remoteLogRowHeight}, color)
		}
	}
	d.Canvas.Unclip()
	if view.buffer == nil || view.matchQuery != "" && drawn == 0 {
		label := m.SelectPath
		if view.matchQuery != "" {
			label = m.NoMatches
		}
		style.Color = p.muted
		d.Canvas.LabelCenter(label, box, style)
	}
}

// refreshRows lays the retained lines out as the rows the output paints: one row
// per line, or several when a line is wider than cols cells. The declaration and
// the paint both pass the output's own width, so the scroll range is measured
// from the rows the paint will draw; cols <= 0 lays each line out as one row,
// before a layout has measured the panel. The wrap ring is keyed by absolute
// line number and reused, so a live tail only measures the lines it newly
// received.
func (view *remoteLogView) refreshRows(cols int) {
	if view.rowsReady && view.rowsRevision == view.snapshot.revision && view.rowsCols == cols && view.rowsQuery == view.matchQuery {
		return
	}
	view.rowsRevision, view.rowsCols, view.rowsQuery, view.rowsReady = view.snapshot.revision, cols, view.matchQuery, true
	view.rows = view.rows[:0]
	lines := view.snapshot.lines
	view.lineStarts = view.lineStarts[:0]
	start := view.snapshot.prefix
	view.plainStart = start
	for _, line := range lines {
		view.lineStarts = append(view.lineStarts, start)
		start += int64(len(line)) + 1
	}
	view.plainEnd = view.plainStart
	if len(lines) > 0 {
		view.plainEnd = start - 1
	}
	if view.wrapCols != cols {
		view.wrapCols = cols
		for index := range view.wrap {
			view.wrap[index].abs = -1
		}
	}
	match := 0
	for index, line := range lines {
		if view.matchQuery != "" {
			for match < len(view.matches) && view.matches[match] < index {
				match++
			}
			if match >= len(view.matches) || view.matches[match] != index {
				continue
			}
		}
		if cols <= 0 {
			view.rows = append(view.rows, remoteLogRow{line: int32(index), end: int32(len(line))})
			continue
		}
		from := int32(0)
		for _, end := range view.wrapLine(view.snapshot.first+int64(index), line, cols) {
			view.rows = append(view.rows, remoteLogRow{line: int32(index), start: from, end: end})
			from = end
		}
	}
	view.clampSelection()
}

// wrapLine is where one line broke at cols cells, remembered until the width
// changes or the ring reuses its slot. A wide character takes two cells, so a
// row ends where the next character would not fit, never inside one.
func (view *remoteLogView) wrapLine(abs int64, line string, cols int) []int32 {
	if len(view.wrap) != remoteLogBufferLines {
		view.wrap = make([]remoteLogLineWrap, remoteLogBufferLines)
		for index := range view.wrap {
			view.wrap[index].abs = -1
		}
	}
	entry := &view.wrap[abs%remoteLogBufferLines]
	if entry.abs == abs {
		return entry.ends
	}
	entry.abs = abs
	entry.ends = entry.ends[:0]
	cells := 0
	for index, r := range line {
		width := text.CellWidth(r)
		if cells+width > cols && cells > 0 {
			entry.ends = append(entry.ends, int32(index))
			cells = 0
		}
		cells += width
	}
	entry.ends = append(entry.ends, int32(len(line)))
	return entry.ends
}

// remoteLogColumns is how many log cells fit in width and the cell width they
// are measured at, as the painted mono label measures them.
func remoteLogColumns(canvas *gfx.Canvas, width float32) (int, float32) {
	cell, _ := canvas.MonoCellSize(metrics.FontSizeM, gfx.MonoSystem)
	if cell <= 0 || width <= remoteLogTextInset {
		return 0, cell
	}
	return int((width - remoteLogTextInset) / cell), cell
}

func remoteLogCells(value string) int {
	cells := 0
	for _, r := range value {
		cells += text.CellWidth(r)
	}
	return cells
}

// resetOutput drops the rows, wrap and selection of the previous stream.
func (view *remoteLogView) resetOutput() {
	view.snapshot = remoteLogSnapshot{}
	view.rows, view.rowsReady, view.rowsRevision, view.rowsQuery, view.rowsCols = view.rows[:0], false, 0, "", 0
	view.lineStarts = view.lineStarts[:0]
	view.plainStart, view.plainEnd = 0, 0
	view.wrapCols = 0
	for index := range view.wrap {
		view.wrap[index].abs = -1
	}
	view.selection.Clear()
}

// selectionRange is the selected part of the retained text, in the byte space
// lineStarts numbers: lines joined by a newline, starting after the dropped
// prefix, so an offset stays put while the stream appends and a rotation only
// moves its lower bound.
func (view *remoteLogView) selectionRange() (int64, int64, bool) {
	if view.selection.Empty() {
		return 0, 0, false
	}
	part := view.selection.In(0)
	start, end := int64(part.Anchor), int64(part.Focus)
	if start > end {
		start, end = end, start
	}
	start = max(start, view.plainStart)
	end = min(end, view.plainEnd)
	return start, end, start < end
}

// clampSelection keeps a selection inside the text that is still retained.
func (view *remoteLogView) clampSelection() {
	if view.selection.Empty() {
		return
	}
	clamp := func(point ui.TextPoint) ui.TextPoint {
		point.Offset = int(min(max(int64(point.Offset), view.plainStart), view.plainEnd))
		return point
	}
	view.selection.Anchor, view.selection.Focus = clamp(view.selection.Anchor), clamp(view.selection.Focus)
	if view.selection.Empty() && !view.selection.Dragging() {
		view.selection.Clear()
	}
}

// selectedText is what a copy puts on the clipboard. Soft wraps stay out of it:
// a wrapped line is copied as the one line the remote host wrote.
func (view *remoteLogView) selectedText() (string, bool) {
	start, end, ok := view.selectionRange()
	if !ok {
		return "", false
	}
	lines := view.snapshot.lines
	if len(lines) == 0 {
		return "", false
	}
	first := sort.Search(len(lines), func(index int) bool {
		return view.lineStarts[index]+int64(len(lines[index])) > start
	})
	var out strings.Builder
	for index := first; index < len(lines); index++ {
		begin := view.lineStarts[index]
		if index > first {
			if end <= begin-1 {
				break
			}
			out.WriteByte('\n')
		}
		lo, hi := max(start, begin), min(end, begin+int64(len(lines[index])))
		if lo < hi {
			out.WriteString(lines[index][lo-begin : hi-begin])
		}
		if begin+int64(len(lines[index])) >= end {
			break
		}
	}
	return out.String(), out.Len() > 0
}

func (view *remoteLogView) selectAll() bool {
	if len(view.snapshot.lines) == 0 || view.plainEnd <= view.plainStart {
		return false
	}
	view.selection.Select(ui.TextPoint{Offset: int(view.plainStart)}, ui.TextPoint{Offset: int(view.plainEnd)})
	return true
}

// selectionPoint is where a window point lies in the text: the row under it,
// clamped to the painted rows, and the byte offset nearest inside that row.
func (view *remoteLogView) selectionPoint(point gfx.Point) (ui.TextPoint, bool) {
	if len(view.rows) == 0 || view.cellWidth <= 0 {
		return ui.TextPoint{}, false
	}
	row := int((point.Y - view.textOrigin.Y) / remoteLogRowHeight)
	row = min(max(row, 0), len(view.rows)-1)
	item := view.rows[row]
	rowText := view.snapshot.lines[item.line][item.start:item.end]
	at := 0
	if x := point.X - view.textOrigin.X; x > 0 {
		at = len(rowText)
		display := float32(0)
		for offset := 0; offset < len(rowText); {
			r, size := utf8.DecodeRuneInString(rowText[offset:])
			width := float32(text.CellWidth(r)) * view.cellWidth
			if x < display+width {
				if x-display < width/2 {
					at = offset
				} else {
					at = offset + size
				}
				break
			}
			display += width
			offset += size
			at = offset
		}
	}
	return ui.TextPoint{Offset: int(view.lineStarts[item.line] + int64(item.start) + int64(at))}, true
}

// remoteLogPointer selects log text with the pointer. The scroll area's own
// bars keep their drag; everything that lands on the output is a selection.
func (state *State) remoteLogPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	if state.modal != modalNone || !state.terminalPage() {
		return gfx.EventResult{}, false
	}
	view := state.workspaceRemoteLogs(state.deck.ActiveWorkspace())
	if view == nil || view.buffer == nil || len(view.snapshot.lines) == 0 {
		return gfx.EventResult{}, false
	}
	primary := event.Button == gfx.PointerButtonPrimary || event.Button == gfx.PointerButtonNone
	if event.Kind == gfx.PointerDown && primary && event.Target == hitRemoteLogOutput {
		if result := view.selection.Pointer(event, view.selectionPoint); result.Handled {
			return gfx.EventResult{Handled: true, Repaint: result.Changed, Focus: hitRemoteLogOutput}, true
		}
		return gfx.EventResult{}, false
	}
	if view.selection.Dragging() {
		if result := view.selection.Pointer(event, view.selectionPoint); result.Handled {
			return gfx.EventResult{Handled: true, Repaint: result.Changed}, true
		}
	}
	return gfx.EventResult{}, false
}

// remoteLogShortcut copies the selected log text and selects all of it. It runs
// only while the output holds the keyboard, so the terminal's own copy is not
// taken over.
func (state *State) remoteLogShortcut(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if state.modal != modalNone || !state.terminalPage() || event.Target != hitRemoteLogOutput {
		return gfx.EventResult{}, false
	}
	view := state.workspaceRemoteLogs(state.deck.ActiveWorkspace())
	if view == nil || view.buffer == nil {
		return gfx.EventResult{}, false
	}
	chord, ok := keymap.FromEvent(event)
	if !ok {
		return gfx.EventResult{}, false
	}
	if chord.Modifiers == keymap.Primary && chord.Char == 'a' {
		if !event.Repeat {
			view.selectAll()
		}
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	if state.shortcuts.actionFor(chord) != shortcutCopy {
		return gfx.EventResult{}, false
	}
	if !event.Repeat {
		if value, selected := view.selectedText(); selected {
			state.copyRemoteLogText(value)
		}
	}
	return gfx.EventResult{Handled: true, Repaint: true}, true
}

func (state *State) copyRemoteLogText(value string) {
	if value == "" || state.options.SetClipboardText == nil {
		return
	}
	if err := state.options.SetClipboardText(value); err != nil {
		state.status = fmt.Sprintf(state.messages.StatusClipboardFailedFmt, err)
	}
}

// keyLogOutput scrolls the log with the arrows while its output holds the
// keyboard, as the old control did.
func (view *remoteLogView) keyLogOutput(event gfx.KeyEvent) gfx.EventResult {
	area := view.scroll
	if area == nil || view.buffer == nil {
		return gfx.EventResult{}
	}
	if event.Key == gfx.KeyEnd {
		area.ScrollToEnd()
		view.follow, view.lastFollow = true, true
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	page := float32(max(1, int(area.Viewport/remoteLogRowHeight))) * remoteLogRowHeight
	switch event.Key {
	case gfx.KeyUp:
		area.ScrollTo(area.Offset - remoteLogRowHeight)
	case gfx.KeyDown:
		area.ScrollTo(area.Offset + remoteLogRowHeight)
	case gfx.KeyPageUp:
		area.ScrollTo(area.Offset - page)
	case gfx.KeyPageDown:
		area.ScrollTo(area.Offset + page)
	case gfx.KeyHome:
		area.ScrollTo(0)
	default:
		return gfx.EventResult{}
	}
	view.follow = area.AtEnd()
	view.lastFollow = view.follow
	view.lastOffset = area.Offset
	return gfx.EventResult{Handled: true, Repaint: true}
}

func (view *remoteLogView) updateMatches() {
	query := strings.ToLower(strings.TrimSpace(view.query.Text()))
	if query == view.matchQuery && view.matchRevision == view.snapshot.revision {
		return
	}
	if query != view.matchQuery {
		view.resetScroll = true
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
	return len(view.rows)
}
