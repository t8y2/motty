//go:build darwin || windows || linux

package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
)

func openIdleKeepAliveForm(t *testing.T, protocol hostProtocol) (*State, *headless.Session) {
	t.Helper()
	state := newTestState(t, testOptions(t))
	state.openHostDialog()
	state.editor.setProtocol(protocol)
	view, err := headless.Open(app.Config{
		Width: 1100, Height: 1200, Tree: Tree(), Paint: state.Paint,
		OnKey: state.Key, OnPointer: state.Pointer, OnShortcut: state.Shortcut,
		OnScroll: state.Scroll, OnResize: state.Resize,
		TextInput: func(context app.TextInputContext) *app.TextInput {
			if editor := state.EditorFor(context.ID); editor != nil {
				return &app.TextInput{Editor: editor}
			}
			return nil
		},
	}, nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(view.Close)
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	return state, view
}

func clickFormHit(t *testing.T, view *headless.Session, id gfx.ID) {
	t.Helper()
	hit, ok := hitByIDSnapshot(view.Snapshot().Hits, id)
	if !ok {
		t.Fatalf("%s was not painted", tree.Text(id))
	}
	if err := view.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
}

// focusFormHit tabs to a control until the form has scrolled it into view.
func focusFormHit(t *testing.T, state *State, view *headless.Session, id gfx.ID) {
	t.Helper()
	for index := 0; index < state.editor.form.count+2; index++ {
		if view.Snapshot().Focused == id {
			if _, ok := hitByIDSnapshot(view.Snapshot().Hits, id); ok {
				return
			}
		}
		if err := view.Keys("Tab"); err != nil {
			t.Fatal(err)
		}
		if err := view.Rest(); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatalf("%s was not reachable with Tab", tree.Text(id))
}

// The Advanced section is where a terminal host says how often it is probed:
// SSH, Telnet and serial hosts have it; a remote desktop does not.
func TestIdleKeepAliveAdvancedSectionIsOnEveryTerminalHost(t *testing.T) {
	for _, protocol := range []hostProtocol{protocolSSH, protocolTelnet, protocolSerial} {
		name := map[hostProtocol]string{
			protocolSSH: "ssh", protocolTelnet: "telnet", protocolSerial: "serial",
		}[protocol]
		t.Run(name, func(t *testing.T) {
			state, view := openIdleKeepAliveForm(t, protocol)
			clickFormHit(t, view, hitDialogAdvanced)
			if !state.editor.advancedOpen {
				t.Fatal("the Advanced section did not open")
			}
			focusFormHit(t, state, view, hitDialogIdleKeepAlive)
			if _, ok := hitByIDSnapshot(view.Snapshot().Hits, hitDialogIdleKeepAlive); !ok {
				t.Fatal("the idle keepalive select was not painted")
			}
		})
	}
	for _, protocol := range []hostProtocol{protocolVNC, protocolRDP} {
		name := map[hostProtocol]string{protocolVNC: "vnc", protocolRDP: "rdp"}[protocol]
		t.Run(name, func(t *testing.T) {
			state, view := openIdleKeepAliveForm(t, protocol)
			if _, ok := hitByIDSnapshot(view.Snapshot().Hits, hitDialogAdvanced); ok {
				t.Fatal("a remote desktop host showed an Advanced section")
			}
			if state.editor.form.idleBody.interval.Valid() {
				t.Fatal("a remote desktop host laid out idle keepalive rows")
			}
		})
	}
}

// The interval list turns the probe on; the send list appears with it and the
// chosen value is what the form keeps.
func TestIdleKeepAliveFormChoosesIntervalAndInput(t *testing.T) {
	state, view := openIdleKeepAliveForm(t, protocolSSH)
	clickFormHit(t, view, hitDialogAdvanced)
	focusFormHit(t, state, view, hitDialogIdleKeepAlive)
	clickFormHit(t, view, hitDialogIdleKeepAlive)
	clickFormHit(t, view, hitDialogIdleKeepAliveItemList.At(2)) // 5 minutes
	if state.editor.idleKeepAlive != idleKeepAliveFiveMinutes {
		t.Fatalf("preset = %v, want five minutes", state.editor.idleKeepAlive)
	}
	focusFormHit(t, state, view, hitDialogIdleInput)
	clickFormHit(t, view, hitDialogIdleInput)
	clickFormHit(t, view, hitDialogIdleInputItemList.At(1)) // NUL, the SSH default
	if state.editor.idleInputChosen || state.editor.idleInput != idleInputNUL {
		t.Fatalf("input = %v chosen=%v, want the default left unpinned", state.editor.idleInput, state.editor.idleInputChosen)
	}
	canvas, texts := paintedTexts(state)
	if !contains(texts, state.messages.Tools.IdleKeepAliveFiveMinutes) {
		t.Fatalf("the five minute choice was not painted:\n%v", canvas.Texts())
	}
	if !contains(texts, state.messages.Tools.IdleKeepAliveInputNUL) {
		t.Fatalf("the NUL choice was not painted:\n%v", canvas.Texts())
	}
}

// Choosing a custom interval reveals its number and unit, and a custom send
// content reveals its field.
func TestIdleKeepAliveFormRevealsCustomRows(t *testing.T) {
	state, view := openIdleKeepAliveForm(t, protocolTelnet)
	clickFormHit(t, view, hitDialogAdvanced)
	focusFormHit(t, state, view, hitDialogIdleKeepAlive)
	clickFormHit(t, view, hitDialogIdleKeepAlive)
	clickFormHit(t, view, hitDialogIdleKeepAliveItemList.At(4)) // Custom…
	if state.editor.idleKeepAlive != idleKeepAliveCustom {
		t.Fatalf("preset = %v, want custom", state.editor.idleKeepAlive)
	}
	if got := state.editor.idleInterval.String(); got != "1" {
		t.Fatalf("the custom interval started at %q, want 1", got)
	}
	focusFormHit(t, state, view, hitDialogIdleInterval)
	if _, ok := hitByIDSnapshot(view.Snapshot().Hits, hitDialogIdleInterval); !ok {
		t.Fatal("the custom interval field was not painted")
	}
	focusFormHit(t, state, view, hitDialogIdleUnit)
	if _, ok := hitByIDSnapshot(view.Snapshot().Hits, hitDialogIdleUnit); !ok {
		t.Fatal("the custom interval unit was not painted")
	}
	focusFormHit(t, state, view, hitDialogIdleInput)
	clickFormHit(t, view, hitDialogIdleInput)
	clickFormHit(t, view, hitDialogIdleInputItemList.At(2)) // Custom…
	focusFormHit(t, state, view, hitDialogIdleCustomInput)
	if _, ok := hitByIDSnapshot(view.Snapshot().Hits, hitDialogIdleCustomInput); !ok {
		t.Fatal("the custom input field was not painted")
	}
}
