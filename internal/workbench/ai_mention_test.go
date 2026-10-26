//go:build !windows

package workbench

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/ui"
	"motty/internal/ai/agent"
	"motty/internal/ai/llm"
	"motty/internal/ai/store"
	"motty/internal/sshconn"
)

// twoHostAssistant is a window with a live tab on Production and one on Staging,
// the Staging one active, and the assistant pointed at a scripted model.
func twoHostAssistant(t *testing.T, model *scriptedModel) (*State, *fakeShell, *fakeShell) {
	t.Helper()
	world, options := newWorld(t)
	options.Invalidate = func() {}
	options.AIStorePath = filepath.Join(t.TempDir(), "ai.db")
	options.AIProvider = func(store.Provider) llm.Provider { return model }
	state := newTestState(t, options)
	t.Cleanup(state.Close)
	shells := make([]*fakeShell, 0, 2)
	for host := 0; host < 2; host++ {
		state.activeSection = sectionHosts
		state.selectedHostID = state.hosts[host].ID
		fake := connect(t, state, world)
		fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseConnected })
		state.Wake()
		shell := fake.shell(t)
		shell.say("\x1b[0m")
		state.Wake()
		shells = append(shells, shell)
	}
	db, err := state.aiStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SaveProvider(store.Provider{ID: "p", Name: "test", Kind: store.KindOpenAI, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetDefaultModel("p", "m"); err != nil {
		t.Fatal(err)
	}
	state.reloadAI()
	state.Paint(&ui.Canvas{}, panelWide)
	return state, shells[0], shells[1]
}

func TestAnAtNameBringsThatSessionsTerminalAlongWithTheQuestion(t *testing.T) {
	model := &scriptedModel{turn: func(int, llm.Request) []llm.Event { return textTurn("compared") }}
	state, production, staging := twoHostAssistant(t, model)
	production.say("prod: replica lag 42s\r\n")
	staging.say("staging: replica lag 0s\r\n")
	state.Wake()

	ask(state, "is @Production behind @Staging? also mail me@example.com", agent.ModeAsk)
	pumpUntil(t, state, "the run to end", func() bool { return state.ai.run == nil })

	sent := model.requests[0].Messages[len(model.requests[0].Messages)-1].Text
	if !strings.Contains(sent, "replica lag 42s") || !strings.Contains(sent, "replica lag 0s") {
		t.Fatalf("the question did not carry both terminals:\n%s", sent)
	}
	// The active session's text goes once: Ask mode attached it, the mention did not repeat it.
	if n := strings.Count(sent, "replica lag 0s"); n != 1 {
		t.Fatalf("Staging's text was sent %d times", n)
	}
	if strings.Contains(state.ai.items[0].text, "replica lag") {
		t.Fatalf("attached text is shown as if the user typed it: %q", state.ai.items[0].text)
	}
}

func TestAnAtNameWorksInAgentModeAndNamesNothingElse(t *testing.T) {
	model := &scriptedModel{turn: func(int, llm.Request) []llm.Event { return textTurn("ok") }}
	state, production, _ := twoHostAssistant(t, model)
	production.say("prod: disk 91% full\r\n")
	state.Wake()

	ask(state, "check @production and @nowhere and user@Staging", agent.ModeAgent)
	pumpUntil(t, state, "the run to end", func() bool { return state.ai.run == nil })
	sent := model.requests[0].Messages[len(model.requests[0].Messages)-1].Text
	if !strings.Contains(sent, "disk 91% full") {
		t.Fatalf("@production (any case) did not attach its terminal:\n%s", sent)
	}
	if strings.Contains(sent, "Terminal of session") && strings.Count(sent, "Terminal of session") != 1 {
		t.Fatalf("an unknown name or an e-mail-like word attached text:\n%s", sent)
	}
}

func TestTypingAtOffersTheSessionsAndAClickOrTabCompletesTheName(t *testing.T) {
	state, _, _ := twoHostAssistant(t, &scriptedModel{turn: func(int, llm.Request) []llm.Event { return nil }})
	state.setAIPanelOpen(true)
	state.ai.input.Insert("compare with @Pr")
	canvas, _ := paintAt(state, panelWide)
	first := panelHit(t, canvas, hitAIMentions.At(0))
	if first.Label != "@Production" {
		t.Fatalf("the first suggestion is %q", first.Label)
	}
	if _, ok := hitByID(canvas, hitAIMentions.At(1)); ok {
		t.Fatal("Staging does not start with Pr")
	}
	state.Click(hitAIMentions.At(0))
	if got := state.ai.input.String(); got != "compare with @Production " {
		t.Fatalf("the box holds %q", got)
	}
	// With nothing after an @ yet, both sessions are offered.
	state.ai.input.Insert("and @")
	canvas, _ = paintAt(state, panelWide)
	for index := 0; index < 2; index++ {
		panelHit(t, canvas, hitAIMentions.At(index))
	}
	// Tab takes the first, and an @ inside a word offers nothing.
	result, handled := state.aiPanelKey(ui.KeyEvent{Key: ui.KeyTab, Target: hitAIInput})
	if !handled || !result.Handled || !strings.HasSuffix(state.ai.input.String(), "and @Production ") {
		t.Fatalf("Tab: handled=%v input=%q", handled, state.ai.input.String())
	}
	state.ai.input.Insert("me@ex")
	canvas, _ = paintAt(state, panelWide)
	if _, ok := hitByID(canvas, hitAIMentions.At(0)); ok {
		t.Fatal("an e-mail address offered a session")
	}
}

func TestTwoTabsWithTheSameTitleGetDistinctNames(t *testing.T) {
	var state State
	state.sessions = []Session{
		{ID: 1, Title: "Prod DB", Address: "a", term: &terminal{}},
		{ID: 2, Title: "Prod DB", Address: "b", term: &terminal{}},
		{ID: 3, Title: "日本 サーバ", Address: "c", term: &terminal{}},
	}
	names := state.mentionNames(nil)
	if names[0].name != "Prod-DB" || names[1].name != "Prod-DB-2" || names[2].name != "日本-サーバ" {
		t.Fatalf("names = %+v", names)
	}
	if got := findMentions("see @prod-db-2, then @日本-サーバ.", names); len(got) != 2 || got[0] != 2 || got[1] != 3 {
		t.Fatalf("mentions = %v", got)
	}
}
