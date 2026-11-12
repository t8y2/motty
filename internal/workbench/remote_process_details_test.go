package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"motty/internal/remotetools"
)

func selectRemoteToolsRow(state *State, index int) {
	if index >= 0 && index < len(state.remote.rows) {
		state.remote.selected = state.remote.rows[index].key
	}
	if state.remote.processDetails.open {
		state.closeRemoteProcessDetails()
	}
}

func selectRemoteToolsWindowRow(w *remoteToolsWindow, index int) {
	if index >= 0 && index < len(w.ui.remote.rows) {
		w.ui.remote.selected = w.ui.remote.rows[index].key
	}
	if w.ui.remote.processDetails.open {
		w.act(hitRemoteProcessDetailsDismiss)
	}
}

func TestProcessRowClickOpensDetailsAndEscapeReturnsToTheList(t *testing.T) {
	state, _, _ := openRemoteToolsDialog(t)
	process := state.remote.procs[0]
	clickRemoteToolsRow(t, state, 0)
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
	if _, found := hitByLabelRoleLast(&canvas, state.messages.Tools.RemoteClose, gfx.RoleButton); !found {
		t.Fatal("detail dialog has no reachable Close button")
	}
	for _, label := range []string{
		state.messages.Tools.RemoteTerminate, state.messages.Tools.RemoteForceKill,
		state.messages.Tools.RemoteReniceLower, state.messages.Tools.RemoteReniceNormal,
		state.messages.Tools.RemoteReniceHigher,
	} {
		if _, found := hitByLabelRoleLast(&canvas, label, gfx.RoleButton); !found {
			t.Errorf("detail dialog lacks process action %q", label)
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
	clickRemoteToolsRow(t, state, 0)
	canvas := remoteToolsPaint(t, state)
	closeHit, ok := hitByLabelRoleLast(canvas, state.messages.Tools.RemoteClose, gfx.RoleButton)
	if !ok {
		t.Fatal("detail dialog has no Close button")
	}
	// Tab is the page's: the dialog leaves it unhandled and the runtime traverses
	// the controls inside its focus trap.
	if result := windowKeyRoutes(state, gfx.KeyEvent{Key: gfx.KeyTab, Target: closeHit.ID}); result.Handled || result.ContinueTraversal {
		t.Fatalf("Tab stayed with the dialog instead of the page's focus traversal: %+v", result)
	}

	clickRemoteToolsButton(t, state, state.messages.Tools.RemoteReniceLower)
	waitFor(t, state, func() bool { return server.ran("renice 10 -p 2211") == 1 && !state.remote.loading })
	if state.remote.processDetails.open {
		t.Fatal("keyboard action did not return to the process list")
	}

	clickRemoteToolsRow(t, state, 0)
	canvas = remoteToolsPaint(t, state)
	closeHit, ok = hitByLabelRoleLast(canvas, state.messages.Tools.RemoteClose, gfx.RoleButton)
	if !ok {
		t.Fatal("detail dialog has no Close button")
	}
	clickRemoteToolsPageHit(t, state, closeHit)
	if state.remote.processDetails.open {
		t.Fatal("Close left the details dialog open")
	}
}

func TestProcessDetailsOwnsClicksAimedAtTheObscuredToolsPage(t *testing.T) {
	state, _, _ := openRemoteToolsDialog(t)
	opened := state.remote.procs[0]
	clickRemoteToolsRow(t, state, 0)
	if !state.remote.processDetails.open {
		t.Fatal("process details did not open")
	}

	canvas := remoteToolsPaint(t, state)
	refresh, ok := hitByLabelRoleLast(canvas, state.messages.Tools.RemoteRefresh, gfx.RoleButton)
	if !ok {
		t.Fatal("the tools page has no refresh button")
	}
	driver, err := app.NewDriver(state.WindowConfig(app.Config{
		Title: "remote tools details", Width: 1400, Height: 900, Tree: Tree(),
		Paint: state.Paint, OnKey: state.Key, OnPointer: state.Pointer,
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	if err := driver.Click(refresh.Bounds.X+refresh.Bounds.Width/2, refresh.Bounds.Y+refresh.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if !state.remote.processDetails.open ||
		state.remote.processDetails.process.PID != opened.PID ||
		state.remote.selected != remotetools.ProcessKey(opened) ||
		state.modal != modalRemoteTools {
		t.Fatalf("detail lost modality: details=%+v selected=%q modal=%d",
			state.remote.processDetails, state.remote.selected, state.modal)
	}

	state.closeRemoteProcessDetails()
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
	rightClickRemoteToolsRow(t, state, 0)
	if state.remote.selected != remotetools.ProcessKey(process) || state.remote.processDetails.open {
		t.Fatalf("right click opened the wrong surface: selected=%q details=%+v", state.remote.selected, state.remote.processDetails)
	}

	canvas := remoteToolsPaint(t, state)
	m := state.messages.Tools
	for _, label := range []string{
		m.RemoteProcessDetails, m.RemoteProcessCopyPID, m.RemoteProcessCopyUser,
		m.RemoteProcessCopyCommand, m.RemoteTerminate, m.RemoteForceKill,
		m.RemoteReniceLower, m.RemoteReniceNormal, m.RemoteReniceHigher,
	} {
		if _, found := hitByLabelAny(canvas, label); !found {
			t.Errorf("menu item %q is not painted", label)
		}
	}
	clickRemoteToolsMenuItem(t, state, m.RemoteProcessCopyPID)
	if copied != "2211" {
		t.Fatalf("copied=%q", copied)
	}

	rightClickRemoteToolsRow(t, state, 0)
	clickRemoteToolsMenuItem(t, state, m.RemoteProcessDetails)
	if !state.remote.processDetails.open || state.remote.processDetails.process.PID != process.PID {
		t.Fatal("View details did not open the selected process")
	}
	state.closeRemoteProcessDetails()

	rightClickRemoteToolsRow(t, state, 0)
	clickRemoteToolsMenuItem(t, state, m.RemoteForceKill)
	if state.modal != modalForm || !strings.Contains(state.form.description, "2211") {
		t.Fatalf("Kill opened modal %d", state.modal)
	}
}

func TestProcessDetailsRunProcessActions(t *testing.T) {
	state, server, _ := openRemoteToolsDialog(t)
	process := state.remote.procs[0]
	state.remote.selected = remotetools.ProcessKey(process)
	state.openRemoteProcessDetails(process)

	clickRemoteToolsButton(t, state, state.messages.Tools.RemoteReniceLower)
	waitFor(t, state, func() bool {
		return server.ran("renice 10 -p 2211") == 1 && !state.remote.loading
	})
	if state.remote.processDetails.open {
		t.Fatal("running an action should return to the process list")
	}
}

func TestProcessMenuIsThePageControlMenuInTheNativeToolsWindow(t *testing.T) {
	withRemoteWindowApplication(t, func(application *app.Application, host *remoteWindowHost) {
		state, _, fake, _ := liveSession(t)
		server := &remoteServer{}
		fake.mu.Lock()
		fake.runCommand = server.run
		fake.mu.Unlock()
		state.options.OpenContentWindow = application.OpenContentWindow
		state.Click(hitRemoteToolsOpen)
		w := state.remoteWindows[0]
		host.until(t, func() bool { return !w.ui.remote.loading })

		size := gfx.Size{Width: 1100, Height: 780}
		var canvas gfx.Canvas
		w.paint(&canvas, size)
		table, ok := hitByLabelRole(&canvas, w.ui.messages.Tools.RemoteTabProcesses, gfx.RoleGroup)
		if !ok {
			t.Fatal("the process table is not a page control")
		}
		bounds := w.ui.remote.tableBodyBounds
		event := gfx.PointerEvent{
			Kind: gfx.PointerDown, Button: gfx.PointerButtonSecondary,
			X: bounds.X + bounds.Width/2, Y: bounds.Y + w.ui.remote.tableRowHeight/2,
			Target: table.ID, Viewport: size,
		}
		if result := w.config().OnPointer(event); !result.Repaint {
			t.Fatal("the table control did not take the right click")
		}
		canvas.BeginPointerPass(event)
		w.paint(&canvas, size)
		canvas.EndPointerPass()
		if w.ui.remote.selected == "" {
			t.Fatal("right click did not select the process row")
		}
		item, ok := hitByLabelAny(&canvas, w.ui.messages.Tools.RemoteProcessDetails)
		if !ok {
			t.Fatal("the page context menu is not painted")
		}
		canvas.BeginEventPass(item.ID, gfx.ClickInfo{})
		w.paint(&canvas, size)
		canvas.EndEventPass()
		canvas.RunAfterPaint()
		if !w.ui.remote.processDetails.open {
			t.Fatal("the context menu's View details did not open the dialog")
		}
	})
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
