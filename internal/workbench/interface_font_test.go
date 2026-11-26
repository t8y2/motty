package workbench

import (
	"slices"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"motty/internal/i18n"
)

var testInterfaceFonts = []string{"Segoe UI", "Microsoft YaHei UI", "Sarasa Gothic"}

// interfaceFontChoice is one call the interface font setter received: the family
// and the two weights it was told.
type interfaceFontChoice struct {
	family     string
	main, bold int
}

// interfaceFontOptions is a window over a platform with the given families, and
// the choices the engine was told, in order.
func interfaceFontOptions(t testing.TB, families []string) (Options, *[]interfaceFontChoice) {
	options := testOptions(t)
	options.FontFamilies = func() []string { return families }
	options.FontWeights = func(string) []int { return nil }
	told := &[]interfaceFontChoice{}
	options.SetInterfaceFont = func(family string, main, bold int) bool {
		*told = append(*told, interfaceFontChoice{family: family, main: main, bold: bold})
		return true
	}
	return options, told
}

func lastTold(told *[]interfaceFontChoice) string {
	if len(*told) == 0 {
		return "<never>"
	}
	return (*told)[len(*told)-1].family
}

// openInterfaceFontPickerFor opens the picker from the settings page and waits
// for the list.
func openInterfaceFontPickerFor(t testing.TB, state *State) {
	t.Helper()
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsAppearance)
	state.Click(hitInterfaceFontChoose)
	waitForPicker(t, state, func() bool { return !state.uiFont.loading })
	paintedTexts(state)
}

// A family's weights are the picker's two choices — the weight the main text is
// drawn at and the weight the bold text is — and Confirm applies them with the
// family, the row names them, and the state file keeps them.
func TestTheInterfaceFontWeightsAreChosenAppliedAndKept(t *testing.T) {
	options, told := interfaceFontOptions(t, testInterfaceFonts)
	options.FontWeights = func(family string) []int {
		if family == "Microsoft YaHei UI" {
			return []int{300, 400, 700}
		}
		return nil
	}
	state := newTestState(t, options)
	openInterfaceFontPickerFor(t, state)
	clickFontPickerRow(t, state, 2) // Microsoft YaHei UI
	waitForPicker(t, state, func() bool { return len(state.fonts.weights["Microsoft YaHei UI"]) > 0 })
	_, labels := paintedTexts(state)
	for _, want := range []string{state.messages.Tools.FontWeightMain, state.messages.Tools.FontWeightBold} {
		if !contains(labels, want) {
			t.Fatalf("the picker does not offer %q: %v", want, labels)
		}
	}
	if got := state.familyWeights("Microsoft YaHei UI"); !slices.Equal(got, []int{300, 400, 700}) {
		t.Fatalf("the family's weights are %v", got)
	}

	// Light for the main text, Bold for the bold text: the drop-downs show the
	// weight chosen from them.
	state.fonts.weightMain, state.fonts.weightBold = 300, 700
	if _, texts := paintedTexts(state); !contains(texts, "Light") || !contains(texts, "Bold") {
		t.Fatalf("the weight drop-downs do not show their choice: %v", texts)
	}
	state.Click(hitFontPickerConfirm)
	last := (*told)[len(*told)-1]
	if state.uiFont.family != "Microsoft YaHei UI" || state.uiFont.main != 300 || state.uiFont.bold != 700 {
		t.Fatalf("the choice is %q %d/%d", state.uiFont.family, state.uiFont.main, state.uiFont.bold)
	}
	if last != (interfaceFontChoice{"Microsoft YaHei UI", 300, 700}) {
		t.Fatalf("the engine was told %+v", last)
	}
	if _, texts := paintedTexts(state); !contains(texts, "Microsoft YaHei UI · Light") {
		t.Fatalf("the row does not name the weights in use: %v", texts)
	}

	// The weights are the state file's, like the family.
	state.save()
	saved, _, err := loadState(state.options.StatePath)
	if err != nil || saved.InterfaceFontMain != 300 || saved.InterfaceFontBold != 700 {
		t.Fatalf("the saved weights are %d/%d, %v", saved.InterfaceFontMain, saved.InterfaceFontBold, err)
	}
	state.Close()
	second, secondTold := interfaceFontOptions(t, testInterfaceFonts)
	second.StatePath = state.options.StatePath
	reloaded := newTestState(t, second)
	if reloaded.uiFont.main != 300 || reloaded.uiFont.bold != 700 {
		t.Fatalf("the weights came back as %d/%d", reloaded.uiFont.main, reloaded.uiFont.bold)
	}
	if last := (*secondTold)[len(*secondTold)-1]; last != (interfaceFontChoice{"Microsoft YaHei UI", 300, 700}) {
		t.Fatalf("the engine was told %+v on reload", last)
	}
}

