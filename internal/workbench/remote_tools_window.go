package workbench

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
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

	// The window's page and the stable callbacks it declares. The callbacks are
	// made once, not in each paint.
	page *ui.Page
	root string

	tableHandlers   ui.Handlers
	refreshHandlers ui.Handlers
	autoHandlers    ui.Handlers
	tableDraw       func(*ui.DrawContext)
	tableHeaderDraw func(*ui.DrawContext)
	refreshDraw     func(*ui.DrawContext)
	autoDraw        func(*ui.DrawContext)
	footerFlow      remoteToolsButtonFlow
	paintTab        remoteTab
	tablePressRow   int
	tableHoverRow   int
	menuCtx         ui.C
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
	w := &remoteToolsWindow{owner: state, group: state.options.Group, connection: session.conn, ctx: ctx, cancel: cancel, pageSize: remoteRowsPerPage, tablePressRow: -1, tableHoverRow: -1}
	w.session.ID = session.ID
	w.bind(state, session)
	w.ui.language, w.ui.messages = state.language, state.messages
	w.ui.appearance, w.ui.pal, w.ui.theme = state.appearance, state.pal, state.theme
	w.ui.modal, w.ui.activeSessionID, w.ui.remoteWindow = modalRemoteTools, session.ID, w
	w.ui.remote = remoteToolsView{session: session.ID, title: session.Title, tab: tab, sort: state.remote.sort, serial: 1}
	window, err := state.options.OpenContentWindow(w.config(), nil)
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

