package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

// The interface text size is one choice the user makes for the whole window —
// the terminal's font size stays its own. It is a step each way, so nothing
// that was measured to sit beside the text needs to move.

func TestTextScaleStepsTheWholeWindow(t *testing.T) {
	defer applyTextMetrics(textScaleRegularFactor) // the package's tokens are shared with every other test
	state := newTestState(t, testOptions(t))
	if state.textScale != textScaleRegular || metrics.FontSizeS != gfx.DefaultMetrics().FontSizeS {
		t.Fatalf("the window opens at the regular size: %v %v", state.textScale, metrics.FontSizeS)
	}
	state.Click(hitSettings)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, hitSettingsTextRegular); !ok {
		t.Fatal("the settings page shows no interface text size choice")
	}
	state.Click(hitSettingsTextLarge)
	if state.textScale != textScaleLarge {
		t.Fatalf("the choice did not stick: %v", state.textScale)
	}
	if metrics.FontSizeM <= gfx.DefaultMetrics().FontSizeM || metrics.FontSizeL <= gfx.DefaultMetrics().FontSizeL {
		t.Fatalf("the large step leaves the sizes at %v %v %v", metrics.FontSizeS, metrics.FontSizeM, metrics.FontSizeL)
	}
	// The theme copies the metrics by value: without a rebuild every framework
	// component — the settings rows, menus, dialogs — stays at the old size.
	if state.theme.Metrics.FontSizeM <= gfx.DefaultMetrics().FontSizeM {
		t.Fatalf("the theme did not follow the large step: %v", state.theme.Metrics.FontSizeM)
	}
	if barTitleSize <= float32(13) {
		t.Fatalf("the pane title stays at %v", barTitleSize)
	}
	state.Click(hitSettingsTextSmall)
	if metrics.FontSizeM >= gfx.DefaultMetrics().FontSizeM {
		t.Fatalf("the small step leaves the sizes at %v %v %v", metrics.FontSizeS, metrics.FontSizeM, metrics.FontSizeL)
	}
	if state.theme.Metrics.FontSizeM >= gfx.DefaultMetrics().FontSizeM {
		t.Fatalf("the theme did not follow the small step: %v", state.theme.Metrics.FontSizeM)
	}
	state.Click(hitSettingsTextRegular)
	if metrics.FontSizeS != gfx.DefaultMetrics().FontSizeS || barTitleSize != 13 {
		t.Fatalf("the regular step does not restore %v %v", metrics.FontSizeS, barTitleSize)
	}
}

func TestTextScaleTravelsThroughTheStateFile(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	state.setTextScale(textScaleSmall)
	state.save()
	saved, _, err := loadState(state.options.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.TextScale != "small" {
		t.Fatalf("the saved scale is %q", saved.TextScale)
	}
	defer applyTextMetrics(textScaleRegularFactor)
	second := newTestState(t, options)
	if second.textScale != textScaleSmall || metrics.FontSizeM >= gfx.DefaultMetrics().FontSizeM {
		t.Fatalf("the saved scale came back as %v at size %v", second.textScale, metrics.FontSizeM)
	}
	if second.theme.Metrics.FontSizeM >= gfx.DefaultMetrics().FontSizeM {
		t.Fatalf("the restored theme did not follow the saved scale: %v", second.theme.Metrics.FontSizeM)
	}
	saved.TextScale = "huge"
	saved.Version = stateFileVersion
	if saveState(state.options.StatePath, saved) != nil {
		t.Fatal("writing the state")
	}
	defer func() { applyTextMetrics(textScaleRegularFactor) }()
	third := newTestState(t, options)
	if third.textScale != textScaleRegular || metrics.FontSizeS != gfx.DefaultMetrics().FontSizeS {
		t.Fatalf("an unknown scale comes back as %v at size %v", third.textScale, metrics.FontSizeS)
	}
}
