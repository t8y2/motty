//go:build darwin || windows || linux

package workbench

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/text"
	"motty/internal/i18n"
	"motty/internal/term"
)

func TestCommandSelectionSnippetDialogUsesPageControls(t *testing.T) {
	s, _, _, shell := liveSession(t)
	view, err := headless.Open(s.WindowConfig(app.Config{
		Title: "Motty", Width: 1400, Height: 900, Tree: Tree(), Paint: s.Paint,
		OnKey: s.Key, OnShortcut: s.Shortcut, OnPointer: s.Pointer, OnScroll: s.Scroll, OnResize: s.Resize,
		TextInput: s.TerminalInput,
	}), nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	shell.say("\x1b[2J\x1b[Hecho first\r\necho second")
	s.Wake()
	session, _ := s.sessionByIDPointer(s.activeSessionID)
	session.term.selection.selectRange(
		session.term.screen.PointAt(term.Position{}),
		session.term.screen.PointAt(term.Position{Row: 1, Column: 10}),
	)
	if err := view.Frame(); err != nil {
		t.Fatal(err)
	}
	if err := view.RightClick(400, 300); err != nil {
		t.Fatal(err)
	}
	if err := view.ClickLabel(s.messages.Tools.SidebarFromSelection); err != nil {
		t.Fatal(err)
	}
	if s.modal != modalSnippet || s.terminalMenu.open || s.snippets.run {
		t.Fatalf("selection capture did not open a non-executing snippet: modal=%v menu=%v run=%v", s.modal, s.terminalMenu.open, s.snippets.run)
	}
	if got := s.snippets.command.String(); got != "echo first\necho second" {
		t.Fatalf("captured command = %q", got)
	}
	click := func(id gfx.ID) {
		t.Helper()
		hit, ok := hitByIDSnapshot(view.Snapshot().Hits, id)
		if !ok {
			t.Fatalf("missing control %s", tree.Text(id))
		}
		if err := view.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
			t.Fatal(err)
		}
	}
	click(hitSnippetCommand)
	if err := view.Keys("cmd+a"); err != nil {
		t.Fatal(err)
	}
	const edited = "echo updated\nprintf done"
	if err := view.Type(edited); err != nil {
		t.Fatal(err)
	}
	click(hitSnippetPin)
	if !s.snippets.pinned {
		t.Fatal("page checkbox did not pin the captured snippet")
	}
	if err := view.Expect("layout", "clean"); err != nil {
		t.Fatal(err)
	}
	if err := view.ClickLabel(s.messages.Tools.SnippetSave); err != nil {
		t.Fatal(err)
	}
	if s.modal != modalNone || len(s.snippets.list) != 1 {
		t.Fatalf("page save did not finish: modal=%v snippets=%d", s.modal, len(s.snippets.list))
	}
	entry := s.snippets.list[0]
	if entry.Name != "echo first" || entry.Command != edited || !entry.Pinned || entry.Run || shell.typed() != "" {
		t.Fatalf("captured snippet = %+v; terminal received %q", entry, shell.typed())
	}
}

func TestCommandSidebarNativeDraftFocusUndoAndLayout(t *testing.T) {
	testCommandSidebarNativeDraftFocusUndoAndLayout(t, false)
}

func TestCommandSidebarCoexistsWithHostsPanel(t *testing.T) {
	testCommandSidebarNativeDraftFocusUndoAndLayout(t, true)
}

