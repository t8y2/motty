package workbench

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/text"

	"motty/internal/charset"
	"motty/internal/serial"
)

func textEditorWith(value string) text.Editor {
	var editor text.Editor
	editor.Insert(value)
	return editor
}

func TestIdleKeepAliveIntervalBounds(t *testing.T) {
	for _, interval := range []time.Duration{time.Second, time.Minute, 24 * time.Hour} {
		if !validIdleKeepAliveInterval(interval) {
			t.Fatalf("validIdleKeepAliveInterval(%v) = false", interval)
		}
	}
	for _, interval := range []time.Duration{0, -time.Second, 24*time.Hour + time.Second} {
		if validIdleKeepAliveInterval(interval) {
			t.Fatalf("validIdleKeepAliveInterval(%v) = true", interval)
		}
	}
}

// The probe waits for a whole interval of silence: sending while the user is
// still typing would put bytes into their line.
func TestIdleKeepAliveDueOnlyAfterTheInterval(t *testing.T) {
	cases := []struct {
		name      string
		now       time.Duration
		lastInput time.Duration
		interval  time.Duration
		want      bool
	}{
		{name: "before the interval", now: 59 * time.Second, lastInput: 0, interval: time.Minute, want: false},
		{name: "exactly at the interval", now: time.Minute, lastInput: 0, interval: time.Minute, want: true},
		{name: "after the interval", now: 2 * time.Minute, lastInput: 0, interval: time.Minute, want: true},
		{name: "just typed", now: 2 * time.Minute, lastInput: 2 * time.Minute, interval: time.Minute, want: false},
		{name: "off", now: time.Hour, lastInput: 0, interval: 0, want: false},
		{name: "clock went backwards", now: time.Minute, lastInput: 2 * time.Minute, interval: time.Minute, want: false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := idleKeepAliveDue(test.now, test.lastInput, test.interval); got != test.want {
				t.Fatalf("idleKeepAliveDue(%v, %v, %v) = %v, want %v", test.now, test.lastInput, test.interval, got, test.want)
			}
		})
	}
}

// A host that leaves the send content unset follows its protocol: NUL is the
// quietest input a Unix shell takes, while a network device's idle timer wants
// the carriage return a person would press.
func TestIdleInputDefaultsFollowTheProtocol(t *testing.T) {
	cases := []struct {
		name     string
		protocol hostProtocol
		want     idleInputMode
	}{
		{name: "ssh", protocol: protocolSSH, want: idleInputNUL},
		{name: "telnet", protocol: protocolTelnet, want: idleInputCR},
		{name: "serial", protocol: protocolSerial, want: idleInputCR},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			host := Host{Protocol: test.protocol}
			if got := host.idleInput(); got != test.want {
				t.Fatalf("idleInput = %v, want %v", got, test.want)
			}
		})
	}
}

// The custom choice is written the way it is shown: plain text with the
// escapes a terminal user already knows.
func TestIdleInputEscapesParseIntoBytes(t *testing.T) {
	cases := []struct {
		name string
		text string
		want []byte
	}{
		{name: "carriage return", text: `\r`, want: []byte{'\r'}},
		{name: "null", text: `\x00`, want: []byte{0}},
		{name: "mixed", text: `a\r\n\t\x1b\\z`, want: []byte{'a', '\r', '\n', '\t', 0x1b, '\\', 'z'}},
		{name: "plain text", text: "hello", want: []byte("hello")},
		{name: "utf8 text", text: "你好", want: []byte("你好")},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseIdleEscapes(test.text)
			if err != nil {
				t.Fatalf("parseIdleEscapes(%q): %v", test.text, err)
			}
			if !bytes.Equal(got, test.want) {
				t.Fatalf("parseIdleEscapes(%q) = %v, want %v", test.text, got, test.want)
			}
		})
	}
}

