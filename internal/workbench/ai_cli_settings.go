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

// ensureAIFormPage makes the page the provider form is declared in. Its fields
// are bound once to the editors the application reads, so typing reaches them
// with the platform's editing, input methods and clipboard.
func (state *State) ensureAIFormPage() *ui.Page {
	cfg := &state.ai.cfg
	if cfg.formPage != nil {
		return cfg.formPage
	}
	cfg.formPage = ui.NewPage(ui.PageConfig{Root: aiFormRoot, Theme: &state.theme})
	cfg.formName.Bind(&cfg.name)
	cfg.formCLIPath.Bind(&cfg.cliPath)
	cfg.formBaseURL.Bind(&cfg.baseURL)
	cfg.formModelsURL.Bind(&cfg.modelsURL)
	cfg.formAPIKey.Bind(&cfg.apiKey)
	return cfg.formPage
}

// paintAIProviderForm paints the provider dialog. The form inside it is one
// page, whatever kind of provider it is adding or editing.
func (state *State) paintAIProviderForm(canvas *gfx.Canvas, viewport gfx.Rect) {
	m, theme := state.messages.AISettings, state.Theme()
	cfg := &state.ai.cfg
	cli := store.IsCLIKind(cfg.kind)
	title, submit := m.FormAddTitle, m.FormAdd
	description := m.FormDescription
	height := float32(0)
	hint := ""
	if cli {
		description, height = m.CLIFormDescription, 182
		if cfg.editID != "" {
			title, submit, height = m.FormEditTitle, m.FormSave, 122
			title += " (" + aiPresets[aiPresetOf(store.Provider{Kind: cfg.kind})].label + ")"
		}
		if cfg.cliAutoPath != "" && cfg.cliPath.String() == cfg.cliAutoPath {
			hint = m.CLIPathDetected
		} else if cfg.cliPathMissing && strings.TrimSpace(cfg.cliPath.String()) == "" {
			hint = m.CLIPathNotFound
		}
		if hint != "" {
			height += 28
		}
	} else {
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
	}
	// No scrim dismiss: a press beside the form must not discard its input.
	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope, CloseID: hitAISetFormClose,
		InitialFocusID: hitAISetFormName, ContentSize: gfx.Size{Width: 500}, FitBody: true, BodyHeight: withError(height, cfg.formErr),
	})
	layout := dialog.Begin(title, description)
	page := state.ensureAIFormPage()
	page.Draw(canvas, layout.Body, state.declareAIProviderForm)
	// An open preset list lies over the fields under it, so it is painted after
	// the page; closed, the list paints itself in its place and Tab reaches it
	// before the fields.
	if cfg.presetOpen && cfg.editID == "" {
		state.paintAIPresetField(canvas, cfg.formPreset.Rect())
	}
	dialog.Actions(layout,
		components.ButtonAction{ID: hitAISetFormCancel, Label: state.messages.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitAISetFormSubmit, Label: submit},
	)
	dialog.End()
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
			if !cfg.presetOpen {
				cfg.formPreset.Draw(func(d *ui.DrawContext) { state.paintAIPresetField(d.Canvas, d.Rect) })
			}
		}
		state.aiFormField(c, hitAISetFormName, &cfg.formName, m.FormName, m.FormNamePH, "", 60)
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
			state.aiFormField(c, hitAISetFormCLIPath, &cfg.formCLIPath, m.CLIPath, m.CLIPathDefault, hint, pathHeight)
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
			state.aiFormField(c, hitAISetFormURL, &cfg.formBaseURL, m.FormBaseURL, placeholder, "", 60)
			state.aiFormField(c, hitAISetFormModelsURL, &cfg.formModelsURL, m.FormModelsURL, m.FormModelsURLPH, "", 60)
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
			state.aiFormKeyField(c, hitAISetFormKey, &cfg.formAPIKey, m.FormKey, m.FormKeyPH, hint, keyHeight)
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
func (state *State) aiFormField(c ui.C, id gfx.ID, field *ui.Field, title, placeholder, hint string, height float32) {
	theme := c.Theme()
	m := theme.Metrics
	gap := m.GapItem / 2
	ui.Column(c).Height(height).Children(func() {
		ui.Text(c, title).FontSize(m.FontSizeM).Weight(gfx.WeightMedium).Color(theme.Foreground).LineHeight(m.FontSizeM + 4).Ellipsis()
		ui.TextFieldID(c, id, field, title, placeholder).Height(m.ControlHeight).MarginSides(gap, 0, 0, 0)
		if hint != "" {
			ui.Text(c, hint).FontSize(m.FontSizeM).Color(theme.MutedForeground).LineHeight(m.FontSizeM+7).MarginSides(gap, 0, 0, 0)
		}
	})
}

// aiFormKeyField is aiFormField for the API key, which shows bullets until its
// eye button shows the text.
func (state *State) aiFormKeyField(c ui.C, id gfx.ID, field *ui.Field, title, placeholder, hint string, height float32) {
	cfg := &state.ai.cfg
	theme := c.Theme()
	m := theme.Metrics
	gap := m.GapItem / 2
	field.Secure = !cfg.formKeyShown
	ui.Column(c).Height(height).Children(func() {
		ui.Text(c, title).FontSize(m.FontSizeM).Weight(gfx.WeightMedium).Color(theme.Foreground).LineHeight(m.FontSizeM + 4).Ellipsis()
		box := ui.TextFieldID(c, id, field, title, placeholder).Height(m.ControlHeight).MarginSides(gap, 0, 0, 0)
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
