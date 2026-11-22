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
	// submitLast says whether the last line is submitted too; a paste always
	// submits it, a snippet leaves that to its Run setting.
	submitLast bool
	next       int
	due        time.Duration
}

// startPasteQueue writes the first line and queues the rest, and reports
// whether the first write was accepted. Every line but the last is submitted
// with Enter; the last is too when submitLast is set.
func (state *State) startPasteQueue(session *Session, lines []string, submitLast bool) bool {
	if session == nil || session.term == nil || len(lines) == 0 {
		return false
	}
	t := session.term
	state.cancelPasteQueue(t)
	queue := terminalPasteQueue{active: true, token: t.folderToken, lines: lines, submitLast: submitLast}
	t.pasteQueue.writing = true
	accepted := state.typed(session, queue.input(0))
	t.pasteQueue.writing = false
	if !accepted {
		state.status = state.messages.PasteReview.Stopped
		return false
	}
	if len(lines) == 1 {
		return true
	}
	queue.next, queue.due = 1, state.now()+pasteLineGap
	t.pasteQueue = queue
	return true
}

// input is what line index sends: the line, and its Enter unless it is the last
// line of a queue that does not submit it.
func (q *terminalPasteQueue) input(index int) []byte {
	line := q.lines[index]
	if index < len(q.lines)-1 || q.submitLast {
		return []byte(line + "\r")
	}
	return []byte(line)
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
	t.pasteQueue.writing = true
	accepted := state.typed(session, t.pasteQueue.input(t.pasteQueue.next))
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
