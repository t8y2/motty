package workbench

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/i18n"
	"motty/internal/update"
)

// The terminal's font size is any whole number of points from the smallest that
// stays legible to one that still leaves room for a shell.
const (
	terminalFontSizeMin     = float32(9)
	terminalFontSizeDefault = float32(13)
	terminalFontSizeMax     = float32(28)
)

// normalizeTerminalFontSize makes a saved or requested size a usable one: whole
// points, inside the range; anything else (a damaged file) is the default.
func normalizeTerminalFontSize(value float32) float32 {
	if value != value || value < terminalFontSizeMin-0.5 || value > terminalFontSizeMax+0.5 {
		return terminalFontSizeDefault
	}
	return min(max(float32(math.Round(float64(value))), terminalFontSizeMin), terminalFontSizeMax)
}

// zoomTerminal changes the font size by delta points, or back to the default
// when reset is set, and tells the user which size it is now. A keyboard step
// saves at once; a wheel gesture defers its save and calls applyZoom instead.
func (state *State) zoomTerminal(delta float32, reset bool) {
	if state.applyZoom(delta, reset) {
		state.save()
	}
}

// applyZoom moves the terminal font size and writes the status line, reporting
// whether the size actually changed. It saves nothing, so a primary+wheel burst
// can coalesce its many steps into one write once the gesture settles.
func (state *State) applyZoom(delta float32, reset bool) bool {
	size := state.terminalFontSize + delta
	if reset {
		size = terminalFontSizeDefault
	}
	size = min(max(size, terminalFontSizeMin), terminalFontSizeMax)
	changed := state.setTerminalFontSize(size)
	state.status = fmt.Sprintf(state.messages.Tools.StatusFontSizeFmt, int(state.terminalFontSize))
	return changed
}

// terminalZoomSaveDelay is how long the font size holds still after the last
// wheel notch before it is written to disk, so a burst of wheeling saves once.
const terminalZoomSaveDelay = 400 * time.Millisecond

// scheduleTerminalZoomSave writes the wheel's font-size change once the gesture
// settles. Each notch re-arms one timer, so a wheel saves on its last notch rather
// than on every notch. The size is applied in memory at once, so the screen and
// Close()'s own save already carry it if the window quits before this fires.
func (state *State) scheduleTerminalZoomSave() {
	if state.terminalZoomSaveTimer != nil {
		state.terminalZoomSaveTimer.Stop()
	}
	state.terminalZoomSaveTimer = time.AfterFunc(terminalZoomSaveDelay, func() {
		state.aiPost(func(state *State) {
			state.terminalZoomSaveTimer = nil
			if !state.closed {
				state.save()
			}
		})
	})
}

// stopTerminalZoomSave cancels a pending wheel save. The size is already applied
// and Close() saves, so nothing is lost by stopping the timer here.
func (state *State) stopTerminalZoomSave() {
	if state.terminalZoomSaveTimer != nil {
		state.terminalZoomSaveTimer.Stop()
		state.terminalZoomSaveTimer = nil
	}
}

// The preview size is how many megabytes of a file a document window loads at
// once. It is chosen with a stepper and has no upper bound; only a value too
// small to open anything is replaced by the default.
const (
	minFilePreviewMB     = 1
	defaultFilePreviewMB = 20
)

// normalizeFilePreviewMB makes a saved size a usable one: a value below the
// minimum is the default, so a damaged state file cannot refuse every file.
func normalizeFilePreviewMB(value int) int {
	if value < minFilePreviewMB {
		return defaultFilePreviewMB
	}
	return value
}

// filePreviewBytes converts the setting to the byte bound a window enforces. A
// value too large for an int is saturated instead of wrapping negative; the
// stepper cannot produce one, only a damaged state file could.
func filePreviewBytes(mb int) int {
	mb = normalizeFilePreviewMB(mb)
	if mb > int(^uint(0)>>1)>>20 {
		return int(^uint(0) >> 1)
	}
	return mb << 20
}

// previewLimitMessage says a file is larger than the window's bound.
func previewLimitMessage(messages i18n.Messages, mb int) string {
	return fmt.Sprintf(messages.FileWindow.TooLargeFmt, strconv.Itoa(normalizeFilePreviewMB(mb))+" MB")
}

// filePreviewEdit is the stepper's field editor: the number is typed here while
// it has the keyboard, and the stored setting follows when the edit commits.
type filePreviewEdit struct {
	input   text.Editor
	field   ui.Field
	editing bool
	bound   bool
}

