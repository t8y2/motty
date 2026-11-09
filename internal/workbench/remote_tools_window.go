package workbench

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

// A tool window reuses the remote-tools and form presenters in an isolated
// presentation state. It never starts a second workbench, config store or SSH
// connection; only the source workbench owns the borrowed session.
type remoteToolsWindow struct {
	owner      *State
	group      *Group
	connection Connection
	session    Session // a stable presenter identity, independent of moved tab IDs
	ui         State
	window     *app.ContentWindow
	ctx        context.Context
	cancel     context.CancelFunc
	closed     bool
	pageSize   int
}

func (state *State) openRemoteToolsWindowTab(session *Session, tab remoteTab, switchTab bool) {
	owners := []*State{state}
	if group := state.options.Group; group != nil {
		group.mu.Lock()
		owners = append(owners, group.members...)
		group.mu.Unlock()
	}
	for _, owner := range owners {
		for _, window := range owner.remoteWindows {
			if !window.closed && window.connection == session.conn {
				window.bind(state, session)
				if switchTab {
					window.ui.remote.tab = tab
				}
				window.ui.remote.selected = ""
				window.ui.remote.page = 0
				window.ui.remote.rows = nil
				window.ui.remote.pageText = ""
				window.ui.remote.collectedAt = time.Time{}
				window.ui.remote.stale = false
				window.window.Show()
				// The window is a snapshot of a remote host, and a container or
				// process may have changed since it was read. Bringing the tools
				// back is a deliberate "show me now", so read the tab again rather
				// than replaying rows that may no longer exist.
				window.ui.refreshRemoteTools()
				return
			}
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	w := &remoteToolsWindow{owner: state, group: state.options.Group, connection: session.conn, ctx: ctx, cancel: cancel, pageSize: remoteRowsPerPage}
	w.session.ID = session.ID
	w.bind(state, session)
	w.ui.language, w.ui.messages = state.language, state.messages
	w.ui.appearance, w.ui.pal, w.ui.theme = state.appearance, state.pal, state.theme
	w.ui.modal, w.ui.activeSessionID, w.ui.remoteWindow = modalRemoteTools, session.ID, w
	w.ui.remote = remoteToolsView{session: session.ID, title: session.Title, tab: tab, sort: state.remote.sort, serial: 1}
	window, err := state.options.OpenContentWindow(app.Config{
		Title: state.messages.Tools.RemoteTitle + " · " + session.Title + " · " + session.Address,
		Icon:  AppIcon(), Width: 1100, Height: 780, Tree: &tree,
		WindowMode: app.WindowCustomTitlebar, TitlebarHeight: TitlebarHeight,
		MinWidth: remoteToolsMinWidth, MinHeight: remoteToolsMinHeight,
		ClearColor: app.Color{Red: float64(state.pal.workspace.Red), Green: float64(state.pal.workspace.Green), Blue: float64(state.pal.workspace.Blue), Alpha: 1},
		Paint:      w.paint, OnKey: w.key, OnPointer: w.pointer, OnClosed: w.dispose,
		OnShortcut: func(event gfx.KeyEvent) gfx.EventResult {
			w.ui.shortcuts = w.owner.shortcuts
			if result, handled := w.ui.remoteDetailShortcut(event); handled {
				return result
			}
			return w.ui.remoteSearchShortcut(event)
		},
		OnMinimize: func(minimized bool) {
			if minimized || w.closed || w.ui.remote.loading {
				return
			}
			// A restored window is looked at again, like a reopened one. Its
			// rows describe the host when they were read, not now.
			w.ui.refreshRemoteTools()
		},
		OnScroll: func(event gfx.ScrollEvent) gfx.EventResult {
			if w.ui.remote.auto.open {
				return gfx.EventResult{Handled: true, Repaint: w.ui.remote.auto.scroll.Wheel(event)}
			}
			if result, handled := w.ui.remoteProcessWheel(event); handled {
				return result
			}
			return gfx.EventResult{Handled: true}
		},
		TextInput: func(c app.TextInputContext) *app.TextInput {
			if w.ui.modal == modalSudoSFTP && c.ID == hitSudoSFTPPassword {
				return &app.TextInput{Editor: &w.ui.sudoSFTP.password}
			}
			if input := w.ui.RemoteToolsInput(c); input != nil {
				return input
			}
			if w.ui.modal != modalForm || c.ID.Scope() != hitFormFieldList {
				return nil
			}
			return &app.TextInput{Editor: w.ui.formEditor(c.ID)}
		},
	}, nil)
	if err != nil {
		w.dispose()
		state.fileNotices.show(state.messages.Tools.RemoteTitle, err.Error(), components.ToastError)
		return
	}
	w.window = window
	if w.closed || window.Closed() {
		w.close()
		return
	}
	state.remoteWindows = append(state.remoteWindows, w)
	w.ui.refreshRemoteTools()
}

func (w *remoteToolsWindow) bind(owner *State, source *Session) {
	if w.owner != owner {
		w.owner.remoteWindows = slices.DeleteFunc(w.owner.remoteWindows, func(other *remoteToolsWindow) bool { return other == w })
		owner.remoteWindows = append(owner.remoteWindows, w)
		w.owner = owner
	}
	w.session.Title, w.session.Address, w.session.host = source.Title, source.Address, source.host
	w.session.conn, w.session.term, w.session.phase = w.connection, source.term, source.phase
	w.session.sudoCommands, w.session.sudoPassword = source.sudoCommands, source.sudoPassword
}

// Locate the captured connection, including after its tab moves to another
// workbench window. The currently selected tab is never used as a substitute.
func (w *remoteToolsWindow) source() (*State, *Session, bool) {
	find := func(owner *State) (*Session, bool) {
		if owner.closed {
			return nil, false
		}
		for i := range owner.sessions {
			if owner.sessions[i].conn == w.connection {
				return &owner.sessions[i], true
			}
		}
		return nil, false
	}
	if session, ok := find(w.owner); ok {
		w.bind(w.owner, session)
		return w.owner, session, true
	}
	if w.group != nil {
		w.group.mu.Lock()
		members := append([]*State(nil), w.group.members...)
		w.group.mu.Unlock()
		for _, owner := range members {
			if session, ok := find(owner); ok {
				w.bind(owner, session)
				return owner, session, true
			}
		}
	}
	return nil, nil, false
}

func (w *remoteToolsWindow) dispose() {
	if w.closed {
		return
	}
	w.closed, w.ui.closed = true, true
	w.ui.sudoSFTP = sudoSFTPDialog{}
	w.ui.modal = modalNone
	w.ui.remote.auto.enabled, w.ui.remote.auto.open = false, false
	w.ui.remote.serial++
	w.ui.cancelRemoteProcessCommand()
	w.ui.cancelRemoteProcessEnvironment()
	w.ui.cancelRemoteContainerEnvironment()
	w.cancel()
	w.session.sudoCommands, w.session.sudoPassword = false, ""
	w.owner.remoteWindows = slices.DeleteFunc(w.owner.remoteWindows, func(other *remoteToolsWindow) bool { return other == w })
}

func (w *remoteToolsWindow) close() {
	w.dispose()
	if w.window != nil {
		w.window.Close()
	}
}

func (state *State) closeRemoteToolsWindows() {
	for len(state.remoteWindows) > 0 {
		w := state.remoteWindows[len(state.remoteWindows)-1]
		if owner, _, ok := w.source(); ok && owner != state {
			continue // source() moved the window's ownership with its session
		}
		w.close()
	}
}

func (state *State) syncRemoteToolsWindows() {
	for i := len(state.remoteWindows) - 1; i >= 0; i-- {
		w := state.remoteWindows[i]
		owner, source, ok := w.source()
		if !ok || !owner.remoteToolsOffered(source) {
			w.close()
		}
	}
}

func (state *State) invalidateRemoteToolsWindows() {
	for _, w := range state.remoteWindows {
		w.window.Invalidate()
	}
}

func (state *State) remoteToolsContext() context.Context {
	if state.remoteWindow != nil {
		return state.remoteWindow.ctx
	}
	return context.Background()
}

func (state *State) postRemoteToolsUpdate(update func(*State)) {
	if w := state.remoteWindow; w != nil {
		w.window.Post(func() {
			if w.closed {
				return
			}
			owner, source, ok := w.source()
			if !ok || !owner.remoteToolsOffered(source) {
				w.close()
				return
			}
			update(&w.ui)
		})
		return
	}
	state.aiPost(update)
}

func (state *State) remotePageSize() int {
	if state.remoteWindow != nil {
		return max(state.remoteWindow.pageSize, 1)
	}
	return remoteRowsPerPage
}

func (state *State) typeRemoteCommand(session *Session, command string) bool {
	if state.remoteWindow == nil {
		return state.typed(session, []byte(command+"\r"))
	}
	owner, session, ok := state.remoteWindow.source()
	if !ok || !owner.remoteToolsOffered(session) {
		return false
	}
	// Tool actions target one captured server even when the source workspace
	// broadcasts keyboard input to its other panes.
	if !session.term.send([]byte(command + "\r")) {
		return false
	}
	owner.ActivatePane(session.ID)
	if owner.options.Raise != nil {
		owner.options.Raise()
	}
	if owner.options.Invalidate != nil {
		owner.options.Invalidate()
	}
	return true
}

func (state *State) openRemoteContainerTab(sourceID uint32, name, command string) error {
	if w := state.remoteWindow; w != nil {
		owner, source, ok := w.source()
		if !ok || !owner.remoteToolsOffered(source) {
			return errors.New(state.messages.History.StatusNotConnected)
		}
		err := owner.openContainerTab(source.ID, name, command)
		if err == nil && owner.options.Raise != nil {
			owner.options.Raise()
		}
		return err
	}
	return state.openContainerTab(sourceID, name, command)
}

func (state *State) finishRemoteTerminalAction() {
	state.closeModal()
	if state.remoteWindow != nil {
		state.modal = modalRemoteTools
	}
}

func (w *remoteToolsWindow) click(id gfx.ID) {
	if w.closed {
		return
	}
	if w.ui.modal == modalSudoSFTP {
		w.ui.clickSudoSFTP(id)
	} else if w.ui.modal == modalForm {
		w.ui.clickForm(id)
	} else if id == hitRemoteClose || id == hitRemoteDismiss {
		w.close()
	} else {
		w.ui.clickRemoteTools(id)
	}
}

func (w *remoteToolsWindow) key(event gfx.KeyEvent) gfx.EventResult {
	if w.ui.modal == modalSudoSFTP {
		if event.Key == gfx.KeyEscape && !event.Repeat {
			w.ui.clickSudoSFTP(hitSudoSFTPCancel)
			return gfx.EventResult{Handled: true, Repaint: true}
		}
		if event.Key == gfx.KeyEnter && !event.Repeat && event.Target == hitSudoSFTPPassword {
			w.ui.submitSudoSFTP()
			return gfx.EventResult{Handled: true, Repaint: true}
		}
		return gfx.EventResult{}
	}
	w.ui.shortcuts = w.owner.shortcuts
	if result, handled := w.ui.remoteProcessDetailsKey(event); handled {
		return result
	}
	if result, handled := w.ui.remoteContainerDetailsKey(event); handled {
		return result
	}
	if result, handled := w.ui.menuKey(event); handled {
		return result
	}
	if result, handled := w.ui.remoteProcessTableKey(event); handled {
		return result
	}
	if result := w.ui.remoteSearchShortcut(event); result.Handled {
		return result
	}
	if result := w.ui.remoteSearchKey(event); result.Handled {
		return result
	}
	if result := w.ui.remoteRefreshKey(event); result.Handled {
		return result
	}
	if event.Key == gfx.KeyEscape && !event.Repeat {
		if w.ui.modal == modalForm {
			w.ui.clickForm(hitFormCancel)
		} else {
			w.close()
		}
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	if w.ui.modal == modalForm {
		if event.Key == gfx.KeyEnter && event.Modifiers == 0 && !event.Repeat {
			if editor := w.ui.formEditor(event.Target); editor != nil {
				if _, composing := editor.Composition(); !composing {
					w.ui.submitForm()
				}
				return gfx.EventResult{Handled: true, Repaint: true}
			}
		}
		return gfx.EventResult{}
	}
	var options [6]components.SegmentOption
	for i, id := range remoteTabHits {
		options[i].ID = id
	}
	if next, handled := components.SegmentedControlKeyDown(event.Target, options[:], components.DirectionLTR, event.Key); handled {
		w.click(next)
		return gfx.EventResult{Focus: next, FocusVisible: true, Handled: true, Repaint: true}
	}
	return gfx.EventResult{}
}

func (w *remoteToolsWindow) paint(canvas *gfx.Canvas, size gfx.Size) {
	if w.closed {
		return
	}
	w.ui.beginMenus()
	defer w.ui.endMenus()
	canvas.SetTree(&tree)
	if id, ok := canvas.ClickTarget(); ok {
		canvas.AfterPaint(func() { w.click(id) })
	}
	w.ui.language, w.ui.messages = w.owner.language, w.owner.messages
	w.ui.appearance, w.ui.pal, w.ui.theme = w.owner.appearance, w.owner.pal, w.owner.theme
	w.ui.viewport = size
	theme := w.ui.Theme()
	canvas.Fill(gfx.Rect{Width: size.Width, Height: size.Height}, theme.Background)
	const pad, buttonHeight, buttonGap = float32(16), float32(32), float32(8)
	w.paintTitlebar(canvas, size.Width)
	width := max(size.Width-pad*2, 0)
	actions, count := w.ui.remoteToolActions()
	var boxes [8]gfx.Rect
	x, y := float32(0), float32(0)
	for i := 0; i < count; i++ {
		buttonWidth := min(components.BeginButton(canvas, theme, gfx.ID{}, gfx.Rect{}).Small().SizeToFit(actions[i].Label).BoundsWidth(), width)
		if x > 0 && x+buttonWidth > width {
			x, y = 0, y+buttonHeight+buttonGap
		}
		boxes[i] = gfx.Rect{X: pad + x, Y: y, Width: buttonWidth, Height: buttonHeight}
		x += buttonWidth + buttonGap
	}
	footerTop := max(pad, size.Height-pad-y-buttonHeight)
	bodyTop := TitlebarHeight + pad
	body := gfx.Rect{X: pad, Y: bodyTop, Width: width, Height: max(footerTop-bodyTop-12, 0)}
	toolbar := remoteToolbarHeight + remoteSearchHeight + 8
	if width < 800 {
		toolbar += remoteToolbarHeight + 8
	}
	if w.ui.remote.tab != remoteProcesses {
		pageSize := max(1, int((body.Height-toolbar-48-components.TableHeaderHeight(theme))/components.TableRowHeight(theme)))
		if pageSize != w.pageSize {
			first := w.ui.remote.page * w.pageSize
			w.pageSize = pageSize
			w.ui.remote.page = min(first/pageSize, max(0, (len(w.ui.remote.rows)-1)/pageSize))
			w.ui.remote.pageText = ""
		}
	}
	canvas.Clip(body)
	w.ui.paintRemoteToolsBody(canvas, body)
	canvas.Unclip()
	for i := 0; i < count; i++ {
		bounds := boxes[i]
		bounds.Y += footerTop
		button := components.BeginButton(canvas, theme, actions[i].ID, bounds).Small()
		if actions[i].Disabled {
			button = button.Disabled()
		}
		switch actions[i].Variant {
		case components.ButtonDestructive:
			button.Destructive(actions[i].Label)
		case components.ButtonSecondary:
			button.Secondary(actions[i].Label)
		default:
			button.Outline(actions[i].Label)
		}
	}
	w.ui.paintRemoteRefreshMenu(canvas)
	w.ui.paintRemoteProcessMenu(canvas, gfx.Rect{Width: size.Width, Height: size.Height})
	if w.ui.modal == modalForm {
		w.ui.paintFormDialog(canvas, gfx.Rect{Width: size.Width, Height: size.Height})
	}
	w.ui.paintRemoteProcessDetails(canvas, gfx.Rect{Width: size.Width, Height: size.Height})
	w.ui.paintRemoteContainerDetails(canvas, gfx.Rect{Width: size.Width, Height: size.Height})
	if w.ui.modal == modalSudoSFTP {
		w.ui.paintSudoSFTP(canvas, gfx.Rect{Width: size.Width, Height: size.Height})
	}
}

func (w *remoteToolsWindow) paintTitlebar(canvas *gfx.Canvas, width float32) {
	canvas.MoveRegion(gfx.Rect{Width: width, Height: TitlebarHeight})
	metrics := w.ui.Theme().Metrics
	leading, trailing := app.TitlebarInsets()
	const gap = float32(14)
	start := leading + gap
	available := width - start - trailing - gap
	if available <= 0 {
		return
	}
	label := w.ui.messages.Tools.RemoteTitle
	labelWidth := canvas.TextWidthWeight(label, metrics.FontSizeS, gfx.WeightMedium)
	if labelWidth > available {
		label = elide(canvas, label, metrics.FontSizeS, gfx.WeightMedium, available)
		labelWidth = canvas.TextWidthWeight(label, metrics.FontSizeS, gfx.WeightMedium)
	}
	bar := gfx.Rect{X: start, Width: labelWidth, Height: TitlebarHeight}
	canvas.Label(label, bar, gfx.TextStyle{Size: metrics.FontSizeS, Weight: gfx.WeightMedium, Color: w.ui.pal.text})
	if room := available - labelWidth - gap; room > 24 {
		bar.X, bar.Width = start+labelWidth+gap, room
		connection := w.session.Title + " · " + w.session.Address
		canvas.Label(elide(canvas, connection, metrics.FontSizeS, gfx.WeightRegular, room), bar,
			gfx.TextStyle{Size: metrics.FontSizeS, Color: w.ui.pal.muted})
	}
}
