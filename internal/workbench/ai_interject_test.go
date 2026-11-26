package workbench

import (
	"sync"
	"testing"

	"motty/internal/ai/agent"
	"motty/internal/ai/llm"
	"motty/internal/ai/tools"
)

// requestCount reads how many model requests the scripted model has received.
func requestCount(model *scriptedModel) int {
	model.mu.Lock()
	defer model.mu.Unlock()
	return len(model.requests)
}

// requestAt returns the messages of the index-th request the model received.
func requestAt(model *scriptedModel, index int) []llm.Message {
	model.mu.Lock()
	defer model.mu.Unlock()
	return model.requests[index].Messages
}

// itemIndex finds the first item of kind carrying text, or -1.
func itemIndex(state *State, kind aiItemKind, text string) int {
	for index, item := range state.ai.items {
		if item.kind == kind && item.text == text {
			return index
		}
	}
	return -1
}

// A message sent while a tool runs reaches the model at the next step, after
// the tool's result, and the run carries on without being cut off.
func TestAIInterjectSteersIntoRunAtNextStep(t *testing.T) {
	var once sync.Once
	release := make(chan struct{})
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	model := &scriptedModel{turn: func(index int, _ llm.Request) []llm.Event {
		if index == 0 {
			<-release
			return toolTurn(tools.NameListSessions, `{}`)
		}
		return textTurn("checked")
	}}
	state := targetTestWorld(t, model)
	ask(state, "look around", agent.ModeAgent)
	pumpUntil(t, state, "the first request", func() bool { return requestCount(model) == 1 })

	state.ai.input.Insert("also check the disk")
	if !state.aiSend() {
		t.Fatal("a message during a run was refused")
	}
	steer := itemIndex(state, aiUser, "also check the disk")
	if steer < 0 || state.ai.items[steer].pending != aiSteerPending {
		t.Fatalf("the interjection is not waiting as a steer: %+v", state.ai.items)
	}
	if state.ai.input.String() != "" {
		t.Fatal("the message box kept the interjection")
	}

	once.Do(func() { close(release) })
	pumpUntil(t, state, "the run to finish", func() bool { return state.ai.run == nil })
	if n := requestCount(model); n != 2 {
		t.Fatalf("the run made %d requests, want 2 (no separate request for the interjection)", n)
	}
	messages := requestAt(model, 1)
	if last := messages[len(messages)-1]; last.Role != llm.RoleUser || last.Text != "also check the disk" {
		t.Fatalf("the next request does not end with the interjection: %+v", messages)
	}
	if state.ai.items[steer].pending != aiSent {
		t.Fatal("a delivered interjection still shows as waiting")
	}
	if answer := itemIndex(state, aiAssistant, "checked"); answer < steer {
		t.Fatalf("the answer landed above the interjection: %+v", state.ai.items)
	}
}

// Ask mode cannot take a steer, so the message waits and is sent as the next
// question once the answer has finished.
func TestAIInterjectQueuesInAskModeAndSendsAfterTheRun(t *testing.T) {
	var once sync.Once
	release := make(chan struct{})
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	model := &scriptedModel{turn: func(index int, _ llm.Request) []llm.Event {
		if index == 0 {
			<-release
			return textTurn("first answer")
		}
		return textTurn("second answer")
	}}
	state := targetTestWorld(t, model)
	ask(state, "first question", agent.ModeAsk)
	pumpUntil(t, state, "the first request", func() bool { return requestCount(model) == 1 })

	state.ai.input.Insert("queued follow-up")
	if !state.aiSend() {
		t.Fatal("a message during a run was refused")
	}
	queued := itemIndex(state, aiUser, "queued follow-up")
	if queued < 0 || state.ai.items[queued].pending != aiQueuePending {
		t.Fatalf("the message is not queued: %+v", state.ai.items)
	}
	if requestCount(model) != 1 {
		t.Fatal("a queued message started a request while the run was still going")
	}

	once.Do(func() { close(release) })
	pumpUntil(t, state, "the queued question to be sent", func() bool {
		return requestCount(model) == 2 && state.ai.run == nil
	})
	messages := requestAt(model, 1)
	if last := messages[len(messages)-1]; last.Role != llm.RoleUser || last.Text != "queued follow-up" {
		t.Fatalf("the queued question was not sent next: %+v", messages)
	}
	for _, item := range state.ai.items {
		if item.pending != aiSent {
			t.Fatalf("a message still waits after the run: %+v", item)
		}
	}
	if itemIndex(state, aiAssistant, "second answer") < 0 {
		t.Fatalf("the queued question got no answer: %+v", state.ai.items)
	}
}

// Stopping a run before the agent read a steer gives the text back to the
// message box; nothing the user wrote is lost and nothing is sent unasked.
func TestAIInterjectStopReturnsTheTextToTheBox(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	model := &scriptedModel{turn: func(index int, _ llm.Request) []llm.Event {
		<-release
		return textTurn("never")
	}}
	state := targetTestWorld(t, model)
	state.ai.stopGrace = 5_000_000 // 5ms
	ask(state, "start", agent.ModeAgent)
	pumpUntil(t, state, "the first request", func() bool { return requestCount(model) == 1 })

	state.ai.input.Insert("keep this")
	state.aiSend()
	state.aiStop()
	pumpUntil(t, state, "the stopped run to finish", func() bool { return state.ai.run == nil })

	if got := state.ai.input.String(); got != "keep this" {
		t.Fatalf("the box holds %q, want the interjection back", got)
	}
	if itemIndex(state, aiUser, "keep this") >= 0 {
		t.Fatal("a stopped interjection still shows in the conversation")
	}
	if n := requestCount(model); n != 1 {
		t.Fatalf("a stop sent %d requests, want 1", n)
	}
}
