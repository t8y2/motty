package workbench

import (
	"fmt"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/ai/gate"
	"motty/internal/ai/probe"
	"motty/internal/ai/risk"
	"motty/internal/ai/store"
)

// Heights of the rows the assistant's section adds to the page. The page's own
// rows are settingsRowHeight tall.
const (
	aiProviderRowHeight = float32(58)
	aiFooterRowHeight   = float32(56)
	aiModelRowHeight    = float32(44)
	aiStatusRowHeight   = float32(34)
	aiPagerRowHeight    = float32(40)
	aiAddRowHeight      = float32(58)
)

// aiText is a row of one line of text.
func (page *settingsPage) aiText(height float32, value string, size float32, weight float32, color gfx.Color) {
	f := page.f
	page.rowOf(height).PadX(18).Children(func() {
		f.Text(value).FontSize(size).Weight(weight).Color(color)
	})
}

// aiFrame declares a row's name and explanation, wrapping to the room the control
// leaves, and the control at the right edge. control declares the control; the row
// is declared as the page's own rows are (settingsPage.line), only the height and
// the explanation's color differ.
func (page *settingsPage) aiFrame(height float32, label, description string, descriptionColor gfx.Color, control func()) {
	page.aiFrameFor(gfx.ID{}, height, label, description, descriptionColor, control)
}

// aiFrameFor is aiFrame whose words label the control with that identity (see
// lineFor): pressing them flips the switch that control declares.
func (page *settingsPage) aiFrameFor(labelFor gfx.ID, height float32, label, description string, descriptionColor gfx.Color, control func()) {
	f, pal := page.f, page.state.pal
	page.rowOf(height).PadX(18).Gap(24).Children(func() {
		words := f.Column().Grow(1).Children(func() {
			f.Text(label).FontSize(metrics.FontSizeM).Weight(gfx.WeightMedium).Color(pal.text).LineHeight(21)
			f.Paragraph(description, settingsDescLines).FontSize(metrics.FontSizeS).Color(descriptionColor).LineHeight(15).MarginSides(1, 0, 0, 0)
		})
		page.labels(labelFor, words, label, false)
		control()
	})
}

// segmentSlot declares a segmented choice of the given width.
func (page *settingsPage) segmentSlot(width float32, options []components.SegmentOption, selected gfx.ID) {
	slot := page.f.Slot().Size(width, 34)
	page.segments(slot, options, selected)
}

type aiButtonStyle uint8

const (
	aiButtonOutline aiButtonStyle = iota
	aiButtonPrimary
	aiButtonDanger
)

// aiButtonSize is how large aiButton makes a button with this label.
func (state *State) aiButtonSize(canvas *gfx.Canvas, label string) (width, height float32) {
	button := components.BeginButton(canvas, state.Theme(), gfx.ID{}, gfx.Rect{}).Small().SizeToFit(label)
	return button.BoundsWidth(), button.BoundsHeight()
}

// aiButtonSlot reserves the room of a button for aiButton to paint into. The
// keyboard visits controls in the order they are painted, so the settings page
// records its buttons (settingsPage.button) and paints them after the frame.
func (state *State) aiButtonSlot(f *ui.Frame, label string) ui.Box {
	width, height := state.aiButtonSize(f.Canvas(), label)
	return f.Slot().Size(width, height)
}

// aiButton paints a small button in a slot.
func (state *State) aiButton(canvas *gfx.Canvas, id gfx.ID, slot gfx.Rect, label string, style aiButtonStyle, disabled bool) {
	button := components.BeginButton(canvas, state.Theme(), id, gfx.Rect{X: slot.X, Y: slot.Y, Width: slot.Width}).Small()
	if disabled {
		button = button.Disabled()
	}
	switch style {
	case aiButtonPrimary:
		button.Default(label)
	case aiButtonDanger:
		button.DestructiveOutline(label)
	default:
		button.Outline(label)
	}
}

const (
	aiSwitchWidth  = float32(36)
	aiSwitchHeight = float32(20)
)

// aiSwitchSlot reserves the room of a switch for aiSwitch to paint into.
func aiSwitchSlot(f *ui.Frame) ui.Box { return f.Slot().Size(aiSwitchWidth, aiSwitchHeight) }

// aiSwitch paints a switch in a slot.
func (state *State) aiSwitch(canvas *gfx.Canvas, id gfx.ID, slot gfx.Rect, on, disabled bool, label string) {
	control := components.BeginSwitch(canvas, state.switchTheme(), id, slot, on).Label(label)
	if disabled {
		control = control.Disabled()
	}
	control.Paint()
}

// paintAISettings paints the assistant's category: providers, models and how
// it behaves.
func (state *State) paintAISettings(page *settingsPage) {
	state.paintAISettingsHead(page)
	page.groups(func() {
		state.paintAIProviders(page)
		state.paintAICLIAuth(page)
		state.paintAIModels(page)
		state.paintAIBehaviour(page)
	})
}

// paintMCPSettings paints the external-access category. It shares the
// assistant's stored choices, so it loads them the same way.
func (state *State) paintMCPSettings(page *settingsPage) {
	state.paintAISettingsHead(page)
	page.groups(func() { state.paintAIMCP(page) })
}

