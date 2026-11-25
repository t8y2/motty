package rdp

import (
	"bytes"
	"encoding/binary"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"motty/internal/rdpcore/protocol/audin"
	"motty/internal/rdpcore/protocol/dvc"
	"motty/internal/rdpcore/protocol/mcs"
	"motty/internal/rdpcore/protocol/sec"
	"motty/internal/rdpcore/protocol/tpkt"
	"motty/internal/rdpcore/protocol/x224"
)

func TestSendClientInfoSetsAudioCaptureFlag(t *testing.T) {
	readFlags := func(withAudioInput bool) uint32 {
		t.Helper()
		cConn, sConn := net.Pipe()
		defer cConn.Close()
		defer sConn.Close()
		_ = cConn.SetDeadline(time.Now().Add(3 * time.Second))
		_ = sConn.SetDeadline(time.Now().Add(3 * time.Second))

		opts := DefaultOptions()
		opts.Host = "127.0.0.1"
		opts.Username = "alice"
		if withAudioInput {
			opts.AudioInput = func(uint32, uint16, uint32) (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(nil)), nil
			}
		}
		client := &Client{
			opts:          opts,
			log:           slog.New(slog.DiscardHandler),
			userChannelID: 1002,
			ioChannelID:   1003,
			tpktConn:      tpkt.NewConn(cConn, slog.New(slog.DiscardHandler)),
		}

		errCh := make(chan error, 1)
		go func() {
			errCh <- client.sendClientInfo()
		}()

		raw, err := tpkt.NewConn(sConn, slog.New(slog.DiscardHandler)).Read()
		if err != nil {
			t.Fatalf("read client info: %v", err)
		}
		if sendErr := <-errCh; sendErr != nil {
			t.Fatalf("sendClientInfo: %v", sendErr)
		}

		dt, err := x224.DecodeDataTPDU(raw)
		if err != nil {
			t.Fatalf("x224: %v", err)
		}
		// Strip MCS SendDataRequest (8 bytes) + basic security header (4 bytes).
		if len(dt) < 12+8 {
			t.Fatalf("client info PDU too short: %d", len(dt))
		}
		info := dt[8+4:]
		return binary.LittleEndian.Uint32(info[4:8])
	}

	if flags := readFlags(false); flags&sec.InfoAudioCapture != 0 {
		t.Fatalf("AudioInput=nil set InfoAudioCapture: %#x", flags)
	}
	if flags := readFlags(true); flags&sec.InfoAudioCapture == 0 {
		t.Fatalf("AudioInput!=nil did not set InfoAudioCapture: %#x", flags)
	}
}

func TestAudioInputDVCStreamsCapturedPCM(t *testing.T) {
	pcmIn, pcmOut := io.Pipe()
	defer pcmOut.Close()

	c := &Client{
		opts: &Options{
			AudioInput: func(rate uint32, ch uint16, frames uint32) (io.ReadCloser, error) {
				if rate != 16000 || ch != 1 || frames != 4 {
					t.Errorf("opened (%d, %d, %d), want (16000, 1, 4)", rate, ch, frames)
				}
				return pcmIn, nil
			},
			Logger: slog.New(slog.DiscardHandler),
		},
		log: slog.New(slog.DiscardHandler),
	}

	sentCh := make(chan []byte, 32)
	c.dvc = dvc.NewHandler(func(data []byte) error {
		sentCh <- append([]byte(nil), data...)
		return nil
	}, slog.New(slog.DiscardHandler), vcChunkSize)

	h := audin.NewHandler(nil, c.opts.AudioInput, slog.New(slog.DiscardHandler))
	c.audinHandler.Store(h)
	c.dvc.OnChannel(func(name string, ch *dvc.DynChannel) {
		if name == audin.ChannelName {
			channels := c.dvc
			h.SetSendFn(func(pdu []byte) error { return channels.SendData(ch.ID, pdu) })
			ch.SetHandler(func(data []byte) { h.ProcessPDU(data) })
			ch.OnClose(func() { h.Reset() })
		}
	})

	// Server creates AUDIO_INPUT dynamic channel (id=7).
	create := append([]byte{(dvc.CmdCreate << 4) | 0, 7}, []byte(audin.ChannelName+"\x00")...)
	c.dvc.ProcessPDU(create)
	resp := <-sentCh
	if resp[0]>>4 != dvc.CmdCreate || resp[1] != 7 || binary.LittleEndian.Uint32(resp[2:6]) != 0 {
		t.Fatalf("unexpected DVC create response: %v", resp)
	}

	wrapDVC := func(payload []byte) []byte {
		out := make([]byte, 2+len(payload))
		out[0] = (dvc.CmdData << 4) | 0
		out[1] = 7
		copy(out[2:], payload)
		return out
	}

	// Server sends MSG_SNDIN_FORMATS with one 16kHz mono 16-bit PCM format.
	formats := make([]byte, 1+8+18)
	formats[0] = 0x02
	binary.LittleEndian.PutUint32(formats[1:5], 1)
	binary.LittleEndian.PutUint32(formats[5:9], uint32(len(formats)))
	binary.LittleEndian.PutUint16(formats[9:], 1)      // PCM
	binary.LittleEndian.PutUint16(formats[11:], 1)     // mono
	binary.LittleEndian.PutUint32(formats[13:], 16000) // 16kHz
	binary.LittleEndian.PutUint32(formats[17:], 32000)
	binary.LittleEndian.PutUint16(formats[21:], 2)
	binary.LittleEndian.PutUint16(formats[23:], 16)
	c.dvc.ProcessPDU(wrapDVC(formats))

	// Expect DATA_INCOMING and MSG_SNDIN_FORMATS wrapped in DVC CmdData.
	p1 := <-sentCh
	p2 := <-sentCh
	if p1[2] != 0x05 || p2[2] != 0x02 {
		t.Fatalf("unexpected format reply PDUs: %v / %v", p1, p2)
	}

	// Server sends MSG_SNDIN_OPEN (4 frames, format index 0).
	openPDU := make([]byte, 9)
	openPDU[0] = 0x03
	binary.LittleEndian.PutUint32(openPDU[1:5], 4)
	binary.LittleEndian.PutUint32(openPDU[5:9], 0)
	c.dvc.ProcessPDU(wrapDVC(openPDU))

	fc := <-sentCh
	or := <-sentCh
	if fc[2] != 0x07 || or[2] != 0x04 || binary.LittleEndian.Uint32(or[3:7]) != 0 {
		t.Fatalf("unexpected open replies: %v / %v", fc, or)
	}

	// Write 8 bytes (4 mono 16-bit frames) from the fake mic and verify DVC output.
	want := []byte{10, 20, 30, 40, 50, 60, 70, 80}
	if _, err := pcmOut.Write(want); err != nil {
		t.Fatal(err)
	}
	inc := <-sentCh
	aud := <-sentCh
	if inc[2] != 0x05 || aud[2] != 0x06 || !bytes.Equal(aud[3:], want) {
		t.Fatalf("captured audio DVC PDUs = %v / %v, want %v", inc, aud, want)
	}

	// Ensure mcs.ChannelOptionInitialized compiles and is untouched.
	_ = mcs.ChannelOptionInitialized
	h.Close()
}
