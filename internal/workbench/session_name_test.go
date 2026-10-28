package workbench

import (
	"testing"

	"github.com/vivaui/viva/text"
	layoutcore "motty/internal/workspace"
)

func TestSessionNamesMatchTabsAndMentions(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	t.Cleanup(state.Close)
	host := state.hosts[0]
	for index, expected := range []string{host.Label, host.Label + "-2", host.Label + "-3"} {
		session := state.newHostSession(host)
		if session == nil || session.Title != expected {
			t.Fatalf("session %d = %+v, want %s", index, session, expected)
		}
		workspace, _, _ := state.deck.PaneForSession(layoutcore.SessionID(session.ID))
		tab, _ := state.workspaceTab(workspace)
		if state.workspaceTabs[tab].Title != expected {
			t.Fatalf("tab and session differ: %q / %q", state.workspaceTabs[tab].Title, expected)
		}
	}
	names := state.mentionNames(nil)
	if len(names) != 3 {
		t.Fatalf("mention count = %d", len(names))
	}
	for index, name := range names {
		if name.title != state.sessions[index].Title || name.name != mentionSlug(name.title) {
			t.Fatalf("mention and tab differ: %+v", name)
		}
	}
	last := state.sessions[2].ID
	state.closeSession(state.sessions[0].ID)
	if session, _ := state.sessionByIDPointer(last); session.Title != host.Label+"-3" {
		t.Fatal("closing a sibling renumbered a session")
	}
}

func TestSessionNameCollisionRenameAndRestore(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	t.Cleanup(state.Close)
	first := state.newHostSession(state.hosts[0]).ID
	second := state.newHostSession(state.hosts[0]).ID
	a, _ := state.sessionByIDPointer(first)
	b, _ := state.sessionByIDPointer(second)
	state.renameSession(a, "Prod DB")
	state.renameSession(b, "Prod-DB") // @ token normalization must also be unique
	if b.Title != "Prod-DB-2" {
		t.Fatalf("normalized collision = %q", b.Title)
	}
	workspace, _, _ := state.deck.PaneForSession(layoutcore.SessionID(second))
	state.rebuildTabOptions()
	state.openTabRename(workspace)
	state.tabRename.editor = *text.NewEditor("Prod DB")
	state.clickTabRenameDialog(hitTabRenameSubmit)
	if b.Title != "Prod DB-2" {
		t.Fatalf("tab rename did not rename session: %q", b.Title)
	}
	state.save()
	restored := newTestState(t, options)
	t.Cleanup(restored.Close)
	if len(restored.sessions) != 2 || restored.sessions[0].Title != "Prod DB" || restored.sessions[1].Title != "Prod DB-2" {
		t.Fatalf("restored names = %+v", restored.sessions)
	}
	for _, session := range restored.sessions {
		workspace, _, _ := restored.deck.PaneForSession(layoutcore.SessionID(session.ID))
		tab, _ := restored.workspaceTab(workspace)
		if restored.workspaceTabs[tab].Title != session.Title {
			t.Fatal("restored tab differs from session")
		}
	}
}

func TestMentionNormalizedCollisionsRemainUnambiguous(t *testing.T) {
	var state State
	for index, title := range []string{"Prod DB", "Prod-DB", "Prod DB.", "Prod DB:"} {
		state.sessions = append(state.sessions, Session{ID: uint32(index + 1), Title: state.uniqueSessionTitle(title, 0), term: &terminal{}})
	}
	names := state.mentionNames(nil)
	for _, name := range names {
		ids := findMentions("@"+name.name+" ", names)
		if len(ids) != 1 || ids[0] != name.session {
			t.Fatalf("mention %q resolves to %v, want %v", name.title, ids, name.session)
		}
	}
}
