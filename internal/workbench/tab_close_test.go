package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

// confirmRunningClose answers the question that closing a session which still
// runs asks, for tests about what closing does rather than about the question.
func confirmRunningClose(state *State) {
	if state.modal == modalCloseTabs {
		state.Click(hitTabsCloseConfirm)
	}
}

func TestClosingARunningSessionAsksFirstAndCancelKeepsIt(t *testing.T) {
	state, _, _, _ := liveSession(t)
	id := state.sessions[0].ID
	state.Click(hitTabCloseList.At(int(id)))
	if state.modal != modalCloseTabs || len(state.tabsClose.running) != 1 {
		t.Fatalf("modal %d, running %v", state.modal, state.tabsClose.running)
	}
	state.Click(hitTabsCloseCancel)
	if _, ok := state.sessionByIDPointer(id); !ok {
		t.Fatal("cancel closed the session")
	}
	state.Click(hitTabCloseList.At(int(id)))
	state.Click(hitTabsCloseConfirm)
	if _, ok := state.sessionByIDPointer(id); ok {
		t.Fatal("confirming left the session open")
	}
	if state.closeConfirmOff {
		t.Fatal("confirming without the box turned the question off")
	}
}

func TestTheDoNotAskBoxStopsTheQuestionAndSurvivesARestart(t *testing.T) {
	state, world, _, _ := liveSession(t)
	liveTab(t, state, world, 1)
	first, second := state.sessions[0].ID, state.sessions[1].ID
	state.Click(hitTabCloseList.At(int(first)))
	state.Click(hitTabsCloseQuiet)
	state.Click(hitTabsCloseConfirm)
	if !state.closeConfirmOff {
		t.Fatal("the box did not turn the question off")
	}
	if state.snapshot().TerminalCloseConfirm != "off" {
		t.Fatal("the choice is not saved")
	}
	state.Click(hitTabCloseList.At(int(second)))
	if state.modal == modalCloseTabs {
		t.Fatal("asked again after being told not to")
	}
	if _, ok := state.sessionByIDPointer(second); ok {
		t.Fatal("the second tab did not close")
	}
}

func TestAnEndedSessionClosesWithoutAsking(t *testing.T) {
	state, _, _, shell := liveSession(t)
	id := state.sessions[0].ID
	shell.end(nil)
	state.Wake()
	state.Click(hitTabCloseList.At(int(id)))
	if state.modal == modalCloseTabs {
		t.Fatal("asked about a session that already ended")
	}
}

func TestMiddleClickClosesTheTargetTabWithoutSwitchingTabs(t *testing.T) {
	for _, orientation := range []struct {
		name     string
		vertical bool
	}{{"horizontal", false}, {"vertical", true}} {
		t.Run(orientation.name, func(t *testing.T) {
			state, tabs := fourTabs(t)
			if orientation.vertical {
				state.setTabOrientation(tabsVertical)
			}
			var canvas gfx.Canvas
			state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
			for _, target := range []gfx.ID{hitTabList.At(int(tabs[1])), hitTabCloseList.At(int(tabs[3]))} {
				if _, ok := hitByID(&canvas, target); !ok {
					t.Fatalf("tab target %v is not painted", target)
				}
			}
			active := state.deck.ActiveWorkspace()
			result := pointer(state, gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonMiddle, Target: hitTabList.At(int(tabs[1]))})
			if !result.Handled || !result.Repaint || state.modal != modalNone || state.deck.ActiveWorkspace() != active {
				t.Fatalf("middle press on inactive tab: result=%+v modal=%d active=%d", result, state.modal, state.deck.ActiveWorkspace())
			}
			result = pointer(state, gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonMiddle, Target: hitTabCloseList.At(int(tabs[3]))})
			if !result.Handled || !result.Repaint || state.modal != modalNone {
				t.Fatalf("middle press on close button: result=%+v modal=%d", result, state.modal)
			}
			if got := tabsOf(state); len(got) != 2 || got[0] != tabs[0] || got[1] != tabs[2] {
				t.Fatalf("remaining tabs = %v", got)
			}
		})
	}
}

func TestMiddleClickRespectsRunningAndLockedTabConfirmations(t *testing.T) {
	state, _, _, _ := liveSession(t)
	id := state.deck.ActiveWorkspace()
	press := gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonMiddle, Target: hitTabList.At(int(id))}
	pointer(state, press)
	if state.modal != modalCloseTabs || len(state.tabsClose.running) != 1 || len(tabsOf(state)) != 1 {
		t.Fatal("middle press bypassed running-session confirmation")
	}
	pointer(state, press)
	if len(tabsOf(state)) != 1 {
		t.Fatal("middle press closed a tab behind the dialog")
	}
	state.Click(hitTabsCloseCancel)
	lockTab(t, state, id)
	pointer(state, press)
	if state.modal != modalCloseTabs || state.tabsClose.locked != 1 || len(tabsOf(state)) != 1 {
		t.Fatal("middle press bypassed locked-tab confirmation")
	}
	state.Click(hitTabsCloseConfirm)
	if len(tabsOf(state)) != 0 {
		t.Fatal("confirming did not close the locked tab")
	}
}