// paintAISettingsHead queues the first load of the stored choices and shows why
// the last change could not be stored.
func (state *State) paintAISettingsHead(page *settingsPage) {
	cfg := &state.ai.cfg
	if !state.ai.storeTried && !cfg.loadPosted {
		// A page restored at startup is painted before it was opened; load the
		// choices off the paint path.
		cfg.loadPosted = true
		state.aiPost(func(s *State) { s.aiSettingsOpened() })
	}

	if cfg.notice != "" {
		page.f.Text(cfg.notice).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(state.pal.danger).LineHeight(20).MarginSides(-4, 4, 8, 4)
	}
}

func (state *State) paintAIProviders(page *settingsPage) {
	f, pal, m := page.f, state.pal, state.messages.AISettings
	providers := state.ai.providers
	if state.ai.storeErr != nil {
		page.group(m.ProvidersTitle, func() {
			page.aiText(settingsRowHeight, fmt.Sprintf(m.StoreErrorFmt, state.ai.storeErr.Error()), metrics.FontSizeS, gfx.WeightRegular, pal.danger)
		})
		return
	}
	count := min(len(providers), aiMaxProviderRows)
	selected, hasSelected := state.aiSelectedProvider()
	page.group(m.ProvidersTitle, func() {
		for index := 0; index < count; index++ {
			provider := providers[index]
			isSelected := hasSelected && provider.ID == selected.ID
			background := gfx.Color{}
			if isSelected {
				background = pal.surfaceSoft
			}
			nameColor := pal.text
			if !provider.Enabled {
				nameColor = pal.muted
			}
			page.separator()
			row := f.Column().Height(aiProviderRowHeight)
			page.hitOn(row, hitAISetProviderRow.At(index), provider.Name, gfx.HitOptions{Role: gfx.RoleButton, Selected: isSelected})
			row.Children(func() {
				f.Row().Grow(1).MarginSides(4, 6, 4, 6).PadX(12).Gap(18).Items(ui.Center).Background(background).Radius(radius.control).Children(func() {
					f.Row().Grow(1).Shrink(1).Gap(12).Items(ui.Center).Children(func() {
						f.Icon(aiProviderIcon(provider)).Size(20, 20).Color(nameColor).Shrink(0)
						f.Column().Grow(1).Shrink(1).Children(func() {
							f.Text(provider.Name).FontSize(metrics.FontSizeM).Weight(gfx.WeightMedium).Color(nameColor).LineHeight(21)
							f.Text(state.aiProviderMeta(provider)).FontSize(metrics.FontSizeS).Color(pal.muted).LineHeight(18)
						})
					})
					f.Row().Gap(12).Items(ui.Center).Shrink(0).Children(func() {
						page.switchSlot(hitAISetProviderSwitch.At(index), provider.Enabled, false, m.ProviderEnabled+" "+provider.Name)
						f.Row().Gap(8).Children(func() {
							page.button(hitAISetProviderEdit.At(index), m.ProviderEdit, aiButtonOutline, false)
							page.button(hitAISetProviderDelete.At(index), m.ProviderDelete, aiButtonOutline, false)
						})
					})
				})
			})
		}
		page.rowOf(aiFooterRowHeight).PadX(18).Gap(12).Children(func() {
			if len(providers) == 0 {
				f.Text(m.ProvidersEmpty).FontSize(metrics.FontSizeS).Color(pal.muted)
			}
			f.Spacer()
			page.button(hitAISetProviderImport, m.ProviderImport, aiButtonOutline, false)
			page.button(hitAISetProviderAdd, m.ProviderAdd, aiButtonPrimary, false)
		})
	})
}

// aiProviderMeta is the line under a provider's name: its kind, address and
// whether a key is saved. The key itself never reaches it.
func (state *State) aiProviderMeta(provider store.Provider) string {
	m := state.messages.AISettings
	if store.IsCLIKind(provider.Kind) {
		path := provider.CLIPath
		if path == "" {
			path = m.CLIPathDefault
		}
		return aiPresets[aiPresetOf(provider)].label + "  ·  " + path
	}
	kind := m.ProviderKindOpenAI
	if provider.Kind == store.KindAnthropic {
		kind = m.ProviderKindClaude
	}
	address := provider.BaseURL
	if address == "" {
		address = m.ProviderOfficial
	}
	key := m.ProviderNoKey
	if provider.HasKey {
		key = m.ProviderKeySaved
	}
	if provider.Kind == store.KindAnthropic {
		auth := m.ProviderAuthAPIKey
		if provider.AuthStyle == store.AuthStyleBearer {
			auth = m.ProviderAuthBearer
		}
		return kind + "  ·  " + address + "  ·  " + auth + "  ·  " + key
	}
	if provider.APIFormat == store.APIFormatResponses {
		return kind + "  ·  " + m.ProviderFormatResponses + "  ·  " + address + "  ·  " + key
	}
	return kind + "  ·  " + address + "  ·  " + key
}

// aiModelsStatus is the line shown at the top of the model list, if any.
func (state *State) aiModelsStatus() (string, bool) {
	cfg := &state.ai.cfg
	m := state.messages.AISettings
	switch {
	case cfg.modelErr != "":
		return cfg.modelErr, true
	case cfg.fetched && cfg.fetchErr != "":
		return fmt.Sprintf(m.ModelsFailedFmt, cfg.fetchErr), true
	case cfg.fetched:
		if provider, ok := state.aiSelectedProvider(); ok && store.IsCLIKind(provider.Kind) {
			return m.CLIModelsNote, false
		}
		return fmt.Sprintf(m.ModelsFetchedFmt, cfg.fetchCount), false
	}
	return "", false
}

