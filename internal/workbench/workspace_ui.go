package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"
	"math"
	"motty/internal/sshconn"
	layoutcore "motty/internal/workspace"
)

const (
	paneDividerHitWidth = float32(8)
	minPaneWidth        = float32(240)
	minPaneHeight       = float32(140)
	contextDrawerAt     = float32(900)
)

func (state *State) baseWorkspacePixelBounds(size ui.Size) ui.Rect {
	return ui.Rect{Y: windowBarHeight, Width: size.Width, Height: max(size.Height-windowBarHeight, 0)}
}

func (state *State) workspacePixelBounds(size ui.Size) ui.Rect {
	return state.workspacePixelBoundsFor(state.deck.ActiveWorkspace(), size)
}

func (state *State) workspacePixelBoundsFor(workspaceID layoutcore.WorkspaceID, size ui.Size) ui.Rect {
	bounds := state.baseWorkspacePixelBounds(size)
	context, ok := state.deck.Context(workspaceID)
	if ok && context.Open && size.Width >= contextDrawerAt {
		// The files panel sits on the left, so the panes start after it and its
		// one-pixel divider.
		panel := state.contextPanelExtent(bounds, context.Width) + 1
		bounds.X += panel
		bounds.Width = max(0, bounds.Width-panel)
	}
	// The assistant's panel sits on the right, so the panes end before it.
	bounds.Width = max(0, bounds.Width-state.aiReserve(workspaceID, size))
	return bounds
}

func (state *State) contextPanelExtent(workspace ui.Rect, preferred float32) float32 {
	maximum := min(layoutcore.MaxContextWidth, workspace.Width*0.45)
	minimum := min(layoutcore.MinContextWidth, maximum)
	return min(max(preferred, minimum), maximum)
}

func pixelBounds(frame ui.Rect, bounds FractionBounds) ui.Rect {
	return ui.Rect{
		X:      frame.X + bounds.X*frame.Width,
		Y:      frame.Y + bounds.Y*frame.Height,
		Width:  bounds.Width * frame.Width,
		Height: bounds.Height * frame.Height,
	}
}

func paneVirtualSize(bounds ui.Rect) ui.Size {
	return ui.Size{Width: bounds.Width, Height: bounds.Height + windowBarHeight}
}

func paneEmbedOrigin(bounds ui.Rect) ui.Point {
	return ui.Point{X: bounds.X, Y: bounds.Y - windowBarHeight}
}

func (state *State) refreshWorkspaceViews() bool {
	workspaceID := state.deck.ActiveWorkspace()
	revision, ok := state.deck.Revision(workspaceID)
	if ok && state.workspaceViewCacheID == workspaceID && state.workspaceViewRevision == revision {
		return false
	}
	state.ensurePaneSlots()
	state.panes = state.Panes(state.panes[:0])
	state.paneSplits = state.PaneSplits(state.paneSplits[:0])
	state.workspaceViewCacheID = workspaceID
	state.workspaceViewRevision = revision
	state.workspaceViewBuilds++
	return true
}

func (state *State) paintWorkspacePanes(canvas *ui.Canvas, size ui.Size) {
	state.refreshWorkspaceViews()
	frame := state.workspacePixelBounds(size)
	if len(state.panes) == 0 {
		state.paintEmptyWorkspace(canvas, frame)
		state.paintContextPanel(canvas, size, frame)
		return
	}
	if len(state.panes) == 1 {
		pane := state.panes[0]
		if session, ok := state.sessionByIDPointer(pane.SessionID); ok {
			state.paneCanvas.BeginEmbedded(canvas, 0)
			state.paintSessionPage(&state.paneCanvas, paneVirtualSize(frame), session)
			state.paneCanvas.EndFrame()
			canvas.Clip(frame)
			canvas.Embed(&state.paneCanvas, paneEmbedOrigin(frame), 0)
			canvas.Unclip()
			adoptEmbeddedFocus(canvas, &state.paneCanvas, 0)
		} else {
			state.paintEmptyPane(canvas, frame, pane)
		}
		state.paintContextPanel(canvas, size, frame)
		return
	}

	for index := range state.panes {
		pane := state.panes[index]
		bounds := pixelBounds(frame, pane.Bounds)
		canvas.HitWithOptions(hitPaneActivateList.At(index), bounds, state.messages.LabelNewTab, ui.HitOptions{Role: ui.RoleButton, SkipFocus: true})
		session, ok := state.sessionByIDPointer(pane.SessionID)
		if !ok {
			state.paintEmptyPane(canvas, bounds, pane)
			continue
		}
		mount := paneMount(session)
		state.paneCanvas.BeginEmbedded(canvas, mount)
		state.paintSessionPage(&state.paneCanvas, paneVirtualSize(bounds), session)
		state.paneCanvas.EndFrame()
		canvas.Clip(bounds)
		canvas.Embed(&state.paneCanvas, paneEmbedOrigin(bounds), mount)
		canvas.Unclip()
		if pane.Active {
			adoptEmbeddedFocus(canvas, &state.paneCanvas, mount)
		}
	}
	state.paintPaneDividers(canvas, frame)
	state.paintActivePaneGlow(canvas, frame, size)
	state.paintContextPanel(canvas, size, frame)
}

