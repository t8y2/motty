package workbench

import (
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/i18n"
)

// Searching the settings. The search box above the category list looks through
// every setting's name and explanation, and the page shows what matches, each
// with the category it lives in; choosing one opens that category scrolled to it.

// settingsSearchHits is the place of the controls this file paints.
var settingsSearchHits = tree.Feature("settings-search")

var (
	hitSettingsSearch     = tree.ID(settingsSearchHits, "settings-search")
	hitSettingsResultList = tree.Sub(settingsSearchHits, tree.Name("settings-result"))
)

// maxSettingsEntries bounds the index: the settings, the assistant's groups and
// one entry per keyboard shortcut.
const maxSettingsEntries = 80

// settingsEntry is one thing a user may look for.
type settingsEntry struct {
	category    settingsCategory
	label       string
	description string
	// anchor is the text the page paints where the entry lives (a row's name or
	// a group's title), which is what a result scrolls to.
	anchor string
}

// settingsUI is the state of searching, and of scrolling to a result.
type settingsUI struct {
	search text.Editor

	// results are what the query found, built when the query or the language
	// last changed.
	raw      string // the box's text the results were built for
	empty    string // what to say when nothing matched
	language i18n.Language
	built    bool
	count    int
	results  [maxSettingsEntries]settingsEntry

	// jump is the anchor the next paint scrolls to; jumped says it has, so one
	// more paint follows.
	jump   string
	jumped bool

	// controls are the page's controls, recorded while it is declared and
	// painted once it is laid out.
	controls []settingsControl
	options  []components.SegmentOption
	actions  []components.ButtonAction
	colors   []ui.Color
}

// settingsSearching reports whether the page shows search results, and brings
// the results up to date with the box.
func (state *State) settingsSearching() bool {
	view := &state.settings
	raw := view.search.String()
	if view.built && raw == view.raw && state.language == view.language {
		return true
	}
	query := strings.ToLower(strings.TrimSpace(raw))
	if query == "" {
		view.built, view.count, view.raw = false, 0, ""
		return false
	}
	view.raw, view.language, view.built, view.count = raw, state.language, true, 0
	terms := strings.Fields(query)
	state.eachSettingsEntry(func(entry settingsEntry) {
		if view.count == len(view.results) {
			return
		}
		haystack := strings.ToLower(entry.label + " " + entry.description + " " + state.settingsCategoryName(entry.category))
		for _, term := range terms {
			if !strings.Contains(haystack, term) {
				return
			}
		}
		view.results[view.count] = entry
		view.count++
	})
	view.empty = strings.Replace(state.messages.Tools.SearchEmptyFmt, "%s", strings.TrimSpace(raw), 1)
	return true
}

