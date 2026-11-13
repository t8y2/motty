package mosh

import (
	"bytes"
	"crypto/rand"
	"testing"
	"time"
)

// pair is a client and a server transport sharing one key and one clock.
type pair struct {
	client *Transport
	server *Transport
	now    time.Time
}

func newPair(t *testing.T) *pair {
	t.Helper()
	key := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	clientOCB, err := NewOCB(key)
	if err != nil {
		t.Fatal(err)
	}
	serverOCB, err := NewOCB(key)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0)
	client := NewTransport(clientOCB, false, newByteState())
	server := NewTransport(serverOCB, true, newByteState())
	for _, transport := range []*Transport{client, server} {
		transport.lastSend = now
		transport.lastRecv = now
		transport.nextAck = now
		transport.nextSend = time.Time{}
	}
	return &pair{client: client, server: server, now: now}
}

// byteState is a host state that only appends: it tests the transport's state
// queue without a terminal emulator. Diffing is the suffix the older state
// does not hold, so a chained state shows exactly the bytes it added.
type byteState struct{ data []byte }

func newByteState() *byteState { return &byteState{} }

func (state *byteState) clone() hostState {
	return &byteState{data: append([]byte(nil), state.data...)}
}

func (state *byteState) apply(diff []byte) (uint64, bool) {
	state.data = append(state.data, diff...)
	return 0, true
}

func (state *byteState) diffFrom(older hostState) []byte {
	if older == nil {
		return append([]byte(nil), state.data...)
	}
	previous := older.(*byteState)
	if len(previous.data) > len(state.data) {
		return nil
	}
	return append([]byte(nil), state.data[len(previous.data):]...)
}

// deliver hands datagrams to a transport and returns the last remote diff.
func deliver(to *Transport, datagrams [][]byte, now time.Time) []byte {
	var diff []byte
	for _, datagram := range datagrams {
		if got := to.Recv(datagram, now); got != nil {
			diff = got
		}
	}
	return diff
}

func TestTransportSendsOneStateAndWaitsForItsAck(t *testing.T) {
	p := newPair(t)
	p.client.SetPending(p.now, []byte("keys"))

	if wait := p.client.NextWake(p.now); wait != sendMinDelay {
		t.Fatalf("NextWake = %v, want %v", wait, sendMinDelay)
	}
	if datagrams := p.client.Tick(p.now); datagrams != nil {
		t.Fatalf("sent before the mindelay: %d datagrams", len(datagrams))
	}
	datagrams := p.client.Tick(p.now.Add(sendMinDelay))
	if len(datagrams) == 0 {
		t.Fatal("no datagram after the mindelay")
	}
	if diff := deliver(p.server, datagrams, p.now.Add(sendMinDelay)); !bytes.Equal(diff, []byte("keys")) {
		t.Fatalf("server received %q, want %q", diff, "keys")
	}
	if p.server.LastRecvNewNum() != 1 {
		t.Fatalf("server state %d, want 1", p.server.LastRecvNewNum())
	}

	// The server acknowledges after the delayed-ack interval.
	p.server.SetPending(p.now.Add(sendMinDelay), []byte("output"))
	out := p.server.Tick(p.now.Add(sendMinDelay + ackDelay))
	if len(out) == 0 {
		t.Fatal("server sent no ack")
	}
	if diff := deliver(p.client, out, p.now.Add(sendMinDelay+ackDelay)); !bytes.Equal(diff, []byte("output")) {
		t.Fatalf("client received %q, want %q", diff, "output")
	}
	if p.client.AckedByRemote() != 1 {
		t.Fatalf("client acked %d, want 1", p.client.AckedByRemote())
	}
	if p.client.HasPending() {
		t.Fatal("client still has a pending diff after the ack")
	}
}

