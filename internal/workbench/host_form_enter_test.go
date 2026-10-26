package workbench

import (
	"testing"

	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

// Enter in a host dialog field submits the form, but not while an IME
// composition is open and not on a repeated key. The handler itself is
// platform-neutral, so this test is not gated to Windows.
func TestHostFormEnterValidatesAndRespectsIME(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.Click(hitAddHost)
	state.Key(ui.KeyEvent{Target: hitDialogLabel, Key: ui.KeyEnter})
	if state.modal != modalNewHost || state.dialogError == "" {
		t.Fatal("Enter skipped validation")
	}
	state.editor.label.Insert("test host")
	state.editor.address.Insert("192.0.2.42")
	state.editor.label.SetMarkedText("测试", text.Range{End: 2})
	state.Key(ui.KeyEvent{Target: hitDialogLabel, Key: ui.KeyEnter})
	if state.modal != modalNewHost {
		t.Fatal("IME Enter submitted the form")
	}
	state.editor.label.Unmark()
	state.Key(ui.KeyEvent{Target: hitDialogAddress, Key: ui.KeyEnter})
	if state.modal != modalNone || len(state.hosts) != 1 {
		t.Fatal("Enter did not save valid host")
	}
}