// config is the window's app.Config: the page's own routing, over the window's
// real handlers for text selection, scrolling and the source's shortcuts.
func (w *remoteToolsWindow) config() app.Config {
	base := app.Config{
		Title: w.ui.messages.Tools.RemoteTitle + " · " + w.session.Title + " · " + w.session.Address,
		Icon:  AppIcon(), Width: 1100, Height: 780, Tree: &tree,
		WindowMode: app.WindowCustomTitlebar, TitlebarHeight: TitlebarHeight,
		MinWidth: remoteToolsMinWidth, MinHeight: remoteToolsMinHeight,
		ClearColor: app.Color{Red: float64(w.ui.pal.workspace.Red), Green: float64(w.ui.pal.workspace.Green), Blue: float64(w.ui.pal.workspace.Blue), Alpha: 1},
		Paint:      w.paint, OnKey: w.key, OnPointer: w.pointer, OnClosed: w.dispose,
		OnShortcut: w.shortcut, OnScroll: w.scroll, TextInput: w.textInput,
		OnMinimize: func(minimized bool) {
			if minimized || w.closed || w.ui.remote.loading {
				return
			}
			// A restored window is looked at again, like a reopened one. Its
			// rows describe the host when they were read, not now.
			w.ui.refreshRemoteTools()
		},
	}
	base.WindowBorderColor = func() app.Color {
		return app.Color{Red: float64(w.ui.pal.workspace.Red), Green: float64(w.ui.pal.workspace.Green), Blue: float64(w.ui.pal.workspace.Blue), Alpha: 1}
	}
	config := w.windowPage().Config(base)
	// The page's Config paints the page; the window's paint keeps the presenters
	// in step with its owner before the page is declared.
	config.Paint = w.paint
	return config
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

// act runs one stable application action: the window's page controls call it
// from their click handlers, and the tests call it directly.
func (w *remoteToolsWindow) act(id gfx.ID) {
	if w.closed {
		return
	}
	switch w.ui.modal {
	case modalSudoSFTP:
		w.ui.clickSudoSFTP(id)
	case modalForm:
		w.ui.clickForm(id)
	default:
		switch {
		case id == hitRemoteClose || id == hitRemoteDismiss:
			w.close()
		case id == hitRemoteAutoToggle:
			w.ui.remote.auto.enabled = !w.ui.remote.auto.enabled
			w.ui.remote.auto.restart()
		case id.Scope() == hitRemoteIntervals:
			if index := item(id); index >= 0 && index < len(remoteRefreshIntervals) {
				w.ui.remote.auto.interval = remoteRefreshIntervals[index]
				w.ui.remote.auto.enabled = true
				w.ui.remote.auto.restart()
			}
		default:
			w.ui.clickRemoteTools(id)
		}
	}
}

// key handles the keys the window's page does not: the dialogs' text editing,
// the tools' own search and detail keys, and Escape closing the window.
func (w *remoteToolsWindow) key(event gfx.KeyEvent) gfx.EventResult {
	if w.closed {
		return gfx.EventResult{Handled: true}
	}
	w.ui.shortcuts = w.owner.shortcuts
	switch w.ui.modal {
	case modalSudoSFTP:
		// The page's dialog owns Escape and its controls.
		if event.Key == gfx.KeyEnter && !event.Repeat && event.Target == hitSudoSFTPPassword {
			w.ui.submitSudoSFTP()
			return gfx.EventResult{Handled: true, Repaint: true}
		}
		return gfx.EventResult{}
	case modalForm:
		// The page's dialog owns Escape and its controls.
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
	if w.ui.remote.processDetails.open {
		if result, handled := w.ui.remoteProcessDetailsKey(event); handled {
			return result
		}
	}
	if w.ui.remote.containerDetails.open {
		if result, handled := w.ui.remoteContainerDetailsKey(event); handled {
			return result
		}
	}
	if w.ui.remote.auto.open {
		// The page's menu has the keyboard.
		return gfx.EventResult{}
	}
	if result := w.ui.remoteSearchShortcut(event); result.Handled {
		return result
	}
	if result := w.ui.remoteSearchKey(event); result.Handled {
		return result
	}
	if (event.Key == gfx.KeyEnter || event.Key == gfx.KeySpace) && !event.Repeat {
		if w.ui.clickRemoteTableSort(event.Target) {
			return gfx.EventResult{Handled: true, Repaint: true}
		}
	}
	if event.Key == gfx.KeyEscape && !event.Repeat {
		w.close()
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	return gfx.EventResult{}
}

// shortcut gives the tools' own find key and the detail text's copy/select-all
// their chords.
func (w *remoteToolsWindow) shortcut(event gfx.KeyEvent) gfx.EventResult {
	if w.closed {
		return gfx.EventResult{Handled: true}
	}
	w.ui.shortcuts = w.owner.shortcuts
	if w.ui.modal == modalForm || w.ui.modal == modalSudoSFTP || w.ui.remote.auto.open {
		return gfx.EventResult{}
	}
	if result, handled := w.ui.remoteDetailShortcut(event); handled {
		return result
	}
	return w.ui.remoteSearchShortcut(event)
}

// pointer handles the pointers the page's controls and overlays do not own:
// selecting and scrolling the detail text, and dragging the process scrollbar.
// A press or release it leaves untouched reaches the page's click pass, which
// is what lets a dialog's buttons, its close target and a menu's items act.
func (w *remoteToolsWindow) pointer(event gfx.PointerEvent) gfx.EventResult {
	if w.closed {
		return gfx.EventResult{Handled: true}
	}
	if w.ui.remote.processDetails.open {
		if result, handled := w.ui.remoteProcessDetailsTextPointer(event); handled {
			return result
		}
		if result, handled := w.ui.remoteProcessDetailsScrollPointer(event); handled {
			return result
		}
	}
	if w.ui.remote.containerDetails.open {
		if result, handled := w.ui.remoteContainerDetailsTextPointer(event); handled {
			return result
		}
		if result, handled := w.ui.remoteContainerDetailsScrollPointer(event); handled {
			return result
		}
	}
	if result, handled := w.ui.remoteProcessScrollPointer(event); handled {
		return result
	}
	return gfx.EventResult{}
}

// scroll sends the wheel to the open dialog, else to the process snapshot.
func (w *remoteToolsWindow) scroll(event gfx.ScrollEvent) gfx.EventResult {
	if w.closed {
		return gfx.EventResult{Handled: true}
	}
	if w.ui.remote.auto.open {
		return gfx.EventResult{Handled: true}
	}
	if result, handled := w.ui.remoteProcessWheel(event); handled {
		return result
	}
	return gfx.EventResult{Handled: true}
}

// textInput answers for a form or sudo field before the page has declared it,
// since the platform may ask for an editor as soon as the dialog opens.
func (w *remoteToolsWindow) textInput(context app.TextInputContext) *app.TextInput {
	if w.ui.modal == modalSudoSFTP && context.ID == hitSudoSFTPPassword {
		return &app.TextInput{Editor: &w.ui.sudoSFTP.password}
	}
	if w.ui.modal != modalForm || context.ID.Scope() != hitFormFieldList {
		return nil
	}
	if editor := w.ui.formEditor(context.ID); editor != nil {
		return &app.TextInput{Editor: editor}
	}
	return nil
}
