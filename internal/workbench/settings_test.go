package workbench

import (
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"motty/internal/i18n"
)

// settingsChoiceIn finds a page Choice's segment by its label: the rows of the
// General page are Choices now, and their identities are the page's, not the
// tree's. The choice declared last wins, which is the one painted over another.
func settingsChoiceIn(canvas *gfx.Canvas, label string) (gfx.Hit, bool) {
	return settingsChoiceInHits(canvas.Hits(), label, true)
}

// settingsChoiceFirstIn is settingsChoiceIn for the first of several choices that
// offer the same words: the MCP page's HTTP and stdio access levels both say
// "Allow all", and the HTTP one is painted first.
func settingsChoiceFirstIn(canvas *gfx.Canvas, label string) (gfx.Hit, bool) {
	return settingsChoiceInHits(canvas.Hits(), label, false)
}

// settingsChoiceInHits finds a page Choice's segment by its label in a hit list,
// the last one when last is set.
func settingsChoiceInHits(hits []gfx.Hit, label string, last bool) (gfx.Hit, bool) {
	if last {
		for i := len(hits) - 1; i >= 0; i-- {
			if hit := hits[i]; hit.Label == label && hit.Role == gfx.RoleRadio {
				return hit, true
			}
		}
		return gfx.Hit{}, false
	}
	for _, hit := range hits {
		if hit.Label == label && hit.Role == gfx.RoleRadio {
			return hit, true
		}
	}
	return gfx.Hit{}, false
}

// settingsChoiceHit paints the settings page and finds the segment with label.
func settingsChoiceHit(t testing.TB, state *State, label string) gfx.Hit {
	t.Helper()
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 3400})
	if hit, ok := settingsChoiceIn(&canvas, label); ok {
		return hit
	}
	var offered []string
	for _, hit := range canvas.Hits() {
		if hit.Role == gfx.RoleRadio {
			offered = append(offered, hit.Label)
		}
	}
	t.Fatalf("no settings choice labeled %q (section %d, category %d); offered %q", label, state.activeSection, state.settingsCategory, offered)
	return gfx.Hit{}
}

// clickSettingsChoice delivers a click on a page Choice segment in the event
// pass, where the choice reads it.
func clickSettingsChoice(t testing.TB, state *State, label string) {
	t.Helper()
	clickSettingsChoiceHit(t, state, settingsChoiceHit(t, state, label))
}

// clickSettingsChoiceFirst is clickSettingsChoice for the first of several
// choices offering the same words.
func clickSettingsChoiceFirst(t testing.TB, state *State, label string) {
	t.Helper()
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 3400})
	hit, ok := settingsChoiceFirstIn(&canvas, label)
	if !ok {
		t.Fatalf("no settings choice labeled %q", label)
	}
	clickSettingsChoiceHit(t, state, hit)
}

func clickSettingsChoiceHit(t testing.TB, state *State, hit gfx.Hit) {
	t.Helper()
	var canvas gfx.Canvas
	canvas.BeginEventPass(hit.ID, gfx.ClickInfo{})
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 3400})
	canvas.EndEventPass()
}

// settingsChoiceInRow finds the segment labeled optionLabel in the Choice row
// whose setting name is rowLabel: several rows offer the same word (Normal is
// the padding, the line spacing and the cursor thickness), so a label alone can
// name more than one segment.
func settingsChoiceInRow(canvas *gfx.Canvas, rowLabel, optionLabel string) (gfx.Hit, bool) {
	rowY, found := float32(0), false
	for _, text := range canvas.Texts() {
		if text.Value == rowLabel {
			rowY, found = text.Bounds.Y+text.Bounds.Height/2, true
			break
		}
	}
	if !found {
		return gfx.Hit{}, false
	}
	var best gfx.Hit
	bestDistance, ok := float32(0), false
	for _, hit := range canvas.Hits() {
		if hit.Label != optionLabel || hit.Role != gfx.RoleRadio {
			continue
		}
		distance := hit.Bounds.Y + hit.Bounds.Height/2 - rowY
		if distance < 0 {
			distance = -distance
		}
		if !ok || distance < bestDistance {
			best, bestDistance, ok = hit, distance, true
		}
	}
	return best, ok
}

