package workbench

import (
	"fmt"
	"testing"

	"github.com/vivaui/viva/ui"
)

func TestNewTabButtonAsksWhereToConnectAndMakesNoTabYet(t *testing.T) {
	state := newTestState(t, testOptions(t))
	before := len(state.deck.Workspaces(nil))
	state.Click(hitNewTab)
	if state.modal != modalCommandPalette || !state.commandPalette.connect {
		t.Fatalf("modal = %d connect = %v", state.modal, state.commandPalette.connect)
	}
	if got := len(state.deck.Workspaces(nil)); got != before {
		t.Fatalf("the tab was created before a choice: %d -> %d", before, got)
	}
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, hitCommandPaletteItems.At(0)); !ok {
		t.Fatal("the picker lists nothing")
	}
	// Closing it creates nothing either.
	state.Click(hitCommandPaletteClose)
	if state.modal != modalNone || len(state.deck.Workspaces(nil)) != before {
		t.Fatalf("after closing: modal %d tabs %d", state.modal, len(state.deck.Workspaces(nil)))
	}
	if state.commandPalette.connect {
		t.Fatal("the next palette would open as the picker")
	}
}

func TestPickingAHostCreatesANewTabForIt(t *testing.T) {
	state := newTestState(t, testOptions(t))
	before := len(state.deck.Workspaces(nil))
	host := state.hosts[0]
	state.Click(hitNewTab)
	// The picker groups the saved hosts, so the row of a host is named by the
	// host rather than by its place among all the rows.
	clickPickerRow(t, state, host.Label)
	if state.modal != modalNone || len(state.deck.Workspaces(nil)) != before+1 {
		t.Fatalf("modal %d, tabs %d -> %d", state.modal, before, len(state.deck.Workspaces(nil)))
	}
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || session.HostID != host.ID || state.activeSection != sectionTerminal {
		t.Fatalf("active session %+v ok=%v section=%v", session, ok, state.activeSection)
	}
}

func TestPickingThisComputerOpensALocalShellInANewTab(t *testing.T) {
	state := newTestState(t, testOptions(t))
	before := len(state.deck.Workspaces(nil))
	state.Click(hitNewTab)
	state.Click(hitCommandPaletteItems.At(0))
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || !session.isLocal() || len(state.deck.Workspaces(nil)) != before+1 {
		t.Fatalf("local ok=%v tabs %d -> %d", ok, before, len(state.deck.Workspaces(nil)))
	}
}

func TestPickerSearchMatchesHostNamesAndAddresses(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitNewTab)
	state.commandPalette.search.Insert(state.hosts[0].Address)
	state.refreshCommandPalette()
	palette := &state.commandPalette
	if palette.visibleCount == 0 {
		t.Fatal("searching a host's address finds nothing")
	}
	if got := palette.entries[palette.visible[0]].target; got != hitConnectPickList.At(int(state.hosts[0].ID)) {
		t.Fatalf("first match target = %d", got)
	}
	state.commandPalette.search.Insert("zzzz-no-such-host")
	state.refreshCommandPalette()
	if palette.visibleCount != 0 {
		t.Fatalf("%d matches for a nonsense query", palette.visibleCount)
	}
}

func TestTheCommandPaletteIsStillTheCommandPalette(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitNewTab)
	state.Click(hitCommandPaletteClose)
	state.openCommandPalette()
	if state.commandPalette.connect {
		t.Fatal("the palette opened as the picker")
	}
	found := false
	for index := uint8(0); index < state.commandPalette.entryCount; index++ {
		found = found || state.commandPalette.entries[index].target == hitSettings
	}
	if !found {
		t.Fatal("the commands are missing from the palette")
	}
}

// clickPickerRow presses the picker row that shows label.
func clickPickerRow(t *testing.T, state *State, label string) {
	t.Helper()
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	for _, hit := range canvas.Hits() {
		if hit.ID.Scope() == hitCommandPaletteItems && hit.Label == label {
			state.Click(hit.ID)
			return
		}
	}
	t.Fatalf("the picker has no row for %q", label)
}

func TestNewTabPickerKeyboardStepsOverGroupHeadings(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitNewTab)
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	// Local terminal, then a heading for Network and Router under it, so the
	// row after the first is a heading the keyboard must not land on.
	first := hitCommandPaletteItems.At(0)
	result := state.Key(ui.KeyEvent{Target: first, Key: ui.KeyDown})
	if !result.Handled || result.Focus != hitCommandPaletteItems.At(2) {
		t.Fatalf("down from the first row focused %v, want the row under the heading", result.Focus)
	}
	last := hitCommandPaletteItems.At(int(state.commandPalette.visibleCount) - 1)
	result = state.Key(ui.KeyEvent{Target: result.Focus, Key: ui.KeyEnd})
	if !result.Handled || result.Focus != last {
		t.Fatalf("End focused %v, want the last choice %v", result.Focus, last)
	}
}

func TestNewTabPickerGroupsHostsAndTheWheelSurvivesTheNextPaint(t *testing.T) {
	options := testOptions(t)
	var hosts []savedHost
	for index := 0; index < 14; index++ {
		group := "Servers"
		if index%2 == 0 {
			group = "Lab"
		}
		hosts = append(hosts, savedHost{
			Label:   fmt.Sprintf("Host %02d", index),
			Group:   group,
			Address: fmt.Sprintf("host%02d.example.com", index),
			Port:    22,
		})
	}
	if err := saveState(options.StatePath, savedState{Hosts: hosts}); err != nil {
		t.Fatal(err)
	}
	state := newTestState(t, options)
	state.Click(hitNewTab)

	var canvas ui.Canvas
	size := ui.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)
	texts := map[string]bool{}
	for _, text := range canvas.Texts() {
		texts[text.Value] = true
	}
	for _, group := range []string{"Lab", "Servers"} {
		if !texts[group] {
			t.Fatalf("the picker does not head its hosts with the group %q: %v", group, texts)
		}
	}
	first, ok := hitByID(&canvas, hitCommandPaletteItems.At(0))
	if !ok {
		t.Fatal("the picker paints no first row")
	}
	if result := state.Scroll(ui.ScrollEvent{DeltaY: 120, Target: first.ID}); !result.Repaint || state.commandPalette.top == 0 {
		t.Fatalf("the wheel did not move the list: %+v, top %d", result, state.commandPalette.top)
	}
	top := state.commandPalette.top
	canvas.Reset()
	state.Paint(&canvas, size)
	if state.commandPalette.top != top {
		t.Fatalf("the next paint returned the list to row %d, want it to stay at %d", state.commandPalette.top, top)
	}
	after, ok := hitByID(&canvas, hitCommandPaletteItems.At(0))
	if !ok || after.Label == first.Label {
		t.Fatalf("the wheel did not change the first row: %q -> %q", first.Label, after.Label)
	}
}
