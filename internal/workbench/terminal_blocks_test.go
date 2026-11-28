package workbench

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"motty/internal/term"
)

func blockMarker(session *Session, value string) string {
	return "\x1b]133;" + value + ";motty=" + session.term.folderToken + "\x07"
}

func sayMarkers(state *State, shell *fakeShell, session *Session, markers ...string) {
	shell.say(strings.Join(markers, ""))
	state.Wake()
}

func startBlockPrompt(state *State, shell *fakeShell, session *Session) {
	sayMarkers(state, shell, session, blockMarker(session, "A"), "$ ", blockMarker(session, "B"))
}

func TestACommandBecomesAFinishedBlock(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	now := time.Second
	state.options.Now = func() time.Duration { return now }

	startBlockPrompt(state, shell, session)
	shell.say("echo hi\r\n")
	state.Wake()
	now = 2 * time.Second
	sayMarkers(state, shell, session, blockMarker(session, "C"))

	if len(session.term.blocks) != 1 {
		t.Fatalf("blocks = %d, want 1", len(session.term.blocks))
	}
	block := &session.term.blocks[0]
	if block.status != blockRunning {
		t.Fatalf("status = %d, want running", block.status)
	}
	if block.text != "echo hi" {
		t.Fatalf("command text = %q, want %q", block.text, "echo hi")
	}
	if output, ok := session.term.blockText(block, blockCopyOutput); !ok || output != "" {
		t.Fatalf("running output = %q, %v; want empty, true", output, ok)
	}

	shell.say("hi\r\n")
	state.Wake()
	now = 3 * time.Second
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))

	block = &session.term.blocks[0]
	if block.status != blockSucceeded || block.exitCode != 0 {
		t.Fatalf("finished block = status %d exit %d, want success 0", block.status, block.exitCode)
	}
	if got := block.duration(); got != time.Second {
		t.Fatalf("duration = %v, want 1s", got)
	}
	if output, ok := session.term.blockText(block, blockCopyOutput); !ok || output != "hi" {
		t.Fatalf("output = %q, %v; want %q", output, ok, "hi")
	}
	if all, ok := session.term.blockText(block, blockCopyAll); !ok || all != "echo hi\nhi" {
		t.Fatalf("block text = %q, %v; want %q", all, ok, "echo hi\nhi")
	}
	if command, ok := session.term.blockText(block, blockCopyCommand); !ok || command != "echo hi" {
		t.Fatalf("command = %q, %v; want %q", command, ok, "echo hi")
	}
}

func TestBlockStatusFollowsTheExitCode(t *testing.T) {
	cases := []struct {
		code   string
		status blockStatus
	}{
		{"D;1", blockFailed},
		{"D;2", blockFailed},
		{"D;130", blockInterrupted},
		{"D;0", blockSucceeded},
	}
	for _, test := range cases {
		t.Run(test.code, func(t *testing.T) {
			state, _, _, shell := liveSession(t)
			session := &state.sessions[0]
			startBlockPrompt(state, shell, session)
			shell.say("false\r\n")
			state.Wake()
			sayMarkers(state, shell, session, blockMarker(session, "C"))
			sayMarkers(state, shell, session, blockMarker(session, test.code))
			if len(session.term.blocks) != 1 {
				t.Fatalf("blocks = %d, want 1", len(session.term.blocks))
			}
			if got := session.term.blocks[0].status; got != test.status {
				t.Fatalf("status = %d, want %d", got, test.status)
			}
		})
	}
}

func TestAnEmptyPromptLineIsNotABlock(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	startBlockPrompt(state, shell, session)
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	if len(session.term.blocks) != 0 {
		t.Fatalf("an empty Enter made %d blocks", len(session.term.blocks))
	}
}

func TestACommandStartWithoutAPromptOpensNoBlock(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	// The integration script prints C once while installing, before the first
	// prompt; it must not become a block.
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	if len(session.term.blocks) != 0 {
		t.Fatalf("an install-time C made %d blocks", len(session.term.blocks))
	}
}

func TestACommandThatEntersTheAlternateScreenKeepsItsMainScreenAnchor(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	startBlockPrompt(state, shell, session)
	shell.say("vim\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say("\x1b[?1049h")
	state.Wake()
	if len(session.term.blocks) != 1 {
		t.Fatalf("blocks = %d, want 1", len(session.term.blocks))
	}
	if session.term.blocks[0].prompt.line != 1 {
		t.Fatalf("prompt line = %d, want 1", session.term.blocks[0].prompt.line)
	}
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	if block := session.term.blocks[0]; block.status != blockSucceeded {
		t.Fatalf("status = %d, want success", block.status)
	}
}

func TestBlocksArePrunedWithTheScrollback(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	session.term.screen.SetScrollbackLimit(2)
	startBlockPrompt(state, shell, session)
	shell.say("echo old\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say("old\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	if len(session.term.blocks) != 1 {
		t.Fatalf("blocks = %d, want 1", len(session.term.blocks))
	}

	_, rows := session.term.screen.Size()
	shell.say(strings.Repeat("later\r\n", rows+20))
	state.Wake()
	session.term.pruneBlocks()
	if len(session.term.blocks) != 0 {
		t.Fatalf("a block whose lines left the buffer survived: %d", len(session.term.blocks))
	}
}

func TestBlocksFollowTheirLinesAcrossAReflow(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	startBlockPrompt(state, shell, session)
	shell.say("echo a-command-that-wraps-when-the-window-narrows\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say("output\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	if len(session.term.blocks) != 1 {
		t.Fatalf("blocks = %d, want 1", len(session.term.blocks))
	}
	before := session.term.blocks[0]
	command, ok := session.term.blockText(&before, blockCopyCommand)
	if !ok {
		t.Fatal("the command is not copyable before the resize")
	}

	cols, rows := session.term.screen.Size()
	session.term.screen.Resize(max(cols/2, minTerminalCols), rows)
	state.remapBlocks(session)

	after := session.term.blocks[0]
	if got, ok := session.term.blockText(&after, blockCopyCommand); !ok || got != command {
		t.Fatalf("command after reflow = %q, %v; want %q", got, ok, command)
	}
	if line, ok := session.term.screen.LineOfID(after.prompt.id); !ok || line != after.prompt.line {
		t.Fatalf("prompt identity resolves to %d, %v; want %d, true", line, ok, after.prompt.line)
	}
}

func TestBlockOutputCopyRefusesTrimmedOutput(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	session.term.screen.SetScrollbackLimit(2)
	startBlockPrompt(state, shell, session)
	shell.say("echo old\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say("old\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))

	_, rows := session.term.screen.Size()
	shell.say(strings.Repeat("later\r\n", rows+20))
	state.Wake()
	block := &session.term.blocks[0]
	if _, ok := session.term.blockText(block, blockCopyOutput); ok {
		t.Fatal("output was copied from a block whose lines left the buffer")
	}
	if command, ok := session.term.blockText(block, blockCopyCommand); !ok || command != "echo old" {
		t.Fatalf("command = %q, %v; want the captured text", command, ok)
	}
}

func TestLineNumbersAndBlockLookupAgree(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	startBlockPrompt(state, shell, session)
	shell.say("one\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say("two\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))

	block := &session.term.blocks[0]
	line := session.term.screen.LineNumber(0)
	if line != block.startLine() {
		t.Fatalf("row 0 line = %d, block start = %d", line, block.startLine())
	}
	if index := session.term.blockAtLine(block.startLine()); index != 0 {
		t.Fatalf("blockAtLine(start) = %d, want 0", index)
	}
	if index := session.term.blockAtLine(block.endLine() + 1); index != -1 {
		t.Fatalf("blockAtLine(after end) = %d, want -1", index)
	}
	if points, ok := session.term.screen.CursorPoint(); !ok || points.Line == 0 {
		t.Fatalf("CursorPoint = %+v, %v", points, ok)
	}
	if _, ok := session.term.screen.LineIDAt(0); ok {
		t.Fatal("LineIDAt(0) is ok")
	}
}

func TestTheTerminalMenuOffersBlockCopyActions(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	startBlockPrompt(state, shell, session)
	shell.say("echo hi\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say("hi\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))

	copied := ""
	state.options.SetClipboardText = func(text string) error {
		copied = text
		return nil
	}
	state.terminalMenu.pointer = gfx.Point{X: 200, Y: 200}
	state.openTerminalMenuAt(session, term.Position{Row: 0, Column: 0})
	if state.terminalMenu.blockID == 0 {
		t.Fatal("the menu did not find the block under the press")
	}
	canvas, texts := paintedTexts(state)
	for _, want := range []string{"Succeeded", "Copy command", "Copy output", "Copy block"} {
		if !contains(texts, want) {
			t.Fatalf("the block menu does not show %q:\n%v", want, texts)
		}
	}
	if !contains(texts, "hi") {
		t.Fatalf("the terminal output disappeared while block chrome was painted:\n%v", texts)
	}
	if _, ok := hitByID(canvas, hitTerminalMenuBlockOutput); !ok {
		t.Fatal("the copy-output row has no hit region")
	}
	chooseMenu(t, state, hitTerminalMenuBlockOutput)
	if copied != "hi" {
		t.Fatalf("clipboard = %q, want %q", copied, "hi")
	}
	if state.status != state.messages.Blocks.Copied {
		t.Fatalf("status = %q, want %q", state.status, state.messages.Blocks.Copied)
	}
}

func TestAnEmptyCommandRowStillGetsBlockChrome(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	startBlockPrompt(state, shell, session)
	shell.say("true\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say("\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))

	canvas, texts := paintedTexts(state)
	if !contains(texts, "true") {
		t.Fatalf("the command text disappeared:\n%v", texts)
	}
	var strips int
	for _, rectangle := range canvas.Rectangles() {
		if rectangle.Bounds.Width == blockStatusWidth {
			strips++
		}
	}
	if strips == 0 {
		t.Fatal("no block status strip was painted")
	}
}

func selectBlockRow(state *State, session *Session, row int) {
	state.selectBlockAt(session, term.Position{Row: row, Column: 0})
}

func TestClickingABlockShowsACopyToolbar(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	startBlockPrompt(state, shell, session)
	shell.say("echo hi\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say("hi\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))

	copied := ""
	state.options.SetClipboardText = func(text string) error {
		copied = text
		return nil
	}
	geometry := state.terminalWindowLayout(session.ID)
	at := gfx.Point{X: geometry.left + 8, Y: geometry.top + 4}
	selectBlockRow(state, session, 0)
	canvas, _ := paintedTexts(state)
	part := terminalControlParts(session.ID).At(terminalPartBlockCopyOutput)
	hit, ok := hitByID(canvas, part)
	if !ok {
		t.Fatal("selecting a block painted no copy-output button")
	}
	if hit.Hint != state.messages.Blocks.CopyOutput {
		t.Fatalf("copy-output hint = %q, want %q", hit.Hint, state.messages.Blocks.CopyOutput)
	}
	if hit.Role != gfx.RoleButton || !hit.Focusable || hit.SkipTab {
		t.Fatalf("copy-output is not a keyboard-reachable button: role=%v focusable=%v skipTab=%v", hit.Role, hit.Focusable, hit.SkipTab)
	}

	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: part, X: at.X, Y: at.Y})
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, Target: part, X: at.X, Y: at.Y})
	if copied != "hi" {
		t.Fatalf("clipboard = %q, want %q", copied, "hi")
	}
	if state.status != state.messages.Blocks.Copied {
		t.Fatalf("status = %q, want %q", state.status, state.messages.Blocks.Copied)
	}

	_, rows := session.term.screen.Size()
	selectBlockRow(state, session, rows-1)
	canvas, _ = paintedTexts(state)
	if _, ok := hitByID(canvas, part); ok {
		t.Fatal("the copy toolbar stayed painted after the block was deselected")
	}
}

