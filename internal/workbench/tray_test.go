package workbench

import (
	"strings"
	"sync/atomic"
	"testing"

	"github.com/vivaui/viva/app"
	"motty/internal/buildinfo"
	"motty/internal/i18n"
	"motty/internal/update"
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
	firstOptions := testOptions(t)
	firstOptions.WindowID = 1
	fixture.first = newTestState(t, firstOptions)
	fixture.first.options.Invalidate = func() { fixture.invalidated.Add(1) }
	openHost(fixture.first)
	openHostInSplit(t, fixture.first, 2, layoutcore.Horizontal)
	secondOptions := testOptions(t)
	secondOptions.WindowID = 2
	fixture.second = newTestState(t, secondOptions)
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

// directItem is the entry of exactly this menu with that title, without
// searching submenus, so an entry and a same-named entry of a submenu stay
// apart.
func directItem(items []app.MenuItem, title string) *app.MenuItem {
	for index := range items {
		if items[index].Title == title {
			return &items[index]
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
	if len(lines) == 0 || lines[0] != "Sessions · 3" || lines[1] != "Production (2)" || !strings.HasPrefix(lines[4], "Router · ") {
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
	// The saved hosts follow the sessions under their groups: a group is a
	// submenu of its hosts, named by the last part of its path.
	hosts := directItem(menu, "Hosts")
	if hosts == nil || hosts.OnSelect != nil || len(hosts.Submenu) != 2 {
		t.Fatalf("hosts entry = %+v", hosts)
	}
	network := directItem(hosts.Submenu, "Network")
	if network == nil || network.OnSelect != nil || len(network.Submenu) != 1 || network.Submenu[0].Title != "Router" || network.Submenu[0].OnSelect == nil {
		t.Fatalf("network group = %+v", network)
	}
	servers := directItem(hosts.Submenu, "Servers")
	if servers == nil || len(servers.Submenu) != 2 || servers.Submenu[0].Title != "Production" || servers.Submenu[1].Title != "Staging" {
		t.Fatalf("servers group = %+v", servers)
	}
	tail := lines[len(lines)-6:]
	want := []string{"---", "New local terminal", "Show " + buildinfo.ProductName, "Check for Updates…", "---", "Quit " + buildinfo.ProductName}
	for index := range want {
		if tail[index] != want[index] {
			t.Fatalf("footer = %q, want %q", tail, want)
		}
	}
	if fixture.tray.SessionCount() != 3 {
		t.Fatalf("SessionCount = %d", fixture.tray.SessionCount())
	}
}

func TestTrayMenuNamesEveryEntryUniquely(t *testing.T) {
	fixture := newTrayFixture(t)
	seen := map[string]string{}
	var walk func([]app.MenuItem)
	walk = func(items []app.MenuItem) {
		for _, item := range items {
			if item.Separator {
				continue
			}
			if item.ID == "" {
				t.Fatalf("entry %q has no id", item.Title)
			}
			if title, duplicate := seen[item.ID]; duplicate {
				t.Fatalf("id %q names both %q and %q", item.ID, title, item.Title)
			}
			seen[item.ID] = item.Title
			walk(item.Submenu)
		}
	}
	walk(fixture.tray.Menu())
	// Both windows number their sessions from 1, so a session's id carries its
	// window's number to name it alone. The three saved hosts and their two
	// groups add their own entries.
	if len(seen) != 15 {
		t.Fatalf("named %d entries, want 15: %v", len(seen), seen)
	}
}

func TestTrayEntryIDsSurviveTitlesAndClosedWindows(t *testing.T) {
	fixture := newTrayFixture(t)
	heading := fixture.tray.Menu()[0].ID
	router := findItem(fixture.tray.Menu(), "Router · ")
	if router == nil {
		t.Fatal("no Router entry")
	}
	routerID := router.ID
	// The heading says how many sessions there are; its name must not move with
	// the number.
	fixture.first.closeSession(fixture.first.sessions[0].ID)
	if got := fixture.tray.Menu()[0]; got.ID != heading || !strings.HasPrefix(got.Title, "Sessions · 2") {
		t.Fatalf("heading = %+v, want id %q counting two", got, heading)
	}
	// A session's title carries its name and how it is doing; neither is its
	// name to a host that kept the menu.
	fixture.second.sessions[0].Title = "Renamed"
	renamed := findItem(fixture.tray.Menu(), "Renamed · ")
	if renamed == nil || renamed.ID != routerID {
		t.Fatalf("renamed entry = %+v, want id %q", renamed, routerID)
	}
	// A window that closes leaves the entries of the others where they were.
	fixture.tray.Remove(fixture.first)
	last := findItem(fixture.tray.Menu(), "Renamed · ")
	if last == nil || last.ID != routerID {
		t.Fatalf("entry after a window closed = %+v, want id %q", last, routerID)
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
	menu := tray.Menu()
	lines := menuTitles(menu, "")
	if lines[0] != "No open sessions" {
		t.Fatalf("menu = %q", lines)
	}
	// The saved hosts are still there to open, under their groups.
	if directItem(menu, "Hosts") == nil {
		t.Fatalf("menu has no saved hosts: %q", lines)
	}
	if directItem(menu, "New local terminal") == nil || directItem(menu, "Quit "+buildinfo.ProductName) == nil {
		t.Fatalf("menu = %q", lines)
	}
}

// A window whose catalog holds no host or workspace has nothing to offer under
// those names, so the menu keeps exactly the entries it always had.
func TestTrayMenuWithoutSavedHostsOrWorkspacesHasNoConnectionSections(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.hosts, state.hostGroups, state.templates = nil, nil, nil
	tray := NewTray(nil)
	tray.Add(state, &fakeTrayWindow{})
	lines := menuTitles(tray.Menu(), "")
	want := []string{"No open sessions", "---", "New local terminal", "Show " + buildinfo.ProductName, "Check for Updates…", "---", "Quit " + buildinfo.ProductName}
	if len(lines) != len(want) {
		t.Fatalf("menu = %q", lines)
	}
	for index := range want {
		if lines[index] != want[index] {
			t.Fatalf("menu = %q, want %q", lines, want)
		}
	}
}

// Choosing a saved host opens it in a tab of the window the user can see,
// whatever window the tray listed the hosts for.
func TestChoosingASavedHostOpensItInTheVisibleWindow(t *testing.T) {
	fixture := newTrayFixture(t)
	fixture.firstView.hidden = true
	hosts := directItem(fixture.tray.Menu(), "Hosts")
	network := directItem(hosts.Submenu, "Network")
	router := directItem(network.Submenu, "Router")
	if router == nil || router.OnSelect == nil {
		t.Fatalf("router entry = %+v", router)
	}
	before := len(fixture.second.sessions)
	router.OnSelect()
	if len(fixture.second.sessions) != before+1 {
		t.Fatalf("second window sessions = %d, want %d", len(fixture.second.sessions), before+1)
	}
	if fixture.secondView.shows != 1 || fixture.firstView.shows != 0 {
		t.Fatalf("shows: first %d, second %d", fixture.firstView.shows, fixture.secondView.shows)
	}
	if fixture.invalidated.Load() == 0 {
		t.Fatal("the window was not asked to repaint")
	}
	var routerHost Host
	for _, host := range fixture.second.hosts {
		if host.Label == "Router" {
			routerHost = host
		}
	}
	if opened := fixture.second.sessions[before]; routerHost.ID == 0 || opened.HostID != routerHost.ID {
		t.Fatalf("opened host %d, want Router %d", opened.HostID, routerHost.ID)
	}
}

// Choosing a saved workspace opens its terminals in the visible window.
func TestChoosingASavedWorkspaceOpensItInTheVisibleWindow(t *testing.T) {
	state := newTestState(t, testOptions(t))
	template := mustWorkspaceTemplate(t, "Cluster", workspaceCatalogIDs(state, 0, 1), 2)
	state.templates = []WorkspaceTemplate{template}
	view := &fakeTrayWindow{}
	tray := NewTray(nil)
	tray.Add(state, view)
	item := directItem(directItem(tray.Menu(), "Workspaces").Submenu, "Cluster")
	if item == nil || item.OnSelect == nil {
		t.Fatalf("workspace entry = %+v", item)
	}
	item.OnSelect()
	if len(state.sessions) != len(template.HostIDs) {
		t.Fatalf("sessions = %d, want %d", len(state.sessions), len(template.HostIDs))
	}
	if len(state.deck.Workspaces(nil)) != 1 {
		t.Fatalf("workspaces = %d, want one", len(state.deck.Workspaces(nil)))
	}
	if view.shows != 1 {
		t.Fatalf("shows = %d", view.shows)
	}
}

// A host entry is named by the host's catalog id, so renaming the host leaves
// the entry the same entry to a menu that outlives the title.
func TestTrayHostEntryKeepsItsIDWhenTheLabelChanges(t *testing.T) {
	fixture := newTrayFixture(t)
	network := directItem(directItem(fixture.tray.Menu(), "Hosts").Submenu, "Network")
	id := network.Submenu[0].ID
	fixture.first.hosts[2].Label = "Gateway"
	renamed := directItem(directItem(directItem(fixture.tray.Menu(), "Hosts").Submenu, "Network").Submenu, "Gateway")
	if renamed == nil || renamed.ID != id {
		t.Fatalf("renamed entry = %+v, want id %q", renamed, id)
	}
}

// The saved hosts follow the group tree the hosts page shows: nested groups are
// nested submenus, and the hosts of the unnamed group come first.
func TestTrayHostsFollowTheirGroupTree(t *testing.T) {
	fixture := newTrayFixture(t)
	fixture.first.hosts[0].Group, fixture.first.hosts[1].Group = "Servers/Web", "Servers/Web"
	fixture.first.hosts[2].Group = ""
	fixture.first.refreshHostGroups()
	hosts := directItem(fixture.tray.Menu(), "Hosts")
	if hosts == nil || len(hosts.Submenu) != 2 || hosts.Submenu[0].Title != "Router" {
		t.Fatalf("hosts menu = %+v", hosts)
	}
	servers := directItem(hosts.Submenu, "Servers")
	if servers == nil || len(servers.Submenu) != 1 || servers.Submenu[0].Title != "Web" {
		t.Fatalf("servers menu = %+v", servers)
	}
	web := servers.Submenu[0]
	if len(web.Submenu) != 2 || web.Submenu[0].Title != "Production" || web.Submenu[1].Title != "Staging" {
		t.Fatalf("web menu = %+v", web)
	}
}

func TestTrayTooltipChangesOnlyWhenTheCountDoes(t *testing.T) {
	fixture := newTrayFixture(t)
	name := buildinfo.ProductName + " " + update.Current
	text, changed := fixture.tray.Tooltip()
	if !changed || text != name+" (3 open)" {
		t.Fatalf("first tooltip = %q, %t", text, changed)
	}
	if _, changed := fixture.tray.Tooltip(); changed {
		t.Fatal("an unchanged count reported a change")
	}
	fixture.second.closeSession(fixture.second.sessions[0].ID)
	if text, changed := fixture.tray.Tooltip(); !changed || text != name+" (2 open)" {
		t.Fatalf("after closing a session: %q, %t", text, changed)
	}
	if _, changed := NewTray(nil).Tooltip(); changed {
		t.Fatal("a tray with no windows has a tooltip")
	}
}

func TestTrayUsesTheBuildProductNameInBothLanguages(t *testing.T) {
	tests := []struct {
		language i18n.Language
		show     string
		quit     string
	}{
		{language: i18n.English, show: "Show " + buildinfo.ProductName, quit: "Quit " + buildinfo.ProductName},
		{language: i18n.Chinese, show: "显示 " + buildinfo.ProductName, quit: "退出 " + buildinfo.ProductName},
	}
	for _, test := range tests {
		t.Run(test.language.String(), func(t *testing.T) {
			state := newTestState(t, testOptions(t))
			state.language = test.language
			state.messages = i18n.For(test.language)
			tray := NewTray(nil)
			tray.Add(state, &fakeTrayWindow{})
			if text, changed := tray.Tooltip(); !changed || text != buildinfo.ProductName+" "+update.Current {
				t.Fatalf("empty tooltip = %q, %t", text, changed)
			}
			menu := tray.Menu()
			if findItem(menu, test.show) == nil {
				t.Fatalf("menu does not show %q: %q", test.show, menuTitles(menu, ""))
			}
			if findItem(menu, test.quit) == nil {
				t.Fatalf("menu does not quit with %q: %q", test.quit, menuTitles(menu, ""))
			}
		})
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

func TestChoosingCheckForUpdatesAsksInAVisibleWindowAndOffersTheRelease(t *testing.T) {
	fixture := newTrayFixture(t)
	fixture.firstView.hidden, fixture.secondView.hidden = true, true
	release := update.Release{Version: "99.0.0", URL: "https://example.com/release"}
	fixture.first.options.CheckUpdate = func() (update.Release, error) { return release, nil }
	item := findItem(fixture.tray.Menu(), "Check for Updates…")
	if item == nil || item.OnSelect == nil || item.Disabled {
		t.Fatalf("item = %+v", item)
	}
	item.OnSelect()
	if fixture.firstView.shows != 1 {
		t.Fatalf("shows = %d, want the hidden window brought forward", fixture.firstView.shows)
	}
	waitUpdate(t, fixture.first, func() bool { return fixture.first.update.available })
	if fixture.first.update.release.Version != "99.0.0" {
		t.Fatalf("release = %+v", fixture.first.update.release)
	}
}

func TestChoosingQuitQuits(t *testing.T) {
	fixture := newTrayFixture(t)
	findItem(fixture.tray.Menu(), "Quit "+buildinfo.ProductName).OnSelect()
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
	findItem(fixture.tray.Menu(), "Show "+buildinfo.ProductName).OnSelect()
	if fixture.firstView.shows != 1 || fixture.secondView.shows != 0 {
		t.Fatalf("with every window hidden: first %d, second %d", fixture.firstView.shows, fixture.secondView.shows)
	}
	fixture.firstView.hidden, fixture.secondView.hidden = true, false
	findItem(fixture.tray.Menu(), "Show "+buildinfo.ProductName).OnSelect()
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

func TestHidingToTrayIsExplainedOnce(t *testing.T) {
	w := newNotifyWorld(t)
	state := newTestState(t, w.options)
	w.session(t, state)
	state.NoteHiddenToTray()
	if len(w.shown) != 1 {
		t.Fatalf("shown = %+v, want one explanation", w.shown)
	}
	// The notification names the running product, which a development build
	// spells differently, so compare through the same substitution it uses.
	if w.shown[0].Title != productText(state.messages.TrayHintTitle) || w.shown[0].Body != productText(state.messages.TrayHintBody) {
		t.Fatalf("shown = %+v", w.shown[0])
	}
	state.NoteHiddenToTray()
	if len(w.shown) != 1 {
		t.Fatalf("shown = %+v, want the second hide to stay silent", w.shown)
	}
	if again := newTestState(t, w.options); !again.trayHintShown {
		t.Fatal("the explanation was not remembered on this device")
	}
}
