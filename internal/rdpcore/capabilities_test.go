package rdp

import (
	"encoding/binary"
	"errors"
	"net"
	"strings"
	"testing"
	"unicode/utf16"

	"motty/internal/rdpcore/protocol/mcs"
	"motty/internal/rdpcore/protocol/pdu"
	"motty/internal/rdpcore/protocol/tpkt"
	"motty/internal/rdpcore/protocol/x224"
)

const testShareID uint32 = 0x000103EA

func capabilitiesClient(t *testing.T) *Client {
	t.Helper()
	opts := DefaultOptions()
	opts.Host = "example"
	client, err := NewClient(opts)
	if err != nil {
		t.Fatal(err)
	}
	client.ioChannelID = 1003
	return client
}

// shareControl wraps a PDU body in a Share Control Header.
func shareControl(pduType uint16, body []byte) []byte {
	buf := make([]byte, 6+len(body))
	binary.LittleEndian.PutUint16(buf[0:2], uint16(len(buf)))
	binary.LittleEndian.PutUint16(buf[2:4], pduType)
	binary.LittleEndian.PutUint16(buf[4:6], 1003)
	copy(buf[6:], body)
	return buf
}

func demandActivePDU() []byte {
	body := make([]byte, 12) // shareID, lengths, capability counts, session ID
	binary.LittleEndian.PutUint32(body[0:4], testShareID)
	return shareControl(pdu.TypeDemandActive, body)
}

func deactivateAllPDU() []byte {
	return shareControl(pdu.TypeDeactivateAll, nil)
}

func errorInfoPDU(code uint32) []byte {
	payload := make([]byte, 4)
	binary.LittleEndian.PutUint32(payload, code)
	return pdu.EncodeShareDataPDU(testShareID, pdu.PDUType2SetErrorInfo, 1003, payload)
}

func saveSessionInfoPDU() []byte {
	payload := make([]byte, 4) // INFOTYPE_LOGON
	return pdu.EncodeShareDataPDU(testShareID, pdu.PDUType2SaveSessionInfo, 1003, payload)
}

func redirectPDU() []byte {
	var body []byte
	body = append(body, 0, 0)       // padding
	body = append(body, 0x00, 0x04) // redirectIdentifier
	body = append(body, 0, 0)       // totalLength
	body = binary.LittleEndian.AppendUint32(body, 0)
	body = binary.LittleEndian.AppendUint32(body, pdu.LBLoadBalanceInfo|pdu.LBUsername)

	token := []byte("routing-token")
	body = binary.LittleEndian.AppendUint32(body, uint32(len(token)))
	body = append(body, token...)

	name := utf16.Encode([]rune("alice\x00"))
	body = binary.LittleEndian.AppendUint32(body, uint32(2*len(name)))
	for _, unit := range name {
		body = binary.LittleEndian.AppendUint16(body, unit)
	}

	return shareControl(pdu.TypeEnhancedRedirect, body)
}

// A server may send a Deactivate All, the logon result, or an error before the
// Demand Active. GNOME Remote Desktop starts the capabilities exchange with a
// Deactivate All; reference clients wait for the Demand Active after these.
func TestCapabilitiesExchangeSkipsPDUsBeforeDemandActive(t *testing.T) {
	client := capabilitiesClient(t)

	for _, pduData := range []struct {
		name string
		data []byte
	}{
		{"Deactivate All", deactivateAllPDU()},
		{"Save Session Info", saveSessionInfoPDU()},
		{"Set Error Info without a code", errorInfoPDU(0)},
	} {
		t.Run(pduData.name, func(t *testing.T) {
			da, err := client.capabilitiesPDU(client.ioChannelID, pduData.data)
			if err != nil {
				t.Fatalf("capabilities exchange rejected %s: %v", pduData.name, err)
			}
			if da != nil {
				t.Fatalf("capabilities exchange returned a Demand Active for %s", pduData.name)
			}
		})
	}

	da, err := client.capabilitiesPDU(client.ioChannelID, demandActivePDU())
	if err != nil {
		t.Fatalf("Demand Active rejected: %v", err)
	}
	if da == nil || da.ShareID != testShareID {
		t.Fatalf("Demand Active = %+v, want share ID %#x", da, testShareID)
	}
}

// A Set Error Info PDU carries the reason the server stopped the connection;
// dropping it would hide that reason behind a protocol error.
func TestCapabilitiesExchangeReportsServerErrorInfo(t *testing.T) {
	client := capabilitiesClient(t)

	_, err := client.capabilitiesPDU(client.ioChannelID, errorInfoPDU(0x00000007))
	if !errors.Is(err, ErrCapabilitiesFailed) {
		t.Fatalf("error = %v, want a capabilities failure", err)
	}
	if want := "ERRINFO_SERVER_DENIED_CONNECTION"; !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want it to name %s", err, want)
	}
}

