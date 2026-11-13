package workbench

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"motty/internal/completion"
	"motty/internal/pathcomp"
)

// pathWorld is a live session with a trusted prompt, a known directory and a
// server whose files are a fixed listing.
func newPathWorld(t *testing.T) (*completionWorld, *workbenchSFTPRemote) {
	t.Helper()
	w := newCompletionWorld(t)
	remote := &workbenchSFTPRemote{
		cwd: "/home/test",
		entries: map[string][]os.FileInfo{
			"/home/test": {
				workbenchFileInfo{name: "notes.txt", size: 11, mode: 0o644, when: time.Unix(1_700_000_000, 0)},
				workbenchFileInfo{name: "docs", mode: os.ModeDir | 0o755, when: time.Unix(1_700_000_100, 0)},
				workbenchFileInfo{name: ".hidden", size: 1, mode: 0o644, when: time.Unix(1_700_000_150, 0)},
			},
			"/home/test/docs": {
				workbenchFileInfo{name: "guide.md", size: 7, mode: 0o644, when: time.Unix(1_700_000_200, 0)},
			},
		},
		files: map[string][]byte{},
	}
	w.conn.sftp = remote
	w.session.startDirectory = "/home/test"
	// Automatic-list mode: these tests are about the list opening on its own.
	w.state.history.inlineOff = true
	return w, remote
}