// settingsChoiceHitInRow paints the settings page and finds the segment with
// optionLabel in the row named rowLabel.
func settingsChoiceHitInRow(t testing.TB, state *State, rowLabel, optionLabel string) gfx.Hit {
	t.Helper()
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 3400})
	if hit, ok := settingsChoiceInRow(&canvas, rowLabel, optionLabel); ok {
		return hit
	}
	t.Fatalf("no settings choice labeled %q in the row %q", optionLabel, rowLabel)
	return gfx.Hit{}
}

// clickSettingsChoiceInRow delivers a click on the segment with optionLabel in
// the row named rowLabel.
func clickSettingsChoiceInRow(t testing.TB, state *State, rowLabel, optionLabel string) {
	t.Helper()
	hit := settingsChoiceHitInRow(t, state, rowLabel, optionLabel)
	var canvas gfx.Canvas
	canvas.BeginEventPass(hit.ID, gfx.ClickInfo{})
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 3400})
	canvas.EndEventPass()
}

// TestSettingsContentFillsTheWorkspaceSlot guards the one rectangle the shell
// computes twice: the slot its flex lays out and the bounds the settings are
// declared in. If they drift, the page paints outside the card it belongs to.
func TestSettingsContentFillsTheWorkspaceSlot(t *testing.T) {
	for _, size := range []gfx.Size{{Width: 1400, Height: 900}, {Width: 1000, Height: 700}, {Width: 900, Height: 520}} {
		state := newTestState(t, testOptions(t))
		state.Click(hitSettings)
		var canvas gfx.Canvas
		state.Paint(&canvas, size)
		want := state.chrome.workspaceSlot.Rect().Inset(gfx.All(workspaceBorder))
		if got := state.settingsWorkspaceBounds(gfx.Rect{Width: size.Width, Height: size.Height}); got != want {
			t.Fatalf("%v: settings content at %+v, workspace slot %+v", size, got, want)
		}
	}
}

// TestSettingsChoiceAndSearchTakeInputThroughTheWorkbenchPage delivers a choice
// row's click in the event pass and types into the search: both are controls the
// workbench page declares now, not a settings page of their own.
func TestSettingsChoiceAndSearchTakeInputThroughTheWorkbenchPage(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitSettings)
	size := gfx.Size{Width: 1400, Height: 900}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	page := state.chrome.windowPage
	if page == nil {
		t.Fatal("the workbench page was not declared")
	}

	// Typing reaches the search editor the page's search box names.
	if editor := state.EditorFor(hitSettingsSearch); editor != &state.settings.search {
		t.Fatalf("the search box is not the settings search editor: %p", editor)
	}
	typeInto(state, hitSettingsSearch, "keepalive")
	canvas.Reset()
	state.Paint(&canvas, size)
	if !state.settingsSearching() || state.settings.count == 0 {
		t.Fatalf("typing into the search box found %d settings", state.settings.count)
	}

	// Leave the search, then deliver a choice row's click in the event pass.
	state.Click(hitSettingsCategoryList.At(int(settingsGeneral)))
	canvas.Reset()
	state.Paint(&canvas, size)
	choice, ok := settingsChoiceIn(&canvas, state.messages.SettingsLanguageChinese)
	if !ok {
		t.Fatal("the language row painted no Chinese segment")
	}
	scope := tree.Sub(tree.Feature(shellPageRoot), tree.Name(settingsWorkspacePart))
	if !windowPageOwns(choice.ID) || !tree.Within(choice.ID.Scope(), scope) {
		t.Fatalf("the choice %d is not inside the workbench page's settings container", choice.ID)
	}
	canvas.BeginEventPass(choice.ID, gfx.ClickInfo{})
	state.Paint(&canvas, size)
	canvas.EndEventPass()
	if state.language != i18n.Chinese {
		t.Fatalf("language = %v after the choice's click", state.language)
	}
}

