package mosh

import (
	"testing"
	"time"
)

// A long session must not keep every keystroke it ever sent once the server has
// acknowledged it, and the indices that point into the action log must follow
// the compaction.
func TestClientCompactsAcknowledgedActions(t *testing.T) {
	actions := make([]UserInstruction, 600)
	for i := range actions {
		actions[i] = UserInstruction{Keys: []byte("k")}
	}
	actions[400].Keys = []byte("marker")
	client := &Client{
		actions:          actions,
		ackedActions:     400,
		pendingInputs:    []pendingInput{{id: 9, actionEnd: 600}},
		sentActionCounts: map[uint64]int{3: 600},
	}
	client.compactActions()

	if len(client.actions) != 200 || client.ackedActions != 0 {
		t.Fatalf("actions=%d acked=%d, want 200 and 0", len(client.actions), client.ackedActions)
	}
	if len(client.pendingInputs) != 1 || client.pendingInputs[0].actionEnd != 200 {
		t.Fatalf("pending input end = %v, want 200", client.pendingInputs)
	}
	if client.sentActionCounts[3] != 200 {
		t.Fatalf("sent state count = %d, want 200", client.sentActionCounts[3])
	}
	if got := string(client.actions[0].Keys); got != "marker" {
		t.Fatalf("first remaining action = %q, want the 401st keystroke", got)
	}
}

// Echo acknowledgement retires input ids in state order, including a state that
// carried several inputs.
func TestClientRetiresInputsByEchoAck(t *testing.T) {
	client := &Client{
		inputStates: []sentInput{{id: 1, state: 1}, {id: 2, state: 2}, {id: 3, state: 2}},
	}
	client.applyEchoAck(1)
	if client.ackedInput != 1 {
		t.Fatalf("acked input = %d, want 1", client.ackedInput)
	}
	client.applyEchoAck(2)
	if client.ackedInput != 3 {
		t.Fatalf("acked input = %d, want 3", client.ackedInput)
	}
}

// A state the server acknowledged must not be sent again just because its echo
// ack is still outstanding. This was the difference between one command running
// and the same keystrokes arriving twice.
func TestClientDoesNotResendTransportAcknowledgedActions(t *testing.T) {
	ocb, err := NewOCB(make([]byte, 16))
	if err != nil {
		t.Fatal(err)
	}
	client := &Client{
		transport:        NewTransport(ocb, false, newByteState()),
		sentActionCounts: map[uint64]int{1: 2},
		actions:          []UserInstruction{{Width: 80, Height: 24}, {Keys: []byte("echo")}},
	}
	client.transport.ackedByRemote = 1
	client.processAcks()
	client.fillPending(time.Now())

	if client.ackedActions != 2 {
		t.Fatalf("acked actions = %d, want 2", client.ackedActions)
	}
	if client.transport.HasPending() {
		t.Fatal("an acknowledged state was prepared for transmission again")
	}
}