func (state *State) paintAIModels(page *settingsPage) {
	f, pal, m := page.f, state.pal, state.messages.AISettings
	cfg := &state.ai.cfg
	provider, ok := state.aiSelectedProvider()
	if state.ai.storeErr != nil || !ok {
		page.group(m.ModelsTitle, func() {
			page.aiText(settingsRowHeight, m.ModelsNoProvider, metrics.FontSizeS, gfx.WeightRegular, pal.muted)
		})
		return
	}
	total, enabled := state.aiModelsOf(provider.ID)
	status, failed := state.aiModelsStatus()
	page.group(m.ModelsTitle, func() {
		// Which provider, what it holds, and the probe & fetch buttons.
		fetchLabel := m.ModelsFetch
		if store.IsCLIKind(provider.Kind) {
			fetchLabel = m.CLIModelsCheck
		}
		description := fmt.Sprintf(m.ModelsDescFmt, total, enabled)
		if cfg.fetch != nil {
			fetchLabel, description = m.ModelsCancel, m.ModelsFetching
		} else if cfg.probeCancel != nil && cfg.probingProvider == provider.ID && cfg.probingModel == "" {
			description = fmt.Sprintf(m.ModelsProbingFmt, cfg.probeProgress[0], cfg.probeProgress[1])
		}
		probeLabel := m.ModelsProbeAll
		if cfg.probeCancel != nil && cfg.probingProvider == provider.ID && cfg.probingModel == "" {
			probeLabel = m.ModelsProbeCancel
		}
		// A CLI provider answers through its own session, so it has no endpoint
		// to probe: the buttons are not offered at all.
		probeable := !store.IsCLIKind(provider.Kind)
		page.aiFrame(settingsRowHeight, fmt.Sprintf(m.ModelsOfFmt, provider.Name), description, pal.muted, func() {
			f.Row().Gap(8).Items(ui.Center).Shrink(0).Children(func() {
				if probeable {
					page.button(hitAISetModelProbeAll, probeLabel, aiButtonOutline, total == 0)
				}
				page.button(hitAISetFetchModels, fetchLabel, aiButtonOutline, false)
			})
		})

		if status != "" {
			color := pal.muted
			if failed {
				color = pal.danger
			}
			page.aiText(aiStatusRowHeight, status, metrics.FontSizeS, gfx.WeightRegular, color)
		}
		if total == 0 {
			page.aiText(aiModelRowHeight, m.ModelsEmpty, metrics.FontSizeS, gfx.WeightRegular, pal.muted)
		}
		filteredModels := state.aiFilteredModels(provider.ID)
		filteredTotal := len(filteredModels)
		shown := min(max(filteredTotal-cfg.page*aiModelPageSize, 0), aiModelPageSize)
		if filteredTotal == 0 && total > 0 {
			page.aiText(aiModelRowHeight, m.ModelsEmpty, metrics.FontSizeS, gfx.WeightRegular, pal.muted)
		}
		defaultColWidth, _ := state.aiButtonSize(f.Canvas(), m.ModelUse)
		if defaultColWidth < 76 {
			defaultColWidth = 76
		}
		const probeColWidth = float32(92)
		for row := 0; row < shown; row++ {
			model := filteredModels[cfg.page*aiModelPageSize+row]
			isDefault := state.aiIsDefault(provider.ID, model.Name)
			nameColor := pal.text
			if !model.Enabled {
				nameColor = pal.muted
			}
			res, hasRes := state.aiGetProbeResult(provider.ID, model.Name)
			isProbing := state.aiIsProbing(provider.ID, model.Name)

			page.rowOf(aiModelRowHeight).PadX(18).Gap(10).Children(func() {
				label := model.Name
				if store.IsCLIKind(provider.Kind) && model.Name == "default" {
					label = m.CLIDefaultModel
				}
				// Name and reasoning chip (left-aligned, clickable to configure reasoning effort)
				f.Row().Grow(1).Gap(8).Items(ui.Center).Children(func() {
					f.Text(label).FontSize(metrics.FontSizeM).Color(nameColor).MarginSides(0, 4, 0, 0)
					_, _, isReasoning, isOverride, isDisabled := state.aiModelReasoningInfo(&model)
					var (
						chipText      string
						chipBg        gfx.Color
						chipTextColor gfx.Color
						chipWeight    float32 = gfx.WeightRegular
					)
					if isDisabled {
						chipText = m.ModelReasoningTagOff
						chipBg = pal.surfaceSoft
						chipTextColor = pal.muted
					} else if isOverride {
						chipText = m.ModelReasoningTagManual
						chipBg = pal.accentSoft
						chipTextColor = pal.accent
						chipWeight = gfx.WeightMedium
					} else if isReasoning {
						chipText = m.ModelReasoningTagAuto
						chipBg = pal.surfaceSoft
						chipTextColor = pal.accent
						chipWeight = gfx.WeightMedium
					} else {
						chipText = m.ModelReasoningTagAuto
						chipBg = pal.surfaceSoft
						chipTextColor = pal.muted
					}
					reasoningBox := f.Row().Height(20).PadX(8).Center().Radius(10).Background(chipBg)
					page.hitOn(reasoningBox, hitAISetModelReasoning.At(row), chipText, gfx.HitOptions{Role: gfx.RoleButton})
					reasoningBox.Children(func() {
						f.Text(chipText).FontSize(metrics.FontSizeS).Color(chipTextColor).Weight(chipWeight)
					})
				})

				// Probe badge or button (fixed-width column for neat vertical alignment)
				if !probeable {
					f.Row().Width(probeColWidth).Shrink(0)
				} else if isProbing {
					f.Row().Width(probeColWidth).Height(24).Center().Background(pal.surfaceSoft).Radius(12).Shrink(0).Children(func() {
						f.Text(m.ModelProbing).FontSize(metrics.FontSizeS).Color(pal.muted)
					})
				} else if hasRes {
					switch res.Status {
					case probe.StatusSuccess:
						ms := res.Latency.Milliseconds()
						badgeText := fmt.Sprintf("%dms", ms)
						textColor := pal.success
						if ms >= 800 && ms < 2500 {
							textColor = pal.warning
						} else if ms >= 2500 {
							textColor = pal.danger
						}
						box := f.Row().Width(probeColWidth).Height(24).Center().Background(pal.surfaceSoft).Radius(12).Shrink(0)
						page.hitOn(box, hitAISetModelProbe.At(row), badgeText, gfx.HitOptions{Role: gfx.RoleButton})
						box.Children(func() {
							f.Text("✓ " + badgeText).FontSize(metrics.FontSizeS).Color(textColor).Weight(gfx.WeightMedium)
						})
					case probe.StatusFailed:
						box := f.Row().Width(probeColWidth).Height(24).Center().Background(pal.surfaceSoft).Radius(12).Shrink(0)
						page.hitOn(box, hitAISetModelProbe.At(row), m.ModelProbeFailed, gfx.HitOptions{Role: gfx.RoleButton})
						box.Children(func() {
							f.Text("✗ " + m.ModelProbeFailed).FontSize(metrics.FontSizeS).Color(pal.danger).Weight(gfx.WeightMedium)
						})
					case probe.StatusSkipped:
						f.Row().Width(probeColWidth).Height(24).Center().Background(pal.surfaceSoft).Radius(12).Shrink(0).Children(func() {
							f.Text(m.ModelProbeSkipped).FontSize(metrics.FontSizeS).Color(pal.muted)
						})
					default:
						f.Row().Width(probeColWidth).Center().Shrink(0).Children(func() {
							page.button(hitAISetModelProbe.At(row), m.ModelProbe, aiButtonOutline, false)
						})
					}
				} else {
					f.Row().Width(probeColWidth).Center().Shrink(0).Children(func() {
						page.button(hitAISetModelProbe.At(row), m.ModelProbe, aiButtonOutline, false)
					})
				}

				// Default status / button (fixed-width column to prevent probe column shift)
				f.Row().Width(defaultColWidth).Center().Shrink(0).Children(func() {
					if isDefault {
						f.Row().Height(20).PadX(8).Center().Background(pal.accentSoft).Radius(10).Children(func() {
							f.Text(m.ModelDefault).FontSize(metrics.FontSizeS).Weight(gfx.WeightSemibold).Color(pal.accent)
						})
					} else {
						page.button(hitAISetModelUse.At(row), m.ModelUse, aiButtonOutline, false)
					}
				})
				page.switchSlot(hitAISetModelSwitch.At(row), model.Enabled, isDefault, m.ModelEnabled+" "+model.Name)
			})
		}
		if filteredTotal > aiModelPageSize {
			first := cfg.page*aiModelPageSize + 1
			page.rowOf(aiPagerRowHeight).PadX(18).Gap(8).Children(func() {
				f.Text(fmt.Sprintf(m.ModelPageFmt, first, first+shown-1, filteredTotal)).FontSize(metrics.FontSizeS).Color(pal.muted)
				f.Spacer()
				page.button(hitAISetModelPrev, m.ModelPrev, aiButtonOutline, cfg.page == 0)
				page.button(hitAISetModelNext, m.ModelNext, aiButtonOutline, first+shown-1 >= filteredTotal)
			})
		}

		// A model can be added by name, for providers that do not list theirs.
		page.rowOf(aiAddRowHeight).PadX(18).Gap(10).Children(func() {
			page.add(settingsControl{kind: controlModelInput, slot: f.Slot().Grow(1).MinWidth(40).Height(32)})
			page.button(hitAISetModelAdd, m.ModelAdd, aiButtonOutline, false)
		})
	})
}