func TestSettingsEntriesOpenARealWorkspace(t *testing.T) {
	for _, entry := range []gfx.ID{hitSettings, hitWindowSettings} {
		state := newTestState(t, testOptions(t))
		state.Click(entry)
		if state.activeSection != sectionSettings {
			t.Fatalf("entry %d opened section %d", entry, state.activeSection)
		}
		// Each category paints its own groups; together they hold every choice.
		for _, category := range []struct {
			category settingsCategory
			texts    []string
			hits     []gfx.ID
			choices  []string
		}{
			{settingsGeneral, []string{state.messages.SettingsLanguage, state.messages.SettingsAppearance, state.messages.SettingsTextSize, state.messages.SettingsInterfaceFont}, []gfx.ID{hitInterfaceFontChoose, hitSettingsTextSizeDown, hitSettingsTextSizeReset, hitSettingsTextSizeUp}, nil},
			{settingsTerminal, []string{
				state.messages.SettingsTerminalOption, state.messages.SettingsTerminalFontSize, state.messages.SettingsTerminalFontFamily,
				state.messages.SettingsTerminalScrollback, state.messages.SettingsTerminalClipboard,
			}, []gfx.ID{hitSettingsFontSizeDown, hitSettingsFontSizeReset, hitSettingsFontSizeUp, hitSettingsFontPick},
				[]string{
					state.messages.SettingsTerminalOptionText, state.messages.SettingsTerminalOptionMeta,
					state.messages.Tools.PaddingCompact, state.messages.Tools.PaddingRoomy,
					state.messages.Tools.LineHeightCompact, state.messages.Tools.LineHeightRoomy,
					state.messages.Tools.CursorThin, state.messages.Tools.CursorThick,
					state.settingsScrollbackLabel(1000), state.settingsScrollbackLabel(10000),
					state.messages.SettingsTerminalClipboardBlock, state.messages.SettingsTerminalClipboardAllow,
				}},
			{settingsConnection, []string{
				state.messages.SettingsConnectionTimeout, state.messages.SettingsConnectionKeepAlive, state.messages.SettingsConnectionReconnect, state.messages.SettingsNetworkProxy,
			}, []gfx.ID{hitSettingsShareToggle, hitSettingsStartupHosts},
				[]string{
					state.settingsTimeoutLabel(5 * time.Second), state.settingsTimeoutLabel(30 * time.Second),
					state.settingsKeepAliveLabel(60 * time.Second),
					state.messages.SettingsConnectionReconnectThree, state.messages.SettingsConnectionReconnectForever,
					state.messages.SettingsRestoreActive, state.messages.SettingsRestoreAll,
					state.messages.SettingsNetworkProxySystem, state.messages.SettingsNetworkProxyDirect,
				}},
		} {
			state.selectSettingsCategory(category.category)
			// Tall enough for the whole category; a short window scrolls instead.
			var canvas gfx.Canvas
			state.Paint(&canvas, gfx.Size{Width: 1400, Height: 1800})
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
				if !tree.Within(id.Scope(), settingsHits) && id != hitSettingsFontPick && id != hitInterfaceFontChoose {
					t.Errorf("settings hit %d is outside the settings place", id)
				}
			}
			for _, label := range category.choices {
				if _, ok := settingsChoiceIn(&canvas, label); !ok {
					t.Errorf("entry %d category %d did not paint the choice %q", entry, category.category, label)
				}
			}
		}
	}
}

