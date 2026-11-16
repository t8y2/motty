package mosh

import (
	"encoding/binary"
	"errors"
	"math/bits"
)

// Hand-rolled protobuf encoding for the three mosh .proto schemas.
// Field numbers match upstream mobile-shell/mosh exactly.

// TransportInstruction is the outer transport wrapper (TransportBuffers.Instruction).
//
// Field numbers:
//
//	1: protocol_version (uint32)
//	2: old_num          (uint64)
//	3: new_num          (uint64)
//	4: ack_num          (uint64)
//	5: throwaway_num    (uint64)
//	6: diff             (bytes)
//	7: chaff            (bytes)
type TransportInstruction struct {
	ProtocolVersion uint32
	OldNum          uint64
	NewNum          uint64
	AckNum          uint64
	ThrowawayNum    uint64
	Diff            []byte
	Chaff           []byte
	LatchCaps       []byte
}

// LatchControl is a latch extension control message.
type LatchControl struct {
	Type    uint32
	Payload []byte
}

const (
	CtrlSessionListReq  uint32 = 1
	CtrlSessionListResp uint32 = 2
	CtrlSessionSwitch   uint32 = 3
	CtrlSessionSwitched uint32 = 4
	CtrlSessionCreate   uint32 = 5
	CtrlSessionCreated  uint32 = 6
)

const (
	CapSessionControl byte = 1 << 0
)

// HostInstruction is one instruction within a HostMessage.
// Represents the extension fields of HostBuffers.Instruction:
//
//	field 2 → HostBytes { field 4: hoststring }
//	field 3 → ResizeMessage { field 5: width, field 6: height }
//	field 7 → EchoAck { field 8: echo_ack_num }
type HostInstruction struct {
	Hoststring []byte
	Width      int32 // 0 = not present
	Height     int32 // 0 = not present
	EchoAckNum int64 // -1 = not present
	Control    *LatchControl
}

// UserInstruction is one instruction within a UserMessage.
// Represents the extension fields of ClientBuffers.Instruction:
//
//	field 2 → Keystroke { field 4: keys }
//	field 3 → ResizeMessage { field 5: width, field 6: height }
type UserInstruction struct {
	Keys    []byte
	Width   int32 // 0 = not present
	Height  int32 // 0 = not present
	Control *LatchControl
}

// Wire type constants.
const (
	wireVarint = 0
	wireBytes  = 2
)

var errTruncated = errors.New("mosh/pb: truncated message")

// --- Encoding helpers ---

func appendTag(b []byte, field int, wtype int) []byte {
	return appendVarint(b, uint64(field<<3|wtype))
}

func appendVarint(b []byte, v uint64) []byte {
	for v >= 0x80 {
		b = append(b, byte(v)|0x80)
		v >>= 7
	}
	return append(b, byte(v))
}

func appendTagVarint(b []byte, field int, v uint64) []byte {
	b = appendTag(b, field, wireVarint)
	return appendVarint(b, v)
}

func appendTagBytes(b []byte, field int, data []byte) []byte {
	b = appendTag(b, field, wireBytes)
	b = appendVarint(b, uint64(len(data)))
	return append(b, data...)
}

// --- Decoding helpers ---

func decodeVarint(b []byte) (uint64, int) {
	var v uint64
	for i, c := range b {
		if i >= binary.MaxVarintLen64 {
			return 0, 0
		}
		v |= uint64(c&0x7f) << (7 * i)
		if c < 0x80 {
			return v, i + 1
		}
	}
	return 0, 0
}

func decodeTag(b []byte) (field, wtype int, n int) {
	v, n := decodeVarint(b)
	if n == 0 {
		return 0, 0, 0
	}
	return int(v >> 3), int(v & 7), n
}

// skipField advances past an unknown field.
func skipField(b []byte, wtype int) int {
	switch wtype {
	case wireVarint:
		_, n := decodeVarint(b)
		return n
	case wireBytes:
		length, n := decodeVarint(b)
		if n == 0 {
			return 0
		}
		return n + int(length)
	case 5: // 32-bit
		return 4
	case 1: // 64-bit
		return 8
	}
	return 0
}

// --- TransportInstruction ---

func (ti *TransportInstruction) Marshal() []byte {
	var b []byte
	if ti.ProtocolVersion != 0 {
		b = appendTagVarint(b, 1, uint64(ti.ProtocolVersion))
	}
	b = appendTagVarint(b, 2, ti.OldNum)
	b = appendTagVarint(b, 3, ti.NewNum)
	b = appendTagVarint(b, 4, ti.AckNum)
	b = appendTagVarint(b, 5, ti.ThrowawayNum)
	if len(ti.Diff) > 0 {
		b = appendTagBytes(b, 6, ti.Diff)
	}
	if len(ti.Chaff) > 0 {
		b = appendTagBytes(b, 7, ti.Chaff)
	}
	if len(ti.LatchCaps) > 0 {
		b = appendTagBytes(b, 8, ti.LatchCaps)
	}
	return b
}

