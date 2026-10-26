package workbench

import (
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
	"motty/internal/i18n"
)

func TestSettingsEntriesOpenARealWorkspace(t *testing.T) {
	for _, entry := range []ui.ID{hitSettings, hitWindowSettings} {
		state := newTestState(t, testOptions(t))
		state.Click(entry)
		if state.activeSection != sectionSettings {
			t.Fatalf("entry %d opened section %d", entry, state.activeSection)
		}
		// Each category paints its own groups; together they hold every choice.
		for _, category := range []struct {
			category settingsCategory
			texts    []string
			hits     []ui.ID
		}{
			{settingsGeneral, []string{state.messages.SettingsLanguage, state.messages.SettingsAppearance}, []ui.ID{hitSettingsLanguageEnglish, hitSettingsLanguageChinese, hitSettingsAppearanceDark, hitSettingsAppearanceLight}},
			{settingsTerminal, []string{
				state.messages.SettingsTerminalOption, state.messages.SettingsTerminalFontSize, state.messages.SettingsTerminalFontFamily,
				state.messages.SettingsTerminalScrollback, state.messages.SettingsTerminalClipboard,
			}, []ui.ID{hitSettingsOptionText, hitSettingsOptionMeta, hitSettingsFontSizeDown, hitSettingsFontSizeReset, hitSettingsFontSizeUp, hitSettingsPadCompact, hitSettingsPadRoomy, hitSettingsFontPick,
				hitSettingsScrollback1000, hitSettingsScrollback10000, hitSettingsClipboardBlock, hitSettingsClipboardAllow}},
			{settingsConnection, []string{
				state.messages.SettingsConnectionTimeout, state.messages.SettingsConnectionKeepAlive, state.messages.SettingsConnectionReconnect,
			}, []ui.ID{hitSettingsTimeout5, hitSettingsTimeout30, hitSettingsKeepAliveOff, hitSettingsKeepAlive60, hitSettingsReconnectOff, hitSettingsReconnectForever,
				hitSettingsShareToggle, hitSettingsRestoreOff, hitSettingsRestoreAll}},
		} {
			state.selectSettingsCategory(category.category)
			// Tall enough for the whole category; a short window scrolls instead.
			var canvas ui.Canvas
			state.Paint(&canvas, ui.Size{Width: 1400, Height: 1800})
			var texts []string
			for _, text := range canvas.Texts() {
				texts = append(texts, text.Value)
			}
			title := state.settingsCategoryName(category.category)
			for _, text := range append([]string{state.messages.SettingsTitle, title}, category.texts...) {
				if !contains(texts, text) {
					t.Errorf("entry %d category %d did not paint %q", entry, category.category, text)
				}
			}
			for _, id := range category.hits {
				if _, ok := hitByID(&canvas, id); !ok {
					t.Errorf("entry %d category %d did not paint settings hit %d", entry, category.category, id)
				}
				if !tree.Within(id.Scope(), settingsHits) && id != hitSettingsFontPick {
					t.Errorf("settings hit %d is outside the settings place", id)
				}
			}
		}
	}
}

