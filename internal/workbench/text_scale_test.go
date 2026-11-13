package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

// The interface text size is the window's body text size in pixels — one choice
// the user makes for the whole window, with the descriptions and headings
// following the framework's proportions of it. The terminal's font size stays
// its own.

func TestInterfaceTextSizeStepsTheWholeWindow(t *testing.T) {
	defer applyTextMetrics(interfaceTextSizeDefault.factor()) // the package's tokens are shared with every other test
	state := newTestState(t, testOptions(t))
	if state.interfaceTextSize != interfaceTextSizeDefault || metrics.FontSizeS != gfx.DefaultMetrics().FontSizeS {
		t.Fatalf("the window opens at the default size: %v %v", state.interfaceTextSize, metrics.FontSizeS)
	}
	titleSize := barTitleSize
	state.Click(hitSettings)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	for _, id := range []gfx.ID{hitSettingsTextSizeDown, hitSettingsTextSizeReset, hitSettingsTextSizeUp} {
		if _, ok := hitByID(&canvas, id); !ok {
			t.Fatalf("the settings page shows no interface text size control %d", id)
		}
	}
	state.Click(hitSettingsTextSizeUp)
	if state.interfaceTextSize != interfaceTextSizeDefault+1 {
		t.Fatalf("the step did not stick: %v", state.interfaceTextSize)
	}
	if metrics.FontSizeM <= gfx.DefaultMetrics().FontSizeM || metrics.FontSizeL <= gfx.DefaultMetrics().FontSizeL {
		t.Fatalf("the larger size leaves the sizes at %v %v %v", metrics.FontSizeS, metrics.FontSizeM, metrics.FontSizeL)
	}
	// The theme copies the metrics by value: without a rebuild every framework
	// component — the settings rows, menus, dialogs — stays at the old size.
	if state.theme.Metrics.FontSizeM <= gfx.DefaultMetrics().FontSizeM {
		t.Fatalf("the theme did not follow the larger size: %v", state.theme.Metrics.FontSizeM)
	}
	if barTitleSize <= titleSize {
		t.Fatalf("the pane title stays at %v", barTitleSize)
	}
	for state.interfaceTextSize > interfaceTextSizeMin {
		state.Click(hitSettingsTextSizeDown)
	}
	state.Click(hitSettingsTextSizeDown) // past the smallest: it stops there
	if state.interfaceTextSize != interfaceTextSizeMin {
		t.Fatalf("the size went past its smallest: %v", state.interfaceTextSize)
	}
	if metrics.FontSizeM >= gfx.DefaultMetrics().FontSizeM {
		t.Fatalf("the smaller size leaves the sizes at %v %v %v", metrics.FontSizeS, metrics.FontSizeM, metrics.FontSizeL)
	}
	if state.theme.Metrics.FontSizeM >= gfx.DefaultMetrics().FontSizeM {
		t.Fatalf("the theme did not follow the smaller size: %v", state.theme.Metrics.FontSizeM)
	}
	state.Click(hitSettingsTextSizeReset)
	if state.interfaceTextSize != interfaceTextSizeDefault || metrics.FontSizeS != gfx.DefaultMetrics().FontSizeS || barTitleSize != titleSize {
		t.Fatalf("the default step does not restore %v %v %v", state.interfaceTextSize, metrics.FontSizeS, barTitleSize)
	}
}