// windowCornerRadius is the radius the window's own content area is clipped to
// at its bottom corners; a pane that touches one has to follow it.
const windowCornerRadius = float32(10)

// The two arcs that carry the rim round a window corner, as one-pixel strokes
// whose centerline runs half a pixel inside the pane's edge. Built once: the rim
// is painted every frame and must not allocate.
var rimCornerRight, rimCornerLeft = func() (right, left ui.Path) {
	const r, k = windowCornerRadius, 0.5522847
	const c = r - 0.5 // radius of the centerline
	// Right corner, box r x r with the circle's center at its top-left.
	right = ui.NewPath(r, r, ui.MoveTo(c, 0), ui.CubicTo(c, c*k, c*k, c, 0, c))
	// Left corner, box r x r with the circle's center at its top-right.
	left = ui.NewPath(r, r, ui.MoveTo(r, c), ui.CubicTo(r-c*k, c, r-c, c*k, r-c, 0))
	return right, left
}()

// activePaneRimColor is the rim as one opaque color: the accent a third of the
// way into the terminal's background. Translucent, the same line came out
// different on every side, because each side lies over something else (the
// window bar, an info bar, a divider).
func (state *State) activePaneRimColor() ui.Color {
	const amount = 0.34
	pal := state.pal
	mix := func(from, to float32) float32 { return from + (to-from)*amount }
	return ui.Color{
		Red: mix(pal.terminal.Red, pal.accent.Red), Green: mix(pal.terminal.Green, pal.accent.Green),
		Blue: mix(pal.terminal.Blue, pal.accent.Blue), Alpha: 1,
	}
}

// paintActivePaneGlow rims the active pane of a split workspace with a faint
// accent line, drawn over the terminals so it shows at their edges. A lone pane
// has nothing to be told apart from and gets none. The pane is snapped to whole
// pixels so all four sides are the same line, and where it meets one of the
// window's rounded bottom corners the rim turns with the window instead of
// ending short of it.
func (state *State) paintActivePaneGlow(canvas *ui.Canvas, frame ui.Rect, window ui.Size) {
	if len(state.panes) < 2 {
		return
	}
	for index := range state.panes {
		if !state.panes[index].Active {
			continue
		}
		const width, epsilon, overlap = float32(1), float32(0.5), float32(0.5)
		bounds := snapRect(pixelBounds(frame, state.panes[index].Bounds))
		line := state.activePaneRimColor()
		right, bottom := bounds.X+bounds.Width, bounds.Y+bounds.Height
		atBottom := bounds.Y+bounds.Height >= window.Height-epsilon
		var leftInset, rightInset float32
		if atBottom && bounds.X <= epsilon {
			leftInset = windowCornerRadius
		}
		if atBottom && right >= window.Width-epsilon {
			rightInset = windowCornerRadius
		}
		// Straight edges. Where an arc takes over, a straight edge runs half a
		// pixel into it: the line is opaque, so the overlap shows nothing, and it
		// hides the feathered end of the arc.
		canvas.Fill(ui.Rect{X: bounds.X, Y: bounds.Y, Width: bounds.Width, Height: width}, line)
		canvas.Fill(ui.Rect{X: bounds.X + leftInset - overlap, Y: bottom - width, Width: max(bounds.Width-leftInset-rightInset+2*overlap, 0), Height: width}, line)
		sideHeight := func(inset float32) float32 {
			if inset == 0 {
				return bounds.Height - 2*width // from the top edge to the bottom edge
			}
			return bounds.Height - width - inset + overlap // from the top edge to the arc
		}
		canvas.Fill(ui.Rect{X: bounds.X, Y: bounds.Y + width, Width: width, Height: max(sideHeight(leftInset), 0)}, line)
		canvas.Fill(ui.Rect{X: right - width, Y: bounds.Y + width, Width: width, Height: max(sideHeight(rightInset), 0)}, line)
		style := ui.PathStyle{Stroke: line, StrokeWidth: width}
		if rightInset > 0 {
			canvas.DrawPath(rimCornerRight, ui.Rect{X: right - rightInset, Y: bottom - rightInset, Width: rightInset, Height: rightInset}, style)
		}
		if leftInset > 0 {
			canvas.DrawPath(rimCornerLeft, ui.Rect{X: bounds.X, Y: bottom - leftInset, Width: leftInset, Height: leftInset}, style)
		}
		return
	}
}

