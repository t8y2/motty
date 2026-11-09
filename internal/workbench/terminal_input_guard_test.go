package workbench

import (
	"fmt"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

func inputGuardPrompt(state *State, session *Session, shell *fakeShell, charset, locale string) {
	token := session.term.folderToken
	shell.say("\x1b]133;C;motty=" + token + "\x07\x1b]777;motty-input;" + token + ";" + charset + ";" + locale + "\x07\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07")
	state.Wake()
}

func TestTerminalIMEConfirmEnterNeverReachesShell(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	state.terminalText(session.ID, app.TextEvent{Kind: app.TextSetMarked, Text: "ni"})
	state.terminalKey(session, gfx.KeyEvent{Key: gfx.KeyEnter})
	if shell.typed() != "" {
		t.Fatalf("candidate confirmation executed shell input %q", shell.typed())
	}
	state.terminalText(session.ID, app.TextEvent{Kind: app.TextInsert, Text: "你"})
	state.terminalKey(session, gfx.KeyEvent{Key: gfx.KeyEnter})
	if shell.typed() != "你\r" {
		t.Fatalf("committed UTF-8/next Enter lost: %q", shell.typed())
	}
}

func TestTerminalSuspiciousInputRequiresExplicitChoiceWithoutExecuting(t *testing.T) {
	for _, action := range []string{"replace", "keep", "cancel"} {
		t.Run(action, func(t *testing.T) {
			state, _, _, shell := liveSession(t)
			session := &state.sessions[0]
			inputGuardPrompt(state, session, shell, "utf8", "")
			state.terminalText(session.ID, app.TextEvent{Kind: app.TextInsert, Text: "tail "})
			state.terminalText(session.ID, app.TextEvent{Kind: app.TextInsert, Text: "\u2014500f"})
			if shell.typed() != "tail " || state.modal != modalTerminalInput {
				t.Fatalf("suspicious text reached shell before choice: %q modal=%v", shell.typed(), state.modal)
			}
			id, want := hitInputReplace, "tail -500f"
			if action == "keep" {
				id, want = hitInputKeep, "tail \u2014500f"
			} else if action == "cancel" {
				id, want = hitInputCancel, "tail "
			}
			state.Click(id)
			if got := shell.typed(); got != want || strings.ContainsAny(got, "\r\n") || state.modal != modalNone {
				t.Fatalf("choice changed/executed command: got=%q want=%q modal=%v", got, want, state.modal)
			}
		})
	}
}

func TestTerminalInputProtectionLeavesLiteralAndUnknownInputUntouched(t *testing.T) {
	for _, mode := range []string{"quoted", "escaped", "chinese", "no-integration", "no-shell-report", "program", "password-no-marker", "alternate", "cursor-edit", "substitution", "multiline", "control-text", "broadcast", "raw-keyboard"} {
		t.Run(mode, func(t *testing.T) {
			state, _, _, shell := liveSession(t)
			session := &state.sessions[0]
			inputGuardPrompt(state, session, shell, "utf8", "")
			prefix, value := "", "\u3000"
			switch mode {
			case "quoted":
				prefix, value = "cat '/中文", "\u3000文件\u2014'"
			case "escaped":
				prefix = "cat \\"
			case "chinese":
				value = "中文路径.txt"
			case "no-integration":
				session.term.resetFolderSync()
			case "no-shell-report":
				session.term.resetFolderSync()
				token := session.term.folderToken
				shell.say("\x1b]133;C;motty=" + token + "\x07\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07")
				state.Wake()
			case "program":
				shell.say("\x1b]133;C;motty=" + session.term.folderToken + "\x07Password: ")
				state.Wake()
			case "password-no-marker":
				shell.say("\r\nPassword: ")
				state.Wake()
			case "alternate":
				shell.say("\x1b[?1049h")
				state.Wake()
			case "cursor-edit":
				state.terminalKey(session, gfx.KeyEvent{Key: gfx.KeyLeft})
			case "substitution":
				prefix = "echo $(printf "
			case "multiline":
				value = "echo \u3000\nnext"
			case "control-text":
				value = "\x1b[0m\u3000"
			case "broadcast":
				state.broadcast = state.deck.ActiveWorkspace()
			case "raw-keyboard":
				shell.say("\x1b[>1u")
				state.Wake()
			}
			state.terminalText(session.ID, app.TextEvent{Kind: app.TextInsert, Text: prefix})
			before := shell.typed()
			state.terminalText(session.ID, app.TextEvent{Kind: app.TextInsert, Text: value})
			if got := shell.typed(); got != before+value || state.modal != modalNone {
				t.Fatalf("%s input changed/intercepted: got=%q want=%q modal=%v", mode, got, before+value, state.modal)
			}
		})
	}
}

func TestTerminalInputConfirmationCannotOutliveOwnerOrPrompt(t *testing.T) {
	for _, change := range []string{"prompt", "close", "section", "broadcast", "overlay"} {
		t.Run(change, func(t *testing.T) {
			state, world, _, shell := liveSession(t)
			session := &state.sessions[0]
			owner := session.ID
			if change == "close" {
				liveTab(t, state, world, 1)
				state.ActivatePane(owner)
				session, _ = state.sessionByIDPointer(owner)
			}
			inputGuardPrompt(state, session, shell, "utf8", "")
			state.terminalText(owner, app.TextEvent{Kind: app.TextInsert, Text: "\u3000"})
			if state.modal != modalTerminalInput {
				t.Fatal("fixture did not open input confirmation")
			}
			switch change {
			case "prompt":
				inputGuardPrompt(state, session, shell, "utf8", "")
			case "close":
				state.closeSession(owner)
			case "section":
				state.activeSection = sectionHosts
			case "broadcast":
				state.broadcast = state.deck.ActiveWorkspace()
			case "overlay":
				state.transfers.open = true
			}
			state.Click(hitInputReplace)
			if shell.typed() != "" {
				t.Fatalf("stale %s confirmation wrote %q", change, shell.typed())
			}
		})
	}
}

func TestTerminalLocaleRepairRequiresConfirmedCurrentIdlePrompt(t *testing.T) {
	for _, mode := range []string{"idle", "already-typing", "typed-after-dialog", "no-candidate", "broadcast"} {
		t.Run(mode, func(t *testing.T) {
			state, _, _, shell := liveSession(t)
			session := &state.sessions[0]
			candidate := "C.UTF-8"
			if mode == "no-candidate" {
				candidate = ""
			}
			inputGuardPrompt(state, session, shell, "other", candidate)
			if mode == "already-typing" {
				state.terminalText(session.ID, app.TextEvent{Kind: app.TextInsert, Text: "tail "})
			} else if mode == "broadcast" {
				state.broadcast = state.deck.ActiveWorkspace()
			}
			state.Click(hitInputLocaleFix)
			if mode == "typed-after-dialog" {
				state.typed(session, []byte("pending"))
			}
			before := shell.typed()
			state.Click(hitInputReplace)
			if mode == "idle" {
				if got := shell.typed(); got != "builtin export LC_ALL='C.UTF-8'\r" {
					t.Fatalf("repair was not explicit validated current-session export: %q", got)
				}
			} else if got := shell.typed(); got != before {
				t.Fatalf("unsafe %s repair modified shell: before=%q got=%q", mode, before, got)
			}
		})
	}
}

func TestTerminalSmartQuoteChoiceDoesNotAlterLaterQuotedPaths(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	inputGuardPrompt(state, session, shell, "utf8", "")
	state.terminalText(session.ID, app.TextEvent{Kind: app.TextInsert, Text: "\u201c"})
	state.Click(hitInputReplace)
	state.terminalText(session.ID, app.TextEvent{Kind: app.TextInsert, Text: "first\" \"中文"})
	state.terminalText(session.ID, app.TextEvent{Kind: app.TextInsert, Text: "文件\u201d.txt\""})
	if state.modal != modalNone || shell.typed() != "\"first\" \"中文文件\u201d.txt\"" {
		t.Fatalf("a former smart quote changed a later literal path: modal=%v got=%q", state.modal, shell.typed())
	}
}

func TestTerminalPairedSmartQuoteChoicesNeverExecute(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	inputGuardPrompt(state, session, shell, "utf8", "")
	for _, text := range []string{"echo ", "\u201c", "中文", "\u201d"} {
		state.terminalText(session.ID, app.TextEvent{Kind: app.TextInsert, Text: text})
		if state.modal == modalTerminalInput {
			state.Click(hitInputReplace)
		}
	}
	if shell.typed() != "echo \"中文\"" {
		t.Fatalf("paired smart quotes were not repaired as explicitly chosen text: %q", shell.typed())
	}
}

func TestTerminalInputChoiceRetainsPendingOnTransportRefusal(t *testing.T) {
	for _, repair := range []bool{false, true} {
		t.Run(fmt.Sprint(repair), func(t *testing.T) {
			state, _, _, shell := liveSession(t)
			session := &state.sessions[0]
			if repair {
				inputGuardPrompt(state, session, shell, "other", "C.UTF-8")
				state.Click(hitInputLocaleFix)
			} else {
				inputGuardPrompt(state, session, shell, "utf8", "")
				state.terminalText(session.ID, app.TextEvent{Kind: app.TextInsert, Text: "\u2014"})
			}
			shell.mu.Lock()
			shell.refuse = true
			shell.mu.Unlock()
			state.Click(hitInputReplace)
			if state.modal != modalTerminalInput || shell.typed() != "" {
				t.Fatalf("refused input was lost or reported as sent: modal=%v input=%q", state.modal, shell.typed())
			}
			shell.mu.Lock()
			shell.refuse = false
			shell.mu.Unlock()
			state.Click(hitInputReplace)
			want := "-"
			if repair {
				want = "builtin export LC_ALL='C.UTF-8'\r"
			}
			if shell.typed() != want || state.modal != modalNone {
				t.Fatalf("retry did not preserve the chosen input: %q", shell.typed())
			}
		})
	}
}

func TestTerminalInputLocaleNoticeSteadyPaintHasNoAllocations(t *testing.T) {
	state, _, _, shell := liveSession(t)
	inputGuardPrompt(state, &state.sessions[0], shell, "other", "C.UTF-8")
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1000, Height: 700}
	state.Paint(&canvas, size)
	if got := testing.AllocsPerRun(100, func() { canvas.Reset(); state.Paint(&canvas, size) }); got != 0 {
		t.Fatalf("steady charset notice paint allocated %v times", got)
	}
}

