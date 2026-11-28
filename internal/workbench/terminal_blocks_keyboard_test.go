package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

func TestBlockToolbarIsReachableAndActivatesFromTheKeyboard(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	startBlockPrompt(state, shell, session)
	shell.say("echo hi\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say("hi\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	selectBlockRow(state, session, 0)

	copied := ""
	state.options.SetClipboardText = func(text string) error { copied = text; return nil }
	driver, err := app.NewDriver(state.WindowConfig(app.Config{
		Title: "block keyboard", Width: 1400, Height: 900, Tree: Tree(),
		Paint: state.Paint, OnKey: state.Key, OnPointer: state.Pointer,
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(driver.Close)

	parts := terminalControlParts(session.ID)
	commandPart := parts.At(terminalPartBlockCopyCommand)
	outputPart := parts.At(terminalPartBlockCopyOutput)
	allPart := parts.At(terminalPartBlockCopyAll)
	foldPart := parts.At(terminalPartBlockToggleFold)

	tab := func() gfx.ID {
		t.Helper()
		if err := driver.Key(gfx.KeyTab, 0); err != nil {
			t.Fatal(err)
		}
		return driver.Snapshot().Focused
	}
	if got := tab(); got != commandPart {
		t.Fatalf("first Tab focused %v, want copy-command %v", got, commandPart)
	}
	if got := tab(); got != outputPart {
		t.Fatalf("second Tab focused %v, want copy-output %v", got, outputPart)
	}
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if copied != "hi" {
		t.Fatalf("Enter on copy-output copied %q, want %q", copied, "hi")
	}

	if got := tab(); got != allPart {
		t.Fatalf("third Tab focused %v, want copy-block %v", got, allPart)
	}
	if err := driver.Key(gfx.KeySpace, 0); err != nil {
		t.Fatal(err)
	}
	if copied != "echo hi\nhi" {
		t.Fatalf("Space on copy-block copied %q, want command and output", copied)
	}

	if got := tab(); got != foldPart {
		t.Fatalf("fourth Tab focused %v, want fold %v", got, foldPart)
	}
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if !session.term.blocks[0].collapsed {
		t.Fatal("Enter on fold did not collapse the block")
	}

	if err := driver.Key(gfx.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	snapshot := driver.Snapshot()
	if snapshot.Focused != session.term.controlID {
		t.Fatalf("Escape focused %v, want the terminal control %v", snapshot.Focused, session.term.controlID)
	}
	if session.term.blockSelected != -1 {
		t.Fatalf("Escape left block %d selected", session.term.blockSelected)
	}
}

func TestBlockToolbarShiftTabEntersFromTheLastButton(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	startBlockPrompt(state, shell, session)
	shell.say("echo hi\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "C"))
	shell.say("hi\r\n")
	state.Wake()
	sayMarkers(state, shell, session, blockMarker(session, "D;0"))
	selectBlockRow(state, session, 0)

	driver, err := app.NewDriver(state.WindowConfig(app.Config{
		Title: "block keyboard reverse", Width: 1400, Height: 900, Tree: Tree(),
		Paint: state.Paint, OnKey: state.Key, OnPointer: state.Pointer,
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(driver.Close)

	if err := driver.Key(gfx.KeyTab, gfx.ModifierShift); err != nil {
		t.Fatal(err)
	}
	if got, want := driver.Snapshot().Focused, terminalControlParts(session.ID).At(terminalPartBlockToggleFold); got != want {
		t.Fatalf("Shift+Tab focused %v, want fold %v", got, want)
	}
}
