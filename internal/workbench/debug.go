//go:build dev

package workbench

import (
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/inspector"
	"github.com/vivaui/viva/keymap"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

// debugSource is the text the gallery's editor starts with: a short program that
// exercises indentation, strings, numbers, keywords, brackets and comments, so
// the highlighting, the tab stops and the bracket matching all have something to
// show.
const debugSource = `package main

import "fmt"

// greeting returns a line for the named person.
func greeting(name string) string {
	return fmt.Sprintf("hello, %s", name)
}

func main() {
	values := []int{1, 2, 3}
	for _, value := range values {
		if value%2 == 0 {
			fmt.Println(greeting("even"), value)
		}
	}
}
`

// The gallery's place in the sidebar and its controls. A release build has none
// of them: the page exists only with the dev tag (see debug_release.go).
var (
	debugHits       = tree.Feature("debug")
	hitDebug        = tree.ID(shellHits, "debug")
	hitDebugEditor  = tree.ID(debugHits, "editor")
	hitDebugScroll  = tree.ID(debugHits, "scroll")
	hitDebugScrollX = tree.ID(debugHits, "scroll-x")
)

// devState is the gallery's own state: the layout of the page and the editor it
// shows. The text, its undo history and its caret live in editor; code holds
// only what a steady paint would otherwise rebuild. A release build keeps none
// of it.
type devState struct {
	layout  flex.View
	editor  text.Editor
	code    components.CodeEditor
	started bool
	// inspector names the control under the pointer, and outlines the controls
	// of the frame, over the whole window. devInspectorChord cycles it.
	inspector inspector.Inspector
	// marks is the highlighter's scratch, reused by every line of every paint.
	marks []components.MarkdownCodeRun
	// highlight is debugHighlight held as a value: a method value made at paint
	// time would allocate a closure on every frame.
	highlight func(language, line string, dst []components.CodeEditorRun) []components.CodeEditorRun
}

// devEditor prepares the editor on its first frame. It starts with the caret at
// the beginning and the Go editing rules, the way a file window does.
func (state *State) devEditor() *text.Editor {
	if state.dev.started {
		return &state.dev.editor
	}
	state.dev.started = true
	state.dev.editor = *text.NewEditor(debugSource)
	state.dev.editor.SetSelection(text.Range{})
	state.dev.editor.SetCode(codeOptionsFor("debug.go", debugSource))
	state.dev.code = components.CodeEditor{}
	state.dev.highlight = state.debugHighlight
	return &state.dev.editor
}

// devDeclareNavigation puts the gallery in the sidebar, above Settings.
func (state *State) devDeclareNavigation(f *flex.Frame) {
	state.navigationItem(f, hitDebug, iconBug, state.messages.NavDebug, state.activeSection == sectionDebug, 4)
}

// devPlaysIconMotion reports whether id is the gallery's row. It is a sidebar
// row like the others, so its icon moves when it is chosen; only the row itself
// is missing from a release build.
func devPlaysIconMotion(id ui.ID) bool { return id == hitDebug }

// devClick opens the gallery from the sidebar and handles its controls.
func (state *State) devClick(id ui.ID) bool {
	if id == hitDebug {
		state.activeSection = sectionDebug
		state.save()
		return true
	}
	if id == hitDebugEditor {
		state.devEditor()
		return true
	}
	return false
}

// devBeginPaint forgets what the last paint laid out.
func (state *State) devBeginPaint() {}

// devInspectorChord is the shortcut that cycles the inspector: off, the control
// under the pointer, every control. Command or Control with Alt and Shift, so no
// terminal program or page binding comes near it.
var devInspectorChord = keymap.Chord{Modifiers: keymap.Primary | ui.ModifierAlt | ui.ModifierShift, Char: 'i'}

// devEndPaint paints the inspector over the finished frame.
func (state *State) devEndPaint(canvas *ui.Canvas, size ui.Size) {
	state.dev.inspector.Paint(canvas, ui.Rect{Width: size.Width, Height: size.Height})
}

// devShortcut cycles the inspector.
func (state *State) devShortcut(event ui.KeyEvent) (ui.EventResult, bool) {
	if chord, ok := keymap.FromEvent(event); !ok || chord != devInspectorChord {
		return ui.EventResult{}, false
	}
	if !event.Release && !event.Repeat {
		state.dev.inspector.Cycle()
	}
	return ui.EventResult{Handled: true, Repaint: true}, true
}

// devPaintPage paints the gallery and reports whether it was the page on show.
func (state *State) devPaintPage(canvas *ui.Canvas, bounds ui.Rect) bool {
	if state.activeSection != sectionDebug {
		return false
	}
	state.paintDebugWorkspace(canvas, bounds)
	return true
}

// devAddPaletteCommands lists the gallery on the command palette.
func (state *State) devAddPaletteCommands(palette *commandPalette) {
	palette.add(state.messages.NavDebug, hitDebug)
}

// devWorkspaceName names the gallery as a workspace to come back to.
func (state *State) devWorkspaceName() (string, bool) {
	if state.activeSection == sectionDebug {
		return "debug", true
	}
	return "", false
}

// devRestoreWorkspace opens the gallery again when it was the workspace saved.
func (state *State) devRestoreWorkspace(workspace string) bool {
	if workspace != "debug" {
		return false
	}
	state.activeSection = sectionDebug
	return true
}

// devEditorFor hands the gallery's editor to the text system, so typing reaches
// it the way it reaches a file window: composition, paste and the caret are the
// framework's, and only the text stays here.
func (state *State) devEditorFor(hit ui.ID) *text.Editor {
	if state.activeSection != sectionDebug || state.modal != modalNone || hit != hitDebugEditor {
		return nil
	}
	return state.devEditor()
}

// devEditorInput connects the gallery's editor to a native text event.
func (state *State) devEditorInput() *app.TextInput {
	editor := state.devEditor()
	var input app.TextInput
	input.Editor = nil
	input.State = app.TextStateOf(editor)
	input.OnEvent = func(event app.TextEvent) (app.TextState, error) {
		event.Apply(editor)
		return app.TextStateOf(editor), nil
	}
	return &input
}

// devEditorKey is what the keys of the gallery's editor do: the editing commands
// and the movement keys, exactly as in a file window.
func (state *State) devEditorKey(event ui.KeyEvent) (ui.EventResult, bool) {
	if state.activeSection != sectionDebug || state.modal != modalNone || event.Target != hitDebugEditor {
		return ui.EventResult{}, false
	}
	if event.Release {
		return ui.EventResult{}, false
	}
	editor := state.devEditor()
	handled := ui.EventResult{Handled: true, Repaint: true}
	if command, ok := components.CodeEditorCommandForKey(event); ok && command.Apply(editor) {
		state.dev.code.Reveal(editor.ByteOffset(editor.Caret()))
		return handled, true
	}
	if movement, extend := components.TextareaMovementForKey(event.Key, event.Modifiers); movement != components.TextareaMoveNone {
		value := editor.String()
		offset := state.dev.code.Move(value, editor.ByteOffset(editor.Caret()), movement)
		editor.MoveTo(len([]rune(value[:offset])), extend)
		return handled, true
	}
	return ui.EventResult{}, false
}

// devEditorPointer routes pointer events to the editor, which uses them for its
// scrollbars. Placing the caret is the text system's job, not the page's.
func (state *State) devEditorPointer(event ui.PointerEvent) (ui.EventResult, bool) {
	if state.activeSection != sectionDebug || state.modal != modalNone {
		return ui.EventResult{}, false
	}
	if event.Target != hitDebugScroll && event.Target != hitDebugScrollX {
		return ui.EventResult{}, false
	}
	return state.dev.code.Pointer(event), true
}

// devEditorScroll scrolls the gallery's editor over it.
func (state *State) devEditorScroll(event ui.ScrollEvent) (ui.EventResult, bool) {
	if state.activeSection != sectionDebug || state.modal != modalNone {
		return ui.EventResult{}, false
	}
	return state.dev.code.Scroll(event), true
}