func TestABlockSurvivesAResizeInsideTheAlternateScreen(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	startBlockPrompt(state, shell, session)
	shell.say("echo hi\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say("hi\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	command, ok := session.term.blockText(&session.term.blocks[0], blockCopyCommand)
	if !ok {
		t.Fatal("the command is not copyable before the alternate screen")
	}

	shell.say("\x1b[?1049h")
	state.Wake()
	paintedTexts(state)
	if !session.term.blockAlt {
		t.Fatal("the paint did not notice the alternate screen")
	}
	cols, rows := session.term.screen.Size()
	session.term.screen.Resize(max(cols/2, minTerminalCols), rows)
	shell.say("\x1b[?1049l")
	state.Wake()
	paintedTexts(state)

	block := &session.term.blocks[0]
	if got, ok := session.term.blockText(block, blockCopyCommand); !ok || got != command {
		t.Fatalf("command after the alternate screen = %q, %v; want %q", got, ok, command)
	}
	if line, ok := session.term.screen.LineOfID(block.prompt.id); !ok || line != block.prompt.line {
		t.Fatalf("prompt identity resolves to %d, %v; want %d, true", line, ok, block.prompt.line)
	}
}

func TestRunningBlockCopyAndToolbar(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	startBlockPrompt(state, shell, session)
	shell.say("sleep 1\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say("partial output\r\n")
	state.Wake()

	copied := ""
	state.options.SetClipboardText = func(text string) error {
		copied = text
		return nil
	}
	geometry := state.terminalWindowLayout(session.ID)
	at := gfx.Point{X: geometry.left + 8, Y: geometry.top + 4}
	selectBlockRow(state, session, 0)
	canvas, _ := paintedTexts(state)
	part := terminalControlParts(session.ID).At(terminalPartBlockCopyOutput)
	if _, ok := hitByID(canvas, part); !ok {
		t.Fatal("a running block has no toolbar")
	}
	if !contains(textsOf(canvas), state.messages.Blocks.Running) {
		t.Fatalf("the toolbar does not say the command is running: %v", textsOf(canvas))
	}
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: part, X: at.X, Y: at.Y})
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, Target: part, X: at.X, Y: at.Y})
	if copied != "partial output" {
		t.Fatalf("running output = %q, want %q", copied, "partial output")
	}
}

func TestBlockToolbarYieldsToProgramsUsingTheMouse(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	startBlockPrompt(state, shell, session)
	shell.say("echo hi\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say("hi\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	shell.say("\x1b[?1000h")
	state.Wake()

	selectBlockRow(state, session, 0)
	canvas, _ := paintedTexts(state)
	part := terminalControlParts(session.ID).At(terminalPartBlockCopyOutput)
	if _, ok := hitByID(canvas, part); ok {
		t.Fatal("the copy toolbar appeared while the program owns the mouse")
	}
}

func TestBlockFinishedWhileScrolledStillAnchorsTheLiveCursor(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	state.options.Now = func() time.Duration { return 5 * time.Second }
	startBlockPrompt(state, shell, session)
	shell.say("echo many\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	_, rows := session.term.screen.Size()
	shell.say(strings.Repeat("line\r\n", rows+10))
	state.Wake()
	if !session.term.screen.Scroll(4) {
		t.Fatal("the test terminal has no scrollback to scroll into")
	}
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))

	block := &session.term.blocks[0]
	cursor, ok := session.term.screen.CursorPoint()
	if !ok {
		t.Fatal("CursorPoint is not available")
	}
	if block.end.line != cursor.Line {
		t.Fatalf("block end line = %d, live cursor line = %d; the view offset leaked into the anchor", block.end.line, cursor.Line)
	}
	output, ok := session.term.blockText(block, blockCopyOutput)
	if !ok {
		t.Fatal("the output is not copyable")
	}
	if strings.Count(output, "line") != rows+10 {
		t.Fatalf("output has %d lines, want %d", strings.Count(output, "line"), rows+10)
	}
}

func textsOf(canvas *gfx.Canvas) []string {
	var texts []string
	for _, text := range canvas.Texts() {
		texts = append(texts, text.Value)
	}
	return texts
}

func setBlockCollapsedForTest(block *commandBlock, terminal *terminal, collapsed bool) {
	block.collapsed = collapsed
	terminal.markFoldsDirty()
}

func TestCollapsingABlockHidesItsOutputAndShowsASummary(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	startBlockPrompt(state, shell, session)
	shell.say("echo many\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say("one\r\ntwo\r\nthree\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))

	block := &session.term.blocks[0]
	setBlockCollapsedForTest(block, session.term, true)
	_, texts := paintedTexts(state)
	if contains(texts, "two") || contains(texts, "three") {
		t.Fatalf("collapsed output is still painted:\n%v", texts)
	}
	summary := fmt.Sprintf(state.messages.Blocks.HiddenLinesFmt, blockFoldLines(block))
	if !contains(texts, summary) {
		t.Fatalf("the fold summary %q is not painted:\n%v", summary, texts)
	}
	if !contains(texts, "echo many") {
		t.Fatalf("the command row disappeared:\n%v", texts)
	}

	setBlockCollapsedForTest(block, session.term, false)
	_, texts = paintedTexts(state)
	if !contains(texts, "two") || !contains(texts, "three") {
		t.Fatalf("expanded output is missing:\n%v", texts)
	}
}

func TestARedrawnCommandRowStaysInTheBlock(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	startBlockPrompt(state, shell, session)
	shell.say("echo many\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say("one\r\ntwo\r\nthree\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))

	block := &session.term.blocks[0]
	commandLine := block.startLine()
	if got := session.term.blockAtLine(commandLine); got != 0 {
		t.Fatalf("command line %d belongs to block %d, want 0", commandLine, got)
	}
	if got := session.term.blockDisplayStartAt(0); got != commandLine {
		t.Fatalf("display start = %d, want command line %d", got, commandLine)
	}

	if output, ok := session.term.blockText(block, blockCopyOutput); !ok || output != "one\ntwo\nthree" {
		t.Fatalf("output copy = %q, %v; want all output", output, ok)
	}
	if all, ok := session.term.blockText(block, blockCopyAll); !ok || all != "echo many\none\ntwo\nthree" {
		t.Fatalf("block copy = %q, %v; want command and output", all, ok)
	}

	setBlockCollapsedForTest(block, session.term, true)
	_, texts := paintedTexts(state)
	if contains(texts, "one") || contains(texts, "two") || contains(texts, "three") {
		t.Fatalf("collapsed output is still painted:\n%v", texts)
	}
	if !contains(texts, "echo many") {
		t.Fatalf("the command row disappeared:\n%v", texts)
	}
	if !contains(texts, fmt.Sprintf(state.messages.Blocks.HiddenLinesFmt, blockFoldLines(block))) {
		t.Fatalf("the fold summary is missing:\n%v", texts)
	}
}

func TestTheToolbarTogglesABlockFold(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	startBlockPrompt(state, shell, session)
	shell.say("echo many\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say("one\r\ntwo\r\nthree\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))

	geometry := state.terminalWindowLayout(session.ID)
	at := gfx.Point{X: geometry.left + 8, Y: geometry.top + 4}
	selectBlockRow(state, session, 0)
	canvas, _ := paintedTexts(state)
	part := terminalControlParts(session.ID).At(terminalPartBlockToggleFold)
	if _, ok := hitByID(canvas, part); !ok {
		t.Fatal("the block toolbar has no fold button")
	}
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: part, X: at.X, Y: at.Y})
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, Target: part, X: at.X, Y: at.Y})
	if !session.term.blocks[0].collapsed {
		t.Fatal("the fold button did not collapse the block")
	}
	_, texts := paintedTexts(state)
	if contains(texts, "two") || contains(texts, "three") {
		t.Fatalf("the folded output is still painted:\n%v", texts)
	}

	// The pointer is still on the same row, now the summary; the toolbar stays
	// reachable and expands the block again.
	canvas, _ = paintedTexts(state)
	if _, ok := hitByID(canvas, part); !ok {
		t.Fatal("the fold toolbar disappeared after collapsing")
	}
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: part, X: at.X, Y: at.Y})
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, Target: part, X: at.X, Y: at.Y})
	if session.term.blocks[0].collapsed {
		t.Fatal("the fold button did not expand the block")
	}
}

func TestTheFoldedDisplayMapSkipsHiddenLinesAndKeepsTheCursor(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	startBlockPrompt(state, shell, session)
	shell.say("echo many\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	_, rows := session.term.screen.Size()
	shell.say(strings.Repeat("line\r\n", rows/2+2))
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))

	terminal := session.term
	block := &terminal.blocks[0]
	setBlockCollapsedForTest(block, terminal, true)
	terminal.buildDisplayMap(rows)
	if len(terminal.display) != rows {
		t.Fatalf("display rows = %d, want %d", len(terminal.display), rows)
	}
	previous := uint64(0)
	summaryRow := -1
	for row, entry := range terminal.display {
		if entry.index < 0 {
			continue
		}
		if entry.line <= previous {
			t.Fatalf("row %d line %d is not after %d", row, entry.line, previous)
		}
		previous = entry.line
		if entry.line > block.exec.line && entry.line < block.endLine() && entry.fold < 0 {
			t.Fatalf("hidden line %d is displayed at row %d", entry.line, row)
		}
		if entry.fold >= 0 {
			summaryRow = row
		}
	}
	if summaryRow < 0 {
		t.Fatal("the folded block has no summary row")
	}
	cursorRow := terminal.displayCursorRow(terminal.presentedCursor())
	cursor, ok := terminal.screen.CursorPoint()
	if !ok {
		t.Fatal("CursorPoint is not available")
	}
	if got := terminal.displayLine(cursorRow); got != cursor.Line {
		t.Fatalf("cursor display row %d shows line %d, want %d", cursorRow, got, cursor.Line)
	}
	total, _ := terminal.displayScroll(rows)
	if want := terminal.screen.RetainedLines() - int(blockFoldRows(block)); total != want {
		t.Fatalf("display scroll total = %d, want %d", total, want)
	}
}

