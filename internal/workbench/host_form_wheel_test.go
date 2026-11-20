package workbench

import (
	"fmt"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

// A wheel no page area under the pointer can use must not come back to the
// window's own handler from the page: the page's fallback is that handler, so
// forwarding the event to the page again overflowed the stack (issue #1145).
func TestWheelTheNewHostFormCannotUseDoesNotReenterTheWindowHandler(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.WindowConfig(app.Config{OnScroll: state.Scroll})
	state.openHostDialog()
	paintedTexts(state)
	body := state.editor.form.viewport
	if body.Width <= 0 || body.Height <= 0 {
		t.Fatalf("the form body did not lay out: %+v", body)
	}
	// The body is at its top, so the page cannot scroll it up.
	result := state.scrollWindow(gfx.ScrollEvent{X: body.X + 4, Y: body.Y + 4, DeltaY: -120})
	if !result.Handled {
		t.Fatal("the new host dialog did not keep the wheel")
	}
	if state.editor.form.offset != 0 {
		t.Fatalf("the form body moved up from its top: %v", state.editor.form.offset)
	}
}

// The dialog owns the wheel over its backdrop: a wheel over the host list that
// lies behind the form must not move that list.
func TestWheelBehindTheNewHostFormKeepsTheHostListStill(t *testing.T) {
	state := newTestState(t, testOptions(t))
	group := state.hosts[0].Group
	for i := 0; i < 60; i++ {
		state.hosts = append(state.hosts, Host{ID: 1000 + uint32(i), Label: fmt.Sprintf("host-%d", i), Address: "192.0.2.1", Port: 22, Group: group})
	}
	state.syncVisible()
	state.WindowConfig(app.Config{OnScroll: state.Scroll})
	paintedTexts(state)
	if state.hostList.scroll == nil {
		t.Fatal("the list did not get a scroll position")
	}
	clickHostAction(state, hitAddHost)
	paintedTexts(state)
	before := state.hostList.scroll.Offset
	if result := state.scrollWindow(gfx.ScrollEvent{X: 250, Y: 500, DeltaY: 120}); !result.Handled {
		t.Fatal("the wheel behind the dialog was not handled")
	}
	if state.hostList.scroll.Offset != before {
		t.Fatalf("the host list scrolled behind the dialog: %v -> %v", before, state.hostList.scroll.Offset)
	}
}

// The same re-entry reached every dialog whose body lives in the page: a form
// dialog that cannot use the wheel gave it back to the window's handler.
func TestWheelAFormDialogCannotUseDoesNotReenterTheWindowHandler(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.WindowConfig(app.Config{OnScroll: state.Scroll})
	state.openForm(formDialog{
		title: "Rename", submit: "OK",
		submitLabel: func(*formDialog) string { return "OK" },
	}, formField{label: "Name", placeholder: "name"})
	paintedTexts(state)
	if result := state.scrollWindow(gfx.ScrollEvent{X: 5, Y: 5, DeltaY: -100}); !result.Handled {
		t.Fatal("the form dialog did not keep the wheel")
	}
}
