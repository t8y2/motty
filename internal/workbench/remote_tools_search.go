package workbench

import (
	"fmt"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

var (
	hitRemoteSearch      = tree.ID(remoteToolsHits, "search")
	hitRemoteSearchScope = tree.ID(remoteToolsHits, "search-scope")
)

const remoteSearchHeight = float32(60)

type remoteSearch struct {
	editor text.Editor
	input  app.TextInput
	ready  bool
	// field and fieldBound are the page field the tools window declares over the
	// editor; the main window edits the same editor through RemoteToolsInput.
	field      ui.Field
	fieldBound bool
}

func (search *remoteSearch) queryEditor() *text.Editor {
	if !search.ready {
		search.editor.Restrict(sftpFilterRune, 256)
		search.ready = true
	}
	return &search.editor
}

// selectQuery selects the whole query, so the Find shortcut's next typed rune
// or paste replaces it instead of appending to it.
func (search *remoteSearch) selectQuery() {
	editor := search.queryEditor()
	editor.SetSelection(text.Range{Start: 0, End: editor.RuneCount()})
}

// RemoteToolsInput exposes native editing without putting user queries in any
// remote command. Each tab owns its draft independently.
func (state *State) RemoteToolsInput(context app.TextInputContext) *app.TextInput {
	if !state.remoteToolsVisible() || context.ID != hitRemoteSearch {
		return nil
	}
	tab := state.remote.tab
	search := &state.remote.searches[tab]
	search.input.Editor = nil
	search.input.State = app.TextStateOf(search.queryEditor())
	search.input.OnEvent = func(event app.TextEvent) (app.TextState, error) {
		before := search.editor.String()
		event.Apply(&search.editor)
		if before != search.editor.String() && state.remoteToolsVisible() && state.remote.tab == tab {
			state.remote.page = 0
			state.rebuildRemoteRows()
		}
		next := app.TextStateOf(&search.editor)
		next.Redraw = true
		return next, nil
	}
	return &search.input
}

func (state *State) filterRemoteRows() {
	view := &state.remote
	view.sourceTotal = len(view.rows)
	query := strings.ToLower(strings.TrimSpace(view.searches[view.tab].queryEditor().String()))
	if query != "" {
		kept := view.rows[:0]
		for _, row := range view.rows {
			for _, cell := range row.cells {
				if strings.Contains(strings.ToLower(cell), query) {
					kept = append(kept, row)
					break
				}
			}
		}
		clear(view.rows[len(kept):])
		view.rows = kept
	}
	selectedVisible := false
	for _, row := range view.rows {
		selectedVisible = selectedVisible || row.key == view.selected
	}
	if !selectedVisible {
		view.selected = ""
	}
	view.searchSummary = fmt.Sprintf(state.messages.Tools.RemoteSearchCountFmt, len(view.rows), view.sourceTotal)
	if view.tab == remoteProcesses && (view.processesTruncated || view.commandsTruncated) {
		view.searchSummary += " · " + state.messages.Tools.RemoteSearchLimited
	}
}

func (state *State) clearRemoteSearch() {
	search := &state.remote.searches[state.remote.tab]
	search.editor = *text.NewEditor("")
	search.ready = false
	state.remote.page = 0
	state.rebuildRemoteRows()
}

func (state *State) remoteSearchShortcut(event gfx.KeyEvent) gfx.EventResult {
	if !state.remoteToolsVisible() || event.Release {
		return gfx.EventResult{}
	}
	chord, ok := keymap.FromEvent(event)
	if !ok || state.shortcuts.actionFor(chord) != shortcutFind {
		return gfx.EventResult{}
	}
	if chord.Modifiers&(gfx.ModifierControl|gfx.ModifierMeta) == 0 && event.Target == hitRemoteSearch {
		return gfx.EventResult{}
	}
	state.remote.searches[state.remote.tab].selectQuery()
	return gfx.EventResult{Handled: true, Repaint: true, Focus: hitRemoteSearch, FocusVisible: true}
}

func (state *State) remoteSearchKey(event gfx.KeyEvent) gfx.EventResult {
	if !state.remoteToolsVisible() || state.remote.auto.open || event.Release || event.Repeat || event.Key != gfx.KeyEscape {
		return gfx.EventResult{}
	}
	search := &state.remote.searches[state.remote.tab]
	if search.queryEditor().String() == "" {
		// No query to clear: Escape belongs to the dialog's dismissal.
		return gfx.EventResult{}
	}
	if _, composing := search.queryEditor().Composition(); composing && event.Target == hitRemoteSearch {
		return gfx.EventResult{Handled: true}
	}
	state.clearRemoteSearch()
	return gfx.EventResult{Handled: true, Repaint: true, Focus: hitRemoteSearch, FocusVisible: true}
}