// snapRect rounds every edge of r to a whole logical pixel.
func snapRect(r ui.Rect) ui.Rect {
	left, top := float32(math.Round(float64(r.X))), float32(math.Round(float64(r.Y)))
	right, bottom := float32(math.Round(float64(r.X+r.Width))), float32(math.Round(float64(r.Y+r.Height)))
	return ui.Rect{X: left, Y: top, Width: right - left, Height: bottom - top}
}

func (state *State) paintEmptyWorkspace(canvas *ui.Canvas, bounds ui.Rect) {
	state.paintNewTabPage(canvas, bounds)
}

func (state *State) paintEmptyPane(canvas *ui.Canvas, bounds ui.Rect, pane Pane) {
	if pane.Active {
		state.paintNewTabPage(canvas, bounds)
		return
	}
	f := state.paneChrome.Begin(canvas, state.Theme(), bounds)
	label := state.messages.LabelNewTab
	f.Column().Fill().Background(state.pal.workspace).Children(func() {
		f.Text(label).FontSize(metrics.FontSizeM).Weight(ui.WeightMedium).Color(state.pal.muted).LineHeight(22).
			Width(canvas.TextWidthWeight(label, metrics.FontSizeM, ui.WeightMedium)+8).MaxWidth(max(bounds.Width-24, 0)).Self(flex.Start).MarginSides(42, 12, 0, 12)
	})
	f.End()
}

func (state *State) paintSessionPage(canvas *ui.Canvas, size ui.Size, session *Session) {
	switch {
	case session.restored:
		state.paintRestoredSessionPage(canvas, size, session)
	case state.showsProgress(session):
		state.paintConnectionPage(canvas, size, session)
	case session.desk != nil:
		state.paintDesktopPage(canvas, size, session)
	default:
		state.paintTerminalPage(canvas, size, session)
	}
}

func (state *State) contextSession() *Session {
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || state.activeSection != sectionTerminal {
		return nil
	}
	return session
}

