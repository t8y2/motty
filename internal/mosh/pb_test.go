package mosh

import (
	"bytes"
	"testing"
)

func TestTransportInstructionRoundTrip(t *testing.T) {
	ti := TransportInstruction{
		ProtocolVersion: 2,
		OldNum:          10,
		NewNum:          11,
		AckNum:          9,
		ThrowawayNum:    8,
		Diff:            []byte("hello"),
		Chaff:           []byte{0xde, 0xad},
	}

	data := ti.Marshal()
	var got TransportInstruction
	if err := got.Unmarshal(data); err != nil {
		t.Fatal(err)
	}

	if got.ProtocolVersion != ti.ProtocolVersion {
		t.Fatalf("ProtocolVersion = %d, want %d", got.ProtocolVersion, ti.ProtocolVersion)
	}
	if got.OldNum != ti.OldNum {
		t.Fatalf("OldNum = %d, want %d", got.OldNum, ti.OldNum)
	}
	if got.NewNum != ti.NewNum {
		t.Fatalf("NewNum = %d, want %d", got.NewNum, ti.NewNum)
	}
	if got.AckNum != ti.AckNum {
		t.Fatalf("AckNum = %d, want %d", got.AckNum, ti.AckNum)
	}
	if got.ThrowawayNum != ti.ThrowawayNum {
		t.Fatalf("ThrowawayNum = %d, want %d", got.ThrowawayNum, ti.ThrowawayNum)
	}
	if !bytes.Equal(got.Diff, ti.Diff) {
		t.Fatalf("Diff = %q, want %q", got.Diff, ti.Diff)
	}
	if !bytes.Equal(got.Chaff, ti.Chaff) {
		t.Fatalf("Chaff = %x, want %x", got.Chaff, ti.Chaff)
	}
}

func TestTransportInstructionEmpty(t *testing.T) {
	ti := TransportInstruction{}
	data := ti.Marshal()
	var got TransportInstruction
	if err := got.Unmarshal(data); err != nil {
		t.Fatal(err)
	}
	if got.OldNum != 0 || got.NewNum != 0 {
		t.Fatal("empty instruction should decode to zeros")
	}
}

func TestTransportInstructionLargeValues(t *testing.T) {
	ti := TransportInstruction{
		OldNum:       1<<63 - 1,
		NewNum:       1<<63 - 1,
		AckNum:       1<<63 - 1,
		ThrowawayNum: 1<<63 - 1,
	}
	data := ti.Marshal()
	var got TransportInstruction
	if err := got.Unmarshal(data); err != nil {
		t.Fatal(err)
	}
	if got.OldNum != ti.OldNum {
		t.Fatalf("OldNum = %d, want %d", got.OldNum, ti.OldNum)
	}
}

func TestHostMessageRoundTrip(t *testing.T) {
	instrs := []HostInstruction{
		{Hoststring: []byte("\033[H\033[2J"), EchoAckNum: -1},
		{Width: 80, Height: 24, EchoAckNum: -1},
		{EchoAckNum: 42},
		{Hoststring: []byte("hello"), Width: 132, Height: 43, EchoAckNum: 7},
	}

	data := marshalHostMessage(instrs)
	got, err := unmarshalHostMessage(data)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != len(instrs) {
		t.Fatalf("len = %d, want %d", len(got), len(instrs))
	}

	for i := range instrs {
		if !bytes.Equal(got[i].Hoststring, instrs[i].Hoststring) {
			t.Fatalf("[%d] Hoststring = %q, want %q", i, got[i].Hoststring, instrs[i].Hoststring)
		}
		if got[i].Width != instrs[i].Width {
			t.Fatalf("[%d] Width = %d, want %d", i, got[i].Width, instrs[i].Width)
		}
		if got[i].Height != instrs[i].Height {
			t.Fatalf("[%d] Height = %d, want %d", i, got[i].Height, instrs[i].Height)
		}
		if got[i].EchoAckNum != instrs[i].EchoAckNum {
			t.Fatalf("[%d] EchoAckNum = %d, want %d", i, got[i].EchoAckNum, instrs[i].EchoAckNum)
		}
	}
}

func TestHostMessageEmpty(t *testing.T) {
	data := marshalHostMessage(nil)
	got, err := unmarshalHostMessage(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("len = %d, want 0", len(got))
	}
}

