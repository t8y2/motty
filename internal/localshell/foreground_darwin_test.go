//go:build darwin

package localshell

import (
	"encoding/binary"
	"slices"
	"testing"
)

func procArgs2(argc int, body string) []byte {
	buf := binary.LittleEndian.AppendUint32(nil, uint32(argc))
	return append(buf, body...)
}

func TestProcArgs2StopsAtArgcAndKeepsTheEnvironmentOut(t *testing.T) {
	// exec path, padding, argv "sh" "" "x", then an environment string.
	buf := procArgs2(3, "/bin/sh\x00\x00\x00sh\x00\x00x\x00EDITOR=/usr/bin/codex\x00")
	got, ok := parseProcArgs2(buf)
	if want := []string{"sh", "x"}; !ok || !slices.Equal(got, want) {
		t.Fatalf("args = %q ok=%v, want %q", got, ok, want)
	}
	if _, ok := parseProcArgs2(procArgs2(0, "/bin/sh\x00")); ok {
		t.Fatal("argc 0 gave arguments")
	}
	if _, ok := parseProcArgs2([]byte{1, 0}); ok {
		t.Fatal("a short buffer gave arguments")
	}
}