func TestSettingsCategoriesAreChosenFromTheListAndStartAtTheTop(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitSettings)
	size := gfx.Size{Width: 1000, Height: 640}
	var canvas gfx.Canvas
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
	if _, ok := settingsChoiceIn(&canvas, state.settingsTimeoutLabel(5*time.Second)); ok {
		t.Fatal("a connection choice is painted on the General page")
	}

	if _, ok := hitByID(&canvas, hitSettingsCategoryList.At(int(settingsShortcuts))); !ok {
		t.Fatal("the keyboard shortcuts category is not in the list")
	}
	state.Click(hitSettingsCategoryList.At(int(settingsTerminal)))
	canvas = gfx.Canvas{}
	state.Paint(&canvas, size)
	state.scrollSettings(gfx.ScrollEvent{DeltaY: 200, Precise: true})
	if state.settingsCategory != settingsTerminal || state.settingsScroll == 0 {
		t.Fatalf("category %d, scroll %v", state.settingsCategory, state.settingsScroll)
	}
	state.Click(hitSettingsCategoryList.At(int(settingsConnection)))
	if state.settingsCategory != settingsConnection || state.settingsScroll != 0 {
		t.Fatalf("category %d, scroll %v: a category opens at its top", state.settingsCategory, state.settingsScroll)
	}
	canvas = gfx.Canvas{}
	state.Paint(&canvas, size)
	if _, ok := settingsChoiceIn(&canvas, state.settingsTimeoutLabel(5*time.Second)); !ok {
		t.Fatal("the Connection page does not paint its choices")
	}
	if _, ok := settingsChoiceIn(&canvas, state.messages.SettingsLanguageEnglish); ok {
		t.Fatal("the Connection page paints a General choice")
	}
}

func TestSettingsChoicesApplyDirectlyToTheWorkbenchAndTerminal(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.sessions = append(state.sessions, Session{ID: 1, term: newTerminal(80, 24)})
	state.Click(hitSettings)
	if state.networkProxy != networkProxyDirect {
		t.Fatalf("default network proxy = %v", state.networkProxy)
	}
	clickSettingsChoice(t, state, state.messages.SettingsLanguageChinese)
	clickSettingsChoice(t, state, state.messages.SettingsAppearanceLight)
	state.selectSettingsCategory(settingsConnection)
	clickSettingsChoice(t, state, state.settingsNetworkProxyLabel(networkProxySystem))
	clickSettingsChoiceInRow(t, state, state.messages.SettingsConnectionTimeout, state.settingsTimeoutLabel(30*time.Second))
	clickSettingsChoiceInRow(t, state, state.messages.SettingsConnectionKeepAlive, state.settingsKeepAliveLabel(60*time.Second))
	clickSettingsChoice(t, state, state.settingsReconnectLabel(reconnectForever))
	state.selectSettingsCategory(settingsTerminal)
	clickSettingsChoice(t, state, state.settingsOptionLabel(terminalOptionMeta))
	for range 4 {
		state.Click(hitSettingsFontSizeUp)
	}
	state.useTerminalFont(fontEntry{face: gfx.MonoClassic})
	clickSettingsChoice(t, state, state.settingsScrollbackLabel(1000))
	clickSettingsChoice(t, state, state.settingsClipboardLabel(terminalClipboardAllow))

	if state.language != i18n.Chinese || state.messages.SettingsTitle != "设置" {
		t.Fatalf("language=%v title=%q", state.language, state.messages.SettingsTitle)
	}
	if state.appearance != appearanceLight || state.Theme().Background != lightPalette.workspace {
		t.Fatalf("appearance=%v theme=%+v", state.appearance, state.Theme())
	}
	if state.networkProxy != networkProxySystem {
		t.Fatalf("network proxy = %v", state.networkProxy)
	}
	if state.terminalOption != terminalOptionMeta {
		t.Fatalf("terminal option=%v", state.terminalOption)
	}
	if state.terminalFontSize != 17 || state.terminalFontFace != gfx.MonoClassic || state.terminalScrollback != 1000 || state.connectionTimeout != 30*time.Second || state.connectionKeepAlive != 60*time.Second || state.connectionReconnect != reconnectForever || state.terminalClipboard != terminalClipboardAllow {
		t.Fatalf("terminal settings = size %v face %v scrollback %d timeout %v keepalive %v reconnect %v clipboard %v", state.terminalFontSize, state.terminalFontFace, state.terminalScrollback, state.connectionTimeout, state.connectionKeepAlive, state.connectionReconnect, state.terminalClipboard)
	}
	input := state.TerminalInput(app.TextInputContext{ID: hitTerminalList.At(1), Terminal: true})
	if input == nil || !input.AltSendsEscape || input.FontSize != 17 || input.MonoFace != gfx.MonoClassic {
		t.Fatalf("terminal input=%+v, want AltSendsEscape", input)
	}

	clickSettingsChoice(t, state, state.settingsOptionLabel(terminalOptionText))
	if input := state.TerminalInput(app.TextInputContext{ID: hitTerminalList.At(1), Terminal: true}); input == nil || input.AltSendsEscape {
		t.Fatalf("terminal input=%+v, want text-producing Option", input)
	}
}