// A weight the family is not installed with is drawn at the nearest one it has:
// opening the picker on another family clamps the choice instead of asking for a
// face that is not there.
func TestAWeightTheFamilyDoesNotHaveSnapsToItsNearest(t *testing.T) {
	if got := nearestWeight(700, []int{300}); got != 300 {
		t.Fatalf("700 in a family of 300 came out %d", got)
	}
	if got := nearestWeight(0, []int{300, 400}); got != 0 {
		t.Fatalf("the platform's own weight was changed to %d", got)
	}
	if got := nearestWeight(550, []int{300, 400, 700}); got != 400 && got != 700 {
		t.Fatalf("550 came out %d, not the nearer of 400 and 700", got)
	}
}

// A family with no weights listed shows no drop-downs, so Confirm gives it the
// platform's own weights rather than the pair chosen for the family highlighted
// before it.
func TestAFamilyWithNoWeightsIsNotGivenAnotherFamilysPair(t *testing.T) {
	options, told := interfaceFontOptions(t, testInterfaceFonts)
	options.FontWeights = func(family string) []int {
		if family == "Microsoft YaHei UI" {
			return []int{300, 400, 700}
		}
		return nil
	}
	state := newTestState(t, options)
	openInterfaceFontPickerFor(t, state)
	clickFontPickerRow(t, state, 2) // Microsoft YaHei UI
	waitForPicker(t, state, func() bool { return len(state.fonts.weights["Microsoft YaHei UI"]) > 0 })
	state.fonts.weightMain, state.fonts.weightBold = 300, 700
	typeInto(state, hitFontPickerSearch, "segoe") // the platform lists no weights for it
	paintedTexts(state)
	clickFontPickerRow(t, state, 0)
	state.Click(hitFontPickerConfirm)
	if last := (*told)[len(*told)-1]; last != (interfaceFontChoice{"Segoe UI", 0, 0}) {
		t.Fatalf("the engine was told %+v for a family with no weights", last)
	}
	if state.uiFont.main != 0 || state.uiFont.bold != 0 {
		t.Fatalf("the family kept the weights %d/%d", state.uiFont.main, state.uiFont.bold)
	}
	state.Close()
}

// A platform that cannot choose a family's weights ignores the ones it is told, so
// the row names the family alone rather than weights it does not draw the text at.
func TestAnInterfaceFontWeightsThePlatformIgnoresAreNotNamed(t *testing.T) {
	options, _ := interfaceFontOptions(t, testInterfaceFonts)
	state := newTestState(t, options)
	state.setInterfaceFont("Microsoft YaHei UI", 300, 700)
	if got := state.interfaceFontInEffect(); got != "Microsoft YaHei UI" {
		t.Fatalf("the row names %q for weights the platform does not draw", got)
	}
	state.Close()
}

