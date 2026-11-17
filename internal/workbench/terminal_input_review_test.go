package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

func TestTerminalInputPublicPasteRoutesRequireChoice(t *testing.T) {
	for _, menu := range []bool{false, true} {
		name := "shortcut"
		if menu {
			name = "context-menu"
		}
		t.Run(name, func(t *testing.T) {
			state, _, _, shell := liveSession(t)
			session := &state.sessions[0]
			inputGuardPrompt(state, session, shell, "utf8", "")
			state.options.ClipboardText = func() (string, error) { return "tail\u3000-500f", nil }
			if menu {
				state.Paint(&gfx.Canvas{}, gfx.Size{Width: 1400, Height: 900})
				rightClick(state, session, 400, 300)
				state.Paint(&gfx.Canvas{}, gfx.Size{Width: 1400, Height: 900})
				state.Click(hitTerminalMenuPaste)
			} else {
				chord := state.shortcuts.chords.Chord(shortcutPaste)
				result := state.Shortcut(gfx.KeyEvent{Target: terminalHit(state, 0), Text: string(chord.Char), Modifiers: chord.Modifiers})
				if !result.Handled {
					t.Fatal("paste shortcut was not handled")
				}
			}
			if shell.typed() != "" || state.modal != modalTerminalInput {
				t.Fatalf("public paste bypassed confirmation: input=%q modal=%v", shell.typed(), state.modal)
			}
			state.Click(hitInputReplace)
			if shell.typed() != "tail -500f" {
				t.Fatalf("confirmed clipboard text changed or executed: %q", shell.typed())
			}
		})
	}
}

func TestTerminalInputDrainsAlreadyReceivedTransitionsBeforeOpeningAndConfirming(t *testing.T) {
	for _, action := range []string{"confirm-glyph", "confirm-locale", "open-glyph", "open-locale"} {
		t.Run(action, func(t *testing.T) {
			state, _, _, shell := liveSession(t)
			session := &state.sessions[0]
			inputGuardPrompt(state, session, shell, "other", "C.UTF-8")
			if action == "confirm-locale" {
				state.Click(hitInputLocaleFix)
			}
			if action == "confirm-glyph" {
				state.terminalText(session.ID, app.TextEvent{Kind: app.TextInsert, Text: "\u3000"})
			}
			// The output already arrived, but the event loop has not called Wake.
			shell.say("\x1b]133;C;motty=" + session.term.folderToken + "\x07Password: ")
			want := ""
			switch action {
			case "open-glyph":
				state.terminalText(session.ID, app.TextEvent{Kind: app.TextInsert, Text: "\u3000"})
				want = "\u3000" // A real password/program input remains unchanged.
			case "open-locale":
				state.Click(hitInputLocaleFix)
			default:
				state.Click(hitInputReplace)
			}
			if got := shell.typed(); got != want || state.modal == modalTerminalInput {
				t.Fatalf("queued transition ignored: input=%q want=%q modal=%v", got, want, state.modal)
			}
		})
	}
}

func TestTerminalInputPairedQuotesSurviveLiteralContentAndTrackedDeletion(t *testing.T) {
	for _, body := range []string{"it's fine", "ab\x7f"} {
		t.Run(body, func(t *testing.T) {
			state, _, _, shell := liveSession(t)
			session := &state.sessions[0]
			inputGuardPrompt(state, session, shell, "utf8", "")
			state.terminalText(session.ID, app.TextEvent{Kind: app.TextInsert, Text: "echo \u201c"})
			state.Click(hitInputReplace)
			if body == "ab\x7f" {
				state.terminalText(session.ID, app.TextEvent{Kind: app.TextInsert, Text: "ab"})
				state.terminalKey(session, gfx.KeyEvent{Key: gfx.KeyBackspace})
			} else {
				state.terminalText(session.ID, app.TextEvent{Kind: app.TextInsert, Text: body})
			}
			state.terminalText(session.ID, app.TextEvent{Kind: app.TextInsert, Text: "\u201d"})
			if state.modal != modalTerminalInput {
				t.Fatalf("closing smart quote escaped protection after literal editing: %q", shell.typed())
			}
			state.Click(hitInputReplace)
			want := "echo \"" + body + "\""
			if shell.typed() != want {
				t.Fatalf("explicit quote repair altered body/ran command: got=%q want=%q", shell.typed(), want)
			}
		})
	}
}

func TestTerminalInputUnsupportedSubstitutionGrammarRemainsTransparent(t *testing.T) {
	for _, prefix := range []string{"echo <(printf ", "echo >(printf ", "echo ${name:-", "echo $[1+", "echo {name,"} {
		t.Run(prefix, func(t *testing.T) {
			state, _, _, shell := liveSession(t)
			session := &state.sessions[0]
			inputGuardPrompt(state, session, shell, "utf8", "")
			state.terminalText(session.ID, app.TextEvent{Kind: app.TextInsert, Text: prefix})
			state.terminalText(session.ID, app.TextEvent{Kind: app.TextInsert, Text: "\u2014"})
			if state.modal != modalNone || shell.typed() != prefix+"\u2014" {
				t.Fatalf("unsupported syntax was rewritten/intercepted: %q modal=%v", shell.typed(), state.modal)
			}
		})
	}
}

