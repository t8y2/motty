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

// The host pickers scroll to the chosen host when they open, keep the keyboard
// in their search field, and let the wheel move the list only over its own
// surface.
func TestProxyHostSelectRevealsTheChosenHostAndScrolls(t *testing.T) {
	state := hostSelectTestState(t)
	view, err := headless.Open(app.Config{
		Title: "proxy", Width: 1400, Height: 900, Paint: state.Paint, OnPointer: state.Pointer,
		OnKey: state.Key, OnScroll: state.Scroll,
	}, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	if err := view.Wait(100 * time.Millisecond); err != nil {
		t.Fatal(err)
	}

	state.Click(hitProxyDialogTarget)
	if err := view.Wait(100 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if !state.proxyEditor.targetSelect.Open {
		t.Fatal("the target picker did not open")
	}
	if focused := view.Snapshot().Focused; focused != hitProxyDialogTarget {
		t.Fatalf("focus on open = %v, want the search field %v", focused, hitProxyDialogTarget)
	}
	canvas, _ := paintedTexts(state)
	last := len(state.hosts) - 1
	if !hasHit(canvas, hitProxyTargetItemList.At(last)) {
		t.Fatalf("the chosen host %d was not revealed", last)
	}
	if hasHit(canvas, hitProxyTargetItemList.At(0)) {
		t.Fatal("the list did not scroll down to the chosen host")
	}

	// The wheel over the popup scrolls it back to the top.
	box := state.proxyEditor.targetPopup
	if err := view.Scroll(gfx.ScrollEvent{X: box.X + box.Width/2, Y: box.Y + box.Height/2, DeltaY: -1000}); err != nil {
		t.Fatal(err)
	}
	if err := view.Wait(50 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	canvas, _ = paintedTexts(state)
	if !hasHit(canvas, hitProxyTargetItemList.At(0)) || hasHit(canvas, hitProxyTargetItemList.At(last)) {
		t.Fatal("the wheel did not move the list")
	}
	// A wheel beside the popup leaves it where it is.
	if err := view.Scroll(gfx.ScrollEvent{X: 4, Y: 4, DeltaY: 1000}); err != nil {
		t.Fatal(err)
	}
	if err := view.Wait(50 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	canvas, _ = paintedTexts(state)
	if !hasHit(canvas, hitProxyTargetItemList.At(0)) || hasHit(canvas, hitProxyTargetItemList.At(last)) {
		t.Fatal("a wheel beside the popup moved the list")
	}
}
