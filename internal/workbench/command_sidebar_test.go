package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"motty/internal/term"
)

func TestCommandSidebarDefaultsAndCollapseSurviveRestart(t *testing.T) {
	world, options := newSharingWorld(t)
	s := newTestState(t, options)
	world.connectFirst(t, s)
	if !s.commandSidebarVisible() {
		t.Fatal("command library must be expanded by default")
	}
	wide := gfx.Size{Width: 1400, Height: 900}
	before := s.workspacePixelBounds(wide)
	s.Click(hitCommandSidebarToggle)
	after := s.workspacePixelBounds(wide)
	if after.Width <= before.Width || !s.commandSidebar.Collapsed {
		t.Fatal("collapsing the library did not give space back to the terminal")
	}
	restarted := newTestState(t, options)
	if !restarted.commandSidebar.Collapsed {
		t.Fatal("collapsed library reopened after restart")
	}
}

func TestCommandSidebarRepeatedClicksUseTheActiveTerminal(t *testing.T) {
	world, options := newSharingWorld(t)
	s := newTestState(t, options)
	world.connectFirst(t, s)
	s.snippets.list = []snippet{{Name: "List", Command: "ls", Run: true}}
	s.refreshSnippets()
	id := s.activeSessionID
	for range 3 {
		s.Click(hitCommandSidebarRun.At(0))
	}
	if got := world.conns[0].shell(t).typed(); got != "ls\rls\rls\r" {
		t.Fatalf("repeated command clicks sent %q", got)
	}
	if s.modal != modalNone || s.activeSessionID != id || !s.commandSidebarVisible() {
		t.Fatal("sending moved the target or closed the command library")
	}
	var canvas gfx.Canvas
	s.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	hit, ok := hitByID(&canvas, hitCommandSidebarRun.At(0))
	if !ok || hit.Focusable || hit.Hint != "ls" {
		t.Fatalf("command chip must preserve focus and preview its command: %+v", hit)
	}
}

func TestTerminalSelectionCreatesAPinnedSnippetWithoutExecuting(t *testing.T) {
	world, options := newSharingWorld(t)
	s := newTestState(t, options)
	world.connectFirst(t, s)
	session, _ := s.sessionByIDPointer(s.activeSessionID)
	session.term.screen.Write([]byte("echo first\r\necho second"))
	session.term.selection.selectRange(session.term.screen.PointAt(term.Position{}), session.term.screen.PointAt(term.Position{Row: 1, Column: 10}))
	s.openTerminalMenu(session)
	clickPageMenuRow(t, s, s.messages.Tools.SidebarFromSelection)
	if s.modal != modalSnippet || s.snippets.name.String() != "echo first" || !strings.Contains(s.snippets.command.String(), "\necho second") {
		t.Fatalf("selection was not prefilled: name=%q command=%q", s.snippets.name.String(), s.snippets.command.String())
	}
	s.Click(hitSnippetPin)
	s.Click(hitSnippetSubmit)
	if len(s.snippets.list) != 1 || !s.snippets.list[0].Pinned || s.snippets.list[0].Run {
		t.Fatalf("saved selection = %+v", s.snippets.list)
	}
	if world.conns[0].shell(t).typed() != "" || s.commandSidebar.highlight != 1 {
		t.Fatal("saving a selection must highlight it without executing")
	}
	s.Click(hitCommandPinnedInsert.At(0))
	if s.commandSidebar.editor.String() != s.snippets.list[0].Command {
		t.Fatal("pinned command did not insert into draft")
	}
	s.openSnippetDialog(0)
	s.snippets.command.SetSelection(text.Range{Start: 0, End: len([]rune(s.snippets.command.String()))})
	s.snippets.command.Insert("echo updated")
	s.Click(hitSnippetSubmit)
	if !s.snippets.list[0].Pinned {
		t.Fatal("editing lost the pin")
	}
	s.commandSidebar.editor = text.Editor{}
	s.Click(hitCommandPinnedInsert.At(0))
	if s.commandSidebar.editor.String() != "echo updated" {
		t.Fatal("pin kept a stale copy of the command")
	}
	restarted := newTestState(t, options)
	if !restarted.snippets.list[0].Pinned || restarted.commandSidebar.editor.String() != "echo updated" {
		t.Fatal("pin or draft did not survive restart")
	}
	s.deleteSnippet(0)
	s.Click(hitCommandPinnedInsert.At(0))
	if len(s.snippets.list) != 0 {
		t.Fatal("deleting the snippet left a separate pinned copy")
	}
}

