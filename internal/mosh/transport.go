package mosh

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"io"
	"time"
)

// SSP transport timing. The values are the C implementation's
// (src/network/transportsender.h and src/network/network.h): frames go out at
// most every sendMinDelay, retransmission starts at the RTO, and an empty ack
// is sent every ackInterval so both ends know the path is still there.
const (
	sendMinDelay    = 8 * time.Millisecond
	sendIntervalMin = 20 * time.Millisecond
	sendIntervalMax = 250 * time.Millisecond
	ackDelay        = 100 * time.Millisecond
	ackInterval     = 3 * time.Second
	minRTO          = 50 * time.Millisecond
	maxRTO          = 1000 * time.Millisecond
	maxRTOInitial   = 1000 * time.Millisecond
	// shutdownTimeout is how long a local close waits for the remote to
	// acknowledge the shutdown state. It runs off the UI thread; five seconds
	// covers a slow link while still ending the goroutine.
	shutdownTimeout = 5 * time.Second
)

// shutdownNum is the state number both ends use to start the shutdown
// handshake (the C implementation's uint64_t(-1)).
const shutdownNum = ^uint64(0)

// Transport implements the mosh State Synchronization Protocol sender and
// receiver. It is not safe for concurrent use: the client's loop goroutine
// owns it.
type Transport struct {
	ocb      *OCB
	toRemote uint64
	toLocal  uint64

	// Outgoing states.
	sentNum        uint64
	ackedByRemote  uint64
	pendingDiff    []byte
	diffSent       bool
	diffOldNum     uint64
	hasPendingBase bool
	pendingDataAck bool

	// Incoming states. states holds one complete state per received state
	// number, sorted by number, because a host diff is relative to the state
	// its old_num names and the sender may resend from an older base than the
	// last one delivered (see Transport::recv in the C client).
	states         []receivedState
	presented      hostState // the state last handed to the owner; nil until then
	ackNum         uint64
	sentAckNum     uint64
	throwawayNum   uint64
	lastRecvOldNum uint64
	lastRecvNewNum uint64
	lastEchoAck    uint64

	// Crypto sequence, independent of SSP state numbering.
	seqOut      uint64
	seqInMax    uint64
	seqInMaxSet bool

	// fragID identifies one instruction to the fragment assembler. Every
	// instruction gets a fresh id: the C sender's chaff changes on every send,
	// so its instruction id does too, and an ack or retransmission whose other
	// fields changed while an older fragment set was in progress must not be
	// mistaken for a duplicate fragment of it.
	fragID uint64

	lastSend time.Time
	lastRecv time.Time
	lastTS   uint16

	srtt    time.Duration
	rttvar  time.Duration
	rto     time.Duration
	rttInit bool

	assembler FragmentAssembler

	// nextSend is when a newly pending user state may be sent, implementing
	// SEND_MINDELAY batching; nextAck is when an ack is due.
	nextSend time.Time
	nextAck  time.Time

	shutdown             bool
	shutdownStart        time.Time
	counterpartyShutdown bool
	shutdownAcked        bool
}

// receivedState is one host state the receiver holds: its number and the
// complete terminal state the remote described.
type receivedState struct {
	num   uint64
	state hostState
}

// hostState is one complete host-side state. The transport keeps one for every
// received state so a diff can be applied to the state its old_num names, and
// hands the owner only the net difference between delivered states. The client
// uses the terminal screen implementation; transport tests use a plain byte
// stream.
type hostState interface {
	// clone returns an independent copy for a diff to be applied to.
	clone() hostState
	// apply applies one host message and returns the echo ack it named. ok is
	// false when the message could not be understood.
	apply(diff []byte) (echoAck uint64, ok bool)
	// diffFrom returns what turns older into this state; older is nil for the
	// first state the owner sees.
	diffFrom(older hostState) []byte
}

// maxReceivedStates bounds the states held for the remote to reference. The
// remote's own send window and throwaway number keep the queue small; this only
// caps a misbehaving sender. A state that would exceed it is ignored and the
// remote retransmits.
const maxReceivedStates = 64

