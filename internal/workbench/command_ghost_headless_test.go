//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/keymap"
)

// Exercise OnShortcut before OnKey: with the default bindings, Right accepts
// the suggestion while Control+F opens Find on Linux and Windows. When the user
// configures suggestion accept key through shortcut settings (unbinding Find
// and binding Control+F), Control+F accepts the inline suggestion.
func TestHeadlessAcceptSuggestionKeyRouting(t *testing.T) {
	// 1. Default accept key (Right arrow) accepts the inline suggestion;
	// default Find shortcut (Control+F) opens Find and does not accept.
	h := ghostWorld(t, "git checkout main")
	h.prompt("deploy@prod:~$ ")
	h.type_("git ch")
	state := h.state
	state.shortcuts.bind(shortcutFind, keymap.Chord{Modifiers: gfx.ModifierControl, Char: 'f'})
	view, err := headless.Open(app.Config{
		Title: "inline suggestion", Width: 1400, Height: 900,
		Paint: state.Paint, TextInput: state.TerminalInput,
		OnPointer: state.Pointer, OnKey: state.Key,
		OnShortcut: state.Shortcut, OnResize: state.Resize,
	}, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	if err := view.ClickLabel(fmt.Sprintf(state.messages.LabelTerminalFmt, state.sessions[0].Address)); err != nil {
		t.Fatal(err)
	}
	if rest, ok := h.ghost(); !ok || rest != "eckout main" {
		t.Fatalf("suggestion = %q, %v", rest, ok)
	}

	before := len(h.shell.typed())
	if err := view.Keys("Right"); err != nil {
		t.Fatal(err)
	}
	if got := h.shell.typed()[before:]; got != "eckout main" {
		t.Fatalf("Right sent %q, want the whole suggestion", got)
	}
	if state.sessions[0].term.find.open {
		t.Fatal("accepting with Right arrow opened Find")
	}

	// With default bindings, Control+F opens Find and does not accept the suggestion.
	hDefault := ghostWorld(t, "git checkout main")
	hDefault.prompt("deploy@prod:~$ ")
	hDefault.type_("git ch")
	stateDefault := hDefault.state
	stateDefault.shortcuts.bind(shortcutFind, keymap.Chord{Modifiers: gfx.ModifierControl, Char: 'f'})
	viewDefault, err := headless.Open(app.Config{
		Title: "inline suggestion default find", Width: 1400, Height: 900,
		Paint: stateDefault.Paint, TextInput: stateDefault.TerminalInput,
		OnPointer: stateDefault.Pointer, OnKey: stateDefault.Key,
		OnShortcut: stateDefault.Shortcut, OnResize: stateDefault.Resize,
	}, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer viewDefault.Close()
	if err := viewDefault.ClickLabel(fmt.Sprintf(stateDefault.messages.LabelTerminalFmt, stateDefault.sessions[0].Address)); err != nil {
		t.Fatal(err)
	}
	beforeDefault := len(hDefault.shell.typed())
	if err := viewDefault.Keys("ctrl+f"); err != nil {
		t.Fatal(err)
	}
	if got := hDefault.shell.typed()[beforeDefault:]; got != "" {
		t.Fatalf("Control+F sent %q, want empty because Find opened", got)
	}
	if !stateDefault.sessions[0].term.find.open {
		t.Fatal("Control+F did not open Find with default shortcut")
	}

	// 2. User configures suggestion accept key through shortcut settings (state.shortcuts.acceptKeys).
	h2 := ghostWorld(t, "git checkout main")
	h2.prompt("deploy@prod:~$ ")
	h2.type_("git ch")
	state2 := h2.state
	state2.shortcuts.unbind(shortcutFind)
	if _, bound := state2.shortcuts.bind(shortcutAcceptSuggestion, keymap.Chord{Modifiers: gfx.ModifierControl, Char: 'f'}); !bound {
		t.Fatal("failed to bind Control+F to accept suggestion")
	}
	view2, err := headless.Open(app.Config{
		Title: "inline suggestion configured", Width: 1400, Height: 900,
		Paint: state2.Paint, TextInput: state2.TerminalInput,
		OnPointer: state2.Pointer, OnKey: state2.Key,
		OnShortcut: state2.Shortcut, OnResize: state2.Resize,
	}, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer view2.Close()
	if err := view2.ClickLabel(fmt.Sprintf(state2.messages.LabelTerminalFmt, state2.sessions[0].Address)); err != nil {
		t.Fatal(err)
	}
	before2 := len(h2.shell.typed())
	if err := view2.Keys("ctrl+f"); err != nil {
		t.Fatal(err)
	}
	if got := h2.shell.typed()[before2:]; got != "eckout main" {
		t.Fatalf("configured Control+F sent %q, want the whole suggestion; find open=%v", got, state2.sessions[0].term.find.open)
	}
	if state2.sessions[0].term.find.open {
		t.Fatal("accepting with configured Control+F opened Find")
	}
}

func TestControlFInlineSuggestionKeepsOtherInputOwners(t *testing.T) {
	for _, test := range []struct {
		name    string
		prepare func(*historyWorld, *gfx.KeyEvent)
		want    string
		find    bool
		palette bool
	}{
		{"default Find with suggestion", func(_ *historyWorld, _ *gfx.KeyEvent) {}, "", true, false},
		{"default Find with no suggestion", func(h *historyWorld, _ *gfx.KeyEvent) { h.type_("x") }, "", true, false},
		{"default Find with suggestions disabled", func(h *historyWorld, _ *gfx.KeyEvent) { h.state.setCompletionMode(completionOff) }, "", true, false},
		{"custom action", func(h *historyWorld, _ *gfx.KeyEvent) {
			h.state.shortcuts.unbind(shortcutFind)
			h.state.shortcuts.bind(shortcutCommandPalette, keymap.Chord{Modifiers: gfx.ModifierControl, Char: 'f'})
		}, "", false, true},
		{"accept suggestion bound to Control+F", func(h *historyWorld, _ *gfx.KeyEvent) {
			h.state.shortcuts.unbind(shortcutFind)
			h.state.shortcuts.bind(shortcutAcceptSuggestion, keymap.Chord{Modifiers: gfx.ModifierControl, Char: 'f'})
		}, "eckout main", false, false},
		{"accept suggestion with no suggestion", func(h *historyWorld, _ *gfx.KeyEvent) {
			h.state.shortcuts.unbind(shortcutFind)
			h.state.shortcuts.bind(shortcutAcceptSuggestion, keymap.Chord{Modifiers: gfx.ModifierControl, Char: 'f'})
			h.type_("x")
		}, "\x06", false, false},
		{"accept suggestion with suggestions disabled", func(h *historyWorld, _ *gfx.KeyEvent) {
			h.state.shortcuts.unbind(shortcutFind)
			h.state.shortcuts.bind(shortcutAcceptSuggestion, keymap.Chord{Modifiers: gfx.ModifierControl, Char: 'f'})
			h.state.setCompletionMode(completionOff)
		}, "\x06", false, false},
		{"Find field", func(h *historyWorld, event *gfx.KeyEvent) {
			h.state.openTerminalFind(&h.state.sessions[0])
			event.Target = hitTerminalFindField
		}, "", true, false},
		{"composition", func(h *historyWorld, _ *gfx.KeyEvent) { h.state.sessions[0].term.preedit.Composing = true }, "", true, false},
		{"kitty with Find", func(h *historyWorld, _ *gfx.KeyEvent) {
			h.shell.say("\x1b[=1u")
			h.state.Wake()
		}, "", true, false},
		{"kitty with accept suggestion", func(h *historyWorld, _ *gfx.KeyEvent) {
			h.state.shortcuts.unbind(shortcutFind)
			h.state.shortcuts.bind(shortcutAcceptSuggestion, keymap.Chord{Modifiers: gfx.ModifierControl, Char: 'f'})
			h.shell.say("\x1b[=1u")
			h.state.Wake()
		}, "\x1b[102;5u", false, false},
		{"win32 with Find", func(h *historyWorld, _ *gfx.KeyEvent) {
			h.shell.say("\x1b[?9001h")
			h.state.Wake()
		}, "", true, false},
		{"release", func(_ *historyWorld, event *gfx.KeyEvent) { event.Release = true }, "", false, false},
		{"Shift is a different chord", func(_ *historyWorld, event *gfx.KeyEvent) { event.Modifiers |= gfx.ModifierShift }, "\x06", false, false},
		{"Command Find stays Find", func(h *historyWorld, event *gfx.KeyEvent) {
			h.state.shortcuts.bind(shortcutFind, keymap.Chord{Modifiers: gfx.ModifierMeta, Char: 'f'})
			event.Modifiers = gfx.ModifierMeta
		}, "", true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.name == "win32 with Find" && runtime.GOOS != "windows" {
				t.Skip("win32-input-mode is only available on Windows")
			}
			h := ghostWorld(t, "git checkout main")
			h.prompt("$ ")
			h.type_("git ch")
			// Exercise the Linux/Windows Find default on every test platform.
			h.state.shortcuts.bind(shortcutFind, keymap.Chord{Modifiers: gfx.ModifierControl, Char: 'f'})
			event := gfx.KeyEvent{Target: terminalHit(h.state, 0), Text: "f", Modifiers: gfx.ModifierControl}
			test.prepare(h, &event)
			before := len(h.shell.typed())
			if result := h.state.Shortcut(event); !result.Handled {
				h.state.Key(event)
			}
			if got := h.shell.typed()[before:]; got != test.want {
				t.Fatalf("key sent %q, want %q", got, test.want)
			}
			if h.state.sessions[0].term.find.open != test.find || (h.state.modal == modalCommandPalette) != test.palette {
				t.Fatalf("Find=%v palette=%v; want %v, %v", h.state.sessions[0].term.find.open, h.state.modal == modalCommandPalette, test.find, test.palette)
			}
		})
	}
}