// previewDigit accepts the characters the field may hold.
func previewDigit(r rune) bool { return r >= '0' && r <= '9' }

// newFilePreviewInput is the field's editor, holding value with the digits and
// length the field accepts.
func newFilePreviewInput(value string) text.Editor {
	var editor text.Editor
	editor.Restrict(previewDigit, 7)
	editor.Insert(value)
	return editor
}

func normalizeTerminalScrollback(value int) int {
	switch value {
	case 1000, 5000, 10000:
		return value
	default:
		return scrollbackLines
	}
}

func normalizeConnectionTimeoutSeconds(value int) int {
	switch value {
	case 5, 10, 30:
		return value
	default:
		return 10
	}
}

func normalizeConnectionKeepAliveSeconds(value int) int {
	switch value {
	case 0, 30, 60:
		return value
	default:
		return 0
	}
}

// defaultConnectionKeepAlive is how often a connection is probed when the state
// file says nothing. A connection whose peer stopped answering is otherwise
// discovered only by the next keystroke, which can be minutes.
const defaultConnectionKeepAlive = 30 * time.Second

// The interface text size is the body text size in logical pixels: any whole
// size from the smallest that stays legible to the largest the window's fixed
// row heights hold. The descriptions and headings are the framework's own
// proportions of it, so they follow without a size of their own. The terminal
// has a font size apart.
const (
	interfaceTextSizeMin  = 11
	interfaceTextSizeMax  = 18
	interfaceTextSizeStep = 1
)

// interfaceTextSize is the body text size in logical pixels.
type interfaceTextSize int

// interfaceTextSizeDefault is the framework's own body size: the window opens at
// it, and the stepper's middle button returns to it.
var interfaceTextSizeDefault = interfaceTextSize(gfx.DefaultMetrics().FontSizeM)

// factor is the multiplier every interface text size is drawn at: the chosen
// body size over the framework's own.
func (size interfaceTextSize) factor() float32 {
	return float32(size) / gfx.DefaultMetrics().FontSizeM
}

// normalizeInterfaceTextSize makes a saved size a usable one: a whole size
// inside the range; anything else (a damaged file) is the default.
func normalizeInterfaceTextSize(value int) interfaceTextSize {
	if value < interfaceTextSizeMin || value > interfaceTextSizeMax {
		return interfaceTextSizeDefault
	}
	return interfaceTextSize(value)
}

// interfaceTextSizeFromLegacy is a saved size from before the setting kept
// pixels: the two named steps become the body size they drew (0.9 and 1.15 of
// the 14 px body), and an unset one the default. Whether the name was known is
// told apart from a file that simply predates the setting.
func interfaceTextSizeFromLegacy(value string) (interfaceTextSize, bool) {
	switch value {
	case "small":
		return 13, true
	case "large":
		return 16, true
	case "":
		return interfaceTextSizeDefault, true
	}
	return 0, false
}

// setInterfaceTextSize is the user choosing an interface text size: the size
// tokens are rebuilt at once, and everything that paints follows in the next
// frame. A size past either end stops at it.
func (state *State) setInterfaceTextSize(size interfaceTextSize) bool {
	size = interfaceTextSize(min(max(int(size), interfaceTextSizeMin), interfaceTextSizeMax))
	if state.interfaceTextSize == size {
		return false
	}
	state.interfaceTextSize = size
	state.applyInterfaceTextSize()
	return true
}

// applyInterfaceTextSize applies the size the row already wrote: the metrics,
// the theme built from them, and the windows that draw with both. The theme is
// rebuilt too — it copied the metrics by value — so framework components, menus
// and dialogs follow the choice and not just workbench text.
func (state *State) applyInterfaceTextSize() {
	state.applyMetrics()
}

// applyMetrics rebuilds the tokens from the chosen text size and corner style
// and the theme with them, then the windows that draw with both.
func (state *State) applyMetrics() {
	applyMetrics(state.interfaceTextSize.factor(), state.cornerStyle)
	state.theme = state.pal.theme()
	state.invalidateRemoteToolsWindows()
}

// interfaceTextSizeLabel is the body size named beside the stepper's buttons,
// so what is in effect is read without pressing either of them.
func (state *State) interfaceTextSizeLabel() string {
	return fmt.Sprintf("%d px", int(state.interfaceTextSize))
}

