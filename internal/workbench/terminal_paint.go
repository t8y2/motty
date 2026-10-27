package workbench

import (
	"math"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
	"motty/internal/term"
)

// spinnerStep is how long a spinner spoke stays lit.
const spinnerStep = 125 * time.Millisecond

// barTool is one icon button on the right of an active pane's bar.
type barTool struct {
	id       ui.ID
	icon     uint8
	hint     string
	disabled bool
	style    iconButtonStyle
	groupEnd bool // the group of pane controls ends here: a wider gap follows
	slot     flex.Box
}

// maxBarTools is how many tools the bar can hold: transfer, Files, export log, export
// recording, record, the resource monitor, close, the two splits and broadcast.
const maxBarTools = 10

const (
	barToolSize = float32(24)
	barToolGap  = float32(4)
)

// paintTerminalPage draws a session: the active pane gets the complete command
// bar while inactive split panes keep only a compact identity and status bar.
// The page is a column: the window bar's room, the pane's bar, then the terminal
// whose cells paintGrid draws.
func (state *State) paintTerminalPage(canvas *ui.Canvas, size ui.Size, session *Session) {
	pal := state.pal
	barHeight := state.terminalInfoBarHeight(session.ID)
	geometry := terminalLayoutWithInfoBar(size, state.cellWidth, state.cellHeight, barHeight, state.padding())
	inactive := state.paneIsInactive(session.ID)
	barBackground, barBorder := state.terminalBarColors(session, inactive)
	inSplit := state.paneInSplit(session.ID)
	finding := !inactive && session.term.find.open

	// The right edge, from the right: Files, export log, export recording,
	// record, the opt-in resource monitor, then a gap and the two splits. The
	// left carries the connection's light, its words when it is not simply
	// connected, and the monitor's readout. Each button is an icon that says what
	// it does in a tooltip. The list is in painting order, Files first; the row
	// lays it out from the other end.
	var toolList [maxBarTools]barTool
	tools := toolList[:0]
	if !inactive && !finding {
		if session.host.isSSH() && !session.isLocal() {
			tools = append(tools, barTool{id: hitSFTPDetach, icon: iconTransfer, hint: state.messages.SFTPWindow.Open, disabled: !session.hasServer() || session.phase != sshconn.PhaseConnected || state.options.OpenContentWindow == nil, style: iconButtonQuiet})
		}
		filesStyle := iconButtonQuiet
		if context, ok := state.deck.Context(state.deck.ActiveWorkspace()); ok && context.Open {
			filesStyle = iconButtonActive
		}
		filesHint := state.messages.TipFiles
		if session.isLocal() {
			filesHint = state.messages.TipFilesLocal
		}
		tools = append(tools, barTool{id: hitSFTPOpen, icon: iconFolderOpen, hint: filesHint, disabled: !session.canBrowseFiles(), style: filesStyle})
		tools = append(tools, barTool{id: hitConnExport, icon: iconFileDown, hint: state.messages.ConnExportLog, style: iconButtonQuiet})
		tools = append(tools, barTool{id: hitTerminalRecordingExport, icon: iconFilm, hint: state.messages.RecordingExport, disabled: len(session.term.recording.events) == 0, style: iconButtonQuiet})
		recordIcon, recordHint, recordStyle := uint8(iconRecord), state.messages.TipRecordStart, iconButtonQuiet
		if session.term.recording.active {
			recordIcon, recordHint, recordStyle = iconRecordStop, state.messages.TipRecordStop, iconButtonDanger
		}
		tools = append(tools, barTool{id: hitTerminalRecord, icon: recordIcon, hint: recordHint, disabled: session.term.shell == nil, style: recordStyle})
		if hint, style, offered := state.remoteStatsToggle(session); offered {
			tools = append(tools, barTool{id: hitRemoteStats, icon: iconActivity, hint: hint, style: style})
		}
		if inSplit {
			tools = append(tools, barTool{id: hitPaneClose, icon: iconClose, hint: state.messages.PaneClose, style: iconButtonQuiet, groupEnd: true})
		}
		tools = append(tools, barTool{id: hitPaneSplitVertical, icon: iconRows2, hint: state.messages.PaneSplitVertical, style: iconButtonQuiet, groupEnd: !inSplit})
		tools = append(tools, barTool{id: hitPaneSplitHorizontal, icon: iconColumns2, hint: state.messages.PaneSplitHorizontal, style: iconButtonQuiet})
		if inSplit {
			hint, style := state.messages.Tools.BroadcastStart, iconButtonQuiet
			if state.broadcasting(session.ID) {
				hint, style = state.messages.Tools.BroadcastStop, iconButtonActive
			}
			tools = append(tools, barTool{id: hitBroadcast, icon: iconRadio, hint: hint, style: style})
		}
	}

	status, dot := state.sessionStatus(session)
	title := session.Title
	if title == "" {
		title = session.Address
	}
	// Anything but a plain connection says so in words.
	showStatus := session.phase != sshconn.PhaseConnected || !session.term.live
	showProgress := !session.isLocal() && !session.term.live && !(inSplit && session.quietOK())
	var light, spinner, statsSlot, progressSlot flex.Box
	var find findBarSlots

	// The bar is a step darker than the window bar above it and a step lighter
	// than the terminal below, so the three read as separate strips.
	f := state.terminalPageLayout.Begin(canvas, state.Theme(), ui.Rect{Width: size.Width, Height: size.Height})
	f.Column().Fill().PadSides(windowBarHeight, 0, 0, 0).Children(func() {
		f.Stack().Height(barHeight).Shrink(0).Background(barBackground).Children(func() {
			switch {
			case inactive:
				spinner = state.declareInactiveBar(f, size.Width, title, status, session)
			case finding:
				find = state.declareFindBar(f, session)
			default:
				f.Row().Fill().PadSides(0, termPadX, 0, termPadX-2).Items(flex.Center).Children(func() {
					// The light's box hangs two pixels outside the bar's padding: the
					// light itself sits on the padding's edge.
					f.Row().Grow(1).Gap(8).MarginSides(0, 10, 0, 0).Items(flex.Center).Children(func() {
						light = f.Slot().Size(16, 16)
						// The name stays beside the light, as it does beside the terminal
						// icon of a pane that is not active: only the icon changes with focus.
						f.Text(title).FontSize(barTitleSize).Weight(ui.WeightSemibold).Color(pal.text).LineHeight(22).
							Width(canvas.TextWidthWeight(title, barTitleSize, ui.WeightSemibold)+2).Ellipsis().MarginSides(0, 4, 0, 0)
						if showStatus {
							f.Text(status).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(pal.muted).LineHeight(22).
								Width(canvas.TextWidthWeight(status, metrics.FontSizeS, ui.WeightMedium)+2).MarginSides(0, 4, 0, 0)
						}
						if session.stats != nil {
							// The readout drops the metrics that do not fit (see
							// paintRemoteStatsReadout): it gets the room, and keeps clear of the tools.
							statsSlot = f.Slot().Grow(1).Height(infoBarHeight).MinWidth(72).MarginSides(0, 12, 0, 0)
						}
						if showProgress {
							// The log is up in place of the connection page: say how to get it back.
							progressSlot = f.Slot().Size(196, infoBarHeight).Shrink(1)
						}
					})
					f.Row().Gap(barToolGap).Shrink(0).Items(flex.Center).Children(func() {
						for index := len(tools) - 1; index >= 0; index-- {
							slot := f.Slot().Size(barToolSize, barToolSize).Shrink(0)
							if tools[index].groupEnd {
								slot = slot.MarginSides(0, barToolGap, 0, 0)
							}
							tools[index].slot = slot
						}
					})
				})
			}
			f.Row().FillWidth().Height(1).Self(flex.End).Background(barBorder)
		})
		f.Row().Grow(1).Background(state.ground(state.termPal.terminal))
	})
	f.End()

	switch {
	case inactive:
		if session.busy {
			phase := components.SpinnerPhase(canvas.Now() / spinnerStep)
			components.PaintSpinner(canvas, state.Theme(), spinner.Rect(), phase)
			canvas.RequestFrameIn(spinnerStep)
		}
		// No status light here: the light is what marks the pane that has the focus,
		// and a green dot on the far side of an inactive bar would read as that.
	case finding:
		state.paintFindBarSlots(canvas, session, find)
	default:
		for index := range tools {
			tool := &tools[index]
			state.paintTintedIconButton(canvas, tool.id, tool.icon, tool.slot.Rect(), tool.hint, tool.disabled, tool.style, barBackground, terminalToolAccent(tool.id))
		}
		spot := light.Rect()
		if session.busy {
			// One spoke step every 125 ms, so the screen is asked for a frame at
			// exactly that pace; an idle window asks for none.
			phase := components.SpinnerPhase(canvas.Now() / spinnerStep)
			components.PaintSpinner(canvas, state.Theme(), spot, phase)
			canvas.RequestFrameIn(spinnerStep)
		} else {
			kind, _ := state.sessionLight(session)
			paintLight(canvas, spot.X+spot.Width/2, spot.Y+spot.Height/2, kind, dot, barBackground)
		}
		if session.stats != nil {
			slot := statsSlot.Rect()
			if len(tools) > 0 && slot.X+slot.Width > tools[len(tools)-1].slot.Rect().X {
				// A pane too narrow for its tools: the readout is what gives way.
				slot.Width = 0
			}
			state.paintRemoteStatsReadout(canvas, session.stats, slot.X, slot.Y, slot.X+slot.Width)
		}
		if showProgress {
			components.BeginButtonRow(canvas, state.theme, progressSlot.Rect()).Size(components.ButtonSizeXSmall).Paint(
				components.ButtonAction{ID: hitConnProgress, Label: state.messages.ConnShowProgress, Variant: components.ButtonGhost})
		}
	}

	state.paintGrid(canvas, session, geometry)
}