// Turning the probe off is a choice, and a state file cannot record it as a
// duration of zero: that value is dropped, so it would read as never having
// chosen, which now means on.
func TestTurningKeepAliveOffSurvivesRestart(t *testing.T) {
	options := freshOptions(t)
	first := newTestState(t, options)
	first.Click(hitSettings)
	first.selectSettingsCategory(settingsConnection)
	clickSettingsChoiceInRow(t, first, first.messages.SettingsConnectionKeepAlive, first.settingsKeepAliveLabel(0))
	first.save()

	saved, report, err := loadState(first.options.StatePath)
	if err != nil || report.outcome != loadOK {
		t.Fatalf("load saved settings: outcome=%d err=%v", report.outcome, err)
	}
	if !saved.ConnectionKeepAliveOff || connectionKeepAlive(saved) != 0 {
		t.Fatalf("saved keepalive = %+v, want the off choice", saved.ConnectionKeepAliveOff)
	}
	second := newTestState(t, options)
	if second.connectionKeepAlive != 0 {
		t.Fatalf("restored keepalive = %v, want off", second.connectionKeepAlive)
	}
}

func TestSettingsChoicesAndWorkspaceSurviveRestart(t *testing.T) {
	options := freshOptions(t)
	first := newTestState(t, options)
	first.Click(hitSettings)
	clickSettingsChoice(t, first, first.messages.SettingsLanguageChinese)
	clickSettingsChoice(t, first, first.messages.SettingsAppearanceLight)
	first.selectSettingsCategory(settingsConnection)
	clickSettingsChoice(t, first, first.settingsNetworkProxyLabel(networkProxySystem))
	clickSettingsChoiceInRow(t, first, first.messages.SettingsConnectionTimeout, first.settingsTimeoutLabel(5*time.Second))
	clickSettingsChoiceInRow(t, first, first.messages.SettingsConnectionKeepAlive, first.settingsKeepAliveLabel(30*time.Second))
	clickSettingsChoiceInRow(t, first, first.messages.SettingsConnectionReconnect, first.settingsReconnectLabel(reconnectThree))
	first.selectSettingsCategory(settingsTerminal)
	clickSettingsChoice(t, first, first.settingsOptionLabel(terminalOptionMeta))
	first.Click(hitSettingsFontSizeUp)
	first.Click(hitSettingsFontSizeUp)
	first.useTerminalFont(fontEntry{face: gfx.MonoClassic})
	first.save()
	clickSettingsChoice(t, first, first.settingsScrollbackLabel(10000))
	clickSettingsChoice(t, first, first.settingsClipboardLabel(terminalClipboardBlock))

	saved, report, err := loadState(first.options.StatePath)
	if err != nil || report.outcome != loadOK {
		t.Fatalf("load saved settings: outcome=%d err=%v", report.outcome, err)
	}
	if saved.NetworkProxy != "system" || saved.TerminalOption != "meta" || saved.TerminalFontSize != 15 || saved.TerminalFontFace != "classic" || saved.TerminalScrollback != 10000 || saved.ConnectionTimeoutSeconds != 5 || connectionKeepAlive(saved) != 30*time.Second || saved.ConnectionReconnect != "three" || saved.TerminalClipboard != "block" || saved.Workspace != "settings" {
		t.Fatalf("saved settings=%+v", saved)
	}

	second := newTestState(t, options)
	if second.activeSection != sectionSettings || second.language != i18n.Chinese ||
		second.appearance != appearanceLight || second.networkProxy != networkProxySystem || second.terminalOption != terminalOptionMeta || second.terminalFontSize != 15 || second.terminalFontFace != gfx.MonoClassic || second.terminalScrollback != 10000 || second.connectionTimeout != 5*time.Second || second.connectionKeepAlive != 30*time.Second || second.connectionReconnect != reconnectThree || second.terminalClipboard != terminalClipboardBlock {
		t.Fatalf("restored section=%d language=%v appearance=%v option=%v size=%v face=%v scrollback=%d timeout=%v keepalive=%v", second.activeSection, second.language, second.appearance, second.terminalOption, second.terminalFontSize, second.terminalFontFace, second.terminalScrollback, second.connectionTimeout, second.connectionKeepAlive)
	}
}

