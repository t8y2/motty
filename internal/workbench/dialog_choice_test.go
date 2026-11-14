package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/serial"
)

// A dialog's segmented choices are page controls now: their options carry the
// page's identities, so the tests find a segment by its label and click it in
// the event pass, where the Choice reads the press.

// hostChoicePaint is tall enough to lay out every row of the host form,
// including the Advanced section, without clipping a choice away.
var hostChoicePaint = gfx.Size{Width: 1400, Height: 1400}

// hostChoiceIn finds a dialog Choice's segment by label.
func hostChoiceIn(canvas *gfx.Canvas, label string) (gfx.Hit, bool) {
	hits := canvas.Hits()
	for i := 0; i < len(hits); i++ {
		if hit := hits[i]; hit.Label == label && hit.Role == gfx.RoleRadio {
			return hit, true
		}
	}
	return gfx.Hit{}, false
}

// hostChoiceHit paints the dialog and finds the segment with label.
func hostChoiceHit(t testing.TB, state *State, label string) gfx.Hit {
	t.Helper()
	var canvas gfx.Canvas
	state.Paint(&canvas, hostChoicePaint)
	if hit, ok := hostChoiceIn(&canvas, label); ok {
		return hit
	}
	t.Fatalf("no dialog choice labeled %q", label)
	return gfx.Hit{}
}

// clickHostChoice delivers a click on a dialog Choice segment in the event
// pass, where the choice writes its value.
func clickHostChoice(t testing.TB, state *State, label string) {
	t.Helper()
	hit := hostChoiceHit(t, state, label)
	activateHostChoice(t, state, hit.ID)
}

// activateHostChoice paints a press on one segment of a dialog Choice.
func activateHostChoice(t testing.TB, state *State, id gfx.ID) {
	t.Helper()
	var canvas gfx.Canvas
	canvas.BeginEventPass(id, gfx.ClickInfo{})
	state.Paint(&canvas, hostChoicePaint)
	canvas.EndEventPass()
}

func TestHostDialogChoosesTheFileProtocol(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitAddHost)
	for _, test := range []struct {
		label string
		want  transferMode
	}{
		{"SCP", transferSCP},
		{"SFTP", transferSFTP},
		{state.messages.Tools.TransferAuto, transferAuto},
	} {
		clickHostChoice(t, state, test.label)
		if state.editor.transfer != test.want {
			t.Fatalf("%q chose %v, want %v", test.label, state.editor.transfer, test.want)
		}
	}

	// The choice is what the form saves.
	clickHostChoice(t, state, "SCP")
	state.editor.label.Insert("Files")
	state.editor.address.Insert("files.example.com")
	state.Click(hitDialogSubmit)
	if state.modal != modalNone {
		t.Fatalf("the form stayed open: %q", state.dialogError)
	}
	added := state.hosts[len(state.hosts)-1]
	if added.Transfer != transferSCP {
		t.Fatalf("saved transfer = %v", added.Transfer)
	}
}

