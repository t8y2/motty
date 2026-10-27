package workbench

import (
	"fmt"
	"testing"

	"github.com/vivaui/viva/ui"
)

func manyHostsState(t *testing.T, count int) *State {
	t.Helper()
	_, options := newWorld(t)
	hosts := make([]savedHost, count)
	for index := range hosts {
		hosts[index] = savedHost{Label: fmt.Sprintf("Host %02d", index), Address: fmt.Sprintf("10.0.0.%d", index+1), User: "ops", Port: 22}
	}
	if err := saveState(options.StatePath, savedState{Hosts: hosts}); err != nil {
		t.Fatal(err)
	}
	return newTestState(t, options)
}

func TestAHostListLongerThanTheWindowScrollsToItsLastHost(t *testing.T) {
	state := manyHostsState(t, 40)
	size := ui.Size{Width: 1400, Height: 900}
	var canvas ui.Canvas
	state.Paint(&canvas, size)
	last := hitHostList.At(int(state.hosts[len(state.hosts)-1].ID))
	if _, ok := hitByID(&canvas, last); ok {
		t.Fatal("the last of forty hosts is reachable without scrolling")
	}
	if _, ok := hitByID(&canvas, hitHostList.At(int(state.hosts[0].ID))); !ok {
		t.Fatal("the first host is not reachable")
	}
	for step := 0; step < 200; step++ {
		state.Scroll(ui.ScrollEvent{DeltaY: 120, Precise: true, X: 600, Y: 400, Viewport: size})
		canvas = ui.Canvas{}
		state.Paint(&canvas, size)
	}
	hit, ok := hitByID(&canvas, last)
	if !ok {
		t.Fatalf("the last host is not reachable after scrolling to the end (scroll %v of %v)", state.hostsScroll, state.hostsContent)
	}
	if hit.Bounds.Y < state.hostsBounds.Y+100 || hit.Bounds.Y+hit.Bounds.Height > size.Height {
		t.Fatalf("the last host is drawn at %+v, outside the list", hit.Bounds)
	}
	if _, ok := hitByID(&canvas, hitHostList.At(int(state.hosts[0].ID))); ok {
		t.Fatal("the first host is still reachable at the end of the list")
	}
	// Back to the top.
	for step := 0; step < 200; step++ {
		state.Scroll(ui.ScrollEvent{DeltaY: -120, Precise: true, X: 600, Y: 400, Viewport: size})
	}
	if state.hostsScroll != 0 {
		t.Fatalf("scroll %v at the top", state.hostsScroll)
	}
}

func TestAShortHostListDoesNotScroll(t *testing.T) {
	state := manyHostsState(t, 3)
	size := ui.Size{Width: 1400, Height: 900}
	state.Paint(&ui.Canvas{}, size)
	result := state.Scroll(ui.ScrollEvent{DeltaY: 200, Precise: true, X: 600, Y: 400, Viewport: size})
	if result.Repaint || state.hostsScroll != 0 {
		t.Fatalf("result %+v, scroll %v", result, state.hostsScroll)
	}
}

func TestTheHostsPageShowsCardsAndRemembersTheChoice(t *testing.T) {
	state := manyHostsState(t, 40)
	size := ui.Size{Width: 1400, Height: 900}
	var canvas ui.Canvas
	state.Paint(&canvas, size)
	if hit, ok := hitByID(&canvas, hitHostViewGrid); !ok || hit.Selected {
		t.Fatalf("grid toggle: found %v", ok)
	}
	state.Click(hitHostViewGrid)
	if state.hostView != hostViewGrid {
		t.Fatal("the grid was not chosen")
	}
	canvas = ui.Canvas{}
	state.Paint(&canvas, size)
	first, ok := hitByID(&canvas, hitHostList.At(int(state.hosts[0].ID)))
	second, ok2 := hitByID(&canvas, hitHostList.At(int(state.hosts[1].ID)))
	if !ok || !ok2 || first.Bounds.Y != second.Bounds.Y || second.Bounds.X <= first.Bounds.X+first.Bounds.Width-1 {
		t.Fatalf("cards are not side by side: %+v %+v", first.Bounds, second.Bounds)
	}
	last := hitHostList.At(int(state.hosts[len(state.hosts)-1].ID))
	if _, ok := hitByID(&canvas, last); ok {
		t.Fatal("the last card is reachable without scrolling")
	}
	for step := 0; step < 100; step++ {
		state.Scroll(ui.ScrollEvent{DeltaY: 200, Precise: true, X: 600, Y: 400, Viewport: size})
		canvas = ui.Canvas{}
		state.Paint(&canvas, size)
	}
	if _, ok := hitByID(&canvas, last); !ok {
		t.Fatalf("the last card is unreachable at the end (scroll %v of %v)", state.hostsScroll, state.hostsContent)
	}

	// Selecting a card and its check box works as it does in the list.
	state.Click(last)
	if state.selectedHostID != state.hosts[len(state.hosts)-1].ID {
		t.Fatalf("selected %d", state.selectedHostID)
	}
	saved, _, err := loadState(state.options.StatePath)
	if err != nil || saved.HostView != "grid" {
		t.Fatalf("saved view %q, %v", saved.HostView, err)
	}
	state.Click(hitHostViewList)
	if state.hostView != hostViewList || state.hostsScroll != 0 {
		t.Fatalf("back to the list: %v scroll %v", state.hostView, state.hostsScroll)
	}
}

// A thousand hosts as cards: only the cards in the window are declared and
// painted, wherever the page is scrolled to.
func TestTheCardGridPaintsOnlyWhatIsInTheWindow(t *testing.T) {
	state := hostsState(t, 1000)
	state.hostView = hostViewGrid
	size := ui.Size{Width: 1400, Height: 900}
	for _, scroll := range []float32{0, 5000, 12345} {
		state.hostsScroll = scroll
		var canvas ui.Canvas
		state.Paint(&canvas, size)
		cards := 0
		for _, host := range state.hosts {
			if _, ok := hitByID(&canvas, hitHostList.At(int(host.ID))); ok {
				cards++
			}
		}
		if cards == 0 || cards >= len(state.hosts)/2 {
			t.Fatalf("scroll %v: %d cards painted", scroll, cards)
		}
	}
}