// The assistant's panel is window text too: its body is the window's body size
// and its captions the window's caption size, so the interface text size reaches
// it like it reaches the settings pages.
func TestAITextFollowsTheInterfaceTextSize(t *testing.T) {
	applyTextMetrics(interfaceTextSizeDefault.factor())
	defer applyTextMetrics(interfaceTextSizeDefault.factor())
	state := newTestState(t, testOptions(t))
	before := aiSizes
	if before.body != gfx.DefaultMetrics().FontSizeM || before.small != gfx.DefaultMetrics().FontSizeS {
		t.Fatalf("the panel opens at body %v caption %v", before.body, before.small)
	}
	state.setInterfaceTextSize(interfaceTextSizeMin)
	if aiSizes.body != state.theme.Metrics.FontSizeM || aiSizes.small != state.theme.Metrics.FontSizeS {
		t.Fatalf("the panel is at body %v caption %v, the interface at %v %v",
			aiSizes.body, aiSizes.small, state.theme.Metrics.FontSizeM, state.theme.Metrics.FontSizeS)
	}
	if aiSizes.body >= before.body || aiSizes.bodyLine >= before.bodyLine || aiSizes.smallLine >= before.smallLine {
		t.Fatalf("the smaller interface leaves the panel at %+v, was %+v", aiSizes, before)
	}
	// A conversation row is its wrapped lines and the height they add up to, so a
	// row measured at the old size must be measured again, never reused as it is.
	view := aiRowView{valid: true, kind: aiAssistant, size: before.body, width: 100, cell: 7}
	item := &aiItem{kind: aiAssistant}
	if view.current(item, 100, 7, "") {
		t.Fatal("a row measured at the old text size survived the size change")
	}
	view.size = aiSizes.body
	if !view.current(item, 100, 7, "") {
		t.Fatal("a row measured at the current text size is not reused")
	}
	state.setInterfaceTextSize(interfaceTextSizeDefault)
	if aiSizes.body != before.body || aiSizes.body != gfx.DefaultMetrics().FontSizeM {
		t.Fatalf("the default size does not restore the panel: %v", aiSizes.body)
	}
}

func TestInterfaceTextSizeTravelsThroughTheStateFile(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	state.setInterfaceTextSize(12)
	state.save()
	saved, _, err := loadState(state.options.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.InterfaceTextSize != 12 {
		t.Fatalf("the saved size is %d", saved.InterfaceTextSize)
	}
	if saved.TextScale != "" {
		t.Fatalf("the state still writes the retired text scale %q", saved.TextScale)
	}
	defer applyTextMetrics(interfaceTextSizeDefault.factor())
	second := newTestState(t, options)
	if second.interfaceTextSize != 12 || metrics.FontSizeM >= gfx.DefaultMetrics().FontSizeM {
		t.Fatalf("the saved size came back as %v at size %v", second.interfaceTextSize, metrics.FontSizeM)
	}
	if second.theme.Metrics.FontSizeM >= gfx.DefaultMetrics().FontSizeM {
		t.Fatalf("the restored theme did not follow the saved size: %v", second.theme.Metrics.FontSizeM)
	}
	for _, legacy := range []struct {
		name string
		want interfaceTextSize
	}{{"small", 13}, {"large", 16}, {"", interfaceTextSizeDefault}} {
		saved.InterfaceTextSize = 0
		saved.TextScale = legacy.name
		saved.Version = stateFileVersion
		if saveState(state.options.StatePath, saved) != nil {
			t.Fatal("writing the state")
		}
		restored := newTestState(t, options)
		if restored.interfaceTextSize != legacy.want {
			t.Fatalf("the retired %q scale came back as %v", legacy.name, restored.interfaceTextSize)
		}
	}
	for _, damaged := range []int{0, interfaceTextSizeMin - 1, interfaceTextSizeMax + 1} {
		saved.InterfaceTextSize = damaged
		saved.TextScale = "huge"
		saved.Version = stateFileVersion
		if saveState(state.options.StatePath, saved) != nil {
			t.Fatal("writing the state")
		}
		restored := newTestState(t, options)
		if restored.interfaceTextSize != interfaceTextSizeDefault || metrics.FontSizeS != gfx.DefaultMetrics().FontSizeS {
			t.Fatalf("a damaged size %d comes back as %v at size %v", damaged, restored.interfaceTextSize, metrics.FontSizeS)
		}
	}
}