func (state *State) paintContextPanel(canvas *ui.Canvas, size ui.Size, workspace ui.Rect) {
	context, ok := state.deck.Context(state.deck.ActiveWorkspace())
	if !ok || !context.Open || context.Kind != layoutcore.ContextSFTP {
		return
	}
	session := state.contextSession()
	// A dialog belongs to the window, not to the narrow panel that raised it.
	if session != nil && session.files != nil && session.files.dialog != sftpDialogNone {
		defer state.paintSFTPDialog(canvas, ui.Rect{Width: size.Width, Height: size.Height}, session.files)
	}
	if size.Width < contextDrawerAt {
		drawer := components.BeginDrawer(canvas, state.Theme(), state.baseWorkspacePixelBounds(size), components.DrawerOptions{
			Layer: dialogLayer, Scope: dialogScope, DismissID: hitSFTPClose, CloseID: hitSFTPClose,
			Side: components.DrawerLeft, ContentExtent: min(context.Width, size.Width*0.82),
			InitialFocusID: hitSFTPClose,
		})
		description := ""
		if session != nil {
			description = session.Title
		}
		layout := drawer.Begin(state.messages.SFTPFiles, description)
		state.paintContextSFTP(canvas, layout.Body, session)
		drawer.End()
		return
	}
	base := state.baseWorkspacePixelBounds(size)
	panelWidth := state.contextPanelExtent(base, context.Width)
	maximumPanel := state.contextPanelExtent(base, layoutcore.MaxContextWidth)
	minimumPanel := state.contextPanelExtent(base, layoutcore.MinContextWidth)
	resizable := components.BeginResizable(canvas, state.Theme(), hitContextDivider, base, panelWidth/max(base.Width-1, 1)).
		Limits(minimumPanel/max(base.Width-1, 1), maximumPanel/max(base.Width-1, 1)).
		HandleSize(paneDividerHitWidth)
	layout := resizable.Paint(state.messages.SFTPFiles)
	panel := layout.First
	state.paintContextSFTP(canvas, panel, session)
}

func (state *State) paintContextSFTP(canvas *ui.Canvas, bounds ui.Rect, session *Session) {
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return
	}
	state.contextRect = bounds
	state.contextCanvas.BeginEmbedded(canvas, contextMount)
	if session != nil && session.files != nil {
		state.paintSFTPPage(&state.contextCanvas, ui.Size{Width: bounds.Width, Height: bounds.Height + windowBarHeight}, session)
	} else {
		state.paintEmptyContextSFTP(&state.contextCanvas, ui.Size{Width: bounds.Width, Height: bounds.Height + windowBarHeight}, session)
	}
	state.contextCanvas.EndFrame()
	canvas.Clip(bounds)
	canvas.Embed(&state.contextCanvas, ui.Point{X: bounds.X, Y: bounds.Y - windowBarHeight}, contextMount)
	canvas.Unclip()
	adoptEmbeddedFocus(canvas, &state.contextCanvas, contextMount)
}

// paintEmptyContextSFTP is the panel before a file list exists: the same title
// and close button as the list, then what the panel needs to start.
func (state *State) paintEmptyContextSFTP(canvas *ui.Canvas, size ui.Size, session *Session) {
	pal, messages := state.pal, state.messages
	bounds := ui.Rect{Y: windowBarHeight, Width: size.Width, Height: max(size.Height-windowBarHeight, 0)}
	canConnect := session != nil && (session.isLocal() || session.hasServer() && session.conn != nil && session.phase == sshconn.PhaseConnected)
	// The message sits a little above the middle of the panel.
	middle := bounds.Height * 0.38

	var closeButton, sessionTitle, openButton flex.Box
	buttonWidth := min(max(size.Width-2*sftpPad, 0), 160)
	f := state.filesLayout.Begin(canvas, state.Theme(), bounds)
	f.Stack().Fill().Background(pal.workspace).Children(func() {
		f.Column().Fill().PadX(sftpPad).Children(func() {
			f.Row().Height(infoBarHeight).Shrink(0).Gap(8).Items(flex.Center).Children(func() {
				f.Text(messages.SFTPFiles).FontSize(metrics.FontSizeM).Weight(ui.WeightSemibold).Color(pal.text).LineHeight(20).Grow(1)
				closeButton = f.Slot().Size(sftpHeaderButton, sftpHeaderButton)
			})
			if session != nil {
				// Whose files these would be: the session the panel follows.
				sessionTitle = f.Slot().Height(16).MarginSides(2, sftpHeaderButton+8, 0, 0)
			}
		})
		f.Column().Fill().PadSides(0, sftpPad, 0, sftpPad).Children(func() {
			if canConnect {
				f.Text(messages.SFTPFiles).FontSize(metrics.FontSizeM).Weight(ui.WeightSemibold).Color(pal.text).LineHeight(22).TextCenter().MarginSides(middle-32, 0, 10, 0)
				openButton = f.Slot().Size(buttonWidth, 34).Self(flex.Center)
			} else {
				f.Text(messages.SFTPUnavailable).FontSize(metrics.FontSizeM).Color(pal.muted).LineHeight(24).TextCenter().MarginSides(middle-12, 0, 0, 0)
			}
		})
	})
	f.End()

	state.paintIconButton(canvas, hitSFTPClose, iconClose, closeButton.Rect(), messages.TipSFTPClose, false, iconButtonQuiet, pal.workspace)
	if session != nil {
		titleBounds := sessionTitle.Rect()
		canvas.Label(elide(canvas, session.Title, metrics.FontSizeS, ui.WeightRegular, titleBounds.Width), titleBounds, ui.TextStyle{Size: metrics.FontSizeS, Color: pal.muted})
	}
	if canConnect {
		components.BeginButton(canvas, state.Theme(), hitSFTPOpen, openButton.Rect()).Default(messages.SFTPOpen)
	}
}

