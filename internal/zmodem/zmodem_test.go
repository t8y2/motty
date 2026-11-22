package zmodem

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"strings"
	"testing"
	"time"
)

func TestKnownVectors(t *testing.T) {
	if got := crc16Fast(0, []byte("123456789")); got != 0x31C3 {
		t.Errorf("CRC-16 = %#x", got)
	}
	if got := crc32Update(0, []byte("123456789")); got != 0xCBF43926 {
		t.Errorf("CRC-32 = %#x", got)
	}
	// The ZRINIT every rz prints: flags 0x23, check 0xbe50.
	got := appendHexHeader(nil, zRINIT, [4]byte{0, 0, 0, 0x23})
	if want := "**\x18B0100000023be50\r\x8a\x11"; string(got) != want {
		t.Errorf("ZRINIT = %q, want %q", got, want)
	}
	// ZRQINIT as sz sends it.
	if want, got := "**\x18B00000000000000\r\x8a\x11", string(appendHexHeader(nil, zRQINIT, [4]byte{})); want != got {
		t.Errorf("ZRQINIT = %q, want %q", got, want)
	}
}

func TestHeadersRoundTripInEveryForm(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for kind := byte(0); kind <= zCOMMAND; kind++ {
			args := [4]byte{0x18, 0x11, 0x0d, 0xff}
			var p parser
			for _, b := range appendBinaryHeader(nil, kind, args, wide) {
				if event := p.step(b); event == evHeader {
					if p.hdr.kind != kind || p.hdr.flags != args || p.hdr.crc32 != wide {
						t.Fatalf("binary wide=%v kind %d: %+v", wide, kind, p.hdr)
					}
					goto next
				} else if event != evNone {
					t.Fatalf("binary wide=%v kind %d: event %d", wide, kind, event)
				}
			}
			t.Fatalf("binary wide=%v kind %d: no header", wide, kind)
		next:
		}
	}
	var p parser
	events := 0
	for _, b := range appendHexHeader(nil, zRPOS, positionBytes(0x01020304)) {
		if p.step(b) == evHeader {
			events++
			if p.hdr.kind != zRPOS || p.hdr.position != 0x01020304 {
				t.Fatalf("hex header = %+v", p.hdr)
			}
		}
	}
	if events != 1 {
		t.Fatalf("%d hex headers", events)
	}
}

func TestEveryByteSurvivesASubpacket(t *testing.T) {
	data := make([]byte, 256*3)
	for index := range data {
		data[index] = byte(index)
	}
	for _, wide := range []bool{false, true} {
		for _, end := range []byte{zcrce, zcrcg, zcrcq, zcrcw} {
			var p parser
			p.wantData(wide)
			encoded := appendSubpacket(nil, data, end, wide)
			var got event
			for _, b := range encoded {
				got = p.step(b)
			}
			if got != evData || !bytes.Equal(p.data, data) || p.end != end {
				t.Fatalf("wide=%v end=%c: event %d, %d bytes", wide, end, got, len(p.data))
			}
		}
	}
}

// memTarget collects received files in memory.
type memTarget struct {
	files map[string]*bytes.Buffer
	order []string
	skip  map[string]bool
	infos map[string]FileInfo
}

type bufferCloser struct{ *bytes.Buffer }

func (bufferCloser) Close() error { return nil }

func (target *memTarget) Open(info FileInfo) (io.WriteCloser, error) {
	if target.skip[info.Name] {
		return nil, nil
	}
	if target.files == nil {
		target.files, target.infos = map[string]*bytes.Buffer{}, map[string]FileInfo{}
	}
	buffer := &bytes.Buffer{}
	target.files[info.Name] = buffer
	target.infos[info.Name] = info
	target.order = append(target.order, info.Name)
	return bufferCloser{buffer}, nil
}

// link connects two engines, cutting every transfer between them at random
// places, and optionally damaging bytes on the way to the receiver.
type link struct {
	random *rand.Rand
	// damage is the chance, per byte, of flipping it on the way to the receiver.
	damage float64
	// damaged counts them.
	damaged int
}