// paintAIMarking draws the strip that says this line's command was started by
// the assistant. It sits in the terminal's left padding, outside the grid, so it
// is not part of the copied command or the emulator's screen cells.
func (state *State) paintAIMarking(canvas *ui.Canvas, geometry terminalGeometry, y, cellHeight float32) {
	x := geometry.area.X + 4
	canvas.Fill(ui.Rect{X: x, Y: y, Width: 3, Height: cellHeight}, withAlpha(state.pal.accent, 0.85))
}

// declareInactiveBar declares the bar of a pane that shares its workspace
// without being the active one: the terminal icon, the name, and on the right the
// reading or the state in words, with a mark when typing goes to this pane too.
// It returns the box where a busy pane's spinner goes.
func (state *State) declareInactiveBar(f *flex.Frame, width float32, title, status string, session *Session) (spinner flex.Box) {
	pal, canvas := state.pal, f.Canvas()
	readout := status
	if session.stats != nil {
		readout = state.remoteStatsText(session.stats)
	}
	gap := float32(29) // from the name to the reading
	if session.busy {
		gap = 6
	}
	f.Row().Fill().PadX(termPadX).Items(flex.Center).Children(func() {
		f.Icon(lucideIcons[iconTerminal]).Size(14, 14).Color(pal.faint).MarginSides(0, 7, 0, 0)
		f.Text(title).FontSize(barTitleSize).Weight(ui.WeightSemibold).Color(pal.muted).LineHeight(22).Ellipsis().Grow(1).MarginSides(0, gap, 0, 0)
		if session.busy {
			spinner = f.Slot().Size(14, 14).MarginSides(0, 9, 0, 0)
		}
		f.Text(readout).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(pal.faint).LineHeight(20).
			Width(canvas.TextWidthWeight(readout, metrics.FontSizeS, ui.WeightMedium) + 2).MaxWidth(width * 0.42).Shrink(0)
		if state.broadcasting(session.ID) {
			// Typing goes to this pane too: the one mark an inactive bar carries.
			f.Icon(lucideIcons[iconRadio]).Size(14, 14).Color(pal.accent).MarginSides(0, 0, 0, 8)
		}
	})
	return spinner
}

