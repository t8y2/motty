package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
	layoutcore "motty/internal/workspace"
)

// restoredWorld leaves two tabs saved, the first one active, and returns the
// options a new State restores them from. policy "" saves a state from before
// the setting existed.
func restoredWorld(t *testing.T, policy restoreConnectPolicy, legacy bool) (*world, Options) {
	t.Helper()
	world, options := newWorld(t)
	first := newTestState(t, options)
	first.restoreConnect = policy
	openHost(first)
	first.Click(hitHosts)
	first.Click(hitHostList.At(2))
	openHost(first)
	first.Click(hitTabList.At(int(first.sessions[0].ID)))
	first.save()
	if len(world.conns) != 2 {
		t.Fatalf("setup dialed %d connections, want 2", len(world.conns))
	}
	if legacy {
		saved, _, err := loadState(options.StatePath)
		if err != nil {
			t.Fatal(err)
		}
		saved.RestoreConnect = ""
		if err := saveState(options.StatePath, saved); err != nil {
			t.Fatal(err)
		}
	}
	return world, options
}

func TestRestoreConnectsOnlyTheActiveTabByDefault(t *testing.T) {
	world, options := restoredWorld(t, restoreConnectActive, true) // saved before the setting existed
	second := newTestState(t, options)
	if len(world.conns) != 2 {
		t.Fatalf("New dialed %d connections before the window existed", len(world.conns)-2)
	}
	second.Wake() // restore connects once the window can be woken
	if len(world.conns) != 3 {
		t.Fatalf("restore dialed %d connections, want just the active tab's", len(world.conns)-2)
	}
	active, _ := second.sessionByIDPointer(second.activeSessionID)
	if active.conn == nil || active.phase != sshconn.PhaseConnecting || !active.autoRestore || active.restored {
		t.Fatalf("active tab = conn %v phase %v auto %v restored %v", active.conn != nil, active.phase, active.autoRestore, active.restored)
	}
	for index := range second.sessions {
		if other := &second.sessions[index]; other.ID != active.ID && (other.conn != nil || !other.restored || other.autoRestore) {
			t.Fatalf("background tab %d was touched: %+v", other.ID, other)
		}
	}
	if !second.showsProgress(active) {
		t.Fatal("the connecting tab does not show its connection page")
	}
}

func TestRestoreConnectOffAndAll(t *testing.T) {
	world, options := restoredWorld(t, restoreConnectOff, false)
	newTestState(t, options).Wake()
	if len(world.conns) != 2 {
		t.Fatalf("with restore connect off, restore dialed %d connections", len(world.conns)-2)
	}

	world, options = restoredWorld(t, restoreConnectAll, false)
	second := newTestState(t, options)
	second.Wake()
	if len(world.conns) != 4 {
		t.Fatalf("with restore connect all, restore dialed %d connections, want 2", len(world.conns)-2)
	}
	for index := range second.sessions {
		if session := &second.sessions[index]; session.conn == nil || !session.autoRestore {
			t.Fatalf("tab %d was not connected: %+v", session.ID, session)
		}
	}
}

func TestRestoreConnectDialsAtMostAFewTabsAtOnce(t *testing.T) {
	world, options := newWorld(t)
	saved := savedState{RestoreConnect: restoreConnectAll.String(), Workspace: "terminal"}
	for index := range 12 {
		saved.Sessions = append(saved.Sessions, savedSession{Label: "Host", Address: string(rune('a'+index)) + ".example.com", Port: 22})
	}
	if err := saveState(options.StatePath, saved); err != nil {
		t.Fatal(err)
	}
	newTestState(t, options).Wake()
	if len(world.conns) != maxAutoRestoreConnections {
		t.Fatalf("restoring 12 tabs dialed %d connections, want the cap of %d", len(world.conns), maxAutoRestoreConnections)
	}
}

