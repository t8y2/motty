package workbench

import (
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/vivaui/viva/gfx"
	"motty/internal/term"
	layoutcore "motty/internal/workspace"
)

func TestPathTabsKeepTheEndWhenTheDirectoryDoesNotFit(t *testing.T) {
	state := newTestState(t, testOptions(t))
	openHost(state)
	state.tabTitleMode = tabTitleDirectory
	session := &state.sessions[0]
	for _, directory := range []string{
		"/workspace/very-long-parent-directory/plugins/payment",
		"/workspace/" + strings.Repeat("很长的目录", 20) + "尾部",
	} {
		state.recordTerminalDirectory(session.ID, term.Cwd{Path: directory}, false)
		for _, orientation := range []tabOrientation{tabsHorizontal, tabsVertical} {
			state.setTabOrientation(orientation)
			for _, alert := range []outputAlertKind{outputAlertNone, outputAlertQuiet, outputAlertRunning, outputAlertError} {
				state.backgroundOutputAlerts = true
				state.activeSection = sectionSettings
				session.outputAlert.kind = alert
				var canvas gfx.Canvas
				canvas.SetReduceMotion(true)
				state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
				hit, ok := hitByID(&canvas, hitTabList.At(int(state.deck.ActiveWorkspace())))
				if !ok || hit.Label != directory || hit.Hint != directory {
					t.Fatalf("orientation %v lost full path: %+v", orientation, hit)
				}
				found := false
				for _, text := range canvas.Texts() {
					if text.Bounds.Y < hit.Bounds.Y || text.Bounds.Y >= hit.Bounds.Y+hit.Bounds.Height || text.Bounds.X < hit.Bounds.X || text.Bounds.X >= hit.Bounds.X+hit.Bounds.Width {
						continue
					}
					if !strings.HasPrefix(text.Value, "…") {
						continue
					}
					found = true
					if canvas.TextWidthWeight(text.Value, text.Style.Size, text.Style.Weight) > text.Bounds.Width || text.Bounds.X+text.Bounds.Width > hit.Bounds.X+hit.Bounds.Width {
						t.Fatalf("path suffix overflows tab: %+v", text)
					}
					if !utf8.ValidString(text.Value) || !strings.HasSuffix(directory, strings.TrimPrefix(text.Value, "…")) {
						t.Fatalf("invalid path suffix %q", text.Value)
					}
					if strings.HasSuffix(directory, "/payment") && !strings.HasSuffix(text.Value, "/payment") {
						t.Fatalf("lost leaf directory: %q", text.Value)
					}
					if strings.HasSuffix(directory, "尾部") && !strings.HasSuffix(text.Value, "尾部") {
						t.Fatalf("lost Unicode suffix: %q", text.Value)
					}
				}
				if !found {
					t.Fatalf("orientation %v did not paint a path suffix", orientation)
				}
			}
		}
	}
}

func TestPathTabAbbreviatesOnlyReportedHomeAndKeepsAbsoluteHint(t *testing.T) {
	state := newTestState(t, testOptions(t))
	openHost(state)
	state.tabTitleMode = tabTitleDirectory
	session := &state.sessions[0]
	screen := session.term.screen
	screen.Write([]byte(folderSyncMarker(session, "A") + "\x1b]777;motty-home;" + session.term.folderToken + ";" + url.PathEscape("/home/alice") + "\a"))
	for _, example := range []struct{ directory, label string }{
		{"/home/alice", "~"}, {"/home/alice/api", "~/api"}, {"/home/alice-backup", "/home/alice-backup"},
	} {
		screen.Write([]byte("\x1b]7;file://server" + example.directory + "\a"))
		if got := state.tabLabel(activeTab(state)); got != example.label {
			t.Fatalf("label = %q, want %q", got, example.label)
		}
		if got := state.tabDirectory(activeTab(state)); got != example.directory {
			t.Fatalf("hint = %q, want %q", got, example.directory)
		}
	}
	for _, orientation := range []tabOrientation{tabsHorizontal, tabsVertical} {
		state.setTabOrientation(orientation)
		var canvas gfx.Canvas
		state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
		hit, ok := hitByID(&canvas, hitTabList.At(int(state.deck.ActiveWorkspace())))
		if !ok || hit.Hint != "/home/alice-backup" {
			t.Fatalf("orientation %v hint = %q, present=%t", orientation, hit.Hint, ok)
		}
	}
	screen.Write([]byte("\x1b]777;motty-home;" + session.term.folderToken + ";" + url.PathEscape(`/srv/a\b`) + "\a"))
	screen.Write([]byte("\x1b]7;file://server/srv/a%5Cb/project\a"))
	if got := state.tabLabel(activeTab(state)); got != "~/project" {
		t.Fatalf("POSIX home containing backslash = %q, want ~/project", got)
	}
	state.openTabRename(state.deck.ActiveWorkspace())
	state.tabRename.editor.Insert("production")
	state.clickTabRenameDialog(hitTabRenameSubmit)
	if got := state.tabLabel(activeTab(state)); got != "production" {
		t.Fatalf("manual name = %q", got)
	}
}

