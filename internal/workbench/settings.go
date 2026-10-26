package workbench

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
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

func (state *State) setTerminalFontSize(value float32) bool {
	value = normalizeTerminalFontSize(value)
	if state.terminalFontSize == value {
		return false
	}
	state.terminalFontSize = value
	state.cellWidth, state.cellHeight = (&ui.Canvas{}).MonoCellSize(value, state.terminalFontFace)
	state.fitTerminals()
	return true
}

func (state *State) setTerminalFontFace(face ui.MonoFace) bool {
	if face > ui.MonoClassic && ui.MonoFamilyName(face) == "" {
		face = ui.MonoSystem
	}
	if state.terminalFontFace == face {
		return false
	}
	state.terminalFontFace = face
	state.cellWidth, state.cellHeight = (&ui.Canvas{}).MonoCellSize(state.terminalFontSize, face)
	state.fitTerminals()
	return true
}

func terminalFontFaceString(face ui.MonoFace) string {
	if face == ui.MonoClassic {
		return "classic"
	}
	return "system" // a named family is kept beside it, in its own field
}

func terminalFontFaceFromString(value string) ui.MonoFace {
	if value == "classic" {
		return ui.MonoClassic
	}
	return ui.MonoSystem
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

func (state *State) openSettings() {
	if session, ok := state.sessionByIDPointer(state.activeSessionID); ok {
		state.closeSFTP(session)
	}
	state.activeSection = sectionSettings
	state.settingsScroll = 0
	state.aiSettingsOpened()
	state.save()
}

// scrollSettings moves the settings page. As everywhere in Viva a positive
// delta reveals content below. A wheel notch is worth at least a row's height
// of travel; a trackpad moves it by exactly what it reports.
func (state *State) scrollSettings(event ui.ScrollEvent) ui.EventResult {
	step := event.DeltaY
	if !event.Precise && step > 0 && step < 24 {
		step = 24
	} else if !event.Precise && step < 0 && step > -24 {
		step = -24
	}
	before := state.settingsScroll
	state.settingsScroll = min(max(before+step, 0), max(state.settingsContent-state.viewport.Height+windowBarHeight+7, 0))
	return ui.EventResult{Handled: true, Repaint: state.settingsScroll != before}
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
			return i18n.Chinese
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
	state.status = state.messages.StatusReadyIdle
	state.refreshHostSelection()
	state.refreshHostGroups()
	state.refreshIdentityCount()
	state.refreshIdentityCells()
	state.refreshForwards()
	state.refreshProxies()
	for index := range state.sessions {
		state.sessions[index].reconnectText = ""
		state.describe(&state.sessions[index])
	}
	state.describeTerminalFinds()
	if state.modal == modalCommandPalette {
		state.rebuildCommandPalette()
	}
	return true
}

func (state *State) clickSettings(id ui.ID) bool {
	if state.modal != modalNone || state.activeSection != sectionSettings {
		return false
	}
	changed := false
	switch id {
	case hitSettingsLanguageEnglish:
		changed = state.chooseLanguage(i18n.English)
	case hitSettingsLanguageChinese:
		changed = state.chooseLanguage(i18n.Chinese)
	case hitSettingsAppearanceSystem:
		changed = state.chooseAppearanceMode(appearanceFollowSystem)
	case hitSettingsAppearanceDark:
		changed = state.chooseAppearanceMode(appearancePinnedDark)
	case hitSettingsAppearanceLight:
		changed = state.chooseAppearanceMode(appearancePinnedLight)
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
	case hitSettingsGutterOff:
		changed = state.setTerminalGutter(gutterOff)
	case hitSettingsGutterNumbers:
		changed = state.setTerminalGutter(gutterNumbers)
	case hitSettingsGutterTime:
		changed = state.setTerminalGutter(gutterTime)
	case hitSettingsGutterBoth:
		changed = state.setTerminalGutter(gutterBoth)
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
	case hitSettingsShareToggle:
		if state.shareConnections == shareConnectionsOff {
			changed = state.setShareConnections(shareConnectionsOn)
		} else {
			changed = state.setShareConnections(shareConnectionsOff)
		}
	default:
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

func (state *State) settingsLanguageOptions() [2]components.SegmentOption {
	return [...]components.SegmentOption{
		{ID: hitSettingsLanguageEnglish, Label: state.messages.SettingsLanguageEnglish},
		{ID: hitSettingsLanguageChinese, Label: state.messages.SettingsLanguageChinese},
	}
}

func (state *State) settingsAppearanceOptions() [3]components.SegmentOption {
	return [...]components.SegmentOption{
		{ID: hitSettingsAppearanceSystem, Label: state.messages.SettingsAppearanceSystem},
		{ID: hitSettingsAppearanceDark, Label: state.messages.SettingsAppearanceDark},
		{ID: hitSettingsAppearanceLight, Label: state.messages.SettingsAppearanceLight},
	}
}

func (state *State) settingsOptionOptions() [2]components.SegmentOption {
	return [...]components.SegmentOption{
		{ID: hitSettingsOptionText, Label: state.messages.SettingsTerminalOptionText},
		{ID: hitSettingsOptionMeta, Label: state.messages.SettingsTerminalOptionMeta},
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

func (state *State) settingsGutterOptions() [4]components.SegmentOption {
	m := state.messages.Tools
	return [...]components.SegmentOption{
		{ID: hitSettingsGutterOff, Label: m.GutterOff},
		{ID: hitSettingsGutterNumbers, Label: m.GutterNumbers},
		{ID: hitSettingsGutterTime, Label: m.GutterTime},
		{ID: hitSettingsGutterBoth, Label: m.GutterBoth},
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

func (state *State) settingsClipboardOptions() [3]components.SegmentOption {
	return [...]components.SegmentOption{
		{ID: hitSettingsClipboardBlock, Label: state.messages.SettingsTerminalClipboardBlock},
		{ID: hitSettingsClipboardAsk, Label: state.messages.SettingsTerminalClipboardAsk},
		{ID: hitSettingsClipboardAllow, Label: state.messages.SettingsTerminalClipboardAllow},
	}
}

func (state *State) settingsKey(event ui.KeyEvent) (ui.EventResult, bool) {
	if state.modal != modalNone || state.activeSection != sectionSettings {
		return ui.EventResult{}, false
	}
	var next ui.ID
	var handled bool
	switch event.Target {
	case hitSettingsLanguageEnglish, hitSettingsLanguageChinese:
		options := state.settingsLanguageOptions()
		next, handled = components.SegmentedControlKeyDown(event.Target, options[:], components.DirectionLTR, event.Key)
	case hitSettingsAppearanceSystem, hitSettingsAppearanceDark, hitSettingsAppearanceLight:
		options := state.settingsAppearanceOptions()
		next, handled = components.SegmentedControlKeyDown(event.Target, options[:], components.DirectionLTR, event.Key)
	case hitSettingsOptionText, hitSettingsOptionMeta:
		options := state.settingsOptionOptions()
		next, handled = components.SegmentedControlKeyDown(event.Target, options[:], components.DirectionLTR, event.Key)
	case hitSettingsPadCompact, hitSettingsPadNormal, hitSettingsPadRoomy:
		options := state.settingsPaddingOptions()
		next, handled = components.SegmentedControlKeyDown(event.Target, options[:], components.DirectionLTR, event.Key)
	case hitSettingsGutterOff, hitSettingsGutterNumbers, hitSettingsGutterTime, hitSettingsGutterBoth:
		options := state.settingsGutterOptions()
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
	case hitSettingsClipboardBlock, hitSettingsClipboardAsk, hitSettingsClipboardAllow:
		options := state.settingsClipboardOptions()
		next, handled = components.SegmentedControlKeyDown(event.Target, options[:], components.DirectionLTR, event.Key)
	case hitSettingsRestoreOff, hitSettingsRestoreActive, hitSettingsRestoreAll:
		options := state.settingsRestoreOptions()
		next, handled = components.SegmentedControlKeyDown(event.Target, options[:], components.DirectionLTR, event.Key)
	default:
		return ui.EventResult{}, false
	}
	if !handled {
		return ui.EventResult{}, true
	}
	state.Click(next)
	return ui.EventResult{Focus: next, FocusVisible: true, Handled: true, Repaint: true}, true
}
