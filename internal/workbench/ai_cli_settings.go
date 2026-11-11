package workbench

import (
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/ai/cli"
	"motty/internal/ai/store"
)

// aiFormRoot names the provider form's controls in the page it is declared in.
const aiFormRoot = "ai-form"

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

// bindAIFormFields points the provider form's page handles at the editors the
// application reads. The editors keep their addresses across the config's life,
// so this runs once; a closed form clears their text, not the handles.
func (cfg *aiSettingsUI) bindAIFormFields() {
	if cfg.formBound {
		return
	}
	cfg.formBound = true
	cfg.formName.Bind(&cfg.name)
	cfg.formCLIPath.Bind(&cfg.cliPath)
	cfg.formBaseURL.Bind(&cfg.baseURL)
	cfg.formModelsURL.Bind(&cfg.modelsURL)
	cfg.formAPIKey.Bind(&cfg.apiKey)
}

func (state *State) aiProviderFormGeometry() (title, description, submit string, height float32) {
	m := state.messages.AISettings
	cfg := &state.ai.cfg
	cli := store.IsCLIKind(cfg.kind)
	title, submit = m.FormAddTitle, m.FormAdd
	description = m.FormDescription
	if cli {
		description = m.CLIFormDescription
		height = 182
		if cfg.editID != "" {
			title, submit, height = m.FormEditTitle, m.FormSave, 122
			title += " (" + aiPresets[aiPresetOf(store.Provider{Kind: cfg.kind})].label + ")"
		}
		if cfg.cliAutoPath != "" && cfg.cliPath.String() == cfg.cliAutoPath {
			height += 28
		} else if cfg.cliPathMissing && strings.TrimSpace(cfg.cliPath.String()) == "" {
			height += 28
		}
		return title, description, submit, height
	}
	presetHeight := float32(60) // a new provider can start from a preset
	if cfg.editID != "" {
		title, submit, presetHeight = m.FormEditTitle, m.FormSave, 0
	}
	keyHeight := float32(60) // a saved key adds a line saying so under the field
	if cfg.hadKey {
		keyHeight = 78
	}
	authHeight := float32(0) // only Anthropic has a choice of credential header
	if cfg.kind == store.KindAnthropic {
		authHeight = 60
	}
	formatHeight := float32(0) // only an OpenAI-compatible provider has a choice of protocol; the route under it adds a line and a gap
	if cfg.kind == store.KindOpenAI {
		formatHeight = 84
	}
	height = 2 + presetHeight + 4*60 + authHeight + formatHeight + keyHeight + 4
	return title, description, submit, height
}

func (state *State) declareAIProviderFormDialog(c ui.C) {
	if !state.aiSettingsVisible() || state.ai.cfg.dialog != aiDialogProvider {
		return
	}
	cfg := &state.ai.cfg
	cfg.bindAIFormFields()
	title, description, submit, bodyHeight := state.aiProviderFormGeometry()
	const width float32 = 500
	viewport := gfx.Rect{Width: c.Bounds().Width, Height: c.Bounds().Height}
	layout := components.BeginDialog(c.Canvas(), *c.Theme(), viewport, components.DialogOptions{
		ContentSize: gfx.Size{Width: width}, FitBody: true,
	}).Layout(description)
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: title, Description: description, Width: width,
		Height: layout.Content.Height + withError(bodyHeight, cfg.formErr), Layer: dialogLayer,
		Scope: dialogScope, CloseID: hitAISetFormClose, InitialFocusID: hitAISetFormName,
	})
	dialog.Body(func() { state.declareAIProviderForm(c) })
	dialog.Footer(func() {
		if ui.Button(c, state.messages.Cancel).Secondary().Shrink(0).Clicked() {
			c.After(func() { state.aiDialogClick(hitAISetFormCancel) })
		}
		if ui.Button(c, submit).Shrink(0).Clicked() {
			c.After(func() { state.aiDialogClick(hitAISetFormSubmit) })
		}
	})
	if dialog.Dismissed() {
		state.aiCloseDialog()
	}
}

