package workbench

import (
	"bytes"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"motty/internal/cmdhistory"
	"motty/internal/term"
)

func TestConsoleInputKeepsWireBytesSeparateFromLogicalInput(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	var observed []byte
	session.term.completionInput = func(input []byte) { observed = append(observed, input...) }
	text := []byte("\x1b[65;30;97;1;0;1000000_tail")
	if !session.term.send(text) || !bytes.Equal(observed, text) || !bytes.HasSuffix(shell.input, text) {
		t.Fatalf("literal input: observed=%q shell=%q", observed, shell.input)
	}
	observed = nil
	epoch := session.term.inputGuard.epoch
	event := gfx.KeyEvent{Key: gfx.KeyEnter, Win32: gfx.Win32Key{VirtualKey: 13, Unicode: 13}}
	wire := term.EncodeWin32Key(event)
	if !state.typedWin32Key(session, wire, term.Win32LogicalKey(event), true) {
		t.Fatal("console input was not sent")
	}
	if !bytes.Equal(observed, []byte("\r")) || !bytes.HasSuffix(shell.input, wire) || session.term.inputGuard.epoch != epoch+1 {
		t.Fatalf("console input: observed=%q shell=%q epoch=%d", observed, shell.input, session.term.inputGuard.epoch)
	}
}

func TestCompletionOnlyConsumesReleasesOfAcceptedKeys(t *testing.T) {
	w := newCompletionWorld(t)
	w.type_("git s")
	w.state.openCompletions()
	if !w.state.completion.list.active {
		t.Fatal("completion list did not open")
	}
	terminal := w.session.term
	terminal.acceptedKeys = map[acceptedKey]struct{}{
		{key: gfx.KeyUp}:   {},
		{key: gfx.KeyDown}: {},
	}
	if _, handled := w.state.completionKey(gfx.KeyEvent{Key: gfx.KeyControlLeft, Release: true}); handled {
		t.Fatal("release of a key sent to the shell was consumed")
	}
	for _, key := range []gfx.Key{gfx.KeyUp, gfx.KeyDown} {
		if _, handled := w.state.completionKey(gfx.KeyEvent{Key: key, Release: true}); !handled {
			t.Fatalf("accepted %v release reached the shell", key)
		}
	}
	if len(terminal.acceptedKeys) != 0 {
		t.Fatal("accepted keys were not cleared")
	}
}

func TestUnrepresentableConsoleInputInvalidatesTheKnownLine(t *testing.T) {
	w := newCompletionWorld(t)
	w.type_("git s")
	w.state.openCompletions()
	w.session.term.inputGuard.active = true
	w.session.term.inputGuard.known = true
	event := gfx.KeyEvent{Win32: gfx.Win32Key{VirtualKey: 'A', Unicode: 0xd83d}}
	wire := term.EncodeWin32Key(event)
	if !w.state.typedWin32Key(w.session, wire, term.Win32LogicalKey(event), true) {
		t.Fatal("console input was not sent")
	}
	if w.session.term.inputGuard.known || !w.session.term.autofill.HasInput() || w.state.completion.list.active || !w.session.term.promptDirty {
		t.Fatalf("unknown input left stale state: guard=%v autofill=%v completion=%v promptDirty=%v", w.session.term.inputGuard.known, w.session.term.autofill.HasInput(), w.state.completion.list.active, w.session.term.promptDirty)
	}
	if !bytes.HasSuffix(w.shell.input, wire) {
		t.Fatalf("shell did not receive the console record: %q", w.shell.input)
	}
}

func TestCompletionRegressionSensitiveLineHasNoStaticSuggestions(t *testing.T) {
	w := newCompletionWorld(t)
	w.type_("git -c access_key=abc --ver")
	w.state.openCompletions()
	if w.state.completion.list.active {
		t.Fatalf("recognized sensitive command still opens list: %+v", w.state.completion.list.candidates)
	}
	if rest, ok := w.state.commandGhost(w.session); ok {
		t.Fatalf("recognized sensitive command still has ghost: %q", rest)
	}
}

func TestCompletionRegressionDeletedHistoryDisappearsFromCompletion(t *testing.T) {
	w := newCompletionWorld(t)
	s := w.state
	s.history.on = true
	key := historyKey(w.session)
	command := "customcmd private-argument"
	store := s.historyStore()
	if err := store.Record(key, command, time.Now()); err != nil {
		t.Fatal(err)
	}
	entries, err := store.List(key)
	if err != nil {
		t.Fatal(err)
	}
	s.history.ghost = map[string][]cmdhistory.Entry{key: entries}
	s.openHistory(w.session)
	s.deleteHistorySelection()
	s.closeHistoryPicker()
	entries, err = store.List(key)
	if err != nil || len(entries) != 0 {
		t.Fatalf("disk history not deleted: %v %v", entries, err)
	}
	if got, _ := s.completionCandidates(w.session, "customcmd "); len(got) > 0 {
		t.Fatalf("deleted history remains suggested: %+v", got)
	}
}

func TestCompletionRegressionNewHistoryIsAvailableToCompletion(t *testing.T) {
	w := newCompletionWorld(t)
	s := w.state
	s.history.on = true
	key := historyKey(w.session)
	s.history.ghost = map[string][]cmdhistory.Entry{key: nil}
	s.rememberCommand(w.session.ID, "customcmd fresh")
	if got, _ := s.completionCandidates(w.session, "customcmd "); len(got) == 0 {
		t.Fatal("newly remembered command missing from cached completion")
	}
}