func (state *State) setTerminalFontSize(value float32) bool {
	value = normalizeTerminalFontSize(value)
	if state.terminalFontSize == value {
		return false
	}
	state.terminalFontSize = value
	return true
}

func (state *State) setTerminalFontFace(face gfx.MonoFace) bool {
	if face > gfx.MonoClassic && gfx.MonoFamilyName(face) == "" && len(gfx.MonoFallbacks(face)) == 0 && !gfx.MonoIsClassic(face) {
		face = gfx.MonoSystem
	}
	if state.terminalFontFace == face {
		return false
	}
	state.terminalFontFace = face
	return true
}

func terminalFontFaceString(classic bool) string {
	if classic {
		return "classic"
	}
	return "system" // a named family is kept beside it, in its own field
}

func terminalFontFaceFromString(value string) gfx.MonoFace {
	if value == "classic" {
		return gfx.MonoClassic
	}
	return gfx.MonoSystem
}

type terminalOptionMode uint8

const (
	terminalOptionText terminalOptionMode = iota
	terminalOptionMeta
)

func (mode terminalOptionMode) String() string {
	if mode == terminalOptionMeta {
		return "meta"
	}
	return "text"
}

func terminalOptionModeFromString(value string) terminalOptionMode {
	if value == terminalOptionMeta.String() {
		return terminalOptionMeta
	}
	return terminalOptionText
}

// closeAction is what closing the last window does while the application has an
// icon in the status area: keep running there, or quit.
type closeAction uint8

const (
	closeToTray closeAction = iota
	closeQuits
)

func (action closeAction) String() string {
	if action == closeQuits {
		return "quit"
	}
	return "tray"
}

func closeActionFromString(value string) closeAction {
	if value == closeQuits.String() {
		return closeQuits
	}
	return closeToTray
}

// keepWindowsPolicy is what quitting the application does with the windows
// that are open: keep their records for the next launch, discard them, or ask.
type keepWindowsPolicy uint8

const (
	keepWindowsAlways keepWindowsPolicy = iota
	keepWindowsNever
	keepWindowsAsk
)

func (policy keepWindowsPolicy) String() string {
	switch policy {
	case keepWindowsNever:
		return "never"
	case keepWindowsAsk:
		return "ask"
	}
	return "always"
}

func keepWindowsPolicyFromString(value string) keepWindowsPolicy {
	switch value {
	case keepWindowsNever.String():
		return keepWindowsNever
	case keepWindowsAsk.String():
		return keepWindowsAsk
	}
	return keepWindowsAlways
}

type relaunchAction uint8

const (
	relaunchRaise relaunchAction = iota
	relaunchNewWindow
)

func (action relaunchAction) String() string {
	if action == relaunchNewWindow {
		return "new-window"
	}
	return "raise"
}

func relaunchActionFromString(value string) relaunchAction {
	if value == relaunchNewWindow.String() {
		return relaunchNewWindow
	}
	return relaunchRaise
}

// networkProxyMode is which proxy a local terminal's programs are told to use.
// Direct, the default, sets nothing: the terminal behaves as it would if it
// were started from any other terminal, with the environment it inherits.
// Following the system sets the proxy variables while the system names a proxy
// and otherwise leaves the inherited ones alone.
type networkProxyMode uint8

const (
	networkProxyDirect networkProxyMode = iota
	networkProxySystem
)

func (mode networkProxyMode) String() string {
	if mode == networkProxyDirect {
		return "direct"
	}
	return "system"
}

func networkProxyModeFromString(value string) networkProxyMode {
	if value == networkProxySystem.String() {
		return networkProxySystem
	}
	return networkProxyDirect
}

func (state *State) openSettings() {
	if localShellSettingVisible() && state.settingsCategory == settingsTerminal && !state.localShellMenu.loaded {
		state.refreshLocalShells()
	}
	if state.settingsCategory == settingsFiles {
		state.refreshEditors()
	}
	if session, ok := state.sessionByIDPointer(state.activeSessionID); ok {
		state.releaseIdleSFTP(session)
	}
	// Returning to an already-open settings tab preserves where the user left
	// the page (scroll offsets, drawer, and visit state). Only opening settings
	// anew starts back at the top.
	reopen := !state.settingsTab
	state.settingsTab = true
	state.setActiveSection(sectionSettings)
	state.settings.pageMotion, state.settings.navMotion = gfx.Motion{}, gfx.Motion{}
	if reopen {
		state.settingsScroll = 0
		state.settings.navScroll = 0
		state.settings.aboutCopied = false
		state.schemeBrowserOpen = false
		state.revealSettingsCategory()
	}
	state.aiSettingsOpened()
	state.save()
}

