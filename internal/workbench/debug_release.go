//go:build !dev

package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"
)

// A release build has no component gallery. The page, its sidebar entry, its
// palette command, its workspace and its controls are built only with the dev
// tag (see debug.go and debug_paint.go); these say so to the shell code that
// integrates every page, so that code reads the same in both builds.

// devState holds nothing: a release build has no gallery state.
type devState struct{}

func (state *State) devDeclareNavigation(*flex.Frame) {}

func (state *State) devClick(ui.ID) bool { return false }

func (state *State) devBeginPaint() {}

func (state *State) devPaintPage(*ui.Canvas, ui.Rect) bool { return false }

func (state *State) devPaintOverlay(*ui.Canvas) {}

func (state *State) devAddPaletteCommands(*commandPalette) {}

func (state *State) devWorkspaceName() (string, bool) { return "", false }

func (state *State) devRestoreWorkspace(string) bool { return false }

func (state *State) devSwatchKey(ui.KeyEvent) (ui.EventResult, bool) { return ui.EventResult{}, false }

func (state *State) devSelectTypeahead(ui.ID) ([]components.MenuTypeaheadItem, func(ui.ID) bool, bool) {
	return nil, nil, false
}