// NewTransport creates a transport. isServer selects the direction bits, and
// initial is the state number 0 both ends start from.
func NewTransport(ocb *OCB, isServer bool, initial hostState) *Transport {
	now := time.Now()
	t := &Transport{
		ocb:      ocb,
		srtt:     1000 * time.Millisecond,
		rttvar:   500 * time.Millisecond,
		rto:      maxRTOInitial,
		lastSend: now,
		lastRecv: now,
		states:   []receivedState{{num: 0, state: initial}},
		nextAck:  now,
	}
	if isServer {
		t.toRemote = dirToClient
		t.toLocal = dirToServer
	} else {
		t.toRemote = dirToServer
		t.toLocal = dirToClient
	}
	return t
}

// SetPending replaces the user state that should be sent next. diff is the
// cumulative user stream since the state the remote has acknowledged. If a
// state is already in flight the diff waits for its acknowledgement.
func (t *Transport) SetPending(now time.Time, diff []byte) {
	t.pendingDiff = diff
	t.diffSent = false
	// The delay is measured from when this state first became pending, not
	// from the latest rebuild: more input joins the state during the delay
	// instead of postponing it.
	if t.nextSend.IsZero() {
		t.nextSend = now.Add(sendMinDelay)
	}
}

// Shutdown starts the local shutdown handshake.
func (t *Transport) Shutdown(now time.Time) {
	if t.shutdown {
		return
	}
	t.shutdown = true
	t.shutdownStart = now
	t.nextSend = now
}

// ShutdownAcked reports that the remote acknowledged our shutdown state.
func (t *Transport) ShutdownAcked() bool { return t.shutdownAcked }

// CounterpartyShutdown reports that the remote asked to shut down. The client
// answers with an ack and then ends the session.
func (t *Transport) CounterpartyShutdown() bool { return t.counterpartyShutdown }

// SentNum returns the newest state number sent.
func (t *Transport) SentNum() uint64 { return t.sentNum }

// AckedByRemote returns the newest state number the remote acknowledged.
func (t *Transport) AckedByRemote() uint64 { return t.ackedByRemote }

// LastRecvNewNum returns the state number of the newest state received.
func (t *Transport) LastRecvNewNum() uint64 { return t.lastRecvNewNum }

// LastEchoAck returns the newest echo ack carried by a delivered state: the
// newest user input the remote has applied to its terminal.
func (t *Transport) LastEchoAck() uint64 { return t.lastEchoAck }

// LastRecv returns when a datagram was last accepted.
func (t *Transport) LastRecv() time.Time { return t.lastRecv }

// RTO returns the current retransmission timeout.
func (t *Transport) RTO() time.Duration { return t.rto }

// HasPending reports whether a user state is waiting to be sent or is in
// flight.
func (t *Transport) HasPending() bool {
	return len(t.pendingDiff) > 0 || t.diffSent
}

// HasUnsentPending reports whether a state has been prepared but not yet put
// on the wire. The client uses it to rebuild the cumulative user stream while
// more input arrives inside the send delay.
func (t *Transport) HasUnsentPending() bool {
	return len(t.pendingDiff) > 0 && !t.diffSent
}

// Heard reports whether a datagram from the remote has ever been accepted.
func (t *Transport) Heard() bool { return t.seqInMaxSet }

// ShutdownTimedOut reports that a local shutdown has waited long enough.
func (t *Transport) ShutdownTimedOut(now time.Time) bool {
	return t.shutdown && !t.shutdownAcked && now.Sub(t.shutdownStart) >= shutdownTimeout
}

// sendInterval is mosh's frame interval: about two frames per RTT, clamped.
func (t *Transport) sendInterval() time.Duration {
	interval := t.srtt / 2
	if interval < sendIntervalMin {
		return sendIntervalMin
	}
	if interval > sendIntervalMax {
		return sendIntervalMax
	}
	return interval
}

