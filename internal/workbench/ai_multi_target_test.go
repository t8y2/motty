package workbench

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/ai/agent"
	"motty/internal/ai/llm"
	"motty/internal/ai/store"
	"motty/internal/sshconn"
	layoutcore "motty/internal/workspace"
)

// multiTargetWorld has three connected terminals: the first is a tab of its
// own, the second and third are the two panes of one split tab.
func multiTargetWorld(t *testing.T, model llm.Provider) *State {
	t.Helper()
	world, options := newWorld(t)
	options.Invalidate = func() {}
	options.AIStorePath = filepath.Join(t.TempDir(), "ai.db")
	options.AIProvider = func(store.Provider) llm.Provider { return model }
	state := newTestState(t, options)
	t.Cleanup(state.Close)
	live := func() {
		fake := world.conns[len(world.conns)-1]
		fake.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
		state.Wake()
		fake.shell(t).say("\x1b[0m")
		state.Wake()
	}
	for index := 0; index < 2; index++ {
		state.activeSection = sectionHosts
		state.selectedHostID = state.hosts[index].ID
		connect(t, state, world)
		live()
	}
	if !state.splitActivePane(2) {
		t.Fatal("could not split the second tab")
	}
	state.Click(hitPaneChooseHost)
	state.Click(hitHostList.At(int(state.hosts[0].ID)))
	state.selectedHostID = state.hosts[0].ID
	connect(t, state, world)
	live()
	withModel(t, state)
	if len(state.sessions) != 3 {
		t.Fatalf("sessions = %d, want 3", len(state.sessions))
	}
	return state
}

func TestTickingConnectionsSendsEveryOneTheirTextWithTheQuestion(t *testing.T) {
	model := &scriptedModel{turn: func(int, llm.Request) []llm.Event { return textTurn("ok") }}
	state := multiTargetWorld(t, model)
	first, second, third := &state.sessions[0], &state.sessions[1], &state.sessions[2]
	first.term.write([]byte("MARK_FIRST"))
	second.term.write([]byte("MARK_SECOND"))
	third.term.write([]byte("MARK_THIRD"))

	state.aiChooseOnlyTarget(hitAITargets.At(int(first.ID)))
	state.aiChooseTarget(hitAITargets.At(int(second.ID)))
	if state.aiTarget().ID != sessionHandle(first.ID) || len(state.ai.extras) != 1 {
		t.Fatalf("default %q with %d more, want the first tick to stay the default", state.aiTarget().ID, len(state.ai.extras))
	}
	ask(state, "compare them", agent.ModeAsk)
	pumpUntil(t, state, "answer", func() bool { return state.ai.run == nil })

	sent := model.requests[0]
	var user string
	for _, message := range sent.Messages {
		if message.Role == llm.RoleUser {
			user += message.Text
		}
	}
	for _, want := range []string{"MARK_FIRST", "MARK_SECOND"} {
		if !strings.Contains(user, want) {
			t.Errorf("the question did not carry %s: %q", want, user)
		}
	}
	if strings.Contains(user, "MARK_THIRD") {
		t.Error("an unticked terminal's text was sent")
	}
	if !strings.Contains(sent.System, "session "+sessionHandle(second.ID)) {
		t.Errorf("the model was not told about the second connection: %q", sent.System)
	}
	if got := state.ai.items[0]; got.kind != aiTargetChange || !strings.Contains(got.text, first.Title) || !strings.Contains(got.text, second.Title) {
		t.Errorf("the separator names %q, want both connections", got.text)
	}
}

func TestUntickingTheDefaultPromotesTheNextAndNoneClears(t *testing.T) {
	state := multiTargetWorld(t, quietModel{"ok"})
	first, second := &state.sessions[0], &state.sessions[1]
	state.aiChooseOnlyTarget(hitAITargets.At(int(first.ID)))
	state.aiChooseTarget(hitAITargets.At(int(second.ID)))
	state.aiChooseTarget(hitAITargets.At(int(first.ID)))
	if state.aiTarget().ID != sessionHandle(second.ID) || len(state.ai.extras) != 0 {
		t.Fatalf("after unticking the default, target = %q with %d more", state.aiTarget().ID, len(state.ai.extras))
	}
	state.aiChooseTarget(hitAITargets.At(int(first.ID)))
	state.aiChooseTarget(hitAITargets.At(0))
	if state.aiTarget().ID != "" || len(state.ai.extras) != 0 || state.ai.panel.targetOpen {
		t.Fatal("choosing no connection kept some selected")
	}
}

