package workbench

import (
	"testing"
	"time"

	"github.com/vivaui/viva/ui"
	"motty/internal/cmdhistory"
)

// ghostWorld is a history world whose host already has some commands.
func ghostWorld(t *testing.T, commands ...string) *historyWorld {
	t.Helper()
	h := newHistoryWorld(t, true)
	store := h.state.historyStore()
	key := historyKey(&h.state.sessions[0])
	base := time.Now()
	for index, command := range commands {
		if err := store.Record(key, command, base.Add(time.Duration(index)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := store.List(key)
	if err != nil {
		t.Fatal(err)
	}
	// Seed the fixture's prepared cache; real windows fill it off the UI thread.
	h.state.forgetCompletionHistory()
	h.state.history.ghost = map[string][]cmdhistory.Entry{key: entries}

	return h
}

func (h *historyWorld) ghost() (string, bool) { return h.state.commandGhost(&h.state.sessions[0]) }

func (h *historyWorld) key(event ui.KeyEvent) {
	event.Target = hitTerminalList.At(int(h.state.sessions[0].ID))
	h.state.Key(event)
	h.state.Wake()
}

func TestTypingTheStartOfAKnownCommandSuggestsTheRestAndRightArrowTakesIt(t *testing.T) {
	h := ghostWorld(t, "docker compose up -d", "git checkout main")
	h.prompt("deploy@prod:~$ ")
	if rest, ok := h.ghost(); ok {
		t.Fatalf("an empty prompt suggests %q", rest)
	}
	h.type_("git ch")
	if rest, ok := h.ghost(); !ok || rest != "eckout main" {
		t.Fatalf("suggestion = %q, %v", rest, ok)
	}
	before := len(h.shell.input)
	h.key(ui.KeyEvent{Key: ui.KeyRight})
	if got := string(h.shell.input[before:]); got != "eckout main" {
		t.Fatalf("the shell got %q, want the rest of the command and no arrow key", got)
	}
	if _, ok := h.ghost(); ok {
		t.Fatal("a complete command still has a suggestion")
	}
	// What was taken is the user's line: Enter remembers it as typed.
	h.type_("\r")
	if got := h.remembered(""); len(got) != 2 || got[0] != "git checkout main" {
		t.Fatalf("remembered %q", got)
	}
}

func TestAltRightTakesOneWordOfTheSuggestion(t *testing.T) {
	h := ghostWorld(t, "kubectl get pods -n kube-system")
	h.prompt("$ ")
	h.type_("kubectl ")
	before := len(h.shell.input)
	h.key(ui.KeyEvent{Key: ui.KeyRight, Modifiers: ui.ModifierAlt})
	if got := string(h.shell.input[before:]); got != "get" {
		t.Fatalf("the shell got %q, want one word", got)
	}
	h.key(ui.KeyEvent{Key: ui.KeyRight, Modifiers: ui.ModifierAlt})
	if got := string(h.shell.input[before:]); got != "get pods" {
		t.Fatalf("the shell got %q, want the second word too", got)
	}
}

func TestRightArrowIsTheShellsWhenNothingIsSuggested(t *testing.T) {
	h := ghostWorld(t, "git status")
	h.prompt("$ ")
	h.type_("ls")
	before := len(h.shell.input)
	h.key(ui.KeyEvent{Key: ui.KeyRight})
	if got := string(h.shell.input[before:]); got != "\x1b[C" {
		t.Fatalf("the shell got %q, want the arrow key", got)
	}
}

func TestNothingIsSuggestedWhereTheHistoryDoesNotApply(t *testing.T) {
	// Switched off, by the setting or by the history itself.
	h := ghostWorld(t, "git status")
	h.prompt("$ ")
	h.type_("git s")
	h.state.setCompletionMode(completionOff)
	if rest, ok := h.ghost(); ok {
		t.Fatalf("suggestions are off but %q is offered", rest)
	}
	h.state.setCompletionMode(completionBoth)
	h.state.history.on = false
	if rest, ok := h.ghost(); ok {
		t.Fatalf("the history is off but %q is offered", rest)
	}
	h.state.history.on = true
	h.state.sessions[0].host.NoHistory = true
	if rest, ok := h.ghost(); ok {
		t.Fatalf("an excluded host is offered %q", rest)
	}

	// A prompt that asks for a secret, even though the answer is echoed.
	h = ghostWorld(t, "git status")
	h.prompt("[sudo] password for deploy: ")
	h.type_("git s")
	if rest, ok := h.ghost(); ok {
		t.Fatalf("an answer to a password prompt is offered %q", rest)
	}

	// A line kept out of the history on purpose.
	h = ghostWorld(t, "git status")
	h.prompt("$ ")
	h.type_(" git s")
	if rest, ok := h.ghost(); ok {
		t.Fatalf("a line starting with a space is offered %q", rest)
	}
}

func TestACommandJustRunIsSuggestedWithoutReloadingTheHistory(t *testing.T) {
	h := ghostWorld(t, "ls")
	h.prompt("$ ")
	h.type_("systemctl status nginx")
	h.type_("\r")
	h.prompt("$ ")
	h.type_("systemctl s")
	if rest, ok := h.ghost(); !ok || rest != "tatus nginx" {
		t.Fatalf("suggestion = %q, %v", rest, ok)
	}
}

func TestTheSuggestionSettingSurvivesARestart(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	state.setCompletionMode(completionOff)
	state.save()
	again := newTestState(t, options)
	if !again.history.suggestOff {
		t.Fatal("turning the suggestions off was forgotten")
	}
}

func TestNoSuggestionIsOfferedWhileBroadcasting(t *testing.T) {
	// Accepting is never typed into several terminals, so showing a suggestion
	// the key could not accept would leave the key doing nothing.
	h := ghostWorld(t, "git status")
	h.prompt("$ ")
	h.type_("git s")
	if _, ok := h.ghost(); !ok {
		t.Fatal("no suggestion before broadcasting")
	}
	h.state.broadcast = h.state.deck.ActiveWorkspace()
	if rest, ok := h.ghost(); ok {
		t.Fatalf("a broadcasting tab is offered %q", rest)
	}
}

// awaitHistory repaints until the background reader has landed the cache for
// key, as a window's wake-ups do, and fails when it never comes. Asking for
// the ghost itself never starts a read, so the test waits the way a window
// learns of one.
func (h *historyWorld) awaitHistory(t *testing.T, key string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		h.state.Wake()
		if _, ok := h.state.history.ghost[key]; ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the history of %q was never read", key)
		}
		time.Sleep(time.Millisecond)
	}
}

// A command this host has never run, but the user ran on another host, is
// suggested once the host's own history has no match: the same machines often
// share one job (#219). The merged cache is not injected: the background read
// the window requests fills it, and the frame only reads what landed.
func TestACommandFromAnotherHostIsSuggestedWhenThisHostHasNoMatch(t *testing.T) {
	h := ghostWorld(t, "docker compose up -d")
	store := h.state.historyStore()
	if err := store.Record("other-host", "systemctl restart nginx", time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	h.prompt("deploy@prod:~$ ")
	// The host's own history still speaks first.
	h.type_("docker co")
	if rest, ok := h.ghost(); !ok || rest != "mpose up -d" {
		t.Fatalf("suggestion = %q, %v; want the host's own command", rest, ok)
	}
	// With no match of its own, what another host knows is offered once the
	// repaint has adopted the read.
	h.type_("\b\b\b\b\b\b\b\b\b")
	h.awaitHistory(t, "")
	h.type_("systemctl re")
	if rest, ok := h.ghost(); !ok || rest != "start nginx" {
		t.Fatalf("suggestion = %q, %v; want the command another host knows", rest, ok)
	}
}

// A host that keeps no history draws no suggestion from anyone else's either,
// and never asks for the merged read.
func TestAHostThatKeepsNoHistoryGetsNoSuggestionFromOtherHosts(t *testing.T) {
	h := ghostWorld(t, "docker compose up -d")
	store := h.state.historyStore()
	if err := store.Record("other-host", "systemctl restart nginx", time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	h.state.sessions[0].host.NoHistory = true
	h.state.forgetCompletionHistory()
	delete(h.state.completion.historyPending, "")
	h.prompt("deploy@prod:~$ ")
	h.type_("systemctl re")
	if rest, ok := h.ghost(); ok {
		t.Fatalf("a no-history host was suggested %q", rest)
	}
	if h.state.completion.historyPending[""] {
		t.Fatal("a host that keeps no history asked for the merged history")
	}
}

// A command just remembered joins the merged cache at its front as well, so a
// second host of the same job suggests it before any reload.
func TestACommandJustRunJoinsTheMergedCacheToo(t *testing.T) {
	h := ghostWorld(t, "ls")
	h.awaitHistory(t, "")
	h.state.history.ghost[""] = make([]cmdhistory.Entry, 0)
	h.prompt("$ ")
	h.type_("systemctl status nginx")
	h.type_("\r")
	for _, key := range []string{historyKey(&h.state.sessions[0]), ""} {
		entries := h.state.history.ghost[key]
		if len(entries) == 0 || entries[0].Command != "systemctl status nginx" {
			t.Fatalf("the cache of %q starts with %v, want the command just run", key, entries)
		}
	}
}

// Reading the ghost with no merged cache is a pure read: a frame must not open
// the history or start a worker, so only a wake-up may ask for it.
func TestAskingForTheGhostNeverStartsAHistoryRead(t *testing.T) {
	h := ghostWorld(t, "docker compose up -d")
	h.prompt("$ ")
	h.type_("systemctl re")
	delete(h.state.history.ghost, "")
	delete(h.state.completion.historyPending, "")
	h.state.commandGhost(&h.state.sessions[0])
	if h.state.completion.historyPending[""] {
		t.Fatal("reading the ghost started a history read")
	}
}