// paintAIModelInput paints the field a model is added by name in.
func (state *State) paintAIModelInput(canvas *gfx.Canvas, bounds gfx.Rect) {
	m := state.messages.AISettings
	value := state.ai.cfg.modelInput.String()
	focused := canvas.Focused() == hitAISetModelInput
	components.PaintInput(canvas, state.Theme(), bounds, components.InputSnapshot{
		ID: hitAISetModelInput, Label: m.ModelAddPH, Value: value, Placeholder: m.ModelAddPH,
		Focused: focused, PlatformEditing: focused && value != "",
	})
}

func (state *State) paintAIBehaviour(page *settingsPage) {
	pal, m := state.pal, state.messages.AISettings
	settings := &state.ai.settings
	choice := min(300, max(float32(190), page.width*0.40))

	selectedRisk := hitAISetRiskLow
	switch settings.autoUpTo {
	case risk.Medium:
		selectedRisk = hitAISetRiskMedium
	case risk.High:
		selectedRisk = hitAISetRiskHigh
	}
	selectedSteps := hitAISetSteps12
	switch settings.maxSteps {
	case 6:
		selectedSteps = hitAISetSteps6
	case 24:
		selectedSteps = hitAISetSteps24
	}
	selectedContext := hitAISetContext60
	switch settings.contextLines {
	case 0:
		selectedContext = hitAISetContext0
	case 30:
		selectedContext = hitAISetContext30
	case 120:
		selectedContext = hitAISetContext120
	}
	description, color := m.ClearHistoryDesc, pal.muted
	switch {
	case state.ai.run != nil:
		description = m.ClearHistoryBusy
	case state.ai.cfg.clearedDone:
		description, color = m.ClearHistoryDone, pal.success
	}
	riskOptions, stepOptions, contextOptions := state.aiRiskOptions(), state.aiStepsOptions(), state.aiContextOptions()
	selectedText := hitAISetText4
	switch settings.maxTextBytes {
	case 1 << 20:
		selectedText = hitAISetText1
	case 8 << 20:
		selectedText = hitAISetText8
	}
	textOptions := state.aiTextLimitOptions()
	page.group(m.BehaviourTitle, func() {
		page.aiFrame(settingsRowHeight, m.AutoRun, m.AutoRunDesc, pal.muted, func() { page.segmentSlot(choice, riskOptions[:], selectedRisk) })
		page.aiFrame(settingsRowHeight, m.MaxSteps, m.MaxStepsDesc, pal.muted, func() { page.segmentSlot(choice, stepOptions[:], selectedSteps) })
		page.aiFrame(settingsRowHeight, m.ContextLines, m.ContextLinesDesc, pal.muted, func() { page.segmentSlot(choice, contextOptions[:], selectedContext) })
		page.aiFrame(settingsRowHeight, m.AttachmentTextLimit, m.AttachmentTextLimitDesc, pal.muted, func() { page.segmentSlot(choice, textOptions[:], selectedText) })
		page.aiFrameFor(hitAISetRedact, settingsRowHeight, m.Redact, m.RedactDesc, pal.muted, func() { page.switchSlot(hitAISetRedact, settings.redact, false, m.Redact) })
		page.aiFrameFor(hitAISetHistory, settingsRowHeight, m.History, m.HistoryDesc, pal.muted, func() { page.switchSlot(hitAISetHistory, settings.keepHistory, false, m.History) })
		page.aiFrame(settingsRowHeight, m.ClearHistory, description, color, func() {
			page.button(hitAISetClearHistory, m.ClearHistory, aiButtonDanger, state.ai.run != nil)
		})
	})
}

