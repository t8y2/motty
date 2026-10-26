package workbench

import (
	"strings"
	"sync/atomic"
	"testing"

	"github.com/vivaui/viva/app"
	layoutcore "motty/internal/workspace"
)

type fakeTrayWindow struct {
	shows  int
	hidden bool
}

func (window *fakeTrayWindow) Show()        { window.shows++; window.hidden = false }
func (window *fakeTrayWindow) Hidden() bool { return window.hidden }

// trayFixture is two windows: the first has a workspace of two split panes
// (Production and Staging), the second a single Router session.
type trayFixture struct {
	tray        *Tray
	first       *State
	second      *State
	firstView   *fakeTrayWindow
	secondView  *fakeTrayWindow
	quits       int
	invalidated atomic.Int32
}

func newTrayFixture(t *testing.T) *trayFixture {
	t.Helper()
	fixture := &trayFixture{firstView: &fakeTrayWindow{}, secondView: &fakeTrayWindow{}}
	fixture.first = newTestState(t, testOptions(t))
	fixture.first.options.Invalidate = func() { fixture.invalidated.Add(1) }
	openHost(fixture.first)
	openHostInSplit(t, fixture.first, 2, layoutcore.Horizontal)
	fixture.second = newTestState(t, testOptions(t))
	fixture.second.options.Invalidate = func() { fixture.invalidated.Add(1) }
	fixture.second.Click(hitHostList.At(3))
	openHost(fixture.second)
	fixture.tray = NewTray(func() { fixture.quits++ })
	fixture.tray.Add(fixture.first, fixture.firstView)
	fixture.tray.Add(fixture.second, fixture.secondView)
	return fixture
}

// menuTitles flattens a menu into lines, a submenu's entries indented under it.
func menuTitles(items []app.MenuItem, indent string) []string {
	var lines []string
	for _, item := range items {
		if item.Separator {
			lines = append(lines, indent+"---")
			continue
		}
		lines = append(lines, indent+item.Title)
		lines = append(lines, menuTitles(item.Submenu, indent+"  ")...)
	}
	return lines
}

func findItem(items []app.MenuItem, prefix string) *app.MenuItem {
	for index := range items {
		if strings.HasPrefix(items[index].Title, prefix) {
			return &items[index]
		}
		if found := findItem(items[index].Submenu, prefix); found != nil {
			return found
		}
	}
	return nil
}

func TestTrayMenuListsEverySessionOfEveryWindowGroupingSplitWorkspaces(t *testing.T) {
	fixture := newTrayFixture(t)
	menu := fixture.tray.Menu()
	lines := menuTitles(menu, "")
	// A split workspace is a submenu of its sessions; a workspace of one session
	// is a single entry; windows keep the order they were added in. The status
	// of each session follows its name.
	if len(lines) != 10 || lines[0] != "Sessions · 3" || lines[1] != "Production (2)" || !strings.HasPrefix(lines[4], "Router · ") {
		t.Fatalf("menu = %q", lines)
	}
	if !menu[0].Disabled || menu[1].Disabled || len(menu[1].Submenu) != 2 {
		t.Fatalf("heading or group wrong: %+v", menu[:2])
	}
	for _, line := range lines[2:4] {
		if !strings.HasPrefix(line, "  ") || strings.HasSuffix(line, " · ") {
			t.Fatalf("submenu line %q", line)
		}
	}
	tail := lines[5:]
	want := []string{"---", "New local terminal", "Show Motty", "---", "Quit Motty"}
	for index := range want {
		if tail[index] != want[index] {
			t.Fatalf("footer = %q, want %q", tail, want)
		}
	}
	if fixture.tray.SessionCount() != 3 {
		t.Fatalf("SessionCount = %d", fixture.tray.SessionCount())
	}
}

func TestTrayDropsAWindowThatClosedAndNotesWhatItsStatesSay(t *testing.T) {
	fixture := newTrayFixture(t)
	fixture.tray.Remove(fixture.first)
	lines := menuTitles(fixture.tray.Menu(), "")
	if lines[0] != "Sessions · 1" || !strings.HasPrefix(lines[1], "Router · ") {
		t.Fatalf("menu after Remove = %q", lines)
	}
	fixture.tray.Remove(fixture.first) // a second Remove is harmless
}