// Tick produces the datagrams that are due at now, if any.
func (t *Transport) Tick(now time.Time) [][]byte {
	switch {
	case t.counterpartyShutdown:
		if t.ackNum != t.sentAckNum {
			return t.send(now, nil, t.sentNum)
		}
		return nil
	case t.shutdown:
		if t.shutdownAcked || now.Sub(t.shutdownStart) >= shutdownTimeout {
			return nil
		}
		if now.Before(t.nextSend) {
			return nil
		}
		t.nextSend = now.Add(t.sendInterval())
		return t.sendShutdown(now)
	}

	haveNewDiff := len(t.pendingDiff) > 0 && !t.diffSent && t.ackedByRemote >= t.sentNum
	needAck := t.ackNum != t.sentAckNum
	retransmit := t.diffSent && t.ackedByRemote < t.sentNum && !now.Before(t.lastSend.Add(t.rto))
	heartbeat := !now.Before(t.lastSend.Add(ackInterval))

	switch {
	case haveNewDiff && !now.Before(t.nextSend):
		t.sentNum++
		t.diffSent = true
		if !t.hasPendingBase {
			t.diffOldNum = t.ackedByRemote
			t.hasPendingBase = true
		}
		t.nextSend = time.Time{}
		return t.send(now, t.pendingDiff, t.sentNum)
	case retransmit:
		return t.send(now, t.pendingDiff, t.sentNum)
	case needAck && !now.Before(t.nextAck):
		t.pendingDataAck = false
		return t.send(now, nil, t.sentNum)
	case heartbeat:
		// An empty state every ackInterval is the keepalive, as in mosh's
		// sender. Its reference is its own number, so a remote that missed it
		// drops the state but still reads the ack it carries.
		t.pendingDataAck = false
		return t.send(now, nil, t.sentNum)
	}
	return nil
}

// NextWake is how long the loop may sleep before calling Tick again. It is
// the earliest of the pending send, the ack deadline, a retransmission, the
// shutdown retry, or the empty-ack heartbeat.
func (t *Transport) NextWake(now time.Time) time.Duration {
	if t.counterpartyShutdown {
		return 0
	}
	var wake time.Time
	if t.shutdown {
		if t.shutdownAcked || now.Sub(t.shutdownStart) >= shutdownTimeout {
			return ackInterval
		}
		wake = t.nextSend
	} else {
		haveNewDiff := len(t.pendingDiff) > 0 && !t.diffSent && t.ackedByRemote >= t.sentNum
		needAck := t.ackNum != t.sentAckNum
		switch {
		case haveNewDiff:
			wake = t.nextSend
		case t.diffSent && t.ackedByRemote < t.sentNum:
			wake = t.lastSend.Add(t.rto)
		case needAck, t.pendingDataAck:
			wake = t.nextAck
			if wake.IsZero() || wake.Before(now) {
				wake = now
			}
		default:
			wake = t.lastSend.Add(ackInterval)
			if wake.Before(now) {
				wake = now
			}
		}
	}
	if wake.Before(now) {
		return 0
	}
	return wake.Sub(now)
}

// send builds, compresses, fragments and encrypts one transport instruction.
// For a new diff (or its retransmission) the reference state is the base the
// diff was made against. An instruction that only carries an ack names the
// newest sent state as both its reference and its new state: a remote that
// already has it deduplicates it, and one that does not drops the state but
// still processes the ack, so an ack never invents a state whose diff was
// lost.
func (t *Transport) send(now time.Time, diff []byte, newNum uint64) [][]byte {
	oldNum := newNum
	if len(diff) > 0 {
		oldNum = t.diffOldNum
	}
	return t.sendInstruction(now, diff, oldNum, newNum)
}

// sendShutdown sends the state number reserved for the shutdown handshake.
// Its reference state is one the remote has: the in-flight diff's base when
// there is one, otherwise the last state sent. Referencing the base lets the
// shutdown register even when the newest state was lost; the last few
// keystrokes of a closing tab are not worth waiting for.
func (t *Transport) sendShutdown(now time.Time) [][]byte {
	oldNum := t.sentNum
	if t.hasPendingBase {
		oldNum = t.diffOldNum
	}
	return t.sendInstruction(now, nil, oldNum, shutdownNum)
}

