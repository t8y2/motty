package workbench

import (
	"crypto/md5"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"motty/internal/trzsz"
)

// The TRZSZ path through the workbench: an announcement arrives in text, the
// offer dialog takes the user's choice, and from then on the protocol bytes are
// taken from the screen like a ZMODEM transfer's. The remote side is scripted
// here; internal/trzsz has the real programs at its other end.

const trzszAnnouncement = "\x1b[s::TRZSZ:TRANSFER:%c:1.2.0:0000001234567:0\r\n"

type trzszScript struct {
	t       *testing.T
	shell   *fakeShell
	step    int
	sending bool
	typed   []byte
	dir     bool
	// stall leaves the last data message unanswered, so the transfer stays
	// running and the dialog's cancel path can be exercised.
	stall bool
}

func (s *trzszScript) write(p []byte) {
	s.typed = append(s.typed, p...)
	line := strings.TrimSuffix(string(p), "\n")
	switch {
	case strings.HasPrefix(line, "#ACT:"):
		payload, err := trzsz.DecodeText(strings.TrimPrefix(line, "#ACT:"), 1<<20)
		if err != nil {
			s.t.Errorf("the client's action did not decode: %v", err)
			return
		}
		var action map[string]any
		if err := json.Unmarshal(payload, &action); err != nil {
			s.t.Errorf("the client's action is not JSON: %v", err)
			return
		}
		if action["confirm"] != true {
			s.t.Errorf("action confirm = %v, want true", action["confirm"])
			return
		}
		if action["binary"] != false {
			s.t.Errorf("action binary = %v, want false", action["binary"])
		}
		config, _ := json.Marshal(map[string]any{
			"protocol": 1, "binary": false, "directory": s.dir, "timeout": 20,
		})
		s.say("#CFG:" + trzsz.EncodeText(config) + "\n")
		if s.sending {
			s.step = 1 // wait for NUM
		} else {
			s.say("#NUM:1\n")
			s.step = 2 // wait for SUCC of NUM
		}
	case !s.sending && s.step == 2 && strings.HasPrefix(line, "#SUCC:"):
		s.say("#NAME:" + trzsz.EncodeText([]byte("hello.txt")) + "\n")
		s.step = 3
	case !s.sending && s.step == 3 && strings.HasPrefix(line, "#SUCC:"):
		s.say("#SIZE:5\n")
		s.step = 4
	case !s.sending && s.step == 4 && strings.HasPrefix(line, "#SUCC:"):
		s.say("#DATA:" + trzsz.EncodeText([]byte("hello")) + "\n")
		if s.stall {
			s.step = 5
			return
		}
		s.step = 5
	case !s.sending && s.step == 5 && strings.HasPrefix(line, "#SUCC:"):
		sum := md5.Sum([]byte("hello"))
		s.say("#MD5:" + trzsz.EncodeText(sum[:]) + "\n")
		s.step = 6
	case s.sending && s.step == 1 && strings.HasPrefix(line, "#NUM:1"):
		s.say("#SUCC:1\n")
		s.step = 2
	case s.sending && s.step == 2 && strings.HasPrefix(line, "#NAME:"):
		s.say("#SUCC:" + trzsz.EncodeText([]byte("hello.txt")) + "\n")
		s.step = 3
	case s.sending && s.step == 3 && strings.HasPrefix(line, "#SIZE:5"):
		s.say("#SUCC:5\n")
		s.step = 4
	case s.sending && s.step == 4 && strings.HasPrefix(line, "#DATA:"):
		s.say("#SUCC:5\n")
		s.step = 5
	case s.sending && s.step == 5 && strings.HasPrefix(line, "#MD5:"):
		sum := md5.Sum([]byte("hello"))
		s.say("#SUCC:" + trzsz.EncodeText(sum[:]) + "\n")
		s.step = 6
	}
}

func (s *trzszScript) say(text string) { s.shell.say(text) }

func TestTrzszAnnouncementIsOfferedWithoutShowingItsBytes(t *testing.T) {
	state, session, shell := transferState(t)
	dir := t.TempDir()
	state.options.OpenDirectory = func(app.OpenDirectoryOptions) (string, error) { return dir, nil }
	script := &trzszScript{t: t, shell: shell}
	shell.onWrite = func(p []byte) { script.write(p) }

	shell.say("before\r\n" + strings.Replace(trzszAnnouncement, "%c", "S", 1))
	waitUntil(t, state, "the TRZSZ offer", func() bool { return state.modal == modalFileTransfer })
	if session.transfer.protocol != transferTrzsz || session.transfer.phase != transferOffered {
		t.Fatalf("transfer = %+v", session.transfer)
	}
	if session.transfer.sending {
		t.Fatal("a tsz announcement was read as an upload")
	}
	if !session.term.transferHoldsInput {
		t.Fatal("the shell was not held while the offer waited")
	}
	screen := screenText(state, 0)
	if !strings.Contains(screen, "before") {
		t.Fatalf("screen = %q: the output before the announcement is gone", screen)
	}
	if strings.Contains(screen, "TRZSZ") {
		t.Fatalf("screen = %q: the announcement was shown", screen)
	}

	state.Click(hitTransferChoose)
	waitUntil(t, state, "the transfer to end", func() bool { return session.transfer.phase == transferNone })
	got, err := os.ReadFile(filepath.Join(dir, "hello.txt"))
	if err != nil || string(got) != "hello" {
		t.Fatalf("hello.txt = %q, %v", got, err)
	}
	if script.step != 6 {
		t.Fatalf("the script stopped at step %d", script.step)
	}
	if session.term.transferHoldsInput {
		t.Fatal("the shell is still held after the transfer")
	}
	if !strings.Contains(screenText(state, 0), "Received 1 files") {
		t.Fatalf("screen = %q", screenText(state, 0))
	}
}