func (state *State) paintAIMCP(page *settingsPage) {
	pal, m := state.pal, state.messages.AISettings
	settings := &state.ai.settings

	selected := hitAISetMCPObserve
	switch settings.mcpMode {
	case gate.ModeAsk:
		selected = hitAISetMCPAsk
	case gate.ModeAllowAll:
		selected = hitAISetMCPAllowAll
	}
	modes := state.aiMCPModeOptions()
	endpoint := m.MCPOff
	if settings.mcpEnabled {
		endpoint = m.MCPNotRunning
		if url := state.aiMCPURL(); url != "" {
			endpoint = url
		}
	}
	ready := state.aiMCPReady()
	description, color := m.MCPTokenDesc, pal.muted
	if state.ai.cfg.tokenReset {
		description, color = m.MCPResetDone, pal.success
	}
	page.group(m.MCPHTTPTitle, func() {
		page.f.Column().Pad(18).Children(func() {
			page.f.Wrap(m.MCPHTTPDescription).FontSize(metrics.FontSizeS).LineHeight(18).Color(pal.muted)
		})
		page.aiFrameFor(hitAISetMCPEnabled, settingsRowHeight, m.MCPEnable, m.MCPEnableDesc, pal.muted, func() {
			page.switchSlot(hitAISetMCPEnabled, settings.mcpEnabled, false, m.MCPEnable)
		})
		page.mcpFrame(m.MCPMode, m.MCPModeDesc, pal.muted, func() {
			width := min(float32(300), page.width-36)
			if page.width >= 500 {
				width = min(width, page.width*.44)
			}
			page.segmentSlot(width, modes[:], selected)
		})
		if settings.mcpMode == gate.ModeAllowAll {
			page.mcpAccessWarning(m.MCPAllowAllWarning)
		}
		page.mcpFrame(m.MCPEndpoint, endpoint, pal.muted, func() {
			page.mcpCopyButton(hitAISetMCPCopyURL, m.MCPCopyURL, !ready)
		})
		page.mcpFrame(m.MCPToken, description, color, func() {
			// The two token actions do not fit side by side in a narrow page.
			if page.width < 500 {
				page.f.Column().Gap(8).Children(func() {
					page.mcpCopyButton(hitAISetMCPCopyToken, m.MCPCopyToken, !ready)
					page.button(hitAISetMCPReset, m.MCPReset, aiButtonOutline, false)
				})
				return
			}
			page.f.Row().Gap(8).Children(func() {
				page.mcpCopyButton(hitAISetMCPCopyToken, m.MCPCopyToken, !ready)
				page.button(hitAISetMCPReset, m.MCPReset, aiButtonOutline, false)
			})
		})
		state.paintAIMCPConfig(page, endpoint, ready)
	})
	state.paintAIAgents(page)
}