func TestTrayMenuOfNoSessionsSaysSoAndOfNoWindowsIsEmpty(t *testing.T) {
	state := newTestState(t, testOptions(t))
	tray := NewTray(nil)
	if menu := tray.Menu(); menu != nil {
		t.Fatalf("a tray with no windows has a menu: %+v", menu)
	}
	tray.Add(state, &fakeTrayWindow{})
	lines := menuTitles(tray.Menu(), "")
	if lines[0] != "No open sessions" || len(lines) != 6 {
		t.Fatalf("menu = %q", lines)
	}
}

func TestTrayTooltipChangesOnlyWhenTheCountDoes(t *testing.T) {
	fixture := newTrayFixture(t)
	text, changed := fixture.tray.Tooltip()
	if !changed || text != "Motty (3 open)" {
		t.Fatalf("first tooltip = %q, %t", text, changed)
	}
	if _, changed := fixture.tray.Tooltip(); changed {
		t.Fatal("an unchanged count reported a change")
	}
	fixture.second.closeSession(fixture.second.sessions[0].ID)
	if text, changed := fixture.tray.Tooltip(); !changed || text != "Motty (2 open)" {
		t.Fatalf("after closing a session: %q, %t", text, changed)
	}
	if _, changed := NewTray(nil).Tooltip(); changed {
		t.Fatal("a tray with no windows has a tooltip")
	}
}

func TestChoosingASessionBringsItsWindowToThatSession(t *testing.T) {
	fixture := newTrayFixture(t)
	// Point the first window somewhere else, then choose the Production session,
	// which belongs to it.
	production := fixture.first.sessions[0].ID
	fixture.first.activeSessionID = 0
	fixture.first.activeSection = sectionHosts
	item := findItem(fixture.tray.Menu(), "Production · ")
	if item == nil || item.OnSelect == nil {
		t.Fatal("no entry for the Production session")
	}
	item.OnSelect()
	if fixture.first.activeSessionID != production || fixture.first.activeSection != sectionTerminal {
		t.Fatalf("window one shows session %d in section %d, want %d in the terminal", fixture.first.activeSessionID, fixture.first.activeSection, production)
	}
	if fixture.firstView.shows != 1 || fixture.secondView.shows != 0 {
		t.Fatalf("shows: first %d, second %d", fixture.firstView.shows, fixture.secondView.shows)
	}
	if fixture.invalidated.Load() == 0 {
		t.Fatal("the revealed window was not asked to repaint")
	}
	// A group is a submenu, not an action; an entry whose session has gone does
	// nothing when chosen.
	group := findItem(fixture.tray.Menu(), "Production (2)")
	if group == nil || group.OnSelect != nil || len(group.Submenu) != 2 {
		t.Fatalf("group = %+v", group)
	}
	stale := findItem(fixture.tray.Menu(), "Router · ")
	fixture.second.closeSession(fixture.second.sessions[0].ID)
	stale.OnSelect()
	if fixture.secondView.shows != 0 {
		t.Fatal("an entry for a closed session brought its window forward")
	}
}

func TestChoosingQuitQuits(t *testing.T) {
	fixture := newTrayFixture(t)
	findItem(fixture.tray.Menu(), "Quit Motty").OnSelect()
	if fixture.quits != 1 {
		t.Fatalf("quits = %d", fixture.quits)
	}
}

func TestChoosingLocalTerminalOpensAShellInAWindowAndShowsIt(t *testing.T) {
	skipWithoutUnixShell(t)
	fixture := newTrayFixture(t)
	before := fixture.tray.SessionCount()
	findItem(fixture.tray.Menu(), "New local terminal").OnSelect()
	t.Cleanup(fixture.first.Close)
	if fixture.tray.SessionCount() != before+1 {
		t.Fatalf("sessions = %d, want %d", fixture.tray.SessionCount(), before+1)
	}
	if fixture.firstView.shows != 1 || fixture.secondView.shows != 0 {
		t.Fatalf("shows: first %d, second %d", fixture.firstView.shows, fixture.secondView.shows)
	}
	if fixture.invalidated.Load() == 0 {
		t.Fatal("the window was not asked to repaint")
	}
}