func TestWordSeparatorsInputPersistsAcrossRestart(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsTerminal)
	input := state.TerminalWordInput(app.TextInputContext{ID: hitSettingsWordSeparators})
	if input == nil || state.EditorFor(hitSettingsWordSeparators) == nil {
		t.Fatal("word separators field is not editable")
	}
	if _, err := input.OnEvent(app.TextEvent{Kind: app.TextInsert, Text: "-/"}); err != nil {
		t.Fatal(err)
	}
	if state.terminalWordSeparators != "-/" {
		t.Fatalf("configured separators = %q", state.terminalWordSeparators)
	}
	reopened := newTestState(t, options)
	if reopened.terminalWordSeparators != "-/" || reopened.terminalWordEditor.String() != "-/" {
		t.Fatalf("restored separators = %q, editor = %q", reopened.terminalWordSeparators, reopened.terminalWordEditor.String())
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
	state.selectSettingsCategory(settingsTerminal)
	clickSettingsChoice(t, state, state.settingsScrollbackLabel(1000))
	if terminal.screen.ScrollbackLimit() != 1000 {
		t.Fatalf("existing terminal scrollback max = %d", terminal.screen.ScrollbackLimit())
	}
	state.selectSettingsCategory(settingsConnection)
	clickSettingsChoiceInRow(t, state, state.messages.SettingsConnectionTimeout, state.settingsTimeoutLabel(30*time.Second))
	clickSettingsChoiceInRow(t, state, state.messages.SettingsConnectionKeepAlive, state.settingsKeepAliveLabel(60*time.Second))
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

	// rove moves the focus from one segment to the next with the right arrow and
	// delivers the activation the key reported, as the runtime does with an event
	// pass: the choice is applied by the paint that reads it.
	rove := func(category settingsCategory, rowLabel, fromLabel, toLabel string, applied func() bool) {
		t.Helper()
		state.selectSettingsCategory(category)
		var canvas gfx.Canvas
		state.Paint(&canvas, gfx.Size{Width: 1400, Height: 3400})
		from, ok := settingsChoiceInRow(&canvas, rowLabel, fromLabel)
		if !ok {
			t.Fatalf("%q: no choice labeled %q", rowLabel, fromLabel)
		}
		to, ok := settingsChoiceInRow(&canvas, rowLabel, toLabel)
		if !ok {
			t.Fatalf("%q: no choice labeled %q", rowLabel, toLabel)
		}
		result := windowKeyRoutes(state, gfx.KeyEvent{Target: from.ID, Key: gfx.KeyRight})
		if !result.Handled || result.Focus != to.ID || result.Activate != to.ID {
			t.Fatalf("%q: from=%v to=%v result=%+v", rowLabel, from.ID, to.ID, result)
		}
		canvas.Reset()
		canvas.BeginEventPass(result.Activate, gfx.ClickInfo{})
		state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
		canvas.EndEventPass()
		if !applied() {
			t.Fatalf("%q was not applied by the activated choice", rowLabel)
		}
	}
	m := state.messages

	rove(settingsTerminal, m.SettingsTerminalOption, state.settingsOptionLabel(terminalOptionText), state.settingsOptionLabel(terminalOptionMeta),
		func() bool { return state.terminalOption == terminalOptionMeta })
	rove(settingsTerminal, m.Tools.PaddingRow, m.Tools.PaddingNormal, m.Tools.PaddingRoomy,
		func() bool { return state.terminalPad == padRoomy })
	rove(settingsTerminal, m.Tools.LineHeightRow, m.Tools.LineHeightNormal, m.Tools.LineHeightRoomy,
		func() bool { return state.terminalLineHeight == lineHeightRoomy })
	rove(settingsTerminal, m.Tools.CursorThicknessRow, m.Tools.CursorNormal, m.Tools.CursorThick,
		func() bool { return state.terminalCursorThickness == cursorThick })
	rove(settingsTerminal, m.Tools.GutterRow, m.Tools.GutterOff, m.Tools.GutterNumbers,
		func() bool { return state.terminalGutter == gutterNumbers })
	rove(settingsTerminal, m.Tools.IntenseRow, m.Tools.IntenseBoth, m.Tools.IntenseBright,
		func() bool { return state.terminalIntense == intenseBright && state.termPal.intense == intenseBright })
	rove(settingsTerminal, m.SettingsTerminalScrollback, state.settingsScrollbackLabel(5000), state.settingsScrollbackLabel(10000),
		func() bool { return state.terminalScrollback == 10000 })
	rove(settingsTerminal, m.SettingsTerminalClipboard, state.settingsClipboardLabel(terminalClipboardBlock), state.settingsClipboardLabel(terminalClipboardAsk),
		func() bool { return state.terminalClipboard == terminalClipboardAsk })
	// Completion starts on its last choice, so move it off before roving on.
	clickSettingsChoiceInRow(t, state, m.History.SuggestRow, state.settingsCompletionLabel(completionOff))
	rove(settingsTerminal, m.History.SuggestRow, state.settingsCompletionLabel(completionOff), state.settingsCompletionLabel(completionList),
		func() bool { return state.completionMode() == completionList })
	rove(settingsTerminal, m.Tools.TabTitleRow, state.settingsTabTitleLabel(tabTitleTerminal), state.settingsTabTitleLabel(tabTitleDirectory),
		func() bool { return state.tabTitleMode == tabTitleDirectory })
	rove(settingsTerminal, m.SettingsLocalStart, state.settingsLocalStartLabel(localStartRemember), state.settingsLocalStartLabel(localStartHome),
		func() bool { return state.localStart == localStartHome })
	rove(settingsConnection, m.SettingsNetworkProxy, state.settingsNetworkProxyLabel(networkProxyDirect), state.settingsNetworkProxyLabel(networkProxySystem),
		func() bool { return state.networkProxy == networkProxySystem })
	rove(settingsConnection, m.SettingsConnectionTimeout, state.settingsTimeoutLabel(10*time.Second), state.settingsTimeoutLabel(30*time.Second),
		func() bool { return state.connectionTimeout == 30*time.Second })
	rove(settingsConnection, m.SettingsConnectionKeepAlive, state.settingsKeepAliveLabel(0), state.settingsKeepAliveLabel(30*time.Second),
		func() bool { return state.connectionKeepAlive == 30*time.Second })
	rove(settingsConnection, m.SettingsConnectionReconnect, state.settingsReconnectLabel(reconnectOff), state.settingsReconnectLabel(reconnectThree),
		func() bool { return state.connectionReconnect == reconnectThree })
	rove(settingsConnection, m.SettingsRestore, state.settingsRestoreLabel(restoreConnectOff), state.settingsRestoreLabel(restoreConnectActive),
		func() bool { return state.restoreConnect == restoreConnectActive })
	rove(settingsConnection, m.SettingsSSHAgent, state.settingsAgentKindLabel(agentKindSystem), state.settingsAgentKindLabel(agentKindOnePassword),
		func() bool { return state.sshAgent.kind == agentKindOnePassword })
	// The language changes the row's own words, so it goes last.
	rove(settingsGeneral, m.SettingsLanguage, state.settingsLanguageLabel(i18n.English), state.settingsLanguageLabel(i18n.Chinese),
		func() bool { return state.language == i18n.Chinese })
}