// paintGrid draws a session's character grid: for each row its background
// spans and one fixed-pitch label, then the cursor. Rows are prepared once per
// change of their content; painting a screen that did not change builds
// nothing.
func (state *State) paintGrid(canvas *ui.Canvas, session *Session, geometry terminalGeometry) {
	state.paintGridWithID(canvas, session, geometry, hitTerminalList.At(int(session.ID)), false)
}

func (state *State) paintGridWithID(canvas *ui.Canvas, session *Session, geometry terminalGeometry, id ui.ID, sensitive bool) {
	t, pal := session.term, &state.termPal
	inactive := state.paneIsInactive(session.ID)
	focused := canvas.Focused() == id
	t.expireSync()
	cols, rows := t.presentedSize()
	cellWidth, cellHeight := state.cellWidth, state.cellHeight
	cursor := t.presentedCursor()
	composing := t.preedit.Composing
	predictionText, predictionRuns, predictionCells, predicting := t.prediction.overlay()
	predicting = predicting && !composing && cursor.Visible && t.sync == nil
	if t.sync != nil && t.clock != nil {
		canvas.RequestFrameIn(t.syncRemaining())
	}
	// The cursor blinks while the terminal has the keyboard and something is
	// happening; it is solid from the moment it moves or the pane is entered.
	blinkOn := true
	if cursor.Visible && focused && !composing && !state.terminalCursorSteady && !cursor.Steady {
		blinkOn = t.blink.on(canvas, cursor.X, cursor.Y)
	} else {
		t.blink.rest(cursor.X, cursor.Y)
	}
	if t.sync != nil {
		blinkOn = true
	}
	block := cursor.Visible && cursor.Style == term.CursorBlock && focused && !composing && !predicting && blinkOn
	style := ui.TextStyle{Size: state.terminalFontSize, Color: pal.termFG, MonoFace: state.terminalFontFace}
	// The strip is empty on the alternate screen, whose program owns the grid.
	gutterShown := !sensitive && state.terminalGutter != gutterOff && !t.presentedAltScreen() && t.sync == nil
	gutterStyle := ui.TextStyle{Size: state.terminalFontSize, Color: pal.faint, MonoFace: state.terminalFontFace}
	labelWidth := float32(math.Ceil(float64(float32(cols)*cellWidth))) + 1

	// A find scan deferred by the refresh interval gets the frame that runs it.
	if t.find.pending {
		delay := terminalFindRefreshInterval
		if t.find.lastScan > 0 {
			delay = max(terminalFindRefreshInterval-(state.now()-t.find.lastScan), 0)
		}
		canvas.RequestFrameIn(delay)
	}
	canvas.Clip(geometry.area)
	t.cache.begin()
	if cap(t.rowIDs) < rows {
		t.rowIDs = make([]uint64, rows)
	} else {
		t.rowIDs = t.rowIDs[:rows]
	}
	firstFindMatch := 0
	for row := 0; row < rows; row++ {
		column := -1
		if block && row == cursor.Y {
			column = cursor.X
		}
		line := t.presentedRow(row)
		t.rowIDs[row] = line.ID
		entry := t.cache.row(line, cols, column, pal, inactive)
		y := geometry.top + float32(row)*cellHeight
		for _, span := range entry.spans {
			from := snap(geometry.left + float32(span.from)*cellWidth)
			to := snap(geometry.left + float32(span.to)*cellWidth)
			canvas.Fill(ui.Rect{X: from, Y: y, Width: to - from, Height: cellHeight}, span.color)
		}
		if t.sync == nil && t.find.open && len(t.find.matches) > 0 {
			firstFindMatch = state.paintTerminalFindMatches(canvas, session, geometry, row, t.screen.ViewLine(row), firstFindMatch)
		}
	}
	if t.sync == nil {
		state.paintTerminalImages(canvas, t, geometry, true, t.rowIDs)
	}
	for row := 0; row < rows; row++ {
		column := -1
		if block && row == cursor.Y {
			column = cursor.X
		}
		entry := t.cache.row(t.presentedRow(row), cols, column, pal, inactive)
		y := geometry.top + float32(row)*cellHeight
		if from, to, selected := t.selection.span(row, t.presentedRow(row), cols); selected {
			left := snap(geometry.left + float32(from)*cellWidth)
			right := snap(geometry.left + float32(to)*cellWidth)
			canvas.Fill(ui.Rect{X: left, Y: y, Width: right - left, Height: cellHeight}, withAlpha(pal.termFG, 0.25))
		}
		if gutterShown && row <= cursor.Y {
			// Rows below the cursor have not been written yet.
			state.paintGutter(canvas, entry, t, geometry, row, y, gutterStyle)
		}
		if !t.presentedAltScreen() && row <= cursor.Y {
			if _, ok := t.aiRows[t.rowIDs[row]]; ok {
				state.paintAIMarking(canvas, geometry, y, cellHeight)
			}
		}
		if len(entry.runs) > 0 {
			canvas.LabelRuns(entry.text, ui.Rect{X: geometry.left, Y: y, Width: labelWidth, Height: cellHeight}, style, cellWidth, entry.runs)
		}
		if t.sync == nil {
			state.paintTerminalLink(canvas, t, geometry, row)
		}
	}
	if gutterShown {
		canvas.Fill(ui.Rect{X: geometry.left - gutterRuleGap/2, Y: geometry.top, Width: 1, Height: float32(rows) * cellHeight}, withAlpha(pal.faint, 0.35))
	}
	if t.sync == nil {
		state.paintTerminalImages(canvas, t, geometry, false, t.rowIDs)
	}
	t.cache.end(rows)
	if predicting && cursor.Y < rows {
		// Typed text the server has not echoed yet, drawn at once over the cursor
		// cell and the cells after it, muted and underlined so it reads as
		// provisional. It never enters the terminal's own buffer.
		x := geometry.left + float32(cursor.X)*cellWidth
		y := geometry.top + float32(cursor.Y)*cellHeight
		width := float32(predictionCells) * cellWidth
		canvas.Fill(ui.Rect{X: x, Y: y, Width: width, Height: cellHeight}, pal.terminal)
		for index := range predictionRuns {
			predictionRuns[index].Color = pal.muted
		}
		canvas.LabelRuns(predictionText, ui.Rect{X: x, Y: y, Width: width, Height: cellHeight}, style, cellWidth, predictionRuns)
		cursor.X += predictionCells
	}

	if cursor.Visible && cursor.Y < rows {
		cursorColor := pal.cursor
		if inactive {
			cursorColor.Alpha *= 0.45
		}
		x := geometry.left + float32(cursor.X)*cellWidth
		y := geometry.top + float32(cursor.Y)*cellHeight
		switch {
		case !blinkOn:
			// the dark half of a blink
		case composing:
			// The input method draws over the cursor cell; clear what is under it.
			canvas.Fill(ui.Rect{X: x, Y: y, Width: float32(1+displayCells(t.preedit.Value)) * cellWidth, Height: cellHeight}, pal.terminal)
		case cursor.Style == term.CursorUnderline:
			canvas.Fill(ui.Rect{X: x, Y: y + cellHeight - 2, Width: cellWidth, Height: 2}, cursorColor)
		case cursor.Style == term.CursorBar:
			canvas.Fill(ui.Rect{X: x, Y: y, Width: 3, Height: cellHeight}, cursorColor)
		case !focused:
			// A block cursor outlines its cell while the terminal does not have the keyboard.
			canvas.Fill(ui.Rect{X: x, Y: y, Width: cellWidth, Height: 1}, cursorColor)
			canvas.Fill(ui.Rect{X: x, Y: y + cellHeight - 1, Width: cellWidth, Height: 1}, cursorColor)
			canvas.Fill(ui.Rect{X: x, Y: y, Width: 1, Height: cellHeight}, cursorColor)
			canvas.Fill(ui.Rect{X: x + cellWidth - 1, Y: y, Width: 1, Height: cellHeight}, cursorColor)
		}
	}
	if !sensitive && focused && !composing && !predicting && t.sync == nil {
		if rest, ok := state.commandGhost(session); ok {
			state.paintGhost(canvas, t, geometry, cursor, rest, block)
		}
	}
	canvas.Unclip()

	canvas.HitWithOptions(id, geometry.area, t.accessibleName(state.messages.LabelTerminalFmt, session.Address), ui.HitOptions{
		Role: ui.RoleTerminal, Autofocus: session.ID == state.activeSessionID,
	})
}

