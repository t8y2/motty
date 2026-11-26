//go:build darwin || windows || linux

package workbench

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/text"
	"motty/internal/serial"
)

// consoleHosts are the host types whose terminal has no handshake for the
// erase character, unlike the pty of an SSH session: a device whose line
// editor erases on BS alone never answers the DEL the Backspace key sends by
// default (#1622).
var consoleHosts = [...]struct {
	name     string
	protocol hostProtocol
}{
	{"serial", protocolSerial},
	{"Telnet", protocolTelnet},
}

// hostFormOffersBackspaceKey paints the host form tall enough for every row
// and reports whether both bytes are among its choices. The field's title is
// what tells the two apart from the flow control's segments.
func (state *State) hostFormOffersBackspaceKey() bool {
	canvas, texts := paintedTextsTall(state)
	m := state.messages.Tools
	_, del := hostChoiceIn(canvas, m.BackspaceDEL)
	_, ctrlH := hostChoiceIn(canvas, m.BackspaceCtrlH)
	for _, text := range texts {
		if del && ctrlH && text == m.FieldBackspace {
			return true
		}
	}
	return false
}

func paintedTextsTall(state *State) (*gfx.Canvas, []string) {
	var canvas gfx.Canvas
	state.Paint(&canvas, hostChoicePaint)
	var texts []string
	for _, text := range canvas.Texts() {
		texts = append(texts, text.Value)
	}
	return &canvas, texts
}

func (protocol hostProtocol) formName(state *State) string {
	if protocol == protocolSerial {
		return state.messages.Tools.ProtocolSerial
	}
	return state.messages.Tools.ProtocolTelnet
}

// Such a host can make its key send Ctrl+H. The choice is the form's, is saved
// only when it is not the default, and comes back. An SSH host has the same
// choice under Advanced; see TestSSHHostsChooseTheBackspaceKeyUnderAdvanced.
func TestConsoleHostsKeepTheByteTheirBackspaceKeysSend(t *testing.T) {
	state := newTestState(t, testOptions(t))
	m := state.messages.Tools
	for _, host := range consoleHosts {
		t.Run(host.name, func(t *testing.T) {
			state.Click(hitAddHost)
			clickHostChoice(t, state, host.protocol.formName(state))
			if !state.hostFormOffersBackspaceKey() {
				t.Fatalf("the form does not offer %q and %q under %q", m.BackspaceDEL, m.BackspaceCtrlH, m.FieldBackspace)
			}
			typeInto(state, hitDialogLabel, "Switch")
			state.editor.address = text.Editor{}
			typeInto(state, hitDialogAddress, "sw.example.com")
			clickHostChoice(t, state, m.BackspaceCtrlH)
			state.Click(hitDialogSubmit)
			if state.modal == modalNewHost {
				t.Fatalf("dialog stayed open: %q", state.dialogError)
			}
			saved := state.hosts[len(state.hosts)-1]
			if saved.Protocol != host.protocol || saved.Backspace != backspaceCtrlH {
				t.Fatalf("saved host = %+v, want the Backspace key kept with it", saved)
			}
			state.openEditHostID(saved.ID)
			if state.editor.backspace != backspaceCtrlH {
				t.Fatalf("form backspace = %v when editing a Ctrl+H host", state.editor.backspace)
			}
			clickHostChoice(t, state, m.BackspaceDEL)
			state.Click(hitDialogSubmit)
			if state.hosts[len(state.hosts)-1].Backspace != backspaceDEL {
				t.Fatalf("returning to DEL kept %v; dialog error %q", state.hosts[len(state.hosts)-1].Backspace, state.dialogError)
			}
		})
	}

	state.Click(hitAddHost)
	clickHostChoice(t, state, m.ProtocolSSH)
	if state.hostFormOffersBackspaceKey() {
		t.Fatalf("an SSH host offers %q with Advanced closed", m.FieldBackspace)
	}
	// A choice made for a console is not carried over to another protocol.
	clickHostChoice(t, state, m.ProtocolSerial)
	clickHostChoice(t, state, m.BackspaceCtrlH)
	clickHostChoice(t, state, m.ProtocolSSH)
	if built := state.editor.host(0); built.Backspace != backspaceDEL {
		t.Fatalf("an SSH host was built with backspace %v", built.Backspace)
	}
	state.Click(hitDialogCancel)

	for _, host := range consoleHosts {
		chosen, err := json.Marshal(Host{Protocol: host.protocol, Line: serial.Default, Backspace: backspaceCtrlH}.transport())
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(chosen), `"backspace":"ctrl-h"`) {
			t.Errorf("%s host saves %s without the choice", host.name, chosen)
		}
		var read savedTransport
		if err := json.Unmarshal(chosen, &read); err != nil {
			t.Fatal(err)
		}
		var restored Host
		if !restored.applyTransport(read) || restored.Backspace != backspaceCtrlH || restored.Protocol != host.protocol {
			t.Errorf("%s host round trips as %+v without the choice", host.name, restored)
		}
		plain, err := json.Marshal(Host{Protocol: host.protocol, Line: serial.Default}.transport())
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(plain), "backspace") {
			t.Errorf("a %s host on the default saves %s", host.name, plain)
		}
		// A record written before the choice existed, or with a value that
		// names nothing, opens on DEL.
		for _, record := range []string{`{}`, `{"backspace":"ctrl-h"}`, `{"backspace":"nul"}`} {
			var old savedTransport
			if err := json.Unmarshal([]byte(record), &old); err != nil {
				t.Fatal(err)
			}
			old.Protocol = map[hostProtocol]string{protocolSerial: "serial", protocolTelnet: "telnet"}[host.protocol]
			old.Line = "9600 8N1"
			var legacy Host
			want := backspaceDEL
			if record == `{"backspace":"ctrl-h"}` {
				want = backspaceCtrlH
			}
			if !legacy.applyTransport(old) || legacy.Backspace != want {
				t.Errorf("%s record %s restores as %+v, want backspace %v", host.name, record, legacy, want)
			}
		}
	}
}

