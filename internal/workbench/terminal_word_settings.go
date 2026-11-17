package workbench

import (
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

func (page *settingsPage) wordSeparatorsRow() {
	label := page.state.messages.Tools.WordSeparatorsRow
	slot := page.lineFor(hitSettingsWordSeparators, label, page.state.messages.Tools.WordSeparatorsDescription, 100, nil)
	page.add(settingsControl{kind: controlWordSeparators, slot: slot})
}

func (state *State) paintWordSeparatorsInput(canvas *gfx.Canvas, bounds gfx.Rect) {
	label := state.messages.Tools.WordSeparatorsRow
	value := state.terminalWordEditor.String()
	focused := canvas.Focused() == hitSettingsWordSeparators
	components.PaintInput(canvas, state.Theme(), bounds, components.InputSnapshot{
		ID: hitSettingsWordSeparators, Label: label, Value: value,
		Placeholder: state.messages.Tools.WordSeparatorsPlaceholder,
		Focused:     focused, PlatformEditing: focused && value != "",
	})
}

func (state *State) TerminalWordInput(context app.TextInputContext) *app.TextInput {
	if context.ID != hitSettingsWordSeparators || state.modal != modalNone || state.activeSection != sectionSettings || state.settingsCategory != settingsTerminal {
		return nil
	}
	return &app.TextInput{
		State: app.TextStateOf(&state.terminalWordEditor),
		OnEvent: func(event app.TextEvent) (app.TextState, error) {
			event.Apply(&state.terminalWordEditor)
			if _, composing := state.terminalWordEditor.Composition(); !composing {
				value := state.terminalWordEditor.String()
				if value != state.terminalWordSeparators {
					state.terminalWordSeparators = value
					state.save()
				}
			}
			return app.TextStateOf(&state.terminalWordEditor), nil
		},
	}
}