func TestSettingsWorkspacePaintSchedulesNoIdleFrame(t *testing.T) {
	state := newTestState(t, testOptions(t))
	// Opening Terminal settings normally discovers Windows shells and starts a
	// WSL listing goroutine. Those system-specific allocations are unrelated to
	// painting and can land during another category's measurement.
	state.localShellMenu.loaded = true
	state.Click(hitSettings)
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1400, Height: 900}
	for category := settingsCategory(0); category < settingsCategoryCount; category++ {
		state.selectSettingsCategory(category)
		state.Paint(&canvas, size)
		canvas.Reset()
		state.Paint(&canvas, size)
		if _, scheduled := canvas.NextFrame(); scheduled {
			t.Fatalf("idle settings category %d scheduled another frame", category)
		}
	}
}

func BenchmarkPaintSettingsWorkspace(b *testing.B) {
	state := newTestState(b, testOptions(b))
	state.Click(hitSettings)
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1400, Height: 900}
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
	size := gfx.Size{Width: 1000, Height: 520}
	paint := func() *gfx.Canvas {
		var canvas gfx.Canvas
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
		state.Scroll(gfx.ScrollEvent{DeltaY: 60, Precise: true, Viewport: size})
	}
	canvas = paint()
	last, ok = hitByID(canvas, hitAISetClearHistory) // the assistant's category ends the page
	if !ok || last.Bounds.Y+last.Bounds.Height > size.Height {
		t.Fatalf("after scrolling the last row is %+v, %v", last.Bounds, ok)
	}
	bottom := state.settingsScroll
	state.Scroll(gfx.ScrollEvent{DeltaY: 60, Precise: true, Viewport: size})
	if state.settingsScroll != bottom {
		t.Fatalf("scrolled past the end: %v then %v", bottom, state.settingsScroll)
	}

	// And back to the top, without going above it.
	for range 80 {
		state.Scroll(gfx.ScrollEvent{DeltaY: -60, Precise: true, Viewport: size})
	}
	if state.settingsScroll != 0 {
		t.Fatalf("scroll = %v, want the top", state.settingsScroll)
	}
}

