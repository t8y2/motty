package workbench

import (
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/ui"
	"motty/internal/cmdhistory"
	"motty/internal/completion"
)

// completionWorld is a live SSH terminal with shell integration: the prompt
// carries the OSC 133 markers the recorder needs, the shell echoes typing, and
// the clock moves so the recorder's quiet window passes.
type completionWorld struct {
	state   *State
	shell   *fakeShell
	session *Session
	clock   time.Duration
}

func newCompletionWorld(t *testing.T) *completionWorld {
	t.Helper()
	state, _, _, shell := liveSession(t)
	w := &completionWorld{state: state, shell: shell, clock: time.Second}
	state.options.Now = func() time.Duration { return w.clock }
	shell.onWrite = func(p []byte) {
		shell.say(strings.ReplaceAll(string(p), "\r", "\r\n"))
	}
	session := &state.sessions[0]
	if !state.ActivatePane(session.ID) {
		t.Fatal("the session has no pane")
	}
	inputGuardPrompt(state, session, shell, "utf8", "")
	catalog, err := completion.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	state.completion.catalog = catalog
	w.session = &state.sessions[0]
	return w
}

// tick passes the recorder's quiet window, as typing after a prompt does.
func (w *completionWorld) tick() { w.clock += 100 * time.Millisecond }

func (w *completionWorld) type_(keys string) {
	w.tick()
	w.state.typed(w.session, []byte(keys))
	w.state.Wake()
}

func (w *completionWorld) line(t *testing.T) string {
	t.Helper()
	line, ok := w.session.term.recorder.Line(cmdhistory.TermScreen{Term: w.session.term.screen})
	if !ok {
		t.Fatal("the recorder cannot confirm the line")
	}
	return line
}

func TestCompletionListInsertsOnlyTheSuffix(t *testing.T) {
	w := newCompletionWorld(t)
	w.type_("git s")
	if got := w.line(t); got != "git s" {
		t.Fatalf("line %q", got)
	}
	w.state.openCompletions()
	list := &w.state.completion.list
	if !list.active || len(list.candidates) < 3 {
		t.Fatalf("list active=%v candidates=%v", list.active, list.candidates)
	}
	if list.candidates[0].Text != "stage" || list.candidates[0].Insert != "tage" {
		t.Fatalf("first candidate %+v", list.candidates[0])
	}
	before := w.shell.typed()
	w.state.Key(ui.KeyEvent{Key: ui.KeyEnter, Target: terminalHit(w.state, 0)})
	if got := w.shell.typed(); got != before+"tage" {
		t.Fatalf("typed %q, want %q", got, before+"tage")
	}
	if strings.ContainsAny(w.shell.typed(), "\r\n") {
		t.Fatal("accepting executed the command")
	}
	if w.state.completion.list.active {
		t.Fatal("the list stayed open")
	}
}

func TestCompletionKeysCloseOrMoveWithoutTyping(t *testing.T) {
	w := newCompletionWorld(t)
	w.type_("git s")
	w.state.openCompletions()
	if !w.state.completion.list.active {
		t.Fatal("the list did not open")
	}
	before := w.shell.typed()

	// Up and Down move the selection and never reach the shell.
	w.state.Key(ui.KeyEvent{Key: ui.KeyDown, Target: terminalHit(w.state, 0)})
	if w.state.completion.list.selected != 1 {
		t.Fatalf("selected %d", w.state.completion.list.selected)
	}
	w.state.Key(ui.KeyEvent{Key: ui.KeyUp, Target: terminalHit(w.state, 0)})
	if w.state.completion.list.selected != 0 || w.shell.typed() != before {
		t.Fatal("movement typed or did not move")
	}

	// Tab closes the list and forwards exactly one Tab to the shell.
	w.state.Key(ui.KeyEvent{Key: ui.KeyTab, Target: terminalHit(w.state, 0)})
	if w.state.completion.list.active {
		t.Fatal("Tab left the list open")
	}
	if got := w.shell.typed(); got != before+"\t" {
		t.Fatalf("Tab typed %q", got)
	}

	// Escape closes it without sending anything.
	w.state.openCompletions()
	if !w.state.completion.list.active {
		t.Fatal("the list did not reopen")
	}
	before = w.shell.typed()
	w.state.Key(ui.KeyEvent{Key: ui.KeyEscape, Target: terminalHit(w.state, 0)})
	if w.state.completion.list.active || w.shell.typed() != before {
		t.Fatal("Escape typed or left the list open")
	}
}

