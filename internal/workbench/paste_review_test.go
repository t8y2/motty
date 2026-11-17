package workbench

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

func pasteForReview(t *testing.T, state *State, value string) {
	t.Helper()
	state.options.ClipboardText = func() (string, error) { return value, nil }
	state.pasteIntoTerminal(&state.sessions[0])
}

func TestMultiLinePasteOpensReviewWithoutWriting(t *testing.T) {
	state, _, _, shell := liveSession(t)
	pasteForReview(t, state, "one\ntwo")
	if state.modal != modalPasteReview {
		t.Fatalf("multi-line paste opened modal %v", state.modal)
	}
	if got := shell.typed(); got != "" {
		t.Fatalf("text reached the shell before review: %q", got)
	}
	if state.modalSession != state.sessions[0].ID {
		t.Fatalf("review belongs to session %d, want %d", state.modalSession, state.sessions[0].ID)
	}
	if state.EditorFor(hitPasteReviewEditor) == nil {
		t.Fatal("the preview is not editable")
	}
}

func TestSingleLinePasteAndDisabledReviewKeepTheOldPath(t *testing.T) {
	t.Run("single-line", func(t *testing.T) {
		state, _, _, shell := liveSession(t)
		pasteForReview(t, state, "one")
		if state.modal != modalNone || shell.typed() != "one" {
			t.Fatalf("single-line paste modal=%v input=%q", state.modal, shell.typed())
		}
	})
	t.Run("setting-off", func(t *testing.T) {
		state, _, _, shell := liveSession(t)
		state.pasteConfirmOff = true
		pasteForReview(t, state, "one\ntwo")
		if state.modal != modalNone || shell.typed() != "one\rtwo" {
			t.Fatalf("disabled review modal=%v input=%q", state.modal, shell.typed())
		}
	})
}

func TestPasteReviewDetectsEveryLineEnding(t *testing.T) {
	for _, test := range []struct {
		name  string
		value string
	}{
		{"lf", "one\ntwo"},
		{"crlf", "one\r\ntwo"},
		{"cr", "one\rtwo"},
		{"trailing", "one\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			state, _, _, shell := liveSession(t)
			pasteForReview(t, state, test.value)
			if state.modal != modalPasteReview || shell.typed() != "" {
				t.Fatalf("paste %q opened modal %v with input %q", test.value, state.modal, shell.typed())
			}
		})
	}
}

func TestPasteReviewWholeSendKeepsPasteBytes(t *testing.T) {
	for _, bracketed := range []bool{false, true} {
		name := "plain"
		if bracketed {
			name = "bracketed"
		}
		t.Run(name, func(t *testing.T) {
			state, _, _, shell := liveSession(t)
			if bracketed {
				shell.say("\x1b[?2004h")
				state.Wake()
			}
			pasteForReview(t, state, "one\ntwo")
			state.Click(hitPasteReviewWhole)
			want := "one\rtwo"
			if bracketed {
				want = "\x1b[200~one\rtwo\x1b[201~"
			}
			if got := shell.typed(); got != want {
				t.Fatalf("whole paste sent %q, want %q", got, want)
			}
			if state.modal != modalNone {
				t.Fatalf("review stayed open: %v", state.modal)
			}
		})
	}
}

func TestPasteReviewSendsTheEditedPreview(t *testing.T) {
	state, _, _, shell := liveSession(t)
	pasteForReview(t, state, "one\ntwo")
	editor := state.EditorFor(hitPasteReviewEditor)
	if editor == nil {
		t.Fatal("the preview is not editable")
	}
	*editor = *text.NewEditor("replacement\nline")
	state.Click(hitPasteReviewWhole)
	if got := shell.typed(); got != "replacement\rline" {
		t.Fatalf("edited preview sent %q", got)
	}
}

