package workbench

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/ui"
	"motty/internal/cmdhistory"
)

func newCompletionPreviewWorld(t *testing.T, echoPreview bool) *completionWorld {
	t.Helper()
	w := newCompletionWorld(t)
	w.session.term.shell = completionPOSIXShell{w.shell}
	w.session.term.integrated = true
	w.session.term.previewClearBound = true
	w.state.completion.previewEnabled = true
	w.shell.onWrite = func(data []byte) {
		if len(data) > 0 && data[0] == 0x15 {
			if echoPreview {
				w.shell.say("\r\x1b[2K$ " + string(data[1:]))
			}
			return
		}
		w.shell.say(strings.ReplaceAll(string(data), "\r", "\r\n"))
	}
	w.type_("cat -")
	w.state.openCompletions()
	if !w.state.completion.list.active {
		t.Fatal("preview fixture has no completion list")
	}
	return w
}

func TestCompletionPreviewRequiresCurrentClearBinding(t *testing.T) {
	w := newCompletionPreviewWorld(t, true)
	w.session.term.previewClearBound = false
	before := w.shell.typed()
	w.state.Key(ui.KeyEvent{Key: ui.KeyDown, Target: terminalHit(w.state, 0)})
	if got := w.shell.typed(); got != before {
		t.Fatalf("unsupported binding received an editing sequence: %q", got)
	}
}

func TestCompletionPreviewClearBindingExpiresAtNextPrompt(t *testing.T) {
	w := newCompletionPreviewWorld(t, true)
	w.state.closeCompletionList()
	w.shell.say("\x1b]133;A;motty=" + w.session.term.folderToken + "\a$ \x1b]133;B;motty=" + w.session.term.folderToken + "\a")
	w.state.Wake()
	if w.session.term.previewClearBound {
		t.Fatal("line-clear capability survived a prompt without a new report")
	}
}

func TestCompletionPreviewAcceptsCurrentOwnedBindingReport(t *testing.T) {
	w := newCompletionPreviewWorld(t, true)
	w.state.closeCompletionList()
	w.session.term.previewClearBound = false
	w.state.typed(w.session, []byte("\r"))
	w.state.Wake()
	token := w.session.term.folderToken
	w.shell.say("\r\x1b[2K\x1b]133;C;motty=" + token + "\a\x1b]777;motty-input;" + token + ";utf8;;ctrl-u\a\x1b]133;A;motty=" + token + "\a$ \x1b]133;B;motty=" + token + "\a")
	w.state.Wake()
	if !w.session.term.previewClearBound {
		t.Fatal("owned report did not establish a current shell binding")
	}
	w.type_("cat -")
	w.state.openCompletions()
	before := w.shell.typed()
	w.state.Key(ui.KeyEvent{Key: ui.KeyDown, Target: terminalHit(w.state, 0)})
	if got := w.shell.typed(); got != before+"\x15cat -e" {
		t.Fatalf("verified prompt report did not enable preview: %q", got)
	}
}

func TestCompletionPreviewHistorySelectionDoesNotConfirmPreviousCatalog(t *testing.T) {
	for _, selectHistory := range []string{"key", "click"} {
		t.Run(selectHistory, func(t *testing.T) {
			w := newCompletionPreviewWorld(t, true)
			w.state.closeCompletionList()
			w.state.history.ghost = map[string][]cmdhistory.Entry{
				historyKey(w.session): {{Command: "cat -n history", Used: time.Now(), Count: 2}},
			}
			w.state.history.on = true
			w.state.openCompletions()
			if len(w.state.completion.list.candidates) < 2 || w.state.completion.list.candidates[0].FromCatalog {
				t.Fatal("expected history before catalog candidates")
			}
			w.state.Key(ui.KeyEvent{Key: ui.KeyDown, Target: terminalHit(w.state, 0)})
			w.state.Wake()
			if selectHistory == "key" {
				w.state.Key(ui.KeyEvent{Key: ui.KeyHome, Target: terminalHit(w.state, 0)})
			} else {
				w.state.Click(hitCompletionList.At(0))
			}
			w.state.Wake()
			if selectHistory == "key" {
				w.state.Key(ui.KeyEvent{Key: ui.KeyEnter, Target: terminalHit(w.state, 0)})
			}
			if got := w.line(t); got != "cat -n history" {
				t.Fatalf("history selection confirmed %q instead of the selected history row", got)
			}
		})
	}
}

