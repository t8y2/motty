package workbench

import (
	"fmt"
	"strings"
	"testing"
)

// longSnippetCommand is the shape the issue reports: a long multi-line command
// with the HTML, quotes and backslashes a README or shell script carries.
func longSnippetCommand() string {
	var command strings.Builder
	for line := 0; command.Len() < 27000; line++ {
		fmt.Fprintf(&command, "<td width=\"50%%\" valign=\"top\">line %d &amp; <strong>tunnels</strong> \\ ' \"</td>\n", line)
	}
	return command.String()
}

// A command of tens of thousands of characters must survive the editor, the
// state file and the next edit: the command field used to cut it at 4096 bytes.
func TestLongSnippetCommandSurvivesSaveAndReload(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	command := longSnippetCommand()
	state.openSnippetDialog(-1)
	state.snippets.name.Insert("long")
	state.snippets.command.Insert(command)
	state.Click(hitSnippetSubmit)
	if state.modal != modalNone {
		t.Fatalf("save failed: %q", state.snippets.dialogError)
	}
	if len(state.snippets.list) != 1 || state.snippets.list[0].Command != command {
		t.Fatalf("saved command has %d bytes, want %d", commandLength(state.snippets.list), len(command))
	}

	loaded := newTestState(t, options)
	if len(loaded.snippets.list) != 1 || loaded.snippets.list[0].Command != command {
		t.Fatalf("restored command has %d bytes, want %d", commandLength(loaded.snippets.list), len(command))
	}

	loaded.openSnippetDialog(0)
	if got := loaded.snippets.command.String(); got != command {
		t.Fatalf("the editor reopened with %d bytes, want %d", len(got), len(command))
	}
}

func commandLength(list []snippet) int {
	if len(list) == 0 {
		return 0
	}
	return len(list[0].Command)
}

// Sending a long snippet types the whole command, not the part the old limit
// kept: both the stored text and the terminal path must agree.
func TestLongSnippetCommandIsSentWhole(t *testing.T) {
	world, options := newSharingWorld(t)
	state := newTestState(t, options)
	command := longSnippetCommand()
	state.openSnippetDialog(-1)
	state.snippets.name.Insert("long send")
	state.snippets.command.Insert(command)
	state.Click(hitSnippetSubmit)
	if state.modal != modalNone {
		t.Fatalf("save failed: %q", state.snippets.dialogError)
	}
	world.connectFirst(t, state)
	shell := world.conns[0].shell(t)
	if !state.sendSnippetTo(0, &state.sessions[0]) {
		t.Fatal("the snippet was not sent")
	}
	want := strings.ReplaceAll(command, "\n", "\r") + "\r"
	if got := shell.typed(); got != want {
		t.Fatalf("the shell received %d bytes, want %d", len(got), len(want))
	}
}

// The issue's upper test case is a shell within 1 MB: the whole command, all
// 900,000 bytes of it, must be stored and read back unchanged.
func TestSnippetCommandNearOneMegabytePersists(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	command := strings.Repeat("echo line\n", 90000)
	state.openSnippetDialog(-1)
	state.snippets.name.Insert("big script")
	state.snippets.command.Insert(command)
	state.Click(hitSnippetSubmit)
	if state.modal != modalNone {
		t.Fatalf("save failed: %q", state.snippets.dialogError)
	}
	if len(state.snippets.list) != 1 || state.snippets.list[0].Command != command {
		t.Fatalf("saved command has %d bytes, want %d", commandLength(state.snippets.list), len(command))
	}
	loaded := newTestState(t, options)
	if len(loaded.snippets.list) != 1 || loaded.snippets.list[0].Command != command {
		t.Fatalf("restored command has %d bytes, want %d", commandLength(loaded.snippets.list), len(command))
	}
}

// A command can be under the 1 MiB text limit and still not fit one JSON
// record: HTML-heavy text expands when the state is encoded. The dialog must
// refuse that too rather than let the store reject it after the dialog closed.
func TestSnippetCommandTooLargeForItsRecordIsRefused(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	command := strings.Repeat("<", 300000)
	if len(command) >= maxSnippetCommand {
		t.Fatalf("the test command is %d bytes, not under the text limit", len(command))
	}
	state.openSnippetDialog(-1)
	state.snippets.name.Insert("too wide")
	state.snippets.command.Insert(command)
	state.Click(hitSnippetSubmit)
	if state.modal != modalSnippet || len(state.snippets.list) != 0 {
		t.Fatalf("the command was saved: modal=%d snippets=%d", state.modal, len(state.snippets.list))
	}
	if state.snippets.dialogError != state.messages.Tools.SnippetErrorTooLong {
		t.Fatalf("dialog error = %q", state.snippets.dialogError)
	}
}

// A command too large for one state record is refused in the dialog, with the
// text still there to shorten: it must never be cut down and saved as if whole.
func TestOversizedSnippetCommandStaysInTheDialog(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	command := strings.Repeat("a", maxSnippetCommand+1)
	state.openSnippetDialog(-1)
	state.snippets.name.Insert("too long")
	state.snippets.command.Insert(command)
	state.Click(hitSnippetSubmit)
	if state.modal != modalSnippet || len(state.snippets.list) != 0 {
		t.Fatalf("the oversized command was saved: modal=%d snippets=%d", state.modal, len(state.snippets.list))
	}
	if state.snippets.dialogError != state.messages.Tools.SnippetErrorTooLong {
		t.Fatalf("dialog error = %q", state.snippets.dialogError)
	}
	if got := state.snippets.command.String(); got != command {
		t.Fatalf("the editor kept %d bytes, want %d", len(got), len(command))
	}
}