// Only the PDUs the connection sequence allows may be skipped: an update or
// any other share PDU before activation means the server is broken, and the
// connection must not pretend otherwise.
func TestCapabilitiesExchangeRejectsOtherDataPDUs(t *testing.T) {
	client := capabilitiesClient(t)

	update := pdu.EncodeShareDataPDU(testShareID, pdu.PDUType2Update, 1003, []byte{0, 0})
	if _, err := client.capabilitiesPDU(client.ioChannelID, update); !errors.Is(err, ErrCapabilitiesFailed) {
		t.Fatalf("update before the Demand Active: error = %v, want a capabilities failure", err)
	}

	unexpected := shareControl(pdu.TypeConfirmActive, make([]byte, 12))
	if _, err := client.capabilitiesPDU(client.ioChannelID, unexpected); !errors.Is(err, ErrCapabilitiesFailed) {
		t.Fatalf("unexpected share PDU: error = %v, want a capabilities failure", err)
	}

	if _, err := client.capabilitiesPDU(client.ioChannelID+1, demandActivePDU()); !errors.Is(err, ErrCapabilitiesFailed) {
		t.Fatalf("Demand Active on another channel: error = %v, want a capabilities failure", err)
	}
}

// A redirection before the Demand Active is a reconnection request, not a
// protocol error: the caller needs the target and routing token from it.
func TestCapabilitiesExchangeSurfacesRedirection(t *testing.T) {
	client := capabilitiesClient(t)

	_, err := client.capabilitiesPDU(client.ioChannelID, redirectPDU())
	var redirect *RedirectError
	if !errors.As(err, &redirect) {
		t.Fatalf("error = %v, want a server redirection", err)
	}
	if got := string(redirect.Info.LoadBalanceInfo); got != "routing-token" {
		t.Fatalf("routing token = %q, want %q", got, "routing-token")
	}
	if redirect.Info.Username != "alice" {
		t.Fatalf("redirect user = %q, want alice", redirect.Info.Username)
	}
}

// sendDataFrame wraps userData in the MCS Send Data Indication and X.224 Data
// framing that carries slow-path PDUs on the wire.
func sendDataFrame(channelID uint16, userData []byte) []byte {
	mcsData := make([]byte, 7+len(userData))
	mcsData[0] = mcs.DomainMCSPDUSendDataIndication
	binary.BigEndian.PutUint16(mcsData[1:3], 2) // initiator
	binary.BigEndian.PutUint16(mcsData[3:5], channelID)
	mcsData[5] = 0x70 // priority high, begin and end
	mcsData[6] = byte(len(userData))
	copy(mcsData[7:], userData)
	return x224.EncodeDataTPDU(mcsData)
}

// readDemandActive must reach the Demand Active even when the server sends
// other PDUs first: this is the sequence GNOME Remote Desktop produces, and
// refusing it is what made the connection fail with "expected Demand Active
// (0x0011), got 0x0017".
func TestReadDemandActiveAfterOutOfSequencePDUs(t *testing.T) {
	for _, servers := range []struct {
		name string
		pdus [][]byte
	}{
		{"Deactivate All", [][]byte{deactivateAllPDU(), demandActivePDU()}},
		{"Save Session Info", [][]byte{saveSessionInfoPDU(), demandActivePDU()}},
		{"both", [][]byte{deactivateAllPDU(), saveSessionInfoPDU(), demandActivePDU()}},
	} {
		t.Run(servers.name, func(t *testing.T) {
			clientSide, serverSide := net.Pipe()
			t.Cleanup(func() { clientSide.Close() })

			client := capabilitiesClient(t)
			client.conn = clientSide
			client.tpktConn = tpkt.NewConn(clientSide, discardLogger)

			go func() {
				writer := tpkt.NewConn(serverSide, discardLogger)
				for _, one := range servers.pdus {
					if err := writer.Write(sendDataFrame(client.ioChannelID, one)); err != nil {
						serverSide.Close()
						return
					}
				}
				serverSide.Close()
			}()

			da, err := client.readDemandActive()
			if err != nil {
				t.Fatalf("readDemandActive: %v", err)
			}
			if da == nil || da.ShareID != testShareID {
				t.Fatalf("Demand Active = %+v, want share ID %#x", da, testShareID)
			}
		})
	}
}