func (ti *TransportInstruction) Unmarshal(data []byte) error {
	for len(data) > 0 {
		field, wtype, n := decodeTag(data)
		if n == 0 {
			return errTruncated
		}
		data = data[n:]

		switch field {
		case 1:
			v, n := decodeVarint(data)
			if n == 0 {
				return errTruncated
			}
			ti.ProtocolVersion = uint32(v)
			data = data[n:]
		case 2:
			v, n := decodeVarint(data)
			if n == 0 {
				return errTruncated
			}
			ti.OldNum = v
			data = data[n:]
		case 3:
			v, n := decodeVarint(data)
			if n == 0 {
				return errTruncated
			}
			ti.NewNum = v
			data = data[n:]
		case 4:
			v, n := decodeVarint(data)
			if n == 0 {
				return errTruncated
			}
			ti.AckNum = v
			data = data[n:]
		case 5:
			v, n := decodeVarint(data)
			if n == 0 {
				return errTruncated
			}
			ti.ThrowawayNum = v
			data = data[n:]
		case 6:
			length, n := decodeVarint(data)
			if n == 0 || int(length) > len(data[n:]) {
				return errTruncated
			}
			ti.Diff = append([]byte(nil), data[n:n+int(length)]...)
			data = data[n+int(length):]
		case 7:
			length, n := decodeVarint(data)
			if n == 0 || int(length) > len(data[n:]) {
				return errTruncated
			}
			ti.Chaff = append([]byte(nil), data[n:n+int(length)]...)
			data = data[n+int(length):]
		case 8:
			length, n := decodeVarint(data)
			if n == 0 || int(length) > len(data[n:]) {
				return errTruncated
			}
			ti.LatchCaps = append([]byte(nil), data[n:n+int(length)]...)
			data = data[n+int(length):]
		default:
			skip := skipField(data, wtype)
			if skip == 0 {
				return errTruncated
			}
			data = data[skip:]
		}
	}
	return nil
}

// --- HostMessage ---

// MarshalHostMessage encodes a list of HostInstructions as a HostMessage protobuf.
func MarshalHostMessage(instrs []HostInstruction) []byte {
	return marshalHostMessage(instrs)
}

func marshalHostMessage(instrs []HostInstruction) []byte {
	var b []byte
	for i := range instrs {
		sub := instrs[i].marshal()
		b = appendTagBytes(b, 1, sub)
	}
	return b
}

// UnmarshalHostMessage decodes a HostMessage protobuf into a list of HostInstructions.
func UnmarshalHostMessage(data []byte) ([]HostInstruction, error) {
	return unmarshalHostMessage(data)
}

func unmarshalHostMessage(data []byte) ([]HostInstruction, error) {
	var instrs []HostInstruction
	for len(data) > 0 {
		field, _, n := decodeTag(data)
		if n == 0 {
			return nil, errTruncated
		}
		data = data[n:]
		if field != 1 {
			return nil, errors.New("mosh/pb: unexpected field in HostMessage")
		}
		length, n := decodeVarint(data)
		if n == 0 || int(length) > len(data[n:]) {
			return nil, errTruncated
		}
		var hi HostInstruction
		hi.EchoAckNum = -1
		if err := hi.unmarshal(data[n : n+int(length)]); err != nil {
			return nil, err
		}
		instrs = append(instrs, hi)
		data = data[n+int(length):]
	}
	return instrs, nil
}

func (hi *HostInstruction) marshal() []byte {
	var b []byte
	if len(hi.Hoststring) > 0 {
		// field 2: HostBytes submessage containing field 4: hoststring
		sub := appendTagBytes(nil, 4, hi.Hoststring)
		b = appendTagBytes(b, 2, sub)
	}
	if hi.Width > 0 || hi.Height > 0 {
		// field 3: ResizeMessage submessage containing field 5: width, field 6: height
		sub := appendTagVarint(nil, 5, uint64(hi.Width))
		sub = appendTagVarint(sub, 6, uint64(hi.Height))
		b = appendTagBytes(b, 3, sub)
	}
	if hi.EchoAckNum >= 0 {
		// field 7: EchoAck submessage containing field 8: echo_ack_num
		sub := appendTagVarint(nil, 8, uint64(hi.EchoAckNum))
		b = appendTagBytes(b, 7, sub)
	}
	if hi.Control != nil {
		sub := appendTagVarint(nil, 10, uint64(hi.Control.Type))
		if len(hi.Control.Payload) > 0 {
			sub = appendTagBytes(sub, 11, hi.Control.Payload)
		}
		b = appendTagBytes(b, 9, sub)
	}
	return b
}