func TestSettingsCategoriesAreChosenFromTheListAndStartAtTheTop(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitSettings)
	size := ui.Size{Width: 1000, Height: 520}
	var canvas ui.Canvas
	state.Paint(&canvas, size)
	for category := settingsCategory(0); category < settingsCategoryCount; category++ {
		hit, ok := hitByID(&canvas, hitSettingsCategoryList.At(int(category)))
		if !ok {
			t.Fatalf("category %d has no entry in the list", category)
		}
		if hit.Selected != (category == settingsGeneral) {
			t.Errorf("category %d selected = %v", category, hit.Selected)
		}
	}
	if _, ok := hitByID(&canvas, hitSettingsTimeout5); ok {
		t.Fatal("a connection choice is painted on the General page")
	}

	if _, ok := hitByID(&canvas, hitSettingsCategoryList.At(int(settingsShortcuts))); !ok {
		t.Fatal("the keyboard shortcuts category is not in the list")
	}
	state.Click(hitSettingsCategoryList.At(int(settingsTerminal)))
	canvas = ui.Canvas{}
	state.Paint(&canvas, size)
	state.scrollSettings(ui.ScrollEvent{DeltaY: 200, Precise: true})
	if state.settingsCategory != settingsTerminal || state.settingsScroll == 0 {
		t.Fatalf("category %d, scroll %v", state.settingsCategory, state.settingsScroll)
	}
	state.Click(hitSettingsCategoryList.At(int(settingsConnection)))
	if state.settingsCategory != settingsConnection || state.settingsScroll != 0 {
		t.Fatalf("category %d, scroll %v: a category opens at its top", state.settingsCategory, state.settingsScroll)
	}
	canvas = ui.Canvas{}
	state.Paint(&canvas, size)
	if _, ok := hitByID(&canvas, hitSettingsTimeout5); !ok {
		t.Fatal("the Connection page does not paint its choices")
	}
	if _, ok := hitByID(&canvas, hitSettingsLanguageEnglish); ok {
		t.Fatal("the Connection page paints a General choice")
	}
}

func TestSettingsChoicesApplyDirectlyToTheWorkbenchAndTerminal(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.sessions = append(state.sessions, Session{ID: 1, term: newTerminal(80, 24)})
	state.Click(hitSettings)
	state.Click(hitSettingsLanguageChinese)
	state.Click(hitSettingsAppearanceLight)
	state.Click(hitSettingsOptionMeta)
	for range 4 {
		state.Click(hitSettingsFontSizeUp)
	}
	state.useTerminalFont(fontEntry{face: ui.MonoClassic})
	state.Click(hitSettingsScrollback1000)
	state.Click(hitSettingsTimeout30)
	state.Click(hitSettingsKeepAlive60)
	state.Click(hitSettingsReconnectForever)
	state.Click(hitSettingsClipboardAllow)

	if state.language != i18n.Chinese || state.messages.SettingsTitle != "设置" {
		t.Fatalf("language=%v title=%q", state.language, state.messages.SettingsTitle)
	}
	if state.appearance != appearanceLight || state.Theme().Background != lightPalette.workspace {
		t.Fatalf("appearance=%v theme=%+v", state.appearance, state.Theme())
	}
	if state.terminalOption != terminalOptionMeta {
		t.Fatalf("terminal option=%v", state.terminalOption)
	}
	if state.terminalFontSize != 17 || state.terminalFontFace != ui.MonoClassic || state.terminalScrollback != 1000 || state.connectionTimeout != 30*time.Second || state.connectionKeepAlive != 60*time.Second || state.connectionReconnect != reconnectForever || state.terminalClipboard != terminalClipboardAllow {
		t.Fatalf("terminal settings = size %v face %v scrollback %d timeout %v keepalive %v reconnect %v clipboard %v", state.terminalFontSize, state.terminalFontFace, state.terminalScrollback, state.connectionTimeout, state.connectionKeepAlive, state.connectionReconnect, state.terminalClipboard)
	}
	input := state.TerminalInput(app.TextInputContext{ID: hitTerminalList.At(1), Terminal: true})
	if input == nil || !input.AltSendsEscape || input.FontSize != 17 || input.MonoFace != ui.MonoClassic {
		t.Fatalf("terminal input=%+v, want AltSendsEscape", input)
	}

	state.Click(hitSettingsOptionText)
	if input := state.TerminalInput(app.TextInputContext{ID: hitTerminalList.At(1), Terminal: true}); input == nil || input.AltSendsEscape {
		t.Fatalf("terminal input=%+v, want text-producing Option", input)
	}
}

