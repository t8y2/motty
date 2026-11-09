package workbench

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"motty/internal/term"
	layoutcore "motty/internal/workspace"
)

func rightClick(state *State, session *Session, x, y float32) gfx.EventResult {
	return state.Pointer(gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonSecondary, Target: hitTerminalList.At(int(session.ID)),
		X: x, Y: y, Viewport: state.viewport,
	})
}

func TestRightClickOpensTheTerminalMenuAtThePointer(t *testing.T) {
	state, _, _, _ := liveSession(t)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, hitTerminalMenuCopy); ok {
		t.Fatal("the menu is up before any click")
	}
	if result := rightClick(state, session, 400, 300); !result.Handled || !result.Repaint {
		t.Fatalf("right click = %+v", result)
	}
	canvas = gfx.Canvas{}
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	copyItem, ok := hitByID(&canvas, hitTerminalMenuCopy)
	if !ok {
		t.Fatal("no menu after a right click")
	}
	if copyItem.Bounds.X < 400 || copyItem.Bounds.X > 420 || copyItem.Bounds.Y < 300 || copyItem.Bounds.Y > 320 {
		t.Fatalf("menu starts at %+v, want at the pointer", copyItem.Bounds)
	}
	for _, id := range []gfx.ID{hitTerminalMenuPaste, hitTerminalMenuSelectAll, hitTerminalMenuClearScreen, hitTerminalMenuClear, hitTerminalMenuSplitRight, hitTerminalMenuSplitDown, hitTerminalMenuFiles} {
		if _, ok := hitByID(&canvas, id); !ok {
			t.Errorf("menu item %d missing", id)
		}
	}
}

func TestTerminalMenuStaysInsideTheWindow(t *testing.T) {
	state, _, _, _ := liveSession(t)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	state.Paint(&gfx.Canvas{}, gfx.Size{Width: 1400, Height: 900})
	rightClick(state, session, 1395, 895)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	last, ok := hitByID(&canvas, hitTerminalMenuFiles)
	if !ok || last.Bounds.X+last.Bounds.Width > 1400 || last.Bounds.Y+last.Bounds.Height > 900 {
		t.Fatalf("the menu runs off the window: %+v, %v", last.Bounds, ok)
	}
}

// A right click on one pane of a split does not move focus, so the menu's close
// row must close the pane it was opened on, and only while there is a split to
// close: a tab of one pane would lose the tab instead.
func TestTheTerminalMenuClosesThePaneItWasOpenedOn(t *testing.T) {
	state, _, _, _ := liveSession(t)
	first, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok {
		t.Fatal("no session to split")
	}
	firstID := first.ID
	openHostInSplit(t, state, 2, layoutcore.Horizontal)
	second, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || second.ID == firstID {
		t.Fatalf("split active session = %v, want a second one", state.activeSessionID)
	}
	secondID := second.ID
	workspace := state.deck.ActiveWorkspace()
	if panes := state.deck.Panes(workspace, nil); len(panes) != 2 {
		t.Fatalf("panes = %+v", panes)
	}

	state.terminalMenu = terminalMenu{open: true, session: firstID}
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, hitTerminalMenuClosePane); !ok {
		t.Fatal("a split tab's menu does not offer closing the pane")
	}
	state.Click(hitTerminalMenuClosePane)
	confirmRunningClose(state)
	if _, ok := state.sessionByIDPointer(firstID); ok {
		t.Fatal("the pane the menu was opened on is still open")
	}
	if panes := state.deck.Panes(workspace, nil); len(panes) != 1 || panes[0].SessionID != layoutcore.SessionID(secondID) {
		t.Fatalf("panes after closing = %+v, want the other pane", panes)
	}
	if state.deck.ActiveWorkspace() != workspace {
		t.Fatal("closing a pane closed the tab")
	}

	// One pane left: the row would close the tab, which this menu does not do.
	state.terminalMenu = terminalMenu{open: true, session: secondID}
	canvas = gfx.Canvas{}
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, hitTerminalMenuClosePane); ok {
		t.Fatal("a tab of one pane offers closing the pane")
	}
}

