package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
)

func TestTerminalTypingSurvivesClosingAnEarlierTab(t *testing.T) {
	state, world, _, _ := liveSession(t)
	liveTab(t, state, world, 1)
	last := liveTab(t, state, world, 2).shell(t)
	lastID := state.sessions[2].ID
	driver := paneDriver(t, state, ui.Size{Width: 1400, Height: 900})
	if err := driver.Type("before"); err != nil {
		t.Fatal(err)
	}

	firstTab := tabOrder(state)[0]
	firstHit := hitOf(t, driver, hitTabList.At(int(firstTab)))
	if err := driver.Move(firstHit.Bounds.X+firstHit.Bounds.Width/2, firstHit.Bounds.Y+firstHit.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	closeHit := hitOf(t, driver, hitTabCloseList.At(int(firstTab)))
	if err := driver.Click(closeHit.Bounds.X+closeHit.Bounds.Width/2, closeHit.Bounds.Y+closeHit.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if len(state.sessions) != 2 || state.activeSessionID != lastID {
		t.Fatalf("closing the earlier tab left %d sessions, active=%d, want %d", len(state.sessions), state.activeSessionID, lastID)
	}
	if err := driver.Type("after"); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(ui.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if got := last.typed(); got != "beforeafter\r" {
		t.Fatalf("surviving terminal received %q, want beforeafter\\r", got)
	}
}

func TestTerminalTextStaysWithItsSessionAfterListCompaction(t *testing.T) {
	state, world, _, _ := liveSession(t)
	ownerShell := liveTab(t, state, world, 1).shell(t)
	ownerID := state.sessions[1].ID
	otherShell := liveTab(t, state, world, 2).shell(t)
	firstID := state.sessions[0].ID
	state.ActivatePane(ownerID)
	input := state.TerminalInput(app.TextInputContext{ID: state.activeTerminalFocusHit(), Terminal: true})
	if input == nil {
		t.Fatal("no terminal input")
	}
	state.closeSession(firstID)
	if _, err := input.OnEvent(app.TextEvent{Kind: app.TextSetMarked, Text: "ni", MarkedSelection: app.TextRange{Start: 2, End: 2}}); err != nil {
		t.Fatal(err)
	}
	owner, _ := state.sessionByIDPointer(ownerID)
	if !owner.term.preedit.Composing || owner.term.preedit.Value != "ni" {
		t.Errorf("composition was lost by its owner: %+v", owner.term.preedit)
	}
	input.OnEvent(app.TextEvent{Kind: app.TextInsert, Text: "你"})
	input.OnEvent(app.TextEvent{Kind: app.TextPaste, Text: " echo hi\n"})
	if got := ownerShell.typed(); got != "你 echo hi\r" {
		t.Errorf("owner received %q, want Chinese commit and paste", got)
	}
	if got := otherShell.typed(); got != "" {
		t.Errorf("input reached another session: %q", got)
	}
}

func TestClosedTerminalTextCannotReachAReusedSessionSlot(t *testing.T) {
	state, world, _, _ := liveSession(t)
	liveTab(t, state, world, 1)
	ownerID := state.sessions[0].ID
	state.ActivatePane(ownerID)
	input := state.TerminalInput(app.TextInputContext{ID: state.activeTerminalFocusHit(), Terminal: true})
	if input == nil {
		t.Fatal("no terminal input")
	}
	state.closeSession(ownerID)
	state.ActivatePane(state.sessions[0].ID)
	if _, err := input.OnEvent(app.TextEvent{Kind: app.TextInsert, Text: "late commit"}); err != nil {
		t.Fatal(err)
	}
	if got := world.conns[1].shell(t).typed(); got != "" {
		t.Fatalf("a closed terminal's text reached another shell: %q", got)
	}
}

func TestTerminalFindSurvivesClosingAnEarlierTab(t *testing.T) {
	state, world, _, _ := liveSession(t)
	liveTab(t, state, world, 1)
	firstID, ownerID := state.sessions[0].ID, state.sessions[1].ID
	owner, _ := state.sessionByIDPointer(ownerID)
	state.openTerminalFind(owner)
	input := state.TerminalFindInput(app.TextInputContext{ID: hitTerminalFindField})
	if input == nil {
		t.Fatal("no find input")
	}
	state.closeSession(firstID)
	if _, err := input.OnEvent(app.TextEvent{Kind: app.TextInsert, Text: "target"}); err != nil {
		t.Fatal(err)
	}
	owner, _ = state.sessionByIDPointer(ownerID)
	if got := owner.term.find.editor.String(); got != "target" {
		t.Fatalf("find received %q after closing an earlier tab", got)
	}
}
