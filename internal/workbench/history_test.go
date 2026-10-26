package workbench

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
)

// historyWorld is a live SSH session whose shell echoes what is typed unless
// told not to, with command history on and a clock the test moves.
type historyWorld struct {
	state *State
	shell *fakeShell
	clock time.Duration
	echo  bool
}

func newHistoryWorld(t *testing.T, on bool) *historyWorld {
	t.Helper()
	world, options := newWorld(t)
	options.Invalidate = func() {}
	options.HistoryPath = filepath.Join(t.TempDir(), "history.db")
	h := &historyWorld{echo: true}
	options.Now = func() time.Duration { return h.clock }
	h.state = newTestState(t, options)
	h.state.history.on = on
	fake := connect(t, h.state, world)
	fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseConnected })
	h.state.Wake()
	h.shell = fake.shell(t)
	h.shell.onWrite = func(p []byte) {
		if h.echo {
			h.shell.say(strings.ReplaceAll(string(p), "\r", "\r\n"))
		}
	}
	return h
}

// prompt makes the remote print a prompt and go quiet, as a shell waiting does.
func (h *historyWorld) prompt(text string) {
	h.shell.say("\r\n" + text)
	h.state.Wake()
	h.clock += time.Second
}

func (h *historyWorld) type_(keys string) {
	h.state.typed(&h.state.sessions[0], []byte(keys))
	h.state.Wake()
	h.clock += 20 * time.Millisecond
}

func (h *historyWorld) remembered(host string) []string {
	var commands []string
	store := h.state.historyStore()
	if store == nil {
		return nil
	}
	entries, err := store.List(host)
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		commands = append(commands, entry.Command)
	}
	return commands
}

func TestHistoryRemembersTypedCommandsButNeverAPassword(t *testing.T) {
	h := newHistoryWorld(t, true)
	h.prompt("deploy@prod:~$ ")
	h.type_("ls -la")
	h.type_("\r")
	h.prompt("[sudo] password for deploy: ")
	h.echo = false
	h.type_("hunter2")
	h.type_("\r")
	h.shell.say("\r\n") // sudo ends the line itself
	h.echo = true
	h.prompt("deploy@prod:~$ ")
	h.type_("systemctl status nginx")
	h.type_("\r")
	// A program that asks for something secret without a telltale prompt.
	h.prompt("Enter: ")
	h.echo = false
	h.type_("correct horse battery staple")
	h.type_("\r")

	got := h.remembered("")
	if strings.Join(got, "|") != "systemctl status nginx|ls -la" {
		t.Fatalf("remembered %q", got)
	}
	for _, command := range got {
		if strings.Contains(command, "hunter2") || strings.Contains(command, "horse") {
			t.Fatalf("a secret was remembered: %q", command)
		}
	}
}

func TestHistoryStaysEmptyWhenOffOrForAnExcludedHost(t *testing.T) {
	h := newHistoryWorld(t, false)
	h.prompt("$ ")
	h.type_("ls")
	h.type_("\r")
	if _, err := filepath.Glob(h.state.historyPath()); err != nil || h.state.history.store != nil {
		t.Fatal("the history file was opened while the history is off")
	}

	h.state.history.on = true
	h.state.sessions[0].host.NoHistory = true
	h.prompt("$ ")
	h.type_("pwd")
	h.type_("\r")
	if got := h.remembered(""); len(got) != 0 {
		t.Fatalf("an excluded host was remembered: %q", got)
	}
}

func TestExcludingAHostSurvivesARestart(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.selectedHostID = state.hosts[0].ID
	state.openEditHost()
	state.Click(hitDialogNoHistory)
	state.Click(hitDialogSubmit)
	if !state.hosts[0].NoHistory {
		t.Fatal("the switch was not saved")
	}
	again := New(state.options)
	defer again.Close()
	if !again.hosts[0].NoHistory {
		t.Fatal("the choice did not survive a restart")
	}
}