func TestShowWindowBringsBackAHiddenWindowOnlyWhenNoneIsVisible(t *testing.T) {
	fixture := newTrayFixture(t)
	fixture.firstView.hidden, fixture.secondView.hidden = true, true
	findItem(fixture.tray.Menu(), "Show Motty").OnSelect()
	if fixture.firstView.shows != 1 || fixture.secondView.shows != 0 {
		t.Fatalf("with every window hidden: first %d, second %d", fixture.firstView.shows, fixture.secondView.shows)
	}
	fixture.firstView.hidden, fixture.secondView.hidden = true, false
	findItem(fixture.tray.Menu(), "Show Motty").OnSelect()
	if fixture.firstView.shows != 1 || fixture.secondView.shows != 1 || !fixture.firstView.hidden {
		t.Fatalf("with a visible window: first %d, second %d, first hidden %t", fixture.firstView.shows, fixture.secondView.shows, fixture.firstView.hidden)
	}
}

func TestWindowWithSessionsWantsToStayAndOneWithoutDoesNot(t *testing.T) {
	state := newTestState(t, testOptions(t))
	if state.WantsToStay() {
		t.Fatal("an empty window wants to stay")
	}
	openHost(state)
	if !state.WantsToStay() {
		t.Fatal("a window with a session does not")
	}
}

func TestRevealSessionFindsOnlyItsOwnSessions(t *testing.T) {
	fixture := newTrayFixture(t)
	fixture.first.activeSection = sectionHosts
	if fixture.first.RevealSession(9999) {
		t.Fatal("revealed a session that does not exist")
	}
	if fixture.first.activeSection != sectionHosts {
		t.Fatal("a failed reveal changed the window")
	}
}

func BenchmarkTrayMenu(b *testing.B) {
	state := newTestState(b, testOptions(b))
	openHost(state)
	tray := NewTray(nil)
	tray.Add(state, &fakeTrayWindow{})
	b.ReportAllocs()
	for b.Loop() {
		tray.Menu()
	}
}

func TestTrayCountsEveryListedWindow(t *testing.T) {
	tray := NewTray(func() {})
	first, second := newTestState(t, testOptions(t)), newTestState(t, testOptions(t))
	tray.Add(first, &fakeTrayWindow{})
	if tray.WindowCount() != 1 {
		t.Fatalf("windows = %d, want 1", tray.WindowCount())
	}
	tray.Add(second, &fakeTrayWindow{hidden: true})
	if tray.WindowCount() != 2 {
		t.Fatalf("windows = %d, want 2: a hidden window still counts", tray.WindowCount())
	}
	tray.Remove(first)
	if tray.WindowCount() != 1 {
		t.Fatalf("windows = %d, want 1 after a removal", tray.WindowCount())
	}
}

func TestClosingAWindowWithSessionsAsksFirst(t *testing.T) {
	state, _, _, _ := liveSession(t)
	closed := 0
	if !state.ConfirmClose(func() { closed++ }) || state.modal != modalForm || closed != 0 {
		t.Fatalf("no question: modal %d, closed %d", state.modal, closed)
	}
	if !strings.HasPrefix(state.form.description, "1 ") {
		t.Fatalf("description %q", state.form.description)
	}
	state.Click(hitFormCancel)
	if closed != 0 || state.modal != modalNone {
		t.Fatalf("cancelling closed: %d, modal %d", closed, state.modal)
	}
	state.ConfirmClose(func() { closed++ })
	state.Click(hitFormSubmit)
	if closed != 1 {
		t.Fatalf("confirming closed %d times", closed)
	}
	// Asked again while the question is up: the user insists.
	state.ConfirmClose(func() { closed++ })
	if state.ConfirmClose(func() { closed++ }) {
		t.Fatal("a second request should be left to close")
	}
}

func TestClosingAnEmptyWindowDoesNotAsk(t *testing.T) {
	state := newTestState(t, testOptions(t))
	if state.ConfirmClose(func() {}) || state.modal != modalNone {
		t.Fatalf("an empty window asked: modal %d", state.modal)
	}
}
