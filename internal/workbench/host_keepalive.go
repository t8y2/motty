package workbench

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"

	"motty/internal/cmdhistory"
)

// idleKeepAlivePreset is the interval the form offers. The custom preset reads
// the number and unit beside it.
type idleKeepAlivePreset uint8

const (
	idleKeepAliveOff idleKeepAlivePreset = iota
	idleKeepAliveOneMinute
	idleKeepAliveFiveMinutes
	idleKeepAliveTenMinutes
	idleKeepAliveCustom
)

// idleIntervalUnit is the unit of a custom interval. The value is stored in
// seconds whatever the unit the user typed.
type idleIntervalUnit uint8

const (
	idleIntervalSeconds idleIntervalUnit = iota
	idleIntervalMinutes
)

// duration is the fixed interval of a preset, or zero for off and custom.
func (preset idleKeepAlivePreset) duration() time.Duration {
	switch preset {
	case idleKeepAliveOneMinute:
		return time.Minute
	case idleKeepAliveFiveMinutes:
		return 5 * time.Minute
	case idleKeepAliveTenMinutes:
		return 10 * time.Minute
	}
	return 0
}

// idleKeepAliveDuration is the interval the form currently describes. The
// second result is false for a custom value that is not a usable interval; off
// is a valid zero.
func (editor *hostEditor) idleKeepAliveDuration() (time.Duration, bool) {
	if editor.idleKeepAlive != idleKeepAliveCustom {
		return editor.idleKeepAlive.duration(), true
	}
	value, err := strconv.Atoi(strings.TrimSpace(editor.idleInterval.String()))
	if err != nil || value <= 0 {
		return 0, false
	}
	unit := time.Second
	if editor.idleUnit == idleIntervalMinutes {
		unit = time.Minute
	}
	// Check the bound before multiplying: a huge value would wrap into a small,
	// apparently valid duration.
	if int64(value) > int64((24*time.Hour)/unit) {
		return 0, false
	}
	interval := time.Duration(value) * unit
	if !validIdleKeepAliveInterval(interval) {
		return 0, false
	}
	return interval, true
}

// idleInputValue is the send content the form shows: the protocol's default
// until the user picks one.
func (editor *hostEditor) idleInputValue() idleInputMode {
	if editor.idleInputChosen {
		return editor.idleInput
	}
	return idleInputForProtocol(editor.protocol)
}

// idleInputModeForSave keeps an untouched choice out of the saved host, so a
// later protocol change still follows that protocol's default.
func (editor *hostEditor) idleInputModeForSave() idleInputMode {
	if !editor.idleInputChosen {
		return idleInputProtocolDefault
	}
	return editor.idleInput
}

// loadIdleKeepAlive fills the form's probe fields from a host.
func (editor *hostEditor) loadIdleKeepAlive(host Host) {
	editor.idleKeepAlive = idleKeepAliveOff
	editor.idleInterval = text.Editor{}
	editor.idleUnit = idleIntervalSeconds
	editor.idleInput = host.idleInput()
	editor.idleInputChosen = host.IdleInput != idleInputProtocolDefault
	editor.idleInputCustom = text.Editor{}
	editor.idleInputCustom.Insert(host.IdleInputCustom)
	editor.idleKeepAliveOpen, editor.idleUnitOpen, editor.idleInputOpen = false, false, false
	switch host.IdleKeepAlive {
	case 0:
	case time.Minute:
		editor.idleKeepAlive = idleKeepAliveOneMinute
	case 5 * time.Minute:
		editor.idleKeepAlive = idleKeepAliveFiveMinutes
	case 10 * time.Minute:
		editor.idleKeepAlive = idleKeepAliveTenMinutes
	default:
		editor.idleKeepAlive = idleKeepAliveCustom
		if host.IdleKeepAlive%time.Minute == 0 {
			editor.idleUnit = idleIntervalMinutes
			editor.idleInterval.Insert(strconv.Itoa(int(host.IdleKeepAlive / time.Minute)))
		} else {
			editor.idleInterval.Insert(strconv.Itoa(int(host.IdleKeepAlive / time.Second)))
		}
	}
}

// resetIdleKeepAlive clears the probe fields for a new host.
func (editor *hostEditor) resetIdleKeepAlive() {
	editor.idleKeepAlive = idleKeepAliveOff
	editor.idleInterval = text.Editor{}
	editor.idleUnit = idleIntervalSeconds
	editor.idleInput = idleInputProtocolDefault
	editor.idleInputChosen = false
	editor.idleInputCustom = text.Editor{}
	editor.idleKeepAliveOpen, editor.idleUnitOpen, editor.idleInputOpen = false, false, false
}