// A connection restore starts must not raise a dialog nobody asked for: when it
// reaches a question it is dropped, the tab says why, and Connect then starts
// an ordinary connection that may ask.
func TestRestoreNeverRaisesAPromptForTheUser(t *testing.T) {
	for _, test := range []struct {
		name  string
		phase sshconn.Phase
		modal modalKind
	}{
		{"password", sshconn.PhasePasswordPrompt, modalPassword},
		{"key passphrase", sshconn.PhaseKeyPassphrasePrompt, modalKeyPassphrase},
		{"host key", sshconn.PhaseHostKeyPrompt, modalHostKey},
	} {
		t.Run(test.name, func(t *testing.T) {
			world, options := restoredWorld(t, restoreConnectActive, false)
			second := newTestState(t, options)
			second.Wake()
			fake := world.conns[len(world.conns)-1]
			fake.set(func(status *sshconn.Status) { status.Phase = test.phase; status.Attempt = 1 })
			second.Wake()

			active, _ := second.sessionByIDPointer(second.activeSessionID)
			if second.modal != modalNone {
				t.Fatalf("restore opened dialog %v", second.modal)
			}
			if fake.canceled != 1 || active.conn != nil || !active.restored || active.autoRestore || active.restoreNote != second.messages.SessionRestoreNeedsYou {
				t.Fatalf("abandoned tab = canceled %d conn %v restored %v auto %v note %q", fake.canceled, active.conn != nil, active.restored, active.autoRestore, active.restoreNote)
			}
			if !second.canReconnect(active) || second.showsProgress(active) {
				t.Fatal("the tab cannot be connected again from its page")
			}
			canvas, texts := paintedTexts(second)
			// The explanation wraps onto several lines.
			page := strings.ReplaceAll(strings.Join(texts, ""), " ", "")
			if !strings.Contains(page, strings.ReplaceAll(second.messages.SessionRestoreNeedsYou, " ", "")) || !hasHit(canvas, hitConnRetry) {
				t.Fatalf("the page does not explain and offer Connect: %v", texts)
			}

			// Connect is an ordinary connection: now it may ask.
			before := len(world.conns)
			second.Click(hitConnRetry)
			if len(world.conns) != before+1 || active.autoRestore || active.restoreNote != "" {
				t.Fatalf("Connect dialed %d, auto %v, note %q", len(world.conns)-before, active.autoRestore, active.restoreNote)
			}
			world.conns[len(world.conns)-1].set(func(status *sshconn.Status) { status.Phase = test.phase; status.Attempt = 1 })
			second.Wake()
			if second.modal != test.modal {
				t.Fatalf("an ordinary connection raised dialog %v, want %v", second.modal, test.modal)
			}
		})
	}
}

func TestRestoredConnectionThatNeedsNoOneJustConnects(t *testing.T) {
	world, options := restoredWorld(t, restoreConnectActive, false)
	second := newTestState(t, options)
	second.Wake()
	fake := world.conns[len(world.conns)-1]
	fake.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseAuthenticating })
	second.Wake()
	fake.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	second.Wake()

	active, _ := second.sessionByIDPointer(second.activeSessionID)
	if active.phase != sshconn.PhaseConnected || active.autoRestore || active.restored || second.modal != modalNone {
		t.Fatalf("tab = phase %v auto %v restored %v modal %v", active.phase, active.autoRestore, active.restored, second.modal)
	}
}

func TestRestoreConnectSettingChoosesAndPersists(t *testing.T) {
	state := newTestState(t, testOptions(t))
	if state.restoreConnect != restoreConnectActive {
		t.Fatalf("default = %v, want the active tab", state.restoreConnect)
	}
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsConnection)
	canvas, _ := paintedTexts(state)
	for _, id := range []ui.ID{hitSettingsRestoreOff, hitSettingsRestoreActive, hitSettingsRestoreAll} {
		var tall ui.Canvas
		state.Paint(&tall, ui.Size{Width: 1400, Height: 1300})
		if _, ok := hitByID(&tall, id); !ok {
			t.Fatalf("settings paint no restore choice %d", id)
		}
	}
	_ = canvas
	options := testOptions(t)
	first := newTestState(t, options)
	first.Click(hitSettings)
	for _, test := range []struct {
		hit  ui.ID
		want restoreConnectPolicy
	}{{hitSettingsRestoreAll, restoreConnectAll}, {hitSettingsRestoreOff, restoreConnectOff}, {hitSettingsRestoreActive, restoreConnectActive}} {
		first.Click(test.hit)
		if first.restoreConnect != test.want {
			t.Fatalf("clicking %d set %v, want %v", test.hit, first.restoreConnect, test.want)
		}
		if again := newTestState(t, options); again.restoreConnect != test.want {
			t.Fatalf("restored %v, want %v", again.restoreConnect, test.want)
		}
	}
}

