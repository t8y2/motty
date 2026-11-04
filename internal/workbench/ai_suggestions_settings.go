package workbench

import (
	"crypto/rand"
	"slices"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

func (state *State) aiSuggestionSourceOptions() []components.SegmentOption {
	m := state.messages.AISettings
	return []components.SegmentOption{{ID: hitAISetSuggestLibrary, Label: m.SuggestionRotate}, {ID: hitAISetSuggestAI, Label: m.SuggestionAI}}
}

func (state *State) aiSuggestionContextOptions() []components.SegmentOption {
	m := state.messages.AISettings
	return []components.SegmentOption{{ID: hitAISetSuggestConnected, Label: m.SuggestionConnected}, {ID: hitAISetSuggestIdle, Label: m.SuggestionIdle}}
}

func (state *State) aiSetSuggestionSource(source aiSuggestionSource) {
	if state.aiPut("ai.suggestions.source", string(source)) {
		state.aiStartSuggestions()
	}
}

func (state *State) aiSuggestionCategory() aiSuggestionContext {
	if state.ai.cfg.suggestionContext == aiSuggestionIdle {
		return aiSuggestionIdle
	}
	return aiSuggestionConnected
}

func (state *State) aiSuggestionEditAt(index int) {
	pool := state.aiSuggestionPool(state.aiSuggestionCategory())
	if index >= 0 && index < len(pool) {
		state.aiOpenSuggestionEditor(pool[index])
	}
}

func (state *State) aiOpenSuggestionEditor(entry aiSuggestion) {
	if entry.ID == "" {
		pool := state.aiSuggestionPool(state.aiSuggestionCategory())
		order := len(pool) + 1
		for _, current := range pool {
			order = max(order, current.Order+1)
		}
		entry = aiSuggestion{ID: "custom-" + rand.Text(), Context: state.aiSuggestionCategory(), Locale: state.aiSuggestionLocale(), Order: order}
	}
	cfg := &state.ai.cfg
	cfg.suggestionEdit, cfg.suggestionText, cfg.suggestionCommand = entry, text.Editor{}, text.Editor{}
	cfg.suggestionText.Restrict(printableLine, 200)
	cfg.suggestionCommand.Restrict(printableLine, 4096)
	cfg.suggestionText.Insert(entry.Text)
	cfg.suggestionCommand.Insert(entry.Command)
	cfg.suggestionError = ""
	cfg.dialog = aiDialogSuggestionEdit
}

func (state *State) aiSubmitSuggestion() {
	cfg := &state.ai.cfg
	entry := cfg.suggestionEdit
	entry.Text, entry.Command = strings.TrimSpace(cfg.suggestionText.String()), strings.TrimSpace(cfg.suggestionCommand.String())
	if err := state.aiSaveSuggestion(entry); err != nil {
		cfg.suggestionError = state.messages.AISettings.SuggestionInvalid
		return
	}
	state.aiCloseDialog()
}

func (state *State) aiSuggestionDisableAt(index int) {
	pool := state.aiSuggestionPool(state.aiSuggestionCategory())
	if index < 0 || index >= len(pool) {
		return
	}
	entry := pool[index]
	entry.Disabled = !entry.Disabled
	state.aiSuggestionSaveOrNotice(entry)
}

func (state *State) aiSuggestionSaveOrNotice(entry aiSuggestion) {
	if err := state.aiSaveSuggestion(entry); err != nil {
		state.ai.cfg.notice = err.Error()
	}
}

func (state *State) aiSuggestionMoveAt(index, delta int) {
	pool := state.aiSuggestionPool(state.aiSuggestionCategory())
	next := index + delta
	if index < 0 || next < 0 || index >= len(pool) || next >= len(pool) {
		return
	}
	first, second := pool[index], pool[next]
	first.Order, second.Order = second.Order, first.Order
	if first.Order == second.Order {
		first.Order, second.Order = next+1, index+1
	}
	state.aiSuggestionSaveOrNotice(first)
	state.aiSuggestionSaveOrNotice(second)
}

func (state *State) aiSuggestionResetAt(index int) {
	pool := state.aiSuggestionPool(state.aiSuggestionCategory())
	if index < 0 || index >= len(pool) || strings.HasPrefix(pool[index].ID, "custom-") {
		return
	}
	if err := state.aiResetBuiltinSuggestion(pool[index].ID, pool[index].Locale); err != nil {
		state.ai.cfg.notice = err.Error()
	}
}

func (state *State) aiSuggestionDeleteAt(index int) {
	pool := state.aiSuggestionPool(state.aiSuggestionCategory())
	if index < 0 || index >= len(pool) || !strings.HasPrefix(pool[index].ID, "custom-") {
		return
	}
	library := state.ai.suggestions.library
	library.Custom = slices.DeleteFunc(slices.Clone(library.Custom), func(current aiSuggestion) bool {
		return current.ID == pool[index].ID && current.Locale == pool[index].Locale
	})
	if err := state.aiStoreSuggestionLibrary(library); err != nil {
		state.ai.cfg.notice = err.Error()
	}
}

func (state *State) paintAISuggestionLibrary(page *settingsPage) {
	m, pal := state.messages.AISettings, state.pal
	f := page.f
	category := state.aiSuggestionCategory()
	source := hitAISetSuggestLibrary
	if state.ai.settings.suggestionSource == aiSuggestionAI {
		source = hitAISetSuggestAI
	}
	contexts := state.aiSuggestionContextOptions()
	sources := state.aiSuggestionSourceOptions()
	selectedContext := hitAISetSuggestConnected
	if category == aiSuggestionIdle {
		selectedContext = hitAISetSuggestIdle
	}
	pool := state.aiSuggestionPool(category)
	page.group(m.SuggestionTitle, func() {
		page.aiFrame(settingsRowHeight, m.SuggestionSource, m.SuggestionSourceDesc, pal.muted, func() {
			page.segmentSlot(280, sources, source)
		})
		f.Column().PadSides(8, 18, 12, 18).Children(func() {
			f.Wrap(m.SuggestionPrivacy).FontSize(metrics.FontSizeS).LineHeight(17).Color(pal.muted)
		})
		page.aiFrame(settingsRowHeight, m.SuggestionPreview, m.SuggestionPreviewDesc, pal.muted, func() {
			page.button(hitAISetSuggestPreview, m.SuggestionPreview, aiButtonOutline, false)
		})
		if state.ai.cfg.suggestionPreview {
			f.Column().Pad(14).Background(pal.terminal).Children(func() {
				f.Wrap(state.aiSuggestionSnapshot()).FontSize(metrics.FontSizeS).LineHeight(17).Color(pal.text)
			})
		}
		page.aiFrame(settingsRowHeight, m.SuggestionCategory, "", pal.muted, func() {
			page.segmentSlot(220, contexts, selectedContext)
		})
		for index, entry := range pool {
			index, entry := index, entry
			page.rowOf(56).PadX(12).Gap(8).Children(func() {
				f.Column().Grow(1).Shrink(1).Children(func() {
					color := pal.text
					if entry.Disabled {
						color = pal.muted
					}
					f.Text(entry.Text).FontSize(metrics.FontSizeS).Color(color).Ellipsis()
					if entry.Command != "" {
						f.Text(entry.Command).FontSize(metrics.FontSizeS).Color(pal.muted).Ellipsis()
					}
				})
				f.Row().Gap(1).Children(func() {
					page.iconButton(hitAISetSuggestMoveUp.At(index), iconArrowUp, m.SuggestionUp, index == 0)
					page.iconButton(hitAISetSuggestMoveDown.At(index), iconChevronDown, m.SuggestionDown, index == len(pool)-1)
					page.iconButton(hitAISetSuggestEdit.At(index), iconPencil, m.SuggestionEdit, false)
					icon, label := iconEyeOff, m.SuggestionDisable
					if entry.Disabled {
						icon, label = iconEye, m.SuggestionEnable
					}
					page.iconButton(hitAISetSuggestDisable.At(index), icon, label, false)
					if strings.HasPrefix(entry.ID, "custom-") {
						page.iconButton(hitAISetSuggestDelete.At(index), iconTrash, m.SuggestionDelete, false)
					} else {
						page.iconButton(hitAISetSuggestReset.At(index), iconRefresh, m.SuggestionReset, false)
					}
				})
			})
		}
		page.aiFrame(settingsRowHeight, m.SuggestionAdd, "", pal.muted, func() {
			page.button(hitAISetSuggestAdd, m.SuggestionAdd, aiButtonOutline, false)
		})
	})
}

func (state *State) paintAISuggestionEditor(canvas *ui.Canvas, viewport ui.Rect) {
	m, cfg, theme := state.messages.AISettings, &state.ai.cfg, state.Theme()
	bodyHeight := float32(72)
	if cfg.suggestionEdit.Context == aiSuggestionConnected {
		bodyHeight += 72
	}
	if cfg.suggestionError != "" {
		bodyHeight += 24
	}
	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		CloseID: hitAISetFormClose, InitialFocusID: hitAISetSuggestText,
		ContentSize: ui.Size{Width: 520}, FitBody: true, BodyHeight: bodyHeight,
	})
	layout := dialog.Begin(m.SuggestionEditTitle, m.SuggestionEditDesc)
	var prompt, command flex.Box
	f := state.settingsRow.Begin(canvas, theme, layout.Body)
	f.Column().Fill().PadSides(2, 4, 0, 4).Children(func() {
		prompt = f.Slot().Height(70)
		if cfg.suggestionEdit.Context == aiSuggestionConnected {
			command = f.Slot().Height(70)
		}
		if cfg.suggestionError != "" {
			f.Text(cfg.suggestionError).FontSize(metrics.FontSizeS).Color(state.pal.danger)
		}
	})
	f.End()
	state.aiFormField(canvas, hitAISetSuggestText, m.SuggestionPrompt, m.SuggestionPrompt, "", false, &cfg.suggestionText, prompt.Rect())
	if cfg.suggestionEdit.Context == aiSuggestionConnected {
		state.aiFormField(canvas, hitAISetSuggestCommand, m.SuggestionCommand, m.SuggestionCommandPlaceholder, m.SuggestionCommandDesc, false, &cfg.suggestionCommand, command.Rect())
	}
	dialog.Actions(layout,
		components.ButtonAction{ID: hitAISetSuggestCancel, Label: state.messages.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitAISetSuggestSave, Label: m.SuggestionSave},
	)
	dialog.End()
}