func TestTerminalMenuActions(t *testing.T) {
	state, _, _, shell := liveSession(t)
	var copied string
	state.options.SetClipboardText = func(value string) error { copied = value; return nil }
	state.options.ClipboardText = func() (string, error) { return "from clipboard", nil }
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	shell.say("hello world\r\n")
	state.Wake()
	state.Paint(&gfx.Canvas{}, gfx.Size{Width: 1400, Height: 900})

	open := func() {
		rightClick(state, session, 400, 300)
		state.Paint(&gfx.Canvas{}, gfx.Size{Width: 1400, Height: 900})
	}

	// Copy is offered only with a selection.
	open()
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if hit, _ := hitByID(&canvas, hitTerminalMenuCopy); !hit.Disabled {
		t.Fatal("Copy is enabled without a selection")
	}
	state.Click(hitTerminalMenuSelectAll)
	if !session.term.selection.selected || state.terminalMenu.open {
		t.Fatalf("Select All: selected=%v open=%v", session.term.selection.selected, state.terminalMenu.open)
	}

	open()
	state.Click(hitTerminalMenuCopy)
	if !strings.Contains(copied, "hello world") {
		t.Fatalf("copied %q", copied)
	}

	open()
	state.Click(hitTerminalMenuPaste)
	if got := shell.typed(); !strings.Contains(got, "from clipboard") {
		t.Fatalf("shell received %q", got)
	}
}

func TestTerminalMenuClosesOnOutsidePressAndEscape(t *testing.T) {
	state, _, _, _ := liveSession(t)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	state.Paint(&gfx.Canvas{}, gfx.Size{Width: 1400, Height: 900})
	rightClick(state, session, 400, 300)
	state.Paint(&gfx.Canvas{}, gfx.Size{Width: 1400, Height: 900})
	state.Click(hitTerminalMenuDismiss)
	if state.terminalMenu.open {
		t.Fatal("an outside click left the menu open")
	}
	rightClick(state, session, 400, 300)
	if result := state.Key(gfx.KeyEvent{Key: gfx.KeyEscape}); !result.Handled || state.terminalMenu.open {
		t.Fatalf("Escape: %+v open=%v", result, state.terminalMenu.open)
	}
	rightClick(state, session, 400, 300)
	state.Paint(&gfx.Canvas{}, gfx.Size{Width: 1400, Height: 900})
	if result := state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonSecondary, Target: hitTerminalMenuDismiss}); !result.Handled || state.terminalMenu.open {
		t.Fatalf("a second right click outside: %+v open=%v", result, state.terminalMenu.open)
	}
}

func TestTerminalMenuYieldsToMouseReportingPrograms(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	shell.say("\x1b[?1000h\x1b[?1006h")
	state.Wake()
	state.Paint(&gfx.Canvas{}, gfx.Size{Width: 1400, Height: 900})
	rightClick(state, session, 400, 300)
	if state.terminalMenu.open {
		t.Fatal("the menu took a right click that belongs to a program using the mouse")
	}
	// Shift takes the press back from the program.
	state.Pointer(gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonSecondary, Modifiers: gfx.ModifierShift,
		Target: hitTerminalList.At(int(session.ID)), X: 400, Y: 300, Viewport: state.viewport,
	})
	if !state.terminalMenu.open {
		t.Fatal("Shift+right click does not open the menu")
	}
}

func TestClearScrollbackKeepsTheScreen(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	shell.say(strings.Repeat("line\r\n", 200))
	state.Wake()
	if session.term.screen.Scrollback() == 0 {
		t.Fatal("no scrollback to clear")
	}
	state.terminalMenu = terminalMenu{open: true, session: session.ID}
	state.Paint(&gfx.Canvas{}, gfx.Size{Width: 1400, Height: 900})
	state.Click(hitTerminalMenuClear)
	if session.term.screen.Scrollback() != 0 || !strings.Contains(session.term.screen.RowText(0), "line") {
		t.Fatalf("scrollback=%d row0=%q", session.term.screen.Scrollback(), session.term.screen.RowText(0))
	}
}