// settingsWheelMotion eases a mouse wheel notch's jump. A notch moves the page
// by a fixed distance in one frame, which reads as a step; a short ease-out
// turns a burst of notches into one glide. A trackpad's precise delta is the
// distance the fingers moved and does not need it.
var settingsWheelMotion = gfx.Motion{Duration: 140 * time.Millisecond, Easing: gfx.EaseOut}

// scrollSettings moves the settings page. As everywhere in Viva a positive
// delta reveals content below. A wheel notch is worth at least a row's height
// of travel; a trackpad moves it by exactly what it reports.
func (state *State) scrollSettings(event gfx.ScrollEvent) gfx.EventResult {
	step := event.StepY(24)
	// The page keeps the target; the declaration eases the value it lays out.
	motion := gfx.Motion{}
	if !event.Precise {
		motion = settingsWheelMotion
	}
	if state.schemeBrowserOpen {
		// The drawer is part of the workbench page: WindowConfig gives a wheel
		// over its body to the page's drawer area, and a wheel anywhere else
		// while the drawer is open scrolls nothing, as a modal panel keeps the
		// rest of the page still.
		return gfx.EventResult{Handled: true}
	}
	view := &state.settings
	nav := view.navRect
	if nav.Width > 0 && event.X >= nav.X && event.X < nav.X+nav.Width && event.Y >= nav.Y && event.Y < nav.Y+nav.Height {
		before := view.navScroll
		view.navScroll = min(max(before+step, 0), max(view.navContent-nav.Height, 0))
		if view.navScroll != before {
			view.navMotion = motion
		}
		return gfx.EventResult{Handled: true, Repaint: view.navScroll != before}
	}
	before := state.settingsScroll
	state.settingsScroll = min(max(before+step, 0), max(state.settingsContent-view.pageView, 0))
	if state.settingsScroll != before {
		view.pageMotion = motion
	}
	return gfx.EventResult{Handled: true, Repaint: state.settingsScroll != before}
}

// settingsKeyStep is how far an arrow moves the settings page: a line of its
// text, as the scroll areas of Viva move by.
const settingsKeyStep = float32(32)

// settingsScrollKey scrolls the settings page from the keyboard, as the monitor
// dashboard and the host form's body do. PageUp and PageDown page it from any of
// its controls, so a long page is read without first finding its viewport; the
// arrows and Home/End move it while the viewport itself has the keyboard, which
// a press on the page's empty room gives it. A key step lands where it lands: only
// a wheel notch glides. A text field keeps the page keys, as it does on the
// dashboard.
func (state *State) settingsScrollKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if event.Release || event.Modifiers != 0 || state.schemeBrowserOpen {
		return gfx.EventResult{}, false
	}
	view := &state.settings
	maxScroll := max(state.settingsContent-view.pageView, 0)
	if maxScroll <= 0 {
		return gfx.EventResult{}, false
	}
	before := state.settingsScroll
	scroll := before
	switch event.Key {
	case gfx.KeyPageDown, gfx.KeyPageUp:
		if state.EditorFor(event.Target) != nil {
			return gfx.EventResult{}, false
		}
		if event.Key == gfx.KeyPageDown {
			scroll += max(view.pageView*0.9, settingsKeyStep)
		} else {
			scroll -= max(view.pageView*0.9, settingsKeyStep)
		}
	case gfx.KeyDown, gfx.KeyUp, gfx.KeyHome, gfx.KeyEnd:
		// The arrows and the ends belong to the control that has the keyboard,
		// as a text field's do: they reach the page once it has the keyboard.
		if event.Target != hitSettingsViewport {
			return gfx.EventResult{}, false
		}
		switch event.Key {
		case gfx.KeyDown:
			scroll += settingsKeyStep
		case gfx.KeyUp:
			scroll -= settingsKeyStep
		case gfx.KeyHome:
			scroll = 0
		case gfx.KeyEnd:
			scroll = maxScroll
		}
	default:
		return gfx.EventResult{}, false
	}
	state.settingsScroll = min(max(scroll, 0), maxScroll)
	if state.settingsScroll == before {
		return gfx.EventResult{Handled: true}, true
	}
	view.pageMotion = gfx.Motion{}
	return gfx.EventResult{Handled: true, Repaint: true}, true
}

