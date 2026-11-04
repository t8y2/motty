package workbench

import (
	"fmt"
	"math"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

// commandPaletteHits is the place of the controls this file paints.
var commandPaletteHits = tree.Feature("command-palette")

var (
	hitCommandPalette        = tree.ID(commandPaletteHits, "command-palette")
	hitCommandPaletteDismiss = tree.ID(commandPaletteHits, "command-palette-dismiss")
	hitCommandPaletteClose   = tree.ID(commandPaletteHits, "command-palette-close")
	hitCommandPaletteSearch  = tree.ID(commandPaletteHits, "command-palette-search")
	hitCommandPaletteItems   = tree.Sub(commandPaletteHits, tree.Name("command-palette-items"))
	hitCommandPaletteBar     = tree.ID(commandPaletteHits, "command-palette-scrollbar")
	// The group headings of the new-tab picker are places too: the pointer over
	// one shows no choice lit, and a press on one chooses nothing.
	hitCommandPaletteHeaders = tree.Sub(commandPaletteHits, tree.Name("command-palette-group"))
)

type commandPaletteEntry struct {
	label       string
	recency     string
	recencyHint string
	detail      string // shown after the label, muted (a host's address)
	search      string
	target      ui.ID
	// header marks a group of the new-tab picker, not a choice in it. Putting
	// the cursor on one shows nothing selected, and the keyboard steps over it.
	header bool
}

type commandPalette struct {
	search   text.Editor
	rawQuery string
	query    string
	filtered bool
	// entries is everything the palette offers and visible the indexes of those
	// the search keeps. They are kept only while the palette is open: a list as
	// long as the saved snippets or hosts costs nothing once it is closed.
	entries []commandPaletteEntry
	visible []int
	// selected and top are places in visible: the choice, and the first row the
	// list shows; top keeps the selection in view and the wheel moves it.
	selected, top int
	// bar is the scrollbar as last painted, and barGrab where on its thumb a
	// drag took hold; dragging is set from the press on the bar to its release.
	bar      components.ScrollAreaGeometry
	barGrab  float32
	dragging bool
	// connect makes the list a picker of where a new tab connects, instead of
	// the palette of commands.
	connect bool
	// snippets makes the list a picker of the saved snippets to send to the
	// active terminal, opened from the terminal toolbar.
	snippets bool
}

func (state *State) openCommandPalette() {
	state.commandPalette.connect, state.commandPalette.snippets = false, false
	state.modal = modalCommandPalette
	state.commandPalette.search = text.Editor{}
	state.commandPalette.query = ""
	state.commandPalette.rawQuery = ""
	state.commandPalette.filtered = false
	state.rebuildCommandPalette()
}

func (state *State) closeCommandPalette() {
	state.commandPalette.connect, state.commandPalette.snippets = false, false
	state.modal = modalNone
	state.commandPalette.search = text.Editor{}
	state.commandPalette.query = ""
	state.commandPalette.rawQuery = ""
	state.commandPalette.filtered = false
	state.commandPalette.entries = nil
	state.commandPalette.visible = nil
	state.commandPalette.dragging = false
	state.commandPalette.selected = 0
	state.commandPalette.top = 0
}

func (state *State) rebuildCommandPalette() {
	state.ensureWorkspaceDeck()
	palette := &state.commandPalette
	palette.entries = palette.entries[:0]
	palette.selected, palette.top = 0, 0
	messages := state.messages
	if palette.connect {
		state.rebuildConnectPicker()
		return
	}
	if palette.snippets {
		state.rebuildSnippetPicker()
		return
	}
	palette.add(messages.NavHosts, hitHosts)
	palette.add(messages.NavWorkspaces, hitWorkspaces)
	palette.add(messages.NavKeychain, hitKeychain)
	palette.add(messages.NavPortForwarding, hitPortForwarding)
	palette.add(messages.NavProxies, hitProxies)
	palette.add(messages.NavKnownHosts, hitKnownHosts)
	palette.add(messages.NavSettings, hitSettings)
	palette.add(messages.NavSnippets, hitSnippets)
	palette.add(messages.SessionLogs.Title, hitSessionLogs)
	state.devAddPaletteCommands(palette)
	if state.options.NewWindow != nil {
		palette.add(messages.NewWindow, hitNewWindow)
	}
	palette.add(messages.LocalTerminal, hitLocalTerminal)
	palette.add(messages.NewHost, hitAddHost)
	palette.add(messages.SSHConfigImport, hitImportSSHConfig)
	palette.add(messages.Tools.ImportSessions, hitImportSessions)
	if state.deck.ActiveWorkspace() != 0 {
		palette.add(messages.AIPanel.Toggle, hitAIToggle)
	}
	if _, ok := state.sessionByIDPointer(state.activeSessionID); ok {
		palette.add(messages.PanePrevious, hitPanePrevious)
		palette.add(messages.PaneNext, hitPaneNext)
		palette.add(messages.PaneSplitHorizontal, hitPaneSplitHorizontal)
		palette.add(messages.PaneSplitVertical, hitPaneSplitVertical)
		palette.add(messages.PaneClose, hitPaneClose)
		palette.add(messages.SFTPOpen, hitSFTPOpen)
		if session, _ := state.sessionByIDPointer(state.activeSessionID); session != nil {
			if _, ok := state.localProxyTarget(session); ok && !session.host.isDesktop() {
				palette.addDetailed(messages.LocalProxy.Title, state.sessionAddress(session.host), hitLocalProxyOpen)
			}
		}
		if session, _ := state.sessionByIDPointer(state.activeSessionID); session != nil && state.remoteToolsOffered(session) {
			palette.add(messages.Tools.RemoteMenu, hitRemoteToolsOpen)
		}
		palette.add(messages.History.Title, hitHistoryOpen)
		palette.add(messages.Completion.Title, hitCompletionOpen)
		palette.add(messages.ConnRetry, hitConnRetry)
		palette.add(messages.ConnExportLog, hitConnExport)
		if state.paneInSplit(state.activeSessionID) {
			label := messages.Tools.BroadcastStart
			if state.broadcasting(state.activeSessionID) {
				label = messages.Tools.BroadcastStop
			}
			palette.add(label, hitBroadcast)
		}
		for index, entry := range state.snippets.list {
			detail := ""
			if entry.Shortcut.valid() {
				detail = entry.Shortcut.label()
			}
			palette.addDetailed(fmt.Sprintf(messages.Tools.SnippetPaletteFmt, entry.Name), detail, hitSnippetSendList.At(index))
		}
	}
	for index := range state.sessions {
		session := &state.sessions[index]
		palette.add(fmt.Sprintf(messages.CommandSwitchSessionFmt, session.Title), hitCommandSessionList.At(int(session.ID)))
	}
	palette.rawQuery = ""
	palette.query = ""
	palette.filtered = false
	state.refreshCommandPalette()
}

func (palette *commandPalette) add(label string, target ui.ID) {
	palette.addDetailed(label, "", target)
}

func (palette *commandPalette) addDetailed(label, detail string, target ui.ID) {
	palette.entries = append(palette.entries, commandPaletteEntry{
		label: label, detail: detail, search: strings.ToLower(label + " " + detail), target: target,
	})
}

// addHeader names a group in the new-tab picker: a row to read, not to choose.
func (palette *commandPalette) addHeader(label, detail string) {
	palette.entries = append(palette.entries, commandPaletteEntry{label: label, detail: detail, header: true})
}

func (state *State) refreshCommandPalette() {
	palette := &state.commandPalette
	rawQuery := strings.TrimSpace(palette.search.String())
	if palette.filtered && rawQuery == palette.rawQuery {
		return
	}
	if palette.connect && state.recentConnectSort {
		state.rebuildRecentConnectPicker(rawQuery)
		return
	}
	query := strings.ToLower(rawQuery)
	var keywords []string
	if palette.connect {
		keywords = strings.Fields(query)
	}
	palette.rawQuery = rawQuery
	palette.query = query
	palette.filtered = true
	palette.visible = palette.visible[:0]
	for index := range palette.entries {
		if palette.entries[index].header {
			// Group names only head the unfiltered list: a search shows what
			// matches, not the folders the matches came from.
			if query == "" {
				palette.visible = append(palette.visible, index)
			}
			continue
		}
		if query != "" {
			search := palette.entries[index].search
			if palette.connect {
				if !matchesSearchWords(search, keywords) {
					continue
				}
			} else if !strings.Contains(search, query) {
				continue
			}
		}
		palette.visible = append(palette.visible, index)
	}
	palette.selected = max(palette.choiceAt(0, 1), 0)
	palette.top = 0
}

// choiceAt returns the first choice from index and in direction, stepping over
// headings. It returns -1 when the direction runs out of choices.
func (palette *commandPalette) choiceAt(index, direction int) int {
	for index >= 0 && index < len(palette.visible) {
		if !palette.entries[palette.visible[index]].header {
			return index
		}
		index += direction
	}
	return -1
}

// followCommandPalette moves the window of rows so that the selected one shows.
func (palette *commandPalette) follow() {
	if palette.selected < palette.top {
		palette.top = palette.selected
	}
	if palette.selected >= palette.top+commandPaletteVisibleRows {
		palette.top = palette.selected - commandPaletteVisibleRows + 1
	}
}

// scrollCommandPalette is the wheel over the open palette: the list moves by a row.
func (state *State) scrollCommandPalette(event ui.ScrollEvent) ui.EventResult {
	state.refreshCommandPalette()
	palette := &state.commandPalette
	beforeTop, beforeSelected := palette.top, palette.selected
	last := max(len(palette.visible)-commandPaletteVisibleRows, 0)
	// A notch moves one row; a long gesture moves as many rows as it spans.
	rows := max(int(math.Abs(float64(event.DeltaY))/34), 1)
	switch {
	case event.DeltaY > 0:
		palette.top = min(palette.top+rows, last)
	case event.DeltaY < 0:
		palette.top = max(palette.top-rows, 0)
	}
	target := event.Target
	if visible, ok := target.In(hitCommandPaletteItems); ok {
		row := palette.top + visible
		if row < len(palette.visible) && !palette.entries[palette.visible[row]].header {
			palette.selected = row
		}
	}
	return ui.EventResult{Handled: true, Repaint: palette.top != beforeTop || palette.selected != beforeSelected}
}

func (state *State) clickCommandPalette(id ui.ID) {
	switch id {
	case hitCommandPaletteDismiss, hitCommandPaletteClose:
		state.closeCommandPalette()
	case hitConnectSortDefault, hitConnectSortRecent:
		if !state.commandPalette.connect {
			return
		}
		state.recentConnectSort = id == hitConnectSortRecent
		state.rebuildCommandPalette()
		state.save()
	case hitCommandPaletteSearch:
		return
	default:
		visible, ok := id.In(hitCommandPaletteItems)
		if !ok {
			return
		}
		state.refreshCommandPalette()
		visible += state.commandPalette.top
		if visible >= len(state.commandPalette.visible) {
			return
		}
		state.executeCommandPalette(visible)
	}
}

func (state *State) executeCommandPalette(visible int) {
	palette := &state.commandPalette
	if visible >= len(palette.visible) {
		return
	}
	target := palette.entries[palette.visible[visible]].target
	if target.IsZero() {
		return
	}
	state.closeCommandPalette()
	// The entry stands for the row rather than being it, so the row's icon stays
	// still: only a click on the row itself plays its motion.
	state.click(target, false)
}

// dragCommandPaletteBar turns the thumb's position into the first row shown.
func (palette *commandPalette) dragBar(y float32) {
	offset := palette.bar.VerticalOffsetForThumb(y - palette.barGrab)
	last := max(len(palette.visible)-commandPaletteVisibleRows, 0)
	palette.top = min(max(int(offset/commandPaletteRowPitch+0.5), 0), last)
}

func (state *State) commandPalettePointer(event ui.PointerEvent) (ui.EventResult, bool) {
	if state.modal != modalCommandPalette {
		return ui.EventResult{}, false
	}
	palette := &state.commandPalette
	// A press on the scrollbar takes the pointer for the drag: until it is
	// released the rows under it must not take the hover, or the highlight
	// stays on whichever row the drag started over.
	switch {
	case event.Kind == ui.PointerDown && event.Target == hitCommandPaletteBar && event.Button == ui.PointerButtonPrimary:
		thumb := palette.bar.VerticalThumb
		palette.barGrab = thumb.Height / 2
		if event.Y >= thumb.Y && event.Y < thumb.Y+thumb.Height {
			palette.barGrab = event.Y - thumb.Y
		}
		palette.dragging = true
		palette.dragBar(event.Y)
		return ui.EventResult{Handled: true, Repaint: true}, true
	case palette.dragging:
		switch event.Kind {
		case ui.PointerMove:
			palette.dragBar(event.Y)
		case ui.PointerUp, ui.PointerExit:
			palette.dragging = false
		}
		return ui.EventResult{Handled: true, Repaint: true}, true
	}
	visible, ok := event.Target.In(hitCommandPaletteItems)
	if !ok {
		return ui.EventResult{}, false
	}
	row := palette.top + visible
	if row >= len(palette.visible) {
		return ui.EventResult{}, false
	}
	if palette.entries[palette.visible[row]].header {
		return ui.EventResult{}, false
	}
	if event.Kind == ui.PointerMove && palette.selected != row {
		palette.selected = row
		return ui.EventResult{Handled: true, Repaint: true}, true
	}
	return ui.EventResult{}, false
}

func (state *State) commandPaletteKey(event ui.KeyEvent) (ui.EventResult, bool) {
	if state.modal != modalCommandPalette {
		return ui.EventResult{}, false
	}
	state.refreshCommandPalette()
	palette := &state.commandPalette
	if event.Key == ui.KeyEscape {
		state.closeCommandPalette()
		return ui.EventResult{Handled: true, Repaint: true}, true
	}
	if palette.connect {
		switch event.Target {
		case hitConnectSortDefault, hitConnectSortRecent:
			// The switch is a radio group of its own: the arrows move between its
			// two options, and choosing one puts the keyboard back in the search
			// field so typing filters the list at once.
			options := state.connectSortOptions()
			if next, handled := components.SegmentedControlKeyDown(event.Target, options[:], components.DirectionLTR, event.Key); handled && next != event.Target && !event.Repeat {
				state.clickCommandPalette(next)
				return ui.EventResult{Handled: true, Repaint: true, Focus: hitCommandPaletteSearch}, true
			}
			if event.Key == ui.KeyEnter || event.Key == ui.KeySpace {
				return ui.EventResult{Handled: true, Focus: hitCommandPaletteSearch}, true
			}
		}
	}
	inSearch := event.Target == hitCommandPaletteSearch
	_, inItems := event.Target.In(hitCommandPaletteItems)
	if !inSearch && !inItems {
		return ui.EventResult{}, false
	}
	if inSearch && event.Key == ui.KeyEnter && !event.Repeat {
		state.executeCommandPalette(palette.selected)
		return ui.EventResult{Handled: true, Repaint: true, Focus: state.modalSearchFocus()}, true
	}
	if inItems && (event.Key == ui.KeyEnter || event.Key == ui.KeySpace) && !event.Repeat {
		state.executeCommandPalette(palette.selected)
		return ui.EventResult{Handled: true, Repaint: true, Focus: state.modalSearchFocus()}, true
	}
	row := palette.selected
	move := func(selected, direction int) (ui.EventResult, bool) {
		if len(palette.visible) == 0 {
			return ui.EventResult{Handled: true}, true
		}
		selected = min(max(selected, 0), len(palette.visible)-1)
		selected = palette.choiceAt(selected, direction)
		if selected < 0 {
			// The move ran past the end of the list: it lands on the last
			// choice there is, not on nothing.
			if direction > 0 {
				selected = palette.choiceAt(len(palette.visible)-1, -1)
			} else {
				selected = palette.choiceAt(0, 1)
			}
		}
		if selected < 0 {
			return ui.EventResult{Handled: true}, true
		}
		palette.selected = selected
		palette.follow()
		if inSearch {
			return ui.EventResult{Handled: true, Repaint: true, Focus: hitCommandPaletteSearch}, true
		}
		return ui.EventResult{
			Handled: true, Repaint: true,
			Focus: hitCommandPaletteItems.At(palette.selected - palette.top), FocusVisible: true,
		}, true
	}
	switch event.Key {
	case ui.KeyUp:
		return move(row-1, -1)
	case ui.KeyDown:
		return move(row+1, 1)
	case ui.KeyPageUp:
		return move(row-commandPaletteVisibleRows, -1)
	case ui.KeyPageDown:
		return move(row+commandPaletteVisibleRows, 1)
	}
	if inItems {
		switch event.Key {
		case ui.KeyHome:
			return move(0, 1)
		case ui.KeyEnd:
			return move(len(palette.visible)-1, -1)
		}
	}
	return ui.EventResult{}, false
}

// modalSearchFocus is where the keyboard goes when a palette command has opened
// a picker with a search field of its own; otherwise it asks for nothing.
func (state *State) modalSearchFocus() ui.ID {
	if state.modal == modalHistory {
		return hitHistorySearch
	}
	return ui.ID{}
}