func TestPasteReviewLineByLinePacesEachLine(t *testing.T) {
	state, _, _, shell := liveSession(t)
	now := time.Duration(0)
	state.options.Now = func() time.Duration { return now }
	pasteForReview(t, state, "one\ntwo\nthree")
	state.Click(hitPasteReviewLines)
	if got := shell.typed(); got != "one\r" {
		t.Fatalf("first line sent %q", got)
	}
	if state.modal != modalNone {
		t.Fatal("the review stayed open")
	}
	now += pasteLineGap - time.Millisecond
	state.Wake()
	if got := shell.typed(); got != "one\r" {
		t.Fatalf("a line was sent before the gap: %q", got)
	}
	now += time.Millisecond
	state.Wake()
	if got := shell.typed(); got != "one\rtwo\r" {
		t.Fatalf("second line sent %q", got)
	}
	now += pasteLineGap
	state.Wake()
	if got := shell.typed(); got != "one\rtwo\rthree\r" {
		t.Fatalf("third line sent %q", got)
	}
	if state.sessions[0].term.pasteQueue.active {
		t.Fatal("the queue stayed active after its last line")
	}
}

func TestPasteReviewLinesDropTheTrailingNewline(t *testing.T) {
	for _, test := range []struct {
		text string
		want string
	}{
		{"one\n", "one\r"},
		{"one\n\n", "one\r\r"},
		{"\n", "\r"},
		{"one\ntwo", "one\rtwo\r"},
	} {
		state, _, _, shell := liveSession(t)
		now := time.Duration(0)
		state.options.Now = func() time.Duration { return now }
		pasteForReview(t, state, test.text)
		state.Click(hitPasteReviewLines)
		now += pasteLineGap
		state.Wake()
		if got := shell.typed(); got != test.want {
			t.Fatalf("%q sent %q, want %q", test.text, got, test.want)
		}
	}
}

func TestPasteReviewInputCancelsTheRemainingLines(t *testing.T) {
	state, _, _, shell := liveSession(t)
	now := time.Duration(0)
	state.options.Now = func() time.Duration { return now }
	pasteForReview(t, state, "one\ntwo\nthree")
	state.Click(hitPasteReviewLines)
	state.terminalText(state.sessions[0].ID, app.TextEvent{Kind: app.TextInsert, Text: "x"})
	if got := shell.typed(); got != "one\rx" {
		t.Fatalf("typing after a line paste sent %q", got)
	}
	now += pasteLineGap
	state.Wake()
	if got := shell.typed(); got != "one\rx" {
		t.Fatalf("a canceled queue sent %q", got)
	}
	if state.sessions[0].term.pasteQueue.active {
		t.Fatal("the queue survived user input")
	}
}

func TestPasteReviewQueueDiesWithTheShell(t *testing.T) {
	state, _, _, shell := liveSession(t)
	now := time.Duration(0)
	state.options.Now = func() time.Duration { return now }
	pasteForReview(t, state, "one\ntwo")
	state.Click(hitPasteReviewLines)
	shell.end(io.EOF)
	state.Wake()
	now += pasteLineGap
	state.Wake()
	if got := shell.typed(); got != "one\r" {
		t.Fatalf("a queue outlived its shell: %q", got)
	}
	if state.sessions[0].term.pasteQueue.active {
		t.Fatal("the queue survived the shell ending")
	}
}

func TestPasteReviewRefusedLineStopsTheQueue(t *testing.T) {
	state, _, _, shell := liveSession(t)
	now := time.Duration(0)
	state.options.Now = func() time.Duration { return now }
	pasteForReview(t, state, "one\ntwo")
	state.Click(hitPasteReviewLines)
	shell.refuse = true
	now += pasteLineGap
	state.Wake()
	if got := shell.typed(); got != "one\r" {
		t.Fatalf("a refused line was written: %q", got)
	}
	if state.sessions[0].term.pasteQueue.active {
		t.Fatal("the queue kept running after a refusal")
	}
	if state.status != state.messages.PasteReview.Stopped {
		t.Fatalf("refusal status = %q", state.status)
	}
}

func TestPasteReviewStaleSessionWritesNothing(t *testing.T) {
	state, _, _, shell := liveSession(t)
	pasteForReview(t, state, "one\ntwo")
	state.sessions[0].term.folderToken = "replaced"
	state.Click(hitPasteReviewWhole)
	if state.modal != modalNone {
		t.Fatalf("stale review stayed open: %v", state.modal)
	}
	if got := shell.typed(); got != "" {
		t.Fatalf("stale review sent %q", got)
	}
	if state.status != state.messages.PasteReview.Stale {
		t.Fatalf("stale status = %q", state.status)
	}
}