func TestTrzszOfferCanBeDeclined(t *testing.T) {
	state, session, shell := transferState(t)
	script := &trzszScript{t: t, shell: shell}
	shell.onWrite = func(p []byte) { script.typed = append(script.typed, p...) }
	shell.say(strings.Replace(trzszAnnouncement, "%c", "S", 1))
	waitUntil(t, state, "the TRZSZ offer", func() bool { return state.modal == modalFileTransfer })
	state.Click(hitTransferDecline)
	waitUntil(t, state, "the dialog to close", func() bool { return state.modal == modalNone })
	if session.transfer.phase != transferNone || session.term.transferHoldsInput {
		t.Fatalf("after declining: transfer %+v, holds input %v", session.transfer, session.term.transferHoldsInput)
	}
	decoded := string(script.typed)
	if !strings.Contains(decoded, "#ACT:") {
		t.Fatalf("the remote was told %q, want an action", decoded)
	}
	payload, err := trzsz.DecodeText(strings.TrimPrefix(strings.TrimSpace(decoded), "#ACT:"), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	var action map[string]any
	if err := json.Unmarshal(payload, &action); err != nil {
		t.Fatal(err)
	}
	if action["confirm"] != false {
		t.Fatalf("declined action = %v", action)
	}
}

func TestTrzszUploadIsChosenFromFiles(t *testing.T) {
	state, session, shell := transferState(t)
	source := filepath.Join(t.TempDir(), "hello.txt")
	if err := os.WriteFile(source, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	state.options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{source}, nil }
	script := &trzszScript{t: t, shell: shell, sending: true}
	shell.onWrite = func(p []byte) { script.write(p) }
	shell.say(strings.Replace(trzszAnnouncement, "%c", "R", 1))
	waitUntil(t, state, "the TRZSZ offer", func() bool { return state.modal == modalFileTransfer })
	if !session.transfer.sending {
		t.Fatal("a trz announcement was read as a download")
	}
	state.Click(hitTransferChoose)
	waitUntil(t, state, "the transfer to end", func() bool { return session.transfer.phase == transferNone })
	if script.step != 6 {
		t.Fatalf("the script stopped at step %d", script.step)
	}
	if !strings.Contains(screenText(state, 0), "Sent 1 files") {
		t.Fatalf("screen = %q", screenText(state, 0))
	}
}

func TestTrzszAnnouncementCutBetweenReadsIsStillOffered(t *testing.T) {
	state, _, shell := transferState(t)
	line := strings.Replace(trzszAnnouncement, "%c", "S", 1)
	cut := strings.Index(line, "TRANSFER") + 3
	shell.say(line[:cut])
	state.Wake()
	if state.modal == modalFileTransfer {
		t.Fatal("a cut announcement opened the offer")
	}
	shell.say(line[cut:])
	waitUntil(t, state, "the TRZSZ offer", func() bool { return state.modal == modalFileTransfer })
}

func TestTrzszDownloadCanBeCancelledFromTheCard(t *testing.T) {
	state, session, shell := transferState(t)
	dir := t.TempDir()
	state.options.OpenDirectory = func(app.OpenDirectoryOptions) (string, error) { return dir, nil }
	script := &trzszScript{t: t, shell: shell, stall: true}
	shell.onWrite = func(p []byte) { script.write(p) }
	shell.say(strings.Replace(trzszAnnouncement, "%c", "S", 1))
	waitUntil(t, state, "the TRZSZ offer", func() bool { return state.modal == modalFileTransfer })
	state.Click(hitTransferChoose)
	waitUntil(t, state, "the running transfer", func() bool { return session.transfer.phase == transferRunning })
	state.Click(hitTransferCancelFor.At(int(session.ID)))
	waitUntil(t, state, "the transfer to end", func() bool { return session.transfer.phase == transferNone })
	if _, err := os.Stat(filepath.Join(dir, "hello.txt")); !os.IsNotExist(err) {
		t.Fatalf("a cancelled download left %v", err)
	}
	if !strings.Contains(string(script.typed), "#fail:") {
		t.Fatalf("the remote was told %q, want a failure", script.typed)
	}
	if !strings.Contains(screenText(state, 0), "File transfer cancelled.") {
		t.Fatalf("screen = %q", screenText(state, 0))
	}
}
