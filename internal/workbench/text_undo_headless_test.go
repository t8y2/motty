//go:build darwin || windows || linux

package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

func TestAssistantMessageUndoRedoRunsThroughTheWindowTextPath(t *testing.T) {
	state, _ := panelState(t, quietModel{})
	state.Click(hitAIToggle)
	canvas, _ := paintAt(state, panelWide)
	box := panelHit(t, canvas, hitAIInput).Bounds

	var nativeInput app.TextInput
	driver, err := app.NewDriver(app.Config{
		Title: "assistant undo", Width: int(panelWide.Width), Height: int(panelWide.Height), Tree: Tree(),
		Paint: state.Paint,
		TextInput: func(context app.TextInputContext) *app.TextInput {
			if input := state.AIPanelInput(context); input != nil {
				return input
			}
			editor := state.EditorFor(context.ID)
			if editor == nil {
				return nil
			}
			nativeInput = app.TextInput{Editor: editor}
			return &nativeInput
		},
		OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut, OnScroll: state.Scroll,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()

	if err := driver.Click(box.X+box.Width/2, box.Y+box.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := driver.Type("draft question"); err != nil {
		t.Fatal(err)
	}
	if handled, err := driver.Shortcut('z'); err != nil || !handled || state.ai.input.String() != "" {
		t.Fatalf("undo handled=%v err=%v input=%q", handled, err, state.ai.input.String())
	}
	if handled, err := driver.ShortcutChord(gfx.ModifierMeta|gfx.ModifierShift, 'z'); err != nil || !handled || state.ai.input.String() != "draft question" {
		t.Fatalf("redo handled=%v err=%v input=%q", handled, err, state.ai.input.String())
	}
}
