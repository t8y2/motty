package workbench

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"motty/internal/i18n"
)

var testFonts = []string{"Fira Code", "Iosevka", "JetBrains Mono", "Menlo"}

func fontOptions(t testing.TB, families []string) Options {
	options := freshOptions(t)
	options.StatePath = options.KnownHostsPath[:strings.LastIndex(options.KnownHostsPath, "known_hosts")] + "state.json"
	options.Keystore = &memKeys{}
	options.MonoFamilies = func() []string { return families }
	return options
}

// waitFor drives the window's wake-ups until condition holds: the font list is
// read off the window thread and arrives through them.
func waitFor(t testing.TB, state *State, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for the window")
		}
		state.Wake()
		time.Sleep(2 * time.Millisecond)
	}
}

func TestChoosingAnInstalledFontSwitchesTheTerminalAndIsKept(t *testing.T) {
	options := fontOptions(t, testFonts)
	state := newTestState(t, options)
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsTerminal)
	state.Click(hitSettingsFontPick)
	if state.modal != modalFontPicker || !state.fonts.loading {
		t.Fatalf("modal %d, loading %v", state.modal, state.fonts.loading)
	}
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900}) // "Reading the installed fonts…"
	waitFor(t, state, func() bool { return !state.fonts.loading })
	canvas = gfx.Canvas{}
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	texts := canvas.Texts()
	var labels []string
	for _, text := range texts {
		labels = append(labels, text.Value)
	}
	for _, want := range append([]string{state.messages.SettingsTerminalFontSystem, state.messages.SettingsTerminalFontClassic}, testFonts...) {
		if !contains(labels, want) {
			t.Fatalf("the picker does not list %q: %v", want, labels)
		}
	}

	// Typing narrows the list; Enter takes the highlighted row.
	typeInto(state, hitFontPickerSearch, "jet")
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if len(state.fonts.filtered) != 1 {
		t.Fatalf("filtered to %d rows", len(state.fonts.filtered))
	}
	before := state.cellWidth
	state.Key(gfx.KeyEvent{Key: gfx.KeyEnter, Target: hitFontPickerSearch})
	if state.modal != modalNone || state.fonts.family != "JetBrains Mono" || gfx.MonoFamilyName(state.terminalFontFace) != "JetBrains Mono" {
		t.Fatalf("modal %d, family %q, face %d", state.modal, state.fonts.family, state.terminalFontFace)
	}
	_ = before
	if !strings.Contains(state.fontDescription(), "JetBrains Mono") {
		t.Fatalf("description %q", state.fontDescription())
	}

	// A new window starts with it, and does not need the list to draw.
	restored := newTestState(t, options)
	if restored.fonts.family != "JetBrains Mono" || gfx.MonoFamilyName(restored.terminalFontFace) != "JetBrains Mono" {
		t.Fatalf("restored family %q face %d", restored.fonts.family, restored.terminalFontFace)
	}
}

func TestAFontThatIsNoLongerInstalledFallsBackToTheSystemOne(t *testing.T) {
	options := fontOptions(t, testFonts)
	first := newTestState(t, options)
	first.useTerminalFont(fontEntry{family: "Iosevka"})
	first.save()

	options.MonoFamilies = func() []string { return []string{"Menlo"} } // Iosevka was uninstalled
	second := newTestState(t, options)
	if second.fonts.family != "Iosevka" {
		t.Fatalf("restored family %q", second.fonts.family)
	}
	waitFor(t, second, func() bool { return second.fonts.family == "" })
	if second.terminalFontFace != gfx.MonoSystem || !strings.Contains(second.status, "Iosevka") {
		t.Fatalf("face %d, status %q", second.terminalFontFace, second.status)
	}
	if third := newTestState(t, options); third.fonts.family != "" {
		t.Fatalf("the fallback was not saved: %q", third.fonts.family)
	}

	// Still installed: nothing changes.
	options.MonoFamilies = func() []string { return testFonts }
	fourth := newTestState(t, fontOptionsFrom(options, "Fira Code"))
	waitFor(t, fourth, func() bool { return fourth.fonts.installed != nil })
	if fourth.fonts.family != "Fira Code" {
		t.Fatalf("a font that is installed was dropped: %q", fourth.fonts.family)
	}
}

func fontOptionsFrom(options Options, family string) Options {
	state := New(options)
	state.useTerminalFont(fontEntry{family: family})
	state.save()
	state.Close()
	return options
}