// chooseLanguage is the user picking a language: it sticks, so the system's
// setting no longer decides. Choosing the language already showing still counts
// (and is saved), which is how someone keeps English on a Chinese system.
func (state *State) chooseLanguage(language i18n.Language) bool {
	wasChosen := state.languageChosen
	state.languageChosen = true
	return state.setLanguage(language) || !wasChosen
}

// systemLanguage is the catalog for the first of the system's preferred languages
// that has one, and English when none does.
func (state *State) systemLanguage() i18n.Language {
	for _, tag := range state.options.PreferredLanguages() {
		primary, _, _ := strings.Cut(strings.ToLower(tag), "-")
		switch primary {
		case "zh":
			return i18n.ChineseVariant(tag)
		case "en":
			return i18n.English
		}
	}
	return i18n.English
}

func (state *State) setLanguage(language i18n.Language) bool {
	if state.language == language {
		return false
	}
	state.language = language
	state.applyLanguage()
	return true
}

// refreshEditMenu takes the words of the text fields' edit menu from the catalog.
func (state *State) refreshEditMenu() {
	m := &state.messages
	state.editMenu = ui.EditMenuLabels{Cut: m.MenuCut, Copy: m.MenuCopy, Paste: m.MenuPaste, SelectAll: m.MenuSelectAll}
}

// applyLanguage applies the language the page's choice already wrote: the
// catalog and everything built from it.
func (state *State) applyLanguage() {
	state.messages = i18n.For(state.language)
	state.refreshEditMenu()
	state.invalidateRemoteToolsWindows()
	state.rebuildTransferCenter()
	state.status = state.messages.StatusReadyIdle
	state.refreshHostSelection()
	// The order is read in the new language, and an unchanged host set and
	// query keep syncVisible from resorting it.
	state.sortVisibleHosts()
	state.refreshHostGroups()
	state.refreshIdentityCount()
	state.refreshIdentityCells()
	state.refreshForwards()
	state.refreshProxies()
	state.refreshSnippets()
	for index := range state.sessions {
		state.sessions[index].reconnectText = ""
		state.describe(&state.sessions[index])
		if view := state.sessions[index].files; view != nil {
			state.refreshLocalCoverage(&view.lfolder)
		}
	}
	for _, window := range state.sftpWindows {
		state.refreshLocalCoverage(&window.view.lfolder)
		window.view.invalidateDetached()
	}
	state.describeTerminalFinds()
	if state.modal == modalCommandPalette {
		state.rebuildCommandPalette()
	}
}

func (state *State) clickSettings(id gfx.ID) bool {
	if state.modal != modalNone || state.activeSection != sectionSettings {
		return false
	}
	if state.clickLocalShell(id) || state.clickStartupHosts(id) {
		return true
	}
	for group, hit := range hitToolbarSettingsOpen {
		if id == hit {
			state.openToolbarSettings(toolbarGroup(group))
			return true
		}
	}
	changed := false
	switch id {
	case hitSidebarSettingsOpen:
		state.openSidebarSettings()
		return true
	case hitSettingsTabNumbers:
		changed = state.setTabNumbers(!state.tabNumbers)
	case hitSettingsOutputAlerts:
		state.backgroundOutputAlerts = !state.backgroundOutputAlerts
		state.clearOutputAlerts()
		changed = true
	case hitSettingsVerticalTabsAutoHide:
		changed = state.setVerticalTabsAutoHide(!state.verticalTabs.autoHide)
	case hitSettingsVerticalTabsGrouping:
		changed = state.setVerticalTabsGrouping(!state.verticalTabs.grouping)
	case hitSettingsTextSizeDown:
		changed = state.setInterfaceTextSize(state.interfaceTextSize - interfaceTextSizeStep)
	case hitSettingsTextSizeUp:
		changed = state.setInterfaceTextSize(state.interfaceTextSize + interfaceTextSizeStep)
	case hitSettingsTextSizeReset:
		changed = state.setInterfaceTextSize(interfaceTextSizeDefault)
	case hitSettingsFontSizeDown:
		changed = state.setTerminalFontSize(state.terminalFontSize - 1)
	case hitSettingsFontSizeUp:
		changed = state.setTerminalFontSize(state.terminalFontSize + 1)
	case hitSettingsFontSizeReset:
		changed = state.setTerminalFontSize(terminalFontSizeDefault)
	case hitSettingsShareToggle:
		if state.shareConnections == shareConnectionsOff {
			changed = state.setShareConnections(shareConnectionsOn)
		} else {
			changed = state.setShareConnections(shareConnectionsOff)
		}
	case hitSettingsStartupHosts:
		state.clickStartupHosts(id)
	case hitSettingsSSHSyncToggle, hitSettingsSSHSyncFile, hitSettingsSSHSyncReset, hitSettingsSSHSyncNow:
		state.clickSSHSync(id)
	case hitSessionLogsSettingsToggle:
		changed = state.toggleSessionLogs()
	case hitSessionLogsSettingsClear:
		state.askClearSessionLogs(false)
	case hitSessionLogsSettingsClearAll:
		state.askClearSessionLogs(true)
	default:
		if state.clickAboutSettings(id) {
			return true
		}
		return state.clickVaultSettings(id)
	}
	if changed {
		state.save()
	}
	return true
}