func TestTheMenuClearsTheScreenAndKeepsThePromptLine(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	shell.say(strings.Repeat("line\r\n", 200))
	shell.say("prompt$ ")
	state.Wake()
	state.terminalMenu = terminalMenu{open: true, session: session.ID}
	state.Paint(&gfx.Canvas{}, gfx.Size{Width: 1400, Height: 900})
	state.Click(hitTerminalMenuClearScreen)
	if session.term.screen.Scrollback() != 0 || !strings.Contains(session.term.screen.RowText(0), "prompt$") {
		t.Fatalf("scrollback=%d row0=%q", session.term.screen.Scrollback(), session.term.screen.RowText(0))
	}
}

func TestTheMenuOffersNoClearToAFullScreenProgram(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	shell.say("\x1b[?1049h\x1b[Hfull screen")
	state.Wake()
	before := session.term.screen.Text()
	state.terminalMenu = terminalMenu{open: true, session: session.ID}
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	row, ok := hitByID(&canvas, hitTerminalMenuClearScreen)
	if !ok || !row.Disabled {
		t.Fatalf("the clear screen row = %+v, %v; want it disabled over a program", row, ok)
	}
	state.Click(hitTerminalMenuClearScreen)
	if got := session.term.screen.Text(); got != before {
		t.Fatalf("the program's screen changed to %q, want %q", got, before)
	}
}

func TestTerminalMenuUsesCheckboxesForMonitorAndFiles(t *testing.T) {
	state, _, _, _ := liveSession(t)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	state.Paint(&gfx.Canvas{}, gfx.Size{Width: 1400, Height: 900})
	rightClick(state, session, 400, 300)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	stats, ok := hitByID(&canvas, hitTerminalMenuStats)
	if !ok {
		t.Fatal("the menu has no resource monitor row on a live shell")
	}
	if stats.Checked {
		t.Fatal("resource monitor starts checked")
	}
	files, ok := hitByID(&canvas, hitTerminalMenuFiles)
	if !ok {
		t.Fatal("the menu has no files row")
	}
	if files.Checked {
		t.Fatal("files starts checked")
	}
	if session.stats != nil {
		t.Fatal("the monitor is running before it was asked for")
	}
	state.Click(hitTerminalMenuStats)
	if session.stats == nil {
		t.Fatal("the menu row did not start the monitor")
	}
	if state.terminalMenu.open {
		t.Fatal("the menu stayed open after a choice")
	}

	rightClick(state, session, 400, 300)
	canvas.Reset()
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if stats, ok = hitByID(&canvas, hitTerminalMenuStats); !ok || !stats.Checked {
		t.Fatalf("running monitor checkbox = %+v, %v", stats, ok)
	}
	if files, ok = hitByID(&canvas, hitTerminalMenuFiles); !ok || files.Checked {
		t.Fatalf("closed files checkbox = %+v, %v", files, ok)
	}
	state.Click(hitTerminalMenuFiles)
	if !state.filesPanelShown() {
		t.Fatal("the files checkbox row did not open the panel")
	}
	rightClick(state, session, 400, 300)
	canvas.Reset()
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if files, ok = hitByID(&canvas, hitTerminalMenuFiles); !ok || !files.Checked {
		t.Fatalf("open files checkbox = %+v, %v", files, ok)
	}
	state.stopRemoteStats(session)
}