func TestFindTemporarilyExpandsFoldedBlocks(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	startBlockPrompt(state, shell, session)
	shell.say("echo many\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say("one\r\ntwo\r\nthree\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	setBlockCollapsedForTest(&session.term.blocks[0], session.term, true)

	session.term.find.open = true
	_, texts := paintedTexts(state)
	if !contains(texts, "two") || !contains(texts, "three") {
		t.Fatalf("find did not expand the folded block:\n%v", texts)
	}
	session.term.find.open = false
	_, texts = paintedTexts(state)
	if contains(texts, "two") || contains(texts, "three") {
		t.Fatalf("the block did not fold again after find closed:\n%v", texts)
	}
}

func TestABlockSurvivesResizeClearAnEmptyEnterAndTheNextCommand(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	startBlockPrompt(state, shell, session)
	shell.say("echo first\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say(strings.Repeat(strings.Repeat("abcdefghij", 12)+"\r\n", 20))
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))

	state.Resize(gfx.Size{Width: 700, Height: 600})
	state.Wake()
	state.Resize(gfx.Size{Width: 1000, Height: 600})
	state.Wake()

	// Run the shell's clear command, then an empty Enter, then a real command.
	startBlockPrompt(state, shell, session)
	shell.say("clear\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say("\x1b[H\x1b[2J\x1b[3J")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	startBlockPrompt(state, shell, session)
	shell.say("\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	startBlockPrompt(state, shell, session)
	shell.say("echo last\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say("last\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))

	if len(session.term.blocks) != 1 {
		t.Fatalf("blocks = %d, want the post-clear command block only", len(session.term.blocks))
	}
	block := &session.term.blocks[0]
	if got := session.term.blockAtLine(block.exec.line); got != 0 {
		t.Fatalf("first output line %d belongs to block %d, want 0", block.exec.line, got)
	}
	if got := session.term.blockAtLine(block.visualEndLine()); got != 0 {
		t.Fatalf("last output line %d belongs to block %d, want 0", block.visualEndLine(), got)
	}
	if got := session.term.blockAtLine(block.endLine()); got != -1 {
		t.Fatalf("D prompt line %d belongs to block %d, want -1", block.endLine(), got)
	}
	if output, ok := session.term.blockText(block, blockCopyOutput); !ok || output != "last" {
		t.Fatalf("output copy = %q, %v; want the next command's output", output, ok)
	}
	if blockFoldLines(block) == 0 {
		t.Fatal("the post-clear output cannot fold")
	}
}

func TestClearingTheScreenDropsCommandBlocks(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	startBlockPrompt(state, shell, session)
	shell.say("echo hi\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say("hi\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	if len(session.term.blocks) != 1 {
		t.Fatalf("blocks = %d, want 1", len(session.term.blocks))
	}

	session.term.clearScreen(state.messages)
	if len(session.term.blocks) != 0 {
		t.Fatalf("clear screen left %d blocks", len(session.term.blocks))
	}
	_, texts := paintedTexts(state)
	if contains(texts, "Succeeded") {
		t.Fatalf("a cleared block is still painted:\n%v", texts)
	}
}

func TestTheShellClearSequenceDropsCommandBlocks(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	startBlockPrompt(state, shell, session)
	shell.say("echo hi\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say("hi\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))

	shell.say("\x1b[H\x1b[2J")
	state.Wake()
	if len(session.term.blocks) != 0 {
		t.Fatalf("CSI 2J left %d blocks", len(session.term.blocks))
	}
	session.term.buildDisplayMap(10)
	if len(session.term.display) != 0 {
		t.Fatalf("a folded display map survived the clear: %d entries", len(session.term.display))
	}
}

func TestClearingScrollbackKeepsAVisibleBlock(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	startBlockPrompt(state, shell, session)
	shell.say("echo hi\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say("hi\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))

	session.term.screen.ClearScrollback()
	if len(session.term.blocks) != 1 {
		t.Fatalf("clearing only history dropped the visible block: %d", len(session.term.blocks))
	}
}

func TestABlockDoesNotCoverTheNextPromptLine(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	startBlockPrompt(state, shell, session)
	shell.say("echo hi\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say("hi\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	block := &session.term.blocks[0]

	promptLine := block.end.line
	if index := session.term.blockAtLine(promptLine); index != -1 {
		t.Fatalf("the next prompt line %d belongs to block %d", promptLine, index)
	}
	if index := session.term.blockAtLine(promptLine - 1); index != 0 {
		t.Fatalf("the last output line %d belongs to block %d, want 0", promptLine-1, index)
	}

	// Typing the next command must not bring the finished block's toolbar back.
	sayMarkers(state, shell, session, blockMarker(session, "A"), "$ ", blockMarker(session, "B"))
	shell.say("next\r\n")
	state.Wake()
	terminal := session.term
	_, rows := terminal.screen.Size()
	terminal.buildDisplayMap(rows)
	for row := 0; row < rows; row++ {
		if terminal.displayLine(row) == promptLine {
			if index := terminal.blockAtLine(promptLine); index != -1 {
				t.Fatalf("typing on line %d reattached block %d", promptLine, index)
			}
			break
		}
	}
}

func TestABlockWithNoOutputDoesNotCoverThePromptLine(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	startBlockPrompt(state, shell, session)
	shell.say("cd /tmp\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"), blockMarker(session, "D;0"))
	block := &session.term.blocks[0]

	if index := session.term.blockAtLine(block.end.line); index != -1 {
		t.Fatalf("the empty prompt line %d belongs to block %d", block.end.line, index)
	}
	if index := session.term.blockAtLine(block.startLine()); index != 0 {
		t.Fatalf("the command line %d belongs to block %d, want 0", block.startLine(), index)
	}
}

func TestASingleOutputLineCanFoldAndExpandsFromItsSummary(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	startBlockPrompt(state, shell, session)
	shell.say("echo data\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say("xyzzy\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	block := &session.term.blocks[0]
	if blockFoldLines(block) != 1 {
		t.Fatalf("foldable lines = %d, want 1", blockFoldLines(block))
	}
	setBlockCollapsedForTest(block, session.term, true)
	canvas, texts := paintedTexts(state)
	if contains(texts, "xyzzy") {
		t.Fatalf("the single output line is still painted:\n%v", texts)
	}
	if !contains(texts, state.messages.Blocks.HiddenOneLine) {
		t.Fatalf("the singular fold summary is missing:\n%v", texts)
	}
	part := terminalControlParts(session.ID).At(terminalPartFoldSummaryBase)
	summaryHit, ok := hitByID(canvas, part)
	if !ok {
		t.Fatal("the summary row has no expand hit region")
	}
	if summaryHit.Role != gfx.RoleButton || !summaryHit.Focusable || summaryHit.SkipTab || !summaryHit.Expandable {
		t.Fatalf("the fold summary is not a keyboard-reachable disclosure: role=%v focusable=%v skipTab=%v expandable=%v", summaryHit.Role, summaryHit.Focusable, summaryHit.SkipTab, summaryHit.Expandable)
	}
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: part})
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, Target: part})
	if block.collapsed {
		t.Fatal("pressing the summary row did not expand the block")
	}
	_, texts = paintedTexts(state)
	if !contains(texts, "xyzzy") {
		t.Fatalf("the output did not return after expanding:\n%v", texts)
	}
}

func TestTheToolbarFollowsTheNextBlockWhenThePreviousOneIsFolded(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	startBlockPrompt(state, shell, session)
	shell.say("echo first\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	_, rows := session.term.screen.Size()
	shell.say(strings.Repeat("first output\r\n", rows/2))
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))

	sayMarkers(state, shell, session, blockMarker(session, "A"), "$ ", blockMarker(session, "B"))
	shell.say("echo second\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say("second\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))

	terminal := session.term
	if len(terminal.blocks) != 2 {
		t.Fatalf("blocks = %d, want 2", len(terminal.blocks))
	}
	setBlockCollapsedForTest(&terminal.blocks[0], terminal, true)
	terminal.buildDisplayMap(rows)
	row, ok := terminal.displayRowOfLine(terminal.blocks[1].startLine())
	if !ok {
		t.Fatal("the second block is not in the display map")
	}
	state.selectBlockAt(session, term.Position{Row: row, Column: 0})
	geometry := state.terminalWindowLayout(session.ID)
	canvas, _ := paintedTexts(state)
	part := terminalControlParts(session.ID).At(terminalPartBlockCopyOutput)
	hit, ok := hitByID(canvas, part)
	if !ok {
		t.Fatal("the second block's toolbar is missing")
	}
	rowTop := geometry.top + float32(row)*state.cellHeight
	if hit.Bounds.Y < rowTop || hit.Bounds.Y >= rowTop+state.cellHeight {
		t.Fatalf("toolbar y = %.1f, want within the block row [%.1f, %.1f)", hit.Bounds.Y, rowTop, rowTop+state.cellHeight)
	}
}

func TestASingleOutputLineWithoutANewlineCanFold(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	startBlockPrompt(state, shell, session)
	shell.say("printf data\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say("xyzzy")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	block := &session.term.blocks[0]
	if blockFoldLines(block) != 1 {
		t.Fatalf("foldable lines = %d, want 1", blockFoldLines(block))
	}
	setBlockCollapsedForTest(block, session.term, true)
	_, texts := paintedTexts(state)
	if contains(texts, "xyzzy") {
		t.Fatalf("the single output line is still painted:\n%v", texts)
	}
	if !contains(texts, state.messages.Blocks.HiddenOneLine) {
		t.Fatalf("the singular fold summary is missing:\n%v", texts)
	}
}

func TestCommandBlockSettingsDefaultOffAndIndependent(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	if state.terminalCommandBlocks {
		t.Fatal("command blocks are on by default")
	}
	if !state.setCommandBlocks(true) {
		t.Fatal("enabling the blocks changed nothing")
	}
	if !state.setCommandBlocks(false) {
		t.Fatal("disabling the blocks changed nothing")
	}
}

func TestCommandBlockSettingsPersist(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.setCommandBlocks(true)
	state.save()

	restored := newTestState(t, options)
	if !restored.terminalCommandBlocks {
		t.Fatal("the command block setting was not restored")
	}
	restored.setCommandBlocks(false)
	restored.save()
	again := newTestState(t, options)
	if again.terminalCommandBlocks {
		t.Fatal("a disabled command block setting was restored as enabled")
	}
}

func TestOpenTerminalsKeepCommandBlocksWhenTheDefaultChanges(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	state.setCommandBlocks(false)
	startBlockPrompt(state, shell, session)
	shell.say("echo kept\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say("kept\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	if len(session.term.blocks) != 1 {
		t.Fatalf("an open terminal stopped making blocks: %d", len(session.term.blocks))
	}
	if !session.term.blocksEnabled {
		t.Fatal("the open terminal lost the mode it started with")
	}

	// Configuring a terminal now is what opening a new one does; only that
	// terminal sees the default that was turned off.
	state.configureTerminal(session)
	if session.term.blocksEnabled {
		t.Fatal("a newly configured terminal picked up the old mode")
	}
	startBlockPrompt(state, shell, session)
	shell.say("echo hidden\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say("hidden\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	if len(session.term.blocks) != 0 {
		t.Fatalf("a new terminal made %d blocks while the default is off", len(session.term.blocks))
	}
}

func TestCommandBlockMenuRowsFollowTheOpenTerminal(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	startBlockPrompt(state, shell, session)
	shell.say("echo hi\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say("hi\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))

	state.setCommandBlocks(false)
	state.openTerminalMenuAt(session, term.Position{Row: 0, Column: 0})
	_, texts := paintedTexts(state)
	if !contains(texts, state.messages.Blocks.CopyCommand) {
		t.Fatalf("an open terminal with blocks lost its menu rows:\n%v", texts)
	}

	state.configureTerminal(session)
	state.openTerminalMenuAt(session, term.Position{Row: 0, Column: 0})
	_, texts = paintedTexts(state)
	if contains(texts, state.messages.Blocks.CopyCommand) {
		t.Fatalf("a terminal with blocks disabled shows block menu rows:\n%v", texts)
	}
}

func TestChangingTheCommandBlockDefaultKeepsExistingBlocks(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	startBlockPrompt(state, shell, session)
	shell.say("echo hi\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say("one\r\ntwo\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	if len(session.term.blocks) != 1 {
		t.Fatalf("blocks = %d, want 1", len(session.term.blocks))
	}
	block := &session.term.blocks[0]
	state.setCommandBlocks(false)
	if len(session.term.blocks) != 1 || !session.term.blocksEnabled {
		t.Fatalf("changing the default removed the open terminal's block: %d, enabled %v", len(session.term.blocks), session.term.blocksEnabled)
	}
	if !state.toggleBlock(session, block) || !block.collapsed {
		t.Fatal("the open terminal could not fold after the default changed")
	}
	if !state.toggleBlock(session, block) || block.collapsed {
		t.Fatal("the open terminal could not unfold after the default changed")
	}

	state.configureTerminal(session)
	if session.term.blocksEnabled || len(session.term.blocks) != 0 {
		t.Fatalf("a newly configured terminal kept the old mode: enabled %v, blocks %d", session.term.blocksEnabled, len(session.term.blocks))
	}
}

func TestCommandBlocksSettingFollowsTheStartupCommandsRow(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsTerminal)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 2000})
	startup, startupVisible := hitByID(&canvas, hitSettingsLocalStartupOpen)
	if !startupVisible {
		state.scrollSettings(gfx.ScrollEvent{DeltaY: state.settingsContent, Precise: true})
		canvas.Reset()
		state.Paint(&canvas, gfx.Size{Width: 1400, Height: 2000})
		startup, startupVisible = hitByID(&canvas, hitSettingsLocalStartupOpen)
	}
	if !startupVisible {
		t.Fatal("the startup commands row is not visible")
	}
	blocks, blocksVisible := hitByID(&canvas, hitSettingsCommandBlocks)
	if !blocksVisible {
		t.Fatal("the command blocks row is not visible")
	}
	if blocks.Bounds.Y <= startup.Bounds.Y {
		t.Fatalf("command blocks row y=%.1f is not below startup commands y=%.1f", blocks.Bounds.Y, startup.Bounds.Y)
	}
}