func (state *State) setTerminalScrollback(value int) bool {
	value = normalizeTerminalScrollback(value)
	if state.terminalScrollback == value {
		return false
	}
	state.terminalScrollback = value
	state.applyTerminalScrollback()
	return true
}

// applyTerminalScrollback applies the limit the page's choice already wrote to
// the terminals that are open.
func (state *State) applyTerminalScrollback() {
	for index := range state.sessions {
		if terminal := state.sessions[index].term; terminal != nil {
			terminal.screen.SetScrollbackLimit(state.terminalScrollback)
			terminal.selection.trim(terminal.screen)
			terminal.find.refreshIfChanged(terminal.screen, state.messages, 0)
		}
	}
}

// The choices behind the General page's choice rows: a page Choice binds the
// value itself, so these are the values it offers.
var (
	settingsLanguageChoices        = [...]i18n.Language{i18n.English, i18n.Chinese, i18n.TraditionalChinese}
	settingsAppearanceChoices      = [...]appearanceMode{appearanceFollowSystem, appearancePinnedDark, appearancePinnedLight}
	settingsCornerChoices          = [...]cornerStyle{cornersRounded, cornersSquare}
	settingsToolbarPositionChoices = [...]terminalToolbarPosition{terminalToolbarLeft, terminalToolbarRight}
	settingsTabOrientationChoices  = [...]tabOrientation{tabsHorizontal, tabsVertical}
	settingsTabDoubleClickChoices  = [...]tabDoubleClickAction{tabDoubleClickRename, tabDoubleClickDuplicate, tabDoubleClickClose, tabDoubleClickNone}
	settingsCloseChoices           = [...]closeAction{closeToTray, closeQuits}
	settingsKeepWindowsChoices     = [...]keepWindowsPolicy{keepWindowsAlways, keepWindowsNever, keepWindowsAsk}
	settingsRelaunchChoices        = [...]relaunchAction{relaunchRaise, relaunchNewWindow}
	settingsUpdateSourceChoices    = [...]update.Source{update.SourceOfficial, update.SourceCNB}
)

// settingsUpdateSourceLabel names an update source for its choice row.
func (state *State) settingsUpdateSourceLabel(source update.Source) string {
	if source == update.SourceCNB {
		return state.messages.Tools.GeneralUpdateSourceCNB
	}
	return state.messages.Tools.GeneralUpdateSourceOfficial
}

func (state *State) settingsLanguageLabel(language i18n.Language) string {
	switch language {
	case i18n.Chinese:
		return state.messages.SettingsLanguageChinese
	case i18n.TraditionalChinese:
		return state.messages.SettingsLanguageTraditionalChinese
	}
	return state.messages.SettingsLanguageEnglish
}

func (state *State) settingsAppearanceLabel(mode appearanceMode) string {
	switch mode {
	case appearancePinnedDark:
		return state.messages.SettingsAppearanceDark
	case appearancePinnedLight:
		return state.messages.SettingsAppearanceLight
	}
	return state.messages.SettingsAppearanceSystem
}

func (state *State) settingsCornerLabel(style cornerStyle) string {
	if style == cornersSquare {
		return state.messages.SettingsCornersSquare
	}
	return state.messages.SettingsCornersRounded
}

func (state *State) settingsToolbarPositionLabel(position terminalToolbarPosition) string {
	if position == terminalToolbarLeft {
		return state.messages.SettingsToolbarPositionLeft
	}
	return state.messages.SettingsToolbarPositionRight
}