// pickerWorld is a history with a few commands, the picker open on the session.
func pickerWorld(t *testing.T) *historyWorld {
	h := newHistoryWorld(t, true)
	store := h.state.historyStore()
	base := time.Now()
	key := historyKey(&h.state.sessions[0])
	for index, command := range []string{"go test ./...", "git status", "grep -rn gst src", "docker ps"} {
		if err := store.Record(key, command, base.Add(time.Duration(index)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Record("another-host", "kubectl get pods", base.Add(10*time.Second)); err != nil {
		t.Fatal(err)
	}
	return h
}

func (h *historyWorld) search(query string) {
	app.TextEvent{Kind: app.TextInsert, Text: query}.Apply(h.state.EditorFor(hitHistorySearch))
	h.state.Paint(&ui.Canvas{}, ui.Size{Width: 1400, Height: 900})
}

func (h *historyWorld) listed() []string {
	var commands []string
	for _, index := range h.state.history.ranked {
		commands = append(commands, h.state.history.entries[index].Command)
	}
	return commands
}

func TestHistoryPickerRanksFuzzyMatchesAndInsertsWithoutEnter(t *testing.T) {
	h := pickerWorld(t)
	state := h.state
	// The palette and the terminal's shortcut both lead to the picker.
	state.Click(hitCommandPalette)
	app.TextEvent{Kind: app.TextInsert, Text: state.messages.History.Title}.Apply(state.EditorFor(hitCommandPaletteSearch))
	state.refreshCommandPalette()
	state.Key(ui.KeyEvent{Target: hitCommandPaletteSearch, Key: ui.KeyEnter})
	if state.modal != modalHistory {
		t.Fatalf("modal = %d", state.modal)
	}
	if got := strings.Join(h.listed(), "|"); got != "docker ps|grep -rn gst src|git status|go test ./..." {
		t.Fatalf("an empty query lists %q, want the most recent first", got)
	}

	h.search("gst")
	if got := h.listed(); len(got) != 3 || got[0] != "git status" || got[1] != "grep -rn gst src" || got[2] != "go test ./..." {
		t.Fatalf("gst lists %q, want the word-start match, then the contiguous one, then the scattered one", got)
	}

	before := len(h.shell.input)
	result := state.Key(ui.KeyEvent{Target: hitHistorySearch, Key: ui.KeyEnter})
	if !result.Handled || state.modal != modalNone {
		t.Fatalf("enter: %+v modal=%d", result, state.modal)
	}
	if got := string(h.shell.input[before:]); got != "git status" {
		t.Fatalf("typed %q, want the command alone, without a newline", got)
	}
}

func TestHistoryPickerShiftEnterRunsAndScopeWidensToAllHosts(t *testing.T) {
	h := pickerWorld(t)
	state := h.state
	state.openHistory(&state.sessions[0])
	h.search("kube")
	if len(h.listed()) != 0 {
		t.Fatalf("another host's command is listed for this host: %q", h.listed())
	}
	state.Click(hitHistoryScopeAll)
	if got := h.listed(); len(got) != 1 || got[0] != "kubectl get pods" {
		t.Fatalf("all hosts lists %q", got)
	}
	before := len(h.shell.input)
	state.Key(ui.KeyEvent{Target: hitHistorySearch, Key: ui.KeyEnter, Modifiers: ui.ModifierShift})
	if got := string(h.shell.input[before:]); got != "kubectl get pods\r" {
		t.Fatalf("typed %q", got)
	}
}

func TestHistoryPickerDeleteForgetsTheSelectedCommand(t *testing.T) {
	h := pickerWorld(t)
	state := h.state
	state.openHistory(&state.sessions[0])
	h.search("g")
	if first := h.listed()[0]; first == "" {
		t.Fatal("nothing listed")
	}
	selected, _ := state.historySelection()
	// In the search field Delete edits the text; on a row it removes the entry.
	if result := state.Key(ui.KeyEvent{Target: hitHistorySearch, Key: ui.KeyDelete}); result.Handled {
		t.Fatal("Delete in a filled search field was taken from the text")
	}
	if result := state.Key(ui.KeyEvent{Target: hitHistoryItems.At(0), Key: ui.KeyDelete}); !result.Handled {
		t.Fatal("Delete on a row did nothing")
	}
	for _, command := range h.remembered("") {
		if command == selected.Command {
			t.Fatalf("%q is still in the history", command)
		}
	}
	for _, command := range h.listed() {
		if command == selected.Command {
			t.Fatalf("%q is still listed", command)
		}
	}
}