func TestCommandBlockSettingsToggleFromThePage(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.activeSection = sectionSettings

	state.Click(hitSettingsCommandBlocks)
	if !state.terminalCommandBlocks {
		t.Fatal("the page switch did not enable command blocks")
	}
	state.Click(hitSettingsCommandBlocks)
	if state.terminalCommandBlocks {
		t.Fatal("the page switch did not disable command blocks")
	}
}

func TestAFoldedSummaryDoesNotShowTheHiddenLineBackground(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	startBlockPrompt(state, shell, session)
	shell.say("docker images\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say("\x1b[48;5;196mInfo\x1b[0m plain\r\nsecond\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	block := &session.term.blocks[0]
	canvas, _ := paintedTexts(state)
	red := func(canvas *gfx.Canvas) int {
		count := 0
		for _, rectangle := range canvas.Rectangles() {
			if rectangle.Color.Red > 0.6 && rectangle.Color.Green < 0.3 && rectangle.Color.Blue < 0.3 {
				count++
			}
		}
		return count
	}
	if red(canvas) == 0 {
		t.Fatal("the fixture painted no red background to hide")
	}
	setBlockCollapsedForTest(block, session.term, true)
	canvas, _ = paintedTexts(state)
	if red(canvas) != 0 {
		t.Fatalf("the hidden line's red ANSI background is still painted: %d rectangles", red(canvas))
	}
	terminal := session.term
	_, rows := terminal.screen.Size()
	terminal.buildDisplayMap(rows)
	row, ok := terminal.displayRowOfLine(block.exec.line)
	if !ok {
		t.Fatal("the fold summary line is not in the display map")
	}
	geometry := state.terminalWindowLayout(session.ID)
	top := geometry.top + float32(row)*state.cellHeight
	for _, rectangle := range canvas.Rectangles() {
		if rectangle.Bounds.Y < top || rectangle.Bounds.Y >= top+state.cellHeight {
			continue
		}
		if rectangle.Color.Red > 0.6 && rectangle.Color.Green < 0.3 && rectangle.Color.Blue < 0.3 {
			t.Fatalf("the hidden line's red ANSI background is still painted on the summary row: %+v", rectangle)
		}
	}
}

func TestFoldingAMiddleBlockKeepsTheBlocksBelowVisible(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	terminal := session.term

	startBlockPrompt(state, shell, session)
	shell.say("echo first\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say("first output\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))

	_, rows := terminal.screen.Size()
	startBlockPrompt(state, shell, session)
	shell.say("echo middle\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say(strings.Repeat("middle output\r\n", rows+10))
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))

	startBlockPrompt(state, shell, session)
	shell.say("echo last\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say("last output\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))

	if len(terminal.blocks) != 3 {
		t.Fatalf("blocks = %d, want 3", len(terminal.blocks))
	}
	middle := &terminal.blocks[1]
	setBlockCollapsedForTest(middle, terminal, true)
	terminal.buildDisplayMap(rows)
	if len(terminal.display) != rows {
		t.Fatalf("display rows = %d, want %d", len(terminal.display), rows)
	}
	for _, entry := range terminal.display {
		if entry.index >= 0 && entry.line > middle.exec.line && entry.line < middle.endLine() {
			t.Fatalf("hidden middle line %d is displayed", entry.line)
		}
	}
	last := &terminal.blocks[2]
	for _, line := range []uint64{last.startLine(), last.command.line, last.exec.line, last.end.line} {
		if _, ok := terminal.displayRowOfLine(line); !ok {
			t.Fatalf("the last block's line %d is not in the display map", line)
		}
	}
}

func TestFoldingAMiddleBlockWhileScrolledShowsTheWholeFoldedView(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	terminal := session.term
	startBlockPrompt(state, shell, session)
	shell.say("echo first\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say("first\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	_, rows := terminal.screen.Size()
	startBlockPrompt(state, shell, session)
	shell.say("echo middle\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say(strings.Repeat("middle\r\n", rows+20))
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	startBlockPrompt(state, shell, session)
	shell.say("echo last\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say("last\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))

	middle := &terminal.blocks[1]
	visible := false
	for attempt := 0; attempt <= terminal.screen.RetainedLines(); attempt++ {
		top := terminal.screen.LineNumber(0)
		if middle.exec.line >= top && middle.exec.line-top < uint64(rows) {
			visible = true
			break
		}
		if !terminal.screen.Scroll(1) {
			break
		}
	}
	if !visible {
		t.Fatal("the fold summary was not visible before folding")
	}
	if !state.toggleBlock(session, middle) {
		t.Fatal("the fold did not change")
	}
	terminal.buildDisplayMap(rows)
	if terminal.screen.ViewOffset() != 0 {
		t.Fatalf("a folded view that fits kept a raw view offset: %d", terminal.screen.ViewOffset())
	}
	if _, ok := terminal.displayRowOfLine(middle.exec.line); !ok {
		t.Fatal("the fold summary disappeared after folding")
	}
	last := &terminal.blocks[2]
	if _, ok := terminal.displayRowOfLine(last.startLine()); !ok {
		t.Fatal("the block below the fold did not come into view")
	}
}

func TestASingleCollapsedBlockStaysVisible(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	terminal := session.term
	_, rows := terminal.screen.Size()
	startBlockPrompt(state, shell, session)
	shell.say("docker images\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say(strings.Repeat("image row\r\n", rows+10))
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	block := &terminal.blocks[0]
	setBlockCollapsedForTest(block, terminal, true)

	terminal.buildDisplayMap(rows)
	if _, ok := terminal.displayRowOfLine(block.exec.line); !ok {
		t.Fatal("collapsing the only block removed its summary from the screen")
	}
	last := terminal.screen.LineNumber(rows - 1)
	if _, ok := terminal.displayRowOfLine(last); !ok {
		t.Fatal("collapsing the only block removed the live prompt from the screen")
	}
	_, texts := paintedTexts(state)
	summary := fmt.Sprintf(state.messages.Blocks.HiddenLinesFmt, blockFoldLines(block))
	if !contains(texts, summary) {
		t.Fatalf("the fold summary %q is not painted:\n%v", summary, texts)
	}
}

func TestClickingSelectsABlockAndClickingTheInputLineClearsIt(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	startBlockPrompt(state, shell, session)
	shell.say("echo hi\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say("hi\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))

	geometry := state.terminalWindowLayout(session.ID)
	hit := terminalTestHit(state, session)
	at := gfx.Point{X: geometry.left + 8, Y: geometry.top + 4}
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, Target: hit, X: at.X, Y: at.Y})
	if session.term.blockSelected != -1 {
		t.Fatal("moving the pointer selected a block")
	}
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: hit, X: at.X, Y: at.Y})
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, Target: hit, X: at.X, Y: at.Y})
	if session.term.blockSelected != 0 {
		t.Fatalf("clicking the block selected %d, want 0", session.term.blockSelected)
	}
	canvas, _ := paintedTexts(state)
	part := terminalControlParts(session.ID).At(terminalPartBlockCopyOutput)
	if _, ok := hitByID(canvas, part); !ok {
		t.Fatal("the selected block has no toolbar")
	}

	_, rows := session.term.screen.Size()
	input := gfx.Point{X: geometry.left + 8, Y: geometry.top + float32(rows-1)*state.cellHeight + 2}
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: hit, X: input.X, Y: input.Y})
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, Target: hit, X: input.X, Y: input.Y})
	if session.term.blockSelected != -1 {
		t.Fatalf("clicking the input line selected block %d", session.term.blockSelected)
	}
	canvas, _ = paintedTexts(state)
	if _, ok := hitByID(canvas, part); ok {
		t.Fatal("the toolbar stayed after the block was deselected")
	}
}

