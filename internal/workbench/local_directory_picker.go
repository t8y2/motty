package workbench

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	layoutcore "motty/internal/workspace"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

var localDirectoryHits = tree.Feature("local-directory")
var (
	hitLocalDirectoryOpen   = tree.ID(localDirectoryHits, "open")
	hitLocalDirectorySearch = tree.ID(localDirectoryHits, "search")
	hitLocalDirectoryBrowse = tree.ID(localDirectoryHits, "browse")
	hitLocalDirectoryClose  = tree.ID(localDirectoryHits, "close")
	hitLocalDirectoryRows   = tree.Sub(localDirectoryHits, tree.Name("row"))
	hitLocalDirectoryRemove = tree.Sub(localDirectoryHits, tree.Name("remove"))
)

type localDirectoryPicker struct {
	search    text.Editor
	field     ui.Field
	visible   []string
	query     string
	selected  int
	target    uint32
	errorText string
	scroll    *ui.ScrollState
	rowHeight float32
	rowGap    float32
}

func (state *State) openLocalDirectoryPicker() {
	if state.modal != modalNone {
		return
	}
	state.localDirectoryPicker = localDirectoryPicker{}
	picker := &state.localDirectoryPicker
	if session := state.focusedShortcutTerminal(gfx.ID{}); session != nil {
		picker.target = session.ID
	}
	picker.field.Bind(&picker.search)
	state.modal = modalLocalDirectory
	state.refreshLocalDirectories()
}

func (state *State) refreshLocalDirectories() {
	picker := &state.localDirectoryPicker
	query := strings.ToLower(strings.TrimSpace(picker.search.String()))
	if query != picker.query {
		picker.selected = 0
		picker.query = query
		if picker.scroll != nil {
			picker.scroll.ScrollTo(0)
		}
	}
	picker.visible = picker.visible[:0]
	for _, directory := range state.localDirectoryHistory {
		if strings.Contains(strings.ToLower(directory), query) {
			picker.visible = append(picker.visible, directory)
		}
	}
	picker.selected = max(0, min(picker.selected, len(picker.visible)-1))
}

func (state *State) clickLocalDirectory(id gfx.ID) {
	picker := &state.localDirectoryPicker
	state.refreshLocalDirectories()
	switch id {
	case hitLocalDirectoryClose:
		state.modal = modalNone
	case hitLocalDirectoryBrowse:
		directory, err := state.options.OpenDirectory(app.OpenDirectoryOptions{Title: state.messages.LocalDirectoryTitle})
		if err != nil {
			picker.errorText = state.messages.LocalDirectoryUnavailable
			return
		}
		if directory != "" {
			state.chooseLocalDirectory(directory)
		}
	default:
		if index, ok := id.In(hitLocalDirectoryRows); ok && index < len(picker.visible) {
			state.chooseLocalDirectory(picker.visible[index])
			return
		}
		if index, ok := id.In(hitLocalDirectoryRemove); ok && index < len(picker.visible) {
			directory := picker.visible[index]
			state.localDirectoryHistory = slices.DeleteFunc(state.localDirectoryHistory, func(value string) bool { return value == directory })
			state.save()
			state.refreshLocalDirectories()
		}
	}
}

func (state *State) chooseLocalDirectory(directory string) {
	picker := &state.localDirectoryPicker
	info, err := os.Stat(directory)
	if err != nil || !info.IsDir() || !validLocalDirectory(directory) {
		picker.errorText = state.messages.LocalDirectoryUnavailable
		return
	}
	directory = filepath.Clean(directory)
	rollback := func() {}
	state.pendingWorkspace, state.pendingPane = 0, 0
	if session, ok := state.sessionByIDPointer(picker.target); ok {
		rollback = state.localDirectoryLayoutRollback(session)
		if !state.prepareAdjacentTerminal(session, state.messages.AddLocalTerminal) {
			return
		}
	}
	workspace := state.pendingWorkspace
	state.pendingDirectory = directory
	if state.openLocalTerminal() == nil {
		rollback()
		state.pendingWorkspace, state.pendingPane, state.pendingDirectory = 0, 0, ""
		picker.errorText = state.messages.LocalDirectoryUnavailable
		return
	}
	if workspace != 0 {
		state.rebuildManagedWorkspace(workspace)
	}
	state.localDirectoryHistory = slices.DeleteFunc(state.localDirectoryHistory, func(value string) bool { return value == directory })
	state.localDirectoryHistory = normalizeLocalDirectoryHistory(append([]string{directory}, state.localDirectoryHistory...))
	state.modal = modalNone
	state.save()
}

const localDirectoryHistoryLimit = 50

func normalizeLocalDirectoryHistory(history []string) []string {
	result := make([]string, 0, min(len(history), localDirectoryHistoryLimit))
	seen := make(map[string]bool)
	for _, directory := range history {
		if !validLocalDirectory(directory) {
			continue
		}
		directory = filepath.Clean(directory)
		if seen[directory] {
			continue
		}
		seen[directory] = true
		result = append(result, directory)
		if len(result) == localDirectoryHistoryLimit {
			break
		}
	}
	return result
}

// localDirectoryLayoutRollback 保留拆分前的布局、焦点及显示模式。
func (state *State) localDirectoryLayoutRollback(session *Session) func() {
	workspace, _, _ := state.deck.PaneForSession(layoutcore.SessionID(session.ID))
	layout, _ := state.deck.Layout(workspace)
	spec := layout.Spec()
	panes := state.deck.Panes(workspace, nil)
	order := make([]layoutcore.PaneID, len(panes))
	for i, pane := range panes {
		order[i] = pane.ID
	}
	active, maximized := state.deck.ActivePane(workspace), state.deck.PaneMaximized(workspace)
	managed, hasManaged := state.managedWorkspaceLayouts[workspace]
	temporary, hasTemporary := state.temporaryWorkspaceLayouts[workspace]
	return func() {
		for _, pane := range state.deck.Panes(workspace, nil) {
			if !slices.Contains(order, pane.ID) && pane.SessionID == 0 {
				state.deck.ClosePane(workspace, pane.ID)
			}
		}
		state.deck.SetLayout(workspace, order, spec)
		state.deck.ActivatePane(workspace, active)
		state.deck.SetPaneMaximized(workspace, maximized)
		if hasManaged {
			state.managedWorkspaceLayouts[workspace] = managed
		}
		if hasTemporary {
			state.temporaryWorkspaceLayouts[workspace] = temporary
		}
		state.syncActiveSession()
		state.refreshWorkspaceViews()
		state.fitTerminals()
	}
}