// maxIdleInputBytes bounds what one idle keepalive may type. It is a probe, not
// a command: a few bytes, never a line that could run something.
const maxIdleInputBytes = 64

var errIdleInputEscapes = errors.New("workbench: invalid idle keepalive input")

// idleInputMode is what an idle keepalive types into a shell. The protocol
// default is kept apart from the explicit choices, so a host that never chose
// one follows its protocol rather than a value frozen when it was saved.
type idleInputMode uint8

const (
	idleInputProtocolDefault idleInputMode = iota
	idleInputCR
	idleInputNUL
	idleInputCustom
)

// idleInputForProtocol is what a host of this protocol sends when it has no
// choice of its own: a Unix shell ignores NUL, while a network device's idle
// timer is reset by the carriage return a person presses.
func idleInputForProtocol(protocol hostProtocol) idleInputMode {
	if protocol == protocolSSH {
		return idleInputNUL
	}
	return idleInputCR
}

// idleInput is the mode this host actually sends.
func (host Host) idleInput() idleInputMode {
	if host.IdleInput == idleInputProtocolDefault {
		return idleInputForProtocol(host.Protocol)
	}
	return host.IdleInput
}

// idleInputBytes is exactly what this host's keepalive writes, or an error when
// the custom text is damaged.
func (host Host) idleInputBytes() ([]byte, error) {
	switch host.idleInput() {
	case idleInputCR:
		return []byte{'\r'}, nil
	case idleInputNUL:
		return []byte{0}, nil
	case idleInputCustom:
		return parseIdleEscapes(host.IdleInputCustom)
	}
	return nil, errIdleInputEscapes
}

// savedIdleInput is the transport's name for the explicit mode. The protocol
// default stays empty, so a host that never chose one follows its protocol
// after a reload instead of freezing today's default into the file.
func (host Host) savedIdleInput() string {
	switch host.IdleInput {
	case idleInputCR:
		return "cr"
	case idleInputNUL:
		return "nul"
	case idleInputCustom:
		return "custom"
	}
	return ""
}

func parseSavedIdleInput(value string) (idleInputMode, bool) {
	switch value {
	case "":
		return idleInputProtocolDefault, true
	case "cr":
		return idleInputCR, true
	case "nul":
		return idleInputNUL, true
	case "custom":
		return idleInputCustom, true
	}
	return idleInputProtocolDefault, false
}

// applyIdleKeepAlive reads the saved setting into the host. A damaged input
// falls back to the protocol default rather than sending bytes nobody chose,
// but a valid interval is kept: one bad field must not discard the whole
// setting. A choice saved while the interval is off is kept too, so reopening
// the form shows what the user last picked.
func (host *Host) applyIdleKeepAlive(transport savedTransport) {
	host.IdleKeepAlive, host.IdleInput, host.IdleInputCustom = 0, idleInputProtocolDefault, ""
	// Bound before multiplying: a hand-edited huge count would wrap into a
	// small, apparently valid interval.
	seconds := int64(transport.IdleKeepAliveSeconds)
	if seconds >= 1 && seconds <= int64(24*time.Hour/time.Second) {
		host.IdleKeepAlive = time.Duration(seconds) * time.Second
	}
	mode, ok := parseSavedIdleInput(transport.IdleInput)
	if !ok {
		return
	}
	if mode == idleInputCustom {
		if _, err := parseIdleEscapes(transport.IdleInputCustom); err != nil {
			return
		}
		host.IdleInputCustom = transport.IdleInputCustom
	}
	host.IdleInput = mode
}

// validIdleKeepAliveInterval reports whether an interval is one this build
// sends at: at least a second (a hotter timer is a busy loop in disguise) and
// at most a day (a longer one could not save a session that expires sooner).
func validIdleKeepAliveInterval(interval time.Duration) bool {
	return interval >= time.Second && interval <= 24*time.Hour
}

// idleKeepAliveDue reports whether the shell has been quiet for a whole
// interval. A zero interval is off, and a clock that moved backwards sends
// nothing rather than treating the negative silence as long enough.
func idleKeepAliveDue(now, lastInput, interval time.Duration) bool {
	if interval <= 0 || now < lastInput {
		return false
	}
	return now-lastInput >= interval
}

// idleKeepAliveProtocol reports whether sessions of this protocol are terminals
// a server may expire for inactivity. A remote desktop has its own heartbeat,
// and a local shell has no server to expire it.
func idleKeepAliveProtocol(protocol hostProtocol) bool {
	switch protocol {
	case protocolSSH, protocolTelnet, protocolSerial:
		return true
	}
	return false
}

