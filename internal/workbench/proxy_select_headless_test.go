//go:build darwin || linux || windows

package workbench

import (
	"fmt"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
)

// hostSelectTestState is the proxy dialog with more hosts than its popup shows,
// opened on the last one so the picker has to reveal its selection.
func hostSelectTestState(t *testing.T) *State {
	t.Helper()
	state := newTestState(t, testOptions(t))
	for i := 0; i < 9; i++ {
		state.hosts = append(state.hosts, Host{
			ID: state.nextHostID, Label: fmt.Sprintf("Host %02d", i), Address: fmt.Sprintf("host%02d.example.com", i),
			User: "me", Port: 22, Protocol: protocolSSH,
		})
		state.nextHostID++
	}
	state.selectedHostID = state.hosts[len(state.hosts)-1].ID
	state.Click(hitProxies)
	state.Click(hitProxyAdd)
	if state.modal != modalNewProxy {
		t.Fatalf("proxy dialog = %d", state.modal)
	}
	return state
}

// The host pickers are page comboboxes: the target field takes the keyboard when
// the dialog opens, and the wheel over the open list moves the list while a
// wheel beside it leaves it where it is.
func TestProxyHostSelectFocusesTheTargetAndScrolls(t *testing.T) {
	state := hostSelectTestState(t)
	view, err := headless.Open(state.WindowConfig(app.Config{
		Title: "proxy", Width: 1400, Height: 900, Paint: state.Paint, OnPointer: state.Pointer,
		OnKey: state.Key, OnScroll: state.Scroll,
	}), nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	if err := view.Wait(100 * time.Millisecond); err != nil {
		t.Fatal(err)
	}

	input := pageComboboxID(t, state, state.messages.ProxyFieldTarget)
	if focused := view.Snapshot().Focused; focused != input {
		t.Fatalf("focus on open = %v, want the target field %v", focused, input)
	}
	field, ok := findSnapshotHit(view.Snapshot(), input)
	if !ok {
		t.Fatalf("the target field %v is not painted", input)
	}
	if err := view.Click(field.Bounds.X+field.Bounds.Width/2, field.Bounds.Y+field.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := view.Wait(100 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	last := state.hosts[len(state.hosts)-1]
	hit, ok := findSnapshotHitByLabel(view.Snapshot(), gfx.RoleOption, last.Label)
	if !ok {
		t.Fatalf("the chosen host %q was not revealed: %v", last.Label, view.Snapshot().Hits)
	}
	if _, ok := findSnapshotHitByLabel(view.Snapshot(), gfx.RoleOption, state.hosts[0].Label); ok {
		t.Fatal("the list did not scroll down to the chosen host")
	}

	// The wheel over the open list scrolls it back towards the top.
	if err := view.Scroll(gfx.ScrollEvent{X: hit.Bounds.X + hit.Bounds.Width/2, Y: hit.Bounds.Y + hit.Bounds.Height/2, DeltaY: -1000}); err != nil {
		t.Fatal(err)
	}
	if err := view.Wait(50 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if _, ok := findSnapshotHitByLabel(view.Snapshot(), gfx.RoleOption, state.hosts[0].Label); !ok {
		t.Fatal("the wheel did not move the list")
	}
	// A wheel beside the list leaves it where it is.
	before := view.Snapshot()
	if err := view.Scroll(gfx.ScrollEvent{X: 4, Y: 4, DeltaY: 1000}); err != nil {
		t.Fatal(err)
	}
	if err := view.Wait(50 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	after := view.Snapshot()
	if _, ok := findSnapshotHitByLabel(after, gfx.RoleOption, last.Label); ok {
		t.Fatal("a wheel beside the list moved it")
	}
	if _, ok := findSnapshotHitByLabel(before, gfx.RoleOption, last.Label); ok {
		t.Fatal("the list showed its last row before the wheel beside it")
	}
}
