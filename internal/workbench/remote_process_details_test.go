package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/remotetools"
)

func selectRemoteToolsRow(state *State, index int) {
	if index >= 0 && index < len(state.remote.rows) {
		state.remote.selected = state.remote.rows[index].key
	}
	if state.remote.processDetails.open {
		state.Click(hitRemoteProcessDetailsDismiss)
	}
}

func selectRemoteToolsWindowRow(w *remoteToolsWindow, index int) {
	if index >= 0 && index < len(w.ui.remote.rows) {
		w.ui.remote.selected = w.ui.remote.rows[index].key
	}
	if w.ui.remote.processDetails.open {
		w.click(hitRemoteProcessDetailsDismiss)
	}
}

func TestProcessRowClickOpensDetailsAndEscapeReturnsToTheList(t *testing.T) {
	state, _, _ := openRemoteToolsDialog(t)
	process := state.remote.procs[0]
	state.Click(hitRemoteRowList.At(0))
	if !state.remote.processDetails.open || state.remote.processDetails.process.PID != process.PID ||
		state.modal != modalRemoteTools {
		t.Fatalf("details=%+v modal=%d", state.remote.processDetails, state.modal)
	}

	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	for _, want := range []string{
		state.messages.Tools.RemoteProcessDetailsTitle,
		state.messages.Tools.RemoteProcessCommand,
		process.Command,
	} {
		if !remoteCanvasHasText(&canvas, want) {
			t.Errorf("detail dialog lacks %q", want)
		}
	}
	closeHit, found := hitByID(&canvas, hitRemoteProcessDetailsClose)
	if !found || closeHit.Label == "" {
		t.Fatal("detail dialog has no reachable Close button")
	}
	for _, id := range []gfx.ID{
		hitRemoteTerminate, hitRemoteKill,
		hitRemoteNiceLower, hitRemoteNiceNormal, hitRemoteNiceHigher,
	} {
		if _, found := hitByID(&canvas, id); !found {
			t.Errorf("detail dialog lacks process action %v", id)
		}
	}
	if result := state.Key(gfx.KeyEvent{Key: gfx.KeyEscape}); !result.Handled {
		t.Fatal("Escape did not handle the detail dialog")
	}
	if state.remote.processDetails.open || state.remote.selected != remotetools.ProcessKey(process) || state.modal != modalRemoteTools {
		t.Fatalf("Escape lost the process selection or the tools page: %+v", state.remote)
	}
}

func TestProcessDetailsKeyboardSupportsTraversalAndActions(t *testing.T) {
	state, server, _ := openRemoteToolsDialog(t)
	state.Click(hitRemoteRowList.At(0))
	if result := state.Key(gfx.KeyEvent{Key: gfx.KeyTab, Target: hitRemoteProcessDetailsClose}); !result.Handled || !result.ContinueTraversal {
		t.Fatalf("Tab did not hand focus traversal back to the dialog: %+v", result)
	}

	result := state.Key(gfx.KeyEvent{Key: gfx.KeySpace, Target: hitRemoteNiceLower})
	if !result.Handled {
		t.Fatal("Space did not activate the focused process action")
	}
	waitFor(t, state, func() bool { return server.ran("renice 10 -p 2211") == 1 && !state.remote.loading })
	if state.remote.processDetails.open {
		t.Fatal("keyboard action did not return to the process list")
	}

	state.Click(hitRemoteRowList.At(0))
	if result := state.Key(gfx.KeyEvent{Key: gfx.KeyEnter, Target: hitRemoteProcessDetailsClose}); !result.Handled {
		t.Fatal("Enter did not activate Close")
	}
	if state.remote.processDetails.open {
		t.Fatal("keyboard Close left the details dialog open")
	}
}

func TestProcessDetailsOwnsClicksAimedAtTheObscuredToolsPage(t *testing.T) {
	state, _, _ := openRemoteToolsDialog(t)
	opened := state.remote.procs[0]
	state.Click(hitRemoteRowList.At(0))
	if !state.remote.processDetails.open {
		t.Fatal("process details did not open")
	}

	state.Click(hitRemoteRowList.At(1))
	state.Click(hitRemoteStart)
	state.Click(hitRemoteRefresh)
	if !state.remote.processDetails.open ||
		state.remote.processDetails.process.PID != opened.PID ||
		state.remote.selected != remotetools.ProcessKey(opened) ||
		state.modal != modalRemoteTools {
		t.Fatalf("detail lost modality: details=%+v selected=%q modal=%d",
			state.remote.processDetails, state.remote.selected, state.modal)
	}

	state.Click(hitRemoteProcessDetailsDismiss)
	if state.remote.processDetails.open || state.modal != modalRemoteTools {
		t.Fatal("dismiss did not return to the tools page")
	}
}