func TestSettingsChoicesAndWorkspaceSurviveRestart(t *testing.T) {
	options := freshOptions(t)
	first := newTestState(t, options)
	first.Click(hitSettings)
	first.Click(hitSettingsLanguageChinese)
	first.Click(hitSettingsAppearanceLight)
	first.Click(hitSettingsOptionMeta)
	first.Click(hitSettingsFontSizeUp)
	first.Click(hitSettingsFontSizeUp)
	first.useTerminalFont(fontEntry{face: ui.MonoClassic})
	first.save()
	first.Click(hitSettingsScrollback10000)
	first.Click(hitSettingsTimeout5)
	first.Click(hitSettingsKeepAlive30)
	first.Click(hitSettingsReconnectThree)
	first.Click(hitSettingsClipboardBlock)

	saved, report, err := loadState(first.options.StatePath)
	if err != nil || report.outcome != loadOK {
		t.Fatalf("load saved settings: outcome=%d err=%v", report.outcome, err)
	}
	if saved.TerminalOption != "meta" || saved.TerminalFontSize != 15 || saved.TerminalFontFace != "classic" || saved.TerminalScrollback != 10000 || saved.ConnectionTimeoutSeconds != 5 || saved.ConnectionKeepAliveSeconds != 30 || saved.ConnectionReconnect != "three" || saved.TerminalClipboard != "block" || saved.Workspace != "settings" {
		t.Fatalf("saved settings=%+v", saved)
	}

	second := newTestState(t, options)
	if second.activeSection != sectionSettings || second.language != i18n.Chinese ||
		second.appearance != appearanceLight || second.terminalOption != terminalOptionMeta || second.terminalFontSize != 15 || second.terminalFontFace != ui.MonoClassic || second.terminalScrollback != 10000 || second.connectionTimeout != 5*time.Second || second.connectionKeepAlive != 30*time.Second || second.connectionReconnect != reconnectThree || second.terminalClipboard != terminalClipboardBlock {
		t.Fatalf("restored section=%d language=%v appearance=%v option=%v size=%v face=%v scrollback=%d timeout=%v keepalive=%v", second.activeSection, second.language, second.appearance, second.terminalOption, second.terminalFontSize, second.terminalFontFace, second.terminalScrollback, second.connectionTimeout, second.connectionKeepAlive)
	}
}

func TestSettingsApplyScrollbackToExistingTerminalsAndReliabilityToNewConnections(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	terminal := newTerminalWithScrollback(10, 2, 5000)
	for index := 0; index < 20; index++ {
		terminal.screen.Write([]byte("history\r\n"))
	}
	state.sessions = append(state.sessions, Session{ID: 1, term: terminal})
	state.Click(hitSettings)
	state.Click(hitSettingsScrollback1000)
	if terminal.screen.ScrollbackLimit() != 1000 {
		t.Fatalf("existing terminal scrollback max = %d", terminal.screen.ScrollbackLimit())
	}
	state.Click(hitSettingsTimeout30)
	state.Click(hitSettingsKeepAlive60)
	state.activeSection = sectionHosts
	openHost(state)
	config := world.configs[len(world.configs)-1]
	if config.DialTimeout != 30*time.Second || config.KeepAlive.Interval != 60*time.Second || config.KeepAlive.Timeout != 10*time.Second {
		t.Fatalf("connection reliability config = %+v", config)
	}
}

