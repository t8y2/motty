package workbench

import (
	"testing"
	"time"
)

// A snippet marked to go line by line sends one line per gap, so a long script
// is not handed to the shell as one paste; Run decides only the last line.
func TestSnippetLinesAreSentOneAtATime(t *testing.T) {
	for _, test := range []struct {
		name string
		run  bool
		want string
	}{
		{"run", true, "one\rtwo\rthree\r"},
		{"no run", false, "one\rtwo\rthree"},
	} {
		t.Run(test.name, func(t *testing.T) {
			state, _, _, shell := liveSession(t)
			now := time.Duration(0)
			state.options.Now = func() time.Duration { return now }
			state.snippets.list = []snippet{{Name: "script", Command: "one\ntwo\nthree", Run: test.run, Lines: true}}
			if !state.sendSnippet(0) {
				t.Fatal("snippet was not sent")
			}
			if got := shell.typed(); got != "one\r" {
				t.Fatalf("first line sent %q", got)
			}
			now += pasteLineGap
			state.Wake()
			now += pasteLineGap
			state.Wake()
			if got := shell.typed(); got != test.want {
				t.Fatalf("sent %q, want %q", got, test.want)
			}
			if state.sessions[0].term.pasteQueue.active {
				t.Fatal("the queue stayed active after its last line")
			}
		})
	}
}

// Without the option, and for a one-line command, nothing is queued.
func TestSnippetWithoutLinesStaysOnePaste(t *testing.T) {
	state, _, _, shell := liveSession(t)
	state.snippets.list = []snippet{
		{Name: "a", Command: "one\ntwo", Run: true},
		{Name: "b", Command: "single", Run: true, Lines: true},
	}
	state.sendSnippet(0)
	state.sendSnippet(1)
	if got := shell.typed(); got != "one\rtwo\rsingle\r" {
		t.Fatalf("sent %q", got)
	}
	if state.sessions[0].term.pasteQueue.active {
		t.Fatal("a snippet that sends as one write queued lines")
	}
}

// The choice is saved with the snippet, through the inline editor that sets it.
func TestSnippetLinesChoiceSurvivesRestart(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	state.startInlineSnippetDraft("")
	state.snippets.inline.name.Insert("script")
	state.snippets.inline.command.Insert("one\ntwo")
	state.Click(hitSnippetInlineLines)
	state.applyInlineSnippetEdits()
	state.flushInlineSnippetSave()
	if len(state.snippets.list) != 1 || !state.snippets.list[0].Lines {
		t.Fatalf("saved snippets = %+v", state.snippets.list)
	}
	if restarted := newTestState(t, options); len(restarted.snippets.list) != 1 || !restarted.snippets.list[0].Lines {
		t.Fatalf("restored snippets = %+v", restarted.snippets.list)
	}
}