func activeTab(state *State) layoutcore.WorkspaceInfo {
	for _, tab := range state.deck.Workspaces(nil) {
		if tab.ID == state.deck.ActiveWorkspace() {
			return tab
		}
	}
	return layoutcore.WorkspaceInfo{}
}

func TestTabShowsTrustedDirectoryOfActivePane(t *testing.T) {
	state := threeVisiblePaneState(t)
	state.tabTitleMode = tabTitleDirectory
	first, second := state.sessions[0].ID, state.sessions[1].ID
	state.recordTerminalDirectory(first, term.Cwd{Path: "/srv/api"}, false)
	state.recordTerminalDirectory(second, term.Cwd{Path: "/srv/web"}, false)
	for _, example := range []struct {
		id   uint32
		want string
	}{{first, "/srv/api"}, {second, "/srv/web"}} {
		state.ActivatePane(example.id)
		if got := state.tabLabel(activeTab(state)); got != example.want {
			t.Fatalf("label = %q, want %q", got, example.want)
		}
	}
	state.sessions[1].term.resetFolderSync()
	if got := state.tabLabel(activeTab(state)); got != activeTab(state).Title {
		t.Fatalf("after reset = %q, want connection name", got)
	}
}

func TestTabFollowsTheTitleTheTerminalReports(t *testing.T) {
	state := newTestState(t, testOptions(t))
	openHost(state)
	hostName := activeTab(state).Title
	state.sessions[0].term.screen.Write([]byte("\x1b]2;work:1:bash\a"))

	if got := state.tabLabel(activeTab(state)); got != hostName {
		t.Fatalf("with the setting off the tab reads %q; want the host name %q", got, hostName)
	}

	state.tabTitleMode = tabTitleTerminal
	if got := state.tabLabel(activeTab(state)); got != "work:1:bash" {
		t.Fatalf("tab = %q; want the reported title", got)
	}
	if got := activeTab(state).Title; got != hostName {
		t.Fatalf("the stored title became %q; the reported title must not be saved", got)
	}

	state.sessions[0].term.screen.Write([]byte("\x1b]2;\a"))
	if got := state.tabLabel(activeTab(state)); got != hostName {
		t.Fatalf("after the title was cleared the tab reads %q; want %q", got, hostName)
	}
}

func TestRenamedTabKeepsItsNameWhenTheTerminalReportsATitle(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.tabTitleMode = tabTitleTerminal
	openHost(state)
	workspace := state.deck.ActiveWorkspace()
	state.openTabRename(workspace)
	state.tabRename.editor.Insert("production")
	state.clickTabRenameDialog(hitTabRenameSubmit)

	state.sessions[0].term.screen.Write([]byte("\x1b]2;work:1:bash\a"))
	if got := state.tabLabel(activeTab(state)); got != "production" {
		t.Fatalf("tab = %q; a name the user chose must win over the terminal's title", got)
	}

	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, hitTabList.At(int(workspace))); !ok {
		t.Fatal("the renamed tab is not painted")
	}
}

