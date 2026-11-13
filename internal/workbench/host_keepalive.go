package workbench

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
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

// applyIdleKeepAlive reads the saved setting into the host. A damaged value
// falls back to off and the protocol default rather than sending bytes nobody
// chose; a choice saved while the interval is off is kept, so reopening the
// form shows what the user last picked.
func (host *Host) applyIdleKeepAlive(transport savedTransport) {
	host.IdleKeepAlive, host.IdleInput, host.IdleInputCustom = 0, idleInputProtocolDefault, ""
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
	interval := time.Duration(transport.IdleKeepAliveSeconds) * time.Second
	if validIdleKeepAliveInterval(interval) {
		host.IdleKeepAlive = interval
	}
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

// idleSecretPromptRe recognizes the line a program uses to ask for a password
// or passphrase. The wording is the program's own (ssh, su, enable, sudo and
// others), so the match is on the visible line, not on the sudo offer the
// password hint tracks. A CR must never answer one: it would submit an empty
// password, and sshd or fail2ban may count the failures.
var idleSecretPromptRe = regexp.MustCompile(`(?i)(password|passphrase|密码|口令)[^:?：？]*[:?：？]`)

// idleSeesSecretPrompt reports whether the line under the cursor is a
// credential question. A line the user has typed but not submitted is not one:
// promptDirty keeps the probe away from a partial command as well.
func idleSeesSecretPrompt(t *terminal) bool {
	if t == nil || t.screen == nil || t.screen.Modes().AltScreen {
		return false
	}
	cursor := t.screen.Cursor()
	_, rows := t.screen.Size()
	if cursor.Y < 0 || cursor.Y >= rows {
		return false
	}
	return idleSecretPromptRe.MatchString(t.screen.RowText(cursor.Y))
}

// idleKeepAliveInterval is the configured interval of a session that has a
// shell to probe, and whether it has one at all.
func idleKeepAliveInterval(session *Session) (time.Duration, bool) {
	if session == nil || session.isLocal() {
		return 0, false
	}
	host, t := session.host, session.term
	if t == nil || t.shell == nil || t.ended || !idleKeepAliveProtocol(host.Protocol) {
		return 0, false
	}
	interval := host.IdleKeepAlive
	if interval <= 0 {
		return 0, false
	}
	return interval, true
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
		return
	}
	t := session.term
	if !idleKeepAliveDue(now, t.idleLastInput, interval) {
		return
	}
	// A transfer's binary stream would take a stray CR/NUL as data. The
	// transfer itself writes straight to the shell and does not refresh
	// idleLastInput, so the probe must stand aside for it.
	if t.transferHoldsInput {
		t.idleLastInput = now
		return
	}
	payload, err := session.host.idleInputBytes()
	if err != nil {
		// A damaged custom setting is repaired by the form or the next load;
		// until then, do not ask for a frame every paint.
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
	t.sendIdleKeepAlive(payload)
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