// eachSettingsEntry lists every setting of the page in the order the categories
// are shown.
func (state *State) eachSettingsEntry(add func(settingsEntry)) {
	m, tools, ai := state.messages, state.messages.Tools, state.messages.AISettings
	m2 := state.messages.Autofill
	row := func(category settingsCategory, label, description string) {
		add(settingsEntry{category: category, label: label, description: description, anchor: label})
	}
	group := func(category settingsCategory, anchor, label, description string) {
		add(settingsEntry{category: category, label: label, description: description, anchor: anchor})
	}
	row(settingsGeneral, m.SettingsLanguage, "")
	row(settingsGeneral, m.SettingsAppearance, "")

	if state.options.StartAtLogin != nil {
		row(settingsGeneral, tools.GeneralLogin, tools.GeneralLoginDescription)
	}
	row(settingsGeneral, tools.GeneralUpdateStartup, tools.GeneralUpdateStartupDescription)
	row(settingsGeneral, tools.GeneralUpdate, tools.GeneralUpdateDescription)
	row(settingsGeneral, tools.DiagnosticsRow, tools.DiagnosticsDescription)
	row(settingsTerminal, m.SettingsTerminalOption, m.SettingsTerminalOptionDescription)
	row(settingsTerminal, m.SettingsTerminalFontSize, m.SettingsTerminalFontSizeDescription)
	row(settingsTerminal, m.SettingsTerminalFontFamily, tools.FontPickerDescription)
	row(settingsTerminal, tools.PaddingRow, "")
	row(settingsTerminal, tools.GutterRow, tools.GutterDescription)
	row(settingsTerminal, tools.CursorRow, tools.CursorDescription)
	row(settingsTerminal, m.SettingsTerminalScrollback, m.SettingsTerminalScrollbackDescription)
	row(settingsTerminal, m.SettingsTerminalClipboard, m.SettingsTerminalClipboardDescription)
	row(settingsTerminal, tools.NotifyRow, tools.NotifyDescription)
	row(settingsTerminal, state.messages.History.SettingsRow, state.messages.History.SettingsDescription)
	row(settingsTerminal, state.messages.History.SuggestRow, state.messages.History.SuggestDescription)
	row(settingsTerminal, state.messages.History.ClearRow, state.messages.History.ClearDescription)
	group(settingsTerminal, tools.SchemeGroup, tools.SchemeGroup, tools.SchemeDescription)
	row(settingsTerminal, tools.HighlightBuiltin, tools.HighlightBuiltinDescription)
	row(settingsTerminal, tools.HighlightCustom, tools.HighlightCustomDescription)
	row(settingsTerminal, m2.Row, m2.RowDescription)
	row(settingsTerminal, tools.SnippetsRow, tools.SnippetsDescription)
	row(settingsTerminal, tools.BgRow, tools.BgRowNone)
	row(settingsTerminal, tools.BgVisibility, "")

	row(settingsConnection, m.SettingsConnectionTimeout, m.SettingsConnectionTimeoutDescription)
	row(settingsConnection, m.SettingsConnectionKeepAlive, m.SettingsConnectionKeepAliveDescription)
	row(settingsConnection, m.SettingsConnectionReconnect, m.SettingsConnectionReconnectDescription)
	row(settingsConnection, m.SettingsShare, m.SettingsShareDescription)
	row(settingsConnection, m.SettingsRestore, m.SettingsRestoreDescription)
	row(settingsConnection, tools.ProxyRow, tools.ProxyDescriptionNone)

	row(settingsFiles, tools.FilesParallel, "")
	row(settingsFiles, tools.FilesKeepTimes, tools.FilesKeepTimesDescription)
	row(settingsFiles, tools.FilesFollow, tools.FilesFollowDescription)
	row(settingsFiles, tools.FilesFollowSetup, tools.FilesFollowSetupDescription)

	row(settingsSecurity, m.SettingsVault, m.SettingsVaultEmpty)
	row(settingsSecurity, m.SettingsMasterPassword, m.SettingsMasterPasswordDescription)

	row(settingsBackup, tools.BackupExportRow, tools.BackupExportDescription)
	row(settingsBackup, tools.SyncRow, tools.SyncRowNone)
	row(settingsBackup, tools.SyncAuto, tools.SyncAutoDescription)
	row(settingsBackup, tools.SyncRunRow, tools.SyncRunNever)

	row(settingsAssistant, ai.ProvidersTitle, ai.Description)
	row(settingsAssistant, ai.ModelsTitle, "")
	group(settingsAssistant, ai.BehaviourTitle, ai.AutoRun, ai.AutoRunDesc)
	group(settingsAssistant, ai.BehaviourTitle, ai.MaxSteps, ai.MaxStepsDesc)
	group(settingsAssistant, ai.BehaviourTitle, ai.ContextLines, ai.ContextLinesDesc)
	group(settingsAssistant, ai.BehaviourTitle, ai.Redact, ai.RedactDesc)
	group(settingsAssistant, ai.BehaviourTitle, ai.History, ai.HistoryDesc)
	group(settingsAssistant, ai.BehaviourTitle, ai.ClearHistory, ai.ClearHistoryDesc)

	row(settingsMCP, ai.MCPEnable, ai.MCPEnableDesc)
	row(settingsMCP, ai.MCPMode, ai.MCPModeDesc)
	row(settingsMCP, ai.MCPEndpoint, "")
	row(settingsMCP, ai.MCPToken, ai.MCPTokenDesc)

	for action := shortcutAction(1); action < shortcutActionCount; action++ {
		row(settingsShortcuts, state.shortcutActionLabel(action), state.shortcuts.labels[action])
	}
}

