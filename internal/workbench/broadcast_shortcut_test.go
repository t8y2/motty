package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
	"motty/internal/sshconn"
	layoutcore "motty/internal/workspace"
)

// The broadcast key is the command palette's and the toolbar button's toggle on
// a chord: in a split tab it starts and stops the broadcast, and in a tab that
// cannot broadcast it says why instead of failing silently.
func TestBroadcastShortcutTogglesTheTabsBroadcast(t *testing.T) {
	state, first, second, firstID, secondID := twoPanes(t)
	chord := state.shortcuts.chords.Chord(shortcutBroadcast)
	if !chord.Valid() {
		t.Fatal("broadcast has no default shortcut")
	}

	if result := pressChord(state, chord); !result.Handled || !state.broadcasting(firstID) || !state.broadcasting(secondID) {
		t.Fatalf("shortcut did not start the broadcast: result=%+v broadcast=%d", result, state.broadcast)
	}
	insertText(state, secondID, "ls")
	if first.typed() != "ls" || second.typed() != "ls" {
		t.Fatalf("while broadcasting: first %q, second %q", first.typed(), second.typed())
	}

	if result := pressChord(state, chord); !result.Handled || state.broadcast != 0 || state.status != state.messages.Tools.StatusBroadcastOff {
		t.Fatalf("shortcut did not stop the broadcast: result=%+v broadcast=%d status=%q", result, state.broadcast, state.status)
	}
}

func TestBroadcastShortcutInATabWithOnePaneSaysWhyItCannotStart(t *testing.T) {
	world, options := newSharingWorld(t)
	state := newTestState(t, options)
	world.connectFirst(t, state)
	state.activeSection = sectionTerminal

	pressChord(state, state.shortcuts.chords.Chord(shortcutBroadcast))
	if state.broadcast != 0 || state.status != state.messages.Tools.StatusBroadcastNeedsSplit {
		t.Fatalf("broadcast=%d status=%q", state.broadcast, state.status)
	}
}

// Away from the terminal there is no tab in view to broadcast to, so the press
// is not taken and stays with the page that has the keyboard.
func TestBroadcastShortcutLeavesOtherPagesAlone(t *testing.T) {
	state, _, _, _, _ := twoPanes(t)
	showShortcutSettings(state)
	if result := pressChord(state, state.shortcuts.chords.Chord(shortcutBroadcast)); result.Handled || state.broadcast != 0 {
		t.Fatalf("shortcut acted outside the terminal: result=%+v broadcast=%d", result, state.broadcast)
	}
}

// The chord is rebindable and clearable like any built-in one: a cleared
// broadcast key is no longer taken, so the terminal program receives it, and a
// user's existing binding on the new default is never replaced.
func TestBroadcastShortcutCanBeReboundClearedAndNeverTakesAUsersChord(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	showShortcutSettings(state)
	state.Click(hitShortcutRowList.At(int(shortcutBroadcast - 1)))
	if state.modal != modalShortcutCapture || state.shortcutCapture != shortcutBroadcast {
		t.Fatal("the broadcast settings row did not open shortcut capture")
	}
	custom := keymap.Chord{Modifiers: keymap.Primary | gfx.ModifierAlt, Char: 'x'}
	pressChord(state, custom)
	restored := newTestState(t, options)
	if restored.shortcuts.chords.Chord(shortcutBroadcast) != custom {
		t.Fatal("the custom broadcast shortcut was not persisted")
	}
	if got := restored.shortcuts.actionFor(defaultShortcuts().chords.Chord(shortcutBroadcast)); got != shortcutNone {
		t.Fatalf("the old default still runs %v after rebinding", got)
	}

	restored.openShortcutCapture(shortcutBroadcast)
	restored.Click(hitShortcutCaptureDelete)
	restored = newTestState(t, options)
	chord := defaultShortcuts().chords.Chord(shortcutBroadcast)
	if restored.shortcuts.chords.Chord(shortcutBroadcast).Valid() {
		t.Fatal("a cleared broadcast shortcut returned after restarting")
	}
	restored.activeSection = sectionTerminal
	if result := pressChord(restored, chord); result.Handled {
		t.Fatal("a cleared broadcast shortcut still took the key from the terminal")
	}

	taken := restoreShortcuts([]savedShortcut{
		{Action: "find", Key: string(chord.Char), Modifiers: uint8(chord.Modifiers)},
	})
	if taken.chords.Chord(shortcutFind) != chord || taken.chords.Chord(shortcutBroadcast).Valid() {
		t.Fatal("the new default replaced a chord the user had already bound")
	}
}