func (hi *HostInstruction) unmarshal(data []byte) error {
	for len(data) > 0 {
		field, wtype, n := decodeTag(data)
		if n == 0 {
			return errTruncated
		}
		data = data[n:]

		switch field {
		case 2: // HostBytes
			length, n := decodeVarint(data)
			if n == 0 || int(length) > len(data[n:]) {
				return errTruncated
			}
			if err := hi.unmarshalHostBytes(data[n : n+int(length)]); err != nil {
				return err
			}
			data = data[n+int(length):]
		case 3: // ResizeMessage
			length, n := decodeVarint(data)
			if n == 0 || int(length) > len(data[n:]) {
				return errTruncated
			}
			if err := hi.unmarshalResize(data[n : n+int(length)]); err != nil {
				return err
			}
			data = data[n+int(length):]
		case 7: // EchoAck
			length, n := decodeVarint(data)
			if n == 0 || int(length) > len(data[n:]) {
				return errTruncated
			}
			if err := hi.unmarshalEchoAck(data[n : n+int(length)]); err != nil {
				return err
			}
			data = data[n+int(length):]
		case 9: // LatchControl
			length, n := decodeVarint(data)
			if n == 0 || int(length) > len(data[n:]) {
				return errTruncated
			}
			ctrl := &LatchControl{}
			if err := ctrl.unmarshal(data[n : n+int(length)]); err != nil {
				return err
			}
			hi.Control = ctrl
			data = data[n+int(length):]
		default:
			skip := skipField(data, wtype)
			if skip == 0 {
				return errTruncated
			}
			data = data[skip:]
		}
	}
	return nil
}

func (hi *HostInstruction) unmarshalHostBytes(data []byte) error {
	for len(data) > 0 {
		field, wtype, n := decodeTag(data)
		if n == 0 {
			return errTruncated
		}
		data = data[n:]
		if field == 4 {
			length, n := decodeVarint(data)
			if n == 0 || int(length) > len(data[n:]) {
				return errTruncated
			}
			hi.Hoststring = append([]byte(nil), data[n:n+int(length)]...)
			data = data[n+int(length):]
		} else {
			skip := skipField(data, wtype)
			if skip == 0 {
				return errTruncated
			}
			data = data[skip:]
		}
	}
	return nil
}

func (hi *HostInstruction) unmarshalResize(data []byte) error {
	return unmarshalResize(data, &hi.Width, &hi.Height)
}

func (hi *HostInstruction) unmarshalEchoAck(data []byte) error {
	for len(data) > 0 {
		field, wtype, n := decodeTag(data)
		if n == 0 {
			return errTruncated
		}
		data = data[n:]
		if field == 8 {
			v, n := decodeVarint(data)
			if n == 0 {
				return errTruncated
			}
			hi.EchoAckNum = int64(v)
			data = data[n:]
		} else {
			skip := skipField(data, wtype)
			if skip == 0 {
				return errTruncated
			}
			data = data[skip:]
		}
	}
	return nil
}

// --- UserMessage ---

// MarshalUserMessage encodes a list of UserInstructions as a UserMessage protobuf.
func MarshalUserMessage(instrs []UserInstruction) []byte {
	return marshalUserMessage(instrs)
}

func marshalUserMessage(instrs []UserInstruction) []byte {
	var b []byte
	for i := range instrs {
		sub := instrs[i].marshal()
		b = appendTagBytes(b, 1, sub)
	}
	return b
}

func unmarshalUserMessage(data []byte) ([]UserInstruction, error) {
	var instrs []UserInstruction
	for len(data) > 0 {
		field, _, n := decodeTag(data)
		if n == 0 {
			return nil, errTruncated
		}
		data = data[n:]
		if field != 1 {
			return nil, errors.New("mosh/pb: unexpected field in UserMessage")
		}
		length, n := decodeVarint(data)
		if n == 0 || int(length) > len(data[n:]) {
			return nil, errTruncated
		}
		var ui UserInstruction
		if err := ui.unmarshal(data[n : n+int(length)]); err != nil {
			return nil, err
		}
		instrs = append(instrs, ui)
		data = data[n+int(length):]
	}
	return instrs, nil
}