func TestCompletionPreviewDrainsPendingOutputBeforeAnotherWrite(t *testing.T) {
	for _, key := range []ui.Key{ui.KeyEscape, ui.KeyDown, ui.KeyEnter} {
		t.Run(fmt.Sprint(key), func(t *testing.T) {
			w := newCompletionPreviewWorld(t, true)
			w.state.Key(ui.KeyEvent{Key: ui.KeyDown, Target: terminalHit(w.state, 0)})
			w.state.Wake()
			before := w.shell.typed()
			w.shell.say("\r\nPassword: ")
			w.state.Key(ui.KeyEvent{Key: key, Target: terminalHit(w.state, 0)})
			if got := w.shell.typed(); got != before {
				t.Fatalf("pending password output followed by %v wrote %q", key, got)
			}
			if w.state.completion.preview.phase != previewIdle {
				t.Fatal("pending password output left a confirmed preview active")
			}
		})
	}
}

func TestCompletionPreviewSelectionAndEscapeRestore(t *testing.T) {
	w := newCompletionPreviewWorld(t, true)
	before := w.shell.typed()
	w.state.Key(ui.KeyEvent{Key: ui.KeyDown, Target: terminalHit(w.state, 0)})
	if got := w.shell.typed(); got != before+"\x15cat -e" {
		t.Fatalf("selected row preview wrote %q", got)
	}
	w.state.Wake()
	if got := w.line(t); got != "cat -e" {
		t.Fatalf("confirmed preview line = %q", got)
	}
	w.state.Key(ui.KeyEvent{Key: ui.KeyEscape, Target: terminalHit(w.state, 0)})
	if got := w.shell.typed(); got != before+"\x15cat -e\x15cat -" {
		t.Fatalf("Escape wrote %q, want the original line", got)
	}
	w.state.Wake()
	if got := w.line(t); got != "cat -" || w.state.completion.list.active {
		t.Fatalf("restored line = %q; list active=%v", got, w.state.completion.list.active)
	}
}

func TestCompletionPreviewFirstEnterDoesNotExecute(t *testing.T) {
	w := newCompletionPreviewWorld(t, true)
	w.state.Key(ui.KeyEvent{Key: ui.KeyDown, Target: terminalHit(w.state, 0)})
	w.state.Wake()
	before := w.shell.typed()
	w.state.Key(ui.KeyEvent{Key: ui.KeyEnter, Target: terminalHit(w.state, 0)})
	if w.shell.typed() != before || w.state.completion.list.active {
		t.Fatalf("first Enter executed or kept the list: %q", w.shell.typed())
	}
	w.state.Key(ui.KeyEvent{Key: ui.KeyEnter, Target: terminalHit(w.state, 0)})
	if got := w.shell.typed(); got != before+"\r" {
		t.Fatalf("second Enter was not sent to the shell: %q", got)
	}
}

func TestCompletionPreviewQueuesSelectionUntilEcho(t *testing.T) {
	w := newCompletionPreviewWorld(t, false)
	before := w.shell.typed()
	w.state.Key(ui.KeyEvent{Key: ui.KeyDown, Target: terminalHit(w.state, 0)})
	w.state.Key(ui.KeyEvent{Key: ui.KeyDown, Target: terminalHit(w.state, 0)})
	if got := w.shell.typed(); got != before+"\x15cat -e" {
		t.Fatalf("second selection wrote before the first echo: %q", got)
	}
	w.shell.say("\r\x1b[2K$ cat -e")
	w.state.Wake()
	if got := w.shell.typed(); got != before+"\x15cat -e\x15cat -n" {
		t.Fatalf("queued selection did not follow the first echo: %q", got)
	}
}