func (page *settingsPage) mcpAccessWarning(message string) {
	page.f.Column().PadSides(0, 18, 12, 18).Children(func() {
		page.f.Wrap(message).FontSize(metrics.FontSizeS).LineHeight(18).Color(page.state.pal.danger)
	})
}

// paintAIDialogs paints the section's open dialog above everything else.
func (state *State) paintAIDialogs(canvas *gfx.Canvas, viewport gfx.Rect) {
	if !state.aiSettingsVisible() {
		return
	}
	switch state.ai.cfg.dialog {
	case aiDialogCLILogin:
		state.paintAICLILogin(canvas, viewport)
	case aiDialogProvider:
		state.paintAIProviderForm(canvas, viewport)
	case aiDialogDelete:
		provider, _ := state.aiProviderByID(state.ai.cfg.deleteID)
		state.paintAIConfirm(canvas, viewport, state.messages.AISettings.DeleteTitle,
			fmt.Sprintf(state.messages.AISettings.DeleteBodyFmt, provider.Name), state.messages.AISettings.DeleteConfirm)
	case aiDialogClear:
		m := state.messages.AISettings
		state.paintAIConfirm(canvas, viewport, m.ClearTitle, m.ClearBody, m.ClearConfirm)
	case aiDialogMCPAllowAll:
		m := state.messages.AISettings
		body := m.MCPAllowAllBody
		if state.ai.cfg.notice != "" {
			body += "\n\n" + state.ai.cfg.notice
		}
		state.paintAIConfirm(canvas, viewport, m.MCPAllowAllTitle, body, m.MCPAllowAllConfirm)
	case aiDialogAgentAllowAll:
		m := state.messages.AISettings
		body := m.AgentAllowAllBody
		if state.ai.cfg.notice != "" {
			body += "\n\n" + state.ai.cfg.notice
		}
		state.paintAIConfirm(canvas, viewport, m.MCPAllowAllTitle, body, m.MCPAllowAllConfirm)
	case aiDialogImport:
		state.paintAIImport(canvas, viewport)
	case aiDialogProbe:
		state.paintAIProbeDialog(canvas, viewport)
	case aiDialogModelReasoning:
		state.paintAIModelReasoningDialog(canvas, viewport)
	}
}

func (state *State) paintAIConfirm(canvas *gfx.Canvas, viewport gfx.Rect, title, body, action string) {
	dialog := components.BeginAlertDialog(canvas, state.Theme(), viewport, components.AlertDialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		ActionID: hitAISetConfirmAction, CancelID: hitAISetConfirmCancel,
		ContentSize: gfx.Size{Width: 480}, ActionVariant: components.ButtonDestructive,
		InitialFocusID: hitAISetConfirmCancel,
	})
	dialog.Begin(title, body, action, state.messages.Cancel)
	dialog.End()
}

func (state *State) paintAIProbeDialog(canvas *gfx.Canvas, viewport gfx.Rect) {
	m, theme := state.messages.AISettings, state.Theme()
	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		CloseID: hitAISetFormClose, InitialFocusID: hitAISetProbeStart,
		ContentSize: gfx.Size{Width: 460}, FitBody: true, BodyHeight: 140,
	})
	layout := dialog.Begin(m.ProbeDialogTitle, m.ProbeDialogDesc)

	var concSlot, timeoutSlot ui.Box
	f := state.settingsRow.Begin(canvas, theme, layout.Body)
	f.Column().Fill().PadSides(2, 4, 0, 4).Gap(10).Children(func() {
		concSlot = f.Slot().Height(60)
		timeoutSlot = f.Slot().Height(60)
	})
	f.End()

	concContent := components.BeginField(canvas, theme, concSlot.Rect()).Paint(m.ProbeConcurrency, "", "")
	concOptions := state.aiProbeConcOptions()
	selectedConc := hitAISetProbeConc4
	switch state.aiProbeConcurrency() {
	case 1:
		selectedConc = hitAISetProbeConc1
	case 2:
		selectedConc = hitAISetProbeConc2
	case 8:
		selectedConc = hitAISetProbeConc8
	}
	components.BeginSegmentedControl(canvas, theme, concContent, concOptions[:], selectedConc).Paint()

	timeoutContent := components.BeginField(canvas, theme, timeoutSlot.Rect()).Paint(m.ProbeTimeout, "", "")
	timeoutOptions := state.aiProbeTimeoutOptions()
	selectedTimeout := hitAISetProbeTimeout15
	switch state.aiProbeTimeout() {
	case 5 * time.Second:
		selectedTimeout = hitAISetProbeTimeout5
	case 10 * time.Second:
		selectedTimeout = hitAISetProbeTimeout10
	case 30 * time.Second:
		selectedTimeout = hitAISetProbeTimeout30
	}
	components.BeginSegmentedControl(canvas, theme, timeoutContent, timeoutOptions[:], selectedTimeout).Paint()

	dialog.Actions(layout,
		components.ButtonAction{ID: hitAISetFormCancel, Label: state.messages.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitAISetProbeStart, Label: m.ProbeStartAction},
	)
	dialog.End()
}