// declareAIProviderForm declares the provider form: the preset a new provider
// starts from, then the fields and choices that provider kind needs.
func (state *State) declareAIProviderForm(c ui.C) {
	cfg, m := &state.ai.cfg, state.messages.AISettings
	cli := store.IsCLIKind(cfg.kind)
	kind := cfg.kind
	cfg.formPreset = ui.Box{}
	ui.Column(c).Fill().PadSides(2, 4, 0, 4).Children(func() {
		if cfg.editID == "" {
			cfg.formPreset = ui.Slot(c).Height(60)
			// The preset list is a Select on the page; its open list paints on a
			// higher layer, over the fields under it.
			cfg.formPreset.Draw(func(d *ui.DrawContext) { state.paintAIPresetField(d.Canvas, d.Rect) })
		}
		state.aiFormField(c, hitAISetFormName, &cfg.formName, m.FormName, m.FormNamePH, "", 60, aiFormFocusName)
		if cli {
			hint := ""
			if cfg.cliAutoPath != "" && cfg.cliPath.String() == cfg.cliAutoPath {
				hint = m.CLIPathDetected
			} else if cfg.cliPathMissing && strings.TrimSpace(cfg.cliPath.String()) == "" {
				hint = m.CLIPathNotFound
			}
			pathHeight := float32(60)
			if hint != "" {
				pathHeight = 88
			}
			state.aiFormField(c, hitAISetFormCLIPath, &cfg.formCLIPath, m.CLIPath, m.CLIPathDefault, hint, pathHeight, aiFormFocusCLIPath)
		} else {
			cfg.formKind = kind
			if cfg.formKind != store.KindAnthropic {
				cfg.formKind = store.KindOpenAI
			}
			state.aiFormChoice(c, m.FormKind, "", &cfg.formKind, aiFormKindChoices[:], state.aiProviderKindLabel, 60, func() { cfg.kind = cfg.formKind })
			if kind == store.KindAnthropic {
				cfg.formAuth = cfg.authStyle
				if cfg.formAuth != store.AuthStyleBearer {
					cfg.formAuth = store.AuthStyleAPIKey
				}
				state.aiFormChoice(c, m.FormAuth, "", &cfg.formAuth, aiFormAuthChoices[:], state.aiAuthStyleLabel, 60, func() { cfg.authStyle = cfg.formAuth })
			}
			placeholder := m.FormBaseURLOpenAI
			if kind == store.KindAnthropic {
				placeholder = m.FormBaseURLClaude
			}
			state.aiFormField(c, hitAISetFormURL, &cfg.formBaseURL, m.FormBaseURL, placeholder, "", 60, aiFormFocusURL)
			state.aiFormField(c, hitAISetFormModelsURL, &cfg.formModelsURL, m.FormModelsURL, m.FormModelsURLPH, "", 60, aiFormFocusModelsURL)
			if kind == store.KindOpenAI {
				cfg.formFormat = cfg.apiFormat
				if cfg.formFormat != store.APIFormatResponses {
					cfg.formFormat = ""
				}
				route := m.FormFormatChatRoute
				if cfg.formFormat == store.APIFormatResponses {
					route = m.FormFormatResponsesRoute
				}
				state.aiFormChoice(c, m.FormFormat, route, &cfg.formFormat, aiFormFormatChoices[:], state.aiFormatLabel, 84, func() { cfg.apiFormat = cfg.formFormat })
			}
			hint := ""
			keyHeight := float32(60)
			if cfg.hadKey {
				hint, keyHeight = m.FormKeySaved, 78
			}
			state.aiFormKeyField(c, hitAISetFormKey, &cfg.formAPIKey, m.FormKey, m.FormKeyPH, hint, keyHeight, aiFormFocusKey)
		}
		if cfg.formErr != "" {
			problem := ui.Text(c, cfg.formErr).FontSize(metrics.FontSizeS).Color(state.pal.danger).LineHeight(18)
			if !cli {
				problem.Weight(gfx.WeightMedium)
			}
		}
	})
}