func TestTransportRetransmitsTheSameState(t *testing.T) {
	p := newPair(t)
	p.client.SetPending(p.now, []byte("lost"))
	first := p.client.Tick(p.now.Add(sendMinDelay))
	if len(first) == 0 {
		t.Fatal("no first datagram")
	}
	// Drop it. Nothing before the RTO, the same state after.
	if datagrams := p.client.Tick(p.now.Add(sendMinDelay + minRTO)); len(datagrams) != 0 {
		t.Fatalf("sent %d datagrams before the RTO", len(datagrams))
	}
	now := p.now.Add(sendMinDelay + p.client.RTO())
	retransmitted := p.client.Tick(now)
	if len(retransmitted) == 0 {
		t.Fatal("no retransmission after the RTO")
	}
	if diff := deliver(p.server, retransmitted, now); !bytes.Equal(diff, []byte("lost")) {
		t.Fatalf("server received %q, want %q", diff, "lost")
	}
}

// An instruction that only carries an ack must not register a state the
// remote never received, or a lost keystroke would be acknowledged as
// delivered. Its reference and new state are the newest sent state, which the
// remote lacks, so the state is dropped while the ack is still processed.
func TestTransportAckDoesNotInventALostState(t *testing.T) {
	p := newPair(t)
	p.client.SetPending(p.now, []byte("lost"))
	dropped := p.client.Tick(p.now.Add(sendMinDelay))
	if len(dropped) == 0 {
		t.Fatal("no datagram")
	}

	// The server sends its own state, so the client owes it an ack.
	p.server.SetPending(p.now.Add(sendMinDelay), []byte("screen"))
	out := p.server.Tick(p.now.Add(sendMinDelay + ackDelay))
	deliver(p.client, out, p.now.Add(sendMinDelay+ackDelay))

	now := p.now.Add(sendMinDelay + 2*ackDelay)
	ack := p.client.Tick(now)
	if len(ack) == 0 {
		t.Fatal("client sent no ack")
	}
	deliver(p.server, ack, now)
	if p.server.LastRecvNewNum() != 0 || len(p.server.states) != 1 {
		t.Fatalf("ack invented server state %d (%d states)", p.server.LastRecvNewNum(), len(p.server.states))
	}

	// The retransmission is still accepted afterwards.
	now = now.Add(sendMinDelay + p.client.RTO())
	retransmitted := p.client.Tick(now)
	if diff := deliver(p.server, retransmitted, now); !bytes.Equal(diff, []byte("lost")) {
		t.Fatalf("server received %q, want %q", diff, "lost")
	}
}

func TestTransportShutdownHandshake(t *testing.T) {
	p := newPair(t)
	// One acknowledged state so both ends have heard each other.
	p.client.SetPending(p.now, []byte("x"))
	deliver(p.server, p.client.Tick(p.now.Add(sendMinDelay)), p.now.Add(sendMinDelay))
	p.server.SetPending(p.now.Add(sendMinDelay), []byte("y"))
	deliver(p.client, p.server.Tick(p.now.Add(sendMinDelay+ackDelay)), p.now.Add(sendMinDelay+ackDelay))

	now := p.now.Add(sendMinDelay + ackDelay)
	p.client.Shutdown(now)
	request := p.client.Tick(now)
	if len(request) == 0 {
		t.Fatal("no shutdown request")
	}
	deliver(p.server, request, now)
	if !p.server.CounterpartyShutdown() {
		t.Fatal("server did not see the shutdown request")
	}
	// The server acknowledges it, and the client is done.
	ack := p.server.Tick(now)
	if len(ack) == 0 {
		t.Fatal("server sent no shutdown ack")
	}
	deliver(p.client, ack, now)
	if !p.client.ShutdownAcked() {
		t.Fatal("client's shutdown was not acknowledged")
	}
}

func TestTransportCounterpartyShutdown(t *testing.T) {
	p := newPair(t)
	p.client.SetPending(p.now, []byte("x"))
	deliver(p.server, p.client.Tick(p.now.Add(sendMinDelay)), p.now.Add(sendMinDelay))
	p.server.SetPending(p.now.Add(sendMinDelay), []byte("y"))
	deliver(p.client, p.server.Tick(p.now.Add(sendMinDelay+ackDelay)), p.now.Add(sendMinDelay+ackDelay))

	now := p.now.Add(sendMinDelay + ackDelay)
	p.server.Shutdown(now)
	request := p.server.Tick(now)
	if len(request) == 0 {
		t.Fatal("server sent no shutdown")
	}
	deliver(p.client, request, now)
	if !p.client.CounterpartyShutdown() {
		t.Fatal("client did not see the server's shutdown")
	}
	ack := p.client.Tick(now)
	if len(ack) == 0 {
		t.Fatal("client sent no shutdown ack")
	}
	deliver(p.server, ack, now)
	if !p.server.ShutdownAcked() {
		t.Fatal("server's shutdown was not acknowledged")
	}
}