// The byte reaches the device: in the session the key sends what its host
// chose, and Ctrl+H is a way to erase on either setting.
func TestConsoleSessionBackspaceSendsTheByteItsHostChose(t *testing.T) {
	for _, host := range consoleHosts {
		for _, setting := range []struct {
			name string
			key  backspaceKey
			want string
		}{
			{"default DEL", backspaceDEL, "\x7f"},
			{"Ctrl+H", backspaceCtrlH, "\b"},
		} {
			t.Run(host.name+" "+setting.name, func(t *testing.T) {
				_, options := newWorld(t)
				state := newTestState(t, options)
				streams := &streamWorld{}
				streams.install(state)
				saved := Host{Label: "Switch", Protocol: host.protocol, Backspace: setting.key, Line: serial.Settings{Baud: 9600, DataBits: 8, StopBits: 1}}
				if host.protocol == protocolSerial {
					saved.Address = "/dev/cu.usbserial-0001"
				} else {
					saved.Address = "10.0.0.2"
				}
				addStreamHost(state, saved)
				openHost(state)
				session, _ := state.sessionByIDPointer(state.activeSessionID)
				waitUntil(t, state, "the shell", func() bool { return session.term.shell != nil })
				shell := streams.shells[0]
				shell.say("<HUAWEI>")
				waitUntil(t, state, "the prompt", func() bool { return strings.Contains(screenText(state, 0), "HUAWEI") })

				window, err := headless.Open(app.Config{
					Title: "console backspace", Width: 1400, Height: 900,
					Paint: state.Paint, TextInput: state.TerminalInput,
					OnPointer: state.Pointer, OnKey: state.Key,
					OnShortcut: state.Shortcut, OnResize: state.Resize,
				}, nil, headless.Options{})
				if err != nil {
					t.Fatal(err)
				}
				defer window.Close()
				if err := window.ClickLabel(fmt.Sprintf(state.messages.LabelTerminalFmt, session.Address)); err != nil {
					t.Fatal(err)
				}
				for _, keys := range []struct{ chord, want string }{
					{"j", "j"},
					{"backspace", setting.want},
					{"ctrl+h", "\b"},
				} {
					shell.input = nil
					if err := window.Keys(keys.chord); err != nil {
						t.Fatalf("%s: %v", keys.chord, err)
					}
					if got := string(shell.input); got != keys.want {
						t.Errorf("%s sent %q, want %q", keys.chord, got, keys.want)
					}
				}
			})
		}
	}
}

