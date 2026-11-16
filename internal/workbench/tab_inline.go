package workbench

import (
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	layoutcore "motty/internal/workspace"
)

// hitTabInline is the tab-title field while one tab is renamed in place, the
// way a double click renames a file in a file manager.
var hitTabInline = tree.ID(shellHits, "tab-inline")

// tabInline is the in-place rename of one tab: the workspace whose title is
// typed over and the editor holding the draft. blink is the phase of the caret
// the tab paints itself.
type tabInline struct {
	workspace layoutcore.WorkspaceID
	editor    text.Editor
	blink     components.CaretBlink
}

// tabInlineActive says a tab is being renamed in place right now.
func (state *State) tabInlineActive() bool {
	return state.tabInline.workspace != 0
}

// tabInlineEditing says the given tab is the one being renamed in place. A file
// tab carries no workspace, so an idle editor must not match it.
func (state *State) tabInlineEditing(id layoutcore.WorkspaceID) bool {
	return state.tabInlineActive() && state.tabInline.workspace == id
}

// beginTabInline renames a tab in place on its tab, starting from the name it
// shows now, all of it selected so typing replaces the name.
func (state *State) beginTabInline(id layoutcore.WorkspaceID) {
	index, ok := state.workspaceTab(id)
	if !ok || state.modal != modalNone {
		return
	}
	state.tabInline = tabInline{workspace: id}
	state.tabInline.editor.Insert(state.tabLabel(state.workspaceTabs[index]))
	state.tabInline.editor.SetSelection(text.Range{End: state.tabInline.editor.RuneCount()})
}

// commitTabInline applies the drafted name and closes the field. It reports
// false while an input method is composing, leaving the field open for the
// caller to hand focus back. An emptied name keeps the tab's old one: there is
// no form here to complain in, so the edit simply comes to nothing.
func (state *State) commitTabInline() bool {
	if !state.tabInlineActive() {
		return true
	}
	if r, ok := state.tabInline.editor.Composition(); ok && r.Start != r.End {
		return false
	}
	workspace := state.tabInline.workspace
	name := strings.TrimSpace(state.tabInline.editor.String())
	state.tabInline = tabInline{}
	if name == "" {
		return true
	}
	state.applyTabRename(workspace, name)
	return true
}

// cancelTabInline drops the draft and keeps the tab's current name.
func (state *State) cancelTabInline() {
	state.tabInline = tabInline{}
}

// tabInlineKey ends the rename from the keyboard: Escape keeps the old name,
// Enter and Tab apply the drafted one, and both give the keyboard back to the
// tab so the roving among tabs continues from it. While an input method
// composes, the keys belong to the method, not the field.
func (state *State) tabInlineKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if !state.tabInlineActive() || state.modal != modalNone || event.Release || event.Repeat {
		return gfx.EventResult{}, false
	}
	if r, ok := state.tabInline.editor.Composition(); ok && r.Start != r.End {
		return gfx.EventResult{}, false
	}
	tab := hitTabList.At(int(state.tabInline.workspace))
	switch event.Key {
	case gfx.KeyEscape:
		state.cancelTabInline()
	case gfx.KeyEnter, gfx.KeyTab:
		if event.Target != hitTabInline {
			return gfx.EventResult{}, false
		}
		state.commitTabInline()
	default:
		return gfx.EventResult{}, false
	}
	result := gfx.EventResult{Handled: true, Repaint: true, Focus: tab}
	if event.Key == gfx.KeyTab {
		result.FocusVisible = true
	}
	return result, true
}

// tabInlineScroll is how far the name's line is moved left so the caret stays
// in view: zero while the line fits, otherwise enough to keep the caret just
// inside the field's trailing edge. The caret is where typing goes, so the line
// follows it even while text is selected. A tab keeps its width while it is
// named, so a name wider than the tab scrolls under it rather than pushing the
// tabs beside it aside.
func tabInlineScroll(canvas *gfx.Canvas, title gfx.Rect, value string, caret int) float32 {
	// Keep a caret's width of room at either edge, so the caret never sits on
	// the clip's boundary where it would not be drawn.
	const edge = float32(2)
	x := canvas.TextCaretStyle(value, gfx.TextStyle{Size: metrics.FontSizeS, Weight: gfx.WeightMedium}, caret)
	room := title.Width - 2*edge
	if x <= room {
		return 0
	}
	return x - room
}

// paintTabInlineField paints the in-place name field into the tab's title. The
// tab is the field's dressing; the content is the framework's, so editing,
// selecting and composing behave exactly as they do in a form's field. The text
// follows the tab title's own style, so the name does not jump when the field
// opens over it.
//
// The field paints its own text, selection, composition and caret, and the
// platform's editor is only where typing and the input method arrive. The
// platform's own caret hides while text is selected, which is exactly how a
// rename opens (the whole name selected), so a field that let the platform draw
// it would show no caret where the user is about to type.
func (state *State) paintTabInlineField(canvas *gfx.Canvas, title gfx.Rect) {
	focused := canvas.Focused() == hitTabInline
	editor := &state.tabInline.editor
	value := editor.String()
	caret := editor.ByteOffset(editor.Caret())
	selection := editor.Selection()
	// The content rectangle is the field's window onto the line: sliding it left
	// scrolls a long name under a tab that keeps its width. The hit and the clip
	// stay on the title, so nothing is drawn over the tabs beside it.
	content := title
	if scroll := tabInlineScroll(canvas, title, value, caret); scroll > 0 {
		// The line slides left, and its rectangle grows by the same amount so
		// its right edge stays on the tab's: the component clips the text to
		// this rectangle, and a rectangle that slid without growing would cut
		// off the caret it scrolled into view.
		content.X -= scroll
		content.Width += scroll
	}
	canvas.Clip(title)
	defer canvas.Unclip()
	components.PaintInputContent(canvas, state.theme, title, content, components.InputSnapshot{
		ID: hitTabInline, Label: state.messages.RenameTabField, Value: value,
		Placeholder: state.messages.RenameTabPlaceholder, Focused: focused,
		HideEditorText: true, ShowCaret: focused, Blink: &state.tabInline.blink,
		Caret:            caret,
		SelectionStart:   editor.ByteOffset(selection.Start),
		SelectionEnd:     editor.ByteOffset(selection.End),
		CompositionStart: state.tabInline.composition().Start,
		CompositionEnd:   state.tabInline.composition().End,
		Text:             components.InputTextStyle{Size: metrics.FontSizeS, Weight: gfx.WeightMedium},
	})
}

// composition is the input method's marked range in bytes, empty when nothing
// is being composed.
func (edit *tabInline) composition() text.Range {
	marked, ok := edit.editor.Composition()
	if !ok {
		return text.Range{}
	}
	return text.Range{Start: edit.editor.ByteOffset(marked.Start), End: edit.editor.ByteOffset(marked.End)}
}
