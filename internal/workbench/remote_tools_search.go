package workbench

import (
	"fmt"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/keymap"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

var (
	hitRemoteSearch      = tree.ID(remoteToolsHits, "search")
	hitRemoteSearchClear = tree.ID(remoteToolsHits, "search-clear")
	hitRemoteSearchScope = tree.ID(remoteToolsHits, "search-scope")
)

const remoteSearchHeight = float32(60)

type remoteSearch struct {
	editor text.Editor
	input  app.TextInput
	ready  bool
}

func (search *remoteSearch) queryEditor() *text.Editor {
	if !search.ready {
		search.editor.Restrict(sftpFilterRune, 256)
		search.ready = true
	}
	return &search.editor
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

func (state *State) remoteSearchShortcut(event ui.KeyEvent) ui.EventResult {
	if !state.remoteToolsVisible() || event.Release {
		return ui.EventResult{}
	}
	chord, ok := keymap.FromEvent(event)
	if !ok || state.shortcuts.actionFor(chord) != shortcutFind {
		return ui.EventResult{}
	}
	if chord.Modifiers&(ui.ModifierControl|ui.ModifierMeta) == 0 && event.Target == hitRemoteSearch {
		return ui.EventResult{}
	}
	return ui.EventResult{Handled: true, Repaint: true, Focus: hitRemoteSearch, FocusVisible: true}
}

func (state *State) remoteSearchKey(event ui.KeyEvent) ui.EventResult {
	if !state.remoteToolsVisible() || state.remote.auto.open || event.Release || event.Repeat || event.Key != ui.KeyEscape {
		return ui.EventResult{}
	}
	search := &state.remote.searches[state.remote.tab]
	if event.Target != hitRemoteSearch && search.queryEditor().String() == "" {
		return ui.EventResult{}
	}
	if _, composing := search.queryEditor().Composition(); composing && event.Target == hitRemoteSearch {
		return ui.EventResult{Handled: true}
	}
	result := ui.EventResult{Handled: true, Repaint: true}
	if state.remote.searches[state.remote.tab].editor.String() != "" {
		state.clearRemoteSearch()
	} else {
		result.Focus, result.FocusVisible = hitRemoteRefresh, true
	}
	return result
}

func (state *State) paintRemoteSearch(canvas *ui.Canvas, bounds ui.Rect) {
	query := state.remote.searches[state.remote.tab].queryEditor().String()
	clearSlot := components.InputGroupSlot{}
	if query != "" {
		clearSlot = components.InputGroupSlot{Kind: components.InputGroupSlotButton, ID: hitRemoteSearchClear,
			Icon: lucideIcons[iconClose], Label: state.messages.Tools.RemoteSearchClear, ButtonSize: components.InputGroupButtonIconXS}
	}
	components.PaintInputGroup(canvas, state.Theme(), ui.Rect{X: bounds.X, Y: bounds.Y, Width: bounds.Width, Height: 32}, components.InputGroupSnapshot{
		ID: hitRemoteSearch, Label: state.messages.Tools.RemoteSearch, Value: query,
		Placeholder: state.messages.Tools.RemoteSearchPlaceholder, NativeEditing: canvas.Focused() == hitRemoteSearch && query != "",
		InlineStart: components.InputGroupSlot{Kind: components.InputGroupSlotIcon, Icon: lucideIcons[iconSearch]}, InlineEnd: clearSlot,
	})
	summary := ui.Rect{X: bounds.X, Y: bounds.Y + 36, Width: bounds.Width, Height: 20}
	canvas.Label(elide(canvas, state.remote.searchSummary, metrics.FontSizeS, ui.WeightRegular, summary.Width), summary, ui.TextStyle{Size: metrics.FontSizeS, Color: state.pal.muted})
	canvas.HitWithOptions(hitRemoteSearchScope, summary, state.remote.searchSummary, ui.HitOptions{Role: ui.RoleStatus, SkipFocus: true, Hint: state.remote.searchSummary})
}