func TestScrollingDoesNotLeaveAFoldedViewThatFits(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	terminal := session.term
	_, rows := terminal.screen.Size()
	startBlockPrompt(state, shell, session)
	shell.say("docker images\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say(strings.Repeat("image row\r\n", rows+20))
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	block := &terminal.blocks[0]
	if !state.toggleBlock(session, block) {
		t.Fatal("the block did not fold")
	}
	if terminal.screen.ViewOffset() != 0 {
		t.Fatalf("folding a fitting view left the view at %d", terminal.screen.ViewOffset())
	}
	for _, direction := range []int{-1, 1} {
		for step := 0; step < rows+5; step++ {
			terminal.scrollDisplay(direction)
			terminal.buildDisplayMap(rows)
			if terminal.screen.ViewOffset() != 0 {
				t.Fatalf("scrolling %d entered hidden rows: view %d", direction, terminal.screen.ViewOffset())
			}
			if _, ok := terminal.displayRowOfLine(block.exec.line); !ok {
				t.Fatalf("scrolling %d hid the folded summary at step %d", direction, step)
			}
		}
	}
}

func TestScrollingAtTheEndsDoesNotDisturbAFoldedViewThatOverflows(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	terminal := session.term
	_, rows := terminal.screen.Size()
	startBlockPrompt(state, shell, session)
	shell.say("docker images\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say(strings.Repeat("image row\r\n", rows*3))
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	block := &terminal.blocks[0]
	if !state.toggleBlock(session, block) {
		t.Fatal("the block did not fold")
	}

	// Remaining content still needs more than one screen.
	startBlockPrompt(state, shell, session)
	shell.say("echo below\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say(strings.Repeat("below row\r\n", rows*2))
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))

	total, _ := terminal.displayScroll(rows)
	if total <= rows {
		t.Fatalf("the folded view fits (%d rows); the test needs an overflowing view", total)
	}

	terminal.buildDisplayMap(rows)
	for step := 0; step < rows*4; step++ {
		terminal.scrollDisplay(-1)
	}
	terminal.buildDisplayMap(rows)
	if terminal.displayOffset != 0 {
		t.Fatalf("scrolling down stopped at offset %d, want 0", terminal.displayOffset)
	}
	bottom := append([]displayEntry(nil), terminal.display...)
	for step := 0; step < rows; step++ {
		terminal.scrollDisplay(-1)
	}
	terminal.buildDisplayMap(rows)
	if terminal.displayOffset != 0 {
		t.Fatalf("extra scrolling down moved the view to %d", terminal.displayOffset)
	}
	for row := range bottom {
		if bottom[row] != terminal.display[row] {
			t.Fatalf("extra scrolling down changed display row %d", row)
		}
	}

	_, maxOffset := terminal.displayCapacity(rows)
	for step := 0; step < maxOffset+rows; step++ {
		terminal.scrollDisplay(1)
	}
	terminal.buildDisplayMap(rows)
	if terminal.displayOffset != maxOffset {
		t.Fatalf("scrolling up stopped at offset %d, want %d", terminal.displayOffset, maxOffset)
	}
	if _, ok := terminal.displayRowOfLine(block.exec.line); !ok {
		t.Fatal("the folded summary left the screen at the top")
	}
	top := append([]displayEntry(nil), terminal.display...)
	for step := 0; step < rows; step++ {
		terminal.scrollDisplay(1)
	}
	terminal.buildDisplayMap(rows)
	if terminal.displayOffset != maxOffset {
		t.Fatalf("extra scrolling up moved the view to %d", terminal.displayOffset)
	}
	for row := range top {
		if top[row] != terminal.display[row] {
			t.Fatalf("extra scrolling up changed display row %d", row)
		}
	}
}

func TestRawIndexForDisplayOffsetMatchesVisibleScan(t *testing.T) {
	terminal := newTerminalWithScrollback(20, 5, 100)
	terminal.screen.Write([]byte(strings.Repeat("line\r\n", 30)))
	terminal.blocksEnabled = true
	terminal.blocks = []commandBlock{
		{id: 1, prompt: blockAnchor{line: 4, ok: true}, command: blockAnchor{line: 4, ok: true}, exec: blockAnchor{line: 5, ok: true}, end: blockAnchor{line: 12, ok: true}, status: blockSucceeded, collapsed: true},
		{id: 2, prompt: blockAnchor{line: 12, ok: true}, command: blockAnchor{line: 12, ok: true}, exec: blockAnchor{line: 13, ok: true}, end: blockAnchor{line: 20, ok: true}, status: blockSucceeded, collapsed: true},
		{id: 3, prompt: blockAnchor{line: 20, ok: true}, command: blockAnchor{line: 20, ok: true}, exec: blockAnchor{line: 21, ok: true}, end: blockAnchor{line: 28, ok: true}, status: blockSucceeded},
	}
	terminal.markFoldsDirty()

	base, ok := terminal.screen.RetainedFrom()
	if !ok {
		t.Fatal("the test terminal has no retained lines")
	}
	count := terminal.screen.RetainedLines()
	var visible []int
	for index := count - 1; index >= 0; index-- {
		line := base + uint64(index)
		hidden := false
		for i := range terminal.blocks {
			block := &terminal.blocks[i]
			if block.collapsed && line > block.exec.line && line < terminal.blockFoldEnd(i) {
				hidden = true
				break
			}
		}
		if !hidden {
			visible = append(visible, index)
		}
	}
	if len(visible) < 10 {
		t.Fatalf("fixture has only %d visible lines", len(visible))
	}
	for offset := 0; offset < len(visible); offset++ {
		want := visible[offset]
		if got := terminal.rawIndexForDisplayOffset(offset); got != want {
			t.Fatalf("offset %d maps to raw index %d, want %d", offset, got, want)
		}
	}
}

func TestCopyingABlockDoesNotIncludeTheNextPrompt(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	startBlockPrompt(state, shell, session)
	shell.say("echo hi\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say("hi\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	// The next prompt is drawn on the line after the output, starting at the
	// exact point D reported. None of it belongs to the block.
	shell.say("$ next prompt")
	state.Wake()

	block := &session.term.blocks[0]
	if command, ok := session.term.blockText(block, blockCopyCommand); !ok || command != "echo hi" {
		t.Fatalf("command = %q, %v; want %q", command, ok, "echo hi")
	}
	if output, ok := session.term.blockText(block, blockCopyOutput); !ok || output != "hi" {
		t.Fatalf("output = %q, %v; want %q", output, ok, "hi")
	}
	if all, ok := session.term.blockText(block, blockCopyAll); !ok || all != "echo hi\nhi" {
		t.Fatalf("block = %q, %v; want %q", all, ok, "echo hi\nhi")
	}
}

func TestACollapsedBlockWhoseHeadLeftTheScrollbackStaysVisible(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	terminal := session.term
	terminal.screen.SetScrollbackLimit(10)
	startBlockPrompt(state, shell, session)
	shell.say("echo many\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say(strings.Repeat("line\r\n", 100))
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))

	block := &terminal.blocks[0]
	first, ok := terminal.screen.RetainedFrom()
	if !ok || first <= block.exec.line {
		t.Fatalf("the fixture did not trim the block head: first %d exec %d", first, block.exec.line)
	}
	if !state.toggleBlock(session, block) {
		t.Fatal("the block did not fold")
	}
	_, rows := terminal.screen.Size()
	terminal.buildDisplayMap(rows)
	summary := terminal.blockFoldSummaryLine(block, first)
	if _, ok := terminal.displayRowOfLine(summary); !ok {
		t.Fatalf("the trimmed block's summary line %d is not visible", summary)
	}
	if _, ok := terminal.displayRowOfLine(first + 1); ok {
		t.Fatal("a hidden trimmed output line is still displayed")
	}
	_, texts := paintedTexts(state)
	if !contains(texts, fmt.Sprintf(state.messages.Blocks.HiddenLinesFmt, blockFoldLines(block))) &&
		!contains(texts, state.messages.Blocks.HiddenOneLine) {
		t.Fatalf("the fold summary is not painted:\n%v", texts)
	}
}

func TestResizingAFoldedBlockNeverShowsItsHiddenOutput(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	terminal := session.term
	startBlockPrompt(state, shell, session)
	shell.say("echo many\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	for line := 0; line < 30; line++ {
		shell.say(fmt.Sprintf("hidden-line-%02d\r\n", line))
	}
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	block := &terminal.blocks[0]
	if !state.toggleBlock(session, block) {
		t.Fatal("the block did not fold")
	}

	for _, size := range []struct{ cols, rows int }{
		{0, 10}, {8, 0}, {0, -4}, {-6, 3}, {4, -2},
	} {
		cols, rows := terminal.screen.Size()
		nextCols, nextRows := cols+size.cols, rows+size.rows
		if nextCols < minTerminalCols || nextRows < minTerminalRows {
			continue
		}
		terminal.screen.Resize(nextCols, nextRows)
		if nextCols != cols {
			state.remapBlocks(session)
		}
		_, rows = terminal.screen.Size()
		terminal.buildDisplayMap(rows)

		first, _ := terminal.screen.RetainedFrom()
		summary := terminal.blockFoldSummaryLine(block, first)
		for _, entry := range terminal.display {
			if entry.index >= 0 && entry.line > summary && entry.line < block.endLine() {
				t.Fatalf("resize %+v displayed hidden line %d above/inside the block", size, entry.line)
			}
		}
		if _, ok := terminal.displayRowOfLine(summary); !ok {
			t.Fatalf("resize %+v lost the folded summary", size)
		}
		_, texts := paintedTexts(state)
		for line := 0; line < 30; line++ {
			if contains(texts, fmt.Sprintf("hidden-line-%02d", line)) {
				t.Fatalf("resize %+v painted hidden output line %d", size, line)
			}
		}
	}
}

func TestResizingWhileScrolledKeepsFoldedRowsHidden(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	terminal := session.term
	_, rows := terminal.screen.Size()
	startBlockPrompt(state, shell, session)
	shell.say("echo many\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say(strings.Repeat("hidden-row\r\n", 60))
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	block := &terminal.blocks[0]
	if !state.toggleBlock(session, block) {
		t.Fatal("the block did not fold")
	}
	startBlockPrompt(state, shell, session)
	shell.say("echo below\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say(strings.Repeat("visible-row\r\n", rows*2))
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	if total, _ := terminal.displayScroll(rows); total <= rows {
		t.Skip("the fixture does not overflow")
	}
	terminal.scrollDisplay(10)
	if terminal.displayOffset == 0 {
		t.Fatal("the test terminal did not scroll")
	}

	for _, size := range []struct{ cols, rows int }{{0, 7}, {5, 0}, {0, -5}, {-4, 4}} {
		cols, rows := terminal.screen.Size()
		nextCols, nextRows := cols+size.cols, rows+size.rows
		if nextCols < minTerminalCols || nextRows < minTerminalRows {
			continue
		}
		terminal.screen.Resize(nextCols, nextRows)
		if nextCols != cols {
			state.remapBlocks(session)
		}
		_, rows = terminal.screen.Size()
		terminal.buildDisplayMap(rows)
		first, _ := terminal.screen.RetainedFrom()
		summary := terminal.blockFoldSummaryLine(block, first)
		for _, entry := range terminal.display {
			if entry.index >= 0 && entry.line > summary && entry.line < block.endLine() {
				t.Fatalf("resize %+v displayed hidden row %d", size, entry.line)
			}
		}
		_, texts := paintedTexts(state)
		if contains(texts, "hidden-row") {
			t.Fatalf("resize %+v painted hidden output", size)
		}
	}
}

func TestFoldingFollowsTheRetainedBufferAfterHistoryTrim(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	terminal := session.term
	startBlockPrompt(state, shell, session)
	shell.say("echo many\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say(strings.Repeat("keep-hidden\r\n", 30))
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	block := &terminal.blocks[0]
	if !state.toggleBlock(session, block) {
		t.Fatal("the block did not fold")
	}
	before, _ := terminal.screen.RetainedFrom()

	// Trim history without dropping the block itself: the fold index's captured
	// buffer indices must be rebuilt against the new base.
	terminal.screen.SetScrollbackLimit(5)
	_, rows := terminal.screen.Size()
	terminal.screen.Write([]byte(strings.Repeat("later\r\n", rows+10)))
	state.Wake()
	first, ok := terminal.screen.RetainedFrom()
	if !ok || first == before {
		t.Fatalf("the fixture did not change the retained base: %d -> %d", before, first)
	}
	if block.endLine() < first {
		return // the block left the buffer with the trimmed history
	}
	terminal.buildDisplayMap(rows)
	summary := terminal.blockFoldSummaryLine(block, first)
	for _, entry := range terminal.display {
		if entry.index >= 0 && entry.line > summary && entry.line < block.endLine() {
			t.Fatalf("history trim displayed hidden line %d", entry.line)
		}
	}
	_, texts := paintedTexts(state)
	if contains(texts, "keep-hidden") {
		t.Fatal("history trim painted hidden block output")
	}
}

func TestALargeFoldedBlockSurvivesAResize(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	terminal := session.term
	terminal.screen.SetScrollbackLimit(50)
	startBlockPrompt(state, shell, session)
	shell.say("echo large-command\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say(strings.Repeat("large-output\r\n", 200))
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	block := &terminal.blocks[0]
	if !state.toggleBlock(session, block) {
		t.Fatal("the large block did not fold")
	}
	cols, rows := terminal.screen.Size()
	terminal.screen.Resize(max(cols-10, minTerminalCols), rows)
	state.remapBlocks(session)

	if !block.collapsed || blockFoldLines(block) == 0 {
		t.Fatalf("the fold was lost after resize: collapsed %v, lines %d", block.collapsed, blockFoldLines(block))
	}
	first, ok := terminal.screen.RetainedFrom()
	if !ok {
		t.Fatal("the test terminal has no retained lines")
	}
	summary := terminal.blockFoldSummaryLine(block, first)
	terminal.buildDisplayMap(rows)
	if _, ok := terminal.displayRowOfLine(summary); !ok {
		t.Fatal("the folded summary is not visible after resize")
	}
	_, texts := paintedTexts(state)
	if !contains(texts, "echo large-command") {
		t.Fatalf("the trimmed command is not shown in the summary:\n%v", texts)
	}
	if !state.toggleBlock(session, block) || block.collapsed {
		t.Fatal("the large block could not expand after resize")
	}
	if !state.toggleBlock(session, block) || !block.collapsed {
		t.Fatal("the large block could not fold again after resize")
	}
}

func TestCopyingALargeBlockUsesTheRetainedTail(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	terminal := session.term
	terminal.screen.SetScrollbackLimit(20)
	startBlockPrompt(state, shell, session)
	shell.say("echo large\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	for line := 0; line < 120; line++ {
		shell.say(fmt.Sprintf("line-%03d\r\n", line))
	}
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	block := &terminal.blocks[0]
	if !state.toggleBlock(session, block) {
		t.Fatal("the block did not fold")
	}
	first, _ := terminal.screen.RetainedFrom()
	if first <= block.exec.line {
		t.Fatalf("the fixture did not trim the output: first %d exec %d", first, block.exec.line)
	}
	if !terminal.blockTextAvailable(block, blockCopyOutput) {
		t.Fatal("the retained output is reported as unavailable")
	}

	copied := ""
	state.options.SetClipboardText = func(text string) error {
		copied = text
		return nil
	}
	terminal.blockSelected = 0
	if !state.copyHoveredBlock(session, blockCopyOutput) {
		t.Fatal("copying the retained output did nothing")
	}
	if copied == "" {
		t.Fatal("the clipboard stayed empty")
	}
	if !strings.Contains(copied, "line-119") {
		t.Fatalf("the retained tail is missing:\n%s", copied)
	}
	if strings.Contains(copied, "line-000") {
		t.Fatalf("the trimmed head was copied:\n%s", copied)
	}

	// A width resize remaps the anchors; the retained tail must stay copyable.
	cols, rows := terminal.screen.Size()
	terminal.screen.Resize(max(cols-8, minTerminalCols), rows)
	state.remapBlocks(session)
	if !terminal.blockTextAvailable(block, blockCopyOutput) {
		t.Fatal("the retained output became unavailable after resize")
	}
	if output, ok := terminal.blockText(block, blockCopyOutput); !ok || !strings.Contains(output, "line-119") {
		t.Fatalf("output after resize = %q, %v", output, ok)
	}
}

func TestAReflowDoesNotRelabelACommandWithAnotherHistoryLine(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	terminal := session.term
	startBlockPrompt(state, shell, session)
	shell.say("echo original-command\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say("one\r\ntwo\r\nthree\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	block := &terminal.blocks[0]
	if !state.toggleBlock(session, block) {
		t.Fatal("the block did not fold")
	}

	// Simulate a reflow losing the prompt and command row identities while the
	// old line numbers still land inside the retained buffer.
	first, ok := terminal.screen.RetainedFrom()
	if !ok {
		t.Fatal("the terminal has no retained lines")
	}
	block.command.line = first
	block.command.id = 1 << 40
	block.prompt.line = first
	block.prompt.id = 1 << 40
	state.remapBlocks(session)
	if block.command.ok || block.prompt.ok {
		t.Fatal("an unresolved command row was kept")
	}
	if got := terminal.blockDisplayStart(block); got != first {
		t.Fatalf("block starts at line %d, want the retained head %d", got, first)
	}
	_, texts := paintedTexts(state)
	if !contains(texts, "echo original-command") {
		t.Fatalf("the real command is not shown in the summary:\n%v", texts)
	}
}

func TestExpandingALargeFoldedBlockAfterResizeStaysOperable(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	terminal := session.term
	terminal.screen.SetScrollbackLimit(5000)
	startBlockPrompt(state, shell, session)
	shell.say("echo large-resize\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say(strings.Repeat(strings.Repeat("x", 200)+"\r\n", 2400))
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	for i := 0; i < 50; i++ {
		state.Wake()
	}
	block := &terminal.blocks[0]
	if !state.toggleBlock(session, block) {
		t.Fatal("the block did not fold")
	}
	if blockFoldLines(block) >= 5000 {
		t.Fatalf("fixture already folds %d lines", blockFoldLines(block))
	}
	cols, rows := terminal.screen.Size()
	terminal.screen.Resize(max(cols/2, minTerminalCols), rows)
	state.remapBlocks(session)
	if blockFoldLines(block) < 5000 {
		t.Skipf("resize did not cross 5000 folded lines: %d", blockFoldLines(block))
	}

	if !state.toggleBlock(session, block) || block.collapsed {
		t.Fatal("the large block did not expand")
	}
	if blockFoldLines(block) == 0 {
		t.Fatal("the block lost its fold range after expanding")
	}
	terminal.buildDisplayMap(rows)
	found := false
	for row := 0; row < rows; row++ {
		line := terminal.displayLine(row)
		if line >= terminal.blockDisplayStart(block) && line <= block.visualEndLine() {
			if index := terminal.blockAtLine(line); index != 0 {
				t.Fatalf("visible block line %d maps to block %d, want 0", line, index)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("no line of the expanded block is visible")
	}
	if !terminal.blockTextAvailable(block, blockCopyCommand) {
		t.Fatal("the command is no longer copyable")
	}
	if !terminal.blockTextAvailable(block, blockCopyOutput) {
		t.Fatal("the retained output is no longer copyable")
	}
	terminal.blockSelected = 0
	canvas, _ := paintedTexts(state)
	parts := terminalControlParts(session.ID)
	for _, part := range []int{terminalPartBlockCopyCommand, terminalPartBlockCopyOutput, terminalPartBlockToggleFold} {
		if _, ok := hitByID(canvas, parts.At(part)); !ok {
			t.Fatalf("the expanded block is missing toolbar part %d", part)
		}
	}
	if !state.toggleBlock(session, block) || !block.collapsed {
		t.Fatal("the large block could no longer fold")
	}
}

func TestATruncatedFoldedBlockShowsAWarningIcon(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	terminal := session.term
	terminal.screen.SetScrollbackLimit(10)
	startBlockPrompt(state, shell, session)
	shell.say("echo many\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say(strings.Repeat("line\r\n", 100))
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	block := &terminal.blocks[0]
	if !state.toggleBlock(session, block) {
		t.Fatal("the block did not fold")
	}
	if !terminal.blockTruncated(block) {
		t.Fatal("the block is not marked truncated")
	}
	terminal.blockSelected = 0
	canvas, _ := paintedTexts(state)
	parts := terminalControlParts(session.ID)
	hint, ok := hitByID(canvas, parts.At(terminalPartBlockTruncatedHint))
	if !ok {
		t.Fatal("the truncated block has no warning icon")
	}
	if hint.Hint != state.messages.Blocks.TruncatedHint {
		t.Fatalf("warning hint = %q, want %q", hint.Hint, state.messages.Blocks.TruncatedHint)
	}
	copyButton, ok := hitByID(canvas, parts.At(terminalPartBlockCopyCommand))
	if !ok {
		t.Fatal("the copy-command button is missing")
	}
	if hint.Bounds.X >= copyButton.Bounds.X {
		t.Fatalf("warning x = %.1f, copy x = %.1f; warning must be left of the actions", hint.Bounds.X, copyButton.Bounds.X)
	}
}

func TestAFullyRetainedFoldedBlockHasNoWarningIcon(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	terminal := session.term
	startBlockPrompt(state, shell, session)
	shell.say("echo short\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say("one\r\ntwo\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	block := &terminal.blocks[0]
	if !state.toggleBlock(session, block) {
		t.Fatal("the block did not fold")
	}
	terminal.blockSelected = 0
	canvas, _ := paintedTexts(state)
	if _, ok := hitByID(canvas, terminalControlParts(session.ID).At(terminalPartBlockTruncatedHint)); ok {
		t.Fatal("a fully retained block shows the truncated warning")
	}
}

func TestLargeFoldSurvivesARealWindowResize(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	terminal := session.term
	terminal.screen.SetScrollbackLimit(5000)
	startBlockPrompt(state, shell, session)
	shell.say("echo huge\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say(strings.Repeat(strings.Repeat("x", 200)+"\r\n", 2400))
	for i := 0; i < 50; i++ {
		state.Wake()
	}
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	for i := 0; i < 10; i++ {
		state.Wake()
	}
	block := &terminal.blocks[0]
	if !state.toggleBlock(session, block) {
		t.Fatalf("initial fold failed: lines=%d status=%d execOK=%v", blockFoldLines(block), block.status, block.exec.ok)
	}
	for _, size := range []gfx.Size{{Width: 1000, Height: 700}, {Width: 1000, Height: 500}, {Width: 700, Height: 500}} {
		state.Resize(size)
		if !block.collapsed {
			t.Fatalf("resize %+v expanded the block", size)
		}
		_, rows := terminal.screen.Size()
		terminal.buildDisplayMap(rows)
		first, _ := terminal.screen.RetainedFrom()
		summary := terminal.blockFoldSummaryLine(block, first)
		if _, ok := terminal.displayRowOfLine(summary); !ok {
			t.Fatalf("resize %+v lost the folded summary", size)
		}
	}
}

func TestCrossingTheScrollbackLimitDuringARealResizeKeepsTheFoldOperable(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	terminal := session.term
	terminal.screen.SetScrollbackLimit(5000)
	startBlockPrompt(state, shell, session)
	shell.say("echo crossing\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say(strings.Repeat(strings.Repeat("x", 200)+"\r\n", 2400))
	for i := 0; i < 50; i++ {
		state.Wake()
	}
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	for i := 0; i < 20; i++ {
		state.Wake()
	}
	block := &terminal.blocks[0]
	if !state.toggleBlock(session, block) {
		t.Fatalf("initial fold failed: %d lines", blockFoldLines(block))
	}
	if blockFoldLines(block) >= 5000 {
		t.Skipf("fixture already folds %d lines", blockFoldLines(block))
	}
	state.Resize(gfx.Size{Width: 700, Height: 600})
	if !block.collapsed {
		t.Fatal("the resize expanded the folded block")
	}
	if blockFoldLines(block) < 5000 {
		t.Skipf("resize did not cross 5000 folded lines: %d", blockFoldLines(block))
	}
	_, rows := terminal.screen.Size()
	terminal.buildDisplayMap(rows)
	first, _ := terminal.screen.RetainedFrom()
	summary := terminal.blockFoldSummaryLine(block, first)
	if _, ok := terminal.displayRowOfLine(summary); !ok {
		t.Fatal("the folded summary disappeared after the resize")
	}
	terminal.blockSelected = 0
	canvas, _ := paintedTexts(state)
	parts := terminalControlParts(session.ID)
	for _, part := range []int{terminalPartBlockCopyCommand, terminalPartBlockCopyOutput, terminalPartBlockToggleFold} {
		if _, ok := hitByID(canvas, parts.At(part)); !ok {
			t.Fatalf("the resized block is missing toolbar part %d", part)
		}
	}
	if _, ok := hitByID(canvas, parts.At(terminalPartFoldSummaryBase)); !ok {
		t.Fatal("the resized fold summary has no hit region")
	}
	if !state.toggleBlock(session, block) || block.collapsed {
		t.Fatal("the resized block could not expand")
	}
	if !state.toggleBlock(session, block) || !block.collapsed {
		t.Fatal("the resized block could not fold again")
	}
}

func TestRepeatedResizesKeepALargeFoldOperable(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	terminal := session.term
	terminal.screen.SetScrollbackLimit(5000)
	startBlockPrompt(state, shell, session)
	shell.say("echo repeated\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say(strings.Repeat("row\r\n", 5200))
	for i := 0; i < 50; i++ {
		state.Wake()
	}
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	for i := 0; i < 10; i++ {
		state.Wake()
	}
	block := &terminal.blocks[0]
	if !state.toggleBlock(session, block) {
		t.Fatalf("initial fold failed: %d lines", blockFoldLines(block))
	}
	sizes := []gfx.Size{
		{Width: 1000, Height: 700}, {Width: 700, Height: 500}, {Width: 1200, Height: 800},
		{Width: 600, Height: 450}, {Width: 900, Height: 650}, {Width: 800, Height: 600},
		{Width: 1100, Height: 750}, {Width: 650, Height: 480},
	}
	for round := 0; round < 5; round++ {
		for _, size := range sizes {
			state.Resize(size)
			if !block.collapsed {
				t.Fatalf("round %d resize %+v expanded the block", round, size)
			}
			_, rows := terminal.screen.Size()
			terminal.buildDisplayMap(rows)
			first, _ := terminal.screen.RetainedFrom()
			summary := terminal.blockFoldSummaryLine(block, first)
			if _, ok := terminal.displayRowOfLine(summary); !ok {
				t.Fatalf("round %d resize %+v lost the summary: lines=%d head=%v stale=%v exec=%d end=%d first=%d", round, size, blockFoldLines(block), block.headTrimmed, block.stale, block.exec.line, block.end.line, first)
			}
			cursor, ok := terminal.screen.CursorPoint()
			if ok {
				for _, entry := range terminal.display {
					if entry.index >= 0 && entry.line > summary && entry.line < cursor.Line {
						t.Fatalf("round %d resize %+v showed folded output line %d before prompt %d", round, size, entry.line, cursor.Line)
					}
				}
			}
		}
	}
}

func TestAFoldedBlockSurvivesAnEndAnchorOutsideTheRetainedBuffer(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	terminal := session.term
	terminal.screen.SetScrollbackLimit(50)
	startBlockPrompt(state, shell, session)
	shell.say("echo large\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say(strings.Repeat("row\r\n", 200))
	for i := 0; i < 20; i++ {
		state.Wake()
	}
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	for i := 0; i < 10; i++ {
		state.Wake()
	}
	block := &terminal.blocks[0]
	if !state.toggleBlock(session, block) {
		t.Fatalf("initial fold failed: %d lines", blockFoldLines(block))
	}
	count := terminal.screen.RetainedLines()
	first, _ := terminal.screen.RetainedFrom()
	block.end.id = 1 << 40
	block.end.line = first + uint64(count) + 50
	state.remapBlocks(session)
	if !block.end.ok || !block.collapsed {
		t.Fatalf("the end anchor was not clamped: ok=%v collapsed=%v", block.end.ok, block.collapsed)
	}
	if block.end.line >= first+uint64(count) {
		t.Fatalf("end line %d is still outside the retained buffer", block.end.line)
	}
	if got := blockFoldLines(block); got == 0 {
		t.Fatal("the fold range disappeared")
	}
	_, rows := terminal.screen.Size()
	terminal.buildDisplayMap(rows)
	summary := terminal.blockFoldSummaryLine(block, first)
	if _, ok := terminal.displayRowOfLine(summary); !ok {
		t.Fatal("the folded summary disappeared")
	}
}

func TestScrollingToTheTopAfterNewOutputKeepsThePreviousFoldHidden(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	terminal := session.term
	terminal.screen.SetScrollbackLimit(5000)
	startBlockPrompt(state, shell, session)
	shell.say("cat history | tail -4883\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say(strings.Repeat("history row\r\n", 5200))
	for i := 0; i < 50; i++ {
		state.Wake()
	}
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	for i := 0; i < 10; i++ {
		state.Wake()
	}
	first := &terminal.blocks[0]
	if !state.toggleBlock(session, first) {
		t.Fatalf("initial fold failed: %d lines", blockFoldLines(first))
	}

	startBlockPrompt(state, shell, session)
	shell.say("cat history | tail -4883\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say(strings.Repeat("new history row\r\n", 1200))
	for i := 0; i < 50; i++ {
		state.Wake()
	}
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	for i := 0; i < 10; i++ {
		state.Wake()
	}
	if len(terminal.blocks) != 2 {
		t.Fatalf("blocks = %d, want 2", len(terminal.blocks))
	}
	second := &terminal.blocks[1]

	_, rows := terminal.screen.Size()
	_, maxOffset := terminal.displayCapacity(rows)
	terminal.scrollDisplay(maxOffset + rows)
	terminal.buildDisplayMap(rows)
	firstVisible, _ := terminal.screen.RetainedFrom()
	summary := terminal.blockFoldSummaryLine(first, firstVisible)
	end := terminal.blockFoldEnd(0)
	sawSummary := false
	for _, entry := range terminal.display {
		if entry.index < 0 {
			continue
		}
		if entry.line == summary {
			sawSummary = true
			continue
		}
		if entry.line > summary && entry.line < end {
			t.Fatalf("row %d: folded block leaked line %d before next block at %d", entry.line, entry.line, second.startLine())
		}
	}
	if !sawSummary {
		t.Fatal("the folded summary is not visible at the top")
	}
}

func TestScrollingToTheTopWithWrappedFoldedRows(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	terminal := session.term
	terminal.screen.SetScrollbackLimit(5000)
	startBlockPrompt(state, shell, session)
	shell.say("cat history\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say(strings.Repeat(strings.Repeat("x", 200)+"\r\n", 2600))
	for i := 0; i < 60; i++ {
		state.Wake()
	}
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	for i := 0; i < 10; i++ {
		state.Wake()
	}
	first := &terminal.blocks[0]
	if !state.toggleBlock(session, first) {
		t.Fatalf("initial fold failed: %d lines", blockFoldLines(first))
	}
	startBlockPrompt(state, shell, session)
	shell.say("cat history | tail -4883\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say(strings.Repeat("new row\r\n", 1200))
	for i := 0; i < 50; i++ {
		state.Wake()
	}
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	for i := 0; i < 10; i++ {
		state.Wake()
	}
	if len(terminal.blocks) != 2 {
		t.Fatalf("blocks = %d, want 2", len(terminal.blocks))
	}
	_, rows := terminal.screen.Size()
	_, maxOffset := terminal.displayCapacity(rows)
	terminal.scrollDisplay(maxOffset + rows)
	terminal.buildDisplayMap(rows)
	firstVisible, _ := terminal.screen.RetainedFrom()
	summary := terminal.blockFoldSummaryLine(first, firstVisible)
	end := terminal.blockFoldEnd(0)
	sawSummary := false
	for _, entry := range terminal.display {
		if entry.index < 0 {
			continue
		}
		if entry.line == summary {
			sawSummary = true
			continue
		}
		if entry.line > summary && entry.line < end {
			t.Fatalf("folded block leaked line %d (summary %d, end %d)", entry.line, summary, end)
		}
	}
	if !sawSummary {
		t.Fatal("the folded summary is not visible at the top")
	}
}

func TestRepeatedHorizontalResizesThenANewCommandDoNotLeakTheFold(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	terminal := session.term
	terminal.screen.SetScrollbackLimit(5000)
	startBlockPrompt(state, shell, session)
	shell.say("cat history | tail -4883\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say(strings.Repeat("history row\r\n", 5200))
	for i := 0; i < 50; i++ {
		state.Wake()
	}
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	for i := 0; i < 10; i++ {
		state.Wake()
	}
	first := &terminal.blocks[0]
	if !state.toggleBlock(session, first) {
		t.Fatalf("initial fold failed: %d lines", blockFoldLines(first))
	}
	widths := []float32{700, 1000, 600, 1200, 800, 500, 1100, 650}
	for round := 0; round < 6; round++ {
		for _, width := range widths {
			state.Resize(gfx.Size{Width: width, Height: 600})
		}
	}

	startBlockPrompt(state, shell, session)
	shell.say("ls\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say("Desktop  Documents  Downloads  home\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	for i := 0; i < 10; i++ {
		state.Wake()
	}
	if len(terminal.blocks) < 2 {
		t.Fatalf("blocks = %d, want at least 2", len(terminal.blocks))
	}
	_, rows := terminal.screen.Size()
	terminal.buildDisplayMap(rows)
	firstVisible, _ := terminal.screen.RetainedFrom()
	summary := terminal.blockFoldSummaryLine(first, firstVisible)
	end := terminal.blockFoldEnd(0)
	if !first.endApprox && !first.stale && end < first.endLine() {
		t.Fatalf("fold end %d is before the recorded end %d", end, first.endLine())
	}
	if end <= summary+1 {
		t.Fatalf("fold range is empty: summary=%d end=%d lines=%d head=%v approx=%v", summary, end, blockFoldLines(first), first.headTrimmed, first.endApprox)
	}
	for _, entry := range terminal.display {
		if entry.index < 0 {
			continue
		}
		if entry.line > summary && entry.line < end {
			t.Fatalf("leaked folded line %d (summary=%d end=%d, foldRows=%d)", entry.line, summary, end, terminal.foldRows)
		}
	}
}

func TestScrolledToTopWhileTypingTheNextCommandDoesNotLeakTheFold(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	terminal := session.term
	terminal.screen.SetScrollbackLimit(5000)
	startBlockPrompt(state, shell, session)
	shell.say("cat history | tail -4883\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say(strings.Repeat("history row\r\n", 5200))
	for i := 0; i < 50; i++ {
		state.Wake()
	}
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	for i := 0; i < 10; i++ {
		state.Wake()
	}
	first := &terminal.blocks[0]
	if !state.toggleBlock(session, first) {
		t.Fatalf("initial fold failed: %d lines", blockFoldLines(first))
	}
	for _, width := range []float32{700, 1000, 600, 1200, 800, 500, 1100, 650} {
		state.Resize(gfx.Size{Width: width, Height: 600})
	}
	_, rows := terminal.screen.Size()
	_, maxOffset := terminal.displayCapacity(rows)
	terminal.scrollDisplay(maxOffset + rows)
	terminal.buildDisplayMap(rows)

	startBlockPrompt(state, shell, session)
	shell.say("ls\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say("Desktop  Documents  Downloads  home\r\n")
	state.Wake()
	for i := 0; i < 20; i++ {
		state.Wake()
	}
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	for i := 0; i < 10; i++ {
		state.Wake()
	}
	_, rows = terminal.screen.Size()
	terminal.buildDisplayMap(rows)
	firstVisible, _ := terminal.screen.RetainedFrom()
	summary := terminal.blockFoldSummaryLine(first, firstVisible)
	end := terminal.blockFoldEnd(0)
	for _, entry := range terminal.display {
		if entry.index >= 0 && entry.line > summary && entry.line < end {
			t.Fatalf("leaked folded line %d (summary=%d end=%d, head=%v approx=%v stale=%v cursor=%+v)", entry.line, summary, end, first.headTrimmed, first.endApprox, first.stale, func() term.Point { p, _ := terminal.screen.CursorPoint(); return p }())
		}
	}
}

func TestAFoldNeverEndsBeforeItsRecordedEnd(t *testing.T) {
	terminal := &terminal{blocks: []commandBlock{
		{prompt: blockAnchor{line: 10, ok: true}, exec: blockAnchor{line: 11, ok: true}, end: blockAnchor{line: 20, ok: true}, status: blockSucceeded, collapsed: true},
		{prompt: blockAnchor{line: 19, ok: true}, exec: blockAnchor{line: 20, ok: true}, end: blockAnchor{line: 21, ok: true}, status: blockSucceeded},
	}}
	if got := terminal.blockFoldEnd(0); got != 20 {
		t.Fatalf("fold end = %d, want the recorded end 20 even when the next prompt is one row early", got)
	}
}

func TestTheFoldEndStopsAtTheNextPromptInsteadOfChasingLaterOutput(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	terminal := session.term
	terminal.screen.SetScrollbackLimit(5000)
	startBlockPrompt(state, shell, session)
	shell.say("echo huge\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say(strings.Repeat("row\r\n", 5200))
	for i := 0; i < 50; i++ {
		state.Wake()
	}
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	for i := 0; i < 10; i++ {
		state.Wake()
	}
	block := &terminal.blocks[0]
	if !state.toggleBlock(session, block) {
		t.Fatalf("initial fold failed: %d lines", blockFoldLines(block))
	}
	startBlockPrompt(state, shell, session)
	_, rows := terminal.screen.Size()
	terminal.buildDisplayMap(rows)
	before := terminal.folds[0].end

	shell.say("first\r\nsecond\r\nthird\r\n")
	for i := 0; i < 10; i++ {
		state.Wake()
	}
	terminal.buildDisplayMap(rows)
	point, ok := terminal.screen.CursorPoint()
	if !ok {
		t.Fatal("no cursor after the prompt moved")
	}
	if point.Line <= before {
		t.Fatalf("cursor did not move: before=%d after=%d", before, point.Line)
	}
	if got := terminal.folds[0].end; got != before {
		t.Fatalf("fold end = %d, want the prompt boundary %d, not later output %d", got, before, point.Line)
	}
}

func TestAPendingPromptEndsTheLastFoldBeforeItIsTyped(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	terminal := session.term
	terminal.screen.SetScrollbackLimit(5000)
	startBlockPrompt(state, shell, session)
	shell.say("echo huge\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say(strings.Repeat("row\r\n", 5200))
	for i := 0; i < 50; i++ {
		state.Wake()
	}
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	for i := 0; i < 10; i++ {
		state.Wake()
	}
	block := &terminal.blocks[0]
	if !state.toggleBlock(session, block) {
		t.Fatalf("initial fold failed: %d lines", blockFoldLines(block))
	}
	startBlockPrompt(state, shell, session)
	if !terminal.blockPendingPrompt {
		t.Fatal("the new prompt was not pending")
	}
	_, rows := terminal.screen.Size()
	terminal.buildDisplayMap(rows)
	want := terminal.blockPrompt.line
	if got := terminal.folds[0].end; got != want {
		t.Fatalf("fold end = %d, want the pending prompt line %d", got, want)
	}
	if _, ok := terminal.displayRowOfLine(want); !ok {
		t.Fatal("the pending prompt is hidden by the fold")
	}
}

func TestAResizeDoesNotDropAFoldBeforeTheNextPendingPromptIsUsed(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	terminal := session.term
	terminal.screen.SetScrollbackLimit(5000)
	startBlockPrompt(state, shell, session)
	shell.say("echo huge\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say(strings.Repeat("row\r\n", 5200))
	for i := 0; i < 50; i++ {
		state.Wake()
	}
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	for i := 0; i < 10; i++ {
		state.Wake()
	}
	// The shell prints its next prompt but the user has not run anything yet.
	startBlockPrompt(state, shell, session)
	block := &terminal.blocks[0]
	if !state.toggleBlock(session, block) {
		t.Fatalf("initial fold failed: %d lines", blockFoldLines(block))
	}
	for round := 0; round < 6; round++ {
		for _, size := range []gfx.Size{{Width: 700, Height: 600}, {Width: 1000, Height: 600}, {Width: 600, Height: 600}, {Width: 1200, Height: 600}} {
			state.Resize(size)
			_, rows := terminal.screen.Size()
			terminal.buildDisplayMap(rows)
			if !block.collapsed {
				t.Fatalf("round %d resize %+v expanded the block", round, size)
			}
			if len(terminal.folds) == 0 {
				t.Fatalf("round %d resize %+v dropped the fold: lines=%d summary=%d end=%d pendingPrompt=%v prompt=%+v endAnchor=%+v approx=%v stale=%v", round, size, blockFoldLines(block), terminal.blockFoldSummaryLine(block, mustBase(t, terminal)), terminal.blockFoldEnd(0), terminal.blockPendingPrompt, terminal.blockPrompt, block.end, block.endApprox, block.stale)
			}
			first, _ := terminal.screen.RetainedFrom()
			summary := terminal.blockFoldSummaryLine(block, first)
			if _, ok := terminal.displayRowOfLine(summary); !ok {
				t.Fatalf("round %d resize %+v lost the summary: lines=%d fold=%+v", round, size, blockFoldLines(block), terminal.folds[0])
			}
		}
	}
	if !terminal.blockOutputAvailable(block) {
		t.Fatal("the folded block lost its output after resizing")
	}
	if !state.setBlockCollapsed(session, block, false) {
		t.Fatal("the folded block could not be expanded after resizing")
	}
	if !state.setBlockCollapsed(session, block, true) {
		t.Fatal("the expanded block could not be folded again after resizing")
	}
}

func mustBase(t *testing.T, terminal *terminal) uint64 {
	t.Helper()
	base, ok := terminal.screen.RetainedFrom()
	if !ok {
		t.Fatal("no retained base")
	}
	return base
}

func TestRepeatedVerticalResizesKeepAFoldOperable(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	terminal := session.term
	terminal.screen.SetScrollbackLimit(5000)
	startBlockPrompt(state, shell, session)
	shell.say("echo huge\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say(strings.Repeat("row\r\n", 5200))
	for i := 0; i < 50; i++ {
		state.Wake()
	}
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	for i := 0; i < 10; i++ {
		state.Wake()
	}
	block := &terminal.blocks[0]
	if !state.toggleBlock(session, block) {
		t.Fatalf("initial fold failed: %d lines", blockFoldLines(block))
	}
	for round := 0; round < 10; round++ {
		for _, height := range []float32{300, 700, 450, 900, 350, 600} {
			state.Resize(gfx.Size{Width: 1000, Height: height})
			_, rows := terminal.screen.Size()
			terminal.buildDisplayMap(rows)
			if len(terminal.folds) == 0 {
				t.Fatalf("round %d height %.0f dropped the fold: lines=%d base=%d end=%d approx=%v stale=%v collapsed=%v", round, height, blockFoldLines(block), mustBase(t, terminal), terminal.blockFoldEnd(0), block.endApprox, block.stale, block.collapsed)
			}
		}
	}
}

func TestTheNextInputLineEndsTheLastFold(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	terminal := session.term
	terminal.screen.SetScrollbackLimit(5000)
	startBlockPrompt(state, shell, session)
	shell.say("echo huge\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say(strings.Repeat("row\r\n", 5200))
	for i := 0; i < 50; i++ {
		state.Wake()
	}
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	for i := 0; i < 10; i++ {
		state.Wake()
	}
	block := &terminal.blocks[0]
	if !state.toggleBlock(session, block) {
		t.Fatalf("initial fold failed: %d lines", blockFoldLines(block))
	}
	startBlockPrompt(state, shell, session)
	if !terminal.blockPendingCommand {
		t.Fatal("the next input line was not pending")
	}
	_, rows := terminal.screen.Size()
	terminal.buildDisplayMap(rows)
	if got := terminal.folds[0].end; got != terminal.blockCommand.line {
		t.Fatalf("fold end = %d, want the next input line %d", got, terminal.blockCommand.line)
	}
}

func TestAFarApproximateEndDoesNotEraseTheFold(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	terminal := session.term
	terminal.screen.SetScrollbackLimit(5000)
	startBlockPrompt(state, shell, session)
	shell.say("echo huge\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say(strings.Repeat("row\r\n", 5200))
	for i := 0; i < 50; i++ {
		state.Wake()
	}
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	for i := 0; i < 10; i++ {
		state.Wake()
	}
	block := &terminal.blocks[0]
	if !state.toggleBlock(session, block) {
		t.Fatalf("initial fold failed: %d lines", blockFoldLines(block))
	}
	// A resize can leave the approximate end clamped far past the real prompt.
	block.end.line = block.endLine() + 6000
	block.endApprox = true
	terminal.markFoldsDirty()
	_, rows := terminal.screen.Size()
	terminal.buildDisplayMap(rows)
	if len(terminal.folds) == 0 {
		t.Fatalf("the fold disappeared: lines=%d end=%d foldEnd=%d", blockFoldLines(block), block.endLine(), terminal.blockFoldEnd(0))
	}
	if !block.collapsed || !terminal.blockOutputAvailable(block) {
		t.Fatalf("the block became inoperable: collapsed=%v output=%v", block.collapsed, terminal.blockOutputAvailable(block))
	}
}

func TestAnApproximateEndUsesTheLivePromptNotAStalePendingLine(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	terminal := session.term
	terminal.screen.SetScrollbackLimit(5000)
	startBlockPrompt(state, shell, session)
	shell.say("echo huge\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say(strings.Repeat("row\r\n", 5200))
	for i := 0; i < 50; i++ {
		state.Wake()
	}
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	for i := 0; i < 10; i++ {
		state.Wake()
	}
	block := &terminal.blocks[0]
	if !state.toggleBlock(session, block) {
		t.Fatalf("initial fold failed: %d lines", blockFoldLines(block))
	}
	point, ok := terminal.screen.CursorPoint()
	if !ok {
		t.Fatal("no cursor")
	}
	block.endApprox = true
	terminal.blockPendingCommand = true
	terminal.blockCommand = blockAnchor{line: point.Line - 100, ok: true}
	_, rows := terminal.screen.Size()
	terminal.buildDisplayMap(rows)
	if got := terminal.folds[0].end; got != point.Line {
		t.Fatalf("fold end = %d, want the live prompt %d instead of stale B %d", got, point.Line, point.Line-100)
	}
}

func TestAPromptRedrawKeepsTheInputAnchorForTheNextCommand(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	startBlockPrompt(state, shell, session)
	shell.say("echo first\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say("first\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	startBlockPrompt(state, shell, session)
	if !session.term.blockPendingCommand {
		t.Fatal("the prompt did not leave a pending input anchor")
	}
	// Resizing redraws the prompt with A but not B.
	sayMarkers(state, shell, session, blockMarker(session, "A"))
	shell.say("echo second\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	if len(session.term.blocks) != 2 {
		t.Fatalf("blocks = %d, want the redrawn prompt to still start a block", len(session.term.blocks))
	}
	if got := session.term.blocks[1].text; got != "echo second" {
		t.Fatalf("new block command = %q, want %q", got, "echo second")
	}
}

func TestAnARedrawAndResizeStillStartTheNextBlock(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	terminal := session.term
	terminal.screen.SetScrollbackLimit(5000)
	startBlockPrompt(state, shell, session)
	shell.say("cat history | tail -4883\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say(strings.Repeat("history row\r\n", 5200))
	for i := 0; i < 50; i++ {
		state.Wake()
	}
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	for i := 0; i < 10; i++ {
		state.Wake()
	}
	first := &terminal.blocks[0]
	if !state.toggleBlock(session, first) {
		t.Fatalf("initial fold failed: %d lines", blockFoldLines(first))
	}
	for _, width := range []float32{700, 1000, 600, 1200} {
		state.Resize(gfx.Size{Width: width, Height: 600})
	}
	// The shell redraws the prompt after the resize but only emits A.
	sayMarkers(state, shell, session, blockMarker(session, "A"))
	shell.say("echo next\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say("next\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	for i := 0; i < 10; i++ {
		state.Wake()
	}
	if len(terminal.blocks) != 2 {
		t.Fatalf("blocks = %d, want the redrawn prompt to start a second block", len(terminal.blocks))
	}
	if !terminal.blocks[0].collapsed {
		t.Fatal("the first block did not stay collapsed")
	}
	_, rows := terminal.screen.Size()
	terminal.buildDisplayMap(rows)
	base := mustBase(t, terminal)
	summary := terminal.blockFoldSummaryLine(first, base)
	if _, ok := terminal.displayRowOfLine(summary); !ok {
		t.Fatal("the first block's summary disappeared")
	}
	for _, entry := range terminal.display {
		if entry.index < 0 || entry.line == summary {
			continue
		}
		start, end, ok := terminal.blockRange(0, first)
		if ok && entry.line >= start && entry.line < end {
			t.Fatalf("the first block leaked line %d", entry.line)
		}
	}
}

func TestSelectingANewBlockAfterExpandingThePreviousOneUsesTheNewRow(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	terminal := session.term
	terminal.screen.SetScrollbackLimit(5000)
	startBlockPrompt(state, shell, session)
	shell.say("cat history | tail -4883\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say(strings.Repeat("history row\r\n", 5200))
	for i := 0; i < 50; i++ {
		state.Wake()
	}
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	for i := 0; i < 10; i++ {
		state.Wake()
	}
	first := &terminal.blocks[0]
	if !state.toggleBlock(session, first) {
		t.Fatalf("initial fold failed: %d lines", blockFoldLines(first))
	}
	for _, width := range []float32{700, 1000, 600, 1200} {
		state.Resize(gfx.Size{Width: width, Height: 600})
	}
	if !state.setBlockCollapsed(session, first, false) {
		t.Fatal("the previous block could not be expanded")
	}
	sayMarkers(state, shell, session, blockMarker(session, "A"))
	shell.say("echo next\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say("next\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	for i := 0; i < 10; i++ {
		state.Wake()
	}
	if len(terminal.blocks) != 2 {
		t.Fatalf("blocks = %d, want 2", len(terminal.blocks))
	}
	second := &terminal.blocks[1]
	// A stale prompt anchor after a redraw must not move the new block into
	// the previous block's physical range.
	_, rows := terminal.screen.Size()
	terminal.scrollToBottomView()
	terminal.buildDisplayMap(rows)
	start := terminal.blockDisplayStartAt(1)
	row := int(start - terminal.screen.LineNumber(0))
	if row < 0 || row >= rows {
		t.Fatalf("new block row = %d outside [0,%d); start=%d top=%d", row, rows, start, terminal.screen.LineNumber(0))
	}
	state.selectBlockAt(session, term.Position{Row: row, Column: 0})
	if terminal.blockSelected != 1 {
		t.Fatalf("selected block = %d, want the new block 1 (clicked row %d, previous visual end %d)", terminal.blockSelected, row, first.visualEndLine())
	}
	prevStart, prevEnd, ok := terminal.blockRange(0, first)
	if ok {
		for line := prevStart; line < prevEnd; line++ {
			if got := terminal.blockAtLine(line); got == 1 {
				t.Fatalf("previous block content line %d is classified as the new block", line)
			}
		}
	}
	geometry := state.terminalWindowLayout(session.ID)
	canvas, _ := paintedTexts(state)
	part := terminalControlParts(session.ID).At(terminalPartBlockCopyOutput)
	hit, ok := hitByID(canvas, part)
	if !ok {
		t.Fatal("the new block toolbar is missing")
	}
	wantTop := geometry.top + float32(row)*state.cellHeight
	if hit.Bounds.Y < wantTop || hit.Bounds.Y >= wantTop+state.cellHeight {
		t.Logf("selected=%d start=%d topLine=%d row=%d display=%v first=%d exec=%d start=%d", terminal.blockSelected, start, terminal.screen.LineNumber(0), row, terminal.display, second.startLine(), second.exec.line, start)
		t.Fatalf("toolbar y = %.1f, want inside the new block row [%.1f, %.1f)", hit.Bounds.Y, wantTop, wantTop+state.cellHeight)
	}
}