func (state *State) settingsTabOrientationLabel(orientation tabOrientation) string {
	if orientation == tabsVertical {
		return state.messages.SettingsTabsVertical
	}
	return state.messages.SettingsTabsHorizontal
}

func (state *State) settingsTabDoubleClickLabel(action tabDoubleClickAction) string {
	switch action {
	case tabDoubleClickRename:
		return state.messages.MenuRenameTab
	case tabDoubleClickClose:
		return state.messages.LabelCloseTab
	case tabDoubleClickNone:
		return state.messages.SettingsTabDoubleClickNone
	}
	return state.messages.MenuDuplicateTab
}

func (state *State) settingsCloseLabel(action closeAction) string {
	if action == closeQuits {
		return state.messages.Tools.GeneralCloseQuit
	}
	return state.messages.Tools.GeneralCloseTray
}

func (state *State) settingsKeepWindowsLabel(policy keepWindowsPolicy) string {
	messages := state.messages.Tools
	switch policy {
	case keepWindowsNever:
		return messages.GeneralKeepWindowsDiscard
	case keepWindowsAsk:
		return messages.GeneralKeepWindowsAsk
	}
	return messages.GeneralKeepWindowsKeep
}

func (state *State) settingsRelaunchLabel(action relaunchAction) string {
	if action == relaunchNewWindow {
		return state.messages.Tools.GeneralRelaunchNewWindow
	}
	return state.messages.Tools.GeneralRelaunchRaise
}

// The choices behind the Terminal and Connection pages, which a page Choice
// binds.
var (
	settingsOptionChoices          = [...]terminalOptionMode{terminalOptionText, terminalOptionMeta}
	settingsLineHeightChoices      = [...]lineHeight{lineHeightCompact, lineHeightNormal, lineHeightRoomy}
	settingsPaddingChoices         = [...]terminalPad{padCompact, padNormal, padRoomy}
	settingsGutterChoices          = [...]terminalGutter{gutterOff, gutterNumbers, gutterTime, gutterBoth}
	settingsIntenseChoices         = [...]terminalIntense{intenseBoth, intenseBright, intenseBold, intensePlain}
	settingsCursorThicknessChoices = [...]cursorThickness{cursorThin, cursorNormal, cursorThick}
	settingsScrollbackChoices      = [...]int{1000, 5000, 10000}
	settingsClipboardChoices       = [...]terminalClipboardPolicy{terminalClipboardBlock, terminalClipboardAsk, terminalClipboardAllow}
	settingsCursorStyleChoices     = [...]terminalCursorStyle{cursorBlock, cursorBar, cursorUnderline}
	settingsTabTitleChoices        = [...]tabTitleMode{tabTitleConnection, tabTitleTerminal, tabTitleDirectory}
	settingsTimeoutChoices         = [...]time.Duration{5 * time.Second, 10 * time.Second, 30 * time.Second}
	settingsKeepAliveChoices       = [...]time.Duration{0, 30 * time.Second, 60 * time.Second}
	settingsReconnectChoices       = [...]reconnectPolicy{reconnectOff, reconnectThree, reconnectForever}
	settingsRestoreChoices         = [...]restoreConnectPolicy{restoreConnectOff, restoreConnectActive, restoreConnectAll}
	settingsNetworkProxyChoices    = [...]networkProxyMode{networkProxyDirect, networkProxySystem}
)

func (state *State) settingsOptionLabel(mode terminalOptionMode) string {
	if mode == terminalOptionMeta {
		return state.messages.SettingsTerminalOptionMeta
	}
	return state.messages.SettingsTerminalOptionText
}

func (state *State) settingsLineHeightLabel(mode lineHeight) string {
	m := state.messages.Tools
	switch mode {
	case lineHeightCompact:
		return m.LineHeightCompact
	case lineHeightRoomy:
		return m.LineHeightRoomy
	}
	return m.LineHeightNormal
}

func (state *State) settingsPaddingLabel(pad terminalPad) string {
	m := state.messages.Tools
	switch pad {
	case padCompact:
		return m.PaddingCompact
	case padRoomy:
		return m.PaddingRoomy
	}
	return m.PaddingNormal
}