func TestFontPickerKeysMoveScrollAndLeaveTheFontAloneOnEscape(t *testing.T) {
	many := make([]string, 40)
	for index := range many {
		many[index] = "Family " + string(rune('A'+index/10)) + string(rune('0'+index%10))
	}
	state := newTestState(t, fontOptions(t, many))
	state.openFontPicker(false)
	waitFor(t, state, func() bool { return !state.fonts.loading })
	state.refreshFontPicker()
	if len(state.fonts.filtered) != 42 || state.fonts.selected != 0 {
		t.Fatalf("rows %d, selected %d", len(state.fonts.filtered), state.fonts.selected)
	}
	for range 12 {
		state.Key(gfx.KeyEvent{Key: gfx.KeyDown, Target: hitFontPickerSearch})
	}
	if state.fonts.selected != 12 || state.fonts.scroll != 12*fontPickerRowStep+30-fontPickerListHeight {
		t.Fatalf("selected %d scroll %g: the window must follow the selection", state.fonts.selected, state.fonts.scroll)
	}
	state.Key(gfx.KeyEvent{Key: gfx.KeyPageUp, Target: hitFontPickerSearch})
	state.Key(gfx.KeyEvent{Key: gfx.KeyPageUp, Target: hitFontPickerSearch})
	state.Key(gfx.KeyEvent{Key: gfx.KeyUp, Target: hitFontPickerSearch}) // already at the top
	if state.fonts.selected != 0 {
		t.Fatalf("selected %d", state.fonts.selected)
	}
	state.Scroll(gfx.ScrollEvent{DeltaY: 100})
	if state.fonts.scroll != 100 {
		t.Fatalf("wheel moved the list to %g", state.fonts.scroll)
	}
	before := state.terminalFontFace
	state.Key(gfx.KeyEvent{Key: gfx.KeyEscape, Target: hitFontPickerSearch})
	if state.modal != modalNone || state.terminalFontFace != before || state.fonts.family != "" {
		t.Fatal("Escape changed the font")
	}

	// Clicking a row selects it for preview without closing the modal; confirm applies it.
	state.openFontPicker(false)
	state.Click(hitFontPickerItemList.At(3)) // System, Classic, Family A0, Family A1
	if state.fonts.selected != 3 || state.modal != modalFontPicker || state.fonts.family != "" {
		t.Fatalf("selected %d modal %d family %q", state.fonts.selected, state.modal, state.fonts.family)
	}
	state.Click(hitFontPickerConfirm)
	if state.fonts.family != "Family A1" || state.modal != modalNone {
		t.Fatalf("family %q modal %d", state.fonts.family, state.modal)
	}
}

func TestTheBuiltInFacesAreChosenFromTheSameList(t *testing.T) {
	state := newTestState(t, fontOptions(t, testFonts))
	state.useTerminalFont(fontEntry{family: "Menlo"})
	state.openFontPicker(false)
	waitFor(t, state, func() bool { return !state.fonts.loading })
	state.Click(hitFontPickerItemList.At(1)) // Classic
	state.Click(hitFontPickerConfirm)
	if state.terminalFontFace != gfx.MonoClassic || state.fonts.family != "" {
		t.Fatalf("face %d family %q", state.terminalFontFace, state.fonts.family)
	}
	state.save()
	if saved, _, err := loadState(state.options.StatePath); err != nil || saved.TerminalFontFace != "classic" || saved.TerminalFontFamily != "" {
		t.Fatalf("saved %q %q %v", saved.TerminalFontFace, saved.TerminalFontFamily, err)
	}
}

func TestADamagedFamilyInTheStateFileIsIgnored(t *testing.T) {
	options := fontOptions(t, testFonts)
	if err := saveState(options.StatePath, savedState{TerminalFontFamily: "bad\nname", TerminalFontFace: "classic"}); err != nil {
		t.Fatal(err)
	}
	state := newTestState(t, options)
	if state.fonts.family != "" || state.terminalFontFace != gfx.MonoClassic {
		t.Fatalf("family %q face %d", state.fonts.family, state.terminalFontFace)
	}
}

