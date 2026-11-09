package workbench

import (
	"fmt"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
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
	target      gfx.ID
	// searchOnly rows answer the search without appearing in the unfiltered
	// list. The shells behind the Local Terminal row use this: one line until
	// the user expands it, but typing "bash" still finds the shell.
	searchOnly bool
	// header marks a group of the new-tab picker, not a choice in it. Putting
	// the cursor on one shows nothing selected, and the keyboard steps over it.
	header bool
	// foldKey makes a header a row that folds its group: a choice of its own,
	// which opens or closes the rows under it. foldKey names the group and
	// expanded says which way it is now. Headers without one only label.
	foldKey  string
	expanded bool
	// checkable rows are choices that tick on and off without closing the
	// palette: the terminals a command is sent to.
	checkable bool
	checked   bool
}

// folds reports whether the entry is a header that opens and closes a group.
func (entry *commandPaletteEntry) folds() bool { return entry.header && entry.foldKey != "" }

// isLabel reports whether the entry only names a group: the pointer and the
// keyboard pass over it.
func (entry *commandPaletteEntry) isLabel() bool { return entry.header && !entry.folds() }

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
	// selected is a logical row in visible; scroll is the pixel offset.
	selected int
	scroll   float32
	listRect gfx.Rect
	// bar is the scrollbar as last painted, and barGrab where on its thumb a
	// drag took hold; dragging is set from the press on the bar to its release.
	bar      components.ScrollAreaGeometry
	barGrab  float32
	dragging bool
	// connect makes the list a picker of where a new tab connects, instead of
	// the palette of commands.
	connect    bool
	workspaces bool
	// localShells expands the Local Terminal row into the shells this computer
	// can run, inside the same searchable new-tab list.
	localShells bool
	// snippets makes the list a picker of the saved snippets to send to the
	// active terminal, opened from the terminal toolbar.
	snippets bool
	// expandedGroups are the snippet groups the picker shows open, by fold key.
	// It starts empty, so every group begins closed and a search is what looks
	// inside them.
	expandedGroups []string
}

func (state *State) openCommandPalette() {
	state.commandPalette.connect, state.commandPalette.snippets, state.commandPalette.workspaces, state.commandPalette.localShells = false, false, false, false
	state.modal = modalCommandPalette
	state.commandPalette.search = text.Editor{}
	state.commandPalette.query = ""
	state.commandPalette.rawQuery = ""
	state.commandPalette.filtered = false
	state.rebuildCommandPalette()
}

func (state *State) closeCommandPalette() {
	state.commandPalette.connect, state.commandPalette.snippets, state.commandPalette.workspaces, state.commandPalette.localShells = false, false, false, false
	state.modal = modalNone
	state.commandPalette.search = text.Editor{}
	state.commandPalette.query = ""
	state.commandPalette.rawQuery = ""
	state.commandPalette.filtered = false
	state.commandPalette.entries = nil
	state.commandPalette.visible = nil
	state.commandPalette.dragging = false
	state.commandPalette.selected = 0
	state.commandPalette.scroll = 0
}