func TestCompletionTypingClosesTheList(t *testing.T) {
	w := newCompletionWorld(t)
	w.type_("git s")
	w.state.openCompletions()
	w.type_("t")
	if w.state.completion.list.active {
		t.Fatal("typing left the list open")
	}
	if got := w.shell.typed(); !strings.HasSuffix(got, "git st") {
		t.Fatalf("typed %q", got)
	}
}

func TestCompletionAcceptanceRechecksTheSnapshot(t *testing.T) {
	w := newCompletionWorld(t)
	w.type_("git s")
	w.state.openCompletions()
	before := w.shell.typed()
	w.state.completion.list.version++ // a prompt change the list did not see
	w.state.Click(hitCompletionList.At(0))
	if got := w.shell.typed(); got != before {
		t.Fatalf("a stale acceptance typed %q", got)
	}
	if w.state.completion.list.active {
		t.Fatal("a stale acceptance left the list open")
	}
}

func TestCompletionNeedsAConfirmedPrompt(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	state.ActivatePane(session.ID)
	if state.completionEligible(session) {
		t.Fatal("completion was eligible without shell integration")
	}
	state.typed(session, []byte("git s"))
	state.Wake()
	state.openCompletions()
	if state.completion.list.active || shell.typed() != "git s" {
		t.Fatal("completion opened without a confirmed prompt")
	}
}

func TestCompletionGhostUsesTheCatalogOnlyWhenUnique(t *testing.T) {
	w := newCompletionWorld(t)
	w.type_("git statu")
	rest, ok := w.state.commandGhost(w.session)
	if !ok || rest != "s" {
		t.Fatalf("ghost %q ok=%v", rest, ok)
	}
	w.session.host.NoHistory = true
	if _, ok := w.state.commandGhost(w.session); ok {
		t.Fatal("NoHistory still offered a ghost")
	}
	other := newCompletionWorld(t)
	other.type_("git chec")
	if _, ok := other.state.commandGhost(other.session); ok {
		t.Fatal("an ambiguous prefix offered a ghost")
	}
}

func TestCompletionHistoryMergesFirstOnlyWhileLearning(t *testing.T) {
	w := newCompletionWorld(t)
	key := historyKey(w.session)
	w.state.history.ghost = map[string][]cmdhistory.Entry{
		key: {{Command: "git status --short", Used: time.Now(), Count: 2}},
	}
	w.state.history.on = true
	w.type_("git sta")
	w.state.openCompletions()
	list := &w.state.completion.list
	if !list.active || len(list.candidates) == 0 {
		t.Fatal("no candidates")
	}
	if list.candidates[0].Text != "git status --short" || list.candidates[0].Insert != "tus --short" {
		t.Fatalf("history did not come first: %+v", list.candidates[0])
	}
	w.state.closeCompletionList()
	w.state.history.on = false
	w.state.openCompletions()
	if w.state.completion.list.active {
		for _, candidate := range w.state.completion.list.candidates {
			if candidate.Text == "git status --short" {
				t.Fatal("the history was read while learning is off")
			}
		}
	}
}

