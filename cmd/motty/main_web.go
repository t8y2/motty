//go:build js && wasm

package main

import (
	"fmt"

	platform "github.com/vivaui/viva/internal/platform"
	"github.com/vivaui/viva/internal/platform/web"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/workbench"
)

func main() {
	state := workbench.New()
	theme := state.Theme()
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})

	var nativeInput web.TextInput
	handleText := func(event web.TextEvent) (web.TextState, error) {
		editor := state.ActiveEditor()
		if editor == nil {
			return web.TextState{}, fmt.Errorf("no active editor")
		}
		state.ApplyWebTextEvent(event)
		return editorState(editor), nil
	}
	resolveInput := func(context web.TextInputContext) *web.TextInput {
		focus, editing := state.TextEditing()
		if !editing || context.ID != focus {
			return nil
		}
		editor := state.ActiveEditor()
		if editor == nil {
			return nil
		}
		nativeInput = web.TextInput{
			Bounds:   ui.Rect{X: context.Bounds.X + 10, Y: context.Bounds.Y, Width: max(context.Bounds.Width-20, 1), Height: context.Bounds.Height},
			FontSize: 13, Color: theme.Foreground,
			State: editorState(editor), OnEvent: handleText,
		}
		return &nativeInput
	}

	web.Run(web.Config{
		Title:     "Motty",
		Width:     1400,
		Height:    900,
		Paint:     state.Paint,
		OnKey:     state.Key,
		TextInput: resolveInput,
	})
}

func editorState(editor *text.Editor) platform.TextState {
	selection := editor.Selection()
	composition, composing := editor.Composition()
	return platform.TextState{
		Value:       editor.String(),
		Selection:   platform.TextRange{Start: selection.Start, End: selection.End},
		Composition: platform.TextRange{Start: composition.Start, End: composition.End},
		Composing:   composing,
	}
}