func TestTerminalInputPublicPastePreservesTransparentCases(t *testing.T) {
	for _, mode := range []string{"multiline", "control", "alternate", "broadcast", "unknown", "quoted", "substitution", "heredoc"} {
		t.Run(mode, func(t *testing.T) {
			state, _, _, shell := liveSession(t)
			session := &state.sessions[0]
			inputGuardPrompt(state, session, shell, "utf8", "")
			value, want := "\u3000", "\u3000"
			switch mode {
			case "multiline":
				value, want = "echo \u3000\nnext", "echo \u3000\rnext"
			case "control":
				value, want = "\x1b[0m\u3000", "\x1b[0m\u3000"
			case "alternate":
				shell.say("\x1b[?1049h")
				state.Wake()
			case "broadcast":
				state.broadcast = state.deck.ActiveWorkspace()
			case "unknown":
				session.term.resetFolderSync()
			case "quoted":
				state.terminalText(session.ID, app.TextEvent{Kind: app.TextInsert, Text: "cat '中文"})
			case "substitution":
				state.terminalText(session.ID, app.TextEvent{Kind: app.TextInsert, Text: "echo $(printf "})
			case "heredoc":
				state.terminalText(session.ID, app.TextEvent{Kind: app.TextInsert, Text: "cat <<EOF "})
			}
			before := shell.typed()
			state.options.ClipboardText = func() (string, error) { return value, nil }
			chord := state.shortcuts.chords.Chord(shortcutPaste)
			state.Shortcut(gfx.KeyEvent{Target: terminalHit(state, 0), Text: string(chord.Char), Modifiers: chord.Modifiers})
			if shell.typed() != before+want || state.modal != modalNone {
				t.Fatalf("public paste %s changed: got=%q want=%q modal=%v", mode, shell.typed(), before+want, state.modal)
			}
		})
	}
}

func TestTerminalInputBracketedPasteRetainsAppendOnlyProtection(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	inputGuardPrompt(state, session, shell, "utf8", "")
	shell.say("\x1b[?2004h")
	state.Wake()
	state.options.ClipboardText = func() (string, error) { return "tail ", nil }
	chord := state.shortcuts.chords.Chord(shortcutPaste)
	state.Shortcut(gfx.KeyEvent{Target: terminalHit(state, 0), Text: string(chord.Char), Modifiers: chord.Modifiers})
	if shell.typed() != "\x1b[200~tail \x1b[201~" {
		t.Fatalf("native bracketed paste wrapper changed: %q", shell.typed())
	}
	state.terminalText(session.ID, app.TextEvent{Kind: app.TextInsert, Text: "\u3000"})
	if state.modal != modalTerminalInput {
		t.Fatal("a known single-line bracketed paste retired append-only input protection")
	}
	state.Click(hitInputReplace)
	if shell.typed() != "\x1b[200~tail \x1b[201~ " {
		t.Fatalf("post-paste correction executed or altered original paste: %q", shell.typed())
	}
}

func TestTerminalInputUnparsedReceivedBacklogCannotAuthorizeActions(t *testing.T) {
	for _, action := range []string{"confirm-glyph", "confirm-locale", "open-glyph", "open-locale"} {
		t.Run(action, func(t *testing.T) {
			state, _, _, shell := liveSession(t)
			session := &state.sessions[0]
			inputGuardPrompt(state, session, shell, "other", "C.UTF-8")
			if action == "confirm-locale" {
				state.Click(hitInputLocaleFix)
			}
			if action == "confirm-glyph" {
				state.terminalText(session.ID, app.TextEvent{Kind: app.TextInsert, Text: "\u3000"})
			}
			state.options.Invalidate = func() {}
			session.term.pumpBudget = 0
			shell.say(strings.Repeat("x", 2*pumpSlice+16) + "\x1b]133;C;motty=" + session.term.folderToken + "\x07Password: ")
			want := ""
			switch action {
			case "open-glyph":
				state.terminalText(session.ID, app.TextEvent{Kind: app.TextInsert, Text: "\u3000"})
				want = "\u3000"
			case "open-locale":
				state.Click(hitInputLocaleFix)
			default:
				state.Click(hitInputReplace)
			}
			if len(session.term.rx) == 0 {
				t.Fatal("fixture did not exercise production output parsing budget")
			}
			if shell.typed() != want || state.modal == modalTerminalInput {
				t.Fatalf("unparsed received output authorized action: input=%q want=%q modal=%v backlog=%d", shell.typed(), want, state.modal, len(session.term.rx))
			}
			if session.term.inputGuard.prefix != "" {
				t.Fatal("uncertain program input was retained in the command tracker")
			}
		})
	}
}
