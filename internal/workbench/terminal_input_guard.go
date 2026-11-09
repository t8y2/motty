package workbench

import (
	"bytes"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"motty/internal/term"
)

var inputGuardHits = tree.Feature("terminal-input-guard")
var (
	hitInputReplace       = tree.ID(inputGuardHits, "replace")
	hitInputKeep          = tree.ID(inputGuardHits, "keep")
	hitInputCancel        = tree.ID(inputGuardHits, "cancel")
	hitInputClose         = tree.ID(inputGuardHits, "close")
	hitInputDismiss       = tree.ID(inputGuardHits, "dismiss")
	hitInputLocaleFix     = tree.ID(inputGuardHits, "locale-fix")
	hitInputLocaleDismiss = tree.ID(inputGuardHits, "locale-dismiss")
)

const maxInputGuardBytes = 4096

type terminalInputGuard struct {
	environment term.InputEnvironment
	epoch       uint64
	active      bool
	known       bool
	prefix      string
	smartQuote  rune
	dismissed   bool
	repairing   bool
}

type terminalInputConfirmation struct {
	sessionID  uint32
	owner      *terminal
	token      string
	epoch      uint64
	version    uint64
	event      app.TextEvent
	fixed      string
	detail     string
	smartQuote rune
	locale     string
	error      string
}

func (g *terminalInputGuard) start() {
	g.epoch++
	g.active, g.known, g.prefix, g.smartQuote = false, true, "", 0
}

func (g *terminalInputGuard) observe(input []byte) {
	if !g.active {
		return
	}
	if len(input) >= 12 && bytes.HasPrefix(input, []byte("\x1b[200~")) && bytes.HasSuffix(input, []byte("\x1b[201~")) {
		input = input[6 : len(input)-6]
	}
	if strings.ContainsAny(string(input), "\r\n\x03\x04") {
		g.start()
		return
	}
	if !g.known {
		return
	}
	if !utf8.Valid(input) || len(g.prefix)+len(input) > maxInputGuardBytes {
		g.known, g.prefix = false, ""
		return
	}
	for _, r := range string(input) {
		switch {
		case r == 0x7f || r == 8:
			last, size := utf8.DecodeLastRuneInString(g.prefix)
			if size == 0 || last > 127 && g.environment.Charset != "utf8" {
				g.known, g.prefix = false, ""
				return
			}
			g.prefix = g.prefix[:len(g.prefix)-size]
			g.retainSmartQuote()
		case r < 0x20:
			g.known, g.prefix = false, ""
			return
		default:
			g.prefix += string(r)
			if r == '\'' || r == '"' {
				g.retainSmartQuote()
			}
		}
	}
}

func (g *terminalInputGuard) retainSmartQuote() {
	if g.smartQuote == 0 {
		return
	}
	expected := '"'
	if g.smartQuote == '\u2019' {
		expected = '\''
	}
	prefix := shellInputPrefix{}
	for _, r := range g.prefix {
		prefix.advance(r)
	}
	if prefix.quote != expected {
		g.smartQuote = 0
	}
}

type shellInputPrefix struct {
	quote            rune
	escaped, comment bool
}

func (p *shellInputPrefix) advance(r rune) {
	if p.comment {
		return
	}
	if p.escaped {
		p.escaped = false
	} else if r == '\\' && p.quote != '\'' {
		p.escaped = true
	} else if r == p.quote {
		p.quote = 0
	} else if p.quote == 0 && (r == '\'' || r == '"') {
		p.quote = r
	} else if p.quote == 0 && r == '#' {
		p.comment = true
	}
}

