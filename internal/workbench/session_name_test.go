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
	titles := []string{host.Label, host.Label + "-2", host.Label + "-3"}
	for index, expected := range titles {
		session := state.newHostSession(host)
		if session == nil || session.Title != expected {
			t.Fatalf("session %d = %+v, want title %s", index, session, expected)
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
		if name.title != state.sessions[index].Title || name.name != titles[index] {
			t.Fatalf("mention %d = %+v, want %q @%s", index, name, state.sessions[index].Title, titles[index])
		}
	}
	last := state.sessions[2].ID
	state.closeSession(state.sessions[0].ID)
	if session, _ := state.sessionByIDPointer(last); session.Title != host.Label+"-3" || session.Mention != host.Label+"-3" {
		t.Fatal("closing a sibling renumbered a session")
	}
}

func TestOpeningTheSameHostNumbersTheTitleButRenameMayRepeatIt(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	t.Cleanup(state.Close)
	host := state.hosts[0]
	first := state.newHostSession(host)
	second := state.newHostSession(host)
	if first.Title != host.Label || second.Title != host.Label+"-2" {
		t.Fatalf("auto titles = %q, %q", first.Title, second.Title)
	}
	state.renameSession(second, first.Title)
	if first.Title != second.Title {
		t.Fatalf("an explicit rename was numbered: %q / %q", first.Title, second.Title)
	}
	if first.Mention == second.Mention {
		t.Fatalf("duplicate titles share the @ name %q", first.Mention)
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
	if b.Title != "Prod-DB" || b.Mention != "Prod-DB-2" {
		t.Fatalf("normalized collision = %q @%q", b.Title, b.Mention)
	}
	workspace, _, _ := state.deck.PaneForSession(layoutcore.SessionID(second))
	state.rebuildTabOptions()
	state.openTabRename(workspace)
	state.tabRename.editor = *text.NewEditor("Prod DB")
	state.clickTabRenameDialog(hitTabRenameSubmit)
	if b.Title != "Prod DB" || b.Mention != "Prod-DB-2" {
		t.Fatalf("tab rename did not rename session: %q @%q", b.Title, b.Mention)
	}
	state.save()
	restored := newTestState(t, options)
	t.Cleanup(restored.Close)
	if len(restored.sessions) != 2 || restored.sessions[0].Title != "Prod DB" || restored.sessions[1].Title != "Prod DB" {
		t.Fatalf("restored names = %+v", restored.sessions)
	}
	if restored.sessions[0].Mention != "Prod-DB" || restored.sessions[1].Mention != "Prod-DB-2" {
		t.Fatalf("restored mentions = %+v", restored.sessions)
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
		session := Session{ID: uint32(index + 1), Title: title, term: &terminal{}}
		session.Mention = state.uniqueMentionName(session.Title, session.ID)
		state.sessions = append(state.sessions, session)
	}
	names := state.mentionNames(nil)
	for _, name := range names {
		ids := findMentions("@"+name.name+" ", names)
		if len(ids) != 1 || ids[0] != name.session {
			t.Fatalf("mention %q resolves to %v, want %v", name.name, ids, name.session)
		}
	}
}