func adoptEmbeddedFocus(canvas, child *ui.Canvas, mount ui.Scope) {
	if scope := child.TrappedFocusScope(); !scope.IsZero() {
		canvas.TrapFocus(tree.MountID(mount, scope))
	}
	if focus := child.InitialFocus(); !focus.IsZero() {
		canvas.SetInitialFocus(tree.MountID(mount, focus))
	}
	if target := child.EscapeTarget(); !target.IsZero() {
		canvas.SetEscapeTarget(tree.MountID(mount, target))
	}
}

func (state *State) paintPaneDividers(canvas *ui.Canvas, frame ui.Rect) {
	layout, _ := state.deck.Layout(state.deck.ActiveWorkspace())
	for index := range state.paneSplits {
		split := state.paneSplits[index]
		bounds := pixelBounds(frame, split.Bounds)
		resizable := components.BeginResizable(
			canvas,
			state.Theme(),
			hitPaneDividerList.At(index),
			bounds,
			split.Ratio,
		).HandleSize(paneDividerHitWidth)
		if split.Axis == layoutcore.Vertical {
			resizable = resizable.Vertical()
		}
		if minimum, maximum, ok := paneSplitRatioLimits(layout, split.NodeID, bounds); ok {
			resizable = resizable.Limits(minimum, maximum)
		} else {
			resizable = resizable.Disabled()
		}
		resizable.Paint(state.messages.PaneResize)
	}
}

func (state *State) paneDividerPointer(event ui.PointerEvent) (ui.EventResult, bool) {
	if event.Kind == ui.PointerDown && event.Target.Scope() == hitPaneDividerList {
		if event.Button != ui.PointerButtonPrimary && event.Button != ui.PointerButtonNone {
			return ui.EventResult{}, true
		}
		state.draggedPaneSplit = uint32(state.paneSplits[item(event.Target)].NodeID)
		return state.dragPaneDivider(event), true
	}
	if state.draggedPaneSplit == 0 {
		return ui.EventResult{}, false
	}
	switch event.Kind {
	case ui.PointerMove:
		if event.Captured {
			return state.dragPaneDivider(event), true
		}
	case ui.PointerUp:
		result := state.dragPaneDivider(event)
		state.draggedPaneSplit = 0
		state.save()
		result.Handled = true
		result.Focus = state.activeTerminalFocusHit()
		return result, true
	case ui.PointerExit:
		state.draggedPaneSplit = 0
		return ui.EventResult{Handled: true}, true
	}
	return ui.EventResult{Handled: true}, true
}

func (state *State) contextDividerPointer(event ui.PointerEvent) (ui.EventResult, bool) {
	if event.Kind == ui.PointerDown && event.Target == hitContextDivider {
		if event.Button != ui.PointerButtonPrimary && event.Button != ui.PointerButtonNone {
			return ui.EventResult{}, true
		}
		context, ok := state.deck.Context(state.deck.ActiveWorkspace())
		if !ok || !context.Open || event.Viewport.Width < contextDrawerAt {
			return ui.EventResult{}, true
		}
		state.draggedContextDivider = true
		return state.dragContextDivider(event), true
	}
	if !state.draggedContextDivider {
		return ui.EventResult{}, false
	}
	switch event.Kind {
	case ui.PointerMove:
		if event.Captured {
			return state.dragContextDivider(event), true
		}
	case ui.PointerUp:
		result := state.dragContextDivider(event)
		state.draggedContextDivider = false
		state.save()
		result.Handled = true
		result.Focus = state.activeTerminalFocusHit()
		return result, true
	case ui.PointerExit:
		state.draggedContextDivider = false
		return ui.EventResult{Handled: true, Focus: state.activeTerminalFocusHit()}, true
	}
	return ui.EventResult{Handled: true}, true
}

