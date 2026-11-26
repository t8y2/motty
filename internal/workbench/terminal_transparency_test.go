package workbench

import (
	"math"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

func TestTerminalTransparencyAppearanceSlider(t *testing.T) {
	state, _, _, _ := liveSession(t)
	state.options.TerminalTransparency = true
	state.windowComposited.Store(true)
	driver := paneDriver(t, state, gfx.Size{Width: 1200, Height: 800})
	clickPaintedHit(t, driver, hitWindowSettings)
	clickPaintedHit(t, driver, hitSettingsCategoryList.At(int(settingsAppearance)))
	slider := hitOf(t, driver, hitTerminalTransparency).Bounds
	x, y := slider.X+slider.Width/2, slider.Y+slider.Height/2
	if err := driver.Press(x, y, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if err := driver.Move(x+slider.Width/4, y); err != nil {
		t.Fatal(err)
	}
	if err := driver.Release(x+slider.Width/4, y, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	got := int(state.terminalTransparency.Value)
	if got < 70 || got > 85 {
		t.Fatalf("dragged to three quarters, got %d%%", got)
	}
	if err := driver.Key(gfx.KeyRight, 0); err != nil {
		t.Fatal(err)
	}
	saved, _, err := loadState(state.options.StatePath)
	if err != nil || saved.TerminalTransparency != got+1 {
		t.Fatalf("after Right: state %v, saved %d, want %d, focus %v, slider %v, err %v", state.terminalTransparency.Value, saved.TerminalTransparency, got+1, driver.Snapshot().Focused, state.terminalTransparency.ID, err)
	}
	again := newTestState(t, state.options)
	if again.terminalTransparency.Value != float32(got+1) {
		t.Fatalf("restored transparency = %v", again.terminalTransparency.Value)
	}
}

func TestTerminalTransparencyPaint(t *testing.T) {
	state, _, _, shell := liveSession(t)
	state.options.TerminalTransparency = true
	state.windowComposited.Store(true)
	shell.say("\x1b[48;2;9;29;73mANSI\x1b[0m normal text\r\n")
	state.Wake()
	size := gfx.Size{Width: 1200, Height: 800}
	window := state.WindowConfig(app.Config{})
	clear := window.ClearColor
	if !window.Transparent || clear.Alpha != 0 {
		t.Fatalf("the window must composite over the desktop with a transparent clear: %+v", window)
	}
	for _, percent := range []int{0, 35, 100} {
		state.terminalTransparency.Value = float32(percent)
		var canvas gfx.Canvas
		state.Paint(&canvas, size)
		// Accumulate every fill covering these pixels; an opaque root anywhere
		// beneath the terminal would otherwise hide the desktop.
		alphaAt := func(x, y float32) float64 {
			alpha := clear.Alpha
			for _, r := range canvas.Rectangles() {
				if pointInRect(x, y, r.Bounds) && (!r.HasClip || pointInRect(x, y, r.ClipOf)) {
					a := float64(r.Color.Alpha)
					alpha = a + alpha*(1-a)
				}
			}
			return alpha
		}
		if got, want := alphaAt(600, 400), 1-float64(percent)/100; math.Abs(got-want) > .001 {
			t.Fatalf("at %d%% transparency terminal alpha = %v, want %v", percent, got, want)
		}
		if alphaAt(600, 2) != 1 {
			t.Fatalf("translucent title bar at %d%%", percent)
		}
		var explicitBG, text bool
		for _, r := range canvas.Rectangles() {
			explicitBG = explicitBG || r.Color == gfx.RGB(9, 29, 73)
		}
		for _, label := range canvas.Texts() {
			if strings.Contains(label.Value, "normal text") {
				text = true
				if label.Style.Color.Alpha != 1 {
					t.Fatalf("terminal text alpha = %v", label.Style.Color.Alpha)
				}
			}
		}
		if !explicitBG || !text {
			t.Fatalf("explicit background drawn = %t, terminal text drawn = %t", explicitBG, text)
		}
	}
	state.bg = backgroundState{set: true, pix: []byte{0, 0, 0, 0}, width: 1, height: 1}
	if state.showsBackground() {
		t.Fatal("an opaque picture would hide the desktop")
	}
	state.terminalTransparency.Value = 0
	if !state.showsBackground() {
		t.Fatal("returning to 0% did not restore the picture")
	}
	state.options.TerminalTransparency = false
	state.terminalTransparency.Value = 50
	if state.desktopTransparency() != 0 || !state.showsBackground() {
		t.Fatal("an unsupported host applied desktop transparency")
	}
}

// A window the platform could not composite shows no desktop, so the setting
// must not make the terminal translucent over a black surface.
func TestTerminalTransparencyNeedsACompositedWindow(t *testing.T) {
	state, _, _, _ := liveSession(t)
	state.options.TerminalTransparency = true
	state.terminalTransparency.Value = 50
	config := state.WindowConfig(app.Config{})
	if !config.Transparent || config.OnTransparentChange == nil {
		t.Fatalf("the window did not ask for transparency: %+v", config)
	}
	if state.desktopTransparency() != 0 {
		t.Fatal("transparency applied before the platform composited the window")
	}
	config.OnTransparentChange(true)
	if state.desktopTransparency() == 0 {
		t.Fatal("transparency not applied once the window is composited")
	}
	config.OnTransparentChange(false)
	if state.desktopTransparency() != 0 {
		t.Fatal("transparency stayed on after the platform reported no compositing")
	}
}

func TestTerminalTransparencyKeepsAppLockOpaque(t *testing.T) {
	state, _, _, _ := liveSession(t)
	state.options.TerminalTransparency = true
	state.windowComposited.Store(true)
	state.terminalTransparency.Value = 100
	if err := finishAppLock(t, state, appLockEnable, "", "fixture password"); err != nil {
		t.Fatal(err)
	}
	state.lockApplication()
	if !state.appLocked() {
		t.Fatal("expected the active terminal to be locked")
	}

	size := gfx.Size{Width: 1200, Height: 800}
	bounds := gfx.Rect{Width: size.Width, Height: size.Height}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	for _, fill := range canvas.Rectangles() {
		if fill.Bounds == bounds && fill.Color.Alpha == 1 && (!fill.HasClip || fill.ClipOf == bounds) {
			return
		}
	}
	t.Fatal("lock screen did not cover the transparent terminal with an opaque background")
}