func (l *link) chop(data []byte) [][]byte {
	var pieces [][]byte
	for len(data) > 0 {
		n := 1 + l.random.Intn(97)
		if n > len(data) {
			n = len(data)
		}
		pieces = append(pieces, data[:n])
		data = data[n:]
	}
	return pieces
}

// run moves bytes between a sender and a receiver until both are done.
func (l *link) run(t *testing.T, sender, receiver Engine, receiverFirst []byte) {
	t.Helper()
	toReceiver := append([]byte(nil), receiverFirst...)
	var toSender []byte
	for step := 0; step < 200000; step++ {
		if sender.Finished() && receiver.Finished() && sender.Settled() && receiver.Settled() {
			return
		}
		toSender = receiver.TakeOutput(toSender)
		toReceiver = sender.TakeOutput(toReceiver)
		if len(toReceiver) == 0 && len(toSender) == 0 {
			if sender.Finished() && receiver.Finished() {
				return
			}
			// Both sides are waiting: the quiet that the owner turns into timeouts.
			sender.Timeout()
			receiver.Timeout()
			continue
		}
		for _, piece := range l.chop(toReceiver) {
			if l.damage > 0 {
				piece = append([]byte(nil), piece...)
				for index := range piece {
					if l.random.Float64() < l.damage {
						piece[index] ^= 1 << uint(l.random.Intn(8))
						l.damaged++
					}
				}
			}
			receiver.Feed(piece)
		}
		toReceiver = toReceiver[:0]
		for _, piece := range l.chop(toSender) {
			sender.Feed(piece)
		}
		toSender = toSender[:0]
	}
	t.Fatal("the transfer never ended")
}

func testFiles(random *rand.Rand, sizes ...int) []SendFile {
	var files []SendFile
	for index, size := range sizes {
		data := make([]byte, size)
		random.Read(data)
		if size > 600 {
			for value := 0; value < 256; value++ { // every byte, so every escape is hit
				data[value*2] = byte(value)
			}
		}
		files = append(files, SendFile{
			Name: fmt.Sprintf("file%d.bin", index), Size: int64(size), ModTime: time.Unix(1700000000+int64(index), 0), Mode: 0o644,
			Source: bytes.NewReader(data),
		})
	}
	return files
}

func contentOf(file SendFile) []byte {
	data := make([]byte, file.Size)
	file.Source.ReadAt(data, 0)
	return data
}

// start is what an rz prints when it waits to receive: a ZRINIT.
func start() []byte {
	return appendHexHeader(nil, zRINIT, [4]byte{0, 0, 0, canFDX | canOVIO | canFC32})
}

func TestAFullTransferInBothDirections(t *testing.T) {
	for name, damage := range map[string]float64{"clean": 0, "noisy": 0.0004} {
		t.Run(name, func(t *testing.T) {
			random := rand.New(rand.NewSource(2))
			files := testFiles(random, 0, 1, 1023, 1024, 1025, 70000, 200000)
			target := &memTarget{}
			sender, receiver := NewSender(files, SendOptions{BlockSize: 1024, Window: 8192}), NewReceiver(target)
			sender.Feed(start()) // rz waiting to receive
			l := &link{random: random, damage: damage}
			l.run(t, sender, receiver, nil)
			if sender.Err() != nil || receiver.Err() != nil {
				t.Fatalf("sender %v receiver %v (damaged %d bytes)", sender.Err(), receiver.Err(), l.damaged)
			}
			if damage > 0 && l.damaged == 0 {
				t.Fatal("the test damaged nothing")
			}
			if len(target.order) != len(files) {
				t.Fatalf("received %v", target.order)
			}
			for _, file := range files {
				got := target.files[file.Name]
				if got == nil || !bytes.Equal(got.Bytes(), contentOf(file)) {
					t.Errorf("%s differs (damaged %d bytes in all)", file.Name, l.damaged)
				}
				if info := target.infos[file.Name]; info.Size != file.Size || !info.ModTime.Equal(file.ModTime) || info.Mode&0o777 != 0o644 {
					t.Errorf("%s info = %+v", file.Name, info)
				}
			}
			if sender.Progress().Files != len(files) || receiver.Progress().Files != len(files) {
				t.Errorf("files done: sender %d receiver %d", sender.Progress().Files, receiver.Progress().Files)
			}
		})
	}
}

