package workbench

import (
	"time"

	"github.com/vivaui/viva/gfx"
)

// Line-by-line paste: each logical line is written with its own Enter and the
// next line waits pasteLineGap. The queue is a deadline on the terminal, served
// by the frame the window asks for; there is no timer, goroutine or loop while
// nothing is queued.

const pasteLineGap = 250 * time.Millisecond

type terminalPasteQueue struct {
	active  bool
	writing bool // the queue itself is writing; user input cancellation must not stop it
	token   string
	lines   []string
	next    int
	due     time.Duration
}

func (state *State) startPasteQueue(session *Session, lines []string) {
	if session == nil || session.term == nil || len(lines) == 0 {
		return
	}
	t := session.term
	state.cancelPasteQueue(t)
	t.pasteQueue.writing = true
	accepted := state.typed(session, []byte(lines[0]+"\r"))
	t.pasteQueue.writing = false
	if !accepted {
		state.status = state.messages.PasteReview.Stopped
		return
	}
	if len(lines) == 1 {
		return
	}
	t.pasteQueue = terminalPasteQueue{
		active: true, token: t.folderToken, lines: lines, next: 1, due: state.now() + pasteLineGap,
	}
}

// advancePasteQueue writes the next line once its deadline passed. It runs from
// Wake, next to the other deadline work, and stops the queue at the first
// refusal so a line cannot be sent twice.
func (state *State) advancePasteQueue(session *Session, now time.Duration) {
	t := session.term
	if t == nil || !t.pasteQueue.active || now < t.pasteQueue.due {
		return
	}
	if t.shell == nil || t.ended || t.folderToken != t.pasteQueue.token || t.pasteQueue.next >= len(t.pasteQueue.lines) {
		state.cancelPasteQueue(t)
		return
	}
	line := t.pasteQueue.lines[t.pasteQueue.next]
	t.pasteQueue.writing = true
	accepted := state.typed(session, []byte(line+"\r"))
	t.pasteQueue.writing = false
	if !accepted {
		state.cancelPasteQueue(t)
		state.status = state.messages.PasteReview.Stopped
		return
	}
	t.pasteQueue.next++
	if t.pasteQueue.next >= len(t.pasteQueue.lines) {
		state.cancelPasteQueue(t)
		return
	}
	t.pasteQueue.due = now + pasteLineGap
}

func (state *State) cancelPasteQueue(t *terminal) {
	if t != nil {
		t.pasteQueue = terminalPasteQueue{}
	}
}

func (state *State) cancelAllPasteQueues() {
	for index := range state.sessions {
		state.cancelPasteQueue(state.sessions[index].term)
	}
}

// requestPasteFrames asks for one frame per waiting queue. An empty queue asks
// for nothing, so an idle window still schedules no frames.
func (state *State) requestPasteFrames(canvas *gfx.Canvas) {
	now := state.now()
	for index := range state.sessions {
		t := state.sessions[index].term
		if t == nil || !t.pasteQueue.active {
			continue
		}
		wait := t.pasteQueue.due - now
		if wait < time.Millisecond {
			wait = time.Millisecond
		}
		canvas.RequestFrameIn(wait)
	}
}
