package workbench

import (
	"errors"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

func TestWindowsHostFormPasteReplacesSelectionAndUndoes(t *testing.T) {
	for _, mode := range []string{"new", "edit", "copy"} {
		t.Run(mode, func(t *testing.T) {
			state := newTestState(t, testOptions(t))
			switch mode {
			case "new":
				state.Click(hitAddHost)
			case "edit":
				state.openEditHostID(state.hosts[0].ID)
			case "copy":
				state.openCopyHostID(state.hosts[0].ID)
			}
			state.editor.algoMode = algoCustom
			reads := 0
			state.options.ClipboardText = func() (string, error) {
				reads++
				return "中文主机", nil
			}
			// Form paste keeps the standard chord even if terminal paste is rebound.
			state.shortcuts.chords[shortcutPaste] = shortcutChord{Modifiers: ui.ModifierControl | ui.ModifierShift, Key: 'v'}
			for _, id := range []ui.ID{hitDialogLabel, hitDialogAddress, hitDialogUser, hitDialogPort, hitDialogGroup, hitDialogTags, hitDialogAlgorithms, hitDialogOTP} {
				editor := state.EditorFor(id)
				if editor == nil {
					t.Fatalf("no editor for %s", tree.Text(id))
				}
				*editor = *text.NewEditor("前旧值后")
				editor.SetSelection(text.Range{Start: 1, End: 3})
				result := state.Shortcut(ui.KeyEvent{Target: id, Modifiers: ui.ModifierControl, Text: "v"})
				if !result.Handled || !result.Repaint || editor.String() != "前中文主机后" || editor.Caret() != 5 {
					t.Fatalf("%s: result=%+v value=%q caret=%d", tree.Text(id), result, editor.String(), editor.Caret())
				}
				editor.Insert("!")
				if !editor.Undo() || editor.String() != "前中文主机后" {
					t.Fatal("typing after paste was not a separate undo unit")
				}
				if !editor.Undo() || editor.String() != "前旧值后" || editor.Selection() != (text.Range{Start: 1, End: 3}) {
					t.Fatal("undo did not restore the text and selection before paste")
				}
			}
			if reads != 8 {
				t.Fatalf("clipboard reads = %d, want 8", reads)
			}
		})
	}
}

func TestWindowsHostFormPasteEmptyAndFailedClipboard(t *testing.T) {
	for _, readError := range []error{nil, errors.New("clipboard unavailable")} {
		state := newTestState(t, testOptions(t))
		state.Click(hitAddHost)
		editor := state.EditorFor(hitDialogLabel)
		editor.Insert("keep selected text")
		editor.SetSelection(text.Range{End: editor.RuneCount()})
		before := editor.Selection()
		state.options.ClipboardText = func() (string, error) { return "", readError }
		result := state.Shortcut(ui.KeyEvent{Target: hitDialogLabel, Modifiers: ui.ModifierControl, Text: "v"})
		if !result.Handled || editor.String() != "keep selected text" || editor.Selection() != before {
			t.Fatalf("result=%+v text=%q selection=%+v", result, editor.String(), editor.Selection())
		}
		if readError != nil && !strings.Contains(state.dialogError, readError.Error()) {
			t.Fatalf("clipboard error was not shown in the dialog: %q", state.dialogError)
		}
	}
}

func TestWindowsHostFormPasteLeavesOtherChordsAndTargetsAlone(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitAddHost)
	state.options.ClipboardText = func() (string, error) {
		t.Fatal("read clipboard without a paste in a focused host field")
		return "", nil
	}
	for _, event := range []ui.KeyEvent{
		{Target: hitDialogLabel, Text: "v"},
		{Target: hitDialogLabel, Modifiers: ui.ModifierControl | ui.ModifierAlt, Text: "v"},
		{Target: hitDialogLabel, Modifiers: ui.ModifierControl | ui.ModifierShift, Text: "v"},
		{Target: hitDialogLabel, Modifiers: ui.ModifierMeta, Text: "v"},
		{Target: hitDialogLabel, Modifiers: ui.ModifierControl, Text: "c"},
		{Target: hitDialogSubmit, Modifiers: ui.ModifierControl, Text: "v"},
	} {
		if result := state.Shortcut(event); result.Handled {
			t.Fatalf("unrelated chord or target was consumed: %+v", event)
		}
	}
	state.openShortcutCapture(shortcutPaste)
	result := state.Shortcut(ui.KeyEvent{Modifiers: ui.ModifierControl, Text: "v"})
	if !result.Handled || state.modal != modalNone {
		t.Fatalf("shortcut capture was interrupted: result=%+v modal=%v", result, state.modal)
	}
}

func TestWindowsHostFormPasteUsesFocusedFieldThroughDriver(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitAddHost)
	reads := 0
	state.options.ClipboardText = func() (string, error) {
		reads++
		return "192.0.2.42", nil
	}
	var input app.TextInput
	driver, err := app.NewDriver(app.Config{
		Title: "host paste", Width: 1100, Height: 900, Tree: Tree(),
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key,
		OnShortcut: state.Shortcut, OnResize: state.Resize,
		TextInput: func(context app.TextInputContext) *app.TextInput {
			editor := state.EditorFor(context.ID)
			if editor == nil {
				return nil
			}
			input = app.TextInput{Editor: editor}
			return &input
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(driver.Close)
	x, y := labelPoint(t, driver, hitDialogAddress)
	if err := driver.Click(x, y); err != nil {
		t.Fatal(err)
	}
	if driver.Snapshot().Focused != hitDialogAddress {
		t.Fatal("click did not focus the address field")
	}
	// The headless driver's built-in paste can hide this Windows bug. Require
	// the application to read the injected clipboard, exactly once.
	if handled, err := driver.ShortcutChord(ui.ModifierControl, 'v'); err != nil || !handled {
		t.Fatalf("paste: handled=%v err=%v", handled, err)
	}
	if reads != 1 || state.editor.address.String() != "192.0.2.42" || state.editor.label.String() != "" {
		t.Fatalf("reads=%d address=%q label=%q", reads, state.editor.address.String(), state.editor.label.String())
	}
	if got := driver.Snapshot().Editor.Value; got != "192.0.2.42" {
		t.Fatalf("native editor was not refreshed after paste: %q", got)
	}
}

func TestWindowsHostFormPasteDoesNotReachActiveTerminal(t *testing.T) {
	state, _, _, shell := liveSession(t)
	state.options.ClipboardText = func() (string, error) { return "host.example.com", nil }
	state.Click(hitAddHost)
	state.Shortcut(ui.KeyEvent{Target: hitDialogAddress, Modifiers: ui.ModifierControl, Text: "v"})
	if shell.typed() != "" || state.editor.address.String() != "host.example.com" {
		t.Fatalf("terminal=%q field=%q", shell.typed(), state.editor.address.String())
	}
	state.Click(hitDialogCancel)
	state.Shortcut(ui.KeyEvent{Target: state.activeTerminalFocusHit(), Modifiers: ui.ModifierControl, Text: "v"})
	if got := shell.typed(); got != "host.example.com" {
		t.Fatalf("terminal paste after closing the dialog = %q", got)
	}
}
