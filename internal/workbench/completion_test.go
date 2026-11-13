package workbench

import (
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"motty/internal/cmdhistory"
	"motty/internal/completion"
	"motty/internal/i18n"
)

// completionWorld is a live SSH terminal with shell integration: the prompt
// carries the OSC 133 markers the recorder needs, the shell echoes typing, and
// the clock moves so the recorder's quiet window passes.
type completionWorld struct {
	state   *State
	shell   *fakeShell
	conn    *fakeConn
	session *Session
	clock   time.Duration
}

func newCompletionWorld(t *testing.T) *completionWorld {
	t.Helper()
	state, _, conn, shell := liveSession(t)
	w := &completionWorld{state: state, shell: shell, conn: conn, clock: time.Second}
	state.options.Now = func() time.Duration { return w.clock }
	// The world starts in inline mode, where the ghost is the offer. Tests of
	// the automatic list set the list mode themselves.
	state.history.suggestOff, state.history.inlineOff = false, false
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

// enterNestedShell plays the integrated login shell handing the terminal to a
// child that does not run the startup hooks (sudo, su, a nested ssh): the OSC
// 133 markers stop and only the child's screen prompt remains (#783).
func (w *completionWorld) enterNestedShell(prompt string) {
	w.type_("sudo -i\r")
	w.shell.say("\r\n" + prompt)
	w.state.Wake()
}

func TestTheShortcutStillOpensTheListInANestedShell(t *testing.T) {
	w := newCompletionWorld(t)
	w.enterNestedShell("root@host:~# ")
	w.type_("git s")
	if !w.state.completionEligible(w.session) {
		t.Fatal("the nested shell's line is not eligible")
	}
	w.state.openCompletions()
	list := &w.state.completion.list
	if !list.active || len(list.candidates) == 0 || list.candidates[0].Text != "stage" {
		t.Fatalf("list active=%v candidates=%v", list.active, list.candidates)
	}
}

func TestTheListStillOpensWhileTypingInANestedShell(t *testing.T) {
	w := newCompletionWorld(t)
	w.state.history.inlineOff = true // automatic-list mode
	w.enterNestedShell("root@host:~# ")
	w.type_("git s")
	waitCompletionList(t, w)
}

// Accepting in a nested shell still revalidates the line and types only the
// suffix: the input guard is unavailable there, but the recorder's proof is not.
func TestNestedShellAcceptanceTypesOnlyTheSuffix(t *testing.T) {
	w := newCompletionWorld(t)
	w.enterNestedShell("root@host:~# ")
	w.type_("git s")
	w.state.openCompletions()
	list := &w.state.completion.list
	if !list.active || list.candidates[0].Insert != "tage" {
		t.Fatalf("list active=%v candidates=%v", list.active, list.candidates)
	}
	before := w.shell.typed()
	w.state.Click(hitCompletionList.At(0))
	if got := w.shell.typed(); got != before+"tage" {
		t.Fatalf("typed %q, want %q", got, before+"tage")
	}
}

// The ghost rests on the same prompt proof as the list: a child shell keeps it,
// a REPL does not.
func TestTheGhostFollowsTheNestedShellsPrompt(t *testing.T) {
	w := newCompletionWorld(t)
	w.state.history.on = true
	w.state.history.ghost = map[string][]cmdhistory.Entry{
		historyKey(w.session): {{Command: "git status"}},
	}
	w.enterNestedShell("root@host:~# ")
	w.type_("git s")
	if rest, ok := w.state.commandGhost(w.session); !ok || rest != "tatus" {
		t.Fatalf("the nested shell's ghost = %q, %v", rest, ok)
	}

	w = newCompletionWorld(t)
	w.state.history.on = true
	w.state.history.ghost = map[string][]cmdhistory.Entry{
		historyKey(w.session): {{Command: "git status"}},
	}
	w.enterNestedShell(">>> ")
	w.type_("git s")
	if rest, ok := w.state.commandGhost(w.session); ok {
		t.Fatalf("the REPL's ghost offered %q", rest)
	}
}

// A dismissal belongs to the line it happened on. In a nested shell no prompt
// marker ends that line, so the key that submits it must.
func TestANestedShellDismissalEndsWithTheLine(t *testing.T) {
	w := newCompletionWorld(t)
	w.state.history.inlineOff = true
	w.enterNestedShell("root@host:~# ")
	w.type_("git s")
	waitCompletionList(t, w)
	w.state.Key(gfx.KeyEvent{Key: gfx.KeyEscape, Target: terminalHit(w.state, 0)})
	if w.state.completion.list.active {
		t.Fatal("Escape left the list open")
	}
	w.type_("\r")
	w.shell.say("\r\nroot@host:~# ")
	w.state.Wake()
	w.type_("git s")
	waitCompletionList(t, w)
}

// A child shell does not report its directory, so the parent's last report
// must not supply paths the child cannot see. Commands are still offered.
func TestANestedShellOffersNoPaths(t *testing.T) {
	w, _ := newPathWorld(t)
	w.enterNestedShell("root@host:~# ")
	w.type_("cd ")
	for index := 0; index < 20; index++ {
		w.state.Wake()
		time.Sleep(time.Millisecond)
	}
	for _, candidate := range w.state.completion.list.candidates {
		if candidate.Kind == completion.KindPath {
			t.Fatalf("the nested shell offered the parent's path %q", candidate.Text)
		}
	}
}

func TestAProgramPromptOpensNeitherTheListNorTheGhost(t *testing.T) {
	w := newCompletionWorld(t)
	w.enterNestedShell(">>> ")
	w.type_("git statu")
	if w.state.completionEligible(w.session) {
		t.Fatal("a REPL prompt was taken for a shell prompt")
	}
	w.state.openCompletions()
	if w.state.completion.list.active {
		t.Fatal("the list opened at a REPL prompt")
	}
	if rest, ok := w.state.commandGhost(w.session); ok {
		t.Fatalf("the ghost offered %q at a REPL prompt", rest)
	}
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
	w.state.Key(gfx.KeyEvent{Key: gfx.KeyEnter, Target: terminalHit(w.state, 0)})
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

// The window's Config routes a terminal key through the application's own key
// order before the terminal encodes it, so the completion list still takes
// Enter. Declaring the terminal control with the terminal's encoder instead
// would send the key to the shell and execute the line.
func TestWindowConfigKeepsCompletionBeforeTheTerminal(t *testing.T) {
	w := newCompletionWorld(t)
	w.type_("git s")
	w.state.openCompletions()
	// Paint once so the page has declared the terminal control Config routes.
	var canvas gfx.Canvas
	w.state.Paint(&canvas, gfx.Size{Width: 1200, Height: 700})
	before := w.shell.typed()
	windowKeyRoutes(w.state, gfx.KeyEvent{Key: gfx.KeyEnter, Target: terminalHit(w.state, 0)})
	if got := w.shell.typed(); got != before+"tage" {
		t.Fatalf("typed %q, want %q", got, before+"tage")
	}
	if strings.ContainsAny(w.shell.typed(), "\r\n") {
		t.Fatal("the key reached the shell instead of the completion list")
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
	w.state.Key(gfx.KeyEvent{Key: gfx.KeyDown, Target: terminalHit(w.state, 0)})
	if w.state.completion.list.selected != 1 {
		t.Fatalf("selected %d", w.state.completion.list.selected)
	}
	w.state.Key(gfx.KeyEvent{Key: gfx.KeyUp, Target: terminalHit(w.state, 0)})
	if w.state.completion.list.selected != 0 || w.shell.typed() != before {
		t.Fatal("movement typed or did not move")
	}

	// Tab closes the list and forwards exactly one Tab to the shell.
	w.state.Key(gfx.KeyEvent{Key: gfx.KeyTab, Target: terminalHit(w.state, 0)})
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
	w.state.Key(gfx.KeyEvent{Key: gfx.KeyEscape, Target: terminalHit(w.state, 0)})
	if w.state.completion.list.active || w.shell.typed() != before {
		t.Fatal("Escape typed or left the list open")
	}
}

func TestCompletionTypingHandsTheListToTheAutomaticOne(t *testing.T) {
	w := newCompletionWorld(t)
	w.state.history.inlineOff = true // automatic-list mode
	w.type_("git s")
	w.state.openCompletions()
	if !w.state.completion.list.active || w.state.completion.list.auto {
		t.Fatal("the shortcut did not open its own list")
	}
	w.type_("t")
	list := &w.state.completion.list
	if !list.active || !list.auto {
		t.Fatal("typing did not hand the list to the automatic one")
	}
	if got := w.shell.typed(); !strings.HasSuffix(got, "git st") {
		t.Fatalf("typed %q", got)
	}
}

// Off stops the automatic menu and the ghost, not the shortcut: the user can
// still ask for the list. Typing after that closes it, as an explicit list does.
func TestCompletionShortcutOpensUnderOff(t *testing.T) {
	w := newCompletionWorld(t)
	w.state.history.suggestOff = true
	w.state.history.inlineOff = true
	w.type_("git s")
	for index := 0; index < 10; index++ {
		w.state.Wake()
		time.Sleep(time.Millisecond)
	}
	if w.state.completion.list.active {
		t.Fatal("Off opened a list on its own")
	}
	w.state.openCompletions()
	list := &w.state.completion.list
	if !list.active || list.auto || len(list.candidates) == 0 {
		t.Fatalf("the shortcut under Off = active %v auto %v candidates %d", list.active, list.auto, len(list.candidates))
	}
	w.type_("t")
	if w.state.completion.list.active {
		t.Fatal("typing left the shortcut's list open under Off")
	}
}

// A single answer is the inline suggestion's job in List + inline; the menu
// opens for it when there is no inline suggestion to show it.
func TestCompletionAutoListLeavesAUniqueMatchToTheGhost(t *testing.T) {
	w := newCompletionWorld(t)
	w.type_("git statu")
	for index := 0; index < 10; index++ {
		w.state.Wake()
		time.Sleep(time.Millisecond)
	}
	if w.state.completion.list.active {
		t.Fatal("a unique answer opened the menu in List + inline mode")
	}
	if rest, ok := w.state.commandGhost(w.session); !ok || rest != "s" {
		t.Fatalf("the unique answer was not offered inline: %q ok=%v", rest, ok)
	}

	// Without the inline suggestion, the list is the only place it can appear.
	listOnly := newCompletionWorld(t)
	listOnly.state.history.inlineOff = true
	listOnly.type_("git statu")
	waitCompletionList(t, listOnly)
	if !listOnly.state.completion.list.auto || len(listOnly.state.completion.list.candidates) != 1 {
		t.Fatalf("the list-only mode did not open the unique answer: %+v", listOnly.state.completion.list.candidates)
	}
}

// The inline mode shows the gray suggestion and never pops a menu.
func TestInlineModeNeverPopsTheList(t *testing.T) {
	w := newCompletionWorld(t)
	w.state.history.suggestOff, w.state.history.inlineOff = false, false
	w.type_("git s")
	for index := 0; index < 20; index++ {
		w.state.Wake()
		time.Sleep(time.Millisecond)
	}
	if w.state.completion.list.active {
		t.Fatal("the inline mode opened the list")
	}
}

func TestCompletionAutoDismissRemembersTheCommand(t *testing.T) {
	w := newCompletionWorld(t)
	w.state.history.inlineOff = true // automatic-list mode
	w.type_("git s")
	waitCompletionList(t, w)
	w.state.Key(gfx.KeyEvent{Key: gfx.KeyEscape, Target: terminalHit(w.state, 0)})
	if w.state.completion.list.active {
		t.Fatal("Escape left the list open")
	}
	w.type_("t")
	for index := 0; index < 10; index++ {
		w.state.Wake()
		time.Sleep(time.Millisecond)
	}
	if w.state.completion.list.active {
		t.Fatal("the dismissed command reopened while typing")
	}
	w.session.term.inputGuard.epoch++
	waitCompletionList(t, w)
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

// A history match's age is bucketed by whole units, and every bucket agrees on
// where it ends: the month bucket runs to a year, so 360 days is twelve months
// and never "0y ago". The wording comes from the catalog, not from here.
func TestHumanRelativeTimeBucketsAndWording(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	m := i18n.CompletionMessages{
		TimeJustNow:    "just now",
		TimeMinutesFmt: "%dm ago",
		TimeHoursFmt:   "%dh ago",
		TimeDaysFmt:    "%dd ago",
		TimeMonthsFmt:  "%dmo ago",
		TimeYearsFmt:   "%dy ago",
	}
	cases := []struct {
		name string
		ago  time.Duration
		want string
	}{
		{"just used", 30 * time.Second, "just now"},
		{"a minute", time.Minute, "1m ago"},
		{"an hour", time.Hour, "1h ago"},
		{"a day", 24 * time.Hour, "1d ago"},
		{"thirty days", 30 * 24 * time.Hour, "1mo ago"},
		{"360 days is twelve months, not a zero year", 360 * 24 * time.Hour, "12mo ago"},
		{"364 days", 364 * 24 * time.Hour, "12mo ago"},
		{"a year", 365 * 24 * time.Hour, "1y ago"},
		{"two years", 730 * 24 * time.Hour, "2y ago"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := humanRelativeTime(now, now.Add(-c.ago), m); got != c.want {
				t.Fatalf("%v ago = %q, want %q", c.ago, got, c.want)
			}
		})
	}
	// A zero time reads as just used rather than as an age nobody recorded, and
	// a clock that went backwards says the same instead of a negative age.
	if got := humanRelativeTime(now, time.Time{}, m); got != "just now" {
		t.Fatalf("zero time = %q", got)
	}
	if got := humanRelativeTime(now, now.Add(time.Hour), m); got != "just now" {
		t.Fatalf("future time = %q", got)
	}
}

// The shared prefix is offered as something the user can accept without
// leaving an unrunnable fragment, so it stops at a whole word. A lead with no
// separator at all is already one word, so it is kept as it is.
func TestPrefixWordEndCutsBackToAWholeWord(t *testing.T) {
	cases := []struct{ in, want string }{
		{"git sta", "git "},
		{"ip route get ", "ip route get "},
		{"git", "git"},
		{"list", "list"},
		{"", ""},
		{"a\tb", "a\t"},
	}
	for _, c := range cases {
		if got := prefixWordEnd(c.in); got != c.want {
			t.Fatalf("prefixWordEnd(%q) = %q, want %q", c.in, got, c.want)
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
	var canvas gfx.Canvas
	w.state.Paint(&canvas, gfx.Size{Width: 900, Height: 600})
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
	var canvas gfx.Canvas
	w.state.Paint(&canvas, gfx.Size{Width: 320, Height: 240})
	if _, ok := hitByID(&canvas, hitCompletionList.At(0)); !ok {
		t.Fatal("the first row is not painted in a narrow window")
	}
}