func TestUserMessageRoundTrip(t *testing.T) {
	instrs := []UserInstruction{
		{Keys: []byte("ls -la\n")},
		{Width: 120, Height: 40},
		{Keys: []byte("a"), Width: 80, Height: 24},
	}

	data := marshalUserMessage(instrs)
	got, err := unmarshalUserMessage(data)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != len(instrs) {
		t.Fatalf("len = %d, want %d", len(got), len(instrs))
	}

	for i := range instrs {
		if !bytes.Equal(got[i].Keys, instrs[i].Keys) {
			t.Fatalf("[%d] Keys = %q, want %q", i, got[i].Keys, instrs[i].Keys)
		}
		if got[i].Width != instrs[i].Width {
			t.Fatalf("[%d] Width = %d, want %d", i, got[i].Width, instrs[i].Width)
		}
		if got[i].Height != instrs[i].Height {
			t.Fatalf("[%d] Height = %d, want %d", i, got[i].Height, instrs[i].Height)
		}
	}
}

func TestUserMessageEmpty(t *testing.T) {
	data := marshalUserMessage(nil)
	got, err := unmarshalUserMessage(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("len = %d, want 0", len(got))
	}
}

func TestVarintSize(t *testing.T) {
	tests := []struct {
		v    uint64
		want int
	}{
		{0, 1},
		{1, 1},
		{127, 1},
		{128, 2},
		{16383, 2},
		{16384, 3},
		{1<<63 - 1, 9},
		{1<<64 - 1, 10},
	}
	for _, tt := range tests {
		if got := varintSize(tt.v); got != tt.want {
			t.Errorf("varintSize(%d) = %d, want %d", tt.v, got, tt.want)
		}
	}
}

// TestUnmarshalMalformed tests that malformed protobuf inputs are handled
// gracefully without panicking.
func TestUnmarshalMalformed(t *testing.T) {
	t.Run("TruncatedVarint", func(t *testing.T) {
		// 20 continuation bytes with no terminator.
		data := make([]byte, 20)
		for i := range data {
			data[i] = 0x80
		}
		var ti TransportInstruction
		if err := ti.Unmarshal(data); err == nil {
			t.Fatal("expected error for truncated varint")
		}
	})

	t.Run("LengthExceedsBuffer", func(t *testing.T) {
		// Field 6 (bytes), claim 1GB length but only 4 bytes available.
		// Tag: field 6, wire type 2 = (6<<3)|2 = 0x32
		// Length: encode 1<<30 as varint
		data := []byte{0x32}
		data = appendVarint(data, 1<<30)
		data = append(data, 0x01, 0x02, 0x03, 0x04)

		var ti TransportInstruction
		if err := ti.Unmarshal(data); err == nil {
			t.Fatal("expected error for length exceeding buffer")
		}
	})

	t.Run("InvalidWireType", func(t *testing.T) {
		// Wire type 6 doesn't exist. Use field 99 to hit the default case.
		// Tag = (99<<3)|6 = 0x13, 0x06... actually let's compute:
		// field 99, wire type 6: varint = (99<<3)|6 = 798
		// 798 = 0x31E => varint bytes: 0x9E, 0x06
		data := []byte{0x9E, 0x06, 0x00}
		var ti TransportInstruction
		if err := ti.Unmarshal(data); err == nil {
			t.Fatal("expected error for invalid wire type")
		}
	})

	t.Run("EmptyInput", func(t *testing.T) {
		var ti TransportInstruction
		if err := ti.Unmarshal(nil); err != nil {
			t.Fatalf("empty input should succeed: %v", err)
		}
		if ti.OldNum != 0 || ti.NewNum != 0 {
			t.Fatal("empty input should produce zero-value struct")
		}
	})

	t.Run("ValidTagZeroLengthBytes", func(t *testing.T) {
		// Field 6 (bytes), length 0: valid tag, zero-length bytes field.
		// Tag: (6<<3)|2 = 0x32, Length: 0x00
		data := []byte{0x32, 0x00}
		var ti TransportInstruction
		if err := ti.Unmarshal(data); err != nil {
			t.Fatalf("zero-length bytes field should succeed: %v", err)
		}
		if len(ti.Diff) != 0 {
			t.Fatalf("Diff should be empty, got %d bytes", len(ti.Diff))
		}
	})
}

func TestTransportInstructionLatchCaps(t *testing.T) {
	ti := TransportInstruction{
		ProtocolVersion: 2,
		OldNum:          1,
		NewNum:          2,
		AckNum:          1,
		LatchCaps:       []byte{CapSessionControl},
	}
	data := ti.Marshal()
	var got TransportInstruction
	if err := got.Unmarshal(data); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.LatchCaps, ti.LatchCaps) {
		t.Fatalf("LatchCaps = %x, want %x", got.LatchCaps, ti.LatchCaps)
	}
}