func TestPasteReviewEmptyPreviewCannotSend(t *testing.T) {
	state, _, _, shell := liveSession(t)
	pasteForReview(t, state, "one\ntwo")
	state.pasteReview.editor = *text.NewEditor("")
	if state.pasteReviewCanSend() {
		t.Fatal("an empty preview can be sent")
	}
	state.Click(hitPasteReviewWhole)
	state.Click(hitPasteReviewLines)
	if state.modal != modalPasteReview || shell.typed() != "" {
		t.Fatalf("empty preview modal=%v input=%q", state.modal, shell.typed())
	}
}

func TestPasteReviewReadOnlyForHugeText(t *testing.T) {
	state, _, _, _ := liveSession(t)
	value := strings.Repeat("a", pasteReviewEditableBytes) + "\nend"
	pasteForReview(t, state, value)
	if !state.pasteReview.readOnly {
		t.Fatal("a text over the edit limit stayed editable")
	}
	if state.EditorFor(hitPasteReviewEditor) != nil {
		t.Fatal("the read-only preview exposes an editor")
	}
	if !state.pasteReviewCanSend() || state.pasteReviewText() != value {
		t.Fatalf("read-only preview sends %d bytes, want %d", len(state.pasteReviewText()), len(value))
	}
}

func TestPasteReviewSummaryAndSplitting(t *testing.T) {
	for _, test := range []struct {
		text  string
		lines int
		split []string
	}{
		{"one\ntwo", 2, []string{"one", "two"}},
		{"one\n", 1, []string{"one"}},
		{"one\n\n", 2, []string{"one", ""}},
		{"\n", 1, []string{""}},
		{"one\ntwo\n", 2, []string{"one", "two"}},
		{"", 0, nil},
	} {
		if got := pasteLineCount(test.text); got != test.lines {
			t.Fatalf("%q counts %d lines, want %d", test.text, got, test.lines)
		}
		got := splitPasteLines(test.text)
		if len(got) != len(test.split) {
			t.Fatalf("%q splits into %q, want %q", test.text, got, test.split)
		}
		for i := range got {
			if got[i] != test.split[i] {
				t.Fatalf("%q splits into %q, want %q", test.text, got, test.split)
			}
		}
	}
}

func TestPasteReviewFramesFollowTheQueue(t *testing.T) {
	state, _, _, _ := liveSession(t)
	now := time.Duration(0)
	state.options.Now = func() time.Duration { return now }
	pasteForReview(t, state, "one\ntwo")
	state.Click(hitPasteReviewLines)
	var canvas gfx.Canvas
	state.requestPasteFrames(&canvas)
	if wait, scheduled := canvas.NextFrame(); !scheduled || wait != pasteLineGap {
		t.Fatalf("active queue scheduled frame %v/%t, want %v", wait, scheduled, pasteLineGap)
	}
	now += pasteLineGap
	state.Wake()
	var finished gfx.Canvas
	state.requestPasteFrames(&finished)
	if _, scheduled := finished.NextFrame(); scheduled {
		t.Fatal("a finished queue still asked for a frame")
	}
}

func TestPasteReviewTextEventTakesTheSameGate(t *testing.T) {
	state, _, _, shell := liveSession(t)
	state.terminalText(state.sessions[0].ID, app.TextEvent{Kind: app.TextPaste, Text: "one\ntwo"})
	if state.modal != modalPasteReview || shell.typed() != "" {
		t.Fatalf("text event paste modal=%v input=%q", state.modal, shell.typed())
	}
}

func TestPasteConfirmSettingSurvivesRestart(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	if state.pasteConfirmOff {
		t.Fatal("a new profile must confirm multi-line pastes")
	}
	state.pasteConfirmOff = true
	state.save()
	again := newTestState(t, options)
	if !again.pasteConfirmOff {
		t.Fatal("the disabled confirmation did not survive a restart")
	}
}