func TestFallbackFontsAreAddedInOrderKeptAndRestored(t *testing.T) {
	options := fontOptions(t, testFonts)
	state := newTestState(t, options)
	state.useTerminalFont(fontEntry{family: "Menlo"})
	state.openFontPicker(true)
	waitFor(t, state, func() bool { return !state.fonts.loading })
	state.refreshFontPicker()
	for _, index := range state.fonts.filtered {
		if state.fonts.entries[index].family == "Menlo" || state.fonts.entries[index].family == "" {
			t.Fatalf("the picker for a fallback offers %+v", state.fonts.entries[index])
		}
	}
	state.Key(gfx.KeyEvent{Key: gfx.KeyEnter, Target: hitFontPickerSearch}) // Fira Code
	state.openFontPicker(true)
	typeInto(state, hitFontPickerSearch, "iosev")
	state.Key(gfx.KeyEvent{Key: gfx.KeyEnter, Target: hitFontPickerSearch})
	if got := state.fonts.fallbacks; len(got) != 2 || got[0] != "Fira Code" || got[1] != "Iosevka" {
		t.Fatalf("fallbacks = %q", got)
	}
	if got := gfx.MonoFallbacks(state.terminalFontFace); len(got) != 2 || gfx.MonoFamilyName(state.terminalFontFace) != "Menlo" {
		t.Fatalf("the terminal draws in %q behind %q", got, gfx.MonoFamilyName(state.terminalFontFace))
	}
	state.openFontPicker(true)
	state.refreshFontPicker()
	for _, index := range state.fonts.filtered {
		if name := state.fonts.entries[index].family; name == "Fira Code" || name == "Iosevka" {
			t.Fatalf("%s is offered again", name)
		}
	}
	state.closeModal()

	restored := newTestState(t, options)
	if got := restored.fonts.fallbacks; len(got) != 2 || got[0] != "Fira Code" || got[1] != "Iosevka" || len(gfx.MonoFallbacks(restored.terminalFontFace)) != 2 {
		t.Fatalf("restored fallbacks = %q", got)
	}
	if !restored.clearFallbackFonts() || len(gfx.MonoFallbacks(restored.terminalFontFace)) != 0 || restored.fonts.family != "Menlo" {
		t.Fatal("clearing the fallbacks must leave the font alone")
	}
}

func TestFallbackFontsAreLimitedAndNeverRepeatTheFont(t *testing.T) {
	state := newTestState(t, fontOptions(t, testFonts))
	state.useTerminalFont(fontEntry{family: "Menlo"})
	if state.addFallbackFont("Menlo") || state.addFallbackFont("") || state.addFallbackFont("a,b") {
		t.Fatal("an unusable family was added")
	}
	for _, name := range []string{"Fira Code", "Iosevka", "JetBrains Mono"} {
		if !state.addFallbackFont(name) {
			t.Fatalf("%s was not added", name)
		}
	}
	if state.addFallbackFont("Another") || len(state.fonts.fallbacks) != maxFallbackFonts {
		t.Fatalf("fallbacks = %q", state.fonts.fallbacks)
	}
	if actions := state.fallbackActions(); len(actions) != 1 || actions[0].ID != hitSettingsFallbackClear {
		t.Fatalf("a full list offers %d actions: it can only be cleared", len(actions))
	}
	// Choosing a fallback as the font takes it out from behind.
	state.useTerminalFont(fontEntry{family: "Iosevka"})
	if got := state.fonts.fallbacks; len(got) != 2 || slices.Contains(got, "Iosevka") {
		t.Fatalf("fallbacks = %q", got)
	}
}

func TestFallbackFontsNotInstalledHereAreSkippedButNotForgotten(t *testing.T) {
	options := fontOptions(t, testFonts)
	first := newTestState(t, options)
	first.useTerminalFont(fontEntry{family: "Menlo"})
	first.addFallbackFont("Fira Code")
	first.addFallbackFont("Songti Elsewhere") // chosen on another computer
	first.save()

	other := newTestState(t, options)
	waitFor(t, other, func() bool { return other.fonts.installed != nil })
	if got := gfx.MonoFallbacks(other.terminalFontFace); len(got) != 1 || got[0] != "Fira Code" {
		t.Fatalf("the face behind a missing family draws with %q", got)
	}
	if len(other.fonts.fallbacks) != 2 || other.snapshot().TerminalFontFallbacks[1] != "Songti Elsewhere" {
		t.Fatalf("the choice was changed for this computer: %q", other.fonts.fallbacks)
	}
}

func TestADamagedFallbackListInTheStateFileIsCleaned(t *testing.T) {
	options := fontOptions(t, testFonts)
	saved := savedState{TerminalFontFamily: "Menlo", TerminalFontFallbacks: []string{"Menlo", "a\nb", " Iosevka ", "Iosevka", "A", "B", "C", "D"}}
	if err := saveState(options.StatePath, saved); err != nil {
		t.Fatal(err)
	}
	state := newTestState(t, options)
	if got := state.fonts.fallbacks; len(got) != maxFallbackFonts || got[0] != "Iosevka" || got[1] != "A" || got[2] != "B" {
		t.Fatalf("fallbacks = %q", got)
	}
}

