package rdp

import (
	"encoding/binary"
	"testing"
	"time"

	"motty/internal/rdpcore/protocol/mcs"
	"motty/internal/rdpcore/protocol/tpkt"
	"motty/internal/rdpcore/protocol/x224"
)

func TestDispatchPacketDoesNotHoldFrameMuForVirtualChannels(t *testing.T) {
	c := &Client{
		log:             discardLogger,
		logX224:         discardLogger,
		logMcs:          discardLogger,
		channelMap:      map[uint16]string{1005: "rdpsnd"},
		channelHandlers: make(map[string]func(uint16, []byte)),
		framebuf:        NewFramebuffer(10, 10),
		vcReassembly:    make(map[uint16][]byte),
	}

	handlerRan := false
	readRectCompletedWhileHandlerRan := false

	c.channelHandlers["rdpsnd"] = func(channelID uint16, data []byte) {
		handlerRan = true
		// Verify that ReadRectBGRA can acquire RLock and complete without deadlock
		// or contention while rdpsnd handler runs.
		buf := make([]byte, 10*10*4)
		done := make(chan struct{})
		go func() {
			c.ReadRectBGRA(buf, 0, 0, 10, 10, 10, func(x, y, w, h int, pix []byte, stride int) {})
			close(done)
		}()

		select {
		case <-done:
			readRectCompletedWhileHandlerRan = true
		case <-time.After(100 * time.Millisecond):
			t.Error("ReadRectBGRA blocked while rdpsnd handler was running (frameMu was held)")
		}
	}

	// Build a virtual channel packet:
	// CHANNEL_PDU_HEADER: totalLength(4) + flags(4) (0x03 = first | last)
	vcPayload := make([]byte, 8+4)
	binary.LittleEndian.PutUint32(vcPayload[0:4], 4)
	binary.LittleEndian.PutUint32(vcPayload[4:8], 0x00000003) // channelFlagFirst | channelFlagLast
	copy(vcPayload[8:], []byte{1, 2, 3, 4})

	// Wrap in MCS SendDataIndication PDU
	mcsBuf := make([]byte, 7+len(vcPayload))
	mcsBuf[0] = mcs.DomainMCSPDUSendDataIndication
	binary.BigEndian.PutUint16(mcsBuf[1:3], 0)
	binary.BigEndian.PutUint16(mcsBuf[3:5], 1005) // channelID = 1005 (rdpsnd)
	mcsBuf[5] = 0x70
	mcsBuf[6] = byte(len(vcPayload))
	copy(mcsBuf[7:], vcPayload)

	// Wrap in X.224 Data TPDU
	x224Buf := x224.EncodeDataTPDU(mcsBuf)

	ended := c.dispatchPacket(tpkt.PacketTPKT, 0, x224Buf)
	if ended {
		t.Fatal("dispatchPacket returned ended = true")
	}
	if !handlerRan {
		t.Fatal("rdpsnd handler was not called")
	}
	if !readRectCompletedWhileHandlerRan {
		t.Fatal("ReadRectBGRA did not complete while rdpsnd handler was running")
	}
}