// A broadcast to all tabs is for servers that were opened one tab each: typing
// into any of them reaches every connected terminal, and it survives moving
// between the tabs, which a broadcast of one tab does not.
func TestBroadcastToAllTabsReachesEveryTabAndSurvivesSwitchingTabs(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	var ids []layoutcore.WorkspaceID
	var sessions [][]Session
	for _, host := range []Host{testWorkspaceHost("web", "web.example.com"), testWorkspaceHost("db", "db.example.com")} {
		workspaceID, launched, err := state.launchWorkspace(WorkspaceLaunchPlan{Hosts: []Host{host}, Layout: layoutcore.GridSpec(1)}, host.Label)
		if err != nil {
			t.Fatal(err)
		}
		ids, sessions = append(ids, workspaceID), append(sessions, launched)
	}
	for index := range world.conns {
		world.conns[index].set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	}
	state.Wake()
	for _, launched := range sessions {
		session, _ := state.sessionByIDPointer(launched[0].ID)
		session.term.live = true
	}
	web, db := world.conns[0].shell(t), world.conns[1].shell(t)
	state.activateWorkspace(ids[1])
	state.activeSection = sectionTerminal

	if result := pressChord(state, state.shortcuts.chords.Chord(shortcutBroadcastAll)); !result.Handled || !state.broadcastAll {
		t.Fatalf("shortcut did not start the broadcast: result=%+v status=%q", result, state.status)
	}
	insertText(state, sessions[1][0].ID, "uptime")
	if web.typed() != "uptime" || db.typed() != "uptime" {
		t.Fatalf("typed into one tab: web %q, db %q", web.typed(), db.typed())
	}

	state.activateWorkspace(ids[0])
	insertText(state, sessions[0][0].ID, "!")
	if !state.broadcastAll || web.typed() != "uptime!" || db.typed() != "uptime!" {
		t.Fatalf("after switching tabs: on=%v web %q, db %q", state.broadcastAll, web.typed(), db.typed())
	}

	state.Click(hitBroadcastTab)
	insertText(state, sessions[0][0].ID, "x")
	if state.broadcastAll || db.typed() != "uptime!" {
		t.Fatalf("after stopping: on=%v db %q", state.broadcastAll, db.typed())
	}
}

// Anything that changes who a keystroke would reach ends the broadcast, as it
// does for one tab.
func TestBroadcastToAllTabsStopsWhenAConnectionStarts(t *testing.T) {
	state, _, _, firstID, _ := twoPanes(t)
	state.toggleBroadcastAll()
	if !state.broadcastAll {
		t.Fatalf("broadcast did not start: %q", state.status)
	}
	session, _ := state.sessionByIDPointer(firstID)
	state.startConnectionMode(session, session.host, false)
	if state.broadcastAll {
		t.Fatal("broadcast kept running while a connection restarted")
	}
}

// The toolbar's broadcast button opens a list of tabs to tick, and the broadcast
// reaches only the ticked ones.
func TestBroadcastButtonLetsTheUserPickTabs(t *testing.T) {
	state, _, _, firstID, secondID := twoPanes(t)
	state.Click(hitBroadcast)
	if !state.broadcastMenu.open || state.broadcastOn() {
		t.Fatalf("button: menu open=%v, broadcast on=%v; want the list and no broadcast yet", state.broadcastMenu.open, state.broadcastOn())
	}
	state.Click(hitBroadcastMenuGo)
	if state.broadcastMenu.open {
		t.Fatal("starting left the list open")
	}
	if !state.broadcastOn() || !state.broadcasting(firstID) || !state.broadcasting(secondID) {
		t.Fatalf("start over the ticked tabs: status %q", state.status)
	}
	state.Click(hitBroadcast)
	state.Click(hitBroadcastMenuGo)
	if state.broadcastOn() {
		t.Fatal("stop left the broadcast on")
	}
}

// A tab left unticked in the list is not broadcast to.
func TestBroadcastReachesOnlyTheTickedTabs(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	var ids []layoutcore.WorkspaceID
	var first []Session
	for index, host := range []Host{testWorkspaceHost("web", "web.example.com"), testWorkspaceHost("db", "db.example.com"), testWorkspaceHost("cache", "cache.example.com")} {
		workspaceID, launched, err := state.launchWorkspace(WorkspaceLaunchPlan{Hosts: []Host{host}, Layout: layoutcore.GridSpec(1)}, host.Label)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, workspaceID)
		if index == 0 {
			first = launched
		}
	}
	for index := range world.conns {
		world.conns[index].set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	}
	state.Wake()
	for index := range state.sessions {
		state.sessions[index].term.live = true
	}
	web, db, cache := world.conns[0].shell(t), world.conns[1].shell(t), world.conns[2].shell(t)
	state.activateWorkspace(ids[0])
	state.activeSection = sectionTerminal

	state.Click(hitBroadcast)
	state.Click(hitBroadcastMenuTabs.At(2)) // untick the third tab
	state.Click(hitBroadcastMenuGo)
	insertText(state, first[0].ID, "ls")
	if web.typed() != "ls" || db.typed() != "ls" || cache.typed() != "" {
		t.Fatalf("web %q, db %q, cache %q; want the unticked tab left alone", web.typed(), db.typed(), cache.typed())
	}

	// "This tab" ticks only the one in view, "All tabs" every tab.
	state.Click(hitBroadcast)
	state.Click(hitBroadcastMenuGo) // stop
	state.Click(hitBroadcast)
	state.Click(hitBroadcastMenuCurrent)
	if len(state.broadcastMenu.picked) != 1 || state.broadcastMenu.picked[0] != ids[0] {
		t.Fatalf("this tab picked %v", state.broadcastMenu.picked)
	}
	state.Click(hitBroadcastMenuAll)
	if len(state.broadcastMenu.picked) != len(ids) {
		t.Fatalf("all tabs picked %v", state.broadcastMenu.picked)
	}
}
