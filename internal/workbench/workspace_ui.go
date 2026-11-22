package workbench

import (
	"github.com/vivaui/viva/gfx"
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

// paneSplitterUI is one pane divider's page Splitter state between paints. The
// deck's ratio is remembered so a layout the deck moves (a preset, a restore)
// is followed, while a drag the page made is not overwritten.
type paneSplitterUI struct {
	node      layoutcore.NodeID
	state     ui.SplitterState
	deckRatio float32
	dragging  bool
}

func (state *State) baseWorkspacePixelBounds(size gfx.Size) gfx.Rect {
	return state.toolbarDockContentBounds(size, true)
}

// Background terminals retain their eventual chrome; section pages only
// reserve the globally available docked actions.
func (state *State) visibleWorkspacePixelBounds(size gfx.Size) gfx.Rect {
	return state.toolbarDockContentBounds(size, state.terminalPage())
}

func (state *State) toolbarDockContentBounds(size gfx.Size, includeTerminal bool) gfx.Rect {
	bounds := state.toolbarDockFrame(size, includeTerminal)
	if includeTerminal {
		// The session manager stands at the left of the terminals' room.
		if extent := state.hostsPanelExtent(bounds.Width); extent > 0 {
			bounds.X += extent
			bounds.Width = max(bounds.Width-extent, 0)
		}
	}
	return bounds
}

// toolbarDockFrame is the content area the docks leave: the room the terminals
// and the panels docked beside them share.
func (state *State) toolbarDockFrame(size gfx.Size, includeTerminal bool) gfx.Rect {
	bounds := state.fullWorkspacePixelBounds(size)
	left := state.toolbarDockSideReserveFor(true, includeTerminal)
	right := state.toolbarDockSideReserveFor(false, includeTerminal)
	bounds.X += left
	bounds.Width = max(bounds.Width-left-right, 0)
	bounds.Height = max(bounds.Height-state.toolbarDockBottomReserve(includeTerminal), 0)
	return bounds
}

func (state *State) fullWorkspacePixelBounds(size gfx.Size) gfx.Rect {
	left := state.verticalTabsReserve(size)
	bounds := gfx.Rect{X: left, Y: windowBarHeight, Width: max(size.Width-left, 0), Height: max(size.Height-windowBarHeight, 0)}
	bounds.Width = max(0, bounds.Width-state.commandsSidebarBounds(size).Width)
	return bounds
}

func (state *State) terminalSideRailBounds(size gfx.Size) (gfx.Rect, bool) {
	if _, ok := state.terminalSideRailSession(); !ok {
		return gfx.Rect{}, false
	}
	bounds := state.toolbarDockSideBounds(size)
	return bounds, true
}

func (state *State) workspacePixelBounds(size gfx.Size) gfx.Rect {
	return state.workspacePixelBoundsFor(state.deck.ActiveWorkspace(), size)
}

func (state *State) workspacePixelBoundsFor(workspaceID layoutcore.WorkspaceID, size gfx.Size) gfx.Rect {
	bounds := state.workspaceBoundsAboveLogs(workspaceID, size)
	if view := state.workspaceRemoteLogs(workspaceID); view != nil {
		bounds.Height -= view.extent(bounds.Height)
	}
	return bounds
}

func (state *State) workspaceBoundsAboveLogs(workspaceID layoutcore.WorkspaceID, size gfx.Size) gfx.Rect {
	bounds := state.baseWorkspacePixelBounds(size)
	context, ok := state.deck.Context(workspaceID)
	if ok && context.Open && size.Width >= contextDrawerAt {
		panel := state.contextPanelWidth(bounds, context) + 1
		if context.Side == layoutcore.ContextLeft {
			bounds.X += panel
		}
		bounds.Width = max(0, bounds.Width-panel)
	}
	// The assistant's panel sits on the right, so the panes end before it.
	monitor := state.independentMonitorReserve(workspaceID, size)
	if context.MonitorLeft {
		bounds.X += monitor
	}
	bounds.Width = max(0, bounds.Width-monitor)
	bounds.Width = max(0, bounds.Width-state.aiReserve(workspaceID, size))
	if state.notesPanelOpenFor(workspaceID) {
		bounds.Width = max(0, bounds.Width-state.notesPanelWidthFor(workspaceID, size))
	}
	if state.workspaceManaged(workspaceID) {
		reserve := workspaceHeaderHeight
		if state.workspaceTabbed(workspaceID) {
			reserve += workspacePaneTabsHeight
		}
		bounds.Y += reserve
		bounds.Height = max(0, bounds.Height-reserve)
	}
	return bounds
}

func (state *State) contextPanelExtent(workspace gfx.Rect, preferred float32) float32 {
	maximum := min(layoutcore.MaxContextWidth, workspace.Width*0.45)
	minimum := min(layoutcore.MinContextWidth, maximum)
	return min(max(preferred, minimum), maximum)
}

func (state *State) contextPanelWidth(workspace gfx.Rect, context layoutcore.ContextState) float32 {
	return state.contextPanelExtent(workspace, context.Width)
}

func pixelBounds(frame gfx.Rect, bounds FractionBounds) gfx.Rect {
	return gfx.Rect{
		X:      frame.X + bounds.X*frame.Width,
		Y:      frame.Y + bounds.Y*frame.Height,
		Width:  bounds.Width * frame.Width,
		Height: bounds.Height * frame.Height,
	}
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

// paintsTerminalPane reports whether a session is shown by the terminal pane
// view: a live terminal that is neither waiting on its connection page nor a
// remote desktop. Every other visible session pane declares a connection,
// restored or desktop body in the same workbench page.
func (state *State) paintsTerminalPane(session *Session) bool {
	return session.term != nil && !session.restored && !state.showsProgress(session) && session.desk == nil
}

// declareWorkspacePanes declares the terminal panes inside the workbench page:
// one Part-named container per visible pane at its deck rectangle. The page
// names them, so focus, keys, text, wheel and hits all use the page's routing;
// the window bar, pane tabs, dividers and the docked SFTP panel stay outside.
func (state *State) declareWorkspacePanes(c ui.C) {
	state.refreshWorkspaceViews()
	for index := range state.sessionBodies {
		state.sessionBodies[index].declared = false
	}
	frame := state.workspacePixelBounds(state.viewport)
	state.declareWorkspaceHeader(c, state.viewport)
	state.declareWorkspacePaneTabs(c, frame)
	c.Part(terminalWorkspacePart)
	ui.Stack(c).Absolute().Left(frame.X).Top(frame.Y).Width(frame.Width).Height(frame.Height).Clip().Children(func() {
		if len(state.panes) == 0 {
			// No pane at all: the whole area is the empty tab.
			state.declareNewTabPage(c, gfx.Rect{Width: frame.Width, Height: frame.Height})
			return
		}
		for index := range state.panes {
			pane := state.panes[index]
			if pane.Bounds.Width <= 0 || pane.Bounds.Height <= 0 {
				continue // a tab that is not showing
			}
			bounds := pixelBounds(frame, pane.Bounds)
			session, ok := state.sessionByIDPointer(pane.SessionID)
			if !ok {
				// The page owns the empty pane: the active one offers the empty
				// tab, a background one only its label.
				state.declareEmptyPane(c, frame, bounds, index, pane.Active)
				continue
			}
			// The container itself is the pane's ground: a press here is a press
			// inside the pane. What the pane shows is declared below it, in the
			// same page and under the same stable part name, so focus, keys, text,
			// wheel and hits all use the page's routing.
			part := state.panePartFor(session)
			c.Part(part)
			ui.Column(c).Absolute().
				Left(bounds.X-frame.X).Top(bounds.Y-frame.Y).
				Width(bounds.Width).Height(bounds.Height).
				HitWith(hitPaneActivateList.At(index), state.messages.LabelNewTab, gfx.HitOptions{Role: gfx.RoleButton, SkipFocus: true}).
				Clip().Children(func() {
				if state.paintsTerminalPane(session) {
					state.declareTerminalPane(c, session, bounds)
					return
				}
				state.declareSessionPane(c, session, bounds)
			})
		}
		state.declarePaneSplitters(c, frame)
	})
}

// declareEmptyPane declares the page's content of a pane with no session: the
// empty tab for the active one, its label for a background one. The pane itself
// is the ground a press switches panes with.
func (state *State) declareEmptyPane(c ui.C, frame, bounds gfx.Rect, index int, active bool) {
	pane := ui.Column(c).Absolute().Left(bounds.X-frame.X).Top(bounds.Y-frame.Y).Width(bounds.Width).Height(bounds.Height).
		HitWith(hitPaneActivateList.At(index), state.messages.LabelNewTab, gfx.HitOptions{Role: gfx.RoleButton, SkipFocus: true})
	if !active {
		label := state.messages.LabelNewTab
		pane.Background(state.pal.workspace).Children(func() {
			ui.Text(c, label).FontSize(metrics.FontSizeM).Weight(gfx.WeightMedium).Color(state.pal.muted).LineHeight(22).
				Width(c.Canvas().TextWidthWeight(label, metrics.FontSizeM, gfx.WeightMedium)+8).
				MaxWidth(max(bounds.Width-24, 0)).Self(ui.Start).MarginSides(42, 12, 0, 12)
		})
		return
	}
	pane.Children(func() { state.declareNewTabPage(c, gfx.Rect{Width: bounds.Width, Height: bounds.Height}) })
}

func (state *State) paintWorkspacePanes(canvas *gfx.Canvas, size gfx.Size) {
	state.refreshWorkspaceViews()
	if !state.workspaceHeaderFocusPending.IsZero() {
		canvas.RequestFocus(state.workspaceHeaderFocusPending)
		state.workspaceHeaderFocusPending = gfx.ID{}
	}
	state.workspacePaneTools.count = 0
	frame := state.workspacePixelBounds(size)
	headerOpen := state.paintWorkspaceHeader(canvas, size)
	if !headerOpen {
		state.workspaceHeaderMenu = workspaceHeaderMenuNone
	}
	if len(state.panes) == 0 {
		state.paintEmptyWorkspace(canvas, frame)
		state.paintContextPanel(canvas, size, frame)
		return
	}
	for index := range state.panes {
		pane := state.panes[index]
		if pane.Bounds.Width <= 0 || pane.Bounds.Height <= 0 {
			continue // a tab that is not showing
		}
		bounds := pixelBounds(frame, pane.Bounds)
		session, ok := state.sessionByIDPointer(pane.SessionID)
		if ok && state.paintsTerminalPane(session) {
			// The page already painted the pane's chrome and terminal; what is
			// left are the pieces it paints itself (find surface, completion,
			// notice, spinner and status light).
			state.paintTerminalPaneDetails(canvas, session, bounds)
			continue
		}
		if !ok {
			// The page owns the empty pane: only the active one paints what its
			// declaration left by hand.
			if pane.Active {
				state.paintNewTabDetails(canvas)
			}
			continue
		}
		// The pane's body is part of the workbench page now; the window paints
		// only what its declaration left by hand.
		canvas.Clip(bounds)
		state.paintSessionPaneDetails(canvas, session, bounds)
		canvas.Unclip()
	}
	if !state.workspaceTabbed(state.deck.ActiveWorkspace()) {
		state.paintActivePaneGlow(canvas, frame, size)
	}
	state.paintTerminalDrop(canvas)
	state.paintContextPanel(canvas, size, frame)
}

// windowCornerRadius is the radius the window's own content area is clipped to
// at its bottom corners; a pane that touches one has to follow it.
const windowCornerRadius = float32(10)

// The two arcs that carry the rim round a window corner, as one-pixel strokes
// whose centerline runs half a pixel inside the pane's edge. Built once: the rim
// is painted every frame and must not allocate.
var rimCornerRight, rimCornerLeft = func() (right, left gfx.Path) {
	const r, k = windowCornerRadius, 0.5522847
	const c = r - 0.5 // radius of the centerline
	// Right corner, box r x r with the circle's center at its top-left.
	right = gfx.NewPath(r, r, gfx.MoveTo(c, 0), gfx.CubicTo(c, c*k, c*k, c, 0, c))
	// Left corner, box r x r with the circle's center at its top-right.
	left = gfx.NewPath(r, r, gfx.MoveTo(r, c), gfx.CubicTo(r-c*k, c, r-c, c*k, r-c, 0))
	return right, left
}()

// activePaneRimColor is the rim as one opaque color: the accent a third of the
// way into the terminal's background. Translucent, the same line came out
// different on every side, because each side lies over something else (the
// window bar, an info bar, a divider).
func (state *State) activePaneRimColor() gfx.Color {
	const amount = 0.34
	pal := state.pal
	mix := func(from, to float32) float32 { return from + (to-from)*amount }
	return gfx.Color{
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
func (state *State) paintActivePaneGlow(canvas *gfx.Canvas, frame gfx.Rect, window gfx.Size) {
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
		canvas.Fill(gfx.Rect{X: bounds.X, Y: bounds.Y, Width: bounds.Width, Height: width}, line)
		canvas.Fill(gfx.Rect{X: bounds.X + leftInset - overlap, Y: bottom - width, Width: max(bounds.Width-leftInset-rightInset+2*overlap, 0), Height: width}, line)
		sideHeight := func(inset float32) float32 {
			if inset == 0 {
				return bounds.Height - 2*width // from the top edge to the bottom edge
			}
			return bounds.Height - width - inset + overlap // from the top edge to the arc
		}
		canvas.Fill(gfx.Rect{X: bounds.X, Y: bounds.Y + width, Width: width, Height: max(sideHeight(leftInset), 0)}, line)
		canvas.Fill(gfx.Rect{X: right - width, Y: bounds.Y + width, Width: width, Height: max(sideHeight(rightInset), 0)}, line)
		style := gfx.PathStyle{Stroke: line, StrokeWidth: width}
		if rightInset > 0 {
			canvas.DrawPath(rimCornerRight, gfx.Rect{X: right - rightInset, Y: bottom - rightInset, Width: rightInset, Height: rightInset}, style)
		}
		if leftInset > 0 {
			canvas.DrawPath(rimCornerLeft, gfx.Rect{X: bounds.X, Y: bottom - leftInset, Width: leftInset, Height: leftInset}, style)
		}
		return
	}
}

// snapRect rounds every edge of r to a whole logical pixel.
func snapRect(r gfx.Rect) gfx.Rect {
	left, top := float32(math.Round(float64(r.X))), float32(math.Round(float64(r.Y)))
	right, bottom := float32(math.Round(float64(r.X+r.Width))), float32(math.Round(float64(r.Y+r.Height)))
	return gfx.Rect{X: left, Y: top, Width: right - left, Height: bottom - top}
}

func (state *State) paintEmptyWorkspace(canvas *gfx.Canvas, bounds gfx.Rect) {
	// The page declared the empty tab; only its badges and footer are painted by
	// hand.
	state.paintNewTabDetails(canvas)
}

func (state *State) contextSession() *Session {
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || state.activeSection != sectionTerminal {
		return nil
	}
	return session
}

// The docked files panel is a component of the workbench page: one Part-named
// container at the rect the window shows it, with the panel's view declared
// inside. Its List and controls are the page's, so pointer, key, wheel and text
// routing need no mount translation. When the window is too narrow to dock it,
// the same view is declared in a drawer overlay the page paints itself.
const contextPanelPart = "sftp-panel"

// contextPanelMode is the view the docked panel declared in the last paint.
type contextPanelMode uint8

const (
	contextPanelNone contextPanelMode = iota
	contextPanelEmpty
	contextPanelList
	contextPanelEditor
)

// contextPanelView picks the view the panel shows for a session.
func contextPanelView(session *Session) contextPanelMode {
	switch {
	case session == nil || session.files == nil:
		return contextPanelEmpty
	case session.files.editPath != "":
		return contextPanelEditor
	default:
		return contextPanelList
	}
}

// declareContextPanel declares the docked files panel in the workbench page.
// The panel's hand-painted parts keep their window identities; its List and the
// page-declared controls are named under contextPanelScope, which event routing
// reads to tell the panel's hits from the window's.
func (state *State) declareContextPanel(c ui.C) {
	state.contextRect = gfx.Rect{}
	state.contextPanelScope = 0
	state.contextPanelMode = contextPanelNone
	state.contextPanelDocked = false
	state.contextDrawerOpen = false
	size := state.viewport
	context, ok := state.deck.Context(state.deck.ActiveWorkspace())
	if !ok || !context.Open || state.monitorDrawer(size) || context.Kind != layoutcore.ContextSFTP {
		return
	}
	session := state.contextSession()
	if size.Width < contextDrawerAt {
		// A narrow window shows the panel as a drawer over the workspace. The
		// page declares its chrome, its body and the panel between them, so the
		// panel paints after the surface and inside its focus and layer.
		viewport := state.baseWorkspacePixelBounds(size)
		description := ""
		if session != nil {
			description = session.Title
		}
		side := ui.DrawerLeft
		if context.Side == layoutcore.ContextRight {
			side = ui.DrawerRight
		}
		extent := min(context.Width, size.Width*0.82)
		body := drawerPanelBody(c, viewport, side, extent, description)
		state.contextDrawerOpen = true
		closed := ui.Drawer(c, &state.contextDrawerOpen, state.messages.SFTPFiles, ui.DrawerSpec{
			Side: side, Size: extent, Description: description,
		}, func() {
			state.declareContextPanelBody(c, body, gfx.Point{X: body.X, Y: body.Y}, session)
		}, nil)
		if closed || !state.contextDrawerOpen {
			c.After(func() {
				if session != nil {
					state.hideFilesPanel(session)
					return
				}
				workspaceID := state.deck.ActiveWorkspace()
				context, _ := state.deck.Context(workspaceID)
				context.Open = false
				_ = state.deck.SetContext(workspaceID, context)
			})
			return
		}
		state.contextPanelScope = 0
		if session != nil && session.files != nil && session.files.dialog != sftpDialogNone {
			state.declareSFTPDialog(c, gfx.Rect{Width: size.Width, Height: size.Height}, session.files)
		}
		return
	}
	base, width, _, _ := state.contextPanelLayout(size, context)
	panel, _ := contextPanelRect(base, width, context.Side)
	state.contextPanelDocked = true
	state.declareContextPanelBody(c, panel, gfx.Point{}, session)
	state.contextPanelScope = tree.Sub(tree.Feature(shellPageRoot), tree.Name(contextPanelPart))
	if session != nil && session.files != nil && session.files.dialog != sftpDialogNone {
		state.declareSFTPDialog(c, gfx.Rect{Width: size.Width, Height: size.Height}, session.files)
	}
}

// declareContextPanelBody declares the panel's container at rect (offset from
// the container it is declared in) and the view the session shows.
func (state *State) declareContextPanelBody(c ui.C, rect gfx.Rect, origin gfx.Point, session *Session) {
	if rect.Width <= 0 || rect.Height <= 0 {
		return
	}
	mode := contextPanelView(session)
	state.contextRect = rect
	state.contextPanelMode = mode
	chrome := &state.chrome
	chrome.sftpPanel = sftpPanelScratch{bounds: rect}
	chrome.sftpEditor = sftpEditorScratch{}
	chrome.sftpEmpty = sftpEmptyScratch{bounds: rect}
	switch mode {
	case contextPanelList:
		view := session.files
		if folder := view.active(); folder.pathEditing && !state.sftpPathInput && c.Canvas().Focused() != hitSFTPPath {
			folder.pathEditing = false
			folder.resetPathInput()
		}
		chrome.sftpPanel.view, chrome.sftpPanel.session = view, session
	case contextPanelEditor:
		chrome.sftpEditor.view = session.files
	default:
		chrome.sftpEmpty.session = session
	}
	c.Part(contextPanelPart)
	panel := ui.Column(c).Absolute().
		Left(rect.X - origin.X).Top(rect.Y - origin.Y).
		Width(rect.Width).Height(rect.Height).Clip()
	if state.contextPanelDraw == nil {
		state.contextPanelDraw = state.drawContextPanelBody
	}
	panel.DrawOver(state.contextPanelDraw)
	panel.Children(func() {
		switch mode {
		case contextPanelList:
			state.declareSFTPPanel(c)
		case contextPanelEditor:
			state.declareSFTPEditor(c)
		default:
			state.declareEmptyContextSFTP(c)
		}
	})
}

// paintContextPanelBody paints what the panel's view lays out but does not
// paint itself: the icon buttons, the path and filter, the list's scroll bar and
// the empty message. It runs inside the panel container, after its children, so
// it paints in the page's order and with the panel's clip.
func (state *State) drawContextPanelBody(d *ui.DrawContext) {
	state.paintContextPanelBody(d.Canvas)
	if !state.contextPanelDocked {
		return
	}
	if context, ok := state.deck.Context(state.deck.ActiveWorkspace()); ok {
		state.paintContextPanelOuterEdge(d.Canvas, state.viewport, state.contextRect, context)
	}
}

// drawerPanelBody is the body rectangle ui.Drawer gives a side drawer, worked
// out before the page declares the panel inside it: the drawer's content inset
// by its padding, below the title and description and above its bottom edge.
// It follows components.Drawer's layout, the one the page paints.
func drawerPanelBody(c ui.C, viewport gfx.Rect, side ui.DrawerSide, extent float32, description string) gfx.Rect {
	metrics := c.Theme().Metrics
	width := min(extent, viewport.Width)
	if width <= 0 {
		return gfx.Rect{}
	}
	x := viewport.X
	if side == ui.DrawerRight {
		x = viewport.X + viewport.Width - width
	}
	content := gfx.Rect{X: x, Y: viewport.Y, Width: width, Height: viewport.Height}
	inner := content.Inset(gfx.All(metrics.PadSurface))
	lineHeight := metrics.FontSizeM + 7
	header := lineHeight
	if description != "" {
		var scratch [3]string
		lines := c.Canvas().WrapLines(scratch[:0], description, metrics.FontSizeM, gfx.WeightRegular, inner.Width, 3)
		header += 6 + float32(len(lines))*lineHeight
	}
	body := gfx.Rect{X: inner.X, Y: inner.Y + header + metrics.GapItem, Width: inner.Width}
	body.Height = max(inner.Y+inner.Height-body.Y, 0)
	return body
}

func (state *State) paintContextPanelBody(canvas *gfx.Canvas) {
	switch state.contextPanelMode {
	case contextPanelList:
		state.paintSFTPPanelDetails(canvas)
	case contextPanelEditor:
		state.paintSFTPEditorDetails(canvas)
	case contextPanelEmpty:
		state.paintEmptyContextPanelDetails(canvas)
	}
}

// paintPanelSideEdge draws a docked panel's 1px boundary on the side where a
// side rail of the window frame stands. The frame takes the panel's own
// workspace surface, so without the line the panel and the rail read as one
// column; the pane divider draws the same line on the panel's other side, and an
// edge that ends at the window's own boundary keeps the window's edge.
func (state *State) paintPanelSideEdge(canvas *gfx.Canvas, rect gfx.Rect, left bool) {
	if canvas == nil || rect.Width <= 0 || rect.Height <= 0 {
		return
	}
	x := rect.X + rect.Width - 1
	if left {
		x = rect.X
	}
	canvas.Fill(gfx.Rect{X: x, Y: rect.Y, Width: 1, Height: rect.Height}, state.pal.line)
}

// paintContextPanelOuterEdge draws the docked files panel's outside edge where a
// side rail stands beside it. The assistant keeps the right edge for itself when
// it is docked there, and its divider is the panel's line on that side.
func (state *State) paintContextPanelOuterEdge(canvas *gfx.Canvas, size gfx.Size, rect gfx.Rect, context layoutcore.ContextState) {
	switch context.Side {
	case layoutcore.ContextLeft:
		if state.toolbarDockSideReserveFor(true, true) > 0 {
			state.paintPanelSideEdge(canvas, rect, true)
		}
	case layoutcore.ContextRight:
		if state.aiReserve(state.deck.ActiveWorkspace(), size) == 0 && state.toolbarDockSideReserveFor(false, true) > 0 {
			state.paintPanelSideEdge(canvas, rect, false)
		}
	}
}

// contextPanelOwns reports whether id is an element the workbench page declared
// inside the docked files panel: its List, a row, or a control of the panel's
// view. Such an element is routed through the page, with no mount.
func (state *State) contextPanelOwns(id gfx.ID) bool {
	scope := state.contextPanelScope
	return !scope.IsZero() && !id.IsZero() && tree.Within(id.Scope(), scope)
}

// contextPanelHit reports whether id belongs to the docked files panel as the
// window sees it: a page element, or one of the hand-painted controls that keep
// the sftp feature's names. The panel always shows the active session.
func (state *State) contextPanelHit(id gfx.ID) bool {
	if id.IsZero() {
		return false
	}
	return state.contextPanelOwns(id) || tree.Within(id.Scope(), sftpHits)
}

// contextPanelSession resolves the active session for an identity of the docked
// files panel. The panel is painted only for the terminal section.
func (state *State) contextPanelSession(id gfx.ID) (*Session, bool) {
	if state.activeSection != sectionTerminal || !state.contextPanelHit(id) {
		return nil, false
	}
	return state.sessionByIDPointer(state.activeSessionID)
}

// paintContextPanel paints what belongs to the window rather than the panel: a
// dialog it raised. The panel itself is part of the workbench page.
func (state *State) paintContextPanel(canvas *gfx.Canvas, size gfx.Size, workspace gfx.Rect) {
	context, ok := state.deck.Context(state.deck.ActiveWorkspace())
	if !ok || !context.Open || state.monitorDrawer(size) {
		return
	}
	if context.Kind != layoutcore.ContextSFTP {
		return
	}
	if size.Width >= contextDrawerAt {
		// The divider between the panel and the workspace is the window's, not
		// the panel's: paint its line and its drag target here, after the page.
		base, width, _, _ := state.contextPanelLayout(size, context)
		_, handleX := contextPanelRect(base, width, context.Side)
		thickness, color := float32(1), state.pal.line
		if canvas.Pressed() == hitContextDivider {
			thickness, color = 3, state.pal.accent
		} else if canvas.Hovered() == hitContextDivider {
			thickness, color = 3, state.pal.accent
		}
		line := gfx.Rect{X: handleX + (1-thickness)/2, Y: base.Y, Width: thickness, Height: base.Height}
		if thickness > 1 {
			canvas.Rounded(line, 1, color)
		} else {
			canvas.Fill(line, color)
		}
		hit := gfx.Rect{X: handleX - (paneDividerHitWidth-1)/2, Y: base.Y, Width: paneDividerHitWidth, Height: base.Height}
		canvas.HitWithOptions(hitContextDivider, hit, state.messages.SFTPFiles, gfx.HitOptions{
			Role: gfx.RoleSeparator, Cursor: gfx.PointerCursorResizeHorizontal,
		})
	}
}

// paintEmptyContextPanelDetails paints the panel before a file list exists: the
// same title and close button as the list, then what the panel needs to start.
func (state *State) paintEmptyContextPanelDetails(canvas *gfx.Canvas) {
	s := &state.chrome.sftpEmpty
	pal := state.pal
	headerBounds := s.header.Rect()
	canvas.Fill(gfx.Rect{X: headerBounds.X, Y: headerBounds.Y + headerBounds.Height - 1, Width: headerBounds.Width, Height: 1}, pal.line)
	if s.session != nil {
		titleBounds := s.sessionTitle.Rect()
		canvas.Label(elide(canvas, s.session.Title, metrics.FontSizeS, gfx.WeightRegular, titleBounds.Width), titleBounds, gfx.TextStyle{Size: metrics.FontSizeS, Color: pal.muted})
	}
}

// declareEmptyContextSFTP declares the panel's chrome and the one action it
// offers while there is no list yet.
func (state *State) declareEmptyContextSFTP(c ui.C) {
	s := &state.chrome.sftpEmpty
	session := s.session
	pal, messages := state.pal, state.messages
	bounds := s.bounds
	s.canConnect = session != nil && (session.isLocal() || session.hasServer() && session.conn != nil && session.phase == sshconn.PhaseConnected)
	// The message sits a little above the middle of the panel.
	middle := bounds.Height * 0.38
	buttonWidth := min(max(bounds.Width-2*sftpPad, 0), 160)

	ui.Stack(c).Fill().Background(pal.workspace).Children(func() {
		ui.Column(c).Fill().PadX(sftpPad).Children(func() {
			s.header = ui.Row(c).FillWidth().Height(infoBarHeight).Shrink(0).MarginSides(0, -sftpPad, 0, -sftpPad).PadX(sftpPad).Background(pal.workspace).Gap(8).Items(ui.Center)
			s.header.Children(func() {
				ui.Text(c, messages.SFTPFiles).FontSize(metrics.FontSizeM).Weight(gfx.WeightSemibold).Color(pal.text).LineHeight(20).Grow(1)
				moveIcon, moveHint := state.sftpPanelMoveAction()
				s.sideButton = state.declareIconButton(c, hitSFTPPanelSide, moveIcon, moveHint, false, iconButtonQuiet).Size(sftpHeaderButton, sftpHeaderButton)
				s.closeButton = state.declareIconButton(c, hitSFTPClose, iconClose, messages.TipSFTPClose, false, iconButtonQuiet).Size(sftpHeaderButton, sftpHeaderButton)
			})
			if session != nil {
				// Whose files these would be: the session the panel follows.
				s.sessionTitle = ui.Slot(c).Height(16).MarginSides(2, sftpHeaderButton+8, 0, 0)
			}
		})
		ui.Column(c).Fill().PadSides(0, sftpPad, 0, sftpPad).Children(func() {
			if s.canConnect {
				ui.Text(c, messages.SFTPFiles).FontSize(metrics.FontSizeM).Weight(gfx.WeightSemibold).Color(pal.text).LineHeight(22).TextCenter().MarginSides(middle-32, 0, 10, 0)
				s.openButton = ui.Button(c, messages.SFTPOpen).Hit(hitSFTPOpen, messages.SFTPOpen).Size(buttonWidth, 34).Self(ui.Center)
			} else {
				ui.Text(c, messages.SFTPUnavailable).FontSize(metrics.FontSizeM).Color(pal.muted).LineHeight(24).TextCenter().MarginSides(middle-12, 0, 0, 0)
			}
		})
	})
}

// declarePaneSplitters declares the dividers between the panes of the active
// split workspace. The deck lays the panes out as fractions of the frame, so
// each divider is a page Splitter between two empty slots at the split's own
// rectangle: the drag, the arrows and the pane minimums are the page's, and the
// deck takes the ratio the paint settles on.
func (state *State) declarePaneSplitters(c ui.C, frame gfx.Rect) {
	if state.workspaceTabbed(state.deck.ActiveWorkspace()) {
		state.paneSplitters = state.paneSplitters[:0]
		return
	}
	layout, _ := state.deck.Layout(state.deck.ActiveWorkspace())
	used := 0
	for index := range state.paneSplits {
		split := state.paneSplits[index]
		bounds := pixelBounds(frame, split.Bounds)
		if bounds.Width <= 0 || bounds.Height <= 0 {
			continue
		}
		entry := state.paneSplitterAt(used, split)
		used++
		if layout != nil {
			if node, ok := layout.Node(split.NodeID); ok && node.Kind == layoutcore.KindSplit {
				first := minimumPaneExtent(layout, node.First, node.Axis)
				second := minimumPaneExtent(layout, node.Second, node.Axis)
				if first <= 0 || second <= 0 {
					continue
				}
				state.declarePaneSplitter(c, frame, bounds, split, entry, first, second)
				continue
			}
		}
		// Without a limit the split has nothing usable to drag.
		entry.state.Pos = 0
	}
	state.paneSplitters = state.paneSplitters[:used]
}

// paneSplitterAt returns the SplitterState kept for a split, reusing the entry
// the same split had in the previous paint so a drag survives the frame.
func (state *State) paneSplitterAt(index int, split PaneSplit) *paneSplitterUI {
	if index < len(state.paneSplitters) {
		entry := &state.paneSplitters[index]
		if entry.node != split.NodeID {
			*entry = paneSplitterUI{node: split.NodeID, deckRatio: split.Ratio}
			entry.state.Ratio = split.Ratio
		} else if entry.deckRatio != split.Ratio {
			// The deck moved the split (a preset, a restored layout); follow it.
			entry.state.Ratio, entry.deckRatio = split.Ratio, split.Ratio
		}
		return entry
	}
	entry := paneSplitterUI{node: split.NodeID, deckRatio: split.Ratio}
	entry.state.Ratio = split.Ratio
	state.paneSplitters = append(state.paneSplitters, entry)
	return &state.paneSplitters[len(state.paneSplitters)-1]
}

func (state *State) declarePaneSplitter(c ui.C, frame, bounds gfx.Rect, split PaneSplit, entry *paneSplitterUI, minFirst, minSecond float32) {
	container := ui.Row(c)
	if split.Axis == layoutcore.Vertical {
		container = ui.Column(c)
	}
	container.Absolute().
		Left(bounds.X - frame.X).Top(bounds.Y - frame.Y).
		Width(bounds.Width).Height(bounds.Height).Children(func() {
		ui.Slot(c).Width(entry.state.Pos).Shrink(0)
		ui.Splitter(c, &entry.state).SplitterThickness(1).Mins(minFirst, minSecond).
			Hit(hitPaneDividerList.At(splitterIndex(state.paneSplits, split.NodeID)), state.messages.PaneResize)
		ui.Slot(c).Grow(1)
	})
	// A drag is handled by the Splitter while the page paints, after this
	// declaration; the sync therefore reads the state once the paint has
	// settled it, so the deck takes the last move and not the one before it.
	if entry.state.Dragging() || entry.dragging {
		node, splitter := split.NodeID, entry
		c.After(func() {
			if splitter.state.Dragging() {
				splitter.deckRatio, splitter.dragging = splitter.state.Ratio, true
				state.applyPaneSplitRatio(node, splitter.state.Ratio)
				return
			}
			if splitter.dragging {
				state.save()
				// The drag took the keyboard: give it back to the terminal the
				// workspace was typing in, as the old divider did on release.
				if id := state.activeTerminalFocusHit(); !id.IsZero() {
					c.Canvas().RequestFocus(id)
				}
			}
			splitter.dragging = false
		})
	}
}

func splitterIndex(splits []PaneSplit, node layoutcore.NodeID) int {
	for index := range splits {
		if splits[index].NodeID == node {
			return index
		}
	}
	return 0
}

// applyPaneSplitRatio moves the deck's split to the ratio the page Splitter
// settled on, then refits the terminals and the files panel round the panes.
func (state *State) applyPaneSplitRatio(node layoutcore.NodeID, ratio float32) {
	if state.SetPaneRatio(node, ratio) != nil {
		return
	}
	state.refreshWorkspaceViews()
	state.fitWorkspaceTerminals(state.deck.ActiveWorkspace())
	state.fitSFTP(state.viewport)
}

func (state *State) contextDividerPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	if event.Kind == gfx.PointerDown && event.Target == hitContextDivider {
		if event.Button != gfx.PointerButtonPrimary && event.Button != gfx.PointerButtonNone {
			return gfx.EventResult{}, true
		}
		context, ok := state.deck.Context(state.deck.ActiveWorkspace())
		if !ok || !context.Open || event.Viewport.Width < contextDrawerAt {
			return gfx.EventResult{}, true
		}
		state.draggedContextDivider = true
		return state.dragContextDivider(event), true
	}
	if !state.draggedContextDivider {
		return gfx.EventResult{}, false
	}
	switch event.Kind {
	case gfx.PointerMove:
		if event.Captured {
			return state.dragContextDivider(event), true
		}
	case gfx.PointerUp:
		result := state.dragContextDivider(event)
		state.draggedContextDivider = false
		state.save()
		result.Handled = true
		result.Focus = state.activeTerminalFocusHit()
		return result, true
	case gfx.PointerExit:
		state.draggedContextDivider = false
		return gfx.EventResult{Handled: true, Focus: state.activeTerminalFocusHit()}, true
	}
	return gfx.EventResult{Handled: true}, true
}

func (state *State) dragContextDivider(event gfx.PointerEvent) gfx.EventResult {
	workspaceID := state.deck.ActiveWorkspace()
	context, ok := state.deck.Context(workspaceID)
	if !ok || !context.Open || event.Viewport.Width < contextDrawerAt {
		return gfx.EventResult{Handled: true}
	}
	base, _, minimum, maximum := state.contextPanelLayout(event.Viewport, context)
	width := event.X - base.X
	if context.Side == layoutcore.ContextRight {
		width = base.X + base.Width - 1 - event.X
	}
	width = min(max(width, minimum), maximum)
	if width == context.Width {
		return gfx.EventResult{Handled: true}
	}
	context.Width = min(max(width, layoutcore.MinContextWidth), layoutcore.MaxContextWidth)
	state.sftpPanelWidth = context.Width
	if state.deck.SetContext(workspaceID, context) != nil {
		return gfx.EventResult{Handled: true}
	}
	state.refreshWorkspaceViews()
	state.fitWorkspaceTerminals(state.deck.ActiveWorkspace())
	state.fitSFTP(state.viewport)
	return gfx.EventResult{Handled: true, Repaint: true}
}

// paneGroundFocus is where the keyboard goes when a pane's own ground — its bar,
// its padding, the page behind its controls — is pressed: the surface the page
// types into, or the one action of a page that has no terminal. Zero when the
// page has nowhere to put the keyboard.
//
// The identity is the one the page paints, before the pane's mount: a page does
// not know which pane it is in.
func (state *State) paneGroundFocus(session *Session) gfx.ID {
	switch {
	case session.restored:
		return hitConnRetry // the page's one action, the key it shows
	case state.showsProgress(session):
		return gfx.ID{}
	case session.desk != nil:
		return hitTerminalList.At(int(session.ID))
	case session.term != nil:
		session.term.pageIdentity(session.ID)
		return session.term.controlID
	}
	return gfx.ID{}
}

// paneGround finds the pane whose ground a hit is, the area a page leaves open
// between its controls. It is where a press switches panes. ok is false for
// everything a page or the window declared itself.
func (state *State) paneGround(hit gfx.ID) (*Session, bool) {
	if hit.Scope() != hitPaneActivateList || state.activeSection != sectionTerminal {
		return nil, false
	}
	state.refreshWorkspaceViews()
	index := item(hit)
	if index >= len(state.panes) {
		return nil, false
	}
	return state.sessionByIDPointer(state.panes[index].SessionID)
}

// paneIdentity names an identity a session's page painted as the pane paints it.
// The terminal pane is part of the workbench page now and its identities are
// already per session. The connection and restored bodies' hand-painted buttons
// and the desktop's controls keep their application identities, so side by side
// each pane mounts them at its own place; a workspace of one pane paints them in
// place.
func (state *State) paneIdentity(session *Session, local gfx.ID) gfx.ID {
	if state.paintsTerminalPane(session) {
		return local
	}
	state.refreshWorkspaceViews()
	if len(state.panes) > 1 {
		return tree.MountID(paneMount(session), local)
	}
	return local
}

func (state *State) activeTerminalFocusHit() gfx.ID {
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || session.term == nil {
		return gfx.ID{}
	}
	if session.desk != nil {
		// The desktop's picture is the session's own place, not one of the
		// page's mounted button identities.
		return hitTerminalList.At(int(session.ID))
	}
	session.term.pageIdentity(session.ID)
	return state.paneIdentity(session, session.term.controlID)
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
	_, ok := state.activePaneSplitBounds(axis)
	if !ok {
		return false
	}
	workspaceID := state.deck.ActiveWorkspace()
	activePane := state.deck.ActivePane(workspaceID)
	if _, err := state.deck.Split(workspaceID, activePane, axis); err != nil {
		return false
	}
	state.syncActiveSession()
	state.refreshWorkspaceViews()
	state.fitWorkspaceTerminals(workspaceID)
	state.fitSFTP(state.viewport)
	return true
}

func (state *State) canSplitActivePane(axis layoutcore.Axis) bool {
	_, ok := state.activePaneSplitBounds(axis)
	return ok
}

func (state *State) activePaneSplitBounds(axis layoutcore.Axis) (gfx.Rect, bool) {
	state.refreshWorkspaceViews()
	workspaceID := state.deck.ActiveWorkspace()
	activePane := state.deck.ActivePane(workspaceID)
	if workspaceID == 0 || activePane == 0 || len(state.panes) >= layoutcore.MaxPanes {
		return gfx.Rect{}, false
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
		return gfx.Rect{}, false
	}
	bounds := pixelBounds(state.workspacePixelBounds(state.viewport), active.Bounds)
	if axis == layoutcore.Horizontal && bounds.Width < minPaneWidth*2+1 ||
		axis == layoutcore.Vertical && bounds.Height < minPaneHeight*2+1 {
		return gfx.Rect{}, false
	}
	return bounds, true
}

// splitWithCurrentHost splits the active pane and opens the same host in the
// new one, in the directory the shell was last reported in: a split is most
// often a second shell on the machine already at hand, to run something beside
// the first in the same place.
// From an empty pane there is no host to repeat, so the new pane stays empty and
// offers the host list.
func (state *State) splitWithCurrentHost(axis layoutcore.Axis) {
	var host Host
	var directory string
	repeat := false
	if session, ok := state.sessionByIDPointer(state.activeSessionID); ok && state.activeSection == sectionTerminal && session.host.Address != "" {
		host, repeat = session.host, true
		directory = session.splitDirectory()
	}
	if !state.splitActivePane(axis) {
		return
	}
	if repeat {
		workspaceID := state.deck.ActiveWorkspace()
		state.pendingWorkspace, state.pendingPane = workspaceID, state.deck.ActivePane(workspaceID)
		state.pendingDirectory = directory
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
// place, and the docked files panel is part of the workbench page now).
func (state *State) decodePaneHit(hit gfx.ID) (*Session, gfx.ID, bool) {
	if state.activeSection != sectionTerminal {
		return nil, gfx.ID{}, false
	}
	mount, ok := tree.MountOf(hit.Scope())
	if !ok {
		return nil, gfx.ID{}, false
	}
	local, ok := tree.Local(mount, hit)
	if !ok {
		return nil, gfx.ID{}, false
	}
	if tree.Parent(mount) != panesHits {
		return nil, gfx.ID{}, false
	}
	slot, ok := tree.ScopeName(mount).Index()
	if !ok {
		return nil, gfx.ID{}, false
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

// paneWindowBounds is where a session's pane sits in the window at size. It is
// false for a session not in the active workspace, or whose pane the deck cannot
// place.
func (state *State) paneWindowBounds(sessionID uint32, size gfx.Size) (gfx.Rect, bool) {
	workspaceID, paneID, ok := state.deck.PaneForSession(layoutcore.SessionID(sessionID))
	if !ok || workspaceID != state.deck.ActiveWorkspace() {
		return gfx.Rect{}, false
	}
	fraction, _, ok := state.paneFractionBounds(workspaceID, paneID)
	if !ok {
		return gfx.Rect{}, false
	}
	return pixelBounds(state.workspacePixelBoundsFor(workspaceID, size), fraction), true
}

func (state *State) paneFractionBounds(workspaceID layoutcore.WorkspaceID, paneID layoutcore.PaneID) (FractionBounds, int, bool) {
	layout, ok := state.deck.Layout(workspaceID)
	if !ok || layout.Root() == 0 {
		return FractionBounds{}, 0, false
	}
	if state.workspaceTabbed(workspaceID) {
		// Every tab is sized for the whole area, so showing one needs no resize.
		_, found := findPaneFractionBounds(layout, layout.Root(), paneID, FractionBounds{Width: 1, Height: 1})
		return FractionBounds{Width: 1, Height: 1}, (layout.Len() + 1) / 2, found
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
func pointInRect(x, y float32, bounds gfx.Rect) bool {
	return x >= bounds.X && y >= bounds.Y && x < bounds.X+bounds.Width && y < bounds.Y+bounds.Height
}
