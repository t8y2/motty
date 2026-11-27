package workbench

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"motty/internal/localshell"
	"motty/internal/shellkind"
	"motty/internal/term"
	layoutcore "motty/internal/workspace"
)

func rightClick(state *State, session *Session, x, y float32) gfx.EventResult {
	return state.Pointer(gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonSecondary, Target: terminalTestHit(state, session),
		X: x, Y: y, Viewport: state.viewport,
	})
}

func TestRightClickOpensTheTerminalMenuAtThePointer(t *testing.T) {
	state, _, _, _ := liveSession(t)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if _, ok := menuHit(&canvas, state, hitTerminalMenuCopy); ok {
		t.Fatal("the menu is up before any click")
	}
	if result := rightClick(state, session, 400, 300); !result.Handled || !result.Repaint {
		t.Fatalf("right click = %+v", result)
	}
	canvas = gfx.Canvas{}
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	copyItem, ok := menuHit(&canvas, state, hitTerminalMenuCopy)
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

func TestTerminalSelectionMenuLabelsAndDisablesSelectionActions(t *testing.T) {
	state, _, _, _ := liveSession(t)
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok {
		t.Fatal("no active session")
	}
	state.Paint(&gfx.Canvas{}, gfx.Size{Width: 1400, Height: 900})
	rightClick(state, session, 400, 300)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	copyItem, copyOK := menuHit(&canvas, state, hitTerminalMenuCopy)
	pasteSelection, pasteOK := menuHit(&canvas, state, hitTerminalMenuPasteSelection)
	if !copyOK || copyItem.Label != state.messages.MenuCopy || !copyItem.Disabled {
		t.Fatalf("copy row = %+v, want disabled %q", copyItem, state.messages.MenuCopy)
	}
	if !pasteOK || pasteSelection.Label != state.messages.MenuPasteSelection || !pasteSelection.Disabled {
		t.Fatalf("paste selection row = %+v, want disabled %q", pasteSelection, state.messages.MenuPasteSelection)
	}
}

func TestTerminalMenuCCopiesTheSelection(t *testing.T) {
	state, session, geometry := selectionState()
	session.term.screen.Write([]byte("hello world"))
	session.term.selection.selectRange(
		session.term.screen.PointAt(term.Position{Row: 0, Column: 0}),
		session.term.screen.PointAt(term.Position{Row: 0, Column: 4}),
	)
	var copied string
	state.options.SetClipboardText = func(value string) error { copied = value; return nil }
	x, y := cellPoint(geometry, 0, 0, state.cellWidth, state.cellHeight)
	rightClick(state, session, x, y)
	result := state.Shortcut(gfx.KeyEvent{Target: terminalTestHit(state, session), Text: "c"})
	if !result.Handled || copied != "hello" || state.terminalMenu.open {
		t.Fatalf("C result = %+v, copied=%q, menu=%v", result, copied, state.terminalMenu.open)
	}
}

func TestTerminalMenuESendsTheSelectionThroughPasteReview(t *testing.T) {
	state, session, geometry := selectionState()
	shell := &fakeShell{done: make(chan struct{})}
	session.term.shell = shell
	session.term.screen.Write([]byte("one\r\ntwo"))
	session.term.selection.selectRange(
		session.term.screen.PointAt(term.Position{Row: 0, Column: 0}),
		session.term.screen.PointAt(term.Position{Row: 1, Column: 2}),
	)
	x, y := cellPoint(geometry, 0, 0, state.cellWidth, state.cellHeight)
	rightClick(state, session, x, y)
	result := state.Shortcut(gfx.KeyEvent{Target: terminalTestHit(state, session), Text: "e"})
	if !result.Handled || state.modal != modalPasteReview || state.terminalMenu.open || len(shell.input) != 0 {
		t.Fatalf("E result = %+v, modal=%v, menu=%v, input=%q", result, state.modal, state.terminalMenu.open, shell.input)
	}
}

func TestTerminalMenuESendsASingleLineSelection(t *testing.T) {
	state, session, geometry := selectionState()
	shell := &fakeShell{done: make(chan struct{})}
	session.term.shell = shell
	session.term.screen.Write([]byte("hello"))
	session.term.selection.selectRange(
		session.term.screen.PointAt(term.Position{Row: 0, Column: 0}),
		session.term.screen.PointAt(term.Position{Row: 0, Column: 4}),
	)
	x, y := cellPoint(geometry, 0, 0, state.cellWidth, state.cellHeight)
	rightClick(state, session, x, y)
	result := state.Shortcut(gfx.KeyEvent{Target: terminalTestHit(state, session), Text: "e"})
	if !result.Handled || string(shell.input) != "hello" || session.term.selection.selected || state.terminalMenu.open {
		t.Fatalf("E result = %+v, input=%q, selected=%v, menu=%v", result, shell.input, session.term.selection.selected, state.terminalMenu.open)
	}
}

func TestTerminalMenuCompositionMessages(t *testing.T) {
	state, session, geometry := selectionState()
	shell := &fakeShell{done: make(chan struct{})}
	session.term.shell = shell
	var copied string
	state.options.SetClipboardText = func(value string) error { copied = value; return nil }
	session.term.screen.Write([]byte("hello"))
	session.term.selection.selectRange(
		session.term.screen.PointAt(term.Position{Row: 0, Column: 0}),
		session.term.screen.PointAt(term.Position{Row: 0, Column: 4}),
	)
	x, y := cellPoint(geometry, 0, 0, state.cellWidth, state.cellHeight)
	state.terminalText(session.ID, app.TextEvent{Kind: app.TextSetMarked, Text: "ni", MarkedSelection: app.TextRange{Start: 2, End: 2}})
	rightClick(state, session, x, y)
	for _, event := range []app.TextEvent{
		{Kind: app.TextSetMarked, Text: "c"}, {Kind: app.TextInsert, Text: "c"},
		{Kind: app.TextSetMarked, Text: "e"}, {Kind: app.TextInsert, Text: "e"},
		{Kind: app.TextUnmark},
	} {
		result := state.terminalText(session.ID, event)
		if !result.Composing || result.Value != "ni" || !state.terminalMenu.open || shell.typed() != "" || copied != "" {
			t.Fatalf("IME notification %+v changed the menu or composition: ack=%+v open=%v shell=%q copied=%q", event, result, state.terminalMenu.open, shell.typed(), copied)
		}
	}
}

func TestTerminalSelectionMenuWhileTyping(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the Windows menu captures key presses before the input method")
	}
	for _, tc := range []struct {
		name, composition, letter, wantShell, wantCopy string
	}{
		{"send", "", "e", "hello", ""},
		{"copy", "", "c", "", "hello"},
		{"send-while-composing", "ni", "e", "hello", ""},
		{"copy-while-composing", "ni", "c", "", "hello"},
		{"escape-while-composing", "ni", "Escape", "", ""},
		{"click-away-while-composing", "ni", "click away", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, _, _, shell := liveSession(t)
			session := &state.sessions[0]
			var copied string
			state.options.SetClipboardText = func(value string) error { copied = value; return nil }
			size := gfx.Size{Width: 1400, Height: 900}
			state.Resize(size)
			driver := paneDriver(t, state, size)
			clickPaneGrid(t, state, driver, session)
			session.term.screen.Write([]byte("\x1b[2J\x1b[Hhello"))
			session.term.selection.selectRange(session.term.screen.PointAt(term.Position{}),
				session.term.screen.PointAt(term.Position{Column: 4}))
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			if tc.composition != "" {
				must(driver.Compose(tc.composition))
			}
			focus := terminalOf(state, session)
			hit := hitOf(t, driver, focus)
			must(driver.ClickButton(hit.Bounds.X+20, hit.Bounds.Y+40, gfx.PointerButtonSecondary))
			if !state.terminalMenu.open {
				t.Fatal("right click did not open the menu")
			}
			if session.term.preedit.Value != tc.composition {
				t.Fatalf("opening the menu changed composition from %q to %q", tc.composition, session.term.preedit.Value)
			}
			switch tc.letter {
			case "Escape":
				must(driver.Key(gfx.KeyEscape, 0))
			case "click away":
				must(driver.Click(hit.Bounds.X+hit.Bounds.Width-16, hit.Bounds.Y+40))
			default:
				// Other menu keys cannot become composition text, and holding
				// the mnemonic must not type a second letter after it closes.
				must(driver.KeyDown(gfx.KeyUnknown, "x"))
				must(driver.KeyUp(gfx.KeyUnknown, "x"))
				must(driver.KeyDown(gfx.KeyUnknown, tc.letter))
				must(driver.KeyDown(gfx.KeyUnknown, tc.letter))
				must(driver.KeyUp(gfx.KeyUnknown, tc.letter))
			}
			if state.terminalMenu.open || shell.typed() != tc.wantShell || copied != tc.wantCopy {
				t.Fatalf("after menu key: open=%v shell=%q clipboard=%q", state.terminalMenu.open, shell.typed(), copied)
			}
			after := driver.Snapshot()
			if after.Focused != focus {
				t.Fatalf("keyboard did not return to the terminal: focus=%v want=%v", after.Focused, focus)
			}
			if tc.composition != "" {
				editor := after.Editor
				if editor == nil || !editor.Composing || editor.Value != tc.composition {
					t.Fatalf("original composition after closing the menu: %+v", editor)
				}
				must(driver.Compose("nihao"))
				must(driver.Type("你好"))
				if shell.typed() != tc.wantShell+"你好" {
					t.Fatalf("resuming composition wrote %q", shell.typed())
				}
			}
		})
	}
}