func TestCompletionPreviewMismatchNeverGuessesARestore(t *testing.T) {
	w := newCompletionPreviewWorld(t, false)
	w.state.Key(ui.KeyEvent{Key: ui.KeyDown, Target: terminalHit(w.state, 0)})
	before := w.shell.typed()
	w.shell.say("\r\x1b[2K$ different")
	w.state.Wake()
	w.state.Key(ui.KeyEvent{Key: ui.KeyEscape, Target: terminalHit(w.state, 0)})
	if strings.Contains(w.shell.typed()[len(before):], "\x15") || w.state.completion.list.active {
		t.Fatalf("mismatched echo led to another shell write: %q", w.shell.typed())
	}
}

func TestCompletionPreviewTypingCommitsWithoutRollback(t *testing.T) {
	w := newCompletionPreviewWorld(t, true)
	w.state.Key(ui.KeyEvent{Key: ui.KeyDown, Target: terminalHit(w.state, 0)})
	w.state.Wake()
	before := w.shell.typed()
	w.type_("x")
	if got := w.shell.typed(); got != before+"x" || w.state.completion.list.active {
		t.Fatalf("normal input after preview = %q", got)
	}
}

func TestCompletionPreviewUnsupportedOrPrivateSessionDoesNotWrite(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*completionWorld)
	}{
		{"not integrated", func(w *completionWorld) { w.session.term.integrated = false }},
		{"unknown shell", func(w *completionWorld) { w.session.term.shell = w.shell }},
		{"NoHistory", func(w *completionWorld) { w.session.host.NoHistory = true }},
		{"IME", func(w *completionWorld) { w.session.term.preedit.Composing = true }},
		{"AltScreen", func(w *completionWorld) { w.shell.say("\x1b[?1049h"); w.state.Wake() }},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := newCompletionPreviewWorld(t, true)
			test.edit(w)
			before := w.shell.typed()
			w.state.Key(ui.KeyEvent{Key: ui.KeyDown, Target: terminalHit(w.state, 0)})
			if strings.Contains(w.shell.typed()[len(before):], "\x15") {
				t.Fatalf("unsafe preview wrote to shell: %q", w.shell.typed())
			}
		})
	}
}

func TestCompletionPreviewClickWaitsForTheChosenCandidate(t *testing.T) {
	w := newCompletionPreviewWorld(t, false)
	w.state.Key(ui.KeyEvent{Key: ui.KeyDown, Target: terminalHit(w.state, 0)})
	w.state.Click(hitCompletionList.At(2))
	before := w.shell.typed()
	if !strings.HasSuffix(before, "\x15cat -e") {
		t.Fatalf("click wrote before the first echo: %q", before)
	}
	w.shell.say("\r\x1b[2K$ cat -e")
	w.state.Wake()
	if got := w.shell.typed(); got != before+"\x15cat -n" {
		t.Fatalf("click did not preview its own selection: %q", got)
	}
	w.shell.say("\r\x1b[2K$ cat -n")
	w.state.Wake()
	if got := w.line(t); got != "cat -n" || w.state.completion.list.active {
		t.Fatalf("clicked candidate not committed: line=%q active=%v", got, w.state.completion.list.active)
	}
}

func TestCompletionPreviewEscapeWaitsForEchoBeforeRestoring(t *testing.T) {
	w := newCompletionPreviewWorld(t, false)
	w.state.Key(ui.KeyEvent{Key: ui.KeyDown, Target: terminalHit(w.state, 0)})
	before := w.shell.typed()
	w.state.Key(ui.KeyEvent{Key: ui.KeyEscape, Target: terminalHit(w.state, 0)})
	if w.shell.typed() != before {
		t.Fatalf("Escape guessed a restore before echo: %q", w.shell.typed())
	}
	w.shell.say("\r\x1b[2K$ cat -e")
	w.state.Wake()
	if got := w.shell.typed(); got != before+"\x15cat -" {
		t.Fatalf("Escape did not restore after echo: %q", got)
	}
	w.shell.say("\r\x1b[2K$ cat -")
	w.state.Wake()
	if w.state.completion.list.active {
		t.Fatal("restored preview left the list open")
	}
}