func (state *State) dragContextDivider(event ui.PointerEvent) ui.EventResult {
	workspaceID := state.deck.ActiveWorkspace()
	context, ok := state.deck.Context(workspaceID)
	if !ok || !context.Open || event.Viewport.Width < contextDrawerAt {
		return ui.EventResult{Handled: true}
	}
	base := state.baseWorkspacePixelBounds(event.Viewport)
	maximumPanel := state.contextPanelExtent(base, layoutcore.MaxContextWidth)
	minimumPanel := state.contextPanelExtent(base, layoutcore.MinContextWidth)
	resizable := components.BeginResizable(nil, state.Theme(), ui.ID{}, base, 0.5).
		Limits(minimumPanel/max(base.Width-1, 1), maximumPanel/max(base.Width-1, 1)).
		HandleSize(paneDividerHitWidth)
	ratio := resizable.FractionAt(event.X)
	width := (base.Width - 1) * ratio
	if width == context.Width {
		return ui.EventResult{Handled: true}
	}
	context.Width = min(max(width, layoutcore.MinContextWidth), layoutcore.MaxContextWidth)
	if state.deck.SetContext(workspaceID, context) != nil {
		return ui.EventResult{Handled: true}
	}
	state.fitWorkspaceTerminals(state.deck.ActiveWorkspace())
	state.fitSFTP(state.viewport)
	return ui.EventResult{Handled: true, Repaint: true}
}

func (state *State) activeTerminalFocusHit() ui.ID {
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || session.term == nil {
		return ui.ID{}
	}
	id := hitTerminalList.At(int(session.ID))
	state.refreshWorkspaceViews()
	if len(state.panes) > 1 {
		id = tree.MountID(paneMount(session), id)
	}
	return id
}

func (state *State) dragPaneDivider(event ui.PointerEvent) ui.EventResult {
	splitID := layoutcore.NodeID(state.draggedPaneSplit)
	var split PaneSplit
	found := false
	for index := range state.paneSplits {
		if state.paneSplits[index].NodeID == splitID {
			split, found = state.paneSplits[index], true
			break
		}
	}
	if !found {
		return ui.EventResult{Handled: true}
	}
	bounds := pixelBounds(state.workspacePixelBounds(event.Viewport), split.Bounds)
	layout, ok := state.deck.Layout(state.deck.ActiveWorkspace())
	minimum, maximum, ok := paneSplitRatioLimits(layout, split.NodeID, bounds)
	if !ok {
		return ui.EventResult{Handled: true}
	}
	resizable := components.BeginResizable(nil, state.Theme(), ui.ID{}, bounds, split.Ratio).
		Limits(minimum, maximum).
		HandleSize(paneDividerHitWidth)
	position := event.X
	if split.Axis == layoutcore.Vertical {
		resizable = resizable.Vertical()
		position = event.Y
	}
	ratio := resizable.FractionAt(position)
	if ratio == split.Ratio || state.SetPaneRatio(split.NodeID, ratio) != nil {
		return ui.EventResult{Handled: true}
	}
	state.refreshWorkspaceViews()
	state.fitWorkspaceTerminals(state.deck.ActiveWorkspace())
	state.fitSFTP(state.viewport)
	return ui.EventResult{Handled: true, Repaint: true}
}