func TestTerminalMenuSelectionKeysRequireAnUnmodifiedPress(t *testing.T) {
	state, session, geometry := selectionState()
	shell := &fakeShell{done: make(chan struct{})}
	session.term.shell = shell
	session.term.screen.Write([]byte("hello"))
	session.term.selection.selectRange(
		session.term.screen.PointAt(term.Position{Row: 0, Column: 0}),
		session.term.screen.PointAt(term.Position{Row: 0, Column: 4}),
	)
	state.options.SetClipboardText = func(string) error { t.Fatal("modified key copied selection"); return nil }
	x, y := cellPoint(geometry, 0, 0, state.cellWidth, state.cellHeight)
	rightClick(state, session, x, y)
	state.Shortcut(gfx.KeyEvent{Target: terminalTestHit(state, session), Text: "c", Modifiers: gfx.ModifierShift})
	if !state.terminalMenu.open {
		t.Fatal("Shift+C closed the menu")
	}
	if result := state.Shortcut(gfx.KeyEvent{Target: terminalTestHit(state, session), Text: "e", Repeat: true}); result.Handled {
		t.Fatalf("repeated E was consumed: %+v", result)
	}
	if !state.terminalMenu.open || len(shell.input) != 0 {
		t.Fatalf("repeated E changed menu/input: menu=%v input=%q", state.terminalMenu.open, shell.input)
	}
}

