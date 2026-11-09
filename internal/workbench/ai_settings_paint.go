package workbench

import (
	"fmt"
	"strconv"
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

// The roots name the controls of the assistant's dialogs in the window's Tree.
// A dialog's choices are declared in a Page of its own, so a click and the arrow
// keys reach the choice the way they reach one on the settings page.
const (
	aiProbeBodyRoot     = "ai-probe-body"
	aiReasoningBodyRoot = "ai-reasoning-body"
)

// The values the assistant's choices offer. A Choice writes one of these itself;
// the changed callback stores it.
var (
	aiRiskChoices         = [...]risk.Level{risk.Low, risk.Medium, risk.High}
	aiStepsChoices        = [...]int{6, 12, 24}
	aiContextChoices      = [...]int{0, 30, 60, 120}
	aiTextLimitChoices    = [...]int{1, 4, 8} // MiB, as stored in ai.attachment_text_mib
	aiMCPModeChoices      = [...]gate.Mode{gate.ModeObserve, gate.ModeAsk, gate.ModeAllowAll}
	aiAgentModeChoices    = [...]gate.Mode{gate.ModeObserve, gate.ModeAsk, gate.ModeAllowAll}
	aiProbeConcChoices    = [...]int{1, 2, 4, 8}
	aiProbeTimeoutChoices = [...]time.Duration{
		5 * time.Second, 10 * time.Second, 15 * time.Second, 30 * time.Second,
	}
	aiReasoningModeChoices   = [...]int{0, 1, 2} // auto, enabled, disabled
	aiReasoningPresetChoices = [...]int{0, 1, 2} // 3, 6, 2 tiers
	aiFormKindChoices        = [...]string{store.KindOpenAI, store.KindAnthropic}
	aiFormAuthChoices        = [...]string{store.AuthStyleAPIKey, store.AuthStyleBearer}
	aiFormFormatChoices      = [...]string{"", store.APIFormatResponses}
	aiModelFilterChoices     = [...]aiModelFilter{
		aiModelFilterAll,
		aiModelFilterAvailable,
		aiModelFilterFailed,
		aiModelFilterEnabled,
	}
	aiModelSortChoices = [...]aiModelSort{
		aiModelSortDefault,
		aiModelSortLatencyAsc,
		aiModelSortLatencyDesc,
		aiModelSortNameAsc,
	}
)

func (state *State) aiRiskLabel(level risk.Level) string {
	switch level {
	case risk.Medium:
		return state.messages.AISettings.RiskMedium
	case risk.High:
		return state.messages.AISettings.RiskHigh
	}
	return state.messages.AISettings.RiskLow
}

// aiRiskSetting is the value of ai.auto_up_to for level.
func aiRiskSetting(level risk.Level) string {
	switch level {
	case risk.Medium:
		return "medium"
	case risk.High:
		return "high"
	}
	return "low"
}

func (state *State) aiContextLabel(lines int) string {
	if lines == 0 {
		return state.messages.AISettings.ContextOff
	}
	return strconv.Itoa(lines)
}

func aiTextLimitLabel(mib int) string { return strconv.Itoa(mib) + " MiB" }

func (state *State) aiMCPModeLabel(mode gate.Mode) string {
	switch mode {
	case gate.ModeAsk:
		return state.messages.AISettings.MCPModeAsk
	case gate.ModeAllowAll:
		return state.messages.AISettings.MCPModeAllowAll
	}
	return state.messages.AISettings.MCPModeObserve
}

func (state *State) aiAgentModeLabel(mode gate.Mode) string {
	switch mode {
	case gate.ModeAsk:
		return state.messages.AISettings.AgentModeRun
	case gate.ModeAllowAll:
		return state.messages.AISettings.MCPModeAllowAll
	}
	return state.messages.AISettings.AgentModeObserve
}

func aiProbeConcurrencyLabel(value int) string { return strconv.Itoa(value) }

func aiProbeTimeoutLabel(value time.Duration) string {
	return strconv.Itoa(int(value/time.Second)) + "s"
}

func (state *State) aiReasoningModeLabel(mode int) string {
	switch mode {
	case 1:
		return state.messages.AISettings.ModelReasoningEnable
	case 2:
		return state.messages.AISettings.ModelReasoningDisable
	}
	return state.messages.AISettings.ModelReasoningAuto
}

func (state *State) aiReasoningModeDescription(mode int) string {
	switch mode {
	case 1:
		return state.messages.AISettings.ModelReasoningEnableDesc
	case 2:
		return state.messages.AISettings.ModelReasoningDisableDesc
	}
	return state.messages.AISettings.ModelReasoningAutoDesc
}

func (state *State) aiReasoningPresetLabel(preset int) string {
	switch preset {
	case 1:
		return state.messages.AISettings.ModelReasoningPreset6
	case 2:
		return state.messages.AISettings.ModelReasoningPreset2
	}
	return state.messages.AISettings.ModelReasoningPreset3
}

func (state *State) aiReasoningPresetDescription(preset int) string {
	switch preset {
	case 1:
		return state.messages.AISettings.ModelReasoningPresetDesc1
	case 2:
		return state.messages.AISettings.ModelReasoningPresetDesc2
	}
	return state.messages.AISettings.ModelReasoningPresetDesc0
}

func (state *State) aiProviderKindLabel(kind string) string {
	if kind == store.KindAnthropic {
		return state.messages.AISettings.ProviderKindClaude
	}
	return state.messages.AISettings.ProviderKindOpenAI
}

func (state *State) aiAuthStyleLabel(style string) string {
	if style == store.AuthStyleBearer {
		return state.messages.AISettings.FormAuthBearer
	}
	return state.messages.AISettings.FormAuthAPIKey
}

func (state *State) aiFormatLabel(format string) string {
	if format == store.APIFormatResponses {
		return state.messages.AISettings.FormFormatResponses
	}
	return state.messages.AISettings.FormFormatChat
}

// applyAIMCPMode stores the shared HTTP access level the choice already wrote.
// Full access is asked for, not written: the confirmation stores it.
func (state *State) applyAIMCPMode(mode gate.Mode) {
	switch mode {
	case gate.ModeAllowAll:
		if state.ai.settings.mcpMode != gate.ModeAllowAll {
			state.ai.cfg.dialog = aiDialogMCPAllowAll
		}
	case gate.ModeAsk:
		state.aiPut("ai.mcp.mode", "ask")
	default:
		state.aiPut("ai.mcp.mode", "observe")
	}
}

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
		} else {
			totalCnt, availCnt, failCnt, _ := state.aiModelCounts(provider.ID)
			filterName := func(filter aiModelFilter) string {
				switch filter {
				case aiModelFilterAvailable:
					return fmt.Sprintf("%s (%d)", m.ModelFilterAvailable, availCnt)
				case aiModelFilterFailed:
					return fmt.Sprintf("%s (%d)", m.ModelFilterFailed, failCnt)
				case aiModelFilterEnabled:
					return m.ModelFilterEnabled
				default:
					return fmt.Sprintf("%s (%d)", m.ModelFilterAll, totalCnt)
				}
			}
			sortName := func(s aiModelSort) string {
				switch s {
				case aiModelSortLatencyAsc:
					return m.ModelSortLatencyAsc
				case aiModelSortLatencyDesc:
					return m.ModelSortLatencyDesc
				case aiModelSortNameAsc:
					return m.ModelSortNameAsc
				default:
					return m.ModelSortDefault
				}
			}

			filterWidth := float32(260)
			sortWidth := float32(220)
			page.rowOf(46).PadX(18).Gap(10).Children(func() {
				f.Row().Grow(1).Gap(12).Items(ui.Center).Children(func() {
					choice := ui.Choice(page.c, &cfg.modelFilter, aiModelFilterChoices[:], filterName).Width(filterWidth)
					if choice.Changed() {
						cfg.page = 0
						state.aiSettingsChanged()
					}
					f.Spacer()
					choice = ui.Choice(page.c, &cfg.modelSort, aiModelSortChoices[:], sortName).Width(sortWidth)
					if choice.Changed() {
						cfg.page = 0
						state.aiSettingsChanged()
					}
				})
			})
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
			nameWeight := gfx.WeightRegular
			if isDefault {
				nameWeight = gfx.WeightMedium
			} else if !model.Enabled {
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
					f.Text(label).FontSize(metrics.FontSizeM).Weight(nameWeight).Color(nameColor).MarginSides(0, 4, 0, 0)
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

	description, color := m.ClearHistoryDesc, pal.muted
	switch {
	case state.ai.run != nil:
		description = m.ClearHistoryBusy
	case state.ai.cfg.clearedDone:
		description, color = m.ClearHistoryDone, pal.success
	}
	page.group(m.BehaviourTitle, func() {
		risk := settings.autoUpTo
		settingsChoiceRow(page, m.AutoRun, m.AutoRunDesc, &risk, aiRiskChoices[:], state.aiRiskLabel,
			func() { state.aiPut("ai.auto_up_to", aiRiskSetting(risk)) }, nil)
		steps := settings.maxSteps
		settingsChoiceRow(page, m.MaxSteps, m.MaxStepsDesc, &steps, aiStepsChoices[:], strconv.Itoa,
			func() { state.aiPut("ai.max_steps", strconv.Itoa(steps)) }, nil)
		lines := settings.contextLines
		settingsChoiceRow(page, m.ContextLines, m.ContextLinesDesc, &lines, aiContextChoices[:], state.aiContextLabel,
			func() { state.aiPut("ai.context_lines", strconv.Itoa(lines)) }, nil)
		text := settings.maxTextBytes >> 20
		settingsChoiceRow(page, m.AttachmentTextLimit, m.AttachmentTextLimitDesc, &text, aiTextLimitChoices[:], aiTextLimitLabel,
			func() { state.aiPut("ai.attachment_text_mib", strconv.Itoa(text)) }, nil)
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
		mode := settings.mcpMode
		settingsChoiceRow(page, m.MCPMode, m.MCPModeDesc, &mode, aiMCPModeChoices[:], state.aiMCPModeLabel,
			func() { state.applyAIMCPMode(mode) }, nil)
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
	cfg := &state.ai.cfg
	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		CloseID: hitAISetFormClose, InitialFocusID: hitAISetProbeStart,
		ContentSize: gfx.Size{Width: 460}, FitBody: true, BodyHeight: 140,
	})
	layout := dialog.Begin(m.ProbeDialogTitle, m.ProbeDialogDesc)
	if cfg.probePage == nil {
		cfg.probePage = ui.NewPage(ui.PageConfig{Root: aiProbeBodyRoot, Theme: &state.theme})
	}
	cfg.probePage.Draw(canvas, layout.Body, state.declareAIProbeBody)
	dialog.Actions(layout,
		components.ButtonAction{ID: hitAISetFormCancel, Label: state.messages.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitAISetProbeStart, Label: m.ProbeStartAction},
	)
	dialog.End()
}

// declareAIProbeBody declares the probe dialog's two choices. The concurrency
// and the timeout are the effective values, so the defaults (4 and 15s) show as
// chosen before anything was stored.
func (state *State) declareAIProbeBody(c ui.C) {
	pal, m, cfg := state.pal, state.messages.AISettings, &state.ai.cfg
	ui.Column(c).Fill().PadSides(2, 4, 0, 4).Gap(10).Children(func() {
		ui.Text(c, m.ProbeConcurrency).FontSize(metrics.FontSizeM).Weight(gfx.WeightMedium).Color(pal.text)
		concurrency := state.aiProbeConcurrency()
		ui.Row(c).FillWidth().Children(func() {
			if ui.Choice(c, &concurrency, aiProbeConcChoices[:], aiProbeConcurrencyLabel).Fill().Changed() {
				cfg.probeConcurrency = concurrency
			}
		})
		ui.Text(c, m.ProbeTimeout).FontSize(metrics.FontSizeM).Weight(gfx.WeightMedium).Color(pal.text).MarginSides(0, 0, 6, 0)
		timeout := state.aiProbeTimeout()
		ui.Row(c).FillWidth().Children(func() {
			if ui.Choice(c, &timeout, aiProbeTimeoutChoices[:], aiProbeTimeoutLabel).Fill().Changed() {
				cfg.probeTimeout = timeout
			}
		})
	})
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
	if cfg.reasoningPage == nil {
		cfg.reasoningPage = ui.NewPage(ui.PageConfig{Root: aiReasoningBodyRoot, Theme: &state.theme})
	}
	cfg.reasoningPage.Draw(canvas, layout.Body, state.declareAIReasoningBody)
	dialog.Actions(layout,
		components.ButtonAction{ID: hitAISetReasoningCancel, Label: state.messages.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitAISetReasoningSave, Label: m.ModelReasoningSave},
	)
	dialog.End()
}

// declareAIReasoningBody declares the reasoning dialog: the mode, and the effort
// preset only while the mode gives the model a manual choice.
func (state *State) declareAIReasoningBody(c ui.C) {
	pal, m, cfg := state.pal, state.messages.AISettings, &state.ai.cfg
	ui.Column(c).Fill().PadSides(2, 4, 0, 4).Gap(10).Children(func() {
		ui.Text(c, m.ModelReasoningMode).FontSize(metrics.FontSizeM).Weight(gfx.WeightMedium).Color(pal.text)
		mode := cfg.reasoningMode
		ui.Row(c).FillWidth().Children(func() {
			if ui.Choice(c, &mode, aiReasoningModeChoices[:], state.aiReasoningModeLabel).Fill().Changed() {
				cfg.reasoningMode = mode
			}
		})
		ui.Text(c, state.aiReasoningModeDescription(cfg.reasoningMode)).FontSize(metrics.FontSizeS).Color(pal.muted).LineHeight(18)
		if cfg.reasoningMode != 1 {
			return
		}
		ui.Text(c, m.ModelReasoningPreset).FontSize(metrics.FontSizeM).Weight(gfx.WeightMedium).Color(pal.text).MarginSides(0, 0, 10, 0)
		preset := cfg.reasoningPreset
		ui.Row(c).FillWidth().Children(func() {
			if ui.Choice(c, &preset, aiReasoningPresetChoices[:], state.aiReasoningPresetLabel).Fill().Changed() {
				cfg.reasoningPreset = preset
			}
		})
		ui.Text(c, state.aiReasoningPresetDescription(cfg.reasoningPreset)).FontSize(metrics.FontSizeS).Color(pal.muted).LineHeight(18)
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