func TestSkippedFilesAreNotSent(t *testing.T) {
	random := rand.New(rand.NewSource(3))
	files := testFiles(random, 5000, 6000, 7000)
	target := &memTarget{skip: map[string]bool{"file1.bin": true}}
	sender, receiver := NewSender(files, SendOptions{}), NewReceiver(target)
	sender.Feed(start())
	(&link{random: random}).run(t, sender, receiver, nil)
	if sender.Err() != nil || receiver.Err() != nil {
		t.Fatalf("%v %v", sender.Err(), receiver.Err())
	}
	if len(target.order) != 2 || target.files["file0.bin"] == nil || target.files["file2.bin"] == nil {
		t.Fatalf("received %v", target.order)
	}
}

func TestThePeerHandsTheTransferBackToTheTerminal(t *testing.T) {
	random := rand.New(rand.NewSource(4))
	files := testFiles(random, 3000)
	sender, receiver := NewSender(files, SendOptions{}), NewReceiver(&memTarget{})
	sender.Feed(start())
	(&link{random: random}).run(t, sender, receiver, nil)
	// What follows the sender's "OO" is the shell's again.
	receiver.Feed([]byte("\r\n$ "))
	if got := string(receiver.Leftover()); got != "\r\n$ " {
		t.Fatalf("leftover = %q", got)
	}
}

func TestOOIsSwallowedEvenWhenItArrivesLate(t *testing.T) {
	receiver := NewReceiver(&memTarget{})
	receiver.Feed(appendHexHeader(nil, zFIN, [4]byte{}))
	if !receiver.Finished() || receiver.Settled() {
		t.Fatalf("finished %v settled %v", receiver.Finished(), receiver.Settled())
	}
	receiver.Feed([]byte("O"))
	receiver.Feed([]byte("O$ "))
	if !receiver.Settled() || string(receiver.Leftover()) != "$ " {
		t.Fatalf("settled %v leftover %q", receiver.Settled(), receiver.Leftover())
	}
}

// The uploading terminal gets the shell back after the receiver's ZFIN, and the
// ZFIN is a hex header: its CR LF is the protocol's, not text to draw.
func TestTheUploadingTerminalComesBackClean(t *testing.T) {
	random := rand.New(rand.NewSource(6))
	files := testFiles(random, 3000)
	sender, receiver := NewSender(files, SendOptions{}), NewReceiver(&memTarget{})
	sender.Feed(start())
	(&link{random: random}).run(t, sender, receiver, nil)
	if sender.Err() != nil {
		t.Fatalf("sender: %v", sender.Err())
	}
	if got := sender.Leftover(); len(got) != 0 {
		t.Fatalf("the closing header's terminator leaked into the terminal: %q", got)
	}
	sender.Feed([]byte("\r\n$ "))
	if got := string(sender.Leftover()); got != "\r\n$ " {
		t.Fatalf("leftover = %q", got)
	}
}

// awaitingClosingHeader drives a sender to the point where only the receiver's
// closing ZFIN is missing.
func awaitingClosingHeader(t *testing.T) *Sender {
	t.Helper()
	sender := NewSender([]SendFile{{Name: "f", Source: bytes.NewReader(nil)}}, SendOptions{})
	sender.Feed(start())                                       // the file is offered
	sender.Feed(appendHexHeader(nil, zRPOS, positionBytes(0))) // its (empty) body is sent
	sender.Feed(start())                                       // the receiver takes it: ZFIN is out
	sender.TakeOutput(nil)
	if sender.Finished() {
		t.Fatal("the transfer finished before the closing header")
	}
	return sender
}

