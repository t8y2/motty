package mcs

import (
	"encoding/binary"
	"log/slog"
	"testing"
)

func TestServerMessageChannelIsNegotiatedSeparatelyFromGraphicsAndStaticChannels(t *testing.T) {
	netData := writeBlockHeader(ServerNetworkDataType, []byte{0xEB, 3, 1, 0, 0xEC, 3})
	message := writeBlockHeader(0x0C04, []byte{0xED, 3})
	for _, data := range [][]byte{append(append([]byte(nil), netData...), message...), append(append([]byte(nil), message...), netData...)} {
		_, _, network, err := DecodeServerData(slog.Default(), data)
		if err != nil || network.MessageChannelID != 1005 || network.IOChannelID != 1003 || len(network.ChannelIDs) != 1 || network.ChannelIDs[0] != 1004 {
			t.Fatalf("negotiated channels = %+v, %v", network, err)
		}
	}
	for _, bad := range [][]byte{
		writeBlockHeader(0x0C04, []byte{1}), message[:5],
		writeBlockHeader(0x0C04, []byte{0xEB, 3}), writeBlockHeader(0x0C04, []byte{0xEC, 3}),
	} {
		_, _, _, err := DecodeServerData(slog.Default(), append(append([]byte(nil), netData...), bad...))
		if err == nil {
			t.Fatalf("accepted invalid message channel %x (id=%d)", bad, binary.LittleEndian.Uint16(append(bad[4:], 0, 0)))
		}
	}
}