// openSettingsResult leaves the search and opens the category of a result,
// scrolled to it.
func (state *State) openSettingsResult(index int) {
	view := &state.settings
	if index < 0 || index >= view.count {
		return
	}
	entry := view.results[index]
	view.search = text.Editor{}
	view.built, view.count, view.raw = false, 0, ""
	state.selectSettingsCategory(entry.category)
	state.settingsScroll = 0
	view.jump = entry.anchor
}

// clickSettingsCategory handles the presses that belong to the category list and
// the search box. It reports whether the id was one of them.
func (state *State) clickSettingsCategory(id ui.ID) bool {
	switch {
	case id == hitSettingsSearch:
		return true // pressing the box only focuses it
	case id.Scope() == hitSettingsResultList:
		state.openSettingsResult(item(id))
		return true
	case id.Scope() == hitSettingsCategoryList:
		// Choosing a category leaves the search.
		state.settings.search = text.Editor{}
		state.settings.jump = ""
		state.selectSettingsCategory(settingsCategory(uint32(item(id))))
		state.settingsScroll = 0
		return true
	}
	return false
}

// settingsSearchKey handles the keys of the search box: Escape empties it, and Enter
// opens the first result. It reports whether it took the key.
func (state *State) settingsSearchKey(event ui.KeyEvent) (ui.EventResult, bool) {
	if state.modal != modalNone || state.activeSection != sectionSettings || event.Target != hitSettingsSearch || event.Repeat {
		return ui.EventResult{}, false
	}
	handled := ui.EventResult{Handled: true, Repaint: true}
	switch event.Key {
	case ui.KeyEscape:
		if state.settings.search.RuneCount() > 0 {
			state.settings.search = text.Editor{}
			return handled, true
		}
	case ui.KeyEnter:
		if state.settingsSearching() && state.settings.count > 0 {
			state.openSettingsResult(0)
			return handled, true
		}
	}
	return ui.EventResult{}, false
}

// paintSettingsResults lists what the query found, each row with the name of its
// category on a button that opens it.
func (state *State) paintSettingsResults(page *settingsPage) {
	view := &state.settings
	if view.count == 0 {
		page.f.Text(view.empty).FontSize(metrics.FontSizeM).Color(state.pal.muted).LineHeight(22).MarginSides(6, 4, 0, 4)
		return
	}
	page.controlWidth = 150 // room for the explanation; the button only names a category
	page.groups(func() {
		page.group("", func() {
			for index := 0; index < view.count; index++ {
				entry := view.results[index]
				page.actionRow(entry.label, entry.description, false, components.ButtonAction{
					ID: hitSettingsResultList.At(index), Label: state.settingsCategoryName(entry.category), Variant: components.ButtonGhost,
				})
			}
		})
	})
}

// paintSettingsSearchBox paints the search box at the top of the category list.
func (state *State) paintSettingsSearchBox(canvas *ui.Canvas, box ui.Rect) {
	value := state.settings.search.String()
	components.PaintInputGroup(canvas, state.Theme(), box, components.InputGroupSnapshot{
		ID: hitSettingsSearch, Label: state.messages.Tools.SearchPlaceholder, Value: value, Placeholder: state.messages.Tools.SearchPlaceholder,
		NativeEditing: canvas.Focused() == hitSettingsSearch && value != "",
		InlineStart:   components.InputGroupSlot{Kind: components.InputGroupSlotIcon, Icon: lucideIcons[iconSearch]},
	})
}