func (state *State) paintAIModelReasoningDialog(canvas *gfx.Canvas, viewport gfx.Rect) {
	m, theme := state.messages.AISettings, state.Theme()
	cfg := &state.ai.cfg

	modeHeight := float32(84)
	presetHeight := float32(0)
	if cfg.reasoningMode == 1 {
		presetHeight = 84
	}
	bodyHeight := modeHeight + 4
	if presetHeight > 0 {
		bodyHeight += presetHeight + 10
	}

	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		CloseID: hitAISetFormClose, InitialFocusID: hitAISetReasoningSave,
		ContentSize: gfx.Size{Width: 520}, FitBody: true, BodyHeight: bodyHeight,
	})
	desc := fmt.Sprintf("%s (%s)", m.ModelReasoningDesc, cfg.reasoningModelName)
	layout := dialog.Begin(m.ModelReasoningTitle, desc)

	var modeSlot, presetSlot ui.Box
	f := state.settingsRow.Begin(canvas, theme, layout.Body)
	f.Column().Fill().PadSides(2, 4, 0, 4).Gap(10).Children(func() {
		modeSlot = f.Slot().Height(modeHeight)
		if presetHeight > 0 {
			presetSlot = f.Slot().Height(presetHeight)
		}
	})
	f.End()

	// 1. 模式选择 (Auto / Enable / Disable)
	modeDesc := m.ModelReasoningAutoDesc
	selectedMode := hitAISetReasoningAuto
	switch cfg.reasoningMode {
	case 1:
		selectedMode = hitAISetReasoningEnable
		modeDesc = m.ModelReasoningEnableDesc
	case 2:
		selectedMode = hitAISetReasoningDisable
		modeDesc = m.ModelReasoningDisableDesc
	}
	modeContent := components.BeginField(canvas, theme, modeSlot.Rect()).Paint(m.ModelReasoningMode, modeDesc, "")
	modeOptions := state.aiReasoningModeOptions()
	components.BeginSegmentedControl(canvas, theme, modeContent, modeOptions[:], selectedMode).Paint()

	// 2. 如果是手动开启模式，选择档位预设
	if presetHeight > 0 {
		presetDesc := m.ModelReasoningPresetDesc0
		selectedPreset := hitAISetReasoningPreset0
		switch cfg.reasoningPreset {
		case 1:
			selectedPreset = hitAISetReasoningPreset1
			presetDesc = m.ModelReasoningPresetDesc1
		case 2:
			selectedPreset = hitAISetReasoningPreset2
			presetDesc = m.ModelReasoningPresetDesc2
		}
		presetContent := components.BeginField(canvas, theme, presetSlot.Rect()).Paint(m.ModelReasoningPreset, presetDesc, "")
		presetOptions := state.aiReasoningPresetOptions()
		components.BeginSegmentedControl(canvas, theme, presetContent, presetOptions[:], selectedPreset).Paint()
	}

	dialog.Actions(layout,
		components.ButtonAction{ID: hitAISetReasoningCancel, Label: state.messages.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitAISetReasoningSave, Label: m.ModelReasoningSave},
	)
	dialog.End()
}

func (state *State) paintAIProviderForm(canvas *gfx.Canvas, viewport gfx.Rect) {
	if store.IsCLIKind(state.ai.cfg.kind) {
		state.paintAICLIProviderForm(canvas, viewport)
		return
	}
	m, theme := state.messages.AISettings, state.Theme()
	cfg := &state.ai.cfg
	title, submit := m.FormAddTitle, m.FormAdd
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
	// No scrim dismiss: a press beside the form must not discard its input.
	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		CloseID: hitAISetFormClose, InitialFocusID: hitAISetFormName,
		ContentSize: gfx.Size{Width: 500}, FitBody: true, BodyHeight: withError(2+presetHeight+4*60+authHeight+formatHeight+keyHeight+4, cfg.formErr), // the fields, key last
	})
	layout := dialog.Begin(title, m.FormDescription)

	hint := ""
	if cfg.hadKey {
		hint = m.FormKeySaved
	}
	selected := hitAISetFormKindOpenAI
	placeholder := m.FormBaseURLOpenAI
	if cfg.kind == store.KindAnthropic {
		selected, placeholder = hitAISetFormKindClaude, m.FormBaseURLClaude
	}
	var preset, name, kind, auth, format, url, modelsURL, key ui.Box
	f := state.settingsRow.Begin(canvas, theme, layout.Body)
	f.Column().Fill().PadSides(2, 4, 0, 4).Children(func() {
		if presetHeight > 0 {
			preset = f.Slot().Height(presetHeight)
		}
		name = f.Slot().Height(60)
		kind = f.Slot().Height(60)
		if authHeight > 0 {
			auth = f.Slot().Height(authHeight)
		}
		url = f.Slot().Height(60)
		modelsURL = f.Slot().Height(60)
		if formatHeight > 0 {
			format = f.Slot().Height(formatHeight)
		}
		key = f.Slot().Height(keyHeight).MarginSides(0, 0, 4, 0)
		if cfg.formErr != "" {
			f.Text(cfg.formErr).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(state.pal.danger).LineHeight(18)
		}
	})
	f.End()

	if presetHeight > 0 {
		state.paintAIPresetField(canvas, preset.Rect())
	}
	state.aiFormField(canvas, hitAISetFormName, m.FormName, m.FormNamePH, "", false, &cfg.name, name.Rect())
	content := components.BeginField(canvas, theme, kind.Rect()).Paint(m.FormKind, "", "")
	kinds := state.aiKindOptions()
	components.BeginSegmentedControl(canvas, theme, content, kinds[:], selected).Paint()
	if authHeight > 0 {
		state.paintAIAuthField(canvas, auth.Rect())
	}
	state.aiFormField(canvas, hitAISetFormURL, m.FormBaseURL, placeholder, "", false, &cfg.baseURL, url.Rect())
	state.aiFormField(canvas, hitAISetFormModelsURL, m.FormModelsURL, m.FormModelsURLPH, "", false, &cfg.modelsURL, modelsURL.Rect())
	if formatHeight > 0 {
		state.paintAIFormatField(canvas, format.Rect())
	}
	state.aiFormField(canvas, hitAISetFormKey, m.FormKey, m.FormKeyPH, hint, true, &cfg.apiKey, key.Rect())
	dialog.Actions(layout,
		components.ButtonAction{ID: hitAISetFormCancel, Label: state.messages.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitAISetFormSubmit, Label: submit},
	)
	dialog.End()
}

