package workbench

import (
	"encoding/binary"
	"time"
)

// encodeTerminalRecording writes the binary .mottyrec format earlier releases
// exported, so the reader that still opens those files has fixtures.
func encodeTerminalRecording(recording *terminalRecording) []byte {
	flags := uint16(0)
	if recording.truncated {
		flags = 1
	}
	data := make([]byte, 16, 16+recording.bytes+len(recording.events)*16)
	copy(data, terminalRecordingMagic[:])
	binary.LittleEndian.PutUint16(data[8:10], terminalRecordingVersion)
	binary.LittleEndian.PutUint16(data[10:12], flags)
	binary.LittleEndian.PutUint32(data[12:16], uint32(len(recording.events)))
	var header [16]byte
	for _, event := range recording.events {
		binary.LittleEndian.PutUint64(header[0:8], uint64(max(event.at, 0)/time.Microsecond))
		header[8] = byte(event.direction)
		header[9], header[10], header[11] = 0, 0, 0
		binary.LittleEndian.PutUint32(header[12:16], uint32(len(event.data)))
		data = append(data, header[:]...)
		data = append(data, event.data...)
	}
	return data
}