func (t *Transport) sendInstruction(now time.Time, diff []byte, oldNum, newNum uint64) [][]byte {
	// Tell the remote the oldest state this side may still reference, so its
	// received-state queue does not grow for the life of the session. The base
	// of an in-flight diff is the oldest thing a future first transmission or
	// retransmission can name; advertising anything newer could make the remote
	// discard a state a retransmission still needs.
	throwaway := t.ackedByRemote
	if t.hasPendingBase {
		throwaway = t.diffOldNum
	}
	ti := TransportInstruction{
		ProtocolVersion: 2,
		OldNum:          oldNum,
		NewNum:          newNum,
		AckNum:          t.ackNum,
		ThrowawayNum:    throwaway,
		Diff:            diff,
	}
	t.sentAckNum = t.ackNum

	payload := zlibCompress(ti.Marshal())
	t.fragID++
	frags := Fragmentize(t.fragID, payload)
	datagrams := make([][]byte, 0, len(frags))
	for i := range frags {
		datagrams = append(datagrams, t.encryptFragment(&frags[i], now))
	}
	t.lastSend = now
	t.nextAck = now.Add(ackInterval)
	return datagrams
}

// Recv processes one datagram and returns the terminal update it produced, if
// any. A state that only extends what the owner has is delivered as its net
// difference from the last delivered state; a state that fills a gap is kept
// for later to reference but not delivered. now is the client's clock, for
// testability.
func (t *Transport) Recv(wire []byte, now time.Time) []byte {
	if len(wire) < minDatagram {
		return nil
	}

	dirSeq := binary.BigEndian.Uint64(wire[:8])
	if dirSeq&dirToClient != t.toLocal&dirToClient {
		return nil
	}
	seq := dirSeq & seqMask
	if t.seqInMaxSet && seq <= t.seqInMax {
		return nil // replayed or reordered
	}

	var nonce [12]byte
	copy(nonce[4:], wire[:8])
	plaintext := t.ocb.Decrypt(nonce[:], wire[8:])
	if plaintext == nil || len(plaintext) < 4 {
		return nil
	}
	remoteTS := binary.BigEndian.Uint16(plaintext[0:])
	tsReply := binary.BigEndian.Uint16(plaintext[2:])
	payload := plaintext[4:]

	t.seqInMax, t.seqInMaxSet = seq, true
	t.lastRecv = now
	t.lastTS = remoteTS
	if tsReply != 0 {
		t.updateRTT(tsReply, now)
	}

	if len(payload) < fragmentHeaderSize {
		return nil // heartbeat without a state
	}
	frag, err := UnmarshalFragment(payload)
	if err != nil {
		return nil
	}
	msg := t.assembler.Add(frag)
	if msg == nil {
		return nil
	}
	decompressed := zlibDecompress(msg)
	if decompressed == nil {
		return nil
	}
	var ti TransportInstruction
	if err := ti.Unmarshal(decompressed); err != nil {
		return nil
	}

	if ti.AckNum > t.ackedByRemote {
		t.ackedByRemote = ti.AckNum
		if t.ackedByRemote >= t.sentNum && t.pendingDiff != nil {
			t.pendingDiff = nil
			t.diffSent = false
			t.hasPendingBase = false
			t.pendingDataAck = false
		}
		if t.ackedByRemote == shutdownNum {
			t.shutdownAcked = true
		}
	}

	if ti.NewNum == shutdownNum {
		t.counterpartyShutdown = true
		t.ackNum = shutdownNum
		t.nextAck = now
		// The final screen diff travels with the shutdown state; show it before
		// the tab ends. It is applied to the presented state like any other
		// message, and only its visible difference is delivered.
		if t.presented != nil && len(ti.Diff) > 0 {
			next := t.presented.clone()
			if _, ok := next.apply(ti.Diff); ok {
				update := next.diffFrom(t.presented)
				t.presented = next
				return update
			}
		}
		return nil
	}

	for _, state := range t.states {
		if state.num == ti.NewNum {
			return nil
		}
	}
	// The diff is relative to this particular state. If it was pruned or never
	// arrived, the state cannot be reconstructed and is dropped; the sender
	// will resend from a state the receiver keeps.
	reference := t.reference(ti.OldNum)
	if reference < 0 {
		return nil
	}
	if ti.ThrowawayNum > t.throwawayNum {
		t.throwawayNum = ti.ThrowawayNum
		kept := t.states[:0]
		for _, state := range t.states {
			if state.num >= t.throwawayNum {
				kept = append(kept, state)
			}
		}
		t.states = kept
		reference = t.reference(ti.OldNum)
		if reference < 0 {
			return nil
		}
	}
	if len(t.states) >= maxReceivedStates {
		return nil
	}
	next := t.states[reference].state.clone()
	echoAck, ok := next.apply(ti.Diff)
	if !ok {
		return nil
	}
	t.lastRecvOldNum = ti.OldNum
	if ti.NewNum > t.lastRecvNewNum {
		t.lastRecvNewNum = ti.NewNum
	}
	position := len(t.states)
	for i := range t.states {
		if t.states[i].num > ti.NewNum {
			position = i
			break
		}
	}
	t.states = append(t.states, receivedState{})
	copy(t.states[position+1:], t.states[position:])
	t.states[position] = receivedState{num: ti.NewNum, state: next}
	if position != len(t.states)-1 {
		// A state older than one already held: it is kept so a later state can
		// be applied to it, but it is neither shown nor acknowledged.
		return nil
	}
	update := next.diffFrom(t.presented)
	t.presented = next
	t.ackNum = ti.NewNum
	if echoAck > t.lastEchoAck {
		t.lastEchoAck = echoAck
	}
	if len(ti.Diff) > 0 {
		t.pendingDataAck = true
	}
	t.nextAck = now.Add(ackDelay)
	return update
}