func paneSplitRatioLimits(layout *layoutcore.Layout, splitID layoutcore.NodeID, bounds ui.Rect) (float32, float32, bool) {
	if layout == nil {
		return 0, 0, false
	}
	split, ok := layout.Node(splitID)
	if !ok || split.Kind != layoutcore.KindSplit {
		return 0, 0, false
	}
	extent := bounds.Width
	if split.Axis == layoutcore.Vertical {
		extent = bounds.Height
	}
	usable := extent - 1
	if usable <= 0 {
		return 0, 0, false
	}
	first := minimumPaneExtent(layout, split.First, split.Axis)
	second := minimumPaneExtent(layout, split.Second, split.Axis)
	if first <= 0 || second <= 0 || usable < first+second {
		return 0, 0, false
	}
	minimum := max(layoutcore.MinPaneRatio, first/usable)
	maximum := min(layoutcore.MaxPaneRatio, 1-second/usable)
	return minimum, maximum, minimum <= maximum
}

func minimumPaneExtent(layout *layoutcore.Layout, nodeID layoutcore.NodeID, axis layoutcore.Axis) float32 {
	node, ok := layout.Node(nodeID)
	if !ok {
		return 0
	}
	if node.Kind == layoutcore.KindLeaf {
		if axis == layoutcore.Vertical {
			return minPaneHeight
		}
		return minPaneWidth
	}
	first := minimumPaneExtent(layout, node.First, axis)
	second := minimumPaneExtent(layout, node.Second, axis)
	if node.Axis == axis {
		return first + 1 + second
	}
	return max(first, second)
}

func (state *State) activateRelativePane(delta int) bool {
	state.refreshWorkspaceViews()
	if len(state.panes) < 2 {
		return false
	}
	active := 0
	for index := range state.panes {
		if state.panes[index].SessionID == state.activeSessionID {
			active = index
			break
		}
	}
	next := (active + delta + len(state.panes)) % len(state.panes)
	if !state.activatePane(state.deck.ActiveWorkspace(), state.panes[next].ID) {
		return false
	}
	return true
}

func (state *State) splitActivePane(axis layoutcore.Axis) bool {
	state.refreshWorkspaceViews()
	workspaceID := state.deck.ActiveWorkspace()
	activePane := state.deck.ActivePane(workspaceID)
	if workspaceID == 0 || activePane == 0 || len(state.panes) >= layoutcore.MaxPanes {
		return false
	}
	var active Pane
	found := false
	for index := range state.panes {
		if state.panes[index].ID == activePane {
			active = state.panes[index]
			found = true
			break
		}
	}
	if !found {
		return false
	}
	bounds := pixelBounds(state.workspacePixelBounds(state.viewport), active.Bounds)
	if axis == layoutcore.Horizontal && bounds.Width < minPaneWidth*2+1 ||
		axis == layoutcore.Vertical && bounds.Height < minPaneHeight*2+1 {
		return false
	}
	if _, err := state.deck.Split(workspaceID, activePane, axis); err != nil {
		return false
	}
	state.syncActiveSession()
	state.refreshWorkspaceViews()
	state.fitWorkspaceTerminals(workspaceID)
	state.fitSFTP(state.viewport)
	return true
}

// splitWithCurrentHost splits the active pane and opens the same host in the
// new one: a split is most often a second shell on the machine already at hand.
// From an empty pane there is no host to repeat, so the new pane stays empty and
// offers the host list.
func (state *State) splitWithCurrentHost(axis layoutcore.Axis) {
	var host Host
	repeat := false
	if session, ok := state.sessionByIDPointer(state.activeSessionID); ok && state.activeSection == sectionTerminal && session.host.Address != "" {
		host, repeat = session.host, true
	}
	if !state.splitActivePane(axis) {
		return
	}
	if repeat {
		workspaceID := state.deck.ActiveWorkspace()
		state.pendingWorkspace, state.pendingPane = workspaceID, state.deck.ActivePane(workspaceID)
		state.newHostSession(host)
	}
	state.save()
}

// paneInSplit reports whether the session's pane shares its tab with another.
// Such panes connect behind their terminals: a whole connection card in each of
// several panes is noise, and the shell simply appears when it is ready.
func (state *State) paneInSplit(sessionID uint32) bool {
	workspaceID, _, ok := state.deck.PaneForSession(layoutcore.SessionID(sessionID))
	if !ok {
		return false
	}
	layout, ok := state.deck.Layout(workspaceID)
	return ok && layout.Len() >= 2
}

func (state *State) claimPaneSlot() uint8 {
	for slot := uint8(1); slot <= uint8(maxRestoredSessions); slot++ {
		if state.sessionForPaneSlot(slot) == nil {
			return slot
		}
	}
	return 0
}