func TestTheInterfaceFontIsPreviewedConfirmedKeptAndRestored(t *testing.T) {
	options, told := interfaceFontOptions(t, testInterfaceFonts)
	state := newTestState(t, options)
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsAppearance)
	state.Click(hitInterfaceFontChoose)
	if state.modal != modalFontPicker || state.fonts.kind != pickInterfaceFont || !state.uiFont.loading {
		t.Fatalf("modal %d, kind %d, loading %v", state.modal, state.fonts.kind, state.uiFont.loading)
	}
	waitForPicker(t, state, func() bool { return !state.uiFont.loading })
	_, labels := paintedTexts(state)
	for _, want := range append([]string{state.messages.SettingsInterfaceFontDefault}, testInterfaceFonts...) {
		if !contains(labels, want) {
			t.Fatalf("the picker does not list %q: %v", want, labels)
		}
	}
	if !contains(labels, interfaceFontSample) {
		t.Fatalf("the picker shows no sample: %v", labels)
	}

	// A click only highlights; Cancel leaves the font as it was.
	told2 := len(*told)
	clickFontPickerRow(t, state, 2)
	if state.modal != modalFontPicker || state.uiFont.family != "" || len(*told) != told2 {
		t.Fatalf("a click applied the font: modal %d, family %q, told %v", state.modal, state.uiFont.family, *told)
	}
	state.Click(hitFontPickerCancel)
	if state.modal != modalNone || state.uiFont.family != "" || len(*told) != told2 {
		t.Fatalf("Cancel applied the font: modal %d, family %q", state.modal, state.uiFont.family)
	}

	// Typing narrows the list; Enter in the search takes the highlighted row.
	state.Click(hitInterfaceFontChoose)
	waitForPicker(t, state, func() bool { return !state.uiFont.loading })
	typeInto(state, hitFontPickerSearch, "yahei")
	paintedTexts(state)
	if len(state.fonts.filtered) != 1 {
		t.Fatalf("filtered to %d rows", len(state.fonts.filtered))
	}
	state.Key(gfx.KeyEvent{Key: gfx.KeyEnter, Target: hitFontPickerSearch})
	if state.modal != modalNone || state.uiFont.family != "Microsoft YaHei UI" || lastTold(told) != "Microsoft YaHei UI" {
		t.Fatalf("modal %d, family %q, told %v", state.modal, state.uiFont.family, *told)
	}
	// The row names the family in effect beside the button, so it is read there
	// rather than in an explanation.
	if _, texts := paintedTexts(state); !contains(texts, "Microsoft YaHei UI") {
		t.Fatalf("the row does not name the font in use: %v", texts)
	}

	// The choice is the state file's, and a new window opens with it in effect.
	state.save()
	saved, _, err := loadState(state.options.StatePath)
	if err != nil || saved.InterfaceFont != "Microsoft YaHei UI" {
		t.Fatalf("the saved family is %q, %v", saved.InterfaceFont, err)
	}
	state.Close()
	second, secondTold := interfaceFontOptions(t, testInterfaceFonts)
	second.StatePath = state.options.StatePath
	reloaded := newTestState(t, second)
	if reloaded.uiFont.family != "Microsoft YaHei UI" || lastTold(secondTold) != "Microsoft YaHei UI" {
		t.Fatalf("the family came back as %q, the engine was told %v", reloaded.uiFont.family, *secondTold)
	}

	// The Default row takes the family away again.
	openInterfaceFontPickerFor(t, reloaded)
	clickFontPickerRow(t, reloaded, 0)
	reloaded.Click(hitFontPickerConfirm)
	if reloaded.uiFont.family != "" || lastTold(secondTold) != "" {
		t.Fatalf("the Default row left the family %q (told %v)", reloaded.uiFont.family, *secondTold)
	}
	if _, texts := paintedTexts(reloaded); !contains(texts, reloaded.messages.SettingsInterfaceFontDefault) {
		t.Fatalf("the row does not name the default font: %v", texts)
	}
}