func TestCompletionRegressionClearInvalidatesOtherWindowCompletion(t *testing.T) {
	a := newCompletionWorld(t)
	b := newCompletionWorld(t)
	group := NewGroup()
	a.state.options.Group = group
	b.state.options.Group = group
	group.join(a.state)
	group.join(b.state)
	path := filepath.Join(t.TempDir(), "shared-history.db")
	a.state.options.HistoryPath = path
	b.state.options.HistoryPath = path
	a.state.history.on = true
	b.state.history.on = true
	key := historyKey(b.session)
	command := "customcmd old"
	store := a.state.historyStore()
	if err := store.Record(key, command, time.Now()); err != nil {
		t.Fatal(err)
	}
	entries, err := store.List(key)
	if err != nil {
		t.Fatal(err)
	}
	b.state.history.ghost = map[string][]cmdhistory.Entry{key: entries}
	a.state.askClearHistory()
	if msg := a.state.form.run(a.state, &a.state.form); msg != "" {
		t.Fatal(msg)
	}
	select {
	case <-a.state.history.clear.done:
		if err := a.state.history.clear.err; err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("clear timeout")
	}
	a.state.adoptHistoryClear()
	if got, _ := b.state.completionCandidates(b.session, "customcmd "); len(got) > 0 {
		t.Fatalf("other window still suggests cleared history: %+v", got)
	}
}

func TestCompletionRegressionClearCompletionWakesAnIdleWindow(t *testing.T) {
	w := newCompletionWorld(t)
	s := w.state
	s.historyStore()
	completedWake := make(chan struct{}, 1)
	s.options.Invalidate = func() {
		job := s.history.clear
		if job == nil {
			return
		}
		select {
		case <-job.done:
			select {
			case completedWake <- struct{}{}:
			default:
			}
		default:
		}
	}
	s.askClearHistory()
	if msg := s.form.run(s, &s.form); msg != "" {
		t.Fatal(msg)
	}
	select {
	case <-completedWake:
	case <-time.After(3 * time.Second):
		t.Fatal("background clear did not wake the idle window")
	}
	s.adoptHistoryClear()
	if s.status != s.messages.History.StatusCleared {
		t.Fatal("completion was not adopted")
	}

	s.history.clear = nil
}

func TestCompletionRegressionStaticGhostDrainsBeforeAccepting(t *testing.T) {
	w := newCompletionWorld(t)
	w.type_("git statu")
	if rest, ok := w.state.commandGhost(w.session); !ok || rest != "s" {
		t.Fatalf("no expected ghost: %q %v", rest, ok)
	}
	before := w.shell.typed()
	w.shell.say("\x1b]133;C;motty=" + w.session.term.folderToken + "\x07")
	w.state.Key(gfx.KeyEvent{Key: gfx.KeyRight, Target: terminalHit(w.state, 0)})
	if got := w.shell.typed(); got != before {
		t.Fatalf("stale static suffix reached running program: %q", got[len(before):])
	}
}

func TestCompletionRepeatedEnterDoesNotExecuteWhileListIsOpen(t *testing.T) {
	w := newCompletionWorld(t)
	w.type_("git s")
	w.state.openCompletions()
	before := w.shell.typed()
	w.state.Key(gfx.KeyEvent{Key: gfx.KeyEnter, Repeat: true, Target: terminalHit(w.state, 0)})
	if got := w.shell.typed(); got != before {
		t.Fatalf("repeated Enter sent %q", got[len(before):])
	}
}

func TestCompletionListRetiresWhenIMECompositionBegins(t *testing.T) {
	w := newCompletionWorld(t)
	w.type_("git s")
	w.state.openCompletions()
	w.session.term.preedit.Composing = true
	w.state.completionKey(gfx.KeyEvent{Key: gfx.KeyDown})
	if w.state.completion.list.active {
		t.Fatal("list retained navigation while IME was composing")
	}
}

func TestCompletionUpdateWaitsForItsBaseline(t *testing.T) {
	w := newCompletionWorld(t)
	w.state.completion.catalog = nil
	w.state.completion.catalogLoading = true
	w.state.completion.catalogReady = make(chan completionCatalogLoad, 1)
	w.state.updateCompletions()
	if w.state.completion.updating {
		t.Fatal("update started without its version baseline")
	}
}

func TestCompletionWaitsForAcceptedTextToEcho(t *testing.T) {
	w := newCompletionWorld(t)
	w.type_("git statu")
	w.shell.onWrite = nil
	before := w.shell.typed()
	if !w.state.acceptGhost(w.session, false) {
		t.Fatal("first suggestion was absent")
	}
	w.state.acceptGhost(w.session, false)
	if got := w.shell.typed(); got != before+"s" {
		t.Fatalf("unacknowledged suffix was sent twice: %q", got[len(before):])
	}
	w.state.openCompletions()
	if w.state.completion.list.active {
		t.Fatal("list reopened on the unacknowledged screen")
	}
	w.shell.say("s")
	w.state.Wake()
	w.state.openCompletions()
	if w.state.completion.list.active {
		t.Fatal("complete command retained a stale suggestion")
	}
}
