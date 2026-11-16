package rdp

import (
	"encoding/binary"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"motty/internal/rdpcore/protocol/mcs"
	"motty/internal/rdpcore/protocol/sec"
	"motty/internal/rdpcore/protocol/tpkt"
	"motty/internal/rdpcore/protocol/x224"
)

func autoRequest(kind uint16, body ...uint32) []byte {
	data := make([]byte, 6+4*len(body))
	data[0] = byte(len(data))
	binary.LittleEndian.PutUint16(data[2:4], 42)
	binary.LittleEndian.PutUint16(data[4:6], kind)
	for i, value := range body {
		binary.LittleEndian.PutUint32(data[6+4*i:], value)
	}
	return data
}

func TestNetworkAutodetectRTTAndBandwidth(t *testing.T) {
	var a networkAutodetect
	now := time.Now()
	for _, kind := range []uint16{0x0001, 0x1001} {
		reply, err := a.request(autoRequest(kind), now)
		if err != nil || len(reply) != 6 || reply[1] != 1 ||
			binary.LittleEndian.Uint16(reply[2:4]) != 42 || binary.LittleEndian.Uint16(reply[4:6]) != 0 {
			t.Fatalf("RTT response: %x, %v", reply, err)
		}
	}
	if _, err := a.request(autoRequest(0x1014), now); err != nil {
		t.Fatal(err)
	}
	payload := []byte{8, 0, 42, 0, 2, 0, 4, 0, 1, 2, 3, 4}
	if _, err := a.request(payload, now); err != nil {
		t.Fatal(err)
	}
	stop := []byte{8, 0, 42, 0, 0x2B, 0, 4, 0, 5, 6, 7, 8}
	reply, err := a.request(stop, now.Add(50*time.Millisecond))
	if err != nil || len(reply) != 14 || binary.LittleEndian.Uint16(reply[4:6]) != 3 ||
		binary.LittleEndian.Uint32(reply[6:10]) != 50 || binary.LittleEndian.Uint32(reply[10:14]) != 24 {
		t.Fatalf("bandwidth result: %x, %v", reply, err)
	}
	for _, kind := range []uint16{0x0840, 0x0880, 0x08C0} {
		values := []uint32{10, 73}
		if kind == 0x08C0 {
			values = []uint32{10, 999, 73}
		}
		if _, err := a.request(autoRequest(kind, values...), now); err != nil {
			t.Fatal(err)
		}
		if a.rtt != 73*time.Millisecond || a.sampled != now {
			t.Fatalf("network RTT = %v at %v", a.rtt, a.sampled)
		}
	}
}

func TestNetworkAutodetectRejectsTruncationAndInvalidPayloads(t *testing.T) {
	for _, data := range [][]byte{
		autoRequest(0x1001), autoRequest(0x0840, 1, 12), autoRequest(0x08C0, 1, 2, 12),
	} {
		for length := 0; length < len(data); length++ {
			var a networkAutodetect
			if _, err := a.request(data[:length], time.Now()); err == nil {
				t.Fatalf("accepted truncated %x", data[:length])
			}
		}
	}
	for _, data := range [][]byte{
		{8, 0, 0, 0, 2, 0, 255, 255}, autoRequest(0x0429),
		{8, 0, 42, 0, 0x2B, 0, 0, 0},
		{7, 0, 0, 0, 1, 0}, {6, 1, 0, 0, 1, 0}, autoRequest(0xFFFF),
	} {
		var a networkAutodetect
		if _, err := a.request(data, time.Now()); err == nil {
			t.Fatalf("accepted invalid %x", data)
		}
	}
}

// Exercise the actual MCS message channel over loopback, with split wire input.
func TestNetworkAutodetectInterleavedBeforeLicensing(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	clientConn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer clientConn.Close()
	serverConn, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer serverConn.Close()
	clientConn.SetDeadline(time.Now().Add(3 * time.Second))
	serverConn.SetDeadline(time.Now().Add(3 * time.Second))
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	c := &Client{userChannelID: 1002, ioChannelID: 1003, messageChannelID: 1005, logMcs: log, tpktConn: tpkt.NewConn(clientConn, log)}
	notified := 0
	c.OnNetworkRTTChanged(func() { notified++ })
	done := make(chan error, 1)
	go func() {
		send := func(channel uint16, flags uint16, payload []byte) error {
			wrapped := append(sec.EncodeBasicSecurityHeader(sec.BasicSecurityHeader{Flags: flags}), payload...)
			data := mcs.EncodeSendDataRequest(1002, channel, wrapped)
			data[0] = mcs.DomainMCSPDUSendDataIndication
			packet := x224.EncodeDataTPDU(data)
			wire := append([]byte{3, 0, byte((len(packet) + 4) >> 8), byte(len(packet) + 4)}, packet...)
			for _, b := range wire {
				if _, err := serverConn.Write([]byte{b}); err != nil {
					return err
				}
			}
			return nil
		}
		if err := send(1005, sec.AutodetectReq, autoRequest(0x1001)); err != nil {
			done <- err
			return
		}
		server := tpkt.NewConn(serverConn, log)
		reply, err := server.Read()
		if err != nil {
			done <- err
			return
		}
		payload, err := x224.DecodeDataTPDU(reply)
		if err == nil {
			payload[0] = mcs.DomainMCSPDUSendDataIndication
			var channel uint16
			channel, payload, err = mcs.DecodeSendDataIndication(log, payload)
			if err == nil && (channel != 1005 || len(payload) != 10 || binary.LittleEndian.Uint16(payload) != sec.AutodetectRsp) {
				err = io.ErrUnexpectedEOF
			}
		}
		if err == nil {
			err = send(1005, sec.AutodetectReq, autoRequest(0x0840, 10, 85))
		}
		if err == nil {
			err = send(1003, sec.LicensePkt, []byte{0xFF, 3, 4, 0})
		}
		done <- err
	}()
	channel, data, err := c.readLicensingDataPDU()
	if err != nil || channel != 1003 || len(data) != 8 {
		t.Fatalf("licensing data = %d %x: %v", channel, data, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	rtt, sampled := c.NetworkRTT()
	if notified != 1 || rtt != 85*time.Millisecond || sampled.IsZero() {
		t.Fatalf("RTT %v, sampled %v, notifications %d", rtt, sampled, notified)
	}
}

func FuzzNetworkAutodetect(f *testing.F) {
	f.Add(autoRequest(0x1001))
	f.Add(autoRequest(0x0840, 10, 85))
	f.Fuzz(func(t *testing.T, data []byte) {
		var a networkAutodetect
		_, _ = a.request(data, time.Now())
	})
}

func BenchmarkNetworkAutodetectResult(b *testing.B) {
	var a networkAutodetect
	data, now := autoRequest(0x0840, 10, 85), time.Now()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := a.request(data, now); err != nil {
			b.Fatal(err)
		}
	}
}
