package workbench

import (
	"strings"
	"sync"

	"github.com/vivaui/viva/text"
)

// aiPendingKind says whether a message sent while a request runs has reached
// the model. A steer waits for the agent's next safe point; a queued message
// waits for the run to finish and is then sent as the next question.
type aiPendingKind uint8

const (
	aiSent aiPendingKind = iota
	aiSteerPending
	aiQueuePending
)

// aiSteers carries interjections from the window thread to the run's
// goroutine. The window adds, the agent takes at its safe points; nothing else
// is shared between them.
type aiSteers struct {
	mu    sync.Mutex
	texts []string
}

func (steers *aiSteers) add(text string) {
	steers.mu.Lock()
	steers.texts = append(steers.texts, text)
	steers.mu.Unlock()
}

func (steers *aiSteers) take() []string {
	steers.mu.Lock()
	defer steers.mu.Unlock()
	out := steers.texts
	steers.texts = nil
	return out
}

// aiInterject handles the message box while a request runs. Enter does not
// cut the request off. A plain question in Agent mode is steered into the run:
// the agent reads it at its next step. Anything that would change the run's
// context (files, @references, Ask mode's terminal text) or a backend that
// cannot take input mid-run waits, and is sent once the run has finished.
func (state *State) aiInterject() bool {
	ai := &state.ai
	run := ai.run
	question := strings.TrimSpace(ai.input.String())
	if question == "" {
		return false
	}
	kind := aiQueuePending
	if state.aiCanSteer(run, question) {
		kind = aiSteerPending
	}
	ai.input = text.Editor{}
	ai.notice = ""
	ai.items = append(ai.items, aiItem{
		kind: aiUser, text: question, pending: kind,
		targetKey: run.targetKey, targetLabel: state.aiConversationTargetLabel(run.target, run.extras...),
	})
	// The streamed answer continues in a new bubble below this message.
	run.open = -1
	if kind == aiSteerPending {
		run.steers.add(question)
	}
	state.aiMessagesToEnd()
	return true
}

// aiCanSteer reports whether question can join run as it stands. The run's
// context is fixed when the request starts, and a steer carries only the words,
// so a question that would add context waits for the next request.
func (state *State) aiCanSteer(run *aiRun, question string) bool {
	ai := &state.ai
	if !run.steerable || ai.attaching > 0 || len(ai.attachments) > 0 {
		return false
	}
	if len(state.aiRequestReferences(question)) != len(run.references) {
		return false
	}
	return len(state.aiNoteReferences(question)) == 0
}

// aiDeliverSteer marks the oldest steer the agent has just read as delivered:
// it is now part of the conversation the model answers from.
func (state *State) aiDeliverSteer(run *aiRun) {
	run.open = -1
	for index := range state.ai.items {
		if state.ai.items[index].pending == aiSteerPending {
			state.ai.items[index].pending = aiSent
			return
		}
	}
}

// aiTakePending removes the messages that never reached the model and returns
// their text in the order they were sent. They leave the conversation until
// they are sent again.
func (state *State) aiTakePending() []string {
	var texts []string
	kept := state.ai.items[:0]
	for _, item := range state.ai.items {
		if item.pending == aiSent {
			kept = append(kept, item)
			continue
		}
		texts = append(texts, item.text)
	}
	clear(state.ai.items[len(kept):])
	state.ai.items = kept
	return texts
}

// aiSendQueued sends text that waited for a run as the next question. The
// message box may hold a draft typed since, so it is set aside for the send and
// restored afterwards: a draft never goes out because a run ended.
func (state *State) aiSendQueued(question string) {
	ai := &state.ai
	draft := ai.input
	ai.input = text.Editor{}
	ai.input.Insert(question)
	state.aiSend()
	ai.input = draft
	if ai.run == nil {
		// Not sent (the model is missing, or a file is still being read): the
		// question goes back in front of the draft, not over it.
		state.aiSetDraft(question)
	}
}

// aiSetDraft puts question back into the message box, in front of what it
// already holds.
func (state *State) aiSetDraft(question string) {
	ai := &state.ai
	full := question
	if rest := ai.input.String(); rest != "" {
		full += "\n" + rest
	}
	ai.input = text.Editor{}
	ai.input.Insert(full)
}

// aiFinishPending settles the messages that were still waiting when a request
// ended. A run that completed sends them as the next question; a stopped,
// failed or escalated run gives them back to the message box so nothing is
// lost.
func (state *State) aiFinishPending(texts []string, completed bool) {
	if len(texts) == 0 {
		return
	}
	question := strings.Join(texts, "\n")
	if completed {
		state.aiSendQueued(question)
		return
	}
	state.aiSetDraft(question)
}