// aiFormField declares one labelled text field of the form: the title row, the
// control, and the hint under it, in the rhythm components.BeginField gives.
func (state *State) aiFormField(c ui.C, id gfx.ID, field *ui.Field, title, placeholder, hint string, height float32, focus aiFormFocus) {
	theme := c.Theme()
	m := theme.Metrics
	gap := m.GapItem / 2
	ui.Column(c).Height(height).Children(func() {
		ui.Text(c, title).FontSize(m.FontSizeM).Weight(gfx.WeightMedium).Color(theme.Foreground).LineHeight(m.FontSizeM + 4).Ellipsis()
		box := ui.TextFieldID(c, id, field, title, placeholder).Height(m.ControlHeight).MarginSides(gap, 0, 0, 0)
		ui.FocusBind(box, &state.ai.cfg.formFocus, focus)
		if hint != "" {
			ui.Text(c, hint).FontSize(m.FontSizeM).Color(theme.MutedForeground).LineHeight(m.FontSizeM+7).MarginSides(gap, 0, 0, 0)
		}
	})
}

// aiFormKeyField is aiFormField for the API key, which shows bullets until its
// eye button shows the text.
func (state *State) aiFormKeyField(c ui.C, id gfx.ID, field *ui.Field, title, placeholder, hint string, height float32, focus aiFormFocus) {
	cfg := &state.ai.cfg
	theme := c.Theme()
	m := theme.Metrics
	gap := m.GapItem / 2
	field.Secure = !cfg.formKeyShown
	ui.Column(c).Height(height).Children(func() {
		ui.Text(c, title).FontSize(m.FontSizeM).Weight(gfx.WeightMedium).Color(theme.Foreground).LineHeight(m.FontSizeM + 4).Ellipsis()
		box := ui.TextFieldID(c, id, field, title, placeholder).Height(m.ControlHeight).MarginSides(gap, 0, 0, 0)
		ui.FocusBind(box, &state.ai.cfg.formFocus, focus)
		icon, label := lucideIcons[iconEye], state.messages.ShowSecret
		if cfg.formKeyShown {
			icon, label = lucideIcons[iconEyeOff], state.messages.HideSecret
		}
		box.Action(icon, label)
		if box.ActionClicked() {
			cfg.formKeyShown = !cfg.formKeyShown
			if state.options.Invalidate != nil {
				state.options.Invalidate()
			}
		}
		if hint != "" {
			ui.Text(c, hint).FontSize(m.FontSizeM).Color(theme.MutedForeground).LineHeight(m.FontSizeM+7).MarginSides(gap, 0, 0, 0)
		}
	})
}

// aiFormChoice declares one labelled choice of the form, with the explanation
// the choice's meaning needs under it.
func (state *State) aiFormChoice(c ui.C, title, description string, value *string, choices []string, label func(string) string, height float32, changed func()) {
	theme := c.Theme()
	m := theme.Metrics
	gap := m.GapItem / 2
	ui.Column(c).Height(height).Children(func() {
		ui.Text(c, title).FontSize(m.FontSizeM).Weight(gfx.WeightMedium).Color(theme.Foreground).LineHeight(m.FontSizeM + 4).Ellipsis()
		box := ui.Choice(c, value, choices, label).Height(m.ControlHeight).MarginSides(gap, 0, 0, 0)
		if description != "" {
			ui.Text(c, description).FontSize(m.FontSizeM).Color(theme.MutedForeground).LineHeight(m.FontSizeM+7).MarginSides(gap, 0, 0, 0)
		}
		if box.Changed() {
			changed()
		}
	})
}