// A split tab shows all its panes, so restoring "the current tab" connects each
// of them, not only the focused one; tabs behind it stay on their pages.
func TestRestoreConnectsEveryPaneOfTheActiveTab(t *testing.T) {
	world, options := newWorld(t)
	first := newTestState(t, options)
	openHost(first)
	openHostInSplit(t, first, 2, layoutcore.Horizontal)
	first.Click(hitHosts)
	first.Click(hitHostList.At(3))
	openHost(first)
	first.Click(hitTabList.At(int(first.deck.Workspaces(nil)[0].ID)))
	first.save()
	dialedBefore := len(world.conns)

	second := newTestState(t, options)
	second.Wake()
	if got := len(world.conns) - dialedBefore; got != 2 {
		t.Fatalf("restore dialed %d connections, want the two panes of the active tab", got)
	}
	background := 0
	for index := range second.sessions {
		session := &second.sessions[index]
		if session.conn == nil {
			background++
			if !session.restored {
				t.Fatalf("tab %d is neither connected nor restored", session.ID)
			}
		}
	}
	if background != 1 {
		t.Fatalf("%d sessions left unconnected, want the other tab's one", background)
	}
}

// Restoring a split tab reconnects every pane, and none of them shows the
// connection card: the panes keep their terminals and the shells appear when
// they are ready. A lone tab keeps its card.
func TestRestoredSplitPanesConnectWithoutProgressPages(t *testing.T) {
	world, options := newWorld(t)
	first := newTestState(t, options)
	openHost(first)
	openHostInSplit(t, first, 2, layoutcore.Horizontal)
	first.Click(hitHosts)
	first.Click(hitHostList.At(3))
	openHost(first)
	first.Click(hitTabList.At(int(first.deck.Workspaces(nil)[0].ID)))
	first.save()
	before := len(world.conns)

	second := newTestState(t, options)
	second.Wake()
	if got := len(world.conns) - before; got != 2 {
		t.Fatalf("restore dialed %d connections, want the two panes", got)
	}
	panes := second.deck.Panes(second.deck.ActiveWorkspace(), nil)
	if len(panes) != 2 {
		t.Fatalf("panes = %+v", panes)
	}
	for _, pane := range panes {
		session, _ := second.sessionByIDPointer(uint32(pane.SessionID))
		if second.showsProgress(session) {
			t.Fatalf("pane of session %d shows a connection card", session.ID)
		}
	}
}

// A restored tab's page has no terminal to hold the keyboard: Enter on it
// reconnects, Command+W closes the tab, and both keys are shown on the page.
func TestRestoredPageTakesEnterAndCommandWAndShowsTheKeys(t *testing.T) {
	_, options := newWorld(t)
	first := newTestState(t, options)
	first.restoreConnect = restoreConnectOff
	openHost(first)
	first.save()

	state := newTestState(t, options)
	state.restoreConnect = restoreConnectOff
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	if !session.restored {
		t.Fatal("setup: the session was not restored")
	}
	canvas, texts := paintedTexts(state)
	if !contains(texts, "↵") || !contains(texts, "W") {
		t.Fatalf("the page does not show its keys: %q", texts)
	}
	if _, ok := hitByID(canvas, hitConnRetry); !ok {
		t.Fatal("no Connect button")
	}

	if result := state.Key(ui.KeyEvent{Key: ui.KeyEnter}); !result.Handled || session.conn == nil {
		t.Fatalf("Enter on the restored page: %+v conn=%v", result, session.conn != nil)
	}

	second := newTestState(t, options)
	second.restoreConnect = restoreConnectOff
	tabs := len(second.deck.Workspaces(nil))
	if result := commandW(second); !result.Handled || len(second.deck.Workspaces(nil)) != tabs-1 {
		t.Fatalf("Command+W on the restored page: %+v tabs %d→%d", result, tabs, len(second.deck.Workspaces(nil)))
	}
}

// Picking a restored tab leaves the keyboard on that tab, not on the page, and
// Enter there must still connect: it used to need a click on the page first.
func TestEnterOnTheActiveTabConnectsARestoredPage(t *testing.T) {
	_, options := newWorld(t)
	first := newTestState(t, options)
	first.restoreConnect = restoreConnectOff
	openHost(first)
	first.save()

	state := newTestState(t, options)
	state.restoreConnect = restoreConnectOff
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	if !session.restored {
		t.Fatal("setup: the session was not restored")
	}
	other := hitTabList.At(int(state.deck.ActiveWorkspace()) + 1)
	if result := state.Key(ui.KeyEvent{Key: ui.KeyEnter, Target: other}); result.Handled && session.conn != nil {
		t.Fatal("Enter on some other tab must not connect this page")
	}
	active := hitTabList.At(int(state.deck.ActiveWorkspace()))
	if result := state.Key(ui.KeyEvent{Key: ui.KeyEnter, Target: active}); !result.Handled || session.conn == nil {
		t.Fatalf("Enter on the active tab: %+v conn=%v", result, session.conn != nil)
	}
}
