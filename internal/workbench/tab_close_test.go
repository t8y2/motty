package workbench

import "testing"

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
