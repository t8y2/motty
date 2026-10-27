package workbench

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
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
}

func (state *State) openRemoteToolsWindow(session *Session) {
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
				window.window.Show()
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
	w.ui.remote = remoteToolsView{session: session.ID, title: session.Title, tab: state.remote.tab, sort: state.remote.sort, serial: 1}
	window, err := state.options.OpenContentWindow(app.Config{
		Title: state.messages.Tools.RemoteTitle + " · " + session.Title + " · " + session.Address,
		Icon:  AppIcon(), Width: 1100, Height: 780, Tree: &tree,
		ClearColor: app.Color{Red: float64(state.pal.workspace.Red), Green: float64(state.pal.workspace.Green), Blue: float64(state.pal.workspace.Blue), Alpha: 1},
		Paint:      w.paint, OnKey: w.key, OnClosed: w.dispose,
		OnScroll: func(ui.ScrollEvent) ui.EventResult { return ui.EventResult{Handled: true} },
		TextInput: func(c app.TextInputContext) *app.TextInput {
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
	w.ui.remote.serial++
	w.cancel()
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

func (w *remoteToolsWindow) click(id ui.ID) {
	if w.closed {
		return
	}
	if w.ui.modal == modalForm {
		w.ui.clickForm(id)
	} else if id == hitRemoteClose || id == hitRemoteDismiss {
		w.close()
	} else {
		w.ui.clickRemoteTools(id)
	}
}

func (w *remoteToolsWindow) key(event ui.KeyEvent) ui.EventResult {
	if event.Key == ui.KeyEscape && !event.Repeat {
		if w.ui.modal == modalForm {
			w.ui.clickForm(hitFormCancel)
		} else {
			w.close()
		}
		return ui.EventResult{Handled: true, Repaint: true}
	}
	if w.ui.modal == modalForm {
		if event.Key == ui.KeyEnter && event.Modifiers == 0 && !event.Repeat {
			if editor := w.ui.formEditor(event.Target); editor != nil {
				if _, composing := editor.Composition(); !composing {
					w.ui.submitForm()
				}
				return ui.EventResult{Handled: true, Repaint: true}
			}
		}
		return ui.EventResult{}
	}
	var options [6]components.SegmentOption
	for i, id := range remoteTabHits {
		options[i].ID = id
	}
	if next, handled := components.SegmentedControlKeyDown(event.Target, options[:], components.DirectionLTR, event.Key); handled {
		w.click(next)
		return ui.EventResult{Focus: next, FocusVisible: true, Handled: true, Repaint: true}
	}
	sorts := [...]components.SegmentOption{{ID: hitRemoteSortCPU}, {ID: hitRemoteSortMemory}}
	if next, handled := components.SegmentedControlKeyDown(event.Target, sorts[:], components.DirectionLTR, event.Key); handled {
		w.click(next)
		return ui.EventResult{Focus: next, FocusVisible: true, Handled: true, Repaint: true}
	}
	return ui.EventResult{}
}

func (w *remoteToolsWindow) paint(canvas *ui.Canvas, size ui.Size) {
	if w.closed {
		return
	}
	canvas.SetTree(&tree)
	if id, ok := canvas.ClickTarget(); ok {
		canvas.AfterPaint(func() { w.click(id) })
	}
	w.ui.language, w.ui.messages = w.owner.language, w.owner.messages
	w.ui.appearance, w.ui.pal, w.ui.theme = w.owner.appearance, w.owner.pal, w.owner.theme
	w.ui.viewport = size
	theme, m := w.ui.Theme(), w.ui.messages.Tools
	canvas.Fill(ui.Rect{Width: size.Width, Height: size.Height}, theme.Background)
	const pad, buttonHeight, buttonGap = float32(16), float32(32), float32(8)
	width := max(size.Width-pad*2, 0)
	canvas.Label(m.RemoteTitle+" · "+w.session.Title, ui.Rect{X: pad, Y: pad, Width: width, Height: 28}, ui.TextStyle{Size: 18, Weight: ui.WeightSemibold, Color: theme.Foreground})
	canvas.Label(w.session.Address+" · "+fmt.Sprintf(m.RemoteDescriptionFmt, w.session.Title), ui.Rect{X: pad, Y: 48, Width: width, Height: 22}, ui.TextStyle{Size: theme.Metrics.FontSizeS, Color: theme.MutedForeground})
	actions, count := w.ui.remoteToolActions()
	var boxes [8]ui.Rect
	x, y := float32(0), float32(0)
	for i := 0; i < count; i++ {
		buttonWidth := min(components.BeginButton(canvas, theme, ui.ID{}, ui.Rect{}).Small().SizeToFit(actions[i].Label).BoundsWidth(), width)
		if x > 0 && x+buttonWidth > width {
			x, y = 0, y+buttonHeight+buttonGap
		}
		boxes[i] = ui.Rect{X: pad + x, Y: y, Width: buttonWidth, Height: buttonHeight}
		x += buttonWidth + buttonGap
	}
	footerTop := max(84, size.Height-pad-y-buttonHeight)
	body := ui.Rect{X: pad, Y: 84, Width: width, Height: max(footerTop-96, 0)}
	toolbar := float32(34)
	if width < 800 && w.ui.remote.tab == remoteProcesses {
		toolbar += 42
	}
	pageSize := max(1, int((body.Height-toolbar-48-components.TableHeaderHeight(theme))/components.TableRowHeight(theme)))
	if pageSize != w.pageSize {
		first := w.ui.remote.page * w.pageSize
		w.pageSize = pageSize
		w.ui.remote.page = min(first/pageSize, max(0, (len(w.ui.remote.rows)-1)/pageSize))
		w.ui.remote.pageText = ""
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
	if w.ui.modal == modalForm {
		w.ui.paintFormDialog(canvas, ui.Rect{Width: size.Width, Height: size.Height})
	}
}
