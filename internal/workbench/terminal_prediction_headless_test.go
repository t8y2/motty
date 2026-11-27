//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
)

func TestHeadlessTerminalPredictionAppearsAfterVerifiedRemoteEcho(t *testing.T) {
	state, _, _, shell := liveSession(t)
	advance := slowLink(state)
	var input app.TextInput
	resolveInput := func(context app.TextInputContext) *app.TextInput {
		if context.Terminal {
			return state.TerminalInput(context)
		}
		input.Editor = state.EditorFor(context.ID)
		if input.Editor == nil {
			return nil
		}
		return &input
	}
	var invalidator app.Invalidator
	session, err := headless.Open(app.Config{
		Title: "predictive echo", Width: 1400, Height: 900,
		Paint: state.Paint, TextInput: resolveInput,
		OnPointer: state.Pointer, OnKey: state.Key,
		OnShortcut: state.Shortcut, OnResize: state.Resize,
		Invalidator: &invalidator, OnWake: state.Wake,
	}, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	terminalLabel := fmt.Sprintf(state.messages.LabelTerminalFmt, state.sessions[0].Address)
	if err := session.ClickLabel(terminalLabel); err != nil {
		t.Fatal(err)
	}
	if err := session.Type("abc"); err != nil {
		t.Fatal(err)
	}
	if containsExact(session.Texts(), "abc") {
		t.Fatal("headless surface displayed input before the remote verified echo")
	}

	advance(80 * time.Millisecond)
	shell.say("abc")
	invalidator.Invalidate()
	if err := session.Driver().Pump(); err != nil {
		t.Fatal(err)
	}
	if err := session.Type("deploy"); err != nil {
		t.Fatal(err)
	}
	if !containsExact(session.Texts(), "deploy") {
		t.Fatalf("headless texts do not contain the prediction: %q", session.Texts())
	}

	advance(80 * time.Millisecond)
	shell.say("de")
	invalidator.Invalidate()
	if err := session.Driver().Pump(); err != nil {
		t.Fatal(err)
	}
	if !containsExact(session.Texts(), "ploy") {
		t.Fatalf("headless surface did not reconcile partial echo: %q", session.Texts())
	}

	shell.say("\r\nPassword: ")
	invalidator.Invalidate()
	if err := session.Driver().Pump(); err != nil {
		t.Fatal(err)
	}
	if err := session.Type("secret"); err != nil {
		t.Fatal(err)
	}
	if containsExact(session.Texts(), "secret") {
		t.Fatal("headless surface exposed unverified password input")
	}
}

func TestHeadlessTerminalPredictionKeepsCommandAcrossEnter(t *testing.T) {
	state, _, _, shell := liveSession(t)
	advance := slowLink(state)
	var input app.TextInput
	resolveInput := func(context app.TextInputContext) *app.TextInput {
		if context.Terminal {
			return state.TerminalInput(context)
		}
		input.Editor = state.EditorFor(context.ID)
		if input.Editor == nil {
			return nil
		}
		return &input
	}
	var invalidator app.Invalidator
	view, err := headless.Open(app.Config{
		Title: "predictive echo enter", Width: 1400, Height: 900,
		Paint: state.Paint, TextInput: resolveInput,
		OnPointer: state.Pointer, OnKey: state.Key,
		OnShortcut: state.Shortcut, OnResize: state.Resize,
		Invalidator: &invalidator, OnWake: state.Wake,
	}, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()

	terminalLabel := fmt.Sprintf(state.messages.LabelTerminalFmt, state.sessions[0].Address)
	if err := view.ClickLabel(terminalLabel); err != nil {
		t.Fatal(err)
	}
	shell.say(folderSyncMarker(&state.sessions[0], "A") + "root@vultr:~# " + folderSyncMarker(&state.sessions[0], "B"))
	invalidator.Invalidate()
	if err := view.Driver().Pump(); err != nil {
		t.Fatal(err)
	}

	if err := view.Type("docker "); err != nil {
		t.Fatal(err)
	}
	advance(150 * time.Millisecond)
	shell.say("docker ")
	invalidator.Invalidate()
	if err := view.Driver().Pump(); err != nil {
		t.Fatal(err)
	}

	if err := view.Type("ps"); err != nil {
		t.Fatal(err)
	}
	if !containsExact(view.Texts(), "ps") {
		t.Fatalf("headless surface did not show predicted suffix before Enter: %q", view.Texts())
	}

	if err := view.Driver().Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if !containsExact(view.Texts(), "ps") {
		t.Fatalf("headless surface withdrew predicted text on Enter: %q", view.Texts())
	}

	advance(150 * time.Millisecond)
	shell.say("p")
	invalidator.Invalidate()
	if err := view.Driver().Pump(); err != nil {
		t.Fatal(err)
	}
	if !containsExact(view.Texts(), "s") {
		t.Fatalf("headless surface did not keep remaining sealed prediction: %q", view.Texts())
	}

	advance(50 * time.Millisecond)
	shell.say("s\r\n")
	invalidator.Invalidate()
	if err := view.Driver().Pump(); err != nil {
		t.Fatal(err)
	}
	if containsExact(view.Texts(), "s") {
		t.Fatalf("headless surface left sealed prediction after newline: %q", view.Texts())
	}
}