func (state *State) rebuildCommandPalette() {
	state.ensureWorkspaceDeck()
	palette := &state.commandPalette
	palette.entries = palette.entries[:0]
	palette.selected, palette.scroll = 0, 0
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
	palette.add(state.noteText("Notes", "笔记"), hitNotes)
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
		palette.add(messages.MenuRenameTerminal, hitPaneRename)
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
			palette.addDetailed(fmt.Sprintf(messages.Tools.SnippetPaletteFmt, entry.Name), snippetDetail(&entry), hitSnippetSendList.At(index))
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

func (palette *commandPalette) add(label string, target gfx.ID) {
	palette.addDetailed(label, "", target)
}

func (palette *commandPalette) addDetailed(label, detail string, target gfx.ID) {
	palette.entries = append(palette.entries, commandPaletteEntry{
		label: label, detail: detail, search: strings.ToLower(label + " " + detail), target: target,
	})
}

// addCandidate adds a row of the new-tab picker, keeping whether a search is
// the only way it reaches the list.
func (palette *commandPalette) addCandidate(item connectPickerCandidate) {
	palette.entries = append(palette.entries, commandPaletteEntry{
		label: item.label, detail: item.detail, search: strings.ToLower(item.label + " " + item.detail),
		target: item.target, searchOnly: item.searchOnly,
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
	if palette.connect && !palette.workspaces && state.recentConnectSort {
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
		entry := &palette.entries[index]
		if entry.header {
			// Group names only head the unfiltered list: a search shows what
			// matches, not the folders the matches came from.
			if query == "" {
				palette.visible = append(palette.visible, index)
			}
			continue
		}
		if entry.searchOnly && query == "" && !palette.localShells {
			continue
		}
		if query != "" {
			search := entry.search
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
	palette.scroll = 0
}

// choiceAt returns the first choice from index and in direction, stepping over
// headings. It returns -1 when the direction runs out of choices.
func (palette *commandPalette) choiceAt(index, direction int) int {
	for index >= 0 && index < len(palette.visible) {
		if entry := &palette.entries[palette.visible[index]]; !entry.header || entry.folds() {
			return index
		}
		index += direction
	}
	return -1
}

const commandPaletteRowStep float32 = commandPaletteRowPitch
const commandPaletteListHeight float32 = commandPaletteVisibleRows*commandPaletteRowStep - 4

func (palette *commandPalette) contentHeight() float32 {
	return max(0, float32(len(palette.visible))*commandPaletteRowStep-4)
}

func (palette *commandPalette) maxScroll() float32 {
	return max(0, palette.contentHeight()-commandPaletteListHeight)
}

// follow reveals the selected row only when keyboard navigation asks for it.
func (palette *commandPalette) follow() {
	top := float32(palette.selected) * commandPaletteRowStep
	palette.scroll = min(max(palette.scroll, top+30-commandPaletteListHeight), top)
	palette.scroll = min(max(palette.scroll, 0), palette.maxScroll())
}

func (state *State) scrollCommandPalette(event gfx.ScrollEvent) gfx.EventResult {
	state.refreshCommandPalette()
	palette := &state.commandPalette
	before, selected := palette.scroll, palette.selected
	palette.scroll = min(max(palette.scroll+event.StepY(commandPaletteRowStep), 0), palette.maxScroll())
	// The new-tab picker keeps its selection while scrolling, like the font picker.
	// Other palettes resolve the row at the pointer after moving content.
	if !palette.connect && !palette.dragging && event.DeltaY != 0 && pointInRect(event.X, event.Y, palette.listRect) {
		row := int((event.Y - palette.listRect.Y + palette.scroll + 2) / commandPaletteRowStep)
		if row >= 0 && row < len(palette.visible) && !palette.entries[palette.visible[row]].isLabel() {
			palette.selected = row
		}
	}
	return gfx.EventResult{Handled: true, Repaint: before != palette.scroll || selected != palette.selected}
}

func (state *State) clickCommandPalette(id gfx.ID) {
	switch id {
	case hitCommandPaletteDismiss, hitCommandPaletteClose:
		state.closeCommandPalette()
	case hitConnectSortDefault, hitConnectSortWorkspaces, hitConnectSortRecent:
		if !state.commandPalette.connect {
			return
		}
		state.commandPalette.workspaces = id == hitConnectSortWorkspaces
		if !state.commandPalette.workspaces {
			state.recentConnectSort = id == hitConnectSortRecent
			state.save()
		}
		state.rebuildCommandPalette()
	case hitCommandPaletteSearch:
		return
	default:
		if state.clickSnippetSend(id) {
			return
		}
		if id == hitConnectPickShellPicker {
			state.commandPalette.localShells = !state.commandPalette.localShells
			state.rebuildCommandPalette()
			return
		}
		// The pencil of a host row opens the host for editing instead of
		// connecting: the palette closes and the edit dialog takes over.
		if id.Scope() == hitConnectPickEdit {
			state.openEditHostID(uint32(item(id)))
			return
		}
		visible, ok := id.In(hitCommandPaletteItems)
		if !ok {
			return
		}
		state.refreshCommandPalette()
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
	if entry := &palette.entries[palette.visible[visible]]; entry.folds() {
		state.foldSnippetGroup(entry.foldKey, !entry.expanded)
		return
	} else if entry.checkable {
		state.toggleSendSession(uint32(item(entry.target)))
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

// dragBar keeps scrollbar dragging as precise as trackpad scrolling.
func (palette *commandPalette) dragBar(y float32) {
	palette.scroll = min(max(palette.bar.VerticalOffsetForThumb(y-palette.barGrab), 0), palette.maxScroll())
}

func (state *State) commandPalettePointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	if state.modal != modalCommandPalette {
		return gfx.EventResult{}, false
	}
	palette := &state.commandPalette
	// A press on the scrollbar takes the pointer for the drag: until it is
	// released the rows under it must not take the hover, or the highlight
	// stays on whichever row the drag started over.
	switch {
	case event.Kind == gfx.PointerDown && event.Target == hitCommandPaletteBar && event.Button == gfx.PointerButtonPrimary:
		thumb := palette.bar.VerticalThumb
		palette.barGrab = thumb.Height / 2
		if event.Y >= thumb.Y && event.Y < thumb.Y+thumb.Height {
			palette.barGrab = event.Y - thumb.Y
		}
		palette.dragging = true
		palette.dragBar(event.Y)
		return gfx.EventResult{Handled: true, Repaint: true}, true
	case palette.dragging:
		switch event.Kind {
		case gfx.PointerMove:
			palette.dragBar(event.Y)
		case gfx.PointerUp, gfx.PointerExit:
			palette.dragging = false
		}
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	visible, ok := event.Target.In(hitCommandPaletteItems)
	if !ok {
		return gfx.EventResult{}, false
	}
	row := visible
	if row >= len(palette.visible) {
		return gfx.EventResult{}, false
	}
	if palette.entries[palette.visible[row]].isLabel() {
		return gfx.EventResult{}, false
	}
	if event.Kind == gfx.PointerMove && palette.selected != row {
		palette.selected = row
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	return gfx.EventResult{}, false
}

func (state *State) commandPaletteKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if state.modal != modalCommandPalette {
		return gfx.EventResult{}, false
	}
	state.refreshCommandPalette()
	palette := &state.commandPalette
	if event.Key == gfx.KeyEscape {
		state.closeCommandPalette()
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	if palette.connect {
		switch event.Target {
		case hitConnectSortDefault, hitConnectSortWorkspaces, hitConnectSortRecent:
			// The switch is a radio group of its own: the arrows move between its
			// options, and choosing one puts the keyboard back in the search
			// field so typing filters the list at once.
			options := state.connectSortOptions()
			if next, handled := components.SegmentedControlKeyDown(event.Target, options[:], components.DirectionLTR, event.Key); handled && next != event.Target && !event.Repeat {
				state.clickCommandPalette(next)
				return gfx.EventResult{Handled: true, Repaint: true, Focus: hitCommandPaletteSearch}, true
			}
			if event.Key == gfx.KeyEnter || event.Key == gfx.KeySpace {
				return gfx.EventResult{Handled: true, Focus: hitCommandPaletteSearch}, true
			}
		}
	}
	if palette.snippets {
		switch event.Target {
		case hitSendTargetCurrent, hitSendTargetAll, hitSendTargetChosen:
			// Like the new-tab picker's switch: the arrows move between the
			// options, and choosing one returns the keyboard to the search field.
			options := state.sendTargetOptions()
			if next, handled := components.SegmentedControlKeyDown(event.Target, options[:], components.DirectionLTR, event.Key); handled && next != event.Target && !event.Repeat {
				state.clickCommandPalette(next)
				return gfx.EventResult{Handled: true, Repaint: true, Focus: hitCommandPaletteSearch}, true
			}
		case hitSnippetSendInput:
			if event.Key == gfx.KeyEnter && !event.Repeat {
				state.submitSend()
				return gfx.EventResult{Handled: true, Repaint: true}, true
			}
		}
	}
	inSearch := event.Target == hitCommandPaletteSearch
	_, inItems := event.Target.In(hitCommandPaletteItems)
	if !inSearch && !inItems {
		return gfx.EventResult{}, false
	}
	if inSearch && event.Key == gfx.KeyEnter && !event.Repeat {
		state.executeCommandPalette(palette.selected)
		return gfx.EventResult{Handled: true, Repaint: true, Focus: state.modalSearchFocus()}, true
	}
	if inItems && (event.Key == gfx.KeyEnter || event.Key == gfx.KeySpace) && !event.Repeat {
		state.executeCommandPalette(palette.selected)
		return gfx.EventResult{Handled: true, Repaint: true, Focus: state.modalSearchFocus()}, true
	}
	row := palette.selected
	if palette.snippets && !event.Repeat && row >= 0 && row < len(palette.visible) {
		// A group row opens with Right and closes with Left, like a tree.
		if entry := &palette.entries[palette.visible[row]]; entry.folds() && (inItems || inSearch && palette.search.String() == "") {
			switch {
			case event.Key == gfx.KeyRight && !entry.expanded, event.Key == gfx.KeyLeft && entry.expanded:
				state.foldSnippetGroup(entry.foldKey, event.Key == gfx.KeyRight)
				return gfx.EventResult{Handled: true, Repaint: true}, true
			}
		}
	}
	if inItems && palette.connect && !event.Repeat {
		toggleShells := func(expand bool) (gfx.EventResult, bool) {
			if palette.localShells == expand {
				return gfx.EventResult{}, false
			}
			palette.localShells = expand
			state.rebuildCommandPalette()
			first := palette.choiceAt(0, 1)
			if first < 0 {
				return gfx.EventResult{Handled: true, Repaint: true}, true
			}
			return gfx.EventResult{Handled: true, Repaint: true, Focus: hitCommandPaletteItems.At(first), FocusVisible: true}, true
		}
		switch event.Key {
		case gfx.KeyRight:
			if row >= 0 && row < len(palette.visible) {
				entry := palette.entries[palette.visible[row]]
				if entry.target == hitConnectPickLocal && localShellSettingVisible() && len(state.localShellMenu.choices) > 1 {
					return toggleShells(true)
				}
			}
		case gfx.KeyLeft:
			return toggleShells(false)
		}
	}
	move := func(selected, direction int) (gfx.EventResult, bool) {
		if len(palette.visible) == 0 {
			return gfx.EventResult{Handled: true}, true
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
			return gfx.EventResult{Handled: true}, true
		}
		palette.selected = selected
		palette.follow()
		if inSearch {
			return gfx.EventResult{Handled: true, Repaint: true, Focus: hitCommandPaletteSearch}, true
		}
		return gfx.EventResult{
			Handled: true, Repaint: true,
			Focus: hitCommandPaletteItems.At(palette.selected), FocusVisible: true,
		}, true
	}
	switch event.Key {
	case gfx.KeyUp:
		return move(row-1, -1)
	case gfx.KeyDown:
		return move(row+1, 1)
	case gfx.KeyPageUp:
		return move(row-commandPaletteVisibleRows, -1)
	case gfx.KeyPageDown:
		return move(row+commandPaletteVisibleRows, 1)
	}
	if inItems {
		switch event.Key {
		case gfx.KeyHome:
			return move(0, 1)
		case gfx.KeyEnd:
			return move(len(palette.visible)-1, -1)
		}
	}
	return gfx.EventResult{}, false
}

// modalSearchFocus is where the keyboard goes when a palette command has opened
// a picker with a search field of its own; otherwise it asks for nothing.
func (state *State) modalSearchFocus() gfx.ID {
	if state.modal == modalHistory {
		return hitHistorySearch
	}
	return gfx.ID{}
}
