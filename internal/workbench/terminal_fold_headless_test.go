//go:build darwin || windows || linux

package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
	"motty/internal/term"
)

// foldWindow is a connected terminal with the gutter on, in a headless window
// that has been shown a command and what it printed, the way the shell
// integration marks them.
func foldWindow(t *testing.T) (*State, *Session, *headless.Session, *fakeShell, func()) {
	t.Helper()
	state, _, _, shell := liveSession(t)
	state.setTerminalGutter(gutterNumbers)
	var input app.TextInput
	resolveInput := func(context app.TextInputContext) *app.TextInput {
		if context.Terminal {
			return state.TerminalInput(context)
		}
		input.Editor = state.EditorFor(context.ID)
		if input.Editor == nil {
			return nil
		}
		return &input
	}
	var invalidator app.Invalidator
	window, err := headless.Open(app.Config{
		Title: "fold", Width: 1400, Height: 900,
		Paint: state.Paint, TextInput: resolveInput,
		OnPointer: state.Pointer, OnKey: state.Key,
		OnShortcut: state.Shortcut, OnResize: state.Resize,
		Invalidator: &invalidator, OnWake: state.Wake,
	}, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(window.Close)
	pump := func() {
		t.Helper()
		invalidator.Invalidate()
		if err := window.Driver().Pump(); err != nil {
			t.Fatal(err)
		}
	}
	session := &state.sessions[0]
	if session.term.folderToken == "" {
		t.Fatal("the session has no integration token to mark commands with")
	}
	suffix := ";motty=" + session.term.folderToken
	shell.say("\x1b]133;A" + suffix + "\x07$ \x1b]133;B" + suffix + "\x07ll\r\n\x1b]133;C" + suffix + "\x07alpha\r\nbeta\r\ngamma\r\n\x1b]133;A" + suffix + "\x07$ ")
	pump()
	return state, session, window, shell, pump
}

func foldHasText(window *headless.Session, want string) bool {
	return containsExact(window.Texts(), want)
}

func TestHeadlessFoldBoxFoldsAndOpensTheOutputWithAClick(t *testing.T) {
	state, _, window, _, pump := foldWindow(t)
	if !foldHasText(window, "alpha") {
		t.Fatalf("the command's output is not on screen: %q", window.Texts())
	}
	if err := window.ClickLabel(state.messages.Tools.FoldOutput); err != nil {
		t.Fatal(err)
	}
	pump()
	if foldHasText(window, "alpha") || foldHasText(window, "gamma") {
		t.Fatalf("the output stayed after folding: %q", window.Texts())
	}
	if !foldHasText(window, "$ ll") {
		t.Fatalf("folding hid the command: %q", window.Texts())
	}
	if err := window.ClickLabel(state.messages.Tools.UnfoldOutput); err != nil {
		t.Fatal(err)
	}
	pump()
	if !foldHasText(window, "alpha") || !foldHasText(window, "gamma") {
		t.Fatalf("the output did not come back: %q", window.Texts())
	}
}

func TestHeadlessADoubleClickOnTheFoldBoxTogglesOnce(t *testing.T) {
	state, _, window, _, pump := foldWindow(t)
	hit, ok := window.Hit(state.messages.Tools.FoldOutput)
	if !ok {
		t.Fatal("no fold button")
	}
	if err := window.DoubleClick(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	pump()
	if foldHasText(window, "alpha") {
		t.Fatalf("a double click left the output open (it folded and unfolded): %q", window.Texts())
	}
}

func TestHeadlessFoldBoxPressIsNeitherASelectionNorTheProgramsMouse(t *testing.T) {
	state, session, window, shell, pump := foldWindow(t)
	shell.say("\x1b[?1000h\x1b[?1006h")
	pump()
	shell.mu.Lock()
	before := len(shell.input)
	shell.mu.Unlock()
	if err := window.ClickLabel(state.messages.Tools.FoldOutput); err != nil {
		t.Fatal(err)
	}
	pump()
	shell.mu.Lock()
	sent := append([]byte(nil), shell.input[before:]...)
	shell.mu.Unlock()
	if len(sent) != 0 {
		t.Fatalf("a click on the fold box reached the program as %q", sent)
	}
	if foldHasText(window, "alpha") {
		t.Fatal("the click did not fold")
	}
	if session.term.selection.selected {
		t.Fatal("the click began a selection")
	}
}

func TestHeadlessFoldBoxIsAButtonThatSaysWhetherItIsFolded(t *testing.T) {
	state, _, window, _, pump := foldWindow(t)
	hit, ok := window.Hit(state.messages.Tools.FoldOutput)
	if !ok {
		t.Fatal("no fold button")
	}
	if hit.Role != ui.RoleButton || !hit.Expandable || !hit.Expanded {
		t.Fatalf("open box = role %v expandable %v expanded %v", hit.Role, hit.Expandable, hit.Expanded)
	}
	if hit.Cursor != ui.PointerCursorHand {
		t.Fatalf("pointer over the box is %v, want the hand", hit.Cursor)
	}
	if hit.Focusable {
		t.Fatal("the box takes the keyboard from the terminal")
	}
	if err := window.ClickLabel(state.messages.Tools.FoldOutput); err != nil {
		t.Fatal(err)
	}
	pump()
	hit, ok = window.Hit(state.messages.Tools.UnfoldOutput)
	if !ok || hit.Expanded {
		t.Fatalf("folded box = %+v (found %v), want a collapsed button", hit, ok)
	}
}

func TestHeadlessPaletteFoldsTheLastCommandForTheKeyboard(t *testing.T) {
	state, _, window, _, pump := foldWindow(t)
	state.openCommandPalette()
	pump()
	if err := window.Type("last command"); err != nil {
		t.Fatal(err)
	}
	pump()
	if err := window.ClickLabel(state.messages.Tools.FoldLastOutput); err != nil {
		t.Fatalf("the palette offers no fold command: %v", err)
	}
	pump()
	if foldHasText(window, "alpha") {
		t.Fatalf("the palette command did not fold: %q", window.Texts())
	}
	state.openCommandPalette()
	pump()
	if err := window.Type("unfold all"); err != nil {
		t.Fatal(err)
	}
	pump()
	if err := window.ClickLabel(state.messages.Tools.UnfoldAllOutput); err != nil {
		t.Fatal(err)
	}
	pump()
	if !foldHasText(window, "alpha") {
		t.Fatalf("Unfold all did not open the block: %q", window.Texts())
	}
}

func TestAWheelOverAProgramUsingTheMouseStillReachesItWhileScrolledBack(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	state.setTerminalGutter(gutterNumbers)
	shell.say("\x1b[?1000h\x1b[?1006h")
	shell.say(strings.Repeat("line\r\n", 200))
	state.Wake()
	session.term.screen.Scroll(10)
	if session.term.screen.ViewOffset() == 0 {
		t.Fatal("the view did not scroll back")
	}
	shell.mu.Lock()
	before := len(shell.input)
	shell.mu.Unlock()
	session.term.sendMouse(termMouse(2, 3))
	shell.mu.Lock()
	sent := len(shell.input) - before
	shell.mu.Unlock()
	if sent == 0 {
		t.Fatal("a mouse event over a scrolled-back view was dropped, not reported")
	}
}

func termMouse(column, row int) term.MouseEvent {
	return term.MouseEvent{Kind: term.MousePress, Button: term.MouseButtonLeft, Column: column, Row: row}
}
