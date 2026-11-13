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
	client := NewTransport(clientOCB, false)
	server := NewTransport(serverOCB, true)
	for _, transport := range []*Transport{client, server} {
		transport.lastSend = now
		transport.lastRecv = now
		transport.nextAck = now
		transport.nextSend = time.Time{}
	}
	return &pair{client: client, server: server, now: now}
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
	if p.server.LastRecvNewNum() != 0 || len(p.server.receivedNums) != 1 {
		t.Fatalf("ack invented server state %d (%d states)", p.server.LastRecvNewNum(), len(p.server.receivedNums))
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
