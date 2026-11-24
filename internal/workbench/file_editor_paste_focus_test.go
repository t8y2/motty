package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
	"github.com/vivaui/viva/text"
	"motty/internal/filecontent"
)

func remoteDocumentInWindow(t *testing.T, state *State, body string) (*fileWindowManager, *fileDocument) {
	t.Helper()
	decoded, err := filecontent.Decode([]byte(body), "", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	d := &fileDocument{
		owner:    state,
		cacheKey: "1:false:/etc/app.conf",
		location: "/etc/app.conf",
		name:     "app.conf",
		source:   "host · 127.0.0.1",
		local:    false,
	}
	d.setText(decoded, nil)
	m := &state.fileWindows
	m.documents = map[string]*fileDocument{d.cacheKey: d}
	m.order = []string{d.cacheKey}
	m.active = d.cacheKey
	return m, d
}

func TestFileEditorPasteFocusWindowDoesNotLeakToTerminal(t *testing.T) {
	for _, tc := range []struct {
		name      string
		clipboard string
	}{
		{name: "single-line", clipboard: "echo leaked"},
		{name: "multi-line", clipboard: "echo one\necho two"},
	} {
		for _, targetCase := range []struct {
			name   string
			target gfx.ID
		}{
			{name: "zero-focus", target: gfx.ID{}},
			{name: "tab-focus", target: hitDocumentTabList.At(0)},
		} {
			t.Run(tc.name+"/"+targetCase.name, func(t *testing.T) {
				state, _, _, shell := liveSession(t)
				state.options.ClipboardText = func() (string, error) { return tc.clipboard, nil }
				m, d := remoteDocumentInWindow(t, state, "initial\n")

				res := m.shortcut(gfx.KeyEvent{Modifiers: keymap.Primary, Text: "v", Target: targetCase.target})
				if !res.Handled {
					// Simulate viva's macOS menu bar fallback when the active window leaves the shortcut unhandled.
					state.MenuBar()[1].Items[1].OnSelect()
				}

				if !res.Handled {
					t.Fatalf("file window shortcut was not handled for %s (%s): %+v", tc.name, targetCase.name, res)
				}
				if got := shell.typed(); got != "" {
					t.Fatalf("paste leaked to terminal shell: %q", got)
				}
				if state.modal != modalNone {
					t.Fatalf("paste opened unexpected modal %v", state.modal)
				}
				if got := d.editor.String(); got != "initial\n" {
					t.Fatalf("editor changed unexpectedly without input focus: %q", got)
				}
			})
		}
	}
}

func TestFileEditorPasteFocusComparisonWindowDoesNotLeakToTerminal(t *testing.T) {
	for _, tc := range []struct {
		name      string
		clipboard string
	}{
		{name: "single-line", clipboard: "echo leaked"},
		{name: "multi-line", clipboard: "echo one\necho two"},
	} {
		for _, targetCase := range []struct {
			name   string
			target gfx.ID
		}{
			{name: "zero-focus", target: gfx.ID{}},
			{name: "diff-viewport", target: hitDocumentDiffViewport},
		} {
			t.Run(tc.name+"/"+targetCase.name, func(t *testing.T) {
				state, _, _, shell := liveSession(t)
				state.options.ClipboardText = func() (string, error) { return tc.clipboard, nil }
				_, d := remoteDocumentInWindow(t, state, "initial\n")
				d.comparison.active = true
				d.comparison.baseline = "initial\n"
				d.comparison.result = components.DiffCodeLines(d.comparison.baseline, d.editor.String())

				res := d.comparisonShortcut(gfx.KeyEvent{Modifiers: keymap.Primary, Text: "v", Target: targetCase.target})
				if !res.Handled {
					state.MenuBar()[1].Items[1].OnSelect()
				}

				if !res.Handled {
					t.Fatalf("comparison window shortcut was not handled for %s (%s): %+v", tc.name, targetCase.name, res)
				}
				if got := shell.typed(); got != "" {
					t.Fatalf("paste leaked to terminal shell: %q", got)
				}
				if state.modal != modalNone {
					t.Fatalf("paste opened unexpected modal %v", state.modal)
				}
				if got := d.editor.String(); got != "initial\n" {
					t.Fatalf("editor changed unexpectedly without input focus: %q", got)
				}
			})
		}
	}
}

func TestFileEditorPasteFocusWindowPastesIntoFocusedInput(t *testing.T) {
	state, _, _, shell := liveSession(t)
	state.options.ClipboardText = func() (string, error) { return "line1\r\nline2", nil }
	m, d := remoteDocumentInWindow(t, state, "initial\n")
	if !d.canEditCode() {
		t.Fatal("expected document to be editable")
	}
	d.editor.SetSelection(text.Range{Start: d.editor.RuneCount(), End: d.editor.RuneCount()})

	res := m.shortcut(gfx.KeyEvent{Modifiers: keymap.Primary, Text: "v", Target: hitDocumentInput})
	if !res.Handled {
		t.Fatalf("expected focused editor paste to be handled: %+v", res)
	}
	if got := d.editor.String(); got != "initial\nline1\nline2" {
		t.Fatalf("editor content = %q, want %q", got, "initial\nline1\nline2")
	}
	if got := shell.typed(); got != "" {
		t.Fatalf("paste leaked to terminal shell: %q", got)
	}
	if state.modal != modalNone {
		t.Fatalf("paste opened unexpected modal %v", state.modal)
	}
}

func TestFileEditorPasteFocusDockedSFTPEditorDoesNotLeakToTerminal(t *testing.T) {
	for _, tc := range []struct {
		name      string
		clipboard string
	}{
		{name: "single-line", clipboard: "echo leaked"},
		{name: "multi-line", clipboard: "echo one\necho two"},
	} {
		for _, targetCase := range []struct {
			name   string
			target gfx.ID
		}{
			{name: "zero-focus", target: gfx.ID{}},
			{name: "sftp-non-editor", target: contextID(hitSFTPListBackground)},
		} {
			t.Run(tc.name+"/"+targetCase.name, func(t *testing.T) {
				state, session, _, _ := openTestSFTP(t)
				view := openSFTPTextEditor(t, state, session, "notes.txt")
				if !state.filesPanelShown() || view.editPath == "" {
					t.Fatalf("expected docked SFTP editor open, shown=%v editPath=%q", state.filesPanelShown(), view.editPath)
				}
				initial := view.editEditor.String()
				state.options.ClipboardText = func() (string, error) { return tc.clipboard, nil }

				// Ensure we can inspect what the terminal shell receives during this test.
				shell := session.term.shell.(*fakeShell)
				res := state.Shortcut(gfx.KeyEvent{Modifiers: keymap.Primary, Text: "v", Target: targetCase.target})
				if !res.Handled {
					t.Fatalf("expected shortcut to be handled when docked SFTP editor is open: %+v", res)
				}
				if got := shell.typed(); got != "" {
					t.Fatalf("paste leaked to terminal shell: %q", got)
				}
				if state.modal != modalNone {
					t.Fatalf("paste opened unexpected modal %v", state.modal)
				}
				if got := view.editEditor.String(); got != initial {
					t.Fatalf("SFTP editor changed unexpectedly without input focus: %q (want %q)", got, initial)
				}
			})
		}
	}
}

func TestFileEditorPasteFocusDockedSFTPEditorAndTerminalTargeting(t *testing.T) {
	t.Run("focused-sftp-editor", func(t *testing.T) {
		state, session, _, _ := openTestSFTP(t)
		view := openSFTPTextEditor(t, state, session, "notes.txt")
		initial := view.editEditor.String()
		view.editEditor.SetSelection(text.Range{Start: view.editEditor.RuneCount(), End: view.editEditor.RuneCount()})
		state.options.ClipboardText = func() (string, error) { return "appended\r\nline", nil }

		shell := session.term.shell.(*fakeShell)
		res := state.Shortcut(gfx.KeyEvent{Modifiers: keymap.Primary, Text: "v", Target: contextID(hitSFTPEditor)})
		if !res.Handled {
			t.Fatalf("expected focused SFTP editor paste to be handled: %+v", res)
		}
		if got, want := view.editEditor.String(), initial+"appended\nline"; got != want {
			t.Fatalf("SFTP editor content = %q, want %q", got, want)
		}
		if got := shell.typed(); got != "" {
			t.Fatalf("paste leaked to terminal shell: %q", got)
		}
		if state.modal != modalNone {
			t.Fatalf("paste opened unexpected modal %v", state.modal)
		}
	})

	t.Run("focused-terminal-single-line", func(t *testing.T) {
		state, session, _, _ := openTestSFTP(t)
		view := openSFTPTextEditor(t, state, session, "notes.txt")
		initial := view.editEditor.String()
		state.options.ClipboardText = func() (string, error) { return "ls -la", nil }

		shell := session.term.shell.(*fakeShell)
		res := state.Shortcut(gfx.KeyEvent{Modifiers: keymap.Primary, Text: "v", Target: state.activeTerminalFocusHit()})
		if !res.Handled {
			t.Fatalf("expected terminal paste shortcut to be handled: %+v", res)
		}
		if got := shell.typed(); !strings.Contains(got, "ls -la") {
			t.Fatalf("expected terminal shell to receive paste, got %q", got)
		}
		if got := view.editEditor.String(); got != initial {
			t.Fatalf("SFTP editor changed unexpectedly: %q", got)
		}
	})

	t.Run("focused-terminal-multi-line", func(t *testing.T) {
		state, session, _, _ := openTestSFTP(t)
		view := openSFTPTextEditor(t, state, session, "notes.txt")
		initial := view.editEditor.String()
		state.options.ClipboardText = func() (string, error) { return "echo one\necho two", nil }

		shell := session.term.shell.(*fakeShell)
		res := state.Shortcut(gfx.KeyEvent{Modifiers: keymap.Primary, Text: "v", Target: state.activeTerminalFocusHit()})
		if !res.Handled {
			t.Fatalf("expected terminal multi-line paste shortcut to be handled: %+v", res)
		}
		if state.modal != modalPasteReview {
			t.Fatalf("expected modalPasteReview for focused terminal multi-line paste, got %v", state.modal)
		}
		if got := shell.typed(); got != "" {
			t.Fatalf("multi-line paste wrote directly to shell before review: %q", got)
		}
		if got := view.editEditor.String(); got != initial {
			t.Fatalf("SFTP editor changed unexpectedly: %q", got)
		}
	})
}
