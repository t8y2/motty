package xymodem

import (
	"bytes"
	"errors"
	"io"
	"math/rand"
	"testing"
	"time"

	"motty/internal/zmodem"
)

type memWriter struct{ bytes.Buffer }

func (*memWriter) Close() error { return nil }

type collect struct {
	files map[string]*memWriter
	order []string
	infos map[string]zmodem.FileInfo
}

func (c *collect) Open(info zmodem.FileInfo) (io.WriteCloser, error) {
	if c.files == nil {
		c.files, c.infos = map[string]*memWriter{}, map[string]zmodem.FileInfo{}
	}
	w := &memWriter{}
	c.files[info.Name] = w
	c.infos[info.Name] = info
	c.order = append(c.order, info.Name)
	return w, nil
}

type engine interface {
	Feed([]byte)
	TakeOutput([]byte) []byte
	Finished() bool
	Err() error
	Timeout()
}

// run moves bytes between two engines, cut at random places, with the receiver
// asking first; it damages bytes on the way to the receiver when damage > 0.
func run(t *testing.T, sender, receiver engine, random *rand.Rand, damage float64) (damaged int) {
	t.Helper()
	chop := func(data []byte) [][]byte {
		var pieces [][]byte
		for len(data) > 0 {
			n := min(1+random.Intn(150), len(data))
			pieces = append(pieces, data[:n])
			data = data[n:]
		}
		return pieces
	}
	var toSender, toReceiver []byte
	for step := 0; step < 100000; step++ {
		if sender.Finished() && receiver.Finished() {
			return
		}
		toSender = receiver.TakeOutput(toSender)
		toReceiver = sender.TakeOutput(toReceiver)
		if len(toSender) == 0 && len(toReceiver) == 0 {
			sender.Timeout()
			receiver.Timeout()
			continue
		}
		for _, piece := range chop(toReceiver) {
			piece = append([]byte(nil), piece...)
			for i := range piece {
				if damage > 0 && random.Float64() < damage {
					piece[i] ^= 1 << uint(random.Intn(8))
					damaged++
				}
			}
			receiver.Feed(piece)
		}
		toReceiver = toReceiver[:0]
		for _, piece := range chop(toSender) {
			sender.Feed(piece)
		}
		toSender = toSender[:0]
	}
	t.Fatal("never ended")
	return
}

func files(random *rand.Rand, sizes ...int) []zmodem.SendFile {
	var out []zmodem.SendFile
	for index, size := range sizes {
		data := make([]byte, size)
		random.Read(data)
		out = append(out, zmodem.SendFile{
			Name: []string{"a.bin", "b.bin", "c.bin", "d.bin", "e.bin"}[index], Size: int64(size), Source: bytes.NewReader(data),
			ModTime: time.Unix(1_700_000_000, 0), Mode: 0o644,
		})
	}
	return out
}

func content(file zmodem.SendFile) []byte {
	data := make([]byte, file.Size)
	file.Source.ReadAt(data, 0)
	return data
}

func TestChecksumAndCRCKnownValues(t *testing.T) {
	if got := crc16([]byte("123456789")); got != 0x31C3 {
		t.Errorf("crc16 = %#x", got)
	}
	if got := checksum([]byte{200, 100, 1}); got != 45 {
		t.Errorf("checksum = %d", got)
	}
}

func TestYmodemBatchRoundTripsWithExactSizes(t *testing.T) {
	for name, damage := range map[string]float64{"clean": 0, "noisy": 0.0005} {
		t.Run(name, func(t *testing.T) {
			random := rand.New(rand.NewSource(1))
			list := files(random, 0, 1, 127, 1024, 5000)
			target := &collect{}
			sender, receiver := NewSender(YMODEM, list), NewReceiver(YMODEM, "", target)
			run(t, sender, receiver, random, damage)
			if sender.Err() != nil || receiver.Err() != nil {
				t.Fatalf("sender %v receiver %v", sender.Err(), receiver.Err())
			}
			if len(target.order) != len(list) {
				t.Fatalf("received %v", target.order)
			}
			for _, file := range list {
				if got := target.files[file.Name]; got == nil || !bytes.Equal(got.Bytes(), content(file)) {
					t.Errorf("%s differs (%d bytes)", file.Name, got.Len())
				}
				if info := target.infos[file.Name]; info.Size != file.Size || !info.ModTime.Equal(file.ModTime) {
					t.Errorf("%s info = %+v", file.Name, info)
				}
			}
		})
	}
}