func TestTransportIdleWakeIsTheHeartbeat(t *testing.T) {
	p := newPair(t)
	if wait := p.client.NextWake(p.now); wait != ackInterval {
		t.Fatalf("idle NextWake = %v, want %v", wait, ackInterval)
	}
}

// craft builds the datagrams of one host state with an explicit reference, so a
// test can reproduce what the C sender's prospective resend does: two states
// with the same old_num, the second carrying the first one's content again.
func craft(sender *Transport, now time.Time, oldNum, newNum uint64, diff string) [][]byte {
	return sender.sendInstruction(now, []byte(diff), oldNum, newNum)
}

func TestTransportAppliesADiffToTheStateItNames(t *testing.T) {
	key := make([]byte, 16)
	ocb, err := NewOCB(key)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0)
	client := NewTransport(ocb, false, newByteState())
	sender := NewTransport(ocb, true, newByteState())

	if got := deliver(client, craft(sender, now, 0, 1, "a"), now); string(got) != "a" {
		t.Fatalf("first state delivered %q, want %q", got, "a")
	}
	// The sender resends from state 0 as a new state, as its prospective resend
	// does when the diff from the oldest unacknowledged state is short. The
	// diff carries "a" again; only the part not already shown may be delivered.
	if got := deliver(client, craft(sender, now, 0, 2, "ab"), now); string(got) != "b" {
		t.Fatalf("overlapping state delivered %q, want %q", got, "b")
	}
	if client.ackNum != 2 {
		t.Fatalf("acknowledged state %d, want 2", client.ackNum)
	}
}

func TestTransportHoldsAnOutOfOrderStateWithoutShowingIt(t *testing.T) {
	key := make([]byte, 16)
	ocb, err := NewOCB(key)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0)
	client := NewTransport(ocb, false, newByteState())
	sender := NewTransport(ocb, true, newByteState())

	if got := deliver(client, craft(sender, now, 0, 1, "a"), now); string(got) != "a" {
		t.Fatalf("state 1 delivered %q", got)
	}
	// State 3 arrives before state 2. Both are based on state 1.
	if got := deliver(client, craft(sender, now, 1, 3, "c"), now); string(got) != "c" {
		t.Fatalf("state 3 delivered %q, want %q", got, "c")
	}
	if got := deliver(client, craft(sender, now, 1, 2, "b"), now); got != nil {
		t.Fatalf("a state older than the screen was delivered: %q", got)
	}
	if client.ackNum != 3 {
		t.Fatalf("acknowledged state %d, want 3: an out-of-order state must not move the ack", client.ackNum)
	}
	// The next state references the filled gap; it updates the screen from the
	// state that is actually shown.
	if got := deliver(client, craft(sender, now, 3, 4, "d"), now); string(got) != "d" {
		t.Fatalf("state 4 delivered %q, want %q", got, "d")
	}
}

func TestTransportPrunesStatesBelowThrowaway(t *testing.T) {
	key := make([]byte, 16)
	ocb, err := NewOCB(key)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0)
	client := NewTransport(ocb, false, newByteState())
	sender := NewTransport(ocb, true, newByteState())
	deliver(client, craft(sender, now, 0, 1, "a"), now)
	deliver(client, craft(sender, now, 1, 2, "b"), now)
	if len(client.states) != 3 {
		t.Fatalf("%d states held, want 3", len(client.states))
	}
	// The sender's next state says it no longer references anything below 2.
	// It still references 2 (its own throwaway floor is 2), so the state is
	// applied and the older states are dropped.
	sender.ackedByRemote = 2
	deliver(client, craft(sender, now, 2, 3, "c"), now)
	if len(client.states) != 2 || client.states[0].num != 2 || client.states[1].num != 3 {
		t.Fatalf("states after throwaway = %+v", client.states)
	}
}
