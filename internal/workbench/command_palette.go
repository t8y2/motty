package workbench

import (
	"fmt"
	"math"
	"strings"

	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

// maxCommandPaletteEntries is what the palette can list: its commands and
// sessions, or, as the new-tab picker, this computer, the host groups and as
// many saved hosts as fit. Counts are bytes, so it cannot pass 255.
const maxCommandPaletteEntries = 255

// commandPaletteHits is the place of the controls this file paints.
var commandPaletteHits = tree.Feature("command-palette")

var (
	hitCommandPalette        = tree.ID(commandPaletteHits, "command-palette")
	hitCommandPaletteDismiss = tree.ID(commandPaletteHits, "command-palette-dismiss")
	hitCommandPaletteClose   = tree.ID(commandPaletteHits, "command-palette-close")
	hitCommandPaletteSearch  = tree.ID(commandPaletteHits, "command-palette-search")
	hitCommandPaletteItems   = tree.Sub(commandPaletteHits, tree.Name("command-palette-items"))
	// The group headings of the new-tab picker are places too: the pointer over
	// one shows no choice lit, and a press on one chooses nothing.
	hitCommandPaletteHeaders = tree.Sub(commandPaletteHits, tree.Name("command-palette-group"))
)

type commandPaletteEntry struct {
	label  string
	detail string // shown after the label, muted (a host's address)
	search string
	target ui.ID
	// header marks a group of the new-tab picker, not a choice in it. Putting
	// the cursor on one shows nothing selected, and the keyboard steps over it.
	header bool
}

type commandPalette struct {
	search       text.Editor
	rawQuery     string
	query        string
	filtered     bool
	entries      [maxCommandPaletteEntries]commandPaletteEntry
	visible      [maxCommandPaletteEntries]uint8
	entryCount   uint8
	visibleCount uint8
	selected     uint8
	// top is the first of the visible entries the list shows; it keeps the
	// selected one in view and the wheel moves it.
	top uint8
	// connect makes the list a picker of where a new tab connects, instead of
	// the palette of commands.
	connect bool
}

func (state *State) openCommandPalette() {
	state.commandPalette.connect = false
	state.modal = modalCommandPalette
	state.commandPalette.search = text.Editor{}
	state.commandPalette.query = ""
	state.commandPalette.rawQuery = ""
	state.commandPalette.filtered = false
	state.rebuildCommandPalette()
}

func (state *State) closeCommandPalette() {
	state.commandPalette.connect = false
	state.modal = modalNone
	state.commandPalette.search = text.Editor{}
	state.commandPalette.query = ""
	state.commandPalette.rawQuery = ""
	state.commandPalette.filtered = false
	state.commandPalette.entryCount = 0
	state.commandPalette.visibleCount = 0
	state.commandPalette.selected = 0
	state.commandPalette.top = 0
}

func (state *State) rebuildCommandPalette() {
	state.ensureWorkspaceDeck()
	palette := &state.commandPalette
	palette.entryCount = 0
	palette.selected, palette.top = 0, 0
	messages := state.messages
	if palette.connect {
		state.rebuildConnectPicker()
		return
	}
	palette.add(messages.NavHosts, hitHosts)
	palette.add(messages.NavKeychain, hitKeychain)
	palette.add(messages.NavPortForwarding, hitPortForwarding)
	palette.add(messages.NavProxies, hitProxies)
	palette.add(messages.NavKnownHosts, hitKnownHosts)
	palette.add(messages.NavSettings, hitSettings)
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
		if session, _ := state.sessionByIDPointer(state.activeSessionID); session != nil && state.remoteToolsOffered(session) {
			palette.add(messages.Tools.RemoteMenu, hitRemoteToolsOpen)
		}
		palette.add(messages.History.Title, hitHistoryOpen)
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
			palette.add(fmt.Sprintf(messages.Tools.SnippetPaletteFmt, entry.Name), hitSnippetSendList.At(index))
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
	if palette.entryCount >= uint8(len(palette.entries)) {
		return
	}
	palette.entries[palette.entryCount] = commandPaletteEntry{
		label: label, detail: detail, search: strings.ToLower(label + " " + detail), target: target,
	}
	palette.entryCount++
}

// addHeader names a group in the new-tab picker: a row to read, not to choose.
func (palette *commandPalette) addHeader(label, detail string) {
	if palette.entryCount >= uint8(len(palette.entries)) {
		return
	}
	palette.entries[palette.entryCount] = commandPaletteEntry{label: label, detail: detail, header: true}
	palette.entryCount++
}

func (state *State) refreshCommandPalette() {
	palette := &state.commandPalette
	rawQuery := strings.TrimSpace(palette.search.String())
	if palette.filtered && rawQuery == palette.rawQuery {
		return
	}
	query := strings.ToLower(rawQuery)
	palette.rawQuery = rawQuery
	palette.query = query
	palette.filtered = true
	palette.visibleCount = 0
	for index := uint8(0); index < palette.entryCount; index++ {
		if palette.entries[index].header {
			// Group names only head the unfiltered list: a search shows what
			// matches, not the folders the matches came from.
			if query == "" {
				palette.visible[palette.visibleCount] = index
				palette.visibleCount++
			}
			continue
		}
		if query != "" && !strings.Contains(palette.entries[index].search, query) {
			continue
		}
		palette.visible[palette.visibleCount] = index
		palette.visibleCount++
	}
	palette.selected, palette.top = 0, 0
}

// choiceAt returns the first choice from index and in direction, stepping over
// headings. It returns -1 when the direction runs out of choices.
func (palette *commandPalette) choiceAt(index, direction int) int {
	for index >= 0 && index < int(palette.visibleCount) {
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
	before := palette.top
	last := max(int(palette.visibleCount)-commandPaletteVisibleRows, 0)
	// A notch moves one row; a long gesture moves as many rows as it spans.
	rows := max(int(math.Abs(float64(event.DeltaY))/34), 1)
	switch {
	case event.DeltaY > 0:
		palette.top = uint8(min(int(palette.top)+rows, last))
	case event.DeltaY < 0:
		palette.top = uint8(max(int(palette.top)-rows, 0))
	}
	return ui.EventResult{Handled: true, Repaint: palette.top != before}
}

func (state *State) clickCommandPalette(id ui.ID) {
	switch id {
	case hitCommandPaletteDismiss, hitCommandPaletteClose:
		state.closeCommandPalette()
	case hitCommandPaletteSearch:
		return
	default:
		visible, ok := id.In(hitCommandPaletteItems)
		if !ok {
			return
		}
		state.refreshCommandPalette()
		visible += int(state.commandPalette.top)
		if visible >= int(state.commandPalette.visibleCount) {
			return
		}
		state.executeCommandPalette(uint8(visible))
	}
}

func (state *State) executeCommandPalette(visible uint8) {
	palette := &state.commandPalette
	if visible >= palette.visibleCount {
		return
	}
	target := palette.entries[palette.visible[visible]].target
	if target.IsZero() {
		return
	}
	state.closeCommandPalette()
	state.Click(target)
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
	if event.Target == hitCommandPaletteSearch && event.Key == ui.KeyEnter && !event.Repeat {
		state.executeCommandPalette(palette.selected)
		return ui.EventResult{Handled: true, Repaint: true, Focus: state.modalSearchFocus()}, true
	}
	visible, ok := event.Target.In(hitCommandPaletteItems)
	if !ok {
		return ui.EventResult{}, false
	}
	row := int(palette.top) + visible
	if row >= int(palette.visibleCount) {
		return ui.EventResult{}, false
	}
	palette.selected = uint8(row)
	move := func(selected, direction int) (ui.EventResult, bool) {
		selected = min(max(selected, 0), int(palette.visibleCount)-1)
		selected = palette.choiceAt(selected, direction)
		if selected < 0 {
			// The move ran past the end of the list: it lands on the last
			// choice there is, not on nothing.
			if direction > 0 {
				selected = palette.choiceAt(int(palette.visibleCount)-1, -1)
			} else {
				selected = palette.choiceAt(0, 1)
			}
		}
		if selected < 0 {
			return ui.EventResult{Handled: true}, true
		}
		palette.selected = uint8(selected)
		palette.follow()
		return ui.EventResult{
			Handled: true, Repaint: true,
			Focus: hitCommandPaletteItems.At(int(palette.selected - palette.top)), FocusVisible: true,
		}, true
	}
	switch event.Key {
	case ui.KeyUp:
		return move(row-1, -1)
	case ui.KeyDown:
		return move(row+1, 1)
	case ui.KeyHome:
		return move(0, 1)
	case ui.KeyEnd:
		return move(int(palette.visibleCount)-1, -1)
	case ui.KeyPageUp:
		return move(row-commandPaletteVisibleRows, -1)
	case ui.KeyPageDown:
		return move(row+commandPaletteVisibleRows, 1)
	case ui.KeyEnter, ui.KeySpace:
		if !event.Repeat {
			state.executeCommandPalette(palette.selected)
		}
		return ui.EventResult{Handled: true, Repaint: true, Focus: state.modalSearchFocus()}, true
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
