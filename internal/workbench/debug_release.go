//go:build !dev

package workbench

import (
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

// A release build has no component gallery. The page, its sidebar entry, its
// palette command, its workspace and its editor are built only with the dev tag
// (see debug.go and debug_paint.go); these say so to the shell code that
// integrates every page, so that code reads the same in both builds.

// devState holds nothing: a release build has no gallery state.
type devState struct{}

func (state *State) devDeclareNavigation(*ui.Frame) {}

func devPlaysIconMotion(gfx.ID) bool { return false }

func (state *State) devClick(gfx.ID) bool { return false }

func (state *State) devBeginPaint() {}

func (state *State) devEndPaint(*gfx.Canvas, gfx.Size) {}

func (state *State) devShortcut(gfx.KeyEvent) (gfx.EventResult, bool) {
	return gfx.EventResult{}, false
}

func (state *State) devPaintPage(*gfx.Canvas, gfx.Rect) bool { return false }

func (state *State) devAddPaletteCommands(*commandPalette) {}

func (state *State) devWorkspaceName() (string, bool) { return "", false }

func (state *State) devRestoreWorkspace(string) bool { return false }

func (state *State) devEditorFor(gfx.ID) *text.Editor { return nil }

func (state *State) devEditorInput() *app.TextInput { return nil }

func (state *State) devEditorKey(gfx.KeyEvent) (gfx.EventResult, bool) {
	return gfx.EventResult{}, false
}

func (state *State) devEditorPointer(gfx.PointerEvent) (gfx.EventResult, bool) {
	return gfx.EventResult{}, false
}

func (state *State) devEditorScroll(gfx.ScrollEvent) (gfx.EventResult, bool) {
	return gfx.EventResult{}, false
}