func TestATabRowTicksEveryTerminalOfASplitTabAndNothingElse(t *testing.T) {
	state := multiTargetWorld(t, quietModel{"ok"})
	first, second := &state.sessions[0], &state.sessions[1]
	workspace, _, ok := state.deck.PaneForSession(layoutcore.SessionID(second.ID))
	if !ok {
		t.Fatal("the second terminal has no tab")
	}
	if _, _, same := state.deck.PaneForSession(layoutcore.SessionID(first.ID)); !same {
		t.Fatal("the first terminal has no tab")
	}
	var rows int
	for _, option := range state.aiTargetOptions() {
		if _, isTab := option.ID.In(hitAITargetTabs); isTab {
			rows++
		}
	}
	if rows != 1 {
		t.Fatalf("%d whole-tab rows, want one for the only split tab", rows)
	}

	state.aiChooseOnlyTarget(hitAITargets.At(int(first.ID)))
	state.aiChooseTarget(hitAITargetTabs.At(int(workspace)))
	ids := state.aiSelectedIDs()
	if len(ids) != 3 || ids[0] != first.ID {
		t.Fatalf("selected %v, want the first terminal then both panes", ids)
	}
	state.aiChooseTarget(hitAITargetTabs.At(int(workspace)))
	ids = state.aiSelectedIDs()
	if len(ids) != 1 || ids[0] != first.ID {
		t.Fatalf("selected %v after unticking the tab, want only the first terminal", ids)
	}
}

func TestRenamingOneTerminalOfASplitTabRenamesOnlyThatTerminal(t *testing.T) {
	state := multiTargetWorld(t, quietModel{"ok"})
	second, third := &state.sessions[1], &state.sessions[2]
	tabTitle := func() string {
		workspace, _, _ := state.deck.PaneForSession(layoutcore.SessionID(second.ID))
		index, _ := state.workspaceTab(workspace)
		return state.workspaceTabs[index].Title
	}
	before := tabTitle()

	state.terminalMenu.pointer = gfx.Point{X: 600, Y: 300}
	state.openTerminalMenu(third)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, hitTerminalMenuRename); !ok {
		t.Fatal("the menu of a split tab's terminal has no rename entry")
	}
	state.Click(hitTerminalMenuRename)
	if state.modal != modalRenameTab || state.tabRename.editor.String() != third.Title {
		t.Fatalf("modal %v with %q, want the dialog on the terminal's own name", state.modal, state.tabRename.editor.String())
	}
	state.tabRename.editor.Insert("web-logs")
	state.Key(gfx.KeyEvent{Key: gfx.KeyEnter, Target: hitTabRenameField})
	if third.Title != "web-logs" {
		t.Fatalf("terminal name = %q", third.Title)
	}
	if second.Title == "web-logs" || tabTitle() != before {
		t.Fatalf("the sibling terminal or the tab was renamed too: %q, tab %q", second.Title, tabTitle())
	}
	var names []string
	for _, name := range state.mentionNames(nil) {
		names = append(names, name.name)
	}
	if !slices.Contains(names, "web-logs") {
		t.Fatalf("@ mentions %v do not offer the new name", names)
	}
	restored := newTestState(t, state.options)
	found := false
	for _, session := range restored.sessions {
		found = found || session.Title == "web-logs"
	}
	if !found {
		t.Fatal("the terminal name did not survive a restart")
	}
}

func TestTheSelectorStopsAtTheConnectionLimitAndSaysWhy(t *testing.T) {
	state := multiTargetWorld(t, quietModel{"ok"})
	ready := 0
	for index := range state.sessions {
		if aiSessionInfo(&state.sessions[index]).Ready {
			ready++
		}
	}
	for ready < maxAITargets+1 {
		// Copies of a ready session stand in for more open connections.
		state.sessions = append(state.sessions, state.sessions[0])
		state.sessions[len(state.sessions)-1].ID = uint32(100 + ready)
		ready++
	}
	state.aiSetSelected(nil)
	for _, session := range state.sessions {
		state.aiChooseTarget(hitAITargets.At(int(session.ID)))
	}
	if got := len(state.aiSelectedIDs()); got != maxAITargets {
		t.Fatalf("selected %d connections, want it to stop at %d", got, maxAITargets)
	}
	if want := fmt.Sprintf(state.messages.AIPanel.TargetLimitFmt, maxAITargets); state.ai.notice != want {
		t.Fatalf("notice = %q, want %q", state.ai.notice, want)
	}
}