func TestCompletionListPaintsItsRowsAndClicksInsert(t *testing.T) {
	w := newCompletionWorld(t)
	w.type_("git s")
	w.state.openCompletions()
	list := &w.state.completion.list
	if !list.active || len(list.candidates) < 2 {
		t.Fatalf("list active=%v candidates=%d", list.active, len(list.candidates))
	}
	var canvas ui.Canvas
	w.state.Paint(&canvas, ui.Size{Width: 900, Height: 600})
	if _, ok := hitByID(&canvas, hitCompletionList.At(0)); !ok {
		t.Fatal("the first row is not painted")
	}
	if _, ok := hitByID(&canvas, hitCompletionList.At(len(list.candidates))); ok {
		t.Fatal("a row past the candidates is painted")
	}
	insert := list.candidates[1].Insert
	before := w.shell.typed()
	w.state.Click(hitCompletionList.At(1))
	if got := w.shell.typed(); got != before+insert {
		t.Fatalf("click typed %q, want %q", got, before+insert)
	}
	if w.state.completion.list.active {
		t.Fatal("the list stayed open after a click")
	}
}

func TestCompletionListPaintsInANarrowWindow(t *testing.T) {
	w := newCompletionWorld(t)
	w.type_("git ")
	w.state.openCompletions()
	if !w.state.completion.list.active {
		t.Fatal("the list did not open")
	}
	var canvas ui.Canvas
	w.state.Paint(&canvas, ui.Size{Width: 320, Height: 240})
	if _, ok := hitByID(&canvas, hitCompletionList.At(0)); !ok {
		t.Fatal("the first row is not painted in a narrow window")
	}
}

func TestCompletionCommonMatrixRoots(t *testing.T) {
	w := newCompletionWorld(t)
	groups := []string{
		"cat ls cd cp mv rm mkdir chmod chown grep sed awk find head tail sort uniq cut wc tee xargs du df tar gzip",
		"ssh scp rsync curl wget ping ip ss netstat ps kill top systemctl journalctl apt dnf yum",
		"git docker kubectl make gcc go cargo rustc java javac npm yarn pnpm node python pip brew",
		"pwsh powershell cmd dir Get-Command Get-ChildItem",
	}
	for _, group := range groups {
		for _, name := range strings.Fields(group) {
			prefix := name[:len(name)-1]
			candidates := w.state.completionCandidates(w.session, prefix)
			found := false
			for _, candidate := range candidates {
				if candidate.Text == name && candidate.FromCatalog && completion.SafeInsert(candidate.Insert) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("no safe root candidate for %q from %q", name, prefix)
			}
		}
	}
}

func TestCompletionCommonOptionsAndDescriptions(t *testing.T) {
	w := newCompletionWorld(t)
	for _, test := range []struct {
		line string
		want []string
	}{
		{"cat -", []string{"-b", "-e", "-n", "-s", "-t", "-u", "-v"}},
		{"ls -", []string{"-a", "-l", "-h"}},
		{"kill -", []string{"-s", "-l"}},
		{"ip ", []string{"addr", "link", "route"}},
		{"ss --", []string{"--all", "--listening", "--numeric"}},
		{"netstat --", []string{"--tcp", "--udp", "--listening"}},
	} {
		t.Run(test.line, func(t *testing.T) {
			candidates := w.state.completionCandidates(w.session, test.line)
			for _, want := range test.want {
				found := false
				for _, candidate := range candidates {
					if candidate.Text == want && candidate.Desc != "" && candidate.FromCatalog && completion.SafeInsert(candidate.Insert) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("%q lacks safe described %q: %+v", test.line, want, candidates)
				}
			}
		})
	}
}

func TestCompletionCommonSurvivesPublicCatalogReplacement(t *testing.T) {
	w := newCompletionWorld(t)
	w.state.completion.catalog = completionTestCatalog(t, 120, 0)
	candidates := w.state.completionCandidates(w.session, "cat -")
	for _, candidate := range candidates {
		if candidate.Text == "-n" && candidate.FromCatalog {
			return
		}
	}
	t.Fatalf("the common cat options vanished after replacing the public catalog: %+v", candidates)
}

func TestCompletionCommonUniqueGhost(t *testing.T) {
	w := newCompletionWorld(t)
	w.type_("yum inst")
	if rest, ok := w.state.commandGhost(w.session); !ok || rest != "all" {
		t.Fatalf("supplemental command ghost = %q, ok=%v; want all", rest, ok)
	}
}