// waitCompletionList pumps the window until the list opens. The directory read
// runs on the session's own worker, so the test has to let it land.
func waitCompletionList(t *testing.T, w *completionWorld) {
	t.Helper()
	deadline := time.Now().Add(waitBudget())
	for time.Now().Before(deadline) {
		w.state.Wake()
		if list := &w.state.completion.list; list.active && !list.loading {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("the completion list did not open: loading=%v candidates=%d",
		w.state.completion.list.loading, len(w.state.completion.list.candidates))
}

func listTexts(w *completionWorld) []string {
	texts := make([]string, len(w.state.completion.list.candidates))
	for index, candidate := range w.state.completion.list.candidates {
		texts[index] = candidate.Text
	}
	return texts
}

func TestPathCompletionOpensAutomaticallyAndOffersTheDirectory(t *testing.T) {
	w, _ := newPathWorld(t)
	w.type_("cd ")
	waitCompletionList(t, w)
	if !w.state.completion.list.auto {
		t.Fatal("the list was not the automatic one")
	}
	got := strings.Join(listTexts(w), ",")
	if got != "docs/,notes.txt" {
		t.Fatalf("candidates = %q, want the directory first and the hidden file left out", got)
	}
	if w.state.completion.list.candidates[0].Kind != completion.KindPath {
		t.Fatalf("candidate kind = %v", w.state.completion.list.candidates[0].Kind)
	}
	if w.state.completion.list.selected != -1 {
		t.Fatalf("the automatic list preselected row %d", w.state.completion.list.selected)
	}
}

// A press chooses a row; it must not move keyboard focus off the terminal, or
// the next keystroke would go nowhere.
func TestCompletionRowsKeepTheTerminalFocus(t *testing.T) {
	w, _ := newPathWorld(t)
	w.type_("cd ")
	waitCompletionList(t, w)
	var canvas gfx.Canvas
	w.state.Paint(&canvas, gfx.Size{Width: 900, Height: 600})
	hit, ok := hitByID(&canvas, hitCompletionList.At(0))
	if !ok {
		t.Fatal("the first row is not painted")
	}
	if hit.Focusable {
		t.Fatal("a press on a completion row would take the terminal's focus")
	}
}

func TestPathCompletionOpensBeforeTheSpace(t *testing.T) {
	w, _ := newPathWorld(t)
	w.type_("cd")
	waitCompletionList(t, w)
	got := strings.Join(listTexts(w), ",")
	if got != "docs/,notes.txt" {
		t.Fatalf("candidates = %q", got)
	}
	before := w.shell.typed()
	w.state.Click(hitCompletionList.At(0))
	if got := w.shell.typed(); got != before+" docs/" {
		t.Fatalf("accepted %q, want the separator and the folder", got[len(before):])
	}
}

func TestCompletionAutoOpensForACommandArgument(t *testing.T) {
	w, _ := newPathWorld(t)
	w.type_("git s")
	waitCompletionList(t, w)
	list := &w.state.completion.list
	if !list.auto {
		t.Fatal("the automatic list did not open for a command argument")
	}
	for _, candidate := range list.candidates {
		if candidate.Kind == completion.KindPath {
			t.Fatalf("a command argument offered a path: %+v", candidate)
		}
	}
}

func TestPathCompletionInsertsOnlyTheUnfinishedSuffix(t *testing.T) {
	w, _ := newPathWorld(t)
	w.type_("cat no")
	waitCompletionList(t, w)
	if texts := listTexts(w); len(texts) != 1 || texts[0] != "notes.txt" {
		t.Fatalf("candidates = %v", texts)
	}
	before := w.shell.typed()
	w.state.Click(hitCompletionList.At(0))
	if got := w.shell.typed(); got != before+"tes.txt" {
		t.Fatalf("typed %q, want the missing suffix only", got[len(before):])
	}
}

func TestPathCompletionEnterRunsWhatWasTyped(t *testing.T) {
	w, _ := newPathWorld(t)
	w.type_("cd ")
	waitCompletionList(t, w)
	before := w.shell.typed()
	w.state.Key(gfx.KeyEvent{Key: gfx.KeyEnter, Target: terminalHit(w.state, 0)})
	if got := w.shell.typed(); got != before+"\r" {
		t.Fatalf("Enter typed %q; it must run the line, not accept the first row", got[len(before):])
	}
	if w.state.completion.list.active {
		t.Fatal("the list stayed open")
	}
}

func TestPathCompletionAcceptanceHoldsTheMenuUntilTheNextKey(t *testing.T) {
	w, _ := newPathWorld(t)
	w.type_("cd ")
	waitCompletionList(t, w)
	w.state.Click(hitCompletionList.At(0))
	if got := w.shell.typed(); !strings.HasSuffix(got, "docs/") {
		t.Fatalf("typed %q", got)
	}
	// The echo of the accepted suffix must not reopen the menu: the next Enter
	// runs `cd docs/`, it is not swallowed by a fresh list.
	for index := 0; index < 10; index++ {
		w.state.Wake()
		time.Sleep(time.Millisecond)
	}
	if w.state.completion.list.active {
		t.Fatal("the menu reopened on its own echo")
	}
	before := w.shell.typed()
	w.state.Key(gfx.KeyEvent{Key: gfx.KeyEnter, Target: terminalHit(w.state, 0)})
	if got := w.shell.typed(); got != before+"\r" {
		t.Fatalf("the second Enter typed %q", got[len(before):])
	}
}

func TestPathCompletionEscapeDismissesTheSameDirectory(t *testing.T) {
	w, _ := newPathWorld(t)
	w.type_("cd ")
	waitCompletionList(t, w)
	w.state.Key(gfx.KeyEvent{Key: gfx.KeyEscape, Target: terminalHit(w.state, 0)})
	if w.state.completion.list.active {
		t.Fatal("Escape left the list open")
	}
	// Typing more of the same argument, in the same directory, stays quiet.
	w.type_("do")
	for index := 0; index < 10; index++ {
		w.state.Wake()
		time.Sleep(time.Millisecond)
	}
	if w.state.completion.list.active {
		t.Fatal("the dismissed directory reopened")
	}
	// A new prompt is a new context. Its report ends with a new input guard
	// epoch, which the dismissal is scoped to.
	w.session.term.inputGuard.epoch++
	waitCompletionList(t, w)
}

func TestPathCompletionExplicitOpenShowsLoadingAndPaths(t *testing.T) {
	w, remote := newPathWorld(t)
	// Hold the read inside the server so the loading row is observable
	// without depending on timing.
	release := make(chan struct{})
	remote.onRead = func(string) { <-release }
	w.type_("cat ")
	w.state.closeCompletionList()
	w.state.openCompletions()
	if !w.state.completion.list.active || !w.state.completion.list.loading {
		t.Fatalf("explicit open = active %v loading %v", w.state.completion.list.active, w.state.completion.list.loading)
	}
	close(release)
	waitCompletionList(t, w)
	if w.state.completion.list.loading {
		t.Fatal("the list stayed in its loading state")
	}
	got := strings.Join(listTexts(w), ",")
	if !strings.Contains(got, "notes.txt") || !strings.Contains(got, "docs/") {
		t.Fatalf("candidates = %q", got)
	}
}

func TestPathCompletionUnreadableDirectoryFailsQuietly(t *testing.T) {
	w, remote := newPathWorld(t)
	remote.unreadable = map[string]bool{"/home/test": true}
	w.type_("cd ")
	for index := 0; index < 20; index++ {
		w.state.Wake()
		time.Sleep(time.Millisecond)
	}
	if w.state.completion.list.active {
		t.Fatal("an unreadable directory opened a menu")
	}
	if len(remote.reads) > 1 {
		t.Fatalf("the directory was read %d times for one prompt", len(remote.reads))
	}
}

func TestPathCompletionClosesItsReaderWithTheSession(t *testing.T) {
	w, remote := newPathWorld(t)
	w.type_("cd ")
	waitCompletionList(t, w)
	w.state.closeCompletionPath(w.session)
	if w.state.completion.list.active {
		t.Fatal("the list survived its session's reader")
	}
	deadline := time.Now().Add(waitBudget())
	for !remote.closed && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !remote.closed {
		t.Fatal("the SFTP channel stayed open")
	}
}

func TestPathSuggestionRevalidatesTheEscapedAlphabet(t *testing.T) {
	w, _ := newPathWorld(t)
	w.type_("cat ")
	before := w.shell.typed()
	w.state.sendPathSuggestion(w.session, "a b")
	if w.shell.typed() != before {
		t.Fatal("a raw space reached the shell")
	}
	w.state.sendPathSuggestion(w.session, `a\ b`)
	if got := w.shell.typed(); got != before+`a\ b` {
		t.Fatalf("escaped suffix typed %q", got[len(before):])
	}
	// The one separator a bare command needs is supplied by the completion; a
	// second raw space is still refused.
	before = w.shell.typed()
	w.state.sendPathSuggestion(w.session, ` docs/`)
	if got := w.shell.typed(); got != before+` docs/` {
		t.Fatalf("separator suffix typed %q", got[len(before):])
	}
	before = w.shell.typed()
	w.state.sendPathSuggestion(w.session, ` a b`)
	if w.shell.typed() != before {
		t.Fatal("a raw space after the separator reached the shell")
	}
}

func TestLocalPathDirectorySortsFoldersFirstAndListsNames(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := readLocalPathDirectory(filepath.ToSlash(dir), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Name != "docs" || !entries[0].Dir || entries[1].Name != "notes.txt" || entries[1].Dir {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestPathCandidatesKeepWhatTheTypedBaseMatches(t *testing.T) {
	ctx, ok := pathcomp.Parse("cat ~/no")
	if !ok || !ctx.Path || !ctx.Home {
		t.Fatalf("home line parsed as %+v ok=%v", ctx, ok)
	}
	candidates := pathcomp.Candidates(ctx, []pathcomp.Entry{{Name: "notes.txt"}, {Name: "other"}})
	if len(candidates) != 1 || candidates[0].Insert != "tes.txt" || candidates[0].Text != "~/notes.txt" {
		t.Fatalf("candidates = %+v", candidates)
	}
}