// An SSH host has the same Backspace key choice, under Advanced with the other
// session options. The choice is saved only when it is Ctrl+H, comes back, and
// reaches the pty as the erase character. A mosh terminal runs on mosh-server,
// not on this pty, so it is not offered the choice.
func TestSSHHostsChooseTheBackspaceKeyUnderAdvanced(t *testing.T) {
	state := newTestState(t, testOptions(t))
	m := state.messages.Tools
	state.Click(hitAddHost)
	clickHostChoice(t, state, m.ProtocolSSH)
	if state.hostFormOffersBackspaceKey() {
		t.Fatalf("an SSH host offers %q with Advanced closed", m.FieldBackspace)
	}
	state.editor.advancedOpen = true
	if !state.hostFormOffersBackspaceKey() {
		t.Fatalf("an SSH host does not offer %q and %q under Advanced", m.BackspaceDEL, m.BackspaceCtrlH)
	}
	typeInto(state, hitDialogLabel, "Box")
	typeInto(state, hitDialogAddress, "box.example.com")
	clickHostChoice(t, state, m.BackspaceCtrlH)
	state.Click(hitDialogSubmit)
	if state.modal == modalNewHost {
		t.Fatalf("dialog stayed open: %q", state.dialogError)
	}
	saved := state.hosts[len(state.hosts)-1]
	if saved.Protocol != protocolSSH || saved.Backspace != backspaceCtrlH {
		t.Fatalf("saved host = %+v, want the Ctrl+H key kept with the SSH host", saved)
	}
	state.openEditHostID(saved.ID)
	if state.editor.backspace != backspaceCtrlH {
		t.Fatalf("form backspace = %v when editing a Ctrl+H SSH host", state.editor.backspace)
	}
	if state.hostFormOffersBackspaceKey() {
		t.Fatal("editing an SSH host opens Advanced; it starts closed")
	}
	state.editor.advancedOpen = true
	if !state.hostFormOffersBackspaceKey() {
		t.Fatal("editing an SSH host under Advanced lost the Backspace key's choice")
	}
	state.Click(hitDialogCancel)

	chosen, err := json.Marshal(saved.transport())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(chosen), `"backspace":"ctrl-h"`) {
		t.Fatalf("SSH host saves %s without the choice", chosen)
	}
	var read savedTransport
	if err := json.Unmarshal(chosen, &read); err != nil {
		t.Fatal(err)
	}
	var restored Host
	if !restored.applyTransport(read) || restored.Backspace != backspaceCtrlH || restored.Protocol != protocolSSH {
		t.Fatalf("SSH host round trips as %+v without the choice", restored)
	}
	plain, err := json.Marshal(Host{Protocol: protocolSSH, Line: serial.Default}.transport())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plain), "backspace") {
		t.Fatalf("an SSH host on the default saves %s", plain)
	}

	// A mosh terminal is not offered the choice, and never saves one.
	state.Click(hitAddHost)
	clickHostChoice(t, state, m.ProtocolSSH)
	state.editor.advancedOpen = true
	clickHostChoice(t, state, m.ProtocolMosh)
	if state.hostFormOffersBackspaceKey() {
		t.Fatalf("a mosh terminal offers %q", m.FieldBackspace)
	}
	state.Click(hitDialogCancel)
}