func TestTerminalMenuSavesTheOutputAndSearchesTheSelection(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	shell.say("first line\r\nsecond ünïcode line\r\n")
	state.Wake()
	state.Paint(&gfx.Canvas{}, gfx.Size{Width: 1400, Height: 900})

	saved := filepath.Join(t.TempDir(), "out.txt")
	state.options.SaveFile = func(app.SaveFileOptions) (string, error) { return saved, nil }
	var opened string
	state.options.OpenURL = func(address string) error { opened = address; return nil }
	open := func() {
		rightClick(state, session, 400, 300)
		state.Paint(&gfx.Canvas{}, gfx.Size{Width: 1400, Height: 900})
	}

	open()
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if hit, ok := hitByID(&canvas, hitTerminalMenuSearch); !ok || !hit.Disabled {
		t.Fatalf("Search is usable without a selection: found %v", ok)
	}
	state.Click(hitTerminalMenuSave)
	data, err := os.ReadFile(saved)
	if err != nil || !strings.Contains(string(data), "first line\nsecond ünïcode line") {
		t.Fatalf("saved %q, %v", data, err)
	}
	if !strings.Contains(state.status, saved) {
		t.Fatalf("status %q", state.status)
	}

	open()
	session.term.selection.selectRange(
		session.term.screen.PointAt(term.Position{Row: 1}),
		session.term.screen.PointAt(term.Position{Row: 1, Column: 20}))
	open()
	state.Click(hitTerminalMenuSearch)
	if !strings.HasPrefix(opened, "https://www.bing.com/search?q=second+") || strings.ContainsAny(opened, "\n ") {
		t.Fatalf("opened %q", opened)
	}
}

// rightClickOn presses the secondary button over the first occurrence of text on
// the screen, which must be painted.
func rightClickOn(t *testing.T, state *State, session *Session, needle string) {
	t.Helper()
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	for _, text := range canvas.Texts() {
		if at := strings.Index(text.Value, needle); at >= 0 {
			x := text.Bounds.X + (float32(at)+float32(len(needle))/2)*state.cellWidth
			y := text.Bounds.Y + state.cellHeight/2
			rightClick(state, session, x, y)
			state.Paint(&gfx.Canvas{}, gfx.Size{Width: 1400, Height: 900})
			return
		}
	}
	t.Fatalf("%q is not on the screen", needle)
}

func TestTheMenuOffersWhatTheTextUnderThePointerCouldBe(t *testing.T) {
	state, world, _, shell := liveSession(t)
	var copied string
	state.options.SetClipboardText = func(value string) error { copied = value; return nil }
	var opened string
	state.options.OpenURL = func(url string) error { opened = url; return nil }
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	shell.say("listening on 10.0.0.5:8080\r\nbackup.tar.gz\r\n")
	state.Wake()

	var canvas gfx.Canvas
	rightClickOn(t, state, session, "10.0.0.5:8080")
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	var labels []string
	for _, text := range canvas.Texts() {
		if strings.Contains(text.Value, "10.0.0.5:8080") && !strings.Contains(text.Value, "listening") {
			labels = append(labels, text.Value)
		}
	}
	if len(labels) != 3 {
		t.Fatalf("the menu offers %q, want connect, browser and copy rows naming the address", labels)
	}
	if _, ok := hitByID(&canvas, hitTerminalMenuAction[1]); !ok {
		t.Fatal("the browser row has no hit")
	}
	state.Click(hitTerminalMenuAction[1])
	if opened != "http://10.0.0.5:8080" {
		t.Fatalf("opened %q", opened)
	}

	rightClickOn(t, state, session, "10.0.0.5:8080")
	state.Click(hitTerminalMenuAction[2])
	if copied != "10.0.0.5:8080" {
		t.Fatalf("copied %q", copied)
	}

	// An archive's unpack line is typed at the prompt and not run.
	rightClickOn(t, state, session, "backup.tar.gz")
	state.Click(hitTerminalMenuAction[0])
	if got := shell.typed(); got != "mkdir -- 'backup' && tar -xf 'backup.tar.gz' -C 'backup'" {
		t.Fatalf("typed %q", got)
	}

	// Plain words get the ordinary menu and nothing more.
	rightClickOn(t, state, session, "listening")
	if state.terminalMenu.count != 0 {
		t.Fatalf("a plain word offered %d actions", state.terminalMenu.count)

		before := len(world.configs)
		rightClickOn(t, state, session, "10.0.0.5:8080")
		state.Click(hitTerminalMenuAction[0])
		if len(world.configs) != before+1 || world.configs[before].Host != "10.0.0.5" || world.configs[before].Port != 8080 {
			t.Fatalf("connection configs after the click: %+v", world.configs[before:])
		}
	}
}
