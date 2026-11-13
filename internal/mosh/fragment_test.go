package mosh

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func TestFragmentRoundTrip(t *testing.T) {
	f := Fragment{
		ID:          42,
		FragmentNum: 3,
		Final:       true,
		Payload:     []byte("hello mosh"),
	}
	wire := f.Marshal()
	got, err := UnmarshalFragment(wire)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != f.ID {
		t.Fatalf("ID = %d, want %d", got.ID, f.ID)
	}
	if got.FragmentNum != f.FragmentNum {
		t.Fatalf("FragmentNum = %d, want %d", got.FragmentNum, f.FragmentNum)
	}
	if got.Final != f.Final {
		t.Fatalf("Final = %v, want %v", got.Final, f.Final)
	}
	if !bytes.Equal(got.Payload, f.Payload) {
		t.Fatalf("Payload = %q, want %q", got.Payload, f.Payload)
	}
}

func TestFragmentNotFinal(t *testing.T) {
	f := Fragment{ID: 1, FragmentNum: 0, Final: false, Payload: []byte("x")}
	wire := f.Marshal()
	got, err := UnmarshalFragment(wire)
	if err != nil {
		t.Fatal(err)
	}
	if got.Final {
		t.Fatal("expected Final=false")
	}
}

func TestFragmentTooShort(t *testing.T) {
	_, err := UnmarshalFragment([]byte{1, 2, 3})
	if err == nil {
		t.Fatal("expected error for short fragment")
	}
}

func TestFragmentizeSmall(t *testing.T) {
	data := []byte("small message")
	frags := Fragmentize(1, data)
	if len(frags) != 1 {
		t.Fatalf("len = %d, want 1", len(frags))
	}
	if !frags[0].Final {
		t.Fatal("single fragment should be final")
	}
	if !bytes.Equal(frags[0].Payload, data) {
		t.Fatalf("Payload = %q, want %q", frags[0].Payload, data)
	}
}

func TestFragmentizeEmpty(t *testing.T) {
	frags := Fragmentize(1, nil)
	if len(frags) != 1 {
		t.Fatalf("len = %d, want 1", len(frags))
	}
	if !frags[0].Final {
		t.Fatal("empty fragment should be final")
	}
	if len(frags[0].Payload) != 0 {
		t.Fatal("empty fragment should have no payload")
	}
}

func TestFragmentizeLarge(t *testing.T) {
	data := make([]byte, maxFragmentPayload*3+500)
	rand.Read(data)
	frags := Fragmentize(7, data)

	if len(frags) != 4 {
		t.Fatalf("len = %d, want 4", len(frags))
	}

	for i, f := range frags {
		if f.ID != 7 {
			t.Fatalf("[%d] ID = %d", i, f.ID)
		}
		if int(f.FragmentNum) != i {
			t.Fatalf("[%d] FragmentNum = %d", i, f.FragmentNum)
		}
		if f.Final != (i == len(frags)-1) {
			t.Fatalf("[%d] Final = %v", i, f.Final)
		}
	}

	// Only the last fragment should be smaller.
	for i := 0; i < len(frags)-1; i++ {
		if len(frags[i].Payload) != maxFragmentPayload {
			t.Fatalf("[%d] Payload len = %d, want %d", i, len(frags[i].Payload), maxFragmentPayload)
		}
	}
	if len(frags[3].Payload) != 500 {
		t.Fatalf("[3] Payload len = %d, want 500", len(frags[3].Payload))
	}
}

func TestFragmentAssemblerInOrder(t *testing.T) {
	data := make([]byte, maxFragmentPayload*2+100)
	rand.Read(data)
	frags := Fragmentize(1, data)

	var a FragmentAssembler
	for i, f := range frags {
		result := a.Add(f)
		if i < len(frags)-1 {
			if result != nil {
				t.Fatalf("premature assembly at fragment %d", i)
			}
		} else {
			if result == nil {
				t.Fatal("assembly failed on final fragment")
			}
			if !bytes.Equal(result, data) {
				t.Fatal("reassembled data mismatch")
			}
		}
	}
}

