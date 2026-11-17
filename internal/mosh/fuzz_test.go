package mosh

import (
	"testing"
	"time"
)

// A server is not trusted beyond the shared key. These fuzz targets keep the
// decoders total: any length, field order or truncation must be rejected
// without a panic or an unbounded allocation.

func FuzzUnmarshalFragment(f *testing.F) {
	f.Add([]byte{})
	f.Add(make([]byte, fragmentHeaderSize))
	f.Add([]byte{0, 0, 0, 0, 0, 0, 0, 1, 0x80, 0, 'x'})
	f.Fuzz(func(t *testing.T, data []byte) {
		fragment, err := UnmarshalFragment(data)
		if err == nil && len(fragment.Payload) > len(data) {
			t.Fatalf("payload %d > input %d", len(fragment.Payload), len(data))
		}
	})
}

func FuzzTransportInstructionUnmarshal(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0x10, 0x02})
	f.Add([]byte{0x32, 0x04, 'x', 'x', 'x', 'x'})
	f.Fuzz(func(t *testing.T, data []byte) {
		var instruction TransportInstruction
		_ = instruction.Unmarshal(data)
	})
}

func FuzzHostMessageUnmarshal(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0x0a, 0x03, 0x22, 0x01, 'x'})
	f.Fuzz(func(t *testing.T, data []byte) {
		instructions, err := UnmarshalHostMessage(data)
		if err != nil {
			return
		}
		for _, instruction := range instructions {
			if instruction.EchoAckNum < -1 {
				t.Fatalf("negative echo ack %d", instruction.EchoAckNum)
			}
		}
	})
}

func FuzzUserMessageUnmarshal(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0x0a, 0x03, 0x22, 0x01, 'x'})
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = unmarshalUserMessage(data)
	})
}

func FuzzTransportRecv(f *testing.F) {
	ocb, err := NewOCB(make([]byte, 16))
	if err != nil {
		f.Fatal(err)
	}
	transport := NewTransport(ocb, false, newByteState())
	now := time.Unix(1_700_000_000, 0)
	f.Add([]byte{})
	f.Add(make([]byte, minDatagram))
	f.Add([]byte{
		0x80, 0, 0, 0, 0, 0, 0, 1,
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	})
	f.Fuzz(func(t *testing.T, data []byte) {
		_ = transport.Recv(data, now)
	})
}
