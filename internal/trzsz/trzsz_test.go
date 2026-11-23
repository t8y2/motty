package trzsz

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestEncodeTextRoundTrip(t *testing.T) {
	for _, input := range []string{"", "a.txt", "ünïcode/文件", string([]byte{0, 1, 2, 0xff})} {
		encoded := EncodeText([]byte(input))
		decoded, err := DecodeText(encoded, maxMessage)
		if err != nil {
			t.Fatalf("DecodeText(%q): %v", input, err)
		}
		if string(decoded) != input {
			t.Fatalf("round trip = %q, want %q", decoded, input)
		}
	}
}

func TestDecodeTextRefusesOversizedPayload(t *testing.T) {
	encoded := EncodeText(bytes.Repeat([]byte("x"), 1024))
	if _, err := DecodeText(encoded, 100); err == nil {
		t.Fatal("DecodeText accepted a payload over its limit")
	}
}

func TestMatchTrigger(t *testing.T) {
	line := "\x1b[s::TRZSZ:TRANSFER:S:1.2.0:0000001234567:0\r\n"
	start, end, trigger, ok, more := MatchTrigger([]byte("prompt$ tsz a" + line))
	if !ok || more {
		t.Fatalf("MatchTrigger = %v, %v, want a complete trigger", ok, more)
	}
	if start != len("prompt$ tsz a") || end != start+len(line) {
		t.Fatalf("MatchTrigger offsets = %d, %d, want %d, %d", start, end, len("prompt$ tsz a"), len("prompt$ tsz a")+len(line))
	}
	if trigger.Mode != 'S' || trigger.Version != [3]int{1, 2, 0} {
		t.Fatalf("trigger = %+v", trigger)
	}
	if trigger.Sending() || trigger.Directory() {
		t.Fatalf("a send trigger was read as an upload or a tree")
	}
	// A pty may double the carriage return the announcement ends with.
	if _, _, _, ok, _ := MatchTrigger([]byte("\x1b[s::TRZSZ:TRANSFER:S:1.2.0:0000001234567:0\r\r\n")); !ok {
		t.Fatal("a pty-doubled line ending hid the trigger")
	}
}

// Servers before 1.2.0 save the cursor with ESC 7 (the Go programs add a BEL),
// and the announcement must be read whichever way they do it, however the
// output is split.
func TestMatchTriggerEveryIntroducer(t *testing.T) {
	for _, introducer := range []string{"\x1b[s", "\x1b7", "\x1b7\x07"} {
		line := introducer + "::TRZSZ:TRANSFER:R:1.1.8:0000001234567:36435\r\n"
		input := "prompt$ trz" + line
		start, end, trigger, ok, more := MatchTrigger([]byte(input))
		if !ok || more || start != len("prompt$ trz") || end != len(input) {
			t.Fatalf("%q: MatchTrigger = %d, %d, %v, %v", introducer, start, end, ok, more)
		}
		if trigger.Mode != 'R' || trigger.Version != [3]int{1, 1, 8} {
			t.Fatalf("%q: trigger = %+v", introducer, trigger)
		}
		// Cut at every byte: what the caller keeps (TailSize) must be enough
		// to find the announcement once the rest arrives.
		for cut := 1; cut < len(input); cut++ {
			kept := []byte(input[:cut])
			at, _, _, found, more := MatchTrigger(kept)
			if found {
				continue
			}
			if more {
				kept = kept[at:]
			} else if len(kept) > TailSize {
				kept = kept[len(kept)-TailSize:]
			}
			kept = append(kept, input[cut:]...)
			if got, _, _, found, _ := MatchTrigger(kept); !found || got != bytes.Index(kept, []byte(introducer)) {
				t.Fatalf("%q cut at %d: announcement lost (found %v at %d)", introducer, cut, found, got)
			}
		}
	}
}

func TestMatchTriggerNeedsMore(t *testing.T) {
	part := "\x1b[s::TRZSZ:TRANSFER:R:1.2.0:00000012345"
	start, _, _, ok, more := MatchTrigger([]byte(part))
	if ok || !more {
		t.Fatalf("MatchTrigger on a cut line = %v, %v, want more", ok, more)
	}
	if start != 0 {
		t.Fatalf("start = %d, want 0", start)
	}
}