func TestHostDialogChoicesHaveRovingKeyboardFocus(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitAddHost)
	m := state.messages.Tools
	// The file protocol choice takes the same keys.
	auto := hostChoiceHit(t, state, m.TransferAuto)
	sftp := hostChoiceHit(t, state, "SFTP")
	result := windowKeyRoutes(state, gfx.KeyEvent{Target: auto.ID, Key: gfx.KeyRight})
	if !result.Handled || result.Focus != sftp.ID {
		t.Fatalf("transfer result = %+v", result)
	}
	activateHostChoice(t, state, result.Activate)
	if state.editor.transfer != transferSFTP {
		t.Fatalf("transfer = %v after the arrow", state.editor.transfer)
	}

	// The connection-type choice roves too. Mosh is the last entry, after the
	// existing protocols; it is the same SSH host with a different terminal
	// transport.
	ssh := hostChoiceHit(t, state, m.ProtocolSSH)
	telnet := hostChoiceHit(t, state, m.ProtocolTelnet)
	result = windowKeyRoutes(state, gfx.KeyEvent{Target: ssh.ID, Key: gfx.KeyRight})
	if !result.Handled || result.Focus != telnet.ID || result.Activate != telnet.ID {
		t.Fatalf("protocol result = %+v", result)
	}
	activateHostChoice(t, state, result.Activate)
	if state.editor.protocol != protocolTelnet {
		t.Fatalf("protocol = %v after the arrow picked the next option", state.editor.protocol)
	}

	rdp := hostChoiceHit(t, state, state.messages.Desktop.ProtocolRDP)
	mosh := hostChoiceHit(t, state, m.ProtocolMosh)
	result = windowKeyRoutes(state, gfx.KeyEvent{Target: rdp.ID, Key: gfx.KeyRight})
	if !result.Handled || result.Focus != mosh.ID || result.Activate != mosh.ID {
		t.Fatalf("connection result = %+v", result)
	}
	activateHostChoice(t, state, result.Activate)
	if state.editor.protocol != protocolSSH || state.editor.terminal != terminalMosh {
		t.Fatalf("connection = protocol %v, terminal %v after the arrow", state.editor.protocol, state.editor.terminal)
	}
}

// A protocol click writes its value after the rows were laid out, and the same
// frame paints into them: the rows a protocol has only for itself, such as
// RDP's console switch, must be painted only when this frame laid them out.
func TestTheHostDialogPaintsTheConsoleSwitchAfterASwitchFromVNCToRDP(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.openHostDialog()
	clickHostChoice(t, state, state.messages.Desktop.ProtocolVNC)
	clickHostChoice(t, state, state.messages.Desktop.ProtocolRDP)
	var canvas gfx.Canvas
	state.Paint(&canvas, hostChoicePaint)
	if _, ok := hitByID(&canvas, hitDialogConsole); !ok {
		t.Fatal("the form switched to RDP without painting its console switch")
	}
}

func TestHostDialogFlowAndAlgoChoicesChange(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitAddHost)
	m := state.messages.Tools

	clickHostChoice(t, state, m.ProtocolSerial)
	clickHostChoice(t, state, m.FlowHardware)
	if state.editor.flow != serial.FlowHardware {
		t.Fatalf("flow = %v after the click", state.editor.flow)
	}
	hardware := hostChoiceHit(t, state, m.FlowHardware)
	software := hostChoiceHit(t, state, m.FlowSoftware)
	result := windowKeyRoutes(state, gfx.KeyEvent{Target: hardware.ID, Key: gfx.KeyRight})
	if !result.Handled || result.Focus != software.ID || result.Activate != software.ID {
		t.Fatalf("flow result = %+v", result)
	}
	activateHostChoice(t, state, result.Activate)
	if state.editor.flow != serial.FlowSoftware {
		t.Fatalf("flow = %v after the arrow", state.editor.flow)
	}

	clickHostChoice(t, state, m.ProtocolSSH)
	state.editor.advancedOpen = true
	clickHostChoice(t, state, m.AlgoCompatible)
	if state.editor.algoMode != algoCompatible {
		t.Fatalf("algorithm = %v after the click", state.editor.algoMode)
	}
	compatible := hostChoiceHit(t, state, m.AlgoCompatible)
	custom := hostChoiceHit(t, state, m.AlgoCustom)
	result = windowKeyRoutes(state, gfx.KeyEvent{Target: compatible.ID, Key: gfx.KeyRight})
	if !result.Handled || result.Focus != custom.ID || result.Activate != custom.ID {
		t.Fatalf("algorithm result = %+v", result)
	}
	activateHostChoice(t, state, result.Activate)
	if state.editor.algoMode != algoCustom {
		t.Fatalf("algorithm = %v after the arrow", state.editor.algoMode)
	}
}