func (state *State) ensurePaneSlots() {
	for index := range state.sessions {
		if state.sessions[index].paneSlot == 0 {
			state.sessions[index].paneSlot = state.claimPaneSlot()
		}
	}
}

func (state *State) sessionForPaneSlot(slot uint8) *Session {
	if slot == 0 {
		return nil
	}
	for index := range state.sessions {
		if state.sessions[index].paneSlot == slot {
			return &state.sessions[index]
		}
	}
	return nil
}

// decodePaneHit finds the session whose page painted an identity and the identity
// as that page painted it: the page does not know which pane it is in, so its
// handlers are given the local one. ok is false for the window's own identities
// and for a page that was not mounted (a workspace of one pane paints its page in
// place).
func (state *State) decodePaneHit(hit ui.ID) (*Session, ui.ID, bool) {
	if state.activeSection != sectionTerminal {
		return nil, ui.ID{}, false
	}
	mount, ok := tree.MountOf(hit.Scope())
	if !ok {
		return nil, ui.ID{}, false
	}
	local, ok := tree.Local(mount, hit)
	if !ok {
		return nil, ui.ID{}, false
	}
	if mount == contextMount {
		session, ok := state.sessionByIDPointer(state.activeSessionID)
		return session, local, ok
	}
	if tree.Parent(mount) != panesHits {
		return nil, ui.ID{}, false
	}
	slot, ok := tree.ScopeName(mount).Index()
	if !ok {
		return nil, ui.ID{}, false
	}
	session := state.sessionForPaneSlot(uint8(slot))
	return session, local, session != nil
}

func (state *State) paneForSession(sessionID uint32) (Pane, bool) {
	state.refreshWorkspaceViews()
	for index := range state.panes {
		if state.panes[index].SessionID == sessionID {
			return state.panes[index], true
		}
	}
	return Pane{}, false
}

func (state *State) paneEventGeometry(sessionID uint32, size ui.Size) (ui.Rect, ui.Point, ui.Size, bool) {
	workspaceID, paneID, ok := state.deck.PaneForSession(layoutcore.SessionID(sessionID))
	if !ok || workspaceID != state.deck.ActiveWorkspace() {
		return ui.Rect{}, ui.Point{}, size, false
	}
	fraction, _, ok := state.paneFractionBounds(workspaceID, paneID)
	if !ok {
		return ui.Rect{}, ui.Point{}, size, false
	}
	bounds := pixelBounds(state.workspacePixelBoundsFor(workspaceID, size), fraction)
	return bounds, paneEmbedOrigin(bounds), paneVirtualSize(bounds), true
}

func (state *State) paneFractionBounds(workspaceID layoutcore.WorkspaceID, paneID layoutcore.PaneID) (FractionBounds, int, bool) {
	layout, ok := state.deck.Layout(workspaceID)
	if !ok || layout.Root() == 0 {
		return FractionBounds{}, 0, false
	}
	bounds, found := findPaneFractionBounds(layout, layout.Root(), paneID, FractionBounds{Width: 1, Height: 1})
	return bounds, (layout.Len() + 1) / 2, found
}

func findPaneFractionBounds(layout *layoutcore.Layout, nodeID layoutcore.NodeID, paneID layoutcore.PaneID, bounds FractionBounds) (FractionBounds, bool) {
	node, ok := layout.Node(nodeID)
	if !ok {
		return FractionBounds{}, false
	}
	if node.Kind == layoutcore.KindLeaf {
		return bounds, layoutcore.PaneID(node.Session) == paneID
	}
	first, second := splitFractionBounds(bounds, node.Axis, node.Ratio)
	if found, ok := findPaneFractionBounds(layout, node.First, paneID, first); ok {
		return found, true
	}
	return findPaneFractionBounds(layout, node.Second, paneID, second)
}

// pointInRect reports whether (x, y) lies inside bounds.
func pointInRect(x, y float32, bounds ui.Rect) bool {
	return x >= bounds.X && y >= bounds.Y && x < bounds.X+bounds.Width && y < bounds.Y+bounds.Height
}