func TestFragmentAssemblerOutOfOrder(t *testing.T) {
	data := make([]byte, maxFragmentPayload*3+1)
	rand.Read(data)
	frags := Fragmentize(1, data)

	// Deliver: 2, 0, 3, 1
	order := []int{2, 0, 3, 1}
	var a FragmentAssembler
	var result []byte
	for _, idx := range order {
		r := a.Add(frags[idx])
		if r != nil {
			result = r
		}
	}
	if result == nil {
		t.Fatal("assembly failed")
	}
	if !bytes.Equal(result, data) {
		t.Fatal("reassembled data mismatch")
	}
}

func TestFragmentAssemblerNewID(t *testing.T) {
	var a FragmentAssembler

	// Start assembling ID=1 but never finish.
	a.Add(Fragment{ID: 1, FragmentNum: 0, Payload: []byte("old")})

	// New ID=2 arrives — old state should be discarded.
	data := []byte("new message")
	frags := Fragmentize(2, data)
	result := a.Add(frags[0])
	if result == nil {
		t.Fatal("single-fragment message should complete")
	}
	if !bytes.Equal(result, data) {
		t.Fatal("data mismatch")
	}
}

func TestFragmentAssemblerStaleID(t *testing.T) {
	var a FragmentAssembler

	// Set current to ID=5.
	a.Add(Fragment{ID: 5, FragmentNum: 0, Final: true, Payload: []byte("five")})

	// Stale ID=3 should be dropped.
	result := a.Add(Fragment{ID: 3, FragmentNum: 0, Final: true, Payload: []byte("three")})
	if result != nil {
		t.Fatal("stale ID should be dropped")
	}
}

func TestAssemblerMissingFragment(t *testing.T) {
	// Send fragment 0, skip fragment 1, send fragment 2 (final).
	// The assembler should NOT return data since fragment 1 is missing.
	var a FragmentAssembler

	frag0 := Fragment{ID: 1, FragmentNum: 0, Final: false, Payload: []byte("aaa")}
	frag2 := Fragment{ID: 1, FragmentNum: 2, Final: true, Payload: []byte("ccc")}

	if r := a.Add(frag0); r != nil {
		t.Fatal("should not complete after fragment 0")
	}
	// Skip fragment 1 entirely.
	if r := a.Add(frag2); r != nil {
		t.Fatal("should not complete with missing fragment 1")
	}
}

func TestAssemblerDuplicateFragment(t *testing.T) {
	// Send fragment 0 twice, then fragment 1 (final).
	// Duplicate should be idempotent — correct reassembly expected.
	var a FragmentAssembler

	frag0 := Fragment{ID: 1, FragmentNum: 0, Final: false, Payload: []byte("hello")}
	frag1 := Fragment{ID: 1, FragmentNum: 1, Final: true, Payload: []byte(" world")}

	if r := a.Add(frag0); r != nil {
		t.Fatal("should not complete after first fragment 0")
	}
	if r := a.Add(frag0); r != nil {
		t.Fatal("should not complete after duplicate fragment 0")
	}
	result := a.Add(frag1)
	if result == nil {
		t.Fatal("should complete after fragment 1")
	}
	if !bytes.Equal(result, []byte("hello world")) {
		t.Fatalf("reassembled = %q, want %q", result, "hello world")
	}
}

func TestFragmentWireRoundTrip(t *testing.T) {
	// Full round-trip: data → fragmentize → marshal → unmarshal → reassemble.
	data := make([]byte, maxFragmentPayload*2+42)
	rand.Read(data)
	frags := Fragmentize(99, data)

	var a FragmentAssembler
	var result []byte
	for _, f := range frags {
		wire := f.Marshal()
		got, err := UnmarshalFragment(wire)
		if err != nil {
			t.Fatal(err)
		}
		if r := a.Add(got); r != nil {
			result = r
		}
	}
	if !bytes.Equal(result, data) {
		t.Fatal("wire round-trip mismatch")
	}
}
