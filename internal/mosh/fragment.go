package mosh

import (
	"encoding/binary"
	"errors"
)

// Fragment wire format (mosh network.cc):
//
//	[instruction_id : 8 bytes, big-endian]
//	[fragment_num(15 bits) | final_flag(1 bit) : 2 bytes, big-endian]
//	[payload : remaining bytes]
//
// fragment_num occupies the upper 15 bits; the lowest bit is the final flag.

const (
	fragmentHeaderSize = 10 // 8 + 2
	fragmentFinalBit   = 0x8000

	// Maximum payload per fragment. Mosh's default send MTU is 500 bytes: it
	// stays below the IPv6 minimum MTU (1280) and avoids IP fragmentation on
	// paths with a reduced MTU or a middlebox that drops large datagrams.
	maxFragmentPayload = 500
)

// Fragment is a single mosh datagram fragment.
type Fragment struct {
	ID          uint64
	FragmentNum uint16 // 0-based
	Final       bool
	Payload     []byte
}

// Marshal encodes a fragment to wire format.
func (f *Fragment) Marshal() []byte {
	b := make([]byte, fragmentHeaderSize+len(f.Payload))
	binary.BigEndian.PutUint64(b[0:], f.ID)
	// Mosh layout: [final:1 bit][fragment_num:15 bits] (big-endian uint16).
	numAndFinal := f.FragmentNum & 0x7fff
	if f.Final {
		numAndFinal |= fragmentFinalBit
	}
	binary.BigEndian.PutUint16(b[8:], numAndFinal)
	copy(b[fragmentHeaderSize:], f.Payload)
	return b
}

// UnmarshalFragment decodes a fragment from wire format.
func UnmarshalFragment(data []byte) (Fragment, error) {
	if len(data) < fragmentHeaderSize {
		return Fragment{}, errors.New("mosh: fragment too short")
	}
	var f Fragment
	f.ID = binary.BigEndian.Uint64(data[0:])
	numAndFinal := binary.BigEndian.Uint16(data[8:])
	f.Final = numAndFinal&fragmentFinalBit != 0
	f.FragmentNum = numAndFinal & 0x7fff
	f.Payload = append([]byte(nil), data[fragmentHeaderSize:]...)
	return f, nil
}

// Fragmentize splits a protobuf-encoded message into fragments.
// Each fragment carries at most maxFragmentPayload bytes.
func Fragmentize(id uint64, data []byte) []Fragment {
	if len(data) == 0 {
		return []Fragment{{ID: id, Final: true}}
	}

	n := (len(data) + maxFragmentPayload - 1) / maxFragmentPayload
	frags := make([]Fragment, n)
	for i := range frags {
		start := i * maxFragmentPayload
		end := start + maxFragmentPayload
		if end > len(data) {
			end = len(data)
		}
		frags[i] = Fragment{
			ID:          id,
			FragmentNum: uint16(i),
			Final:       i == n-1,
			Payload:     data[start:end],
		}
	}
	return frags
}

const maxReassembledSize = 1 << 20 // 1 MiB

// FragmentAssembler reassembles fragments into complete messages.
type FragmentAssembler struct {
	currentID uint64
	fragments [][]byte
	totalNum  int // expected count, -1 if unknown
	totalSize int // accumulated payload bytes
}

// Add processes a fragment and returns the reassembled message when complete.
// Returns nil if the message is not yet complete.
// Drops fragments from old instruction IDs.
func (a *FragmentAssembler) Add(f Fragment) []byte {
	if f.ID < a.currentID {
		return nil // stale
	}

	// New instruction ID — reset.
	if f.ID != a.currentID {
		a.currentID = f.ID
		a.fragments = nil
		a.totalNum = -1
		a.totalSize = 0
	}

	// Check total accumulated size.
	a.totalSize += len(f.Payload)
	if a.totalSize > maxReassembledSize {
		a.fragments = nil
		a.totalNum = -1
		a.totalSize = 0
		return nil
	}

	// Extend slice if needed.
	idx := int(f.FragmentNum)
	for len(a.fragments) <= idx {
		a.fragments = append(a.fragments, nil)
	}
	a.fragments[idx] = f.Payload

	if f.Final {
		a.totalNum = idx + 1
	}

	// Check completeness.
	if a.totalNum < 0 || len(a.fragments) < a.totalNum {
		return nil
	}
	for i := 0; i < a.totalNum; i++ {
		if a.fragments[i] == nil {
			return nil
		}
	}

	// Reassemble.
	var total int
	for i := 0; i < a.totalNum; i++ {
		total += len(a.fragments[i])
	}
	msg := make([]byte, 0, total)
	for i := 0; i < a.totalNum; i++ {
		msg = append(msg, a.fragments[i]...)
	}

	a.fragments = nil
	a.totalNum = -1
	return msg
}