func TestProcessRightClickCopiesFieldsAndOpensTheSameActions(t *testing.T) {
	state, _, _ := openRemoteToolsDialog(t)
	process := state.remote.procs[0]
	copied := ""
	state.options.SetClipboardText = func(value string) error {
		copied = value
		return nil
	}
	session := openWorkbenchSession(t, state)
	row := gfx.Hit{}
	for _, hit := range session.Snapshot().Hits {
		if hit.ID == hitRemoteRowList.At(0) {
			row = hit
		}
	}
	if row.ID.IsZero() {
		t.Fatal("process row is not painted")
	}
	x, y := row.Bounds.X+row.Bounds.Width/2, row.Bounds.Y+row.Bounds.Height/2
	driver := session.Driver()
	if err := driver.Press(x, y, gfx.PointerButtonSecondary); err != nil {
		t.Fatal(err)
	}
	if err := driver.Release(x, y, gfx.PointerButtonSecondary); err != nil {
		t.Fatal(err)
	}
	if !state.remote.processMenu.open || state.remote.selected != remotetools.ProcessKey(process) || state.remote.processDetails.open {
		t.Fatalf("right click opened the wrong surface: menu=%+v details=%+v", state.remote.processMenu, state.remote.processDetails)
	}

	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	for _, id := range []gfx.ID{
		hitRemoteProcessMenuView, hitRemoteProcessMenuCopyPID, hitRemoteProcessMenuCopyUser,
		hitRemoteProcessMenuCopyCommand, hitRemoteProcessMenuTerminate, hitRemoteProcessMenuKill,
		hitRemoteProcessMenuNiceLower, hitRemoteProcessMenuNiceNormal, hitRemoteProcessMenuNiceHigher,
	} {
		if _, found := hitByID(&canvas, id); !found {
			t.Errorf("menu item %v is not painted", id)
		}
	}
	state.Click(hitRemoteProcessMenuCopyPID)
	if copied != "2211" || state.remote.processMenu.open {
		t.Fatalf("copied=%q menuOpen=%v", copied, state.remote.processMenu.open)
	}

	state.remote.processMenu = remoteProcessMenu{open: true, key: remotetools.ProcessKey(process), at: gfx.Point{X: x, Y: y}}
	state.Click(hitRemoteProcessMenuView)
	if !state.remote.processDetails.open || state.remote.processDetails.process.PID != process.PID {
		t.Fatal("View details did not open the selected process")
	}
	state.Click(hitRemoteProcessDetailsClose)

	state.remote.processMenu = remoteProcessMenu{open: true, key: remotetools.ProcessKey(process), at: gfx.Point{X: x, Y: y}}
	state.Click(hitRemoteProcessMenuKill)
	if state.modal != modalForm || state.remote.processMenu.open || !strings.Contains(state.form.description, "2211") {
		t.Fatalf("Kill opened modal %d from menu=%+v", state.modal, state.remote.processMenu)
	}
}

func TestProcessDetailsRunProcessActions(t *testing.T) {
	state, server, _ := openRemoteToolsDialog(t)
	process := state.remote.procs[0]
	state.remote.selected = remotetools.ProcessKey(process)
	state.openRemoteProcessDetails(process)

	state.Click(hitRemoteNiceLower)
	waitFor(t, state, func() bool {
		return server.ran("renice 10 -p 2211") == 1 && !state.remote.loading
	})
	if state.remote.processDetails.open {
		t.Fatal("running an action should return to the process list")
	}
}

func TestProcessMenuKeyboardNavigatesInTheNativeToolsWindow(t *testing.T) {
	state, _, _ := openRemoteToolsDialog(t)
	process := state.remote.procs[0]
	state.remote.processMenu = remoteProcessMenu{
		open: true, key: remotetools.ProcessKey(process), at: gfx.Point{X: 120, Y: 120},
	}
	var canvas gfx.Canvas
	state.paintRemoteProcessMenu(&canvas, gfx.Rect{Width: 900, Height: 700})

	w := &remoteToolsWindow{owner: state}
	w.ui.modal = state.modal
	w.ui.remote.processMenu = state.remote.processMenu
	w.ui.popups = state.popups
	result := w.key(gfx.KeyEvent{Target: hitRemoteProcessMenuView, Key: gfx.KeyDown})
	if !result.Handled || result.Focus == hitRemoteProcessMenuView || !result.FocusVisible {
		t.Fatalf("native process menu did not accept arrow-key focus: %+v", result)
	}
	result = w.key(gfx.KeyEvent{Key: gfx.KeyEscape})
	if !result.Handled || w.ui.remote.processMenu.open {
		t.Fatalf("native process menu did not close with Escape: %+v", result)
	}
}

func TestProcessDetailStateLabelsAndUnknownValues(t *testing.T) {
	state, _, _ := openRemoteToolsDialog(t)
	for _, test := range []struct {
		raw  string
		want string
	}{
		{"R", state.messages.Tools.RemoteProcessStateRunning},
		{"S<", state.messages.Tools.RemoteProcessStateSleeping},
		{"D", state.messages.Tools.RemoteProcessStateWaiting},
		{"Z", state.messages.Tools.RemoteProcessStateZombie},
		{"T", state.messages.Tools.RemoteProcessStateStopped},
		{"Ready", state.messages.Tools.RemoteProcessStateReady},
		{"Suspended", state.messages.Tools.RemoteProcessStateSuspended},
		{"Not Responding", state.messages.Tools.RemoteProcessStateNotResponding},
		{"unknown-state", "unknown-state"},
	} {
		if got := processStateText(state, test.raw); got != test.want {
			t.Errorf("state %q = %q, want %q", test.raw, got, test.want)
		}
	}
	if processText("") != "—" || processInteger(0) != "—" || processBytes(0) != "—" || processCollectedAt(state.remote.collectedAt) == "" {
		t.Fatal("missing process values are not represented consistently")
	}
}