// This recognizes only an append-only, simple shell line. It is deliberately
// not a shell parser: uncertain grammar and quoted literals remain untouched.
func commandGlyphFix(prefix, value string, smart rune) (fixed, detail string, nextSmart rune) {
	unsafeControl := strings.IndexFunc(value, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0
	if unsafeControl || len(prefix)+len(value) > maxInputGuardBytes || strings.ContainsAny(prefix+value, "\r\n`({") || strings.Contains(prefix+value, "$[") || strings.Contains(prefix+value, "<<") {
		return value, "", smart
	}
	context := shellInputPrefix{}
	for _, r := range prefix {
		context.advance(r)
	}
	nextSmart = smart
	var out strings.Builder
	for _, r := range value {
		replacement := r
		if !context.escaped && !context.comment && (context.quote == 0 || r == nextSmart) {
			switch r {
			case '\u3000', '\u00a0':
				replacement = ' '
			case '\u2010', '\u2011', '\u2012', '\u2013', '\u2014', '\u2212', '\ufe63', '\uff0d':
				replacement = '-'
			case '\u2018', '\u2019':
				replacement = '\''
			case '\u201c', '\u201d':
				replacement = '"'
			}
		}
		if replacement != r {
			if detail == "" {
				detail = fmt.Sprintf("%q (U+%04X) -> %q (U+%04X)", r, r, replacement, replacement)
			}
			if replacement == '\'' || replacement == '"' {
				if context.quote == 0 {
					if replacement == '\'' {
						nextSmart = '\u2019'
					} else {
						nextSmart = '\u201d'
					}
				} else {
					nextSmart = 0
				}
			}
		}
		out.WriteRune(replacement)
		context.advance(replacement)
	}
	return out.String(), detail, nextSmart
}

func (state *State) inputGuardAvailable(session *Session) bool {
	t := session.term
	return state.modal == modalNone && state.activeSection == sectionTerminal && state.activeSessionID == session.ID && state.broadcast == 0 && !state.transfers.open && state.knownHosts.action == knownHostsActionNone &&
		t != nil && t.shell != nil && !t.ended && t.exec == nil && len(t.rx) == 0 && t.inputGuard.active && t.inputGuard.environment.Charset != "" && t.commandReporting && t.folderToken != "" &&
		session.desk == nil && session.containerCommand == "" && !t.screen.Modes().AltScreen && t.screen.KeyboardFlags() == 0 && !t.screen.Modes().Win32Input &&
		!(state.transfer.phase != transferNone && state.transfer.sessionID == session.ID)
}

func (state *State) guardTerminalText(session *Session, event app.TextEvent) bool {
	current := state.inputOwnerAfterDrain(session.ID, session.term)
	if current == nil {
		state.status = state.messages.TerminalInput.Stale
		return true
	}
	session = current
	if state.modal != modalNone || !state.inputGuardAvailable(session) || !session.term.inputGuard.known ||
		(event.Kind != app.TextInsert && event.Kind != app.TextPaste) {
		return false
	}
	g := &session.term.inputGuard
	fixed, detail, smart := commandGlyphFix(g.prefix, event.Text, g.smartQuote)
	if fixed == event.Text {
		return false
	}
	state.inputConfirmation = state.inputAsk(session)
	state.inputConfirmation.event, state.inputConfirmation.fixed = event, fixed
	state.inputConfirmation.detail, state.inputConfirmation.smartQuote = detail, smart
	state.modal = modalTerminalInput
	return true
}

func (state *State) inputAsk(session *Session) terminalInputConfirmation {
	t := session.term
	return terminalInputConfirmation{sessionID: session.ID, owner: t, token: t.folderToken, epoch: t.inputGuard.epoch, version: t.promptVersion}
}

func (state *State) inputOwnerAfterDrain(id uint32, owner *terminal) *Session {
	session, ok := state.sessionByIDPointer(id)
	if !ok || owner == nil || session.term != owner {
		return nil
	}
	if owner.takeOutput != nil {
		owner.takeOutput()
	}
	session, ok = state.sessionByIDPointer(id)
	if !ok || session.term != owner {
		return nil
	}
	// A bounded pump may leave a received transition unparsed. Retire the
	// proof rather than blocking the UI to drain an unbounded output stream.
	if len(owner.rx) > 0 {
		owner.inputGuard.start()
	}
	state.fitTerminal(session)
	return session
}

func (state *State) inputLocaleRepairReady(session *Session) bool {
	return state.inputGuardAvailable(session) && session.term.promptReady && !session.term.promptDirty &&
		session.term.inputGuard.environment.Charset == "other" && session.term.inputGuard.environment.Locale != ""
}

func (state *State) clickTerminalInput(id gfx.ID) bool {
	if state.modal != modalTerminalInput {
		if state.modal != modalNone || id != hitInputLocaleFix && id != hitInputLocaleDismiss {
			return false
		}
		session, ok := state.sessionByIDPointer(state.activeSessionID)
		if !ok || session.term == nil {
			return true
		}
		if id == hitInputLocaleDismiss {
			session.term.inputGuard.dismissed = true
			state.fitTerminal(session)
			return true
		}
		session = state.inputOwnerAfterDrain(session.ID, session.term)
		if session == nil {
			return true
		}
		if state.inputLocaleRepairReady(session) {
			state.inputConfirmation = state.inputAsk(session)
			state.inputConfirmation.locale = session.term.inputGuard.environment.Locale
			state.modal = modalTerminalInput
		}
		return true
	}
	if id != hitInputReplace && id != hitInputKeep && id != hitInputCancel && id != hitInputClose && id != hitInputDismiss {
		return true
	}
	ask := state.inputConfirmation
	state.closeModal()
	if id != hitInputReplace && id != hitInputKeep {
		state.status = state.messages.TerminalInput.Canceled
		if ask.locale != "" {
			state.status = state.messages.TerminalInput.LocaleCanceled
		}
		return true
	}
	session := state.inputOwnerAfterDrain(ask.sessionID, ask.owner)
	if session == nil || !state.inputGuardAvailable(session) || session.term.folderToken != ask.token ||
		session.term.inputGuard.epoch != ask.epoch || session.term.promptVersion != ask.version {
		state.status = state.messages.TerminalInput.Stale
		return true
	}
	t := session.term
	if ask.locale != "" {
		if id == hitInputReplace && state.inputLocaleRepairReady(session) && t.inputGuard.environment.Locale == ask.locale {
			t.inputGuard.repairing = t.sendAssistant([]byte("builtin export LC_ALL=" + shellQuote(ask.locale) + "\r"))
			if !t.inputGuard.repairing {
				state.retryInputChoice(ask)
			}
		}
		return true
	}
	value := ask.event.Text
	if id == hitInputReplace {
		value = ask.fixed
	}
	input := []byte(value)
	if ask.event.Kind == app.TextPaste {
		input = term.EncodePaste(value, t.screen.Modes().BracketedPaste)
	}
	if !t.sendInput(input, false) {
		state.retryInputChoice(ask)
	} else if id == hitInputReplace {
		t.inputGuard.smartQuote = ask.smartQuote
	}
	return true
}

func (state *State) retryInputChoice(ask terminalInputConfirmation) {
	ask.error = state.messages.TerminalInput.SendFailed
	state.inputConfirmation = ask
	state.modal = modalTerminalInput
}

func (state *State) terminalInputKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if state.modal != modalTerminalInput {
		return gfx.EventResult{}, false
	}
	if !event.Repeat && !event.Release && event.Modifiers == 0 {
		if event.Key == gfx.KeyEscape {
			state.clickTerminalInput(hitInputCancel)
		}
		if (event.Key == gfx.KeyEnter || event.Key == gfx.KeySpace) && (event.Target == hitInputReplace || event.Target == hitInputKeep || event.Target == hitInputCancel) {
			state.clickTerminalInput(event.Target)
		}
	}
	return gfx.EventResult{Handled: event.Key != gfx.KeyTab, Repaint: true}, true
}

func (state *State) receiveInputEnvironment(sessionID uint32, environment term.InputEnvironment) {
	if session, ok := state.sessionByIDPointer(sessionID); ok && session.term != nil {
		g := &session.term.inputGuard
		g.environment = environment
		if g.repairing {
			g.repairing = false
			message, kind := state.messages.TerminalInput.LocaleFailed, components.ToastWarning
			if environment.Charset == "utf8" {
				message, kind = state.messages.TerminalInput.LocaleSuccess, components.ToastSuccess
			}
			state.fileNotices.show(state.messages.TerminalInput.LocaleTitle, message, kind)
		}
	}
}

func (state *State) terminalInputNoticeVisible(session *Session) bool {
	t := session.term
	// The bar keeps this height while the warning applies, including inside a
	// full-screen program: hiding it there would add rows to the PTY on entry
	// and take them back on exit, and vim/tmux would see a resize each time.
	return t != nil && t.shell != nil && !t.ended && t.commandReporting && !t.inputGuard.dismissed &&
		t.inputGuard.environment.Charset == "other"
}