// idleSeesSecretPrompt reports whether the line under the cursor is a
// credential question. The wording is the program's own (ssh, su, enable, sudo
// and others), so the test uses the same vocabulary the command recorder uses
// to refuse secrets, and requires the colon or question mark that makes it a
// prompt. A CR must never answer one: it would submit an empty password, and
// sshd or fail2ban may count the failures.
//
// A scrolled-back view cannot show a prompt that just arrived, so the probe
// stands aside while the user is reading history. Soft wrapping can leave the
// prompt word on the row above the cursor, so the previous row is checked when
// the terminal says it wrapped onto this one. A hard newline ends the prompt:
// after "Password:\r\nRouter# " the row above is history, not part of the
// prompt the cursor is on.
func idleSeesSecretPrompt(t *terminal) bool {
	if t == nil || t.screen == nil || t.screen.Modes().AltScreen {
		return false
	}
	if t.screen.ViewOffset() != 0 {
		return true
	}
	cursor := t.screen.Cursor()
	_, rows := t.screen.Size()
	if cursor.Y < 0 || cursor.Y >= rows {
		return true
	}
	line := t.screen.RowText(cursor.Y)
	if cursor.Y > 0 && t.screen.Row(cursor.Y-1).Wrapped {
		line = t.screen.RowText(cursor.Y-1) + " " + line
	}
	return cmdhistory.PasswordPrompt(line) && strings.ContainsAny(line, ":?：？")
}

// idleKeepAliveInterval is the interval this shell was opened with, and whether
// it has one at all. The host is not read here: editing a connected host
// changes its next connection, as every other host setting does.
func idleKeepAliveInterval(session *Session) (time.Duration, bool) {
	if session == nil || session.isLocal() || session.term == nil {
		return 0, false
	}
	t := session.term
	if t.shell == nil || t.ended || t.idleInterval <= 0 {
		return 0, false
	}
	return t.idleInterval, true
}

// idleKeepAliveWait is what Paint asks: how long until this session's probe is
// due. It does no shell I/O. Zero means no timer; a nanosecond when due asks
// the update loop for the frame that runs the probe.
func (state *State) idleKeepAliveWait(session *Session, now time.Duration) time.Duration {
	interval, ok := idleKeepAliveInterval(session)
	if !ok {
		return 0
	}
	t := session.term
	if idleKeepAliveDue(now, t.idleLastInput, interval) {
		return time.Nanosecond
	}
	if now < t.idleLastInput {
		return interval
	}
	return interval - (now - t.idleLastInput)
}

// runIdleKeepAlive decides and writes in the update loop, next to the login
// command, so Paint never touches the shell.
//
// A transfer that owns the shell, a password prompt, an assistant command, a
// completion suggestion, a full-screen program and a partially typed line are
// all states an Enter could disturb or submit, so the probe waits for another
// interval instead. A refused write is retried at the next interval, never in a
// loop.
func (state *State) runIdleKeepAlive(session *Session, now time.Duration) {
	interval, ok := idleKeepAliveInterval(session)
	if !ok {
		state.armIdleKeepAlive(session, 0, now)
		return
	}
	t := session.term
	// Keep a wake scheduled even when the window stops painting (hidden,
	// minimized or covered): the probe must not depend on a repaint. The wait is
	// read in the closure, after this attempt moved idleLastInput.
	defer func() {
		state.armIdleKeepAlive(session, state.idleKeepAliveWait(session, now), now)
	}()
	if !idleKeepAliveDue(now, t.idleLastInput, interval) {
		return
	}
	// The pump before this point is bounded; a prompt that arrived after its
	// budget is only on screen after another drain. This runs in the update
	// loop, never in Paint.
	if t.takeOutput != nil {
		t.takeOutput()
		if t.shell == nil || t.ended {
			return
		}
	}
	// A transfer's binary stream would take a stray CR/NUL as data. The
	// transfer itself writes straight to the shell and does not refresh
	// idleLastInput, so the probe must stand aside for it. transferBusy covers
	// an offered ZMODEM transfer before the user has accepted it.
	if t.transferHoldsInput || session.transferBusy() {
		t.idleLastInput = now
		return
	}
	// A modal question or an input-method composition is waiting for the user;
	// neither has sent a byte to the shell yet.
	if state.modal != modalNone || t.preedit.Composing {
		t.idleLastInput = now
		return
	}
	if len(t.idlePayload) == 0 {
		t.idleLastInput = now
		return
	}
	if t.passwordHint != "" || t.exec != nil || t.suggestionPending != nil ||
		t.screen.Modes().AltScreen || t.promptDirty || idleSeesSecretPrompt(t) {
		t.idleLastInput = now
		return
	}
	// The next check starts from this attempt, whether or not the shell took
	// the bytes: a refused write is retried then, not on the next frame.
	t.idleLastInput = now
	t.sendIdleKeepAlive(t.idlePayload)
}