func TestCommandComposerMultilineSaveRetainClearAndIME(t *testing.T) {
	world, options := newSharingWorld(t)
	s := newTestState(t, options)
	world.connectFirst(t, s)
	apply := func(event app.TextEvent) {
		input := s.CommandComposerInput(app.TextInputContext{ID: hitCommandComposer})
		if input == nil {
			t.Fatal("composer is not available")
		}
		if _, err := input.OnEvent(event); err != nil {
			t.Fatal(err)
		}
	}
	apply(app.TextEvent{Kind: app.TextInsert, Text: "echo 一"})
	apply(app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextInsertNewline})
	apply(app.TextEvent{Kind: app.TextInsert, Text: "echo two"})
	draft := "echo 一\necho two"
	if s.commandSidebar.editor.String() != draft {
		t.Fatalf("Enter must insert a draft newline: %q", s.commandSidebar.editor.String())
	}
	restarted := newTestState(t, options)
	if restarted.commandSidebar.editor.String() != draft {
		t.Fatal("committed native input was not auto-saved")
	}
	session, _ := s.sessionByIDPointer(s.activeSessionID)
	session.term.screen.Write([]byte("\x1b[?2004h"))
	s.commandSidebar.editor.SetMarkedText("中", text.Range{Start: 1, End: 1})
	s.Key(gfx.KeyEvent{Target: hitCommandComposer, Key: gfx.KeyEnter, Modifiers: gfx.ModifierControl})
	if world.conns[0].shell(t).typed() != "" {
		t.Fatal("Ctrl+Enter executed input-method preedit")
	}
	s.commandSidebar.editor.Unmark()
	s.commandSidebar.editor = text.Editor{}
	s.commandSidebar.editor.Insert(draft)
	s.Key(gfx.KeyEvent{Target: hitCommandComposer, Key: gfx.KeyEnter, Modifiers: gfx.ModifierControl})
	if got := world.conns[0].shell(t).typed(); got != string(term.EncodePaste(draft, true))+"\r" {
		t.Fatalf("draft did not use the existing bracketed-paste channel: %q", got)
	}
	if s.commandSidebar.editor.String() != draft {
		t.Fatal("default send discarded the draft")
	}
	s.Click(hitCommandComposeClear)
	s.Click(hitCommandComposeSend)
	if s.commandSidebar.editor.String() != "" {
		t.Fatal("clear-after-send did not clear an accepted draft")
	}
	s.commandSidebar.editor.Insert("echo keep")
	session.term.shell = nil
	s.Click(hitCommandComposeSend)
	if s.commandSidebar.editor.String() != "echo keep" {
		t.Fatal("a failed send discarded the draft")
	}
}

func TestCommandComposerRiskConfirmationIsBoundToCommandAndTarget(t *testing.T) {
	world, options := newSharingWorld(t)
	s := newTestState(t, options)
	world.connectFirst(t, s)
	s.commandSidebar.editor.Insert("rm -rf /")
	s.Click(hitCommandComposeAllow) // cannot bypass the first review
	if world.conns[0].shell(t).typed() != "" || s.commandSidebar.approvedCommand == "" {
		t.Fatal("dangerous draft bypassed review")
	}
	s.commandSidebar.approvedSession++ // a different target invalidates approval
	s.Click(hitCommandComposeAllow)
	if world.conns[0].shell(t).typed() != "" {
		t.Fatal("approval was reused for a different terminal")
	}
	s.Click(hitCommandComposeAllow)
	if got := world.conns[0].shell(t).typed(); got != "rm -rf /\r" {
		t.Fatalf("confirmed draft sent %q", got)
	}
	if s.commandSidebar.approvedCommand != "" {
		t.Fatal("approval must be consumed after sending")
	}
}

func TestPinnedVariableCommandResolvesIntoDraftWithoutSending(t *testing.T) {
	world, options := newSharingWorld(t)
	s := newTestState(t, options)
	world.connectFirst(t, s)
	s.snippets.list = []snippet{{Name: "Tail", Command: "tail -n {{lines:20}} /var/log/messages", Pinned: true}}
	s.Click(hitCommandPinnedInsert.At(0))
	if s.modal != modalSnippetVariables || !s.snippetPrompt.toComposer {
		t.Fatal("pinned command skipped variable resolution")
	}
	s.submitSnippetVariables()
	if s.commandSidebar.editor.String() != "tail -n 20 /var/log/messages" || world.conns[0].shell(t).typed() != "" {
		t.Fatalf("variable resolution sent instead of inserting: %q", s.commandSidebar.editor.String())
	}
}