// The header's CR LF may be cut anywhere from the header itself; neither byte
// may reach the terminal, and the transfer is settled once both have arrived.
func TestTheClosingHeaderTerminatorIsSwallowedHoweverItIsCut(t *testing.T) {
	sender := awaitingClosingHeader(t)
	closing := appendHexHeader(nil, zFIN, [4]byte{})
	trailer := closing[len(closing)-2:]
	sender.Feed(closing[:len(closing)-2])
	if !sender.Finished() || sender.Settled() {
		t.Fatalf("finished %v settled %v before the header ended", sender.Finished(), sender.Settled())
	}
	sender.Feed(trailer[:1])
	if sender.Settled() {
		t.Fatal("settled in the middle of the header's terminator")
	}
	sender.Feed(trailer[1:])
	if !sender.Settled() {
		t.Fatal("the transfer never settled once the header ended")
	}
	if got := sender.Leftover(); len(got) != 0 {
		t.Fatalf("leftover = %q", got)
	}
	sender.Feed([]byte("$ "))
	if got := string(sender.Leftover()); got != "$ " {
		t.Fatalf("leftover = %q", got)
	}
}

func TestCancellingEitherSideEndsBothWithTheRightReason(t *testing.T) {
	random := rand.New(rand.NewSource(5))
	files := testFiles(random, 100000)
	target := &memTarget{}
	sender, receiver := NewSender(files, SendOptions{Window: 4096}), NewReceiver(target)
	sender.Feed(start())
	// Run a few exchanges, then cancel the receiver.
	var toSender, toReceiver []byte
	for range 6 {
		toSender = receiver.TakeOutput(toSender)
		toReceiver = sender.TakeOutput(toReceiver)
		receiver.Feed(toReceiver)
		sender.Feed(toSender)
		toSender, toReceiver = toSender[:0], toReceiver[:0]
	}
	receiver.Cancel()
	if !errors.Is(receiver.Err(), ErrCancelled) {
		t.Fatalf("receiver err = %v", receiver.Err())
	}
	sender.Feed(receiver.TakeOutput(nil))
	if !errors.Is(sender.Err(), ErrPeerCancelled) {
		t.Fatalf("sender err = %v", sender.Err())
	}
}

func TestAPeerNeverGetsToChooseWhereAFileGoesOrRunACommand(t *testing.T) {
	var names []string
	target := targetFunc(func(info FileInfo) (io.WriteCloser, error) {
		names = append(names, info.Name)
		return nil, nil
	})
	receiver := NewReceiver(target)
	for _, name := range []string{"../../etc/passwd", `C:\Windows\evil.exe`, "/abs/path/x", "a\x01b\x7fc", "..", "", strings.Repeat("n", 500)} {
		info := fmt.Sprintf("%s\x00123 0 644", name)
		receiver.Feed(appendBinaryHeader(nil, zFILE, [4]byte{0, 0, 0, 1}, true))
		receiver.Feed(appendSubpacket(nil, []byte(info), zcrcw, true))
	}
	want := []string{"passwd", "evil.exe", "x", "a_b_c", "download", "download"}
	for index, w := range want {
		if names[index] != w {
			t.Errorf("name %d = %q, want %q", index, names[index], w)
		}
	}
	if len(names[len(names)-1]) > 200 {
		t.Errorf("a %d byte name was accepted", len(names[len(names)-1]))
	}
	// A command is answered with failure and nothing else.
	receiver = NewReceiver(target)
	receiver.Feed(appendBinaryHeader(nil, zCOMMAND, [4]byte{}, false))
	receiver.Feed(appendSubpacket(nil, []byte("rm -rf /\x00"), zcrcw, false))
	out := receiver.TakeOutput(nil)
	var p parser
	var kinds []byte
	for _, b := range out {
		if p.step(b) == evHeader {
			kinds = append(kinds, p.hdr.kind)
		}
	}
	if len(kinds) != 1 || kinds[0] != zCOMPL || p.hdr.flags[0] != 1 {
		t.Fatalf("answered %v", kinds)
	}
}

type targetFunc func(FileInfo) (io.WriteCloser, error)

func (f targetFunc) Open(info FileInfo) (io.WriteCloser, error) { return f(info) }