func testCommandSidebarNativeDraftFocusUndoAndLayout(t *testing.T, hostsPanelOpen bool) {
	t.Helper()
	s, _, _, shell := liveSession(t)
	shell.say("Welcome to Motty command sidebar preview\r\n\r\n$ df -h\r\nFilesystem      Size  Used Avail Use%\r\n/dev/sda1       100G   24G   76G  24%\r\n\r\n$ ")
	s.Wake()
	s.setLanguage(i18n.Chinese)
	s.snippets.list = []snippet{
		{Name: "查看磁盘", Command: "df -h", Run: true, Pinned: true},
		{Name: "Docker 日志", Command: "docker logs --tail 50 app", Run: true},
		{Name: "系统负载", Command: "uptime", Run: true},
	}
	s.refreshSnippets()
	if hostsPanelOpen {
		s.Click(hitHostsPanel)
	}
	view, err := headless.Open(s.WindowConfig(app.Config{
		Title: "Motty", Width: 1400, Height: 900, Tree: Tree(), Paint: s.Paint,
		OnKey: s.Key, OnShortcut: s.Shortcut, OnPointer: s.Pointer, OnScroll: s.Scroll, OnResize: s.Resize,
		TextInput: func(context app.TextInputContext) *app.TextInput {
			if context.Terminal {
				return s.TerminalInput(context)
			}
			if input := s.CommandComposerInput(context); input != nil {
				return input
			}
			if editor := s.EditorFor(context.ID); editor != nil {
				return &app.TextInput{Editor: editor}
			}
			return nil
		},
	}), nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	click := func(id gfx.ID) {
		t.Helper()
		hit, ok := hitByIDSnapshot(view.Snapshot().Hits, id)
		if !ok {
			t.Fatalf("missing control %s", tree.Text(id))
		}
		if err := view.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
			t.Fatal(err)
		}
	}
	if hostsPanelOpen {
		click(hitHostsPanelSearch)
		if err := view.Type("panel search"); err != nil {
			t.Fatalf("session-manager search input: %v; focus=%s", err, tree.Text(view.Snapshot().Focused))
		}
		if s.hostsPanel.search.Text() != "panel search" || s.commandSidebar.editor.String() != "" {
			t.Fatal("session-manager search input reached the command draft")
		}
	}
	click(hitCommandPinnedInsert.At(0))
	if s.commandSidebar.editor.String() != "df -h" {
		t.Fatal("native pin click did not insert")
	}
	if err := view.Type(" # draft"); err != nil {
		hit, _ := hitByIDSnapshot(view.Snapshot().Hits, hitCommandComposer)
		t.Fatalf("%v; focus=%s pending=%v composer=%+v", err, tree.Text(view.Snapshot().Focused), s.commandSidebar.focusDraft, hit)
	}
	if s.commandSidebar.editor.String() != "df -h # draft" {
		t.Fatalf("pin insertion did not focus the draft at its end: %q", s.commandSidebar.editor.String())
	}
	if err := view.Keys("cmd+z"); err != nil {
		t.Fatal(err)
	}
	if s.commandSidebar.editor.String() != "df -h" {
		t.Fatalf("native undo did not restore the draft: %q", s.commandSidebar.editor.String())
	}
	// Repeated library clicks leave focus in the composer, and never close it.
	click(hitCommandSidebarRun.At(2))
	if err := view.Type(" # retained focus"); err != nil {
		t.Fatal(err)
	}
	if s.commandSidebar.editor.String() != "df -h # retained focus" {
		t.Fatal("executing a library command stole composer focus")
	}
	for _, size := range []gfx.Size{{Width: 1400, Height: 900}, {Width: 900, Height: 640}} {
		if err := view.Resize(int(size.Width), int(size.Height)); err != nil {
			t.Fatal(err)
		}
		if err := view.Rest(); err != nil {
			t.Fatal(err)
		}
		if err := view.Expect("layout", "clean"); err != nil {
			t.Fatal(err)
		}
		if hostsPanelOpen {
			search, ok := hitByIDSnapshot(view.Snapshot().Hits, hitHostsPanelSearch)
			if !ok {
				t.Fatal("the command sidebar displaced the session manager")
			}
			panes := s.workspacePixelBounds(size)
			if search.Bounds.X+search.Bounds.Width > panes.X || panes.Width < aiMinTerminal {
				t.Fatalf("session manager overlaps or crowds the terminals: search=%+v panes=%+v", search.Bounds, panes)
			}
			if composer, ok := hitByIDSnapshot(view.Snapshot().Hits, hitCommandComposer); ok && composer.Bounds.X < panes.X+panes.Width {
				t.Fatalf("command draft overlaps the terminals: composer=%+v panes=%+v", composer.Bounds, panes)
			}
			if s.hostsPanel.search.Text() != "panel search" {
				t.Fatal("command draft input changed the session-manager search")
			}
		}
		seen := make(map[gfx.ID]bool)
		for _, hit := range view.Snapshot().Hits {
			if hit.ID.Scope() == commandSidebarHits {
				if seen[hit.ID] {
					t.Fatalf("duplicate sidebar control: %s", tree.Text(hit.ID))
				}
				seen[hit.ID] = true
			}
		}
		if dir := os.Getenv("MOTTY_COMMAND_SCREENSHOTS"); dir != "" {
			if hostsPanelOpen {
				dir = filepath.Join(dir, "with-hosts-panel")
			}
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			name := "command-sidebar.png"
			if size.Width < 1000 {
				name = "command-sidebar-narrow.png"
			}
			if err := view.Screenshot(filepath.Join(dir, name)); err != nil {
				t.Fatal(err)
			}
		}
	}
	s.commandSidebar.editor = text.Editor{}
	s.commandSidebar.editor.Insert("rm -rf /")
	s.sendCommandDraft(false)
	if err := view.Frame(); err != nil {
		t.Fatal(err)
	}
	if err := view.Expect("layout", "clean"); err != nil {
		t.Fatal(err)
	}
	if dir := os.Getenv("MOTTY_COMMAND_SCREENSHOTS"); dir != "" {
		if hostsPanelOpen {
			dir = filepath.Join(dir, "with-hosts-panel")
		}
		if err := view.Screenshot(filepath.Join(dir, "command-sidebar-review.png")); err != nil {
			t.Fatal(err)
		}
	}
}