func TestFontPickerClickSelectsAndPreviewsWithoutClosing(t *testing.T) {
	state := newTestState(t, fontOptions(t, testFonts))
	state.useTerminalFont(fontEntry{family: "Menlo"})
	state.openFontPicker(false)
	waitFor(t, state, func() bool { return !state.fonts.loading })
	state.refreshFontPicker()

	// System, Classic, Fira Code, Iosevka, JetBrains Mono, Menlo
	state.Click(hitFontPickerItemList.At(3))
	if state.modal != modalFontPicker || state.fonts.selected != 3 || state.fonts.family != "Menlo" {
		t.Fatalf("modal %d selected %d family %q", state.modal, state.fonts.selected, state.fonts.family)
	}

	state.Click(hitFontPickerCancel)
	if state.modal != modalNone || state.fonts.family != "Menlo" {
		t.Fatalf("cancel left modal %d family %q", state.modal, state.fonts.family)
	}
}

func TestFontPickerDoubleClickConfirms(t *testing.T) {
	state := newTestState(t, fontOptions(t, testFonts))
	state.openFontPicker(false)
	waitFor(t, state, func() bool { return !state.fonts.loading })

	// The first press of a double click is an ordinary click: it only selects.
	state.Click(hitFontPickerItemList.At(2))
	if state.modal != modalFontPicker || state.fonts.selected != 2 {
		t.Fatalf("modal %d selected %d", state.modal, state.fonts.selected)
	}

	// The second one carries ClickCount 2, which the platform counts by the system's
	// double click setting; the picker reads it while painting.
	doubleClick(state, hitFontPickerItemList.At(2))
	if state.modal != modalNone || state.fonts.family != "Fira Code" {
		t.Fatalf("modal %d, family %q", state.modal, state.fonts.family)
	}
}

func TestFontPickerEnterOnAButtonIsThatButtonsOwn(t *testing.T) {
	state := newTestState(t, fontOptions(t, testFonts))
	state.openFontPicker(false)
	waitFor(t, state, func() bool { return !state.fonts.loading })
	state.Click(hitFontPickerItemList.At(2))

	for _, target := range []gfx.ID{hitFontPickerCancel, hitFontPickerClose} {
		result := state.Key(gfx.KeyEvent{Key: gfx.KeyEnter, Target: target})
		if result.Handled || state.modal != modalFontPicker || state.fonts.family != "" {
			t.Fatalf("Enter on %v: handled %v, modal %d, family %q", target, result.Handled, state.modal, state.fonts.family)
		}
	}

	// On the focused row it takes that row, whichever one is highlighted.
	state.Key(gfx.KeyEvent{Key: gfx.KeyEnter, Target: hitFontPickerItemList.At(3)})
	if state.modal != modalNone || state.fonts.family != "Iosevka" {
		t.Fatalf("modal %d, family %q", state.modal, state.fonts.family)
	}
}

func TestFontPickerCannotConfirmWhatTheButtonRefuses(t *testing.T) {
	state := newTestState(t, fontOptions(t, testFonts))
	state.openFontPicker(false)
	if !state.fonts.loading {
		t.Fatal("the list was read at once")
	}
	state.Key(gfx.KeyEvent{Key: gfx.KeyEnter, Target: hitFontPickerSearch})
	if state.modal != modalFontPicker {
		t.Fatal("Enter confirmed while the fonts were being read")
	}
	waitFor(t, state, func() bool { return !state.fonts.loading })

	typeInto(state, hitFontPickerSearch, "no such font")
	state.refreshFontPicker()
	state.Key(gfx.KeyEvent{Key: gfx.KeyEnter, Target: hitFontPickerSearch})
	if state.modal != modalFontPicker || state.fonts.family != "" {
		t.Fatalf("Enter confirmed an empty list: modal %d, family %q", state.modal, state.fonts.family)
	}
}

func TestTheTerminalFontRowsDescriptionsFollowTheLanguage(t *testing.T) {
	state := newTestState(t, fontOptions(t, testFonts))
	state.addFallbackFont("Iosevka")
	english, englishFallback := state.fontDescription(), state.fallbackDescription()
	state.setLanguage(i18n.Chinese)
	if state.fontDescription() == english || state.fallbackDescription() == englishFallback {
		t.Fatalf("the descriptions stayed in English: %q, %q", state.fontDescription(), state.fallbackDescription())
	}
}
