package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

// A dialog's segmented choices are pages now: their options carry the page's
// identities, so the tests find a segment by its label and click it in the event
// pass, where the Choice reads the press.

// dialogChoicePaint is tall enough to lay out every row of the host form,
// including the Advanced section, without clipping a choice away.
var dialogChoicePaint = gfx.Size{Width: 1400, Height: 1400}

// dialogChoiceIn finds a dialog Choice's segment by label.
func dialogChoiceIn(canvas *gfx.Canvas, label string) (gfx.Hit, bool) {
	hits := canvas.Hits()
	for i := len(hits) - 1; i >= 0; i-- {
		if hit := hits[i]; hit.Label == label && hit.Role == gfx.RoleRadio {
			return hit, true
		}
	}
	return gfx.Hit{}, false
}

// dialogChoiceHit paints the dialog and finds the segment with label.
func dialogChoiceHit(t testing.TB, state *State, label string) gfx.Hit {
	t.Helper()
	var canvas gfx.Canvas
	state.Paint(&canvas, dialogChoicePaint)
	if hit, ok := dialogChoiceIn(&canvas, label); ok {
		return hit
	}
	t.Fatalf("no dialog choice labeled %q", label)
	return gfx.Hit{}
}

// clickDialogChoice delivers a click on a dialog Choice segment in the event
// pass, where the choice writes its value.
func clickDialogChoice(t testing.TB, state *State, label string) {
	t.Helper()
	hit := dialogChoiceHit(t, state, label)
	var canvas gfx.Canvas
	canvas.BeginEventPass(hit.ID, gfx.ClickInfo{})
	state.Paint(&canvas, dialogChoicePaint)
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
		clickDialogChoice(t, state, test.label)
		if state.editor.transfer != test.want {
			t.Fatalf("%q chose %v, want %v", test.label, state.editor.transfer, test.want)
		}
	}

	// The choice is what the form saves.
	clickDialogChoice(t, state, "SCP")
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
	ssh := dialogChoiceHit(t, state, m.ProtocolSSH)
	telnet := dialogChoiceHit(t, state, m.ProtocolTelnet)
	result := state.Key(gfx.KeyEvent{Target: ssh.ID, Key: gfx.KeyRight})
	if !result.Handled || result.Focus != telnet.ID || result.Activate != telnet.ID {
		t.Fatalf("protocol result = %+v", result)
	}
	var canvas gfx.Canvas
	canvas.BeginEventPass(result.Activate, gfx.ClickInfo{})
	state.Paint(&canvas, dialogChoicePaint)
	canvas.EndEventPass()
	if state.editor.protocol != protocolTelnet {
		t.Fatalf("protocol = %v after the arrow picked the next option", state.editor.protocol)
	}
}