// paintAIAuthField paints the Anthropic credential-header choice. The style is
// not a secret; the key itself stays in the secure field below.
func (state *State) paintAIAuthField(canvas *gfx.Canvas, bounds gfx.Rect) {
	cfg, m, theme := &state.ai.cfg, state.messages.AISettings, state.Theme()
	content := components.BeginField(canvas, theme, bounds).Paint(m.FormAuth, "", "")
	selected := hitAISetFormAuthAPIKey
	if cfg.authStyle == store.AuthStyleBearer {
		selected = hitAISetFormAuthBearer
	}
	options := state.aiAuthOptions()
	components.BeginSegmentedControl(canvas, theme, content, options[:], selected).Paint()
}

// paintAIFormatField paints the OpenAI-compatible protocol choice, with the
// route each protocol is sent to underneath: the address typed above is only
// the base, and which route follows is not guessable from it.
func (state *State) paintAIFormatField(canvas *gfx.Canvas, bounds gfx.Rect) {
	cfg, m, theme := &state.ai.cfg, state.messages.AISettings, state.Theme()
	route := m.FormFormatChatRoute
	selected := hitAISetFormFormatChat
	if cfg.apiFormat == store.APIFormatResponses {
		route, selected = m.FormFormatResponsesRoute, hitAISetFormFormatResp
	}
	content := components.BeginField(canvas, theme, bounds).Paint(m.FormFormat, route, "")
	options := state.aiFormatOptions()
	components.BeginSegmentedControl(canvas, theme, content, options[:], selected).Paint()
}

// aiFormField paints one labelled input of the provider dialog. A secure field
// shows bullets in place of its text.
func (state *State) aiFormField(canvas *gfx.Canvas, id gfx.ID, title, placeholder, hint string, secure bool, editor interface{ String() string }, bounds gfx.Rect) {
	theme := state.Theme()
	content := components.BeginField(canvas, theme, bounds).Paint(title, hint, "")
	value := editor.String()
	focused := canvas.Focused() == id
	state.paintSecretInput(canvas, content, components.InputSnapshot{
		ID: id, Label: title, Value: value, Placeholder: placeholder, Secure: secure,
		Focused: focused, PlatformEditing: focused && value != "",
	})
}

// paintAIPresetField paints the list of providers a new provider can start from.
func (state *State) paintAIPresetField(canvas *gfx.Canvas, bounds gfx.Rect) {
	cfg, m, theme := &state.ai.cfg, state.messages.AISettings, state.Theme()
	content := components.BeginField(canvas, theme, bounds).Paint(m.FormPreset, "", "")
	selectControl := components.BeginSelect(canvas, theme, content, components.SelectOptions{
		TriggerID: hitAISetFormPreset, Label: m.FormPreset, Value: state.aiPresetLabel(cfg.preset), Icon: aiPresets[cfg.preset].icon, Open: cfg.presetOpen,
		Content: gfx.Rect{X: content.X, Y: content.Y + content.Height + 4, Width: content.Width, Height: float32(8 + len(aiPresets)*32)},
		Layer:   dialogLayer + 1, Scope: dialogScope, DismissID: hitAISetFormPresetDismiss, Viewport: state.windowRect(),
		Scroll: state.selectScroll(cfg.presetOpen, hitAISetFormPresetDismiss),
	})
	defer selectControl.End()
	if cfg.presetOpen {
		state.menuPlaced(selectControl.ContentBounds())
	}
	for index := range aiPresets {
		selectControl.AddItem(components.SelectItem{
			ID: hitAISetFormPresetItems.At(index), Label: state.aiPresetLabel(index), Icon: aiPresets[index].icon, Selected: index == cfg.preset,
		})
	}
}
