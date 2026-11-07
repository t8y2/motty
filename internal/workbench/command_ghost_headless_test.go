//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
)

// Exercise OnShortcut before OnKey: on Linux and Windows the default Find
// binding otherwise takes Control+F before the terminal can accept its ghost.
func TestHeadlessControlFAcceptsTheWholeInlineSuggestion(t *testing.T) {
	h := ghostWorld(t, "git checkout main")
	h.prompt("deploy@prod:~$ ")
	h.type_("git ch")
	state := h.state
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
	if err := view.Keys("ctrl+f"); err != nil {
		t.Fatal(err)
	}
	if got := h.shell.typed()[before:]; got != "eckout main" {
		t.Fatalf("Ctrl+F sent %q, want the whole suggestion; find open=%v", got, state.sessions[0].term.find.open)
	}
	if state.sessions[0].term.find.open {
		t.Fatal("accepting the suggestion opened Find")
	}
}

func TestControlFInlineSuggestionKeepsOtherInputOwners(t *testing.T) {
	for _, test := range []struct {
		name    string
		prepare func(*historyWorld, *ui.KeyEvent)
		want    string
		find    bool
		palette bool
	}{
		{"no suggestion", func(h *historyWorld, _ *ui.KeyEvent) { h.type_("x") }, "", true, false},
		{"suggestions disabled", func(h *historyWorld, _ *ui.KeyEvent) { h.state.setCompletionMode(completionOff) }, "", true, false},
		{"unbound Find", func(h *historyWorld, _ *ui.KeyEvent) { h.state.shortcuts.unbind(shortcutFind) }, "eckout main", false, false},
		{"unbound with no suggestion", func(h *historyWorld, _ *ui.KeyEvent) {
			h.state.shortcuts.unbind(shortcutFind)
			h.type_("x")
		}, "\x06", false, false},
		{"custom action", func(h *historyWorld, _ *ui.KeyEvent) {
			h.state.shortcuts.unbind(shortcutFind)
			h.state.shortcuts.bind(shortcutCommandPalette, shortcutChord{Modifiers: ui.ModifierControl, Key: 'f'})
		}, "", false, true},
		{"Find field", func(h *historyWorld, event *ui.KeyEvent) {
			h.state.openTerminalFind(&h.state.sessions[0])
			event.Target = hitTerminalFindField
		}, "", true, false},
		{"composition", func(h *historyWorld, _ *ui.KeyEvent) { h.state.sessions[0].term.preedit.Composing = true }, "", true, false},
		{"kitty with Find", func(h *historyWorld, _ *ui.KeyEvent) {
			h.shell.say("\x1b[=1u")
			h.state.Wake()
		}, "", true, false},
		{"kitty without Find", func(h *historyWorld, _ *ui.KeyEvent) {
			h.state.shortcuts.unbind(shortcutFind)
			h.shell.say("\x1b[=1u")
			h.state.Wake()
		}, "\x1b[102;5u", false, false},
		{"win32 with Find", func(h *historyWorld, _ *ui.KeyEvent) {
			h.shell.say("\x1b[?9001h")
			h.state.Wake()
		}, "", true, false},
		{"release", func(_ *historyWorld, event *ui.KeyEvent) { event.Release = true }, "", false, false},
		{"Shift is a different chord", func(_ *historyWorld, event *ui.KeyEvent) { event.Modifiers |= ui.ModifierShift }, "\x06", false, false},
		{"Command Find stays Find", func(h *historyWorld, event *ui.KeyEvent) {
			h.state.shortcuts.bind(shortcutFind, shortcutChord{Modifiers: ui.ModifierMeta, Key: 'f'})
			event.Modifiers = ui.ModifierMeta
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
			h.state.shortcuts.bind(shortcutFind, shortcutChord{Modifiers: ui.ModifierControl, Key: 'f'})
			event := ui.KeyEvent{Target: terminalHit(h.state, 0), Text: "f", Modifiers: ui.ModifierControl}
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
