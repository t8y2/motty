package workbench

import (
	"crypto/rand"
	"slices"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

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

func (state *State) declareAISuggestionLibrary(page *settingsPage) {
	m, pal := state.messages.AISettings, state.pal
	c := page.c
	category := state.aiSuggestionCategory()
	pool := state.aiSuggestionPool(category)
	page.group(m.SuggestionTitle, func() {
		page.lineWith(m.SuggestionCategory, "", 214, func() {
			ui.Row(c).Gap(4).Items(ui.Center).Children(func() {
				connected, idle := aiButtonOutline, aiButtonOutline
				if category == aiSuggestionConnected {
					connected = aiButtonPrimary
				} else {
					idle = aiButtonPrimary
				}
				page.button(hitAISetSuggestConnected, m.SuggestionConnected, connected, false)
				page.button(hitAISetSuggestIdle, m.SuggestionIdle, idle, false)
			})
		})
		for index, entry := range pool {
			page.rowOf(56).PadX(12).Gap(8).Children(func() {
				ui.Column(c).Grow(1).Shrink(1).Children(func() {
					color := pal.text
					if entry.Disabled {
						color = pal.muted
					}
					ui.Text(c, entry.Text).FontSize(metrics.FontSizeS).Color(color).Ellipsis()
					if entry.Command != "" {
						ui.Text(c, entry.Command).FontSize(metrics.FontSizeS).Color(pal.muted).Ellipsis()
					}
				})
				ui.Row(c).Gap(1).Children(func() {
					button := func(id gfx.ID, icon uint8, label string, disabled bool) {
						state.declareIconButton(c, id, icon, label, disabled, iconButtonQuiet)
					}
					button(hitAISetSuggestMoveUp.At(index), iconArrowUp, m.SuggestionUp, index == 0)
					button(hitAISetSuggestMoveDown.At(index), iconChevronDown, m.SuggestionDown, index == len(pool)-1)
					button(hitAISetSuggestEdit.At(index), iconPencil, m.SuggestionEdit, false)
					icon, label := iconEyeOff, m.SuggestionDisable
					if entry.Disabled {
						icon, label = iconEye, m.SuggestionEnable
					}
					button(hitAISetSuggestDisable.At(index), icon, label, false)
					if strings.HasPrefix(entry.ID, "custom-") {
						button(hitAISetSuggestDelete.At(index), iconTrash, m.SuggestionDelete, false)
					} else {
						button(hitAISetSuggestReset.At(index), iconRefresh, m.SuggestionReset, false)
					}
				})
			})
		}
		page.aiFrame(settingsRowHeight, m.SuggestionAdd, "", pal.muted, func() {
			page.button(hitAISetSuggestAdd, m.SuggestionAdd, aiButtonOutline, false)
		})
	})
}

func (state *State) declareAISuggestionEditor(c ui.C) {
	m, cfg := state.messages.AISettings, &state.ai.cfg
	cfg.suggestionTextField.Bind(&cfg.suggestionText)
	cfg.suggestionCommandField.Bind(&cfg.suggestionCommand)
	dialog := ui.Dialog(c, ui.DialogSpec{Title: m.SuggestionEditTitle, Width: 520,
		Layer: dialogLayer, Scope: dialogScope, CloseID: hitAISetFormClose, InitialFocusID: hitAISetSuggestText,
	})
	dialog.Body(func() {
		ui.Column(c).Gap(8).Children(func() {
			ui.Text(c, m.SuggestionPrompt).FontSize(metrics.FontSizeS).Color(state.pal.text)
			ui.TextField(c, &cfg.suggestionTextField, m.SuggestionPrompt, "", ui.WithID(hitAISetSuggestText)).FillWidth().Height(32)
			if cfg.suggestionEdit.Context == aiSuggestionConnected {
				ui.Text(c, m.SuggestionCommand).FontSize(metrics.FontSizeS).Color(state.pal.text)
				ui.TextField(c, &cfg.suggestionCommandField, m.SuggestionCommand, "", ui.WithID(hitAISetSuggestCommand)).FillWidth().Height(32)
				ui.Paragraph(c, m.SuggestionCommandDesc, 0).FontSize(metrics.FontSizeS).Color(state.pal.muted)
			}
			if cfg.suggestionError != "" {
				ui.Paragraph(c, cfg.suggestionError, 0).FontSize(metrics.FontSizeS).Color(state.pal.danger)
			}
		})
	})
	dialog.Footer(func() {
		ui.Button(c, state.messages.Cancel).Secondary().Hit(hitAISetSuggestCancel, state.messages.Cancel)
		ui.Button(c, m.SuggestionSave).Hit(hitAISetSuggestSave, m.SuggestionSave)
	})
	if dialog.Dismissed() {
		state.aiCloseDialog()
	}
}
