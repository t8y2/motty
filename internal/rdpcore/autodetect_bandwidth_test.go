package rdp

import (
	"encoding/binary"
	"fmt"
	"net"
	"testing"
	"time"

	"motty/internal/rdpcore/protocol/fastpath"
	"motty/internal/rdpcore/protocol/mcs"
	"motty/internal/rdpcore/protocol/pdu"
	"motty/internal/rdpcore/protocol/sec"
	"motty/internal/rdpcore/protocol/tpkt"
	"motty/internal/rdpcore/protocol/x224"
)

func TestContinuousBandwidthCountsOrdinaryPDUsDuringFinalizationAndReceive(t *testing.T) {
	for _, connected := range []bool{false, true} {
		t.Run(fmt.Sprintf("connected=%v", connected), func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			conn, err := net.Dial("tcp", listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			serverConn, err := listener.Accept()
			if err != nil {
				t.Fatal(err)
			}
			defer serverConn.Close()
			conn.SetDeadline(time.Now().Add(3 * time.Second))
			serverConn.SetDeadline(time.Now().Add(3 * time.Second))
			opts := DefaultOptions()
			opts.Host, opts.HeartbeatTimeout = "127.0.0.1", 0
			c, err := NewClient(opts)
			if err != nil {
				t.Fatal(err)
			}
			c.conn, c.tpktConn = conn, tpkt.NewConn(conn, discardLogger)
			c.userChannelID, c.ioChannelID, c.messageChannelID = 1002, 1003, 1005
			c.channelMap = map[uint16]string{1004: "test"}
			c.channelHandlers = map[string]func(uint16, []byte){"test": func(uint16, []byte) {}}
			c.vcReassembly = make(map[uint16][]byte)
			c.receiveLoopDone = make(chan struct{})
			slow := pdu.EncodeShareDataPDU(1, pdu.PDUType2Synchronize, 1002, pdu.EncodeSynchronize(1002))
			fast := buildUpdate(fastpath.UpdateBitmap, fastpath.FragSingle, 0, buildBitmapPayload([]byte{0xAA, 0xBB}))
			virtual := []byte{3, 0, 0, 0, 3, 0, 0, 0, 1, 2, 3}
			stop := autoRequest(0x0429)
			want := len(slow) + len(fast) + len(virtual) + len(stop)
			serverDone := make(chan error, 1)
			go func() {
				send := func(channel, flags uint16, payload []byte) error {
					if flags != 0 {
						payload = append(sec.EncodeBasicSecurityHeader(sec.BasicSecurityHeader{Flags: flags}), payload...)
					}
					data := mcs.EncodeSendDataRequest(1002, channel, payload)
					data[0] = mcs.DomainMCSPDUSendDataIndication
					packet := x224.EncodeDataTPDU(data)
					return tpkt.NewConn(serverConn, discardLogger).Write(packet)
				}
				run := func() error {
					if err := send(1005, sec.AutodetectReq, autoRequest(0x0014)); err != nil {
						return err
					}
					if err := send(1003, 0, slow); err != nil {
						return err
					}
					wire := append([]byte{0, byte(len(fast) + 2)}, fast...)
					for _, b := range wire {
						if _, err := serverConn.Write([]byte{b}); err != nil {
							return err
						}
					}
					if err := send(1004, 0, virtual); err != nil {
						return err
					}
					if err := send(1005, sec.AutodetectReq, stop); err != nil {
						return err
					}
					reply, err := tpkt.NewConn(serverConn, discardLogger).Read()
					if err != nil {
						return err
					}
					data, err := x224.DecodeDataTPDU(reply)
					if err != nil {
						return err
					}
					data[0] = mcs.DomainMCSPDUSendDataIndication
					channel, data, err := mcs.DecodeSendDataIndication(discardLogger, data)
					if err != nil {
						return err
					}
					if channel != 1005 || len(data) != 18 || binary.LittleEndian.Uint16(data) != sec.AutodetectRsp ||
						binary.LittleEndian.Uint16(data[8:10]) != 0x000B || int(binary.LittleEndian.Uint32(data[14:18])) != want {
						return fmt.Errorf("bandwidth response %x on channel %d, want %d bytes excluding security headers", data, channel, want)
					}
					return send(1003, 0, slow)
				}
				serverDone <- run()
				serverConn.Close()
			}()
			if connected {
				go c.receiveLoop()
			} else {
				// Ordinary packets return to finalization; measurement packets
				// and the interleaved fast-path update are dispatched inline.
				for range 3 {
					if _, _, err := c.readDecryptedPDU(); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := <-serverDone; err != nil {
				t.Fatal(err)
			}
			if connected {
				select {
				case <-c.receiveLoopDone:
				case <-time.After(3 * time.Second):
					t.Fatal("receive loop did not stop")
				}
			}
		})
	}
}

func TestBandwidthRestartStopAndOverflow(t *testing.T) {
	var a networkAutodetect
	now := time.Now()
	start := autoRequest(0x0014)
	if _, err := a.request(start, now); err != nil {
		t.Fatal(err)
	}
	if err := a.received(100); err != nil {
		t.Fatal(err)
	}
	if _, err := a.request(start, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := a.received(37); err != nil {
		t.Fatal(err)
	}
	reply, err := a.request(autoRequest(0x0429), now.Add(2*time.Second))
	if err != nil || len(reply) != 14 || binary.LittleEndian.Uint32(reply[10:14]) != 37 {
		t.Fatalf("restart retained old bytes: %x, %v", reply, err)
	}
	if err := a.received(500); err != nil || a.bytes != 37 {
		t.Fatalf("stopped measurement counted data: %d, %v", a.bytes, err)
	}
	if _, err := a.request(start, now); err != nil {
		t.Fatal(err)
	}
	a.bytes = ^uint32(0)
	if err := a.received(1); err == nil || a.bytes != ^uint32(0) {
		t.Fatalf("bandwidth overflow was not rejected: %d, %v", a.bytes, err)
	}
	if _, err := a.request(autoRequest(0x1014), now); err != nil {
		t.Fatal(err)
	}
	if err := a.received(500); err != nil || a.bytes != 0 {
		t.Fatalf("connect-time measurement counted ordinary data: %d, %v", a.bytes, err)
	}
}