func TestCompletionPreviewTimeoutAndReconnectDoNotWriteRollback(t *testing.T) {
	for _, failure := range []string{"timeout", "reconnect"} {
		t.Run(failure, func(t *testing.T) {
			w := newCompletionPreviewWorld(t, false)
			w.state.Key(ui.KeyEvent{Key: ui.KeyDown, Target: terminalHit(w.state, 0)})
			before := w.shell.typed()
			if failure == "timeout" {
				w.clock += completionPreviewWait + 1
			} else {
				w.session.conn = &fakeConn{}
			}
			w.state.Wake()
			if w.shell.typed() != before || w.state.completion.list.active || w.state.status != w.state.messages.Completion.PreviewUncertain {
				t.Fatalf("%s guessed a rollback or hid uncertainty: input=%q status=%q", failure, w.shell.typed(), w.state.status)
			}
		})
	}
}

func TestCompletionPreviewFitsTheActualPromptWidth(t *testing.T) {
	if previewFitsLine(12, "cat -e", 18) {
		t.Fatal("a long prompt left no cell for the candidate without wrapping")
	}
	if !previewFitsLine(2, "cat -e", 18) {
		t.Fatal("a short prompt and candidate should fit")
	}
}

func TestCompletionPreviewFocusChangeDropsQueuedRestore(t *testing.T) {
	w := newCompletionPreviewWorld(t, false)
	w.state.Key(ui.KeyEvent{Key: ui.KeyDown, Target: terminalHit(w.state, 0)})
	w.state.Key(ui.KeyEvent{Key: ui.KeyEscape, Target: terminalHit(w.state, 0)})
	before := w.shell.typed()
	w.state.activeSection = sectionHosts
	w.shell.say("\r\x1b[2K$ cat -e")
	w.state.Wake()
	if got := w.shell.typed(); got != before || w.state.completion.list.active {
		t.Fatalf("inactive terminal received a delayed restore: %q", got)
	}
}

func TestCompletionPreviewPasswordPromptStopsPendingWrites(t *testing.T) {
	w := newCompletionPreviewWorld(t, false)
	w.state.Key(ui.KeyEvent{Key: ui.KeyDown, Target: terminalHit(w.state, 0)})
	before := w.shell.typed()
	w.shell.say("\r\nPassword: ")
	w.state.Wake()
	if got := w.shell.typed(); got != before || w.state.completion.list.active {
		t.Fatalf("password prompt received a preview write: %q", got)
	}
}

func TestCompletionPreviewRepeatedSelectionWritesOncePerEcho(t *testing.T) {
	w := newCompletionPreviewWorld(t, false)
	w.state.Key(ui.KeyEvent{Key: ui.KeyDown, Target: terminalHit(w.state, 0)})
	w.state.Key(ui.KeyEvent{Key: ui.KeyEnd, Target: terminalHit(w.state, 0)})
	w.state.Key(ui.KeyEvent{Key: ui.KeyEnd, Target: terminalHit(w.state, 0)})
	if count := strings.Count(w.shell.typed(), "\x15"); count != 1 {
		t.Fatalf("repeated selection wrote %d times before echo", count)
	}
	w.shell.say("\r\x1b[2K$ cat -e")
	w.state.Wake()
	if got := w.shell.typed(); !strings.HasSuffix(got, "\x15cat -v") || strings.Count(got, "\x15") != 2 {
		t.Fatalf("queued selection was not written exactly once: %q", got)
	}
}