func TestTerminalMenuStaysInsideTheWindow(t *testing.T) {
	state, _, _, _ := liveSession(t)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	state.Paint(&gfx.Canvas{}, gfx.Size{Width: 1400, Height: 900})
	rightClick(state, session, 1395, 895)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	last, ok := menuHit(&canvas, state, hitTerminalMenuFiles)
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
	if _, ok := menuHit(&canvas, state, hitTerminalMenuClosePane); !ok {
		t.Fatal("a split tab's menu does not offer closing the pane")
	}
	chooseMenu(t, state, hitTerminalMenuClosePane)
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
	if _, ok := menuHit(&canvas, state, hitTerminalMenuClosePane); ok {
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
	if hit, _ := menuHit(&canvas, state, hitTerminalMenuCopy); !hit.Disabled {
		t.Fatal("Copy is enabled without a selection")
	}
	chooseMenu(t, state, hitTerminalMenuSelectAll)
	if !session.term.selection.selected || state.terminalMenu.open {
		t.Fatalf("Select All: selected=%v open=%v", session.term.selection.selected, state.terminalMenu.open)
	}

	open()
	chooseMenu(t, state, hitTerminalMenuCopy)
	if !strings.Contains(copied, "hello world") {
		t.Fatalf("copied %q", copied)
	}

	open()
	chooseMenu(t, state, hitTerminalMenuPaste)
	if got := shell.typed(); !strings.Contains(got, "from clipboard") {
		t.Fatalf("shell received %q", got)
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
		Target: terminalTestHit(state, session), X: 400, Y: 300, Viewport: state.viewport,
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
	chooseMenu(t, state, hitTerminalMenuClear)
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
	chooseMenu(t, state, hitTerminalMenuClearScreen)
	if session.term.screen.Scrollback() != 0 || !strings.Contains(session.term.screen.RowText(0), "prompt$") {
		t.Fatalf("scrollback=%d row0=%q", session.term.screen.Scrollback(), session.term.screen.RowText(0))
	}
}

func TestTheMenuClearsScreenSendsCtrlLToPowerShell(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	session.term.shell = kindShell{fakeShell: shell, kind: shellkind.PowerShell}
	shell.say("PS C:\\> ")
	state.Wake()
	state.terminalMenu = terminalMenu{open: true, session: session.ID}
	state.Paint(&gfx.Canvas{}, gfx.Size{Width: 1400, Height: 900})
	chooseMenu(t, state, hitTerminalMenuClearScreen)

	shell.mu.Lock()
	input := string(shell.input)
	shell.mu.Unlock()
	if !strings.Contains(input, "\x0c") {
		t.Fatalf("shell input = %q, want form feed (\\x0c) for PowerShell", input)
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
	row, ok := menuHit(&canvas, state, hitTerminalMenuClearScreen)
	if !ok || !row.Disabled {
		t.Fatalf("the clear screen row = %+v, %v; want it disabled over a program", row, ok)
	}
	chooseMenu(t, state, hitTerminalMenuClearScreen)
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
	stats, ok := menuHit(&canvas, state, hitTerminalMenuStats)
	if !ok {
		t.Fatal("the menu has no resource monitor row on a live shell")
	}
	if stats.Checked {
		t.Fatal("resource monitor starts checked")
	}
	files, ok := menuHit(&canvas, state, hitTerminalMenuFiles)
	if !ok {
		t.Fatal("the menu has no files row")
	}
	if files.Checked {
		t.Fatal("files starts checked")
	}
	if session.stats != nil {
		t.Fatal("the monitor is running before it was asked for")
	}
	chooseMenu(t, state, hitTerminalMenuStats)
	if session.stats == nil {
		t.Fatal("the menu row did not start the monitor")
	}
	if state.terminalMenu.open {
		t.Fatal("the menu stayed open after a choice")
	}

	rightClick(state, session, 400, 300)
	canvas.Reset()
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if stats, ok = menuHit(&canvas, state, hitTerminalMenuStats); !ok || !stats.Checked {
		t.Fatalf("running monitor checkbox = %+v, %v", stats, ok)
	}
	if files, ok = menuHit(&canvas, state, hitTerminalMenuFiles); !ok || files.Checked {
		t.Fatalf("closed files checkbox = %+v, %v", files, ok)
	}
	chooseMenu(t, state, hitTerminalMenuFiles)
	if !state.filesPanelShown() {
		t.Fatal("the files checkbox row did not open the panel")
	}
	rightClick(state, session, 400, 300)
	canvas.Reset()
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if files, ok = menuHit(&canvas, state, hitTerminalMenuFiles); !ok || !files.Checked {
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
	if hit, ok := menuHit(&canvas, state, hitTerminalMenuSearch); !ok || !hit.Disabled {
		t.Fatalf("Search is usable without a selection: found %v", ok)
	}
	chooseMenu(t, state, hitTerminalMenuSave)
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
	chooseMenu(t, state, hitTerminalMenuSearch)
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
	if _, ok := menuHit(&canvas, state, hitTerminalMenuAction[1]); !ok {
		t.Fatal("the browser row has no hit")
	}
	chooseMenu(t, state, hitTerminalMenuAction[1])
	if opened != "http://10.0.0.5:8080" {
		t.Fatalf("opened %q", opened)
	}

	rightClickOn(t, state, session, "10.0.0.5:8080")
	chooseMenu(t, state, hitTerminalMenuAction[2])
	if copied != "10.0.0.5:8080" {
		t.Fatalf("copied %q", copied)
	}

	// An archive's unpack line is typed at the prompt and not run.
	rightClickOn(t, state, session, "backup.tar.gz")
	chooseMenu(t, state, hitTerminalMenuAction[0])
	if got := shell.typed(); got != "mkdir -- 'backup' && tar -xf 'backup.tar.gz' -C 'backup'" {
		t.Fatalf("typed %q", got)
	}

	// Plain words get the ordinary menu and nothing more.
	rightClickOn(t, state, session, "listening")
	if state.terminalMenu.count != 0 {
		t.Fatalf("a plain word offered %d actions", state.terminalMenu.count)

		before := len(world.configs)
		rightClickOn(t, state, session, "10.0.0.5:8080")
		chooseMenu(t, state, hitTerminalMenuAction[0])
		if len(world.configs) != before+1 || world.configs[before].Host != "10.0.0.5" || world.configs[before].Port != 8080 {
			t.Fatalf("connection configs after the click: %+v", world.configs[before:])
		}
	}
}

// A path a local terminal prints is an entry of this computer, so its menu row
// shows it in the system's file manager. The file panel remains the transfer
// surface, one menu row away, and a WSL distribution's paths stay in it because
// this computer's file manager cannot show them.
func TestALocalTerminalPathOpensInTheSystemFileManager(t *testing.T) {
	state := newTestState(t, testOptions(t))
	folder := t.TempDir()
	file := filepath.Join(folder, "report.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	screen := term.New(80, 24, 100)
	screen.Write([]byte("report.txt\r\n"))
	local := &Session{ID: 1, HostID: localHostID, conn: newLocalConn("", networkProxyDirect), term: &terminal{screen: screen}}
	local.term.reportedDirectory = term.Cwd{Path: folder}
	state.terminalMenu = terminalMenu{open: true, session: local.ID}
	state.offerActionsAt(local, term.Position{Row: 0, Column: 2})
	if state.terminalMenu.count != 1 {
		t.Fatalf("menu offers %d actions: %+v", state.terminalMenu.count, state.terminalMenu.found[:state.terminalMenu.count])
	}
	first := state.terminalMenu.found[0]
	if first.kind != actionReveal || first.target != file || first.label != "report.txt" {
		t.Fatalf("first action = %+v, want showing %q", first, file)
	}
	revealed := ""
	state.options.RevealPath = func(path string) error { revealed = path; return nil }
	state.runAction(local, first)
	waitFor(t, state, func() bool { return !state.revealBusy })
	if revealed != file {
		t.Fatalf("revealed %q, want %q", revealed, file)
	}

	// A server's paths are not this computer's: an SSH terminal keeps the file
	// panel's row.
	// A server prints POSIX paths: the local file above is a Windows path
	// there, which is not a remote path at all.
	remoteScreen := term.New(80, 24, 100)
	remoteScreen.Write([]byte("/srv/report.txt\r\n"))
	remote := &Session{ID: 2, host: Host{Protocol: protocolSSH, Address: "box"}, conn: newLocalConn("", networkProxyDirect), term: &terminal{screen: remoteScreen}}
	state.terminalMenu = terminalMenu{open: true, session: remote.ID}
	state.offerActionsAt(remote, term.Position{Row: 0, Column: 2})
	if got := state.terminalMenu.found[0].kind; got != actionFiles {
		t.Fatalf("an SSH terminal offered %v, want the file panel", got)
	}

	// A WSL distribution's files are reached through the panel that maps its
	// paths; the host's file manager cannot show them.
	wslScreen := term.New(80, 24, 100)
	wslScreen.Write([]byte("/home/report.txt\r\n"))
	wsl := &Session{ID: 3, HostID: localHostID, conn: newLocalConn(localshell.WSLShell("Ubuntu"), networkProxyDirect), term: &terminal{screen: wslScreen}}
	state.terminalMenu = terminalMenu{open: true, session: wsl.ID}
	state.offerActionsAt(wsl, term.Position{Row: 0, Column: 2})
	if got := state.terminalMenu.found[0].kind; got != actionFiles {
		t.Fatalf("a WSL terminal offered %v, want the file panel", got)
	}

	// Before shell integration reports a folder, a local terminal still resolves
	// relative names against its remembered localDirectory.
	remembered := &Session{ID: 4, HostID: localHostID, localDirectory: folder, conn: newLocalConn("", networkProxyDirect), term: &terminal{screen: screen}}
	state.terminalMenu = terminalMenu{open: true, session: remembered.ID}
	state.offerActionsAt(remembered, term.Position{Row: 0, Column: 2})
	if state.terminalMenu.count != 1 || state.terminalMenu.found[0].kind != actionReveal || state.terminalMenu.found[0].target != file {
		t.Fatalf("remembered localDirectory actions = %+v, want reveal %q", state.terminalMenu.found[:state.terminalMenu.count], file)
	}
}
