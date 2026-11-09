package workbench

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"motty/internal/cmdhistory"
)

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
	if got := s.completionCandidates(w.session, "customcmd "); len(got) > 0 {
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
	if got := s.completionCandidates(w.session, "customcmd "); len(got) == 0 {
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
	if got := b.state.completionCandidates(b.session, "customcmd "); len(got) > 0 {
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