func (state *State) settingsGutterLabel(gutter terminalGutter) string {
	m := state.messages.Tools
	switch gutter {
	case gutterNumbers:
		return m.GutterNumbers
	case gutterTime:
		return m.GutterTime
	case gutterBoth:
		return m.GutterBoth
	}
	return m.GutterOff
}

func (state *State) settingsIntenseLabel(style terminalIntense) string {
	m := state.messages.Tools
	switch style {
	case intenseBright:
		return m.IntenseBright
	case intenseBold:
		return m.IntenseBold
	case intensePlain:
		return m.IntensePlain
	}
	return m.IntenseBoth
}

func (state *State) settingsCursorThicknessLabel(mode cursorThickness) string {
	m := state.messages.Tools
	switch mode {
	case cursorThin:
		return m.CursorThin
	case cursorThick:
		return m.CursorThick
	}
	return m.CursorNormal
}

func (state *State) settingsScrollbackLabel(lines int) string {
	switch lines {
	case 1000:
		return "1,000"
	case 10000:
		return "10,000"
	}
	return "5,000"
}

func (state *State) settingsClipboardLabel(policy terminalClipboardPolicy) string {
	switch policy {
	case terminalClipboardBlock:
		return state.messages.SettingsTerminalClipboardBlock
	case terminalClipboardAsk:
		return state.messages.SettingsTerminalClipboardAsk
	}
	return state.messages.SettingsTerminalClipboardAllow
}

func (state *State) settingsCursorStyleLabel(style terminalCursorStyle) string {
	m := state.messages.Tools
	switch style {
	case cursorBar:
		return m.CursorShapeBar
	case cursorUnderline:
		return m.CursorShapeUnderline
	}
	return m.CursorShapeBlock
}

func (state *State) settingsTabTitleLabel(mode tabTitleMode) string {
	m := state.messages.Tools
	switch mode {
	case tabTitleTerminal:
		return m.TabTitleTerminal
	case tabTitleDirectory:
		return m.TabTitleDirectory
	}
	return m.TabTitleConnection
}

func (state *State) settingsTimeoutLabel(value time.Duration) string {
	return fmt.Sprintf("%d s", int(value/time.Second))
}

func (state *State) settingsKeepAliveLabel(value time.Duration) string {
	if value == 0 {
		return state.messages.SettingsConnectionKeepAliveOff
	}
	return state.settingsTimeoutLabel(value)
}

func (state *State) settingsReconnectLabel(policy reconnectPolicy) string {
	switch policy {
	case reconnectThree:
		return state.messages.SettingsConnectionReconnectThree
	case reconnectForever:
		return state.messages.SettingsConnectionReconnectForever
	}
	return state.messages.SettingsConnectionReconnectOff
}

func (state *State) settingsRestoreLabel(policy restoreConnectPolicy) string {
	switch policy {
	case restoreConnectActive:
		return state.messages.SettingsRestoreActive
	case restoreConnectAll:
		return state.messages.SettingsRestoreAll
	}
	return state.messages.SettingsRestoreOff
}

func (state *State) settingsNetworkProxyLabel(mode networkProxyMode) string {
	if mode == networkProxySystem {
		return state.messages.SettingsNetworkProxySystem
	}
	return state.messages.SettingsNetworkProxyDirect
}

func (state *State) settingsKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if state.modal != modalNone || state.activeSection != sectionSettings {
		return gfx.EventResult{}, false
	}
	// The preview stepper's field commits on Enter and on leaving the field;
	// Escape puts the stored number back without changing it.
	if state.settingsCategory == settingsFiles && state.filePreviewEdit.editing {
		if event.Target != hitSettingsPreviewValue {
			state.commitFilePreviewEdit()
		} else {
			switch event.Key {
			case gfx.KeyEnter:
				state.commitFilePreviewEdit()
				return gfx.EventResult{Handled: true, Repaint: true}, true
			case gfx.KeyEscape:
				state.filePreviewEdit.editing = false
				return gfx.EventResult{Handled: true, Repaint: true}, true
			case gfx.KeyTab:
				state.commitFilePreviewEdit()
			}
		}
	}
	if event.Target == hitSettingsLocalShell && localShellSettingVisible() && !state.localShellMenu.open && (event.Key == gfx.KeyDown || event.Key == gfx.KeyUp) {
		state.refreshLocalShells()
		if len(state.localShellMenu.choices) == 0 {
			return gfx.EventResult{}, false
		}
		state.localShellMenu.open = true
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	return state.settingsScrollKey(event)
}