func TestASenderThatLiesAboutSizeCostsNothing(t *testing.T) {
	target := &memTarget{}
	receiver := NewReceiver(target)
	receiver.Feed(appendBinaryHeader(nil, zFILE, [4]byte{0, 0, 0, 1}, true))
	receiver.Feed(appendSubpacket(nil, []byte("big\x0099999999999999 0 644"), zcrcw, true))
	if receiver.Finished() || len(target.files) != 0 {
		t.Fatal("a file larger than a position can hold was accepted")
	}
	// ...and a stream that never ends a subpacket is bounded.
	receiver = NewReceiver(&memTarget{})
	receiver.Feed(appendBinaryHeader(nil, zFILE, [4]byte{0, 0, 0, 1}, false))
	receiver.Feed(bytes.Repeat([]byte("x"), 10<<20))
	if len(receiver.p.data) > maxSubpacket {
		t.Fatalf("buffered %d bytes", len(receiver.p.data))
	}
}

func TestAStreamOfGarbageEndsTheTransfer(t *testing.T) {
	receiver := NewReceiver(&memTarget{})
	for range 100 {
		receiver.Feed([]byte("**\x18Bzz\r\n"))
	}
	if !receiver.Finished() || !errors.Is(receiver.Err(), ErrTooManyErrors) {
		t.Fatalf("finished %v err %v", receiver.Finished(), receiver.Err())
	}
}

func TestFiveCancelsFromThePeerEndTheTransfer(t *testing.T) {
	receiver := NewReceiver(&memTarget{})
	receiver.Feed(append(bytes.Repeat([]byte{zdle}, 8), bytes.Repeat([]byte{8}, 10)...))
	receiver.Feed([]byte("$ "))
	if !errors.Is(receiver.Err(), ErrPeerCancelled) || string(receiver.Leftover()) != "$ " {
		t.Fatalf("err %v leftover %q", receiver.Err(), receiver.Leftover())
	}
}

func TestASenderStopsWhenItsFileShrinks(t *testing.T) {
	file := SendFile{Name: "f", Size: 5000, Source: bytes.NewReader(make([]byte, 100))}
	sender := NewSender([]SendFile{file}, SendOptions{})
	sender.Feed(start())
	sender.Feed(appendHexHeader(nil, zRPOS, positionBytes(0)))
	if !sender.Finished() || sender.Err() == nil {
		t.Fatalf("finished %v err %v", sender.Finished(), sender.Err())
	}
}

func TestAReceiverAsksAgainWhereADamagedBlockWas(t *testing.T) {
	random := rand.New(rand.NewSource(6))
	files := testFiles(random, 20000)
	target := &memTarget{}
	receiver := NewReceiver(target)
	receiver.Feed(appendBinaryHeader(nil, zFILE, [4]byte{0, 0, 0, 1}, true))
	receiver.Feed(appendSubpacket(nil, []byte("file0.bin\x0020000 0 644"), zcrcw, true))
	receiver.TakeOutput(nil)
	data := contentOf(files[0])
	receiver.Feed(appendBinaryHeader(nil, zDATA, positionBytes(0), true))
	good := appendSubpacket(nil, data[:1024], zcrcg, true)
	receiver.Feed(good)
	bad := appendSubpacket(nil, data[1024:2048], zcrcg, true)
	bad[10] ^= 0x01
	receiver.Feed(bad)
	out := receiver.TakeOutput(nil)
	var p parser
	var asked uint32
	for _, b := range out {
		if p.step(b) == evHeader && p.hdr.kind == zRPOS {
			asked = p.hdr.position
		}
	}
	if asked != 1024 {
		t.Fatalf("asked to resume at %d", asked)
	}
	if got := target.files["file0.bin"].Len(); got != 1024 {
		t.Fatalf("%d bytes written; a damaged block must not be", got)
	}
}

func FuzzReceiver(f *testing.F) {
	f.Add(appendHexHeader(nil, zRQINIT, [4]byte{}))
	f.Add(append(appendBinaryHeader(nil, zFILE, [4]byte{0, 0, 0, 1}, true), appendSubpacket(nil, []byte("a\x0012 0 644"), zcrcw, true)...))
	f.Add([]byte("**\x18B\x18\x18\x18\x18\x18"))
	f.Fuzz(func(t *testing.T, data []byte) {
		receiver := NewReceiver(&memTarget{})
		receiver.Feed(data)
		receiver.Feed(data)
		receiver.TakeOutput(nil)
		receiver.Progress()
		receiver.Cancel()
	})
}