// reference finds the held state a diff names, or -1.
func (t *Transport) reference(num uint64) int {
	for i := range t.states {
		if t.states[i].num == num {
			return i
		}
	}
	return -1
}

// encryptFragment wraps one fragment in the mosh datagram format.
func (t *Transport) encryptFragment(f *Fragment, now time.Time) []byte {
	t.seqOut++
	dirSeq := t.toRemote | (t.seqOut & seqMask)
	var dirSeqBytes [8]byte
	binary.BigEndian.PutUint64(dirSeqBytes[:], dirSeq)

	var nonce [12]byte
	copy(nonce[4:], dirSeqBytes[:])

	fragWire := f.Marshal()
	ts := uint16(now.UnixMilli() & 0xffff)
	plaintext := make([]byte, 4+len(fragWire))
	binary.BigEndian.PutUint16(plaintext[0:], ts)
	binary.BigEndian.PutUint16(plaintext[2:], t.lastTS)
	copy(plaintext[4:], fragWire)

	tagAndCT := t.ocb.Encrypt(nonce[:], plaintext)
	wire := make([]byte, 8+len(tagAndCT))
	copy(wire[:8], dirSeqBytes[:])
	copy(wire[8:], tagAndCT)
	return wire
}

// updateRTT follows RFC 6298 with mosh's clamps.
func (t *Transport) updateRTT(tsReply uint16, now time.Time) {
	now16 := uint16(now.UnixMilli() & 0xffff)
	rttMS := int(now16) - int(tsReply)
	if rttMS < 0 {
		rttMS += 65536
	}
	if rttMS > 30000 {
		return
	}
	rtt := time.Duration(rttMS) * time.Millisecond
	if !t.rttInit {
		t.srtt = rtt
		t.rttvar = rtt / 2
		t.rttInit = true
	} else {
		delta := t.srtt - rtt
		if delta < 0 {
			delta = -delta
		}
		t.rttvar = (3*t.rttvar + delta) / 4
		t.srtt = (7*t.srtt + rtt) / 8
	}
	t.rto = t.srtt + 4*t.rttvar
	if t.rto < minRTO {
		t.rto = minRTO
	}
	if t.rto > maxRTO {
		t.rto = maxRTO
	}
}

// zlibCompress compresses one transport instruction.
func zlibCompress(data []byte) []byte {
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	_, _ = w.Write(data)
	_ = w.Close()
	return buf.Bytes()
}

// zlibDecompress decompresses one transport instruction, bounded so a
// malicious datagram cannot expand without limit.
func zlibDecompress(data []byte) []byte {
	r, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	defer r.Close()
	out, err := io.ReadAll(io.LimitReader(r, maxReassembledSize))
	if err != nil || len(out) >= maxReassembledSize {
		return nil
	}
	return out
}