func (ui *UserInstruction) marshal() []byte {
	var b []byte
	if len(ui.Keys) > 0 {
		// field 2: Keystroke submessage containing field 4: keys
		sub := appendTagBytes(nil, 4, ui.Keys)
		b = appendTagBytes(b, 2, sub)
	}
	if ui.Width > 0 || ui.Height > 0 {
		// field 3: ResizeMessage submessage
		sub := appendTagVarint(nil, 5, uint64(ui.Width))
		sub = appendTagVarint(sub, 6, uint64(ui.Height))
		b = appendTagBytes(b, 3, sub)
	}
	if ui.Control != nil {
		sub := appendTagVarint(nil, 10, uint64(ui.Control.Type))
		if len(ui.Control.Payload) > 0 {
			sub = appendTagBytes(sub, 11, ui.Control.Payload)
		}
		b = appendTagBytes(b, 9, sub)
	}
	return b
}

func (ui *UserInstruction) unmarshal(data []byte) error {
	for len(data) > 0 {
		field, wtype, n := decodeTag(data)
		if n == 0 {
			return errTruncated
		}
		data = data[n:]

		switch field {
		case 2: // Keystroke
			length, n := decodeVarint(data)
			if n == 0 || int(length) > len(data[n:]) {
				return errTruncated
			}
			if err := ui.unmarshalKeystroke(data[n : n+int(length)]); err != nil {
				return err
			}
			data = data[n+int(length):]
		case 3: // ResizeMessage
			length, n := decodeVarint(data)
			if n == 0 || int(length) > len(data[n:]) {
				return errTruncated
			}
			if err := unmarshalResize(data[n:n+int(length)], &ui.Width, &ui.Height); err != nil {
				return err
			}
			data = data[n+int(length):]
		case 9: // LatchControl
			length, n := decodeVarint(data)
			if n == 0 || int(length) > len(data[n:]) {
				return errTruncated
			}
			ctrl := &LatchControl{}
			if err := ctrl.unmarshal(data[n : n+int(length)]); err != nil {
				return err
			}
			ui.Control = ctrl
			data = data[n+int(length):]
		default:
			skip := skipField(data, wtype)
			if skip == 0 {
				return errTruncated
			}
			data = data[skip:]
		}
	}
	return nil
}

func (ui *UserInstruction) unmarshalKeystroke(data []byte) error {
	for len(data) > 0 {
		field, wtype, n := decodeTag(data)
		if n == 0 {
			return errTruncated
		}
		data = data[n:]
		if field == 4 {
			length, n := decodeVarint(data)
			if n == 0 || int(length) > len(data[n:]) {
				return errTruncated
			}
			ui.Keys = append(ui.Keys, data[n:n+int(length)]...)
			data = data[n+int(length):]
		} else {
			skip := skipField(data, wtype)
			if skip == 0 {
				return errTruncated
			}
			data = data[skip:]
		}
	}
	return nil
}

// Shared ResizeMessage decoder (used by both Host and User).
func unmarshalResize(data []byte, width, height *int32) error {
	for len(data) > 0 {
		field, wtype, n := decodeTag(data)
		if n == 0 {
			return errTruncated
		}
		data = data[n:]
		switch field {
		case 5:
			v, n := decodeVarint(data)
			if n == 0 {
				return errTruncated
			}
			*width = int32(v)
			data = data[n:]
		case 6:
			v, n := decodeVarint(data)
			if n == 0 {
				return errTruncated
			}
			*height = int32(v)
			data = data[n:]
		default:
			skip := skipField(data, wtype)
			if skip == 0 {
				return errTruncated
			}
			data = data[skip:]
		}
	}
	return nil
}

func (c *LatchControl) unmarshal(data []byte) error {
	for len(data) > 0 {
		field, wtype, n := decodeTag(data)
		if n == 0 {
			return errTruncated
		}
		data = data[n:]
		switch field {
		case 10:
			v, n := decodeVarint(data)
			if n == 0 {
				return errTruncated
			}
			c.Type = uint32(v)
			data = data[n:]
		case 11:
			length, n := decodeVarint(data)
			if n == 0 || int(length) > len(data[n:]) {
				return errTruncated
			}
			c.Payload = append([]byte(nil), data[n:n+int(length)]...)
			data = data[n+int(length):]
		default:
			skip := skipField(data, wtype)
			if skip == 0 {
				return errTruncated
			}
			data = data[skip:]
		}
	}
	return nil
}

// varintSize returns the encoded size of a varint.
func varintSize(v uint64) int {
	if v == 0 {
		return 1
	}
	return (bits.Len64(v) + 6) / 7
}