func TestSettingsScrollDoesNotOverlapWorkspaceBorder(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitSettings)
	size := gfx.Size{Width: 1000, Height: 640}
	state.selectSettingsCategory(settingsTerminal)
	// Paint once so the page knows how tall it is and how much room it has, then
	// take the wheel to the end: a later paint must not pull the page back.
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	state.scrollSettings(gfx.ScrollEvent{DeltaY: 10000, Precise: true})
	scrolled := state.settingsScroll
	if scrolled <= 0 {
		t.Fatal("the wheel did not scroll the settings page")
	}
	// The wheel must reach the end the paint allows: the page's content box, not
	// the window's own height.
	if want := max(state.settingsContent-state.settings.pageView, 0); scrolled != want {
		t.Fatalf("the wheel stopped at %v, but the page ends at %v", scrolled, want)
	}
	var again gfx.Canvas
	state.Paint(&again, size)
	if state.settingsScroll != scrolled {
		t.Fatalf("paint moved the page from %v to %v: the wheel and the paint clamp disagree", scrolled, state.settingsScroll)
	}

	// Every settings rectangle is clipped to the workspace content box, so none
	// can draw over the slot's own border.
	foundSettingsRect := false
	for _, r := range again.Rectangles() {
		if r.Color != state.pal.surface {
			continue
		}
		foundSettingsRect = true
		if !r.HasClip {
			t.Fatalf("settings surface rectangle has no clip: %+v", r)
		}
		if r.ClipOf.Y < windowBarHeight+workspaceBorder {
			t.Fatalf("settings surface rectangle ClipOf.Y = %v, expected at least %v", r.ClipOf.Y, windowBarHeight+workspaceBorder)
		}
	}
	if !foundSettingsRect {
		t.Fatal("did not find any settings surface rectangles")
	}
}
