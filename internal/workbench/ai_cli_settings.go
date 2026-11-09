package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/ai/cli"
	"motty/internal/ai/store"
	"strings"
)

func (state *State) aiDiscoverCLIPath() {
	cfg := &state.ai.cfg
	if !store.IsCLIKind(cfg.kind) || strings.TrimSpace(cfg.cliPath.String()) != "" {
		return
	}
	path, err := cli.ExecutablePath(cfg.kind, "")
	cfg.cliPathMissing = err != nil
	if err != nil {
		return
	}
	cfg.cliAutoPath = path
	cfg.cliPath.Insert(path)
	cfg.cliPath.SetSelection(text.Range{Start: 0, End: cfg.cliPath.RuneCount()})
}

func (state *State) paintAICLIProviderForm(canvas *gfx.Canvas, viewport gfx.Rect) {
	cfg, m, theme := &state.ai.cfg, state.messages.AISettings, state.Theme()
	title, submit := m.FormAddTitle, m.FormAdd
	height := float32(182)
	if cfg.editID != "" {
		title, submit, height = m.FormEditTitle, m.FormSave, 122
		title += " (" + aiPresets[aiPresetOf(store.Provider{Kind: cfg.kind})].label + ")"
	}
	hint := ""
	if cfg.cliAutoPath != "" && cfg.cliPath.String() == cfg.cliAutoPath {
		hint = m.CLIPathDetected
	} else if cfg.cliPathMissing && strings.TrimSpace(cfg.cliPath.String()) == "" {
		hint = m.CLIPathNotFound
	}
	pathHeight := float32(60)
	if hint != "" {
		pathHeight, height = 88, height+28
	}
	// No scrim dismiss: a press beside the form must not discard its input.
	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope, CloseID: hitAISetFormClose,
		InitialFocusID: hitAISetFormName, ContentSize: gfx.Size{Width: 500}, FitBody: true, BodyHeight: withError(height, cfg.formErr),
	})
	layout := dialog.Begin(title, m.CLIFormDescription)
	var preset, name, path ui.Box
	f := state.settingsRow.Begin(canvas, theme, layout.Body)
	f.Column().Fill().PadSides(2, 4, 0, 4).Children(func() {
		if cfg.editID == "" {
			preset = f.Slot().Height(60)
		}
		name = f.Slot().Height(60)
		path = f.Slot().Height(pathHeight)
		if cfg.formErr != "" {
			f.Text(cfg.formErr).FontSize(metrics.FontSizeS).Color(state.pal.danger).LineHeight(18)
		}
	})
	f.End()
	if cfg.editID == "" {
		state.paintAIPresetField(canvas, preset.Rect())
	}
	state.aiFormField(canvas, hitAISetFormName, m.FormName, m.FormNamePH, "", false, &cfg.name, name.Rect())
	state.aiFormField(canvas, hitAISetFormCLIPath, m.CLIPath, m.CLIPathDefault, hint, false, &cfg.cliPath, path.Rect())
	dialog.Actions(layout, components.ButtonAction{ID: hitAISetFormCancel, Label: state.messages.Cancel, Variant: components.ButtonSecondary}, components.ButtonAction{ID: hitAISetFormSubmit, Label: submit})
	dialog.End()
}
