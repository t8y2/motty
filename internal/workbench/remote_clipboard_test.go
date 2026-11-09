package workbench

import (
	"errors"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

func newClipboardState(t testing.TB, options Options) (*State, *Session) {
	t.Helper()
	state := newTestState(t, options)
	state.sessions = append(state.sessions, Session{ID: 77, Address: "user@remote.test", term: newTerminal(40, 4)})
	session := &state.sessions[len(state.sessions)-1]
	state.configureTerminal(session)
	return state, session
}

func TestOSC52DefaultWritesWithoutAsking(t *testing.T) {
	driver, err := app.NewDriver(app.Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()

	state, session := newClipboardState(t, testOptions(t))
	session.term.screen.Write([]byte("\x1b]52;c;cmVtb3RlIHRleHQ=\x07"))
	if state.modal != modalNone || state.clipboardRequest.pending || driver.Clipboard() != "remote text" || state.status != state.messages.StatusClipboardCopied {
		t.Fatalf("modal=%v request=%+v clipboard=%q status=%q", state.modal, state.clipboardRequest, driver.Clipboard(), state.status)
	}
}

func TestOSC52AskDescribesTheRequestAndAlwaysAllowStopsIt(t *testing.T) {
	writes := []string{}
	options := testOptions(t)
	options.SetClipboardText = func(value string) error {
		writes = append(writes, value)
		return nil
	}
	state, session := newClipboardState(t, options)
	state.terminalClipboard = terminalClipboardAsk

	session.term.screen.Write([]byte("\x1b]52;c;cmVtb3RlIHRleHQ=\x07"))
	if state.modal != modalRemoteClipboard || !state.clipboardRequest.pending || len(writes) != 0 {
		t.Fatalf("modal=%v request=%+v writes=%q", state.modal, state.clipboardRequest, writes)
	}
	if !strings.Contains(state.clipboardRequest.description, session.Address) || state.clipboardRequest.preview != `"remote text"` {
		t.Fatalf("request=%+v", state.clipboardRequest)
	}

	state.Click(hitClipboardAlways)
	if state.modal != modalNone || state.clipboardRequest.pending || len(writes) != 1 || writes[0] != "remote text" {
		t.Fatalf("writes=%q modal=%v request=%+v", writes, state.modal, state.clipboardRequest)
	}
	if state.terminalClipboard != terminalClipboardAllow {
		t.Fatalf("policy = %v after Always allow", state.terminalClipboard)
	}
	// The choice is saved, so the next launch does not ask again.
	reopened := newTestState(t, options)
	if reopened.terminalClipboard != terminalClipboardAllow {
		t.Fatalf("reloaded policy = %v after Always allow", reopened.terminalClipboard)
	}
}

func TestOSC52BlockAllowAndReadSecurityBoundaries(t *testing.T) {
	writes := []string{}
	options := testOptions(t)
	options.SetClipboardText = func(value string) error {
		writes = append(writes, value)
		return nil
	}
	state, session := newClipboardState(t, options)

	state.terminalClipboard = terminalClipboardBlock
	session.term.screen.Write([]byte("\x1b]52;c;YmxvY2tlZA==\x07"))
	if len(writes) != 0 || state.modal != modalNone || state.status != state.messages.StatusClipboardBlocked {
		t.Fatalf("blocked writes=%q modal=%v status=%q", writes, state.modal, state.status)
	}

	state.terminalClipboard = terminalClipboardAllow
	session.term.screen.Write([]byte("\x1b]52;c;YWxsb3dlZA==\x07"))
	if len(writes) != 1 || writes[0] != "allowed" || state.modal != modalNone {
		t.Fatalf("allowed writes=%q modal=%v", writes, state.modal)
	}

	session.term.screen.Write([]byte("\x1b]52;c;?\x07"))
	if len(writes) != 1 {
		t.Fatalf("clipboard read query changed local clipboard: %q", writes)
	}
}

func TestOSC52AskKeepsFirstBoundedRequestAndCancelDoesNotWrite(t *testing.T) {
	writes := 0
	options := testOptions(t)
	options.SetClipboardText = func(string) error { writes++; return nil }
	state, session := newClipboardState(t, options)
	state.terminalClipboard = terminalClipboardAsk
	session.term.screen.Write([]byte("\x1b]52;c;Zmlyc3Q=\x07\x1b]52;c;c2Vjb25k\x07"))
	if state.clipboardRequest.text != "first" {
		t.Fatalf("pending text = %q", state.clipboardRequest.text)
	}
	state.Click(hitClipboardCancel)
	if writes != 0 || state.modal != modalNone || state.clipboardRequest.pending {
		t.Fatalf("writes=%d modal=%v request=%+v", writes, state.modal, state.clipboardRequest)
	}
}

func TestOSC52ClipboardFailureIsVisible(t *testing.T) {
	options := testOptions(t)
	options.SetClipboardText = func(string) error { return errors.New("clipboard unavailable") }
	state, session := newClipboardState(t, options)
	state.terminalClipboard = terminalClipboardAllow
	session.term.screen.Write([]byte("\x1b]52;c;eA==\x07"))
	if !strings.Contains(state.status, "clipboard unavailable") {
		t.Fatalf("status = %q", state.status)
	}
}

func TestOSC52ConfirmationDialogPaintsWithoutSchedulingWork(t *testing.T) {
	state, session := newClipboardState(t, testOptions(t))
	state.terminalClipboard = terminalClipboardAsk
	session.term.screen.Write([]byte("\x1b]52;c;cHJldmlldw==\x07"))
	var canvas gfx.Canvas
	state.paintRemoteClipboardDialog(&canvas, gfx.Rect{Width: 900, Height: 600})
	for _, id := range []gfx.ID{hitClipboardAllow, hitClipboardAlways} {
		if _, ok := hitByID(&canvas, id); !ok {
			t.Fatalf("confirmation dialog did not paint %v", id)
		}
	}
	if _, scheduled := canvas.NextFrame(); scheduled {
		t.Fatal("clipboard confirmation scheduled background work")
	}
}