func FuzzSender(f *testing.F) {
	f.Add(start())
	f.Add(appendHexHeader(nil, zRPOS, positionBytes(5)))
	f.Fuzz(func(t *testing.T, data []byte) {
		sender := NewSender([]SendFile{{Name: "x", Size: 3000, Source: bytes.NewReader(make([]byte, 3000))}}, SendOptions{})
		sender.Feed(start())
		sender.Feed(data)
		sender.TakeOutput(nil)
		sender.Progress()
	})
}

// fileHeaderCount counts ZFILE headers, the frame that offers one file.
func fileHeaderCount(data []byte) int {
	return bytes.Count(data, []byte{'*', 0x18, 'A', zFILE}) + bytes.Count(data, []byte{'*', 0x18, 'C', zFILE})
}

// A receiver that is still waiting for the first file sends ready again. That
// ready means the header never arrived, so it is said again.
func TestTheFirstFileIsOfferedAgainWhileTheReceiverIsStillWaiting(t *testing.T) {
	sender := NewSender([]SendFile{{Name: "only.bin", Size: 4, Source: bytes.NewReader([]byte("aaaa"))}}, SendOptions{})
	sender.Feed(start())
	if got := fileHeaderCount(sender.TakeOutput(nil)); got != 1 {
		t.Fatalf("the first offer contained %d file headers", got)
	}
	sender.Feed(start())
	if got := fileHeaderCount(sender.TakeOutput(nil)); got != 1 {
		t.Fatalf("a ready before the first file was accepted offered it %d times", got)
	}
	if sender.Progress().Name != "only.bin" {
		t.Fatalf("progress name = %q", sender.Progress().Name)
	}
}

// lrz answers a repeated end-of-file by sending ready again. That second ready
// arrives after the next file has already been offered, and offering it again
// makes lrz open another file.
func TestARepeatedReadyBetweenFilesOffersTheNextFileOnce(t *testing.T) {
	sender := NewSender([]SendFile{
		{Name: "file0.bin", Size: 4, Source: bytes.NewReader([]byte("aaaa"))},
		{Name: "file1.bin", Size: 4, Source: bytes.NewReader([]byte("bbbb"))},
	}, SendOptions{})
	sender.Feed(start())
	if got := fileHeaderCount(sender.TakeOutput(nil)); got != 1 || sender.Progress().Name != "file0.bin" {
		t.Fatalf("first offer has %d file headers, name %q", got, sender.Progress().Name)
	}
	sender.Feed(appendBinaryHeader(nil, zRPOS, positionBytes(0), true))
	sender.TakeOutput(nil) // the file, then its end
	sender.Feed(append(start(), start()...))
	next := sender.TakeOutput(nil)
	if got := fileHeaderCount(next); got != 1 {
		t.Fatalf("two readies between files offered the next file %d times", got)
	}
	if sender.Progress().Name != "file1.bin" {
		t.Fatalf("progress name = %q", sender.Progress().Name)
	}
	sender.Timeout()
	if got := fileHeaderCount(sender.TakeOutput(nil)); got != 1 {
		t.Fatalf("a timeout offered the waiting file %d times", got)
	}
}

func TestASilentPeerEndsTheTransferAfterEnoughTimeouts(t *testing.T) {
	for name, engine := range map[string]Engine{
		"receiver": NewReceiver(&memTarget{}),
		"sender":   NewSender([]SendFile{{Name: "x", Size: 10, Source: bytes.NewReader(make([]byte, 10))}}, SendOptions{}),
	} {
		for range 20 {
			engine.Timeout()
		}
		if !engine.Finished() || !errors.Is(engine.Err(), ErrTimeout) {
			t.Errorf("%s: finished %v err %v", name, engine.Finished(), engine.Err())
		}
	}
}
