//go:build darwin || windows || linux

package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

func labelDriver(t *testing.T, state *State) *app.Driver {
	t.Helper()
	var canvas gfx.Canvas
	driver, err := app.NewDriver(app.Config{Title: "labels", Width: 1100, Height: 900, Tree: Tree(),
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut, OnResize: state.Resize}, &canvas)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(driver.Close)
	return driver
}

// labelPoint is the middle of the label hit of a control: where its words are.
func labelPoint(t *testing.T, driver *app.Driver, id gfx.ID) (x, y float32) {
	t.Helper()
	for _, hit := range driver.Snapshot().Hits {
		if hit.ID == id && hit.IsLabel {
			return hit.Bounds.X + hit.Bounds.Width/2, hit.Bounds.Y + hit.Bounds.Height/2
		}
	}
	t.Fatalf("%s has no label", tree.Text(id))
	return 0, 0
}

// Pressing the words of a field focuses its control; pressing the words of a
// checkbox or a switch flips it, as the label of an HTML control does.
func TestPressingTheWordsOfAControlFocusesOrFlipsIt(t *testing.T) {
	state := newTestState(t, testOptions(t))
	driver := labelDriver(t, state)

	state.Click(hitAddHost)
	driver.Resize(1100, 901)
	x, y := labelPoint(t, driver, hitDialogPort)
	if err := driver.Click(x, y); err != nil {
		t.Fatal(err)
	}
	if got := driver.Snapshot().Focused; got != hitDialogPort {
		t.Fatalf("after pressing the Port label focus is %s, want the port field", tree.Text(got))
	}
	state.Click(hitDialogAdvanced)
	// The switches are at the end of a long form: the window is tall enough to
	// show all of it.
	driver.Resize(1100, 1300)
	x, y = labelPoint(t, driver, hitDialogForwardX11)
	if err := driver.Click(x, y); err != nil {
		t.Fatal(err)
	}
	if !state.editor.forwardX11 {
		t.Fatal("pressing the words beside the Forward X11 checkbox did not check it")
	}
	state.Click(hitDialogCancel)

	state.Click(hitSettings)
	state.selectSettingsCategory(settingsTerminal)
	// This tests label activation, so keep the cursor row visible below the
	// learning and completion controls. Scrolling is covered separately.
	driver.Resize(1100, 1400)
	steady := state.terminalCursorSteady
	x, y = labelPoint(t, driver, hitSettingsCursorToggle)
	if err := driver.Click(x, y); err != nil {
		t.Fatal(err)
	}
	if state.terminalCursorSteady == steady {
		t.Fatal("pressing the words of the cursor setting did not flip its switch")
	}
}
