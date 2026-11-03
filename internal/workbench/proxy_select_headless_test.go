//go:build darwin || linux || windows

package workbench

import (
	"fmt"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
)

// hostSelectTestState is the proxy dialog with more hosts than the popup shows,
// opened on the last one so the select has to scroll to its selection.
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
	state.Click(hitProxies)
	state.Click(hitProxyAdd)
	if state.modal != modalNewProxy {
		t.Fatalf("proxy dialog = %d", state.modal)
	}
	state.proxyEditor.targetID = state.hosts[len(state.hosts)-1].ID
	return state
}

// The jump and target selects paint six rows and let MenuScroll move the rest:
// the wheel changes which hosts show, and opening lands on the current host so
// the arrow keys continue from there instead of from the top of the list.
func TestProxyHostSelectScrollsAndOpensOnTheSelectedHost(t *testing.T) {
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
	if !state.proxyEditor.targetOpen {
		t.Fatal("the target select did not open")
	}
	if got, want := state.popups.surface.Height, float32(8+proxyHostVisible*32); got != want {
		t.Fatalf("popup = %.0f tall, want %.0f", got, want)
	}
	if state.popups.scroll.Offset == 0 {
		t.Fatal("the popup did not scroll to the selected host")
	}
	last := hitProxyTargetItemList.At(int(state.proxyEditor.targetID))
	if focused := view.Snapshot().Focused; focused != last {
		t.Fatalf("focus on open = %v, want the selected row %v", focused, last)
	}

	// Up from the selection must land on the previous host, not on the first.
	previous := hitProxyTargetItemList.At(int(state.hosts[len(state.hosts)-2].ID))
	if err := view.Driver().Key(ui.KeyUp, 0); err != nil {
		t.Fatal(err)
	}
	if err := view.Wait(50 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if focused := view.Snapshot().Focused; focused != previous {
		t.Fatalf("Up moved focus to %v, want %v", focused, previous)
	}

	// The wheel over the popup moves the list; one over the page behind it does not.
	box, scroll := state.popups.surface, state.popups.scroll.Offset
	if err := view.Scroll(ui.ScrollEvent{X: box.X + box.Width/2, Y: box.Y + box.Height/2, DeltaY: -96}); err != nil {
		t.Fatal(err)
	}
	if err := view.Wait(50 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if state.popups.scroll.Offset >= scroll {
		t.Fatalf("the wheel did not move the list: %.0f -> %.0f", scroll, state.popups.scroll.Offset)
	}
	moved := state.popups.scroll.Offset
	if err := view.Scroll(ui.ScrollEvent{X: 4, Y: 4, DeltaY: 1000}); err != nil {
		t.Fatal(err)
	}
	if err := view.Wait(50 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if state.popups.scroll.Offset != moved {
		t.Fatalf("a wheel beside the popup moved it: %.0f -> %.0f", moved, state.popups.scroll.Offset)
	}
}
