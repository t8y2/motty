package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/ui"
)

var testInterfaceFonts = []string{"Segoe UI", "Microsoft YaHei UI", "Sarasa Gothic"}

func interfaceFontOptions(t testing.TB, families []string) (Options, *[]string) {
	options := testOptions(t)
	options.FontFamilies = func() []string { return families }
	applied := &[]string{}
	options.SetInterfaceFont = func(family string) bool { *applied = append(*applied, family); return true }
	return options, applied
}

func TestTheInterfaceFontIsChosenKeptAndReloaded(t *testing.T) {
	options, applied := interfaceFontOptions(t, testInterfaceFonts)
	state := newTestState(t, options)
	state.Click(hitSettings)
	state.Click(hitInterfaceFontChoose)
	if state.modal != modalInterfaceFont || !state.uiFont.loading {
		t.Fatalf("modal %d, loading %v", state.modal, state.uiFont.loading)
	}
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	waitFor(t, state, func() bool { return !state.uiFont.loading })
	canvas = ui.Canvas{}
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	var labels []string
	for _, text := range canvas.Texts() {
		labels = append(labels, text.Value)
	}
	for _, want := range append([]string{state.messages.SettingsInterfaceFontDefault}, testInterfaceFonts...) {
		if !contains(labels, want) {
			t.Fatalf("the picker does not list %q: %v", want, labels)
		}
	}

	// Typing narrows the list; Enter takes the highlighted row and applies it.
	typeInto(state, hitInterfaceFontSearch, "yahei")
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	if len(state.uiFont.filtered) != 1 {
		t.Fatalf("filtered to %d rows: %v", len(state.uiFont.filtered), state.uiFont.filtered)
	}
	state.Key(ui.KeyEvent{Key: ui.KeyEnter, Target: hitInterfaceFontSearch})
	if state.modal != modalNone || state.uiFont.family != "Microsoft YaHei UI" {
		t.Fatalf("modal %d, family %q", state.modal, state.uiFont.family)
	}
	if len(*applied) == 0 || (*applied)[len(*applied)-1] != "Microsoft YaHei UI" {
		t.Fatalf("the engine was told %v", *applied)
	}
	if !strings.Contains(state.interfaceFontDescription(), "Microsoft YaHei UI") {
		t.Fatalf("description %q", state.interfaceFontDescription())
	}

	// The choice is the state file's, and a new window opens with it applied.
	state.save()
	saved, _, err := loadState(state.options.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.InterfaceFont != "Microsoft YaHei UI" {
		t.Fatalf("the saved family is %q", saved.InterfaceFont)
	}
	state.Close()
	second, _ := interfaceFontOptions(t, testInterfaceFonts)
	second.StatePath = state.options.StatePath
	reloaded := newTestState(t, second)
	if reloaded.uiFont.family != "Microsoft YaHei UI" {
		t.Fatalf("the family came back as %q", reloaded.uiFont.family)
	}

	// The default row takes the family away again.
	reloaded.Click(hitSettings)
	reloaded.Click(hitInterfaceFontChoose)
	waitFor(t, reloaded, func() bool { return !reloaded.uiFont.loading })
	reloaded.chooseInterfaceFont(0)
	if reloaded.uiFont.family != "" || !strings.Contains(reloaded.interfaceFontDescription(), reloaded.messages.SettingsInterfaceFontDefault) {
		t.Fatalf("the default row left the family %q", reloaded.uiFont.family)
	}
}

func TestAnInterfaceFontThatIsNoLongerInstalledFallsBackToTheDefault(t *testing.T) {
	options, _ := interfaceFontOptions(t, testInterfaceFonts)
	state := newTestState(t, options)
	if !state.setInterfaceFont("Gone Serif") {
		t.Fatal("the choice did not change anything")
	}
	state.validateInterfaceFont()
	waitFor(t, state, func() bool { return state.uiFont.installed != nil })
	if state.uiFont.family != "" {
		t.Fatalf("a family nothing has stays %q", state.uiFont.family)
	}
	if state.status == "" {
		t.Fatal("the fallback does not tell the user")
	}
	// The file keeps the applied default, and nothing loops once the list is in.
	before := state.uiFont.installed
	state.validateInterfaceFont()
	if state.uiFont.installed == nil || len(state.uiFont.installed) != len(before) {
		t.Fatal("reading the list again lost it")
	}
}

func TestTheInterfaceFontRowHidesWhereThePlatformCannotFollow(t *testing.T) {
	options := testOptions(t)
	options.FontFamilies = func() []string { return nil }
	options.SetInterfaceFont = func(string) bool { return false }
	state := newTestState(t, options)
	state.Click(hitSettings)
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, hitInterfaceFontChoose); ok {
		t.Fatal("a platform without a say in the family still offers the row")
	}
}
