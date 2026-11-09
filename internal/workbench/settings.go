package workbench

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"motty/internal/i18n"
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
// when reset is set, and tells the user which size it is now.
func (state *State) zoomTerminal(delta float32, reset bool) {
	size := state.terminalFontSize + delta
	if reset {
		size = terminalFontSizeDefault
	}
	size = min(max(size, terminalFontSizeMin), terminalFontSizeMax)
	if state.setTerminalFontSize(size) {
		state.save()
	}
	state.status = fmt.Sprintf(state.messages.Tools.StatusFontSizeFmt, int(state.terminalFontSize))
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

// textScale is the interface text size: the default, or a step each way. It is
// saved by name, as the appearance mode is.
type textScale uint8

const (
	textScaleRegular textScale = iota
	textScaleSmall
	textScaleLarge
)

// factor is the multiplier every interface text size is drawn at.
func (scale textScale) factor() float32 {
	switch scale {
	case textScaleSmall:
		return textScaleSmallFactor
	case textScaleLarge:
		return textScaleLargeFactor
	}
	return textScaleRegularFactor
}

func (scale textScale) String() string {
	switch scale {
	case textScaleSmall:
		return "small"
	case textScaleLarge:
		return "large"
	}
	return ""
}

func textScaleFromString(value string) textScale {
	switch value {
	case "small":
		return textScaleSmall
	case "large":
		return textScaleLarge
	}
	return textScaleRegular
}

// setTextScale is the user choosing an interface text size: the size tokens are
// rebuilt at once, and everything that paints follows in the next frame. The
// theme is rebuilt too — it copied the metrics by value — so framework
// components, menus and dialogs follow the choice and not just workbench text.
func (state *State) setTextScale(scale textScale) bool {
	if state.textScale == scale {
		return false
	}
	state.textScale = scale
	applyTextMetrics(scale.factor())
	state.theme = state.pal.theme()
	state.invalidateRemoteToolsWindows()
	return true
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
	state.activeSection = sectionSettings
	state.settingsScroll = 0
	state.settings.navScroll = 0
	state.settings.aboutCopied = false
	state.schemeBrowserOpen = false
	state.revealSettingsCategory()
	state.aiSettingsOpened()
	state.save()
}

// scrollSettings moves the settings page. As everywhere in Viva a positive
// delta reveals content below. A wheel notch is worth at least a row's height
// of travel; a trackpad moves it by exactly what it reports.
func (state *State) scrollSettings(event gfx.ScrollEvent) gfx.EventResult {
	step := event.StepY(24)
	if state.schemeBrowserOpen {
		if pointInRect(event.X, event.Y, state.schemeDrawerBody) {
			result := state.schemeDrawerView.Wheel(event)
			result.Handled = true
			return result
		}
		return gfx.EventResult{Handled: true}
	}
	view := &state.settings
	nav := view.navRect
	if nav.Width > 0 && event.X >= nav.X && event.X < nav.X+nav.Width && event.Y >= nav.Y && event.Y < nav.Y+nav.Height {
		before := view.navScroll
		view.navScroll = min(max(before+step, 0), max(view.navContent-nav.Height, 0))
		return gfx.EventResult{Handled: true, Repaint: view.navScroll != before}
	}
	before := state.settingsScroll
	state.settingsScroll = min(max(before+step, 0), max(state.settingsContent-view.pageView, 0))
	return gfx.EventResult{Handled: true, Repaint: state.settingsScroll != before}
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
	state.messages = i18n.For(language)
	state.invalidateRemoteToolsWindows()
	state.rebuildTransferCenter()
	state.status = state.messages.StatusReadyIdle
	state.refreshHostSelection()
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
	return true
}

func (state *State) clickSettings(id gfx.ID) bool {
	if state.modal != modalNone || state.activeSection != sectionSettings {
		return false
	}
	if state.clickLocalShell(id) || state.clickEditorSelect(id) || state.clickStartupHosts(id) {
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
	case hitSettingsLanguageEnglish:
		changed = state.chooseLanguage(i18n.English)
	case hitSettingsLanguageChinese:
		changed = state.chooseLanguage(i18n.Chinese)
	case hitSettingsLanguageTraditional:
		changed = state.chooseLanguage(i18n.TraditionalChinese)
	case hitSettingsAppearanceSystem:
		changed = state.chooseAppearanceMode(appearanceFollowSystem)
	case hitSettingsAppearanceDark:
		changed = state.chooseAppearanceMode(appearancePinnedDark)
	case hitSettingsAppearanceLight:
		changed = state.chooseAppearanceMode(appearancePinnedLight)
	case hitSettingsTextSmall:
		changed = state.setTextScale(textScaleSmall)
	case hitSettingsTextRegular:
		changed = state.setTextScale(textScaleRegular)
	case hitSettingsTextLarge:
		changed = state.setTextScale(textScaleLarge)
	case hitSettingsTabsHorizontal:
		changed = state.setTabOrientation(tabsHorizontal)
	case hitSettingsTabsVertical:
		changed = state.setTabOrientation(tabsVertical)
	case hitSettingsTabNumbers:
		changed = state.setTabNumbers(!state.tabNumbers)
	case hitSettingsTabNumbersPrimary, hitSettingsTabNumbersAlt, hitSettingsTabNumbersShift:
		changed = state.setTabNumberModifier(id)
	case hitSettingsOutputAlerts:
		state.backgroundOutputAlerts = !state.backgroundOutputAlerts
		state.clearOutputAlerts()
		changed = true
	case hitSettingsVerticalTabsAutoHide:
		changed = state.setVerticalTabsAutoHide(!state.verticalTabs.autoHide)
	case hitSettingsVerticalTabsGrouping:
		changed = state.setVerticalTabsGrouping(!state.verticalTabs.grouping)
	case hitSettingsNetworkSystem:
		changed = state.networkProxy != networkProxySystem
		state.networkProxy = networkProxySystem
	case hitSettingsNetworkDirect:
		changed = state.networkProxy != networkProxyDirect
		state.networkProxy = networkProxyDirect
	case hitSettingsToolbarLeft:
		changed = state.setTerminalToolbarPosition(terminalToolbarLeft)
	case hitSettingsToolbarRight:
		changed = state.setTerminalToolbarPosition(terminalToolbarRight)
	case hitSettingsCloseTray:
		changed = state.closeAction != closeToTray
		state.closeAction = closeToTray
	case hitSettingsCloseQuit:
		changed = state.closeAction != closeQuits
		state.closeAction = closeQuits
	case hitSettingsRelaunchRaise:
		changed = state.relaunchAction != relaunchRaise
		state.relaunchAction = relaunchRaise
	case hitSettingsRelaunchNewWindow:
		changed = state.relaunchAction != relaunchNewWindow
		state.relaunchAction = relaunchNewWindow
	case hitSettingsOptionText:
		changed = state.terminalOption != terminalOptionText
		state.terminalOption = terminalOptionText
	case hitSettingsOptionMeta:
		changed = state.terminalOption != terminalOptionMeta
		state.terminalOption = terminalOptionMeta
	case hitSettingsFontSizeDown:
		changed = state.setTerminalFontSize(state.terminalFontSize - 1)
	case hitSettingsFontSizeUp:
		changed = state.setTerminalFontSize(state.terminalFontSize + 1)
	case hitSettingsFontSizeReset:
		changed = state.setTerminalFontSize(terminalFontSizeDefault)
	case hitSettingsPadCompact:
		changed = state.setTerminalPadding(padCompact)
	case hitSettingsPadNormal:
		changed = state.setTerminalPadding(padNormal)
	case hitSettingsPadRoomy:
		changed = state.setTerminalPadding(padRoomy)
	case hitSettingsLineHeightCompact:
		changed = state.setTerminalLineHeight(lineHeightCompact)
	case hitSettingsLineHeightNormal:
		changed = state.setTerminalLineHeight(lineHeightNormal)
	case hitSettingsLineHeightRoomy:
		changed = state.setTerminalLineHeight(lineHeightRoomy)
	case hitSettingsCursorThin:
		changed = state.setTerminalCursorThickness(cursorThin)
	case hitSettingsCursorNormal:
		changed = state.setTerminalCursorThickness(cursorNormal)
	case hitSettingsCursorThick:
		changed = state.setTerminalCursorThickness(cursorThick)
	case hitSettingsGutterOff:
		changed = state.setTerminalGutter(gutterOff)
	case hitSettingsGutterNumbers:
		changed = state.setTerminalGutter(gutterNumbers)
	case hitSettingsGutterTime:
		changed = state.setTerminalGutter(gutterTime)
	case hitSettingsGutterBoth:
		changed = state.setTerminalGutter(gutterBoth)
	case hitSettingsIntenseBoth:
		changed = state.setTerminalIntense(intenseBoth)
	case hitSettingsIntenseBright:
		changed = state.setTerminalIntense(intenseBright)
	case hitSettingsIntenseBold:
		changed = state.setTerminalIntense(intenseBold)
	case hitSettingsIntensePlain:
		changed = state.setTerminalIntense(intensePlain)
	case hitSettingsScrollback1000:
		changed = state.setTerminalScrollback(1000)
	case hitSettingsScrollback5000:
		changed = state.setTerminalScrollback(5000)
	case hitSettingsScrollback10000:
		changed = state.setTerminalScrollback(10000)
	case hitSettingsTimeout5:
		changed = state.connectionTimeout != 5*time.Second
		state.connectionTimeout = 5 * time.Second
	case hitSettingsTimeout10:
		changed = state.connectionTimeout != 10*time.Second
		state.connectionTimeout = 10 * time.Second
	case hitSettingsTimeout30:
		changed = state.connectionTimeout != 30*time.Second
		state.connectionTimeout = 30 * time.Second
	case hitSettingsKeepAliveOff:
		changed = state.connectionKeepAlive != 0
		state.connectionKeepAlive = 0
	case hitSettingsKeepAlive30:
		changed = state.connectionKeepAlive != 30*time.Second
		state.connectionKeepAlive = 30 * time.Second
	case hitSettingsKeepAlive60:
		changed = state.connectionKeepAlive != 60*time.Second
		state.connectionKeepAlive = 60 * time.Second
	case hitSettingsReconnectOff:
		changed = state.setReconnectPolicy(reconnectOff)
	case hitSettingsReconnectThree:
		changed = state.setReconnectPolicy(reconnectThree)
	case hitSettingsReconnectForever:
		changed = state.setReconnectPolicy(reconnectForever)
	case hitSettingsCompletionOff:
		changed = state.setCompletionMode(completionOff)
	case hitSettingsCompletionList:
		changed = state.setCompletionMode(completionList)
	case hitSettingsCompletionBoth:
		changed = state.setCompletionMode(completionBoth)
	case hitSettingsClipboardBlock:
		changed = state.terminalClipboard != terminalClipboardBlock
		state.terminalClipboard = terminalClipboardBlock
	case hitSettingsClipboardAsk:
		changed = state.terminalClipboard != terminalClipboardAsk
		state.terminalClipboard = terminalClipboardAsk
	case hitSettingsClipboardAllow:
		changed = state.terminalClipboard != terminalClipboardAllow
		state.terminalClipboard = terminalClipboardAllow
	case hitSettingsRestoreActive:
		changed = state.setRestoreConnectPolicy(restoreConnectActive)
	case hitSettingsRestoreOff:
		changed = state.setRestoreConnectPolicy(restoreConnectOff)
	case hitSettingsRestoreAll:
		changed = state.setRestoreConnectPolicy(restoreConnectAll)
	case hitSettingsLocalStartRemember:
		changed = state.setLocalStartPolicy(localStartRemember)
	case hitSettingsLocalStartHome:
		changed = state.setLocalStartPolicy(localStartHome)
	case hitSettingsAgentSystem:
		changed = state.setSSHAgent(agentChoice{})
	case hitSettingsAgentOnePassword:
		changed = state.setSSHAgent(agentChoice{kind: agentKindOnePassword})
	case hitSettingsAgentBitwarden:
		changed = state.setSSHAgent(agentChoice{kind: agentKindBitwarden})
	case hitSettingsAgentCustom:
		state.openSSHAgentPathForm()
	case hitSettingsShareToggle:
		if state.shareConnections == shareConnectionsOff {
			changed = state.setShareConnections(shareConnectionsOn)
		} else {
			changed = state.setShareConnections(shareConnectionsOff)
		}
	case hitSettingsStartupHosts, hitSettingsStartupLocal, hitSettingsStartupDismiss:
		state.clickStartupHosts(id)
	case hitSettingsCursorBlock:
		changed = state.setTerminalCursorStyle(cursorBlock)
	case hitSettingsCursorBar:
		changed = state.setTerminalCursorStyle(cursorBar)
	case hitSettingsCursorUnderline:
		changed = state.setTerminalCursorStyle(cursorUnderline)
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
	for index := range state.sessions {
		if terminal := state.sessions[index].term; terminal != nil {
			terminal.screen.SetScrollbackLimit(value)
			terminal.find.refreshIfChanged(terminal.screen, state.messages, 0)
		}
	}
	return true
}

func (state *State) settingsLanguageOptions() [3]components.SegmentOption {
	return [...]components.SegmentOption{
		{ID: hitSettingsLanguageEnglish, Label: state.messages.SettingsLanguageEnglish},
		{ID: hitSettingsLanguageChinese, Label: state.messages.SettingsLanguageChinese},
		{ID: hitSettingsLanguageTraditional, Label: state.messages.SettingsLanguageTraditionalChinese},
	}
}

func (state *State) settingsTextSizeOptions() [3]components.SegmentOption {
	return [...]components.SegmentOption{
		{ID: hitSettingsTextSmall, Label: state.messages.SettingsTextSizeSmall},
		{ID: hitSettingsTextRegular, Label: state.messages.SettingsTextSizeRegular},
		{ID: hitSettingsTextLarge, Label: state.messages.SettingsTextSizeLarge},
	}
}

func (state *State) settingsAppearanceOptions() [3]components.SegmentOption {
	return [...]components.SegmentOption{
		{ID: hitSettingsAppearanceSystem, Label: state.messages.SettingsAppearanceSystem},
		{ID: hitSettingsAppearanceDark, Label: state.messages.SettingsAppearanceDark},
		{ID: hitSettingsAppearanceLight, Label: state.messages.SettingsAppearanceLight},
	}
}

func (state *State) settingsNetworkProxyOptions() [2]components.SegmentOption {
	return [...]components.SegmentOption{
		{ID: hitSettingsNetworkDirect, Label: state.messages.SettingsNetworkProxyDirect},
		{ID: hitSettingsNetworkSystem, Label: state.messages.SettingsNetworkProxySystem},
	}
}

func (state *State) settingsCloseOptions() [2]components.SegmentOption {
	m := state.messages.Tools
	return [...]components.SegmentOption{
		{ID: hitSettingsCloseTray, Label: m.GeneralCloseTray},
		{ID: hitSettingsCloseQuit, Label: m.GeneralCloseQuit},
	}
}

func (state *State) settingsRelaunchOptions() [2]components.SegmentOption {
	m := state.messages.Tools
	return [...]components.SegmentOption{
		{ID: hitSettingsRelaunchRaise, Label: m.GeneralRelaunchRaise},
		{ID: hitSettingsRelaunchNewWindow, Label: m.GeneralRelaunchNewWindow},
	}
}

func (state *State) settingsOptionOptions() [2]components.SegmentOption {
	return [...]components.SegmentOption{
		{ID: hitSettingsOptionText, Label: state.messages.SettingsTerminalOptionText},
		{ID: hitSettingsOptionMeta, Label: state.messages.SettingsTerminalOptionMeta},
	}
}

func (state *State) settingsCursorThicknessOptions() [3]components.SegmentOption {
	m := state.messages.Tools
	return [...]components.SegmentOption{
		{ID: hitSettingsCursorThin, Label: m.CursorThin},
		{ID: hitSettingsCursorNormal, Label: m.CursorNormal},
		{ID: hitSettingsCursorThick, Label: m.CursorThick},
	}
}

func (state *State) settingsPaddingOptions() [3]components.SegmentOption {
	m := state.messages.Tools
	return [...]components.SegmentOption{
		{ID: hitSettingsPadCompact, Label: m.PaddingCompact},
		{ID: hitSettingsPadNormal, Label: m.PaddingNormal},
		{ID: hitSettingsPadRoomy, Label: m.PaddingRoomy},
	}
}

func (state *State) settingsLineHeightOptions() [3]components.SegmentOption {
	m := state.messages.Tools
	return [...]components.SegmentOption{
		{ID: hitSettingsLineHeightCompact, Label: m.LineHeightCompact},
		{ID: hitSettingsLineHeightNormal, Label: m.LineHeightNormal},
		{ID: hitSettingsLineHeightRoomy, Label: m.LineHeightRoomy},
	}
}

func (state *State) settingsGutterOptions() [4]components.SegmentOption {
	m := state.messages.Tools
	return [...]components.SegmentOption{
		{ID: hitSettingsGutterOff, Label: m.GutterOff},
		{ID: hitSettingsGutterNumbers, Label: m.GutterNumbers},
		{ID: hitSettingsGutterTime, Label: m.GutterTime},
		{ID: hitSettingsGutterBoth, Label: m.GutterBoth},
	}
}

func (state *State) settingsIntenseOptions() [4]components.SegmentOption {
	m := state.messages.Tools
	return [...]components.SegmentOption{
		{ID: hitSettingsIntenseBoth, Label: m.IntenseBoth},
		{ID: hitSettingsIntenseBright, Label: m.IntenseBright},
		{ID: hitSettingsIntenseBold, Label: m.IntenseBold},
		{ID: hitSettingsIntensePlain, Label: m.IntensePlain},
	}
}

func (state *State) settingsScrollbackOptions() [3]components.SegmentOption {
	return [...]components.SegmentOption{
		{ID: hitSettingsScrollback1000, Label: "1,000"},
		{ID: hitSettingsScrollback5000, Label: "5,000"},
		{ID: hitSettingsScrollback10000, Label: "10,000"},
	}
}

func (state *State) settingsTimeoutOptions() [3]components.SegmentOption {
	return [...]components.SegmentOption{
		{ID: hitSettingsTimeout5, Label: "5 s"},
		{ID: hitSettingsTimeout10, Label: "10 s"},
		{ID: hitSettingsTimeout30, Label: "30 s"},
	}
}

func (state *State) settingsKeepAliveOptions() [3]components.SegmentOption {
	return [...]components.SegmentOption{
		{ID: hitSettingsKeepAliveOff, Label: state.messages.SettingsConnectionKeepAliveOff},
		{ID: hitSettingsKeepAlive30, Label: "30 s"},
		{ID: hitSettingsKeepAlive60, Label: "60 s"},
	}
}

func (state *State) settingsReconnectOptions() [3]components.SegmentOption {
	return [...]components.SegmentOption{
		{ID: hitSettingsReconnectOff, Label: state.messages.SettingsConnectionReconnectOff},
		{ID: hitSettingsReconnectThree, Label: state.messages.SettingsConnectionReconnectThree},
		{ID: hitSettingsReconnectForever, Label: state.messages.SettingsConnectionReconnectForever},
	}
}

func (state *State) settingsRestoreOptions() [3]components.SegmentOption {
	return [...]components.SegmentOption{
		{ID: hitSettingsRestoreOff, Label: state.messages.SettingsRestoreOff},
		{ID: hitSettingsRestoreActive, Label: state.messages.SettingsRestoreActive},
		{ID: hitSettingsRestoreAll, Label: state.messages.SettingsRestoreAll},
	}
}

func (state *State) settingsLocalStartOptions() [2]components.SegmentOption {
	return [...]components.SegmentOption{
		{ID: hitSettingsLocalStartRemember, Label: state.messages.SettingsLocalStartRemember},
		{ID: hitSettingsLocalStartHome, Label: state.messages.SettingsLocalStartHome},
	}
}

func (state *State) settingsClipboardOptions() [3]components.SegmentOption {
	return [...]components.SegmentOption{
		{ID: hitSettingsClipboardBlock, Label: state.messages.SettingsTerminalClipboardBlock},
		{ID: hitSettingsClipboardAsk, Label: state.messages.SettingsTerminalClipboardAsk},
		{ID: hitSettingsClipboardAllow, Label: state.messages.SettingsTerminalClipboardAllow},
	}
}

func (state *State) settingsCursorStyleOptions() []components.SegmentOption {
	return []components.SegmentOption{
		{ID: hitSettingsCursorBlock, Label: state.messages.Tools.CursorShapeBlock},
		{ID: hitSettingsCursorBar, Label: state.messages.Tools.CursorShapeBar},
		{ID: hitSettingsCursorUnderline, Label: state.messages.Tools.CursorShapeUnderline},
	}
}

func (state *State) settingsKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if state.modal != modalNone || state.activeSection != sectionSettings {
		return gfx.EventResult{}, false
	}
	if event.Target == hitSettingsLocalShell && localShellSettingVisible() && !state.localShellMenu.open && (event.Key == gfx.KeyDown || event.Key == gfx.KeyUp) {
		state.refreshLocalShells()
		if len(state.localShellMenu.choices) == 0 {
			return gfx.EventResult{}, false
		}
		state.localShellMenu.open = true
		return gfx.EventResult{Focus: hitSettingsLocalShellItems.At(max(0, state.localShellSelected())), FocusVisible: true, Handled: true, Repaint: true}, true
	}
	if event.Target == hitSettingsEditor && !state.editorMenu.open && (event.Key == gfx.KeyDown || event.Key == gfx.KeyUp) {
		state.refreshEditors()
		state.editorMenu.open = true
		return gfx.EventResult{Focus: hitSettingsEditorItems.At(max(0, state.editorSelected())), FocusVisible: true, Handled: true, Repaint: true}, true
	}
	var next gfx.ID
	var handled bool
	switch event.Target {
	case hitSettingsPanelLeft, hitSettingsPanelRight:
		options := state.settingsSFTPPanelPositionOptions()
		next, handled = components.SegmentedControlKeyDown(event.Target, options[:], components.DirectionLTR, event.Key)
	case hitSettingsPathBreadcrumbs, hitSettingsPathInput:
		options := state.settingsPathOptions()
		next, handled = components.SegmentedControlKeyDown(event.Target, options[:], components.DirectionLTR, event.Key)
	case hitSettingsLanguageEnglish, hitSettingsLanguageChinese, hitSettingsLanguageTraditional:
		options := state.settingsLanguageOptions()
		next, handled = components.SegmentedControlKeyDown(event.Target, options[:], components.DirectionLTR, event.Key)
	case hitSettingsAppearanceSystem, hitSettingsAppearanceDark, hitSettingsAppearanceLight:
		options := state.settingsAppearanceOptions()
		next, handled = components.SegmentedControlKeyDown(event.Target, options[:], components.DirectionLTR, event.Key)
	case hitSettingsTextSmall, hitSettingsTextRegular, hitSettingsTextLarge:
		options := state.settingsTextSizeOptions()
		next, handled = components.SegmentedControlKeyDown(event.Target, options[:], components.DirectionLTR, event.Key)
	case hitSettingsTabsHorizontal, hitSettingsTabsVertical:
		options := state.settingsTabOrientationOptions()
		next, handled = components.SegmentedControlKeyDown(event.Target, options[:], components.DirectionLTR, event.Key)
	case hitSettingsTabNumbersPrimary, hitSettingsTabNumbersAlt, hitSettingsTabNumbersShift:
		options := state.tabNumberOptions()
		next, handled = components.SegmentedControlKeyDown(event.Target, options[:], components.DirectionLTR, event.Key)
	case hitSettingsNetworkSystem, hitSettingsNetworkDirect:
		options := state.settingsNetworkProxyOptions()
		next, handled = components.SegmentedControlKeyDown(event.Target, options[:], components.DirectionLTR, event.Key)
	case hitSettingsToolbarLeft, hitSettingsToolbarRight:
		options := state.settingsToolbarPositionOptions()
		next, handled = components.SegmentedControlKeyDown(event.Target, options[:], components.DirectionLTR, event.Key)
	case hitSettingsCloseTray, hitSettingsCloseQuit:
		options := state.settingsCloseOptions()
		next, handled = components.SegmentedControlKeyDown(event.Target, options[:], components.DirectionLTR, event.Key)
	case hitSettingsRelaunchRaise, hitSettingsRelaunchNewWindow:
		options := state.settingsRelaunchOptions()
		next, handled = components.SegmentedControlKeyDown(event.Target, options[:], components.DirectionLTR, event.Key)
	case hitSettingsOptionText, hitSettingsOptionMeta:
		options := state.settingsOptionOptions()
		next, handled = components.SegmentedControlKeyDown(event.Target, options[:], components.DirectionLTR, event.Key)
	case hitSettingsPadCompact, hitSettingsPadNormal, hitSettingsPadRoomy:
		options := state.settingsPaddingOptions()
		next, handled = components.SegmentedControlKeyDown(event.Target, options[:], components.DirectionLTR, event.Key)
	case hitSettingsLineHeightCompact, hitSettingsLineHeightNormal, hitSettingsLineHeightRoomy:
		options := state.settingsLineHeightOptions()
		next, handled = components.SegmentedControlKeyDown(event.Target, options[:], components.DirectionLTR, event.Key)
	case hitSettingsCursorThin, hitSettingsCursorNormal, hitSettingsCursorThick:
		options := state.settingsCursorThicknessOptions()
		next, handled = components.SegmentedControlKeyDown(event.Target, options[:], components.DirectionLTR, event.Key)
	case hitSettingsGutterOff, hitSettingsGutterNumbers, hitSettingsGutterTime, hitSettingsGutterBoth:
		options := state.settingsGutterOptions()
		next, handled = components.SegmentedControlKeyDown(event.Target, options[:], components.DirectionLTR, event.Key)
	case hitSettingsIntenseBoth, hitSettingsIntenseBright, hitSettingsIntenseBold, hitSettingsIntensePlain:
		options := state.settingsIntenseOptions()
		next, handled = components.SegmentedControlKeyDown(event.Target, options[:], components.DirectionLTR, event.Key)
	case hitSettingsScrollback1000, hitSettingsScrollback5000, hitSettingsScrollback10000:
		options := state.settingsScrollbackOptions()
		next, handled = components.SegmentedControlKeyDown(event.Target, options[:], components.DirectionLTR, event.Key)
	case hitSettingsTimeout5, hitSettingsTimeout10, hitSettingsTimeout30:
		options := state.settingsTimeoutOptions()
		next, handled = components.SegmentedControlKeyDown(event.Target, options[:], components.DirectionLTR, event.Key)
	case hitSettingsKeepAliveOff, hitSettingsKeepAlive30, hitSettingsKeepAlive60:
		options := state.settingsKeepAliveOptions()
		next, handled = components.SegmentedControlKeyDown(event.Target, options[:], components.DirectionLTR, event.Key)
	case hitSettingsReconnectOff, hitSettingsReconnectThree, hitSettingsReconnectForever:
		options := state.settingsReconnectOptions()
		next, handled = components.SegmentedControlKeyDown(event.Target, options[:], components.DirectionLTR, event.Key)
	case hitSettingsCompletionOff, hitSettingsCompletionList, hitSettingsCompletionBoth:
		options := state.settingsCompletionOptions()
		next, handled = components.SegmentedControlKeyDown(event.Target, options[:], components.DirectionLTR, event.Key)
	case hitSettingsClipboardBlock, hitSettingsClipboardAsk, hitSettingsClipboardAllow:
		options := state.settingsClipboardOptions()
		next, handled = components.SegmentedControlKeyDown(event.Target, options[:], components.DirectionLTR, event.Key)
	case hitSettingsRestoreOff, hitSettingsRestoreActive, hitSettingsRestoreAll:
		options := state.settingsRestoreOptions()
		next, handled = components.SegmentedControlKeyDown(event.Target, options[:], components.DirectionLTR, event.Key)
	case hitSettingsLocalStartRemember, hitSettingsLocalStartHome:
		options := state.settingsLocalStartOptions()
		next, handled = components.SegmentedControlKeyDown(event.Target, options[:], components.DirectionLTR, event.Key)
	case hitSettingsAgentSystem, hitSettingsAgentOnePassword, hitSettingsAgentBitwarden, hitSettingsAgentCustom:
		options := state.settingsAgentOptions()
		next, handled = components.SegmentedControlKeyDown(event.Target, options[:], components.DirectionLTR, event.Key)
	default:
		return gfx.EventResult{}, false
	}
	if !handled {
		return gfx.EventResult{}, true
	}
	state.Click(next)
	return gfx.EventResult{Focus: next, FocusVisible: true, Handled: true, Repaint: true}, true
}
