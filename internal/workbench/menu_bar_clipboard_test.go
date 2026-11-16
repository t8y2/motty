package workbench

import (
	"errors"
	"testing"

	"github.com/vivaui/viva/components"
	"motty/internal/i18n"
	"motty/internal/term"
)

// AppKit invokes OnSelect for the Copy menu's key equivalent, bypassing the
// normal key-event result handler that writes ClipboardText to the clipboard.
func TestMenuBarCopyWritesSelectedTerminalText(t *testing.T) {
	state, session, _ := selectionState()
	session.term.write([]byte("alpha beta"))
	session.term.selection.selectRange(
		session.term.screen.PointAt(term.Position{}),
		session.term.screen.PointAt(term.Position{Column: 4}),
	)
	copied := "previous clipboard"
	state.options.SetClipboardText = func(value string) error {
		copied = value
		return nil
	}
	state.MenuBar()[1].Items[0].OnSelect()
	if copied != "alpha" {
		t.Fatalf("menu Copy left clipboard = %q, want alpha", copied)
	}
	if !session.term.selection.selected {
		t.Fatal("copy cleared the terminal selection")
	}
}

func TestMenuBarCopyWithoutSelectionPreservesClipboard(t *testing.T) {
	state, session, _ := selectionState()
	session.term.write([]byte("alpha beta"))
	state.options.SetClipboardText = func(value string) error {
		t.Fatalf("copy without selection replaced clipboard with %q", value)
		return nil
	}
	state.MenuBar()[1].Items[0].OnSelect()
}

func TestMenuBarCopyReportsClipboardFailure(t *testing.T) {
	state, session, _ := selectionState()
	state.messages = i18n.For(i18n.English)
	session.term.write([]byte("alpha beta"))
	session.term.selection.selectRange(
		session.term.screen.PointAt(term.Position{}),
		session.term.screen.PointAt(term.Position{Column: 4}),
	)
	want := errors.New("clipboard unavailable")
	state.options.SetClipboardText = func(string) error { return want }
	invalidations := 0
	state.options.Invalidate = func() { invalidations++ }
	state.MenuBar()[1].Items[0].OnSelect()
	// The main window paints none of state.status: the failure is only visible
	// if it reaches the shared notice queue.
	if len(state.fileNotices.items) != 1 {
		t.Fatalf("clipboard failure queued %v, want one visible notice", state.fileNotices.items)
	}
	notice, messages := state.fileNotices.items[0], state.messages
	if notice.title != messages.MenuCopy || notice.detail != messages.ActionFeedback.ClipboardFailed || notice.kind != components.ToastError {
		t.Fatalf("notice = %+v, want %q / %q", notice, messages.MenuCopy, messages.ActionFeedback.ClipboardFailed)
	}
	if invalidations != 1 {
		t.Fatalf("clipboard failure requested %d repaints, want 1", invalidations)
	}
}