func TestXmodemSendsOneFileAndTheReceiverTrimsThePadding(t *testing.T) {
	for _, mode := range []Mode{XMODEM, XMODEM1K} {
		random := rand.New(rand.NewSource(2))
		list := files(random, 300)
		list[0].Source = bytes.NewReader(bytes.Repeat([]byte("text line\n"), 30))
		list[0].Size = 300
		target := &collect{}
		sender, receiver := NewSender(mode, list), NewReceiver(mode, "saved.txt", target)
		run(t, sender, receiver, random, 0)
		if sender.Err() != nil || receiver.Err() != nil {
			t.Fatalf("mode %d: %v %v", mode, sender.Err(), receiver.Err())
		}
		got := target.files["saved.txt"]
		if got == nil || !bytes.Equal(got.Bytes(), bytes.Repeat([]byte("text line\n"), 30)) {
			t.Fatalf("mode %d: saved %q", mode, got)
		}
	}
}

func TestXmodemSurvivesNoiseAndLostAcknowledgements(t *testing.T) {
	random := rand.New(rand.NewSource(3))
	list := files(random, 20000)
	// Text-like data so the SUB trimming cannot hide a loss.
	data := bytes.Repeat([]byte("0123456789abcdef"), 1250)
	list[0].Source, list[0].Size = bytes.NewReader(data), int64(len(data))
	target := &collect{}
	sender, receiver := NewSender(XMODEM1K, list), NewReceiver(XMODEM1K, "x", target)
	damaged := run(t, sender, receiver, random, 0.0004)
	if damaged == 0 {
		t.Fatal("nothing was damaged")
	}
	if sender.Err() != nil || receiver.Err() != nil || !bytes.Equal(target.files["x"].Bytes(), data) {
		t.Fatalf("err %v %v, got %d bytes", sender.Err(), receiver.Err(), target.files["x"].Len())
	}
}

func TestAReceiverFallsBackToChecksumsForAnOldSender(t *testing.T) {
	data := bytes.Repeat([]byte("old sender "), 40)
	target := &collect{}
	receiver := NewReceiver(XMODEM, "f", target)
	receiver.TakeOutput(nil)
	for range 3 {
		receiver.Timeout()
	}
	out := receiver.TakeOutput(nil)
	if len(out) == 0 || out[len(out)-1] != nak {
		t.Fatalf("after silence the receiver sent %v, want it to ask for checksums", out)
	}
	// A checksum block.
	block := []byte{soh, 1, 254}
	chunk := append(append([]byte(nil), data[:128]...), nil...)
	block = append(block, chunk...)
	block = append(block, checksum(chunk))
	receiver.Feed(block)
	if got := receiver.TakeOutput(nil); len(got) != 1 || got[0] != ack {
		t.Fatalf("reply = %v", got)
	}
	receiver.Feed([]byte{eot})
	if !receiver.Finished() || !bytes.Equal(target.files["f"].Bytes(), data[:128]) {
		t.Fatalf("finished %v, got %d bytes", receiver.Finished(), target.files["f"].Len())
	}
}

func TestADuplicateBlockIsAcknowledgedAndNotWrittenTwice(t *testing.T) {
	target := &collect{}
	receiver := NewReceiver(XMODEM, "f", target)
	receiver.TakeOutput(nil)
	data := bytes.Repeat([]byte{'a'}, 128)
	sum := crc16(data)
	block := append(append([]byte{soh, 1, 254}, data...), byte(sum>>8), byte(sum))
	receiver.Feed(block)
	receiver.Feed(block)
	if got := receiver.TakeOutput(nil); !bytes.Equal(got, []byte{ack, ack}) {
		t.Fatalf("replies = %v", got)
	}
	receiver.Feed([]byte{eot})
	if target.files["f"].Len() != 128 {
		t.Fatalf("wrote %d bytes", target.files["f"].Len())
	}
}

func TestOutOfOrderBlocksAndFloodsOfDamageEndTheTransfer(t *testing.T) {
	receiver := NewReceiver(XMODEM, "f", &collect{})
	receiver.TakeOutput(nil)
	data := make([]byte, 128)
	sum := crc16(data)
	receiver.Feed(append(append([]byte{soh, 5, 250}, data...), byte(sum>>8), byte(sum)))
	if !errors.Is(receiver.Err(), ErrOutOfStep) {
		t.Fatalf("err = %v", receiver.Err())
	}
	receiver = NewReceiver(XMODEM, "f", &collect{})
	for range 40 {
		receiver.Feed(append(append([]byte{soh, 1, 254}, make([]byte, 129)...), 0x55))
	}
	if !errors.Is(receiver.Err(), ErrTooManyErrors) {
		t.Fatalf("err = %v", receiver.Err())
	}
}

