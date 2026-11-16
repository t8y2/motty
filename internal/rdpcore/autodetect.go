package rdp

import (
	"encoding/binary"
	"fmt"
	"sync"
	"time"

	"motty/internal/rdpcore/protocol/mcs"
	"motty/internal/rdpcore/protocol/sec"
	"motty/internal/rdpcore/protocol/x224"
)

// MS-RDPBCGR 2.2.14: the server measures RTT; the client answers requests
// and receives the server's network characteristics results. No client timer
// or synthetic graphics/input requests are needed.
type networkAutodetect struct {
	started    time.Time
	bytes      uint32
	continuous bool
	mu         sync.Mutex
	rtt        time.Duration
	sampled    time.Time
}

// received counts the PDU data after its security header, before compression
// expands it. Connect-time detection counts only explicit measurement payloads.
func (a *networkAutodetect) received(size int) error {
	if !a.continuous || a.started.IsZero() {
		return nil
	}
	if size < 0 || uint64(a.bytes)+uint64(size) > uint64(^uint32(0)) {
		return fmt.Errorf("rdp network detection: bandwidth byte count overflow")
	}
	a.bytes += uint32(size)
	return nil
}

// NetworkRTT returns the server's most recent average TCP RTT and the time
// it was reported, not an input-to-display latency estimate.
func (c *Client) NetworkRTT() (time.Duration, time.Time) {
	c.autodetect.mu.Lock()
	defer c.autodetect.mu.Unlock()
	return c.autodetect.rtt, c.autodetect.sampled
}

// OnNetworkRTTChanged registers a callback for a server-reported RTT sample.
func (c *Client) OnNetworkRTTChanged(fn func()) {
	c.cbMu.Lock()
	c.onNetworkRTT = fn
	c.cbMu.Unlock()
}

func (c *Client) readLicensingDataPDU() (uint16, []byte, error) {
	for {
		channel, data, err := c.readDataPDU()
		if err != nil || channel != c.messageChannelID || c.messageChannelID == 0 {
			return channel, data, err
		}
		if err := c.handleMessageChannel(data); err != nil {
			return 0, nil, err
		}
	}
}

func (c *Client) handleMessageChannel(data []byte) error {
	header, payload, err := sec.DecodeBasicSecurityHeader(data)
	if err != nil {
		return fmt.Errorf("rdp network detection: %w", err)
	}
	if header.Flags&sec.Encrypt != 0 {
		if c.crypto == nil {
			return fmt.Errorf("rdp network detection: encrypted packet without security context")
		}
		payload, err = c.crypto.Decrypt(payload)
		if err != nil {
			return fmt.Errorf("rdp network detection: %w", err)
		}
	}
	if header.Flags&sec.AutodetectReq == 0 {
		return fmt.Errorf("rdp network detection: unsupported security flags %#x", header.Flags)
	}
	if err := c.autodetect.received(len(payload)); err != nil {
		return err
	}
	reply, err := c.autodetect.request(payload, time.Now())
	if err != nil {
		return err
	}
	kind := binary.LittleEndian.Uint16(payload[4:6])
	if kind == 0x0840 || kind == 0x0880 || kind == 0x08C0 {
		c.cbMu.RLock()
		fn := c.onNetworkRTT
		c.cbMu.RUnlock()
		if fn != nil {
			fn()
		}
	}
	if reply == nil {
		return nil
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	var wrapped []byte
	if c.crypto != nil {
		wrapped = c.crypto.Encrypt(reply, sec.AutodetectRsp|sec.Encrypt)
	} else {
		wrapped = append(sec.EncodeBasicSecurityHeader(sec.BasicSecurityHeader{Flags: sec.AutodetectRsp}), reply...)
	}
	return c.tpktConn.Write(x224.EncodeDataTPDU(mcs.EncodeSendDataRequest(c.userChannelID, c.messageChannelID, wrapped)))
}

func (a *networkAutodetect) request(data []byte, now time.Time) ([]byte, error) {
	bad := func() ([]byte, error) { return nil, fmt.Errorf("rdp network detection: invalid request") }
	if len(data) < 6 || data[1] != 0 {
		return bad()
	}
	headerLength, kind := int(data[0]), binary.LittleEndian.Uint16(data[4:6])
	sequence := binary.LittleEndian.Uint16(data[2:4])
	response := func(kind uint16, size int) []byte {
		out := make([]byte, size)
		out[0], out[1] = byte(size), 1
		binary.LittleEndian.PutUint16(out[2:4], sequence)
		binary.LittleEndian.PutUint16(out[4:6], kind)
		return out
	}
	addPayload := func() bool {
		if a.continuous || headerLength != 8 || len(data) < 8 {
			return false
		}
		size := int(binary.LittleEndian.Uint16(data[6:8]))
		if size == 0 || len(data) != 8+size || a.started.IsZero() || uint64(a.bytes)+uint64(len(data)) > uint64(^uint32(0)) {
			return false
		}
		a.bytes += uint32(len(data))
		return true
	}
	switch kind {
	case 0x0001, 0x1001: // continuous / connect-time RTT request
		if headerLength != 6 || len(data) != 6 {
			return bad()
		}
		return response(0, 6), nil
	case 0x0014, 0x1014: // TCP bandwidth measurement start
		if headerLength != 6 || len(data) != 6 {
			return bad()
		}
		a.started, a.bytes = now, 0
		a.continuous = kind == 0x0014
	case 0x0002:
		if !addPayload() {
			return bad()
		}
	case 0x002B, 0x0429: // connect-time / continuous bandwidth stop
		if kind == 0x002B {
			if !addPayload() {
				return bad()
			}
		} else if headerLength != 6 || len(data) != 6 || a.started.IsZero() || !a.continuous {
			return bad()
		}
		ms := now.Sub(a.started).Milliseconds()
		if ms < 0 || ms > int64(^uint32(0)) {
			return bad()
		}
		responseKind := uint16(0x0003)
		if kind == 0x0429 {
			responseKind = 0x000B
		}
		out := response(responseKind, 14)
		binary.LittleEndian.PutUint32(out[6:10], uint32(ms))
		binary.LittleEndian.PutUint32(out[10:14], a.bytes)
		a.started = time.Time{}
		return out, nil
	case 0x0840, 0x0880, 0x08C0: // server's network characteristics
		size := 14
		if kind == 0x08C0 {
			size = 18
		}
		if headerLength != size || len(data) != size {
			return bad()
		}
		rtt := time.Duration(binary.LittleEndian.Uint32(data[size-4:])) * time.Millisecond
		a.mu.Lock()
		a.rtt, a.sampled = rtt, now
		a.mu.Unlock()
	default:
		return nil, fmt.Errorf("rdp network detection: unsupported request %#x", kind)
	}
	return nil, nil
}