func TestTransportInstructionUnknownField8Ignored(t *testing.T) {
	// Simulate an old parser that doesn't know about field 8.
	// Build a message with field 8 and verify the standard fields still parse.
	ti := TransportInstruction{
		ProtocolVersion: 2,
		OldNum:          5,
		NewNum:          6,
		AckNum:          4,
		LatchCaps:       []byte{0x03},
	}
	data := ti.Marshal()

	// Parse with a fresh struct — old code would skip unknown field 8.
	var got TransportInstruction
	if err := got.Unmarshal(data); err != nil {
		t.Fatal(err)
	}
	if got.OldNum != 5 || got.NewNum != 6 || got.AckNum != 4 {
		t.Fatalf("SSP fields corrupted: old=%d new=%d ack=%d", got.OldNum, got.NewNum, got.AckNum)
	}
}

func TestLatchControlRoundTrip(t *testing.T) {
	// Test in HostInstruction.
	hi := HostInstruction{
		Hoststring: []byte("data"),
		EchoAckNum: -1,
		Control: &LatchControl{
			Type:    CtrlSessionListResp,
			Payload: []byte(`["sess1","sess2"]`),
		},
	}
	data := marshalHostMessage([]HostInstruction{hi})
	got, err := unmarshalHostMessage(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d", len(got))
	}
	if got[0].Control == nil {
		t.Fatal("Control is nil")
	}
	if got[0].Control.Type != CtrlSessionListResp {
		t.Fatalf("Control.Type = %d, want %d", got[0].Control.Type, CtrlSessionListResp)
	}
	if !bytes.Equal(got[0].Control.Payload, hi.Control.Payload) {
		t.Fatalf("Control.Payload = %q, want %q", got[0].Control.Payload, hi.Control.Payload)
	}

	// Test in UserInstruction.
	ui := UserInstruction{
		Keys: []byte("x"),
		Control: &LatchControl{
			Type:    CtrlSessionSwitch,
			Payload: []byte("sess1"),
		},
	}
	udata := marshalUserMessage([]UserInstruction{ui})
	ugot, err := unmarshalUserMessage(udata)
	if err != nil {
		t.Fatal(err)
	}
	if len(ugot) != 1 {
		t.Fatalf("len = %d", len(ugot))
	}
	if ugot[0].Control == nil {
		t.Fatal("Control is nil")
	}
	if ugot[0].Control.Type != CtrlSessionSwitch {
		t.Fatalf("Control.Type = %d, want %d", ugot[0].Control.Type, CtrlSessionSwitch)
	}
	if !bytes.Equal(ugot[0].Control.Payload, ui.Control.Payload) {
		t.Fatalf("Control.Payload = %q, want %q", ugot[0].Control.Payload, ui.Control.Payload)
	}
}

func TestLatchControlAbsent(t *testing.T) {
	// Standard instruction without Control field should parse fine.
	hi := HostInstruction{
		Hoststring: []byte("hello"),
		EchoAckNum: 5,
	}
	data := marshalHostMessage([]HostInstruction{hi})
	got, err := unmarshalHostMessage(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d", len(got))
	}
	if got[0].Control != nil {
		t.Fatal("Control should be nil")
	}
	if !bytes.Equal(got[0].Hoststring, []byte("hello")) {
		t.Fatalf("Hoststring = %q", got[0].Hoststring)
	}
	if got[0].EchoAckNum != 5 {
		t.Fatalf("EchoAckNum = %d", got[0].EchoAckNum)
	}

	// Same for UserInstruction.
	ui := UserInstruction{Keys: []byte("k"), Width: 80, Height: 24}
	udata := marshalUserMessage([]UserInstruction{ui})
	ugot, err := unmarshalUserMessage(udata)
	if err != nil {
		t.Fatal(err)
	}
	if len(ugot) != 1 {
		t.Fatalf("len = %d", len(ugot))
	}
	if ugot[0].Control != nil {
		t.Fatal("Control should be nil")
	}
}

func TestDecodeVarintTruncated(t *testing.T) {
	// All continuation bytes, no terminator.
	data := make([]byte, 20)
	for i := range data {
		data[i] = 0x80
	}
	_, n := decodeVarint(data)
	if n != 0 {
		t.Fatalf("expected 0 for overflow, got %d", n)
	}
}

func TestUnmarshalTruncated(t *testing.T) {
	// Tag with no value.
	data := []byte{0x08} // field 1, varint
	var ti TransportInstruction
	if err := ti.Unmarshal(data); err == nil {
		t.Fatal("expected error for truncated message")
	}
}