// armIdleKeepAlive schedules the wake that runs the probe while the window is
// not painting. The timer exists only when the app can wake the window; a
// headless test drives Wake itself.
//
// A pending timer that is no later than the wanted one is kept: when it fires,
// Wake runs this function again with the interval in effect then, so the chain
// continues without touching window state from the timer goroutine.
func (state *State) armIdleKeepAlive(session *Session, wait, now time.Duration) {
	if session == nil || session.term == nil {
		return
	}
	t := session.term
	if wait <= 0 {
		if t.idleTimer != nil {
			t.idleTimer.Stop()
			t.idleTimer = nil
		}
		t.idleTimerAt = 0
		return
	}
	due := now + wait
	// A timer still ahead of now and no later than the wanted one is kept. A
	// timer whose deadline has passed has fired (or is firing): Wake is running
	// because of it, so replace it to continue the chain.
	if t.idleTimer != nil && t.idleTimerAt > now && t.idleTimerAt <= due {
		return
	}
	if t.idleTimer != nil {
		t.idleTimer.Stop()
	}
	invalidate := state.options.Invalidate
	if invalidate == nil {
		t.idleTimer, t.idleTimerAt = nil, 0
		return
	}
	t.idleTimer = time.AfterFunc(wait, invalidate)
	t.idleTimerAt = due
}

// stopIdleKeepAlive drops the wake scheduled for a shell that is gone. Its only
// job is to wake the window for a probe, and a closed shell has none to run.
func (t *terminal) stopIdleKeepAlive() {
	if t == nil || t.idleTimer == nil {
		return
	}
	t.idleTimer.Stop()
	t.idleTimer = nil
	t.idleTimerAt = 0
}

// requestIdleKeepAliveFrames asks for the frame in which each connected
// terminal is next due for its probe. A session with no timer asks for none.
func (state *State) requestIdleKeepAliveFrames(canvas *gfx.Canvas) {
	now := state.now()
	for index := range state.sessions {
		if wait := state.idleKeepAliveWait(&state.sessions[index], now); wait > 0 {
			canvas.RequestFrameIn(wait)
		}
	}
}

// parseIdleEscapes turns the text shown in the form into the bytes written to a
// shell. It accepts the escapes a terminal user knows: \r, \n, \t, \xNN and \\
// for a literal backslash. Anything else is refused, so a typo cannot become
// input with a meaning nobody asked for.
func parseIdleEscapes(text string) ([]byte, error) {
	out := make([]byte, 0, min(len(text), maxIdleInputBytes))
	for index := 0; index < len(text); index++ {
		if text[index] != '\\' {
			out = append(out, text[index])
			continue
		}
		if index+1 == len(text) {
			return nil, fmt.Errorf("%w: trailing backslash", errIdleInputEscapes)
		}
		index++
		switch text[index] {
		case 'r':
			out = append(out, '\r')
		case 'n':
			out = append(out, '\n')
		case 't':
			out = append(out, '\t')
		case '\\':
			out = append(out, '\\')
		case 'x':
			if index+2 >= len(text) {
				return nil, fmt.Errorf("%w: short hex escape", errIdleInputEscapes)
			}
			value, ok := hexByte(text[index+1], text[index+2])
			if !ok {
				return nil, fmt.Errorf("%w: bad hex escape", errIdleInputEscapes)
			}
			out = append(out, value)
			index += 2
		default:
			return nil, fmt.Errorf("%w: unknown escape \\%c", errIdleInputEscapes, text[index])
		}
	}
	if len(out) == 0 || len(out) > maxIdleInputBytes {
		return nil, fmt.Errorf("%w: must be 1 to %d bytes", errIdleInputEscapes, maxIdleInputBytes)
	}
	return out, nil
}

func hexByte(high, low byte) (byte, bool) {
	highNibble, okHigh := hexNibble(high)
	lowNibble, okLow := hexNibble(low)
	if !okHigh || !okLow {
		return 0, false
	}
	return highNibble<<4 | lowNibble, true
}

func hexNibble(value byte) (byte, bool) {
	switch {
	case value >= '0' && value <= '9':
		return value - '0', true
	case value >= 'a' && value <= 'f':
		return value - 'a' + 10, true
	case value >= 'A' && value <= 'F':
		return value - 'A' + 10, true
	}
	return 0, false
}