func TestTheInterfaceFontPickerGuardsItsButtonsLikeTheTerminalFontOne(t *testing.T) {
	options, _ := interfaceFontOptions(t, testInterfaceFonts)
	state := newTestState(t, options)
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsAppearance)
	state.Click(hitInterfaceFontChoose)
	if !state.uiFont.loading {
		t.Fatal("the list was read at once")
	}
	// Nothing can be applied while the families are being read.
	state.Key(gfx.KeyEvent{Key: gfx.KeyEnter, Target: hitFontPickerSearch})
	if state.modal != modalFontPicker || state.uiFont.family != "" {
		t.Fatalf("Enter confirmed while the list was being read: modal %d, family %q", state.modal, state.uiFont.family)
	}
	waitForPicker(t, state, func() bool { return !state.uiFont.loading })
	paintedTexts(state)

	// Enter on a button is that button's own.
	clickFontPickerRow(t, state, 1)
	for _, target := range []gfx.ID{hitFontPickerCancel, hitFontPickerClose} {
		result := state.Key(gfx.KeyEvent{Key: gfx.KeyEnter, Target: target})
		if result.Handled || state.modal != modalFontPicker || state.uiFont.family != "" {
			t.Fatalf("Enter on %v: handled %v, modal %d, family %q", target, result.Handled, state.modal, state.uiFont.family)
		}
	}

	// A double click takes the row it is on.
	doubleClick(state, hitFontPickerItemList.At(2))
	if state.modal != modalNone || state.uiFont.family != "Microsoft YaHei UI" {
		t.Fatalf("modal %d, family %q", state.modal, state.uiFont.family)
	}
}

func TestAnInterfaceFontThatIsNotInstalledIsKeptButNotUsed(t *testing.T) {
	options, _ := interfaceFontOptions(t, testInterfaceFonts)
	first := newTestState(t, options)
	first.setInterfaceFont("Sarasa Gothic", 0, 0)
	first.save()
	first.Close()

	// The same state on a computer that does not have the family.
	options, told := interfaceFontOptions(t, []string{"Segoe UI"})
	options.StatePath = first.options.StatePath
	second := newTestState(t, options)
	if second.uiFont.family != "Sarasa Gothic" || lastTold(told) != "Sarasa Gothic" {
		t.Fatalf("the saved choice came back as %q, told %v", second.uiFont.family, *told)
	}
	waitFor(t, second, func() bool { return second.uiFont.missing })
	if second.uiFont.family != "Sarasa Gothic" || lastTold(told) != "" {
		t.Fatalf("the choice is %q, the engine was last told %q", second.uiFont.family, lastTold(told))
	}
	second.Click(hitSettings)
	second.selectSettingsCategory(settingsAppearance)
	_, texts := paintedTexts(second)
	if !contains(texts, "Sarasa Gothic") || !contains(texts, "no longer installed") {
		t.Fatalf("the settings row does not say the family is not used: %v", texts)
	}
	second.save()
	saved, _, err := loadState(second.options.StatePath)
	if err != nil || saved.InterfaceFont != "Sarasa Gothic" {
		t.Fatalf("the choice was not kept: %q, %v", saved.InterfaceFont, err)
	}
	second.Close()

	// Back where the family is installed, it is in use again: the row names it.
	options, told = interfaceFontOptions(t, testInterfaceFonts)
	options.StatePath = first.options.StatePath
	third := newTestState(t, options)
	waitFor(t, third, func() bool { return third.uiFont.installed != nil })
	if third.uiFont.missing || lastTold(told) != "Sarasa Gothic" {
		t.Fatalf("missing %v, told %v", third.uiFont.missing, *told)
	}
	third.Click(hitSettings)
	third.selectSettingsCategory(settingsAppearance)
	if _, texts := paintedTexts(third); !contains(texts, "Sarasa Gothic") {
		t.Fatalf("the row does not name the font in use: %v", texts)
	}
}

func TestAnInterfaceFontIsNotDoubtedWhenThePlatformCannotListFonts(t *testing.T) {
	options, told := interfaceFontOptions(t, []string{})
	options.StatePath = options.KnownHostsPath[:strings.LastIndex(options.KnownHostsPath, "known_hosts")] + "state.json"
	first := newTestState(t, options)
	first.setInterfaceFont("Sarasa Gothic", 0, 0)
	first.save()
	first.Close()

	second := newTestState(t, options)
	waitFor(t, second, func() bool { return second.uiFont.installed != nil })
	if second.uiFont.missing || lastTold(told) != "Sarasa Gothic" {
		t.Fatalf("an empty list dropped the family: missing %v, told %v", second.uiFont.missing, *told)
	}
}