func TestActivatingPaneRefreshesCachedTabIdentityAndKeepsRenamedTitle(t *testing.T) {
	state := threeVisiblePaneState(t)
	state.tabTitleMode = tabTitleTerminal

	workspaceID := state.deck.ActiveWorkspace()
	workspaceIndex, ok := state.workspaceTab(workspaceID)
	if !ok {
		t.Fatal("the active workspace is missing from the tab cache")
	}
	firstSession := &state.sessions[0]
	secondSession := &state.sessions[1]
	_, firstPane, ok := state.deck.PaneForSession(layoutcore.SessionID(firstSession.ID))
	if !ok {
		t.Fatal("the first session has no pane")
	}
	_, secondPane, ok := state.deck.PaneForSession(layoutcore.SessionID(secondSession.ID))
	if !ok {
		t.Fatal("the second session has no pane")
	}

	firstSession.term.screen.Write([]byte("\x1b]2;first-shell\a"))
	secondSession.term.screen.Write([]byte("\x1b]2;second-shell\a"))
	state.rebuildTabOptions()

	state.ActivatePane(firstSession.ID)
	cached := state.workspaceTabs[workspaceIndex]
	if cached.ActivePane != firstPane || state.tabSession(cached) != firstSession || state.tabLabel(cached) != "first-shell" {
		t.Fatalf("after first activation: cached pane=%d session=%p label=%q", cached.ActivePane, state.tabSession(cached), state.tabLabel(cached))
	}

	state.ActivatePane(secondSession.ID)
	cached = state.workspaceTabs[workspaceIndex]
	if cached.ActivePane != secondPane || state.tabSession(cached) != secondSession || state.tabLabel(cached) != "second-shell" {
		t.Fatalf("after second activation: cached pane=%d session=%p label=%q", cached.ActivePane, state.tabSession(cached), state.tabLabel(cached))
	}

	state.openTabRename(workspaceID)
	state.tabRename.editor.Insert("production-main")
	state.clickTabRenameDialog(hitTabRenameSubmit)
	state.ActivatePane(firstSession.ID)
	cached = state.workspaceTabs[workspaceIndex]
	if !cached.Named || cached.ActivePane != firstPane || state.tabSession(cached) != firstSession {
		t.Fatalf("after renamed tab activation: named=%t cached pane=%d session=%p", cached.Named, cached.ActivePane, state.tabSession(cached))
	}
	if got := state.tabLabel(cached); got != "production-main" {
		t.Fatalf("renamed tab changed to %q after activating another pane", got)
	}
}

func TestTabTitleSettingSurvivesRestart(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	state.tabTitleMode = tabTitleTerminal
	state.save()
	if again := newTestState(t, options); again.tabTitleMode != tabTitleTerminal {
		t.Fatal("the setting was lost after a restart")
	}
}

func TestTabNameModeMigratesAndSurvivesRestart(t *testing.T) {
	for _, example := range []struct {
		name       string
		legacy     bool
		mode, want tabTitleMode
	}{
		{"default", false, "", tabTitleConnection},
		{"legacy-title", true, "", tabTitleTerminal},
		{"explicit-connection", true, tabTitleConnection, tabTitleConnection},
		{"directory", false, tabTitleDirectory, tabTitleDirectory},
	} {
		t.Run(example.name, func(t *testing.T) {
			options := testOptions(t)
			saveReleaseState(t, options.StatePath, savedState{Version: stateFileVersion, TerminalTabTitle: example.legacy, TabTitleMode: example.mode})
			state := newTestState(t, options)
			if state.tabTitleMode != example.want {
				t.Fatalf("mode = %q, want %q", state.tabTitleMode, example.want)
			}
			state.save()
			if again := newTestState(t, options); again.tabTitleMode != example.want {
				t.Fatalf("restored mode = %q", again.tabTitleMode)
			}
		})
	}
}

func TestPathModeEnablesReportsWithoutFileSyncOnNextShell(t *testing.T) {
	state, session, fake, _ := openTestSFTP(t)
	state.sftpFollowOff, state.sftpTerminalFollow = true, false
	fake.mu.Lock()
	before := len(fake.configs)
	fake.mu.Unlock()
	state.Click(hitSettingsTabDirectory)
	fake.mu.Lock()
	after := len(fake.configs)
	fake.mu.Unlock()
	if after != before {
		t.Fatal("changing title mode reopened the shell")
	}
	session.term.shell.Close()
	session.term.shell = nil
	state.openShell(session)
	fake.mu.Lock()
	config := fake.configs[len(fake.configs)-1]
	fake.mu.Unlock()
	if !config.IntegrateShell {
		t.Fatal("path mode without file sync did not enable reports")
	}
}