func TestTerminalInputLocaleNoticeFitsPTYAndDoesNotJumpOnEveryCommand(t *testing.T) {
	state, _, _, shell := liveSession(t)
	state.Resize(gfx.Size{Width: 1000, Height: 700})
	session := &state.sessions[0]
	inputGuardPrompt(state, session, shell, "other", "C.UTF-8")
	checkSize := func() {
		t.Helper()
		cols, rows := session.term.screen.Size()
		geometry := state.layoutForSession(session.ID)
		if cols != geometry.cols || rows != geometry.rows {
			t.Fatalf("notice did not resize terminal/PTY: %dx%d vs geometry %dx%d", cols, rows, geometry.cols, geometry.rows)
		}
	}
	checkSize()
	state.typed(session, []byte("ls\r"))
	if !state.terminalInputNoticeVisible(session) {
		t.Fatal("charset warning collapsed/resized on each command instead of keeping stable session geometry")
	}
	state.Click(hitInputLocaleDismiss)
	checkSize()
}

// Entering a full-screen program must not take rows away from the PTY and give
// them back on exit: vim and tmux would see a resize each time.
func TestTerminalInputLocaleNoticeKeepsItsRowsInAFullScreenProgram(t *testing.T) {
	state, _, _, shell := liveSession(t)
	state.Resize(gfx.Size{Width: 1000, Height: 700})
	session := &state.sessions[0]
	inputGuardPrompt(state, session, shell, "other", "C.UTF-8")
	check := func() {
		t.Helper()
		cols, rows := session.term.screen.Size()
		geometry := state.layoutForSession(session.ID)
		if cols != geometry.cols || rows != geometry.rows {
			t.Fatalf("terminal %dx%d does not match geometry %dx%d", cols, rows, geometry.cols, geometry.rows)
		}
		if !state.terminalInputNoticeVisible(session) {
			t.Fatal("charset notice collapsed inside a full-screen program")
		}
	}
	check()
	_, before := session.term.screen.Size()
	session.term.screen.Write([]byte("\x1b[?1049h"))
	state.Wake()
	check()
	if _, rows := session.term.screen.Size(); rows != before {
		t.Fatalf("entering the alternate screen changed rows %d -> %d", before, rows)
	}
	session.term.screen.Write([]byte("\x1b[?1049l"))
	state.Wake()
	check()
}

// A composing terminal must not swallow the Enter of a program running on the
// alternate screen: the input method owns that key there, not the shell.
func TestTerminalIMECompositionKeepsAFullScreenProgramsEnter(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	state.terminalText(session.ID, app.TextEvent{Kind: app.TextSetMarked, Text: "ni"})
	session.term.screen.Write([]byte("\x1b[?1049h"))
	state.terminalKey(session, gfx.KeyEvent{Key: gfx.KeyEnter})
	if shell.typed() != "\r" {
		t.Fatalf("full-screen Enter was swallowed: %q", shell.typed())
	}
}