func TestMatchTriggerIgnoresLookalikes(t *testing.T) {
	for _, input := range []string{
		"::TRZSZ:TRANSFER:S:1.2.0\n",
		"\x1b[s::TRZSZ:TRANSFER:X:1.2.0:0000001234567:0\n",
		"\x1b[s::TRZSZ:TRANSFER:S:one:0000001234567:0\n",
		"\x1b[s::TRZSZ:TRANSFER:S:1.2.0:not-a-number:0\n",
	} {
		if start, _, _, ok, _ := MatchTrigger([]byte(input)); ok {
			t.Fatalf("MatchTrigger(%q) = %d, true, want false", input, start)
		}
	}
}

func TestCheckComponent(t *testing.T) {
	good := []string{"a", "a.txt", "..a", "a..b", "文件"}
	for _, name := range good {
		if err := checkComponent(name); err != nil {
			t.Fatalf("checkComponent(%q) = %v", name, err)
		}
	}
	bad := []string{"", ".", "..", "a/b", `a\b`, "a\x00b", "a\nb"}
	for _, name := range bad {
		if err := checkComponent(name); err == nil {
			t.Fatalf("checkComponent(%q) accepted it", name)
		}
	}
}

func TestWithin(t *testing.T) {
	root := string(filepath.Separator) + filepath.Join("tmp", "download")
	inside := filepath.Join(root, "a", "b")
	if !within(root, inside) {
		t.Fatalf("within(%q, %q) = false", root, inside)
	}
	outside := filepath.Join(root, "..", "b")
	if within(root, outside) {
		t.Fatalf("within(%q, %q) = true", root, outside)
	}
}

func TestListDirectoryRefusesLinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links need a privilege on Windows")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "file.txt"), filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := ListDirectory(dir); err == nil {
		t.Fatal("ListDirectory followed a symbolic link")
	}
}

func TestParseCountRejectsJunk(t *testing.T) {
	for _, input := range []string{"", "1a", "-1", " 1", "999999999999999999999"} {
		if _, err := parseCount(input); err == nil {
			t.Fatalf("parseCount(%q) accepted it", input)
		}
	}
	if value, err := parseCount("1024"); err != nil || value != 1024 {
		t.Fatalf("parseCount(1024) = %d, %v", value, err)
	}
}

func TestCreateRootKeepsAnExistingFile(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(existing, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	transfer := &Transfer{dir: dir}
	local, _, file, err := transfer.createRoot("a.txt", false)
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	if local != "a (1).txt" {
		t.Fatalf("local name = %q, want a (1).txt", local)
	}
	if data, _ := os.ReadFile(existing); string(data) != "keep" {
		t.Fatalf("the existing file was replaced: %q", data)
	}
	transfer.discardReceived()
	if _, err := os.Stat(existing); err != nil {
		t.Fatalf("discarding the download removed an existing file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, local)); !os.IsNotExist(err) {
		t.Fatalf("the created file was not removed: %v", err)
	}
}

func TestCreateEntryRefusesToLeaveTheFolder(t *testing.T) {
	dir := t.TempDir()
	transfer := &Transfer{dir: dir, dirMode: true, topNames: map[int]string{}}
	payload, err := json.Marshal(sourceFile{PathID: 0, PathName: []string{"top", "..", "evil"}, IsDir: false})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transfer.createEntry(payload); err == nil {
		t.Fatal("a path with .. was accepted")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("the refused entry created %d things", len(entries))
	}
}

func TestCancelBeforeTheAnswerEndsTheTransfer(t *testing.T) {
	dir := t.TempDir()
	transfer, err := NewReceive(dir, Trigger{Mode: 'S'})
	if err != nil {
		t.Fatal(err)
	}
	transfer.Cancel()
	if !transfer.Finished() || !transfer.Settled() || !errors.Is(transfer.Err(), ErrCancelled) {
		t.Fatalf("after cancelling: finished %v, settled %v, err %v", transfer.Finished(), transfer.Settled(), transfer.Err())
	}
	if out := string(transfer.TakeOutput(nil)); !strings.Contains(out, "#fail:") {
		t.Fatalf("the remote was told %q, want a failure", out)
	}
}