func TestCancellationFromEitherSide(t *testing.T) {
	receiver := NewReceiver(YMODEM, "", &collect{})
	receiver.Feed([]byte{can, can})
	if !errors.Is(receiver.Err(), ErrPeerCancelled) {
		t.Fatalf("receiver err = %v", receiver.Err())
	}
	sender := NewSender(YMODEM, files(rand.New(rand.NewSource(4)), 100))
	sender.Cancel()
	if !errors.Is(sender.Err(), ErrCancelled) || len(sender.TakeOutput(nil)) == 0 {
		t.Fatalf("sender err = %v", sender.Err())
	}
}

func TestSilenceEndsTheTransfer(t *testing.T) {
	for name, e := range map[string]engine{
		"receiver": NewReceiver(XMODEM, "f", &collect{}),
		"sender":   NewSender(XMODEM, files(rand.New(rand.NewSource(5)), 10)),
	} {
		for range 20 {
			e.Timeout()
		}
		if !e.Finished() || !errors.Is(e.Err(), ErrTimeout) {
			t.Errorf("%s: finished %v err %v", name, e.Finished(), e.Err())
		}
	}
}

func TestAYmodemNameIsReducedToAPlainName(t *testing.T) {
	info := parseFileInfo([]byte("../../etc/passwd\x001234 0 644"))
	if info.Name != "passwd" || info.Size != 1234 {
		t.Fatalf("info = %+v", info)
	}
	if parseFileInfo([]byte("\x00")).Name != "download" {
		t.Fatal("an empty name needs a stand-in")
	}
}

func FuzzReceiver(f *testing.F) {
	f.Add([]byte{soh, 1, 254, 0, 0})
	f.Add([]byte{eot, eot, can, can})
	f.Fuzz(func(t *testing.T, data []byte) {
		for _, mode := range []Mode{XMODEM, XMODEM1K, YMODEM} {
			receiver := NewReceiver(mode, "f", &collect{})
			receiver.Feed(data)
			receiver.Feed(data)
			receiver.TakeOutput(nil)
			receiver.Timeout()
			receiver.Progress()
		}
	})
}

func FuzzSender(f *testing.F) {
	f.Add([]byte{'C', ack, ack, nak, 'C'})
	f.Fuzz(func(t *testing.T, data []byte) {
		for _, mode := range []Mode{XMODEM, YMODEM} {
			sender := NewSender(mode, []zmodem.SendFile{{Name: "x", Size: 3000, Source: bytes.NewReader(make([]byte, 3000))}})
			sender.Feed([]byte{'C'})
			sender.Feed(data)
			sender.TakeOutput(nil)
			sender.Timeout()
			sender.Progress()
		}
	})
}

func TestTwoCANBytesInsideABlockAreData(t *testing.T) {
	data := bytes.Repeat([]byte{can}, 128)
	target := &collect{}
	receiver := NewReceiver(XMODEM, "f", target)
	receiver.TakeOutput(nil)
	sum := crc16(data)
	receiver.Feed(append(append([]byte{soh, 1, 254}, data...), byte(sum>>8), byte(sum)))
	receiver.Feed([]byte{eot})
	if receiver.Err() != nil || !bytes.Equal(target.files["f"].Bytes(), data) {
		t.Fatalf("err %v, got %d bytes", receiver.Err(), target.files["f"].Len())
	}
}

// A receiver sends its start NAK every second until it sees SOH, so a backlog
// of them can arrive after the first block is out; they are not damage reports.
func TestXmodemSenderIgnoresQueuedStartNAKs(t *testing.T) {
	random := rand.New(rand.NewSource(3))
	list := files(random, 300)
	sender := NewSender(XMODEM, list)
	sender.Feed(bytes.Repeat([]byte{nak}, 20))
	if sender.Err() != nil || sender.Finished() {
		t.Fatalf("a backlog of start NAKs ended the transfer: %v", sender.Err())
	}
	if got := len(sender.TakeOutput(nil)); got != 3+128+1 {
		t.Fatalf("sent %d bytes, want exactly one 128-byte block", got)
	}
	sender.Timeout() // a real NAK for block 1 is recovered by the timeout
	if got := len(sender.TakeOutput(nil)); got != 3+128+1 {
		t.Fatalf("timeout resent %d bytes", got)
	}
}