func TestWhereThePlatformCannotFollowAFamilyTheRowIsHiddenOrSaysSo(t *testing.T) {
	calls := 0
	options := testOptions(t)
	options.StatePath = options.KnownHostsPath[:strings.LastIndex(options.KnownHostsPath, "known_hosts")] + "state.json"
	options.SetInterfaceFont = func(string, int, int) bool { calls++; return false }
	state := newTestState(t, options)
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsAppearance)
	for range 3 {
		var canvas gfx.Canvas
		state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
		if _, ok := hitByID(&canvas, hitInterfaceFontChoose); ok {
			t.Fatal("a platform without a say in the family still offers the row")
		}
	}
	if calls != 1 {
		t.Fatalf("the platform was asked %d times; painting must not ask it", calls)
	}
	state.settings.search = *text.NewEditor("font")
	paintedTexts(state)
	for index := 0; index < state.settings.count; index++ {
		if state.settings.results[index].label == state.messages.SettingsInterfaceFont {
			t.Fatal("the search finds a row the page does not have")
		}
	}
	state.Close()

	// A choice made elsewhere is kept, and the row says it is not used here.
	if err := saveState(options.StatePath, savedState{InterfaceFont: "Sarasa Gothic"}); err != nil {
		t.Fatal(err)
	}
	carried := newTestState(t, options)
	carried.Click(hitSettings)
	carried.selectSettingsCategory(settingsAppearance)
	canvas, texts := paintedTexts(carried)
	hit, ok := hitByID(canvas, hitInterfaceFontChoose)
	if !ok || !hit.Disabled || !contains(texts, "cannot change the interface font") {
		t.Fatalf("row present %v, disabled %v, texts %v", ok, hit.Disabled, texts)
	}
	if carried.uiFont.family != "Sarasa Gothic" || carried.uiFont.applied != "" {
		t.Fatalf("choice %q, in effect %q", carried.uiFont.family, carried.uiFont.applied)
	}
}

func TestTheInterfaceFontRowIsSearchableAndFollowsTheLanguage(t *testing.T) {
	options, _ := interfaceFontOptions(t, testInterfaceFonts)
	state := newTestState(t, options)
	state.setInterfaceFont("Segoe UI", 0, 0)
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsAppearance)
	typeInto(state, hitSettingsSearch, "interface font")
	paintedTexts(state)
	found := false
	for _, result := range state.settings.results[:state.settings.count] {
		found = found || result.label == state.messages.SettingsInterfaceFont
	}
	if !found {
		t.Fatalf("results = %+v", state.settings.results[:state.settings.count])
	}

	// The value the row names is translated: with no family chosen it is the
	// default's own name, and that follows the language like the rest of the row.
	state.setInterfaceFont("", 0, 0)
	english := state.interfaceFontInEffect()
	state.setLanguage(i18n.Chinese)
	if chinese := state.interfaceFontInEffect(); chinese == english {
		t.Fatalf("the default's name is not translated: English %q, Chinese %q", english, chinese)
	}
}

// The font in effect is read off the row itself, so it has to be visible without
// pressing anything: beside the button, on the same line, and never over it.
func TestTheInterfaceFontRowNamesTheFontBesideTheButton(t *testing.T) {
	options, _ := interfaceFontOptions(t, testInterfaceFonts)
	state := newTestState(t, options)
	state.setInterfaceFont("Segoe UI", 0, 0)
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsAppearance)
	canvas, _ := paintedTexts(state)

	button, ok := hitByID(canvas, hitInterfaceFontChoose)
	if !ok {
		t.Fatal("the row offers no button to choose a font with")
	}
	var value gfx.Rect
	found := false
	for _, text := range canvas.Texts() {
		if text.Value == "Segoe UI" {
			value, found = text.Bounds, true
			break
		}
	}
	if !found {
		t.Fatalf("the row does not name the font in use")
	}
	if value.X+value.Width > button.Bounds.X {
		t.Fatalf("the name %v runs into the button %v", value, button.Bounds)
	}
	// Same row: the name's middle lies within the button's height, whatever the
	// leading of either is.
	if middle := value.Y + value.Height/2; middle < button.Bounds.Y || middle > button.Bounds.Y+button.Bounds.Height {
		t.Fatalf("the name %v is not on the button's line %v", value, button.Bounds)
	}
}
