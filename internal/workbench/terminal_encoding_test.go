package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"golang.org/x/text/encoding/simplifiedchinese"

	"motty/internal/sshconn"
)

// encodingLiveSession is liveSession for a host with a remote charset. The
// encoding has to be on the saved host before the shell is opened, as the
// dialog would have saved it.
func encodingLiveSession(t testing.TB, encoding string) (*State, *world, *fakeConn, *fakeShell) {
	t.Helper()
	world, options := newWorld(t)
	options.Invalidate = func() {}
	state := newTestState(t, options)
	state.hosts[0].Encoding = encoding
	fake := connect(t, state, world)
	fake.set(func(s *sshconn.Status) {
		s.Phase = sshconn.PhaseConnected
		s.ServerVersion = "SSH-2.0-Test"
		s.Method = "password"
	})
	state.Wake()
	shell := fake.shell(t)
	shell.say("\x1b[0m")
	state.Wake()
	return state, world, fake, shell
}

// gbkBytes encodes text with x/text directly, independently of the decoder
// under test, so a shared mistake in the codec mapping cannot hide a bug.
func gbkBytes(t testing.TB, text string) []byte {
	t.Helper()
	data, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(text))
	if err != nil {
		t.Fatalf("encode %q as GBK: %v", text, err)
	}
	return data
}

// TestGB18030OutputIsDecodedToTheScreen is the reported bug: a server whose
// LANG is GB18030 sends `ls -lrt` output in GB18030, which must not be a row
// of replacement characters.
func TestGB18030OutputIsDecodedToTheScreen(t *testing.T) {
	state, world, _, shell := encodingLiveSession(t, "GB18030")
	shell.say(string(gbkBytes(t, "总计 中文目录\r\n")))
	state.Wake()
	screen := screenText(state, 0)
	if !strings.Contains(screen, "总计 中文目录") {
		t.Fatalf("GB18030 output was not decoded:\n%q", screen)
	}
	if strings.Contains(screen, "\uFFFD") {
		t.Fatalf("GB18030 output produced replacement characters:\n%q", screen)
	}
	// A host with an explicit remote charset must not ask the server to switch
	// its locale to UTF-8: the flag reaches sshconn through ShellConfig.
	if len(world.conns) == 0 || len(world.conns[0].configs) == 0 || !world.conns[0].configs[0].SkipUTF8Locale {
		t.Fatal("the SSH session was not told to keep the server's locale")
	}
}

func TestUTF8OutputIsUntouched(t *testing.T) {
	state, _, _, shell := encodingLiveSession(t, "")
	shell.say("总计 中文目录\r\n")
	state.Wake()
	if !strings.Contains(screenText(state, 0), "总计 中文目录") {
		t.Fatalf("UTF-8 output changed:\n%q", screenText(state, 0))
	}
}

func TestGBKOutputSplitAcrossReads(t *testing.T) {
	// Drain can end in the middle of a multibyte character. The two halves must
	// join into one character on the screen, not two replacement runes.
	state, _, _, shell := encodingLiveSession(t, "GBK")
	data := gbkBytes(t, "中文\r\n")
	shell.say(string(data[:3]))
	state.Wake()
	shell.say(string(data[3:]))
	state.Wake()
	if !strings.Contains(screenText(state, 0), "中文") {
		t.Fatalf("split GBK output was not rejoined:\n%q", screenText(state, 0))
	}
	if strings.Contains(screenText(state, 0), "\uFFFD") {
		t.Fatalf("split GBK output produced replacement characters:\n%q", screenText(state, 0))
	}
}

func TestTypedChineseIsEncodedForTheRemoteCharset(t *testing.T) {
	state, _, _, shell := encodingLiveSession(t, "GB18030")
	state.terminalText(state.sessions[0].ID, app.TextEvent{Kind: app.TextInsert, Text: "中文"})
	typed := shell.typed()
	want := gbkBytes(t, "中文")
	if !strings.Contains(typed, string(want)) {
		t.Fatalf("typed Chinese was not encoded to GB18030: %q (want %q)", typed, want)
	}
	if strings.Contains(typed, "中文") {
		t.Fatalf("UTF-8 Chinese leaked into the remote stream: %q", typed)
	}
}

func TestControlKeysAreNotReencoded(t *testing.T) {
	state, _, _, shell := encodingLiveSession(t, "GB18030")
	state.terminalText(state.sessions[0].ID, app.TextEvent{Kind: app.TextInsert, Text: "echo"})
	// Enter is a plain carriage return; encoding must leave it byte-identical.
	if !state.sessions[0].term.writeShellInput([]byte("\r")) {
		t.Fatal("Enter was not written to the shell")
	}
	if !strings.Contains(shell.typed(), "echo\r") {
		t.Fatalf("Enter did not reach the shell unchanged: %q", shell.typed())
	}
}

func TestShellConfigSkipsUTF8LocaleForLegacyCharset(t *testing.T) {
	// The fake connection records the ShellConfig openShell built.
	_, world, _, _ := encodingLiveSession(t, "Big5")
	if len(world.conns) == 0 || len(world.conns[0].configs) == 0 {
		t.Fatal("no shell config was recorded")
	}
	if !world.conns[0].configs[0].SkipUTF8Locale {
		t.Fatal("a Big5 host must not request a UTF-8 LANG")
	}
}

func TestShellConfigKeepsUTF8LocaleByDefault(t *testing.T) {
	_, world, _, _ := encodingLiveSession(t, "")
	if len(world.conns) == 0 || len(world.conns[0].configs) == 0 {
		t.Fatal("no shell config was recorded")
	}
	if world.conns[0].configs[0].SkipUTF8Locale {
		t.Fatal("a UTF-8 host must keep asking the server for a UTF-8 locale")
	}
}