// A damaged escape or a payload that cannot be one keystroke is refused rather
// than sent to a shell.
func TestIdleInputEscapesRejectDamagedOrOversizedText(t *testing.T) {
	cases := []struct {
		name string
		text string
	}{
		{name: "empty", text: ""},
		{name: "unknown escape", text: `\q`},
		{name: "short hex", text: `\x0`},
		{name: "bad hex", text: `\xzz`},
		{name: "trailing backslash", text: `abc\`},
		{name: "too long", text: strings.Repeat("a", maxIdleInputBytes+1)},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseIdleEscapes(test.text); err == nil {
				t.Fatalf("parseIdleEscapes(%q) accepted the text", test.text)
			}
		})
	}
	if _, err := parseIdleEscapes(strings.Repeat("a", maxIdleInputBytes)); err != nil {
		t.Fatalf("the largest allowed input was refused: %v", err)
	}
}

func TestHostIdleInputBytesUsesItsChoice(t *testing.T) {
	cases := []struct {
		name string
		host Host
		want []byte
	}{
		{name: "carriage return", host: Host{IdleInput: idleInputCR}, want: []byte{'\r'}},
		{name: "null", host: Host{IdleInput: idleInputNUL}, want: []byte{0}},
		{name: "custom", host: Host{IdleInput: idleInputCustom, IdleInputCustom: `\x00hi`}, want: []byte{0, 'h', 'i'}},
		{name: "ssh default", host: Host{Protocol: protocolSSH}, want: []byte{0}},
		{name: "telnet default", host: Host{Protocol: protocolTelnet}, want: []byte{'\r'}},
		{name: "serial default", host: Host{Protocol: protocolSerial}, want: []byte{'\r'}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.host.idleInputBytes()
			if err != nil {
				t.Fatalf("idleInputBytes: %v", err)
			}
			if !bytes.Equal(got, test.want) {
				t.Fatalf("idleInputBytes = %v, want %v", got, test.want)
			}
		})
	}
}

func TestHostIdleInputBytesRefusesADamagedCustomChoice(t *testing.T) {
	host := Host{IdleInput: idleInputCustom, IdleInputCustom: `\q`}
	if _, err := host.idleInputBytes(); err == nil {
		t.Fatal("a damaged custom choice produced bytes to send")
	}
}

// The setting belongs to the saved host, whether it is SSH, Telnet or serial:
// each protocol's transport writes it before its own fields.
func TestIdleKeepAliveRoundTripsForEveryTerminalProtocol(t *testing.T) {
	line, err := serial.ParseLine("115200 8N1", serial.FlowNone)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		host Host
	}{
		{name: "ssh", host: Host{Protocol: protocolSSH, IdleKeepAlive: 5 * time.Minute, IdleInput: idleInputCustom, IdleInputCustom: `\x00`}},
		{name: "telnet", host: Host{Protocol: protocolTelnet, IdleKeepAlive: time.Minute, IdleInput: idleInputNUL}},
		{name: "serial", host: Host{Protocol: protocolSerial, Line: line, IdleKeepAlive: 10 * time.Minute, IdleInput: idleInputCR}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			transport := test.host.transport()
			if transport.IdleKeepAliveSeconds != int(test.host.IdleKeepAlive/time.Second) {
				t.Fatalf("saved interval = %d, want %d", transport.IdleKeepAliveSeconds, int(test.host.IdleKeepAlive/time.Second))
			}
			var restored Host
			if !restored.applyTransport(transport) {
				t.Fatal("applyTransport refused the saved host")
			}
			if restored.IdleKeepAlive != test.host.IdleKeepAlive {
				t.Fatalf("interval = %v, want %v", restored.IdleKeepAlive, test.host.IdleKeepAlive)
			}
			if restored.idleInput() != test.host.idleInput() {
				t.Fatalf("input = %v, want %v", restored.idleInput(), test.host.idleInput())
			}
			if restored.IdleInputCustom != test.host.IdleInputCustom {
				t.Fatalf("custom input = %q, want %q", restored.IdleInputCustom, test.host.IdleInputCustom)
			}
		})
	}
}

func TestIdleKeepAliveIsOffAndDefaultWhenTheSavedEntrySaysNothing(t *testing.T) {
	var host Host
	if !host.applyTransport(savedTransport{Protocol: "ssh"}) {
		t.Fatal("applyTransport refused the saved host")
	}
	if host.IdleKeepAlive != 0 {
		t.Fatalf("interval = %v, want off", host.IdleKeepAlive)
	}
	if host.IdleInput != idleInputProtocolDefault {
		t.Fatalf("input mode = %v, want the protocol default", host.IdleInput)
	}
	if got, err := host.idleInputBytes(); err != nil || !bytes.Equal(got, []byte{0}) {
		t.Fatalf("default bytes = %v, %v; want NUL", got, err)
	}
}

// A hand-edited entry cannot turn on an interval this build never offered or
// send text that does not parse. The host keeps its safe default instead.
func idleKeepAliveSession(t *testing.T, host Host) (*State, *Session, *fakeShell) {
	t.Helper()
	state := newTestState(t, testOptions(t))
	shell := &fakeShell{done: make(chan struct{})}
	term := newTerminal(80, 24)
	term.shell = shell
	session := &Session{HostID: 1, host: host, term: term}
	return state, session, shell
}

// When the shell has been quiet for the whole interval, the probe is written
// into it and the next interval starts then.
func TestIdleKeepAliveWritesTheHostsInputWhenDue(t *testing.T) {
	cases := []struct {
		name string
		host Host
		want string
	}{
		{name: "ssh default nul", host: Host{Protocol: protocolSSH, IdleKeepAlive: time.Minute}, want: "\x00"},
		{name: "telnet default cr", host: Host{Protocol: protocolTelnet, IdleKeepAlive: time.Minute}, want: "\r"},
		{name: "serial default cr", host: Host{Protocol: protocolSerial, IdleKeepAlive: time.Minute}, want: "\r"},
		{name: "chosen cr", host: Host{Protocol: protocolSSH, IdleKeepAlive: time.Minute, IdleInput: idleInputCR}, want: "\r"},
		{name: "custom", host: Host{Protocol: protocolSSH, IdleKeepAlive: time.Minute, IdleInput: idleInputCustom, IdleInputCustom: `hi\n`}, want: "hi\n"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			state, session, shell := idleKeepAliveSession(t, test.host)
			if wait := state.idleKeepAliveWait(session, time.Minute); wait != time.Minute {
				t.Fatalf("wait = %v, want %v", wait, time.Minute)
			}
			if got := shell.typed(); got != test.want {
				t.Fatalf("typed %q, want %q", got, test.want)
			}
			if session.term.idleLastInput != time.Minute {
				t.Fatalf("last input = %v, want %v", session.term.idleLastInput, time.Minute)
			}
		})
	}
}

func TestIdleKeepAliveWaitsForTheWholeQuietInterval(t *testing.T) {
	state, session, shell := idleKeepAliveSession(t, Host{Protocol: protocolSSH, IdleKeepAlive: time.Minute})
	session.term.idleLastInput = 30 * time.Second
	if wait := state.idleKeepAliveWait(session, time.Minute); wait != 30*time.Second {
		t.Fatalf("wait = %v, want 30s", wait)
	}
	if got := shell.typed(); got != "" {
		t.Fatalf("typed %q before the interval passed", got)
	}
}

// Typing, whoever does it, starts the quiet interval again: a probe must never
// land in the middle of what the user is writing.
func TestIdleKeepAliveTypingPostponesTheProbe(t *testing.T) {
	state, session, shell := idleKeepAliveSession(t, Host{Protocol: protocolSSH, IdleKeepAlive: time.Minute})
	session.term.clock = func() time.Duration { return 10 * time.Second }
	if !session.term.sendInput([]byte("x"), false) {
		t.Fatal("sendInput refused the user's key")
	}
	if wait := state.idleKeepAliveWait(session, time.Minute); wait != 10*time.Second {
		t.Fatalf("wait = %v, want 10s", wait)
	}
	if got := shell.typed(); got != "x" {
		t.Fatalf("typed %q, want only the user's key", got)
	}
}

// A password prompt is a question an empty Enter would answer: the probe waits
// instead of submitting a blank password.
func TestIdleKeepAliveSkipsAPasswordPrompt(t *testing.T) {
	state, session, shell := idleKeepAliveSession(t, Host{Protocol: protocolSSH, IdleKeepAlive: time.Minute})
	session.term.passwordHint = "Password:"
	if wait := state.idleKeepAliveWait(session, time.Minute); wait != time.Minute {
		t.Fatalf("wait = %v, want %v", wait, time.Minute)
	}
	if got := shell.typed(); got != "" {
		t.Fatalf("typed %q into a password prompt", got)
	}
	if session.term.idleLastInput != time.Minute {
		t.Fatalf("last input = %v, want the check deferred", session.term.idleLastInput)
	}
}

// The probe is not typing: it must not change the line the user is editing,
// clear a password question, or count as input for a reconnect decision.
func TestIdleKeepAliveWriteDoesNotLookLikeTypedInput(t *testing.T) {
	_, session, shell := idleKeepAliveSession(t, Host{Protocol: protocolSSH, IdleKeepAlive: time.Minute})
	t0 := session.term
	t0.promptVersion = 7
	t0.inputLine = true
	t0.passwordHint = "Password:"
	if !t0.sendIdleKeepAlive([]byte{0}) {
		t.Fatal("sendIdleKeepAlive refused the write")
	}
	if got := shell.typed(); got != "\x00" {
		t.Fatalf("typed %q, want the NUL probe", got)
	}
	if t0.promptVersion != 7 || !t0.inputLine || t0.passwordHint != "Password:" {
		t.Fatalf("the probe changed typed-input state: version=%d line=%v hint=%q", t0.promptVersion, t0.inputLine, t0.passwordHint)
	}
	if t0.inputSent {
		t.Fatal("the probe counted as input that keeps the session alive")
	}
}

// A custom probe is input like any other: it follows the shell's charset, so a
// GB18030 server gets the bytes it expects.
func TestIdleKeepAliveWritesTheShellEncoding(t *testing.T) {
	_, session, shell := idleKeepAliveSession(t, Host{Protocol: protocolSSH, IdleKeepAlive: time.Minute})
	session.term.encoding = "GB18030"
	input := []byte("你好")
	if !session.term.sendIdleKeepAlive(input) {
		t.Fatal("sendIdleKeepAlive refused the write")
	}
	want := string(charset.EncodeInput(input, "GB18030"))
	if got := shell.typed(); got != want {
		t.Fatalf("typed %q, want the GB18030 bytes %q", got, want)
	}
}

func TestIdleKeepAliveIsOnlyForRemoteTerminals(t *testing.T) {
	cases := []struct {
		name string
		host Host
		id   uint32
	}{
		{name: "off", host: Host{Protocol: protocolSSH}, id: 1},
		{name: "vnc", host: Host{Protocol: protocolVNC, IdleKeepAlive: time.Minute}, id: 1},
		{name: "rdp", host: Host{Protocol: protocolRDP, IdleKeepAlive: time.Minute}, id: 1},
		{name: "local", host: Host{Protocol: protocolSSH, IdleKeepAlive: time.Minute}, id: localHostID},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			state, session, shell := idleKeepAliveSession(t, test.host)
			session.HostID = test.id
			if wait := state.idleKeepAliveWait(session, time.Minute); wait != 0 {
				t.Fatalf("wait = %v, want no timer", wait)
			}
			if got := shell.typed(); got != "" {
				t.Fatalf("typed %q", got)
			}
		})
	}
}

// A refused write is not retried in a loop: the next attempt is a whole
// interval away, like a sent probe.
func TestIdleKeepAliveRefusedWriteWaitsAnotherInterval(t *testing.T) {
	state, session, shell := idleKeepAliveSession(t, Host{Protocol: protocolSSH, IdleKeepAlive: time.Minute})
	shell.refuse = true
	if wait := state.idleKeepAliveWait(session, time.Minute); wait != time.Minute {
		t.Fatalf("wait = %v, want %v", wait, time.Minute)
	}
	if session.term.idleLastInput != time.Minute {
		t.Fatalf("last input = %v, want the check deferred", session.term.idleLastInput)
	}
}

func TestIdleKeepAliveStopsWhenTheShellEndedOrTheTextIsDamaged(t *testing.T) {
	t.Run("ended", func(t *testing.T) {
		state, session, shell := idleKeepAliveSession(t, Host{Protocol: protocolSSH, IdleKeepAlive: time.Minute})
		session.term.ended = true
		if wait := state.idleKeepAliveWait(session, time.Minute); wait != 0 {
			t.Fatalf("wait = %v, want no timer", wait)
		}
		if got := shell.typed(); got != "" {
			t.Fatalf("typed %q", got)
		}
	})
	t.Run("damaged custom text", func(t *testing.T) {
		state, session, shell := idleKeepAliveSession(t, Host{
			Protocol: protocolSSH, IdleKeepAlive: time.Minute,
			IdleInput: idleInputCustom, IdleInputCustom: `\q`,
		})
		if wait := state.idleKeepAliveWait(session, time.Minute); wait != 0 {
			t.Fatalf("wait = %v, want no timer", wait)
		}
		if got := shell.typed(); got != "" {
			t.Fatalf("typed %q", got)
		}
	})
}

func TestHostEditorSavesIdleKeepAlive(t *testing.T) {
	cases := []struct {
		name     string
		protocol hostProtocol
		preset   idleKeepAlivePreset
		unit     idleIntervalUnit
		value    string
		input    idleInputMode
		chosen   bool
		custom   string
		want     time.Duration
		wantMode idleInputMode
	}{
		{name: "five minutes", protocol: protocolSSH, preset: idleKeepAliveFiveMinutes, input: idleInputCR, chosen: true, want: 5 * time.Minute, wantMode: idleInputCR},
		{name: "protocol default left unchosen", protocol: protocolTelnet, preset: idleKeepAliveOneMinute, input: idleInputCR, want: time.Minute, wantMode: idleInputProtocolDefault},
		{name: "custom ninety seconds", protocol: protocolSSH, preset: idleKeepAliveCustom, unit: idleIntervalSeconds, value: "90", input: idleInputCustom, chosen: true, custom: `\x00hi`, want: 90 * time.Second, wantMode: idleInputCustom},
		{name: "custom two minutes", protocol: protocolSerial, preset: idleKeepAliveCustom, unit: idleIntervalMinutes, value: "2", input: idleInputNUL, chosen: true, want: 2 * time.Minute, wantMode: idleInputNUL},
		{name: "off", protocol: protocolSSH, preset: idleKeepAliveOff, input: idleInputCustom, chosen: true, custom: `\x00`, want: 0, wantMode: idleInputCustom},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			state := newTestState(t, testOptions(t))
			editor := &state.editor
			editor.reset()
			editor.protocol = test.protocol
			editor.idleKeepAlive = test.preset
			editor.idleUnit = test.unit
			editor.idleInterval = textEditorWith(test.value)
			editor.idleInput = test.input
			editor.idleInputChosen = test.chosen
			editor.idleInputCustom = textEditorWith(test.custom)
			host := editor.host(0)
			if host.IdleKeepAlive != test.want {
				t.Fatalf("interval = %v, want %v", host.IdleKeepAlive, test.want)
			}
			if host.IdleInput != test.wantMode {
				t.Fatalf("input = %v, want %v", host.IdleInput, test.wantMode)
			}
			if test.wantMode == idleInputCustom && host.IdleInputCustom != test.custom {
				t.Fatalf("custom input = %q, want %q", host.IdleInputCustom, test.custom)
			}
		})
	}
}

func TestHostEditorLoadsIdleKeepAlive(t *testing.T) {
	cases := []struct {
		name         string
		host         Host
		wantPreset   idleKeepAlivePreset
		wantUnit     idleIntervalUnit
		wantValue    string
		wantInput    idleInputMode
		wantChosen   bool
		wantCustom   string
		wantAdvanced bool
	}{
		{
			name:       "five minutes",
			host:       Host{Protocol: protocolSSH, IdleKeepAlive: 5 * time.Minute},
			wantPreset: idleKeepAliveFiveMinutes, wantInput: idleInputNUL, wantAdvanced: true,
		},
		{
			name:       "custom interval in seconds",
			host:       Host{Protocol: protocolSSH, IdleKeepAlive: 90 * time.Second, IdleInput: idleInputCustom, IdleInputCustom: `\x00`},
			wantPreset: idleKeepAliveCustom, wantUnit: idleIntervalSeconds, wantValue: "90",
			wantInput: idleInputCustom, wantChosen: true, wantCustom: `\x00`, wantAdvanced: true,
		},
		{
			name:       "custom interval in minutes",
			host:       Host{Protocol: protocolTelnet, IdleKeepAlive: 2 * time.Minute, IdleInput: idleInputCR},
			wantPreset: idleKeepAliveCustom, wantUnit: idleIntervalMinutes, wantValue: "2",
			wantInput: idleInputCR, wantChosen: true, wantAdvanced: true,
		},
		{
			name:       "off with the protocol default",
			host:       Host{Protocol: protocolSerial},
			wantPreset: idleKeepAliveOff, wantInput: idleInputCR,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			state := newTestState(t, testOptions(t))
			editor := &state.editor
			editor.load(test.host)
			if editor.idleKeepAlive != test.wantPreset {
				t.Fatalf("preset = %v, want %v", editor.idleKeepAlive, test.wantPreset)
			}
			if test.wantPreset == idleKeepAliveCustom {
				if editor.idleUnit != test.wantUnit || editor.idleInterval.String() != test.wantValue {
					t.Fatalf("custom interval = %q %v, want %q %v", editor.idleInterval.String(), editor.idleUnit, test.wantValue, test.wantUnit)
				}
			}
			if got := editor.idleInputValue(); got != test.wantInput {
				t.Fatalf("input shown = %v, want %v", got, test.wantInput)
			}
			if editor.idleInputChosen != test.wantChosen {
				t.Fatalf("input chosen = %v, want %v", editor.idleInputChosen, test.wantChosen)
			}
			if editor.idleInputCustom.String() != test.wantCustom {
				t.Fatalf("custom input = %q, want %q", editor.idleInputCustom.String(), test.wantCustom)
			}
			if editor.advancedOpen != test.wantAdvanced {
				t.Fatalf("advanced open = %v, want %v", editor.advancedOpen, test.wantAdvanced)
			}
		})
	}
}

func TestHostEditorValidatesIdleKeepAlive(t *testing.T) {
	cases := []struct {
		name   string
		preset idleKeepAlivePreset
		unit   idleIntervalUnit
		value  string
		input  idleInputMode
		chosen bool
		custom string
		want   string
	}{
		{name: "zero custom interval", preset: idleKeepAliveCustom, unit: idleIntervalSeconds, value: "0", input: idleInputNUL, want: "interval"},
		{name: "beyond a day", preset: idleKeepAliveCustom, unit: idleIntervalMinutes, value: "1441", input: idleInputNUL, want: "interval"},
		{name: "one minute", preset: idleKeepAliveCustom, unit: idleIntervalMinutes, value: "1", input: idleInputNUL},
		{name: "damaged custom input", preset: idleKeepAliveOneMinute, input: idleInputCustom, chosen: true, custom: `\q`, want: "input"},
		{name: "valid custom input", preset: idleKeepAliveOneMinute, input: idleInputCustom, chosen: true, custom: `\x00hi`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			state := newTestState(t, testOptions(t))
			editor := &state.editor
			editor.reset()
			editor.label.Insert("alpha")
			editor.address.Insert("alpha.example.com")
			editor.idleKeepAlive = test.preset
			editor.idleUnit = test.unit
			editor.idleInterval = textEditorWith(test.value)
			editor.idleInput = test.input
			editor.idleInputChosen = test.chosen
			editor.idleInputCustom = textEditorWith(test.custom)
			got := editor.validate(nil, 0, state.messages)
			switch test.want {
			case "interval":
				if got != state.messages.Tools.ErrIdleKeepAliveInterval {
					t.Fatalf("validate = %q, want the interval error", got)
				}
			case "input":
				if got != state.messages.Tools.ErrIdleKeepAliveInput {
					t.Fatalf("validate = %q, want the input error", got)
				}
			default:
				if got != "" {
					t.Fatalf("validate = %q, want no error", got)
				}
			}
		})
	}
}

func TestIdleKeepAliveIgnoresDamagedSavedValues(t *testing.T) {
	cases := []struct {
		name      string
		transport savedTransport
	}{
		{name: "negative interval", transport: savedTransport{Protocol: "ssh", IdleKeepAliveSeconds: -5}},
		{name: "too long interval", transport: savedTransport{Protocol: "ssh", IdleKeepAliveSeconds: int((25 * time.Hour) / time.Second)}},
		{name: "unknown input", transport: savedTransport{Protocol: "ssh", IdleKeepAliveSeconds: 60, IdleInput: "wat"}},
		{name: "damaged custom input", transport: savedTransport{Protocol: "ssh", IdleKeepAliveSeconds: 60, IdleInput: "custom", IdleInputCustom: `\q`}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var host Host
			if !host.applyTransport(test.transport) {
				t.Fatal("applyTransport refused the saved host")
			}
			if host.IdleKeepAlive != 0 {
				t.Fatalf("interval = %v, want off", host.IdleKeepAlive)
			}
			if host.IdleInput != idleInputProtocolDefault || host.IdleInputCustom != "" {
				t.Fatalf("input = %v %q, want the protocol default", host.IdleInput, host.IdleInputCustom)
			}
		})
	}
}