func TestSettingsChoicesHaveRovingKeyboardFocus(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitSettings)

	result := state.Key(ui.KeyEvent{Target: hitSettingsLanguageEnglish, Key: ui.KeyRight})
	if !result.Handled || result.Focus != hitSettingsLanguageChinese || state.language != i18n.Chinese {
		t.Fatalf("language result=%+v language=%v", result, state.language)
	}
	result = state.Key(ui.KeyEvent{Target: hitSettingsOptionText, Key: ui.KeyRight})
	if !result.Handled || result.Focus != hitSettingsOptionMeta || state.terminalOption != terminalOptionMeta {
		t.Fatalf("option result=%+v option=%v", result, state.terminalOption)
	}
	for _, test := range []struct {
		from, to ui.ID
		check    func() bool
	}{
		{hitSettingsPadNormal, hitSettingsPadRoomy, func() bool { return state.terminalPad == padRoomy }},
		{hitSettingsScrollback5000, hitSettingsScrollback10000, func() bool { return state.terminalScrollback == 10000 }},
		{hitSettingsTimeout10, hitSettingsTimeout30, func() bool { return state.connectionTimeout == 30*time.Second }},
		{hitSettingsKeepAliveOff, hitSettingsKeepAlive30, func() bool { return state.connectionKeepAlive == 30*time.Second }},
		{hitSettingsReconnectOff, hitSettingsReconnectThree, func() bool { return state.connectionReconnect == reconnectThree }},
		{hitSettingsClipboardBlock, hitSettingsClipboardAsk, func() bool { return state.terminalClipboard == terminalClipboardAsk }},
	} {
		result = state.Key(ui.KeyEvent{Target: test.from, Key: ui.KeyRight})
		if !result.Handled || result.Focus != test.to || !test.check() {
			t.Fatalf("setting %d result=%+v", test.from, result)
		}
	}
}

func TestSettingsWorkspacePaintDoesNotAllocateOnceWarm(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitSettings)
	var canvas ui.Canvas
	size := ui.Size{Width: 1400, Height: 900}
	for category := settingsCategory(0); category < settingsCategoryCount; category++ {
		state.selectSettingsCategory(category)
		state.Paint(&canvas, size)
		if allocations := testing.AllocsPerRun(100, func() {
			canvas.Reset()
			state.Paint(&canvas, size)
		}); allocations != 0 {
			t.Fatalf("settings category %d paint allocations = %v", category, allocations)
		}
		if _, scheduled := canvas.NextFrame(); scheduled {
			t.Fatalf("idle settings category %d scheduled another frame", category)
		}
	}
}

func BenchmarkPaintSettingsWorkspace(b *testing.B) {
	state := newTestState(b, testOptions(b))
	state.Click(hitSettings)
	var canvas ui.Canvas
	size := ui.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)
	b.ReportAllocs()
	for b.Loop() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}
}

func TestSettingsPageScrollsWhenTheWindowIsShort(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsAssistant)
	size := ui.Size{Width: 1000, Height: 520}
	paint := func() *ui.Canvas {
		var canvas ui.Canvas
		state.Paint(&canvas, size)
		return &canvas
	}
	canvas := paint()
	last, ok := hitByID(canvas, hitAISetClearHistory)
	if ok && last.Bounds.Y+last.Bounds.Height <= size.Height {
		t.Fatalf("the last row already fits a short window: %+v", last.Bounds)
	}
	if state.settingsContent <= size.Height {
		t.Fatalf("page height %v does not exceed the window", state.settingsContent)
	}

	// A positive delta reveals content below; far enough reveals the last row and then stops.
	for range 120 {
		state.Scroll(ui.ScrollEvent{DeltaY: 60, Precise: true, Viewport: size})
	}
	canvas = paint()
	last, ok = hitByID(canvas, hitAISetClearHistory) // the assistant's category ends the page
	if !ok || last.Bounds.Y+last.Bounds.Height > size.Height {
		t.Fatalf("after scrolling the last row is %+v, %v", last.Bounds, ok)
	}
	bottom := state.settingsScroll
	state.Scroll(ui.ScrollEvent{DeltaY: 60, Precise: true, Viewport: size})
	if state.settingsScroll != bottom {
		t.Fatalf("scrolled past the end: %v then %v", bottom, state.settingsScroll)
	}

	// And back to the top, without going above it.
	for range 80 {
		state.Scroll(ui.ScrollEvent{DeltaY: -60, Precise: true, Viewport: size})
	}
	if state.settingsScroll != 0 {
		t.Fatalf("scroll = %v, want the top", state.settingsScroll)
	}
}
