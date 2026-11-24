package rdp

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"

	"motty/internal/desktop"
	rdpcore "motty/internal/rdpcore"
	"motty/internal/rdpcore/protocol/mcs"
)

// A server refuses a scale that is not one Windows offers, and the library
// refuses to connect with one: whatever a display reports must land on a level.
func TestScalePercentIsAlwaysALevelWindowsOffers(t *testing.T) {
	for scale, want := range map[float32]uint32{0: 100, 1: 100, 1.1: 100, 1.25: 125, 1.33: 125, 1.5: 150, 1.75: 175, 2: 200, 2.2: 225, 3: 300, 9: 500} {
		got := scalePercent(scale)
		if got != want {
			t.Errorf("scalePercent(%v) = %d, want %d", scale, got, want)
		}
		options := rdpcore.DefaultOptions()
		options.Host, options.DesktopScaleFactor = "example.com", got
		if err := options.Validate(); err != nil {
			t.Errorf("scale %v gives %d, which the library refuses: %v", scale, got, err)
		}
	}
}

// Even on a 100% display, the MCS Connect Initial handshake must send the
// optional DPI fields (desktopScaleFactor = 100, deviceScaleFactor = 100, and
// 96-DPI physical dimensions). Omitting them leaves a reconnected Windows
// session at whatever scale a previous HiDPI client set, or lets Windows pick a
// default scale for the resolution before the display control channel opens.
func TestOpenAlwaysSendsDesktopScaleFactorInHandshake(t *testing.T) {
	for _, test := range []struct {
		scale                float32
		width, height        int
		wantScale            uint32
		wantPhysW, wantPhysH uint32
	}{
		{0, 1920, 1080, 100, 508, 286},
		{1, 1920, 1080, 100, 508, 286},
		{1.5, 1920, 1080, 150, 339, 191},
		{2, 2560, 1440, 200, 339, 191},
	} {
		var coreBlock []byte
		dial := func(context.Context, string, string) (net.Conn, error) {
			clientConn, serverConn := net.Pipe()
			go func() {
				defer serverConn.Close()
				// Read X.224 Connection Request
				var hdr [4]byte
				if _, err := io.ReadFull(serverConn, hdr[:]); err != nil {
					return
				}
				crLen := int(binary.BigEndian.Uint16(hdr[2:4])) - 4
				if crLen <= 0 {
					return
				}
				if _, err := io.ReadFull(serverConn, make([]byte, crLen)); err != nil {
					return
				}
				// Reply with X.224 Connection Confirm selecting Standard RDP Security (0)
				_, _ = serverConn.Write([]byte{3, 0, 0, 19, 14, 0xd0, 0, 0, 0, 0, 0, 2, 0, 8, 0, 0, 0, 0, 0})
				// Read MCS Connect Initial TPKT
				if _, err := io.ReadFull(serverConn, hdr[:]); err != nil {
					return
				}
				mcsLen := int(binary.BigEndian.Uint16(hdr[2:4])) - 4
				if mcsLen <= 0 {
					return
				}
				payload := make([]byte, mcsLen)
				if _, err := io.ReadFull(serverConn, payload); err != nil {
					return
				}
				coreBlock = findClientCoreBlock(payload)
				// Send an invalid MCS Connect Response so the client exits mcsConnect
				// only after we have read the full MCS Connect Initial.
				_, _ = serverConn.Write([]byte{3, 0, 0, 8, 2, 0xf0, 0x80, 0})
			}()
			return clientConn, nil
		}
		_, err := Open(context.Background(), dial, "127.0.0.1:3389", desktop.Options{
			Username:         "alice",
			Password:         "secret",
			StandardSecurity: true,
			Width:            test.width,
			Height:           test.height,
			Scale:            test.scale,
		})
		if err == nil {
			t.Fatal("expected handshake to stop after synthetic server closed")
		}
		// TS_UD_CS_CORE with DPI fields is 4 (header) + 212 (fixed) + 18 (DPI) = 234 bytes.
		if len(coreBlock) != 234 {
			t.Fatalf("scale %v: TS_UD_CS_CORE block len = %d, want 234 (with optional DPI fields)", test.scale, len(coreBlock))
		}
		data := coreBlock[4:]
		physW := binary.LittleEndian.Uint32(data[212:216])
		physH := binary.LittleEndian.Uint32(data[216:220])
		orient := binary.LittleEndian.Uint16(data[220:222])
		desktopScale := binary.LittleEndian.Uint32(data[222:226])
		deviceScale := binary.LittleEndian.Uint32(data[226:230])
		if desktopScale != test.wantScale || deviceScale != 100 || orient != 0 {
			t.Errorf("scale %v: got desktopScale=%d deviceScale=%d orient=%d, want %d/100/0",
				test.scale, desktopScale, deviceScale, orient, test.wantScale)
		}
		if physW != test.wantPhysW || physH != test.wantPhysH {
			t.Errorf("scale %v: got physical %dx%d mm, want %dx%d mm",
				test.scale, physW, physH, test.wantPhysW, test.wantPhysH)
		}
	}
}

func findClientCoreBlock(pdu []byte) []byte {
	duca := []byte("Duca")
	idx := bytes.Index(pdu, duca)
	if idx < 0 {
		return nil
	}
	// After "Duca", PER octet string length determinant (1 or 2 bytes) precedes userData.
	pos := idx + len(duca)
	if pos >= len(pdu) {
		return nil
	}
	if pdu[pos]&0x80 != 0 {
		pos += 2
	} else {
		pos++
	}
	for pos+4 <= len(pdu) {
		blockType := binary.LittleEndian.Uint16(pdu[pos : pos+2])
		blockLen := int(binary.LittleEndian.Uint16(pdu[pos+2 : pos+4]))
		if blockLen < 4 || pos+blockLen > len(pdu) {
			return nil
		}
		if blockType == mcs.ClientCoreDataType {
			return append([]byte(nil), pdu[pos:pos+blockLen]...)
		}
		pos += blockLen
	}
	return nil
}

func TestRequestedSizeIsWhatTheDisplayChannelAccepts(t *testing.T) {
	for _, test := range []struct{ width, height, wantWidth, wantHeight int }{
		{1751, 1007, 1750, 1007},
		{10, 10, minSize, minSize},
		{1 << 20, 1 << 20, maxSize, maxSize},
	} {
		if width, height := clampSize(test.width, test.height); width != test.wantWidth || height != test.wantHeight {
			t.Errorf("clampSize(%d, %d) = %d, %d, want %d, %d", test.width, test.height, width, height, test.wantWidth, test.wantHeight)
		}
	}
}