func (state *State) paintTerminalImages(canvas *ui.Canvas, terminal *terminal, geometry terminalGeometry, behind bool, visible []uint64) {
	placements := terminal.screen.Images()
	if len(placements) == 0 {
		return
	}
	for _, placement := range placements {
		if placement.Image == nil || behind != (placement.Z < 0) {
			continue
		}
		row, ok := visibleRow(visible, placement.LineID)
		if !ok {
			// An image anchored off-screen may still reach into the viewport;
			// resolving that rare case keeps its old cost.
			row, ok = terminal.screen.ImageRow(placement.LineID)
		}
		if !ok {
			continue
		}
		sourceWidth, sourceHeight := placement.SourceWidth, placement.SourceHeight
		if sourceWidth <= 0 || sourceHeight <= 0 {
			continue
		}
		width, height := float32(sourceWidth), float32(sourceHeight)
		if placement.WidthCells > 0 {
			width = float32(placement.WidthCells) * state.cellWidth
		} else if placement.WidthPixels > 0 {
			width = float32(placement.WidthPixels)
		}
		if placement.HeightCells > 0 {
			height = float32(placement.HeightCells) * state.cellHeight
		} else if placement.HeightPixels > 0 {
			height = float32(placement.HeightPixels)
		}
		if placement.WidthCells > 0 || placement.WidthPixels > 0 {
			if placement.HeightCells == 0 && placement.HeightPixels == 0 {
				height = width * float32(sourceHeight) / float32(sourceWidth)
			}
		} else if placement.HeightCells > 0 || placement.HeightPixels > 0 {
			width = height * float32(sourceWidth) / float32(sourceHeight)
		}
		bounds := ui.Rect{
			X:     geometry.left + float32(placement.Column)*state.cellWidth,
			Y:     geometry.top + float32(row)*state.cellHeight,
			Width: width, Height: height,
		}
		canvas.DrawBitmapRegion(ui.Bitmap{
			Pix: placement.Image.Pix, Width: placement.Image.Width, Height: placement.Image.Height,
			Stride: placement.Image.Stride, ID: placement.Image.ID,
		}, ui.Rect{
			X: float32(placement.SourceX), Y: float32(placement.SourceY),
			Width: float32(sourceWidth), Height: float32(sourceHeight),
		}, bounds)
	}
}

// visibleRow finds a line identity among the visible rows. There are only a
// screenful of entries, so a linear scan beats allocating a map per paint.
func visibleRow(visible []uint64, id uint64) (int, bool) {
	for row, candidate := range visible {
		if candidate == id {
			return row, true
		}
	}
	return 0, false
}
