package workbench

import (
	"fmt"
	"strings"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/ai/agent"
	"motty/internal/ai/risk"
)

const (
	aiAutoRows       = 8 // the most rows the box grows to by itself
	aiMaxReasons     = 4
	aiMaxSummary     = 5
	aiApprovalPad    = float32(10)
	aiApprovalSize   = float32(11)
	aiApprovalLine   = float32(15)
	aiButtonHeight   = float32(30)
	aiSendSize       = float32(28)
	aiComposerBar    = float32(48) // the row under the text: the model chip and Send
	aiComposerTarget = float32(32) // the compact target selector above the text
	aiComposerMargin = float32(8)  // between the box and the panel's edges
	aiBottomPadding  = float32(12)
	aiMentionRow     = float32(28)
)

// aiApprovalLayout is the part of the pinned question that is measured before it
// is declared, for the width of the panel: the command wrapped in the terminal's
// font, whose line count sets the height of the box it is painted in.
type aiApprovalLayout struct {
	summary  []string
	summaryH float32
}

// aiBody holds where the panel's hand-painted parts went once its layout was
// done: the controls and fields that are not flex elements.
type aiBody struct {
	close, history, newChat            ui.Box
	setup, list                        ui.Box
	summary, confirm, decline, approve ui.Box
	mentions                           [maxMentionSuggestions]ui.Box
	box                                ui.Box
}

// aiPrepareBottom measures what the declaration of the bottom needs to know
// before it is declared, and returns the height of the message box. The box grows
// with the text, up to a few rows, and scrolls beyond that; the text is wrapped
// and laid out by the box itself (components.TextareaLayout).
func (state *State) aiPrepareBottom(canvas *gfx.Canvas, width float32) (boxHeight float32) {
	ai, panel := &state.ai, &state.ai.panel
	theme := state.Theme()
	lineHeight := theme.Metrics.FontSizeM + 7
	boxWidth := width + 2*aiPad - 2*aiComposerMargin
	rows := max(panel.inputLayout.RowCount(canvas, theme, ai.input.String(), boxWidth, false), 2) // two rows to begin with
	state.aiSyncMentions()
	state.aiLayoutChips(canvas, width)
	if len(ai.approvals) > 0 {
		state.aiLayoutApprovalSummary(canvas, ai.approvals[0], width)
	}
	height := float32(min(rows, aiAutoRows))*lineHeight + 8 + aiComposerBar + aiComposerTarget
	if limit := panel.rect.Height * 0.6; limit > height {
		height = min(max(height, ai.composerHeight), limit)
	}
	return height
}

// aiByteOffset is the byte offset of the n-th character of value.
func aiByteOffset(value string, n int) int {
	for offset := range value {
		if n == 0 {
			return offset
		}
		n--
	}
	return len(value)
}

// declareAIBottom declares what sits under the messages, from the top down: the
// notice, the pinned question, the sessions an @ can name and the message box.
// The divider above is the first pixel of the 8 pixels between it and them.
func (state *State) declareAIBottom(f *ui.Frame, body *aiBody, boxHeight float32) {
	ai, panel, pal := &state.ai, &state.ai.panel, state.pal
	f.Column().Children(func() {
		f.Column().PadSides(7, aiPad, 0, aiPad).Gap(8).Children(func() {
			if text := state.aiNoticeText(); text != "" {
				f.Column().PadSides(6, 10, 6, 10).Background(withAlpha(pal.danger, 0.12)).Radius(radius.control).Children(func() {
					f.Paragraph(text, 6).FontSize(aiSmallSize).Weight(gfx.WeightRegular).Color(pal.danger).LineHeight(aiSmallLine)
				})
			}
			if len(ai.approvals) > 0 {
				state.declareAIApproval(f, body)
			}
			if len(panel.mentions) > 0 {
				f.Column().Pad(3).Border(1, pal.line).Background(pal.surface).Radius(radius.popup).Children(func() {
					for index := range panel.mentions {
						body.mentions[index] = f.Slot().Height(aiMentionRow)
					}
				})
			}
			if panel.chipRows > 0 {
				state.declareAIChips(f)
			}
		})
		body.box = f.Slot().Height(boxHeight).MarginSides(aiComposerMargin, aiComposerMargin, aiComposerMargin, aiComposerMargin)
	})
}

// aiNoticeText is the line above the message box: why the last send did
// nothing, or that the database is unusable. Not having a model is not a
// notice; the setup card says it.
func (state *State) aiNoticeText() string {
	ai, panel := &state.ai, &state.ai.panel
	if ai.notice != "" && ai.notice != errNoAIModel.Error() {
		return ai.notice
	}
	if ai.storeErr != nil {
		if panel.storeNotice == "" || panel.storeNoticeFor != ai.storeErr {
			panel.storeNoticeFor = ai.storeErr
			panel.storeNotice = fmt.Sprintf(state.messages.AIPanel.NoticeStoreError, ai.storeErr)
		}
		return panel.storeNotice
	}
	return ""
}

// aiLayoutApprovalSummary wraps the command of the pinned question in the
// terminal's font, which sets the height of the box it is painted in.
func (state *State) aiLayoutApprovalSummary(canvas *gfx.Canvas, approval *aiApproval, width float32) {
	layout := &state.ai.panel.approvalLay
	cell, cellHeight := canvas.MonoCellSize(aiMonoSize, state.terminalFontFace)
	layout.summary, _ = aiMonoWrap(layout.summary[:0], approval.request.Summary, max(int((width-2*aiApprovalPad-12)/cell), 8), aiMaxSummary)
	layout.summaryH = aiBlockHeight(len(layout.summary), cellHeight, aiBoxPad)
}

// declareAIApproval declares the pinned question (a border is inside the padding,
// hence one pixel less of it): what is asked, how risky it is,
// the command, why, a safer way, the word that confirms it, and the answer.
func (state *State) declareAIApproval(f *ui.Frame, body *aiBody) {
	ai, panel, pal := &state.ai, &state.ai.panel, state.pal
	m := state.messages.AIPanel
	approval := ai.approvals[0]
	request := &approval.request
	assessment := &request.Assessment
	foreground, fill := state.aiRiskColors(assessment.Level)
	title := m.ApprovalTitle
	if approval.escalation {
		foreground, fill = pal.accent, pal.accentSoft
		title = m.EscalationTitle
	}

	f.Column().PadSides(0, aiApprovalPad-1, aiApprovalPad-1, aiApprovalPad-1).Background(pal.surface).Border(1, withAlpha(foreground, 0.7)).Radius(radius.popup).Children(func() {
		f.Row().Height(aiApprovalPad + 24).Items(ui.Start).Children(func() {
			f.Text(title).FontSize(metrics.FontSizeM).Bold().Color(pal.text).LineHeight(20).Grow(1).MarginSides(8, 0, 0, 0)
			if !approval.escalation {
				f.Row().Height(16).PadX(6).Center().Shrink(0).MarginSides(10, 0, 0, 0).Background(fill).Radius(radius.control).Children(func() {
					f.Text(state.aiRiskText(assessment.Level)).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(foreground).LineHeight(14)
				})
			}
		})
		body.summary = f.Slot().Height(panel.approvalLay.summaryH).MarginSides(0, 0, 8, 0)
		if request.SessionLabel != "" {
			f.Wrap(m.Target+": "+request.SessionLabel).FontSize(aiSmallSize).Color(pal.text).LineHeight(aiSmallLine).MarginSides(0, 0, 8, 0)
		}
		if len(assessment.Reasons) > 0 {
			f.Column().MarginSides(0, 0, 4, 0).Children(func() {
				for index, reason := range assessment.Reasons {
					if index == aiMaxReasons {
						break
					}
					f.Row().Items(ui.Start).Children(func() {
						f.Text("•").FontSize(aiApprovalSize).Color(pal.muted).LineHeight(aiApprovalLine).Width(12)
						f.Paragraph(reason, 2).FontSize(aiApprovalSize).Weight(gfx.WeightRegular).Color(pal.muted).LineHeight(aiApprovalLine).Grow(1)
					})
				}
			})
		}
		if assessment.Safer != "" {
			f.Column().MarginSides(0, 0, 4, 0).Children(func() {
				f.Text(m.ApprovalSafer).FontSize(aiApprovalSize).Weight(gfx.WeightMedium).Color(pal.text).LineHeight(aiApprovalLine)
				f.Paragraph(assessment.Safer, 2).FontSize(aiApprovalSize).Weight(gfx.WeightRegular).Color(pal.muted).LineHeight(aiApprovalLine)
			})
		}
		if request.NeedsText {
			if panel.confirmFor != approval || panel.confirmFormat != m.ConfirmFmt {
				panel.confirmFor, panel.confirmFormat = approval, m.ConfirmFmt
				panel.confirmText = fmt.Sprintf(m.ConfirmFmt, request.Confirm)
			}
			f.Column().MarginSides(0, 0, 8, 0).Children(func() {
				f.Text(panel.confirmText).FontSize(aiSmallSize).Weight(gfx.WeightMedium).Color(pal.text).LineHeight(18).MarginSides(0, 0, 2, 0)
				body.confirm = f.Slot().Height(32)
			})
		}
		f.Row().Height(aiButtonHeight).Gap(8).Items(ui.Center).Children(func() {
			// More questions behind this one.
			if more := len(ai.approvals) - 1; more > 0 {
				if panel.moreCount != more || panel.moreFormat != m.ApprovalMoreFmt {
					panel.moreCount, panel.moreFormat = more, m.ApprovalMoreFmt
					panel.moreText = fmt.Sprintf(m.ApprovalMoreFmt, more)
				}
				f.Text(panel.moreText).FontSize(metrics.FontSizeS).Color(pal.muted).Grow(1)
			} else {
				f.Spacer()
			}
			body.decline = f.Slot().Size(92, aiButtonHeight)
			body.approve = f.Slot().Size(92, aiButtonHeight)
		})
	})
}

// paintAIBottom paints into the slots of the bottom the parts that are not flex
// elements, in the order the keyboard visits them: the pinned question's field and
// buttons, the sessions an @ can name, the message box and Send.
func (state *State) paintAIBottom(canvas *gfx.Canvas, body *aiBody) {
	ai, panel, pal := &state.ai, &state.ai.panel, state.pal
	m := state.messages.AIPanel
	theme := state.Theme()

	if len(ai.approvals) > 0 {
		approval := ai.approvals[0]
		request := &approval.request
		f := state.aiFlex.summary.Begin(canvas, theme, body.summary.Rect())
		f.Column().Fill().Pad(aiBoxPad).Background(pal.terminal).Radius(radius.item).Children(func() {
			state.aiMonoLines(f, panel.approvalLay.summary, pal.text)
		})
		f.End()
		allowed := true
		if request.NeedsText {
			allowed = strings.TrimSpace(ai.confirm.String()) == request.Confirm
			value := ai.confirm.String()
			focused := canvas.Focused() == hitAIConfirm
			components.PaintInput(canvas, theme, body.confirm.Rect(), components.InputSnapshot{
				ID: hitAIConfirm, Label: m.ConfirmField, Value: value, Placeholder: request.Confirm,
				Focused: focused, PlatformEditing: focused && value != "",
			})
		}
		approveLabel, declineLabel := m.Approve, m.Decline
		if approval.escalation {
			approveLabel, declineLabel = m.EscalationApprove, m.EscalationDecline
		}
		approve := components.BeginButton(canvas, theme, hitAIApprove, body.approve.Rect()).Small()
		decline := components.BeginButton(canvas, theme, hitAIDecline, body.decline.Rect()).Small()
		decline.Outline(declineLabel)
		if !allowed {
			approve = approve.Disabled()
		}
		if approval.escalation {
			approve.Default(approveLabel)
		} else if request.Assessment.Level >= risk.High {
			approve.Destructive(approveLabel)
		} else {
			approve.Default(approveLabel)
		}
	}

	for index, name := range panel.mentions {
		id := hitAIMentions.At(index)
		background := gfx.Color{}
		if panel.mentionIndex == index || canvas.FocusVisible() == id {
			background = pal.surfaceHot
		}
		label := "@" + name.name
		row := state.aiFlex.mention.Begin(canvas, theme, body.mentions[index].Rect())
		row.Row().Fill().PadX(8).Gap(10).Items(ui.Center).Background(background).Radius(radius.item).
			HitWith(id, label, gfx.HitOptions{Role: gfx.RoleButton, Hint: name.detail, Selected: panel.mentionIndex == index, SkipTab: true}).Children(func() {
			row.Text(label).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.text).LineHeight(18).Ellipsis().Grow(1)
			row.Text(name.detail).FontSize(metrics.FontSizeS).Color(pal.muted).LineHeight(16).MaxWidth(120).Ellipsis()
		})
		row.End()
	}

	// The message box, with the model chip and Send inside it.
	value := ai.input.String()
	focused := canvas.Focused() == hitAIInput
	selection, composition := ai.input.Selection(), ai.input.Composition
	box := body.box.Rect()
	panel.targetBounds = gfx.Rect{X: box.X + 8, Y: box.Y + 6, Width: min(200, max(box.Width-16, 0)), Height: 24}
	placeholder := m.InputPlaceholder
	if ai.mode == agent.ModeAsk {
		placeholder = m.InputPlaceholderAsk
	}
	snapshot := components.TextareaSnapshot{
		ID: hitAIInput, Label: m.InputLabel, Value: value, Placeholder: placeholder,
		Focused: focused, HideEditorText: true, ShowCaret: selection.Start == selection.End,
		Layout: &panel.inputLayout, InsetTop: aiComposerTarget, InsetBottom: aiComposerBar - 8,
		SelectionStart: aiByteOffset(value, selection.Start), SelectionEnd: aiByteOffset(value, selection.End),
		Caret: aiByteOffset(value, ai.input.Caret()),
	}
	if marked, composing := composition(); composing {
		snapshot.CompositionStart = aiByteOffset(value, marked.Start)
		snapshot.CompositionEnd = aiByteOffset(value, marked.End)
	}
	components.PaintTextarea(canvas, theme, box, snapshot)
	panel.composer = box
	panel.boxHeight = box.Height
	// The box's top edge is what is dragged to make it taller.
	canvas.HitWithOptions(hitAIComposerGrip, gfx.Rect{X: box.X + 8, Y: box.Y - 3, Width: max(box.Width-16, 0), Height: 7}, m.ResizeComposer,
		gfx.HitOptions{Role: gfx.RoleSlider, Cursor: gfx.PointerCursorResizeVertical, SkipFocus: true})

	running := ai.run != nil
	_, hasModel := state.aiModelText()
	disabled := !running && (!state.aiCanSend() || !hasModel)

	// Inside the box, along its bottom: the model chip, and Send (or Stop while a
	// request is running) as a round button.
	fill, foreground := pal.accent, pal.onAccent
	icon, label := iconArrowUp, m.Send
	if running {
		icon, label = iconRecordStop, m.Stop
	}
	switch {
	case disabled:
		fill, foreground = pal.surfaceHot, pal.faint
	case canvas.Pressed() == hitAISend, canvas.Hovered() == hitAISend:
		fill = pal.accentHot
	}
	var chip, scopeChip, reasoningChip ui.Box
	_, _, hasReasoning := state.aiCurrentModelReasoning()
	f := state.aiFlex.send.Begin(canvas, theme, box)
	f.Column().Fill().Justify(ui.End).Children(func() {
		f.Row().Height(aiSendSize+8).PadSides(0, 12, 6, 12).Gap(5).Items(ui.Center).Children(func() {
			state.declareAIModePill(f)
			if hasModel {
				chip = state.declareAIModelChip(f)
				if hasReasoning {
					reasoningChip = state.declareAIReasoningChip(f)
				}
				if ai.mode == agent.ModeAgent {
					scopeChip = state.declareAIAutoScopeChip(f, running || len(ai.approvals) > 0)
				}
			}
			f.Spacer()
			state.declareAIAttachButton(f)
			if ai.mode == agent.ModeAgent {
				state.declareAIUploadButton(f)
			}
			f.Stack().Size(aiSendSize, aiSendSize).Shrink(0).Children(func() {
				// The focus ring is two pixels outside the button.
				if !disabled && canvas.FocusVisible() == hitAISend {
					f.Column().Fill().Margin(-2).Background(withAlpha(pal.accent, 0.5)).Radius(aiSendSize/2 + 2)
				}
				f.Column().Fill().Center().Background(fill).Radius(aiSendSize/2).
					HitWith(hitAISend, label, gfx.HitOptions{Role: gfx.RoleButton, Disabled: disabled, Hint: label}).Children(func() {
					if running {
						// Stop is a solid square; the lucide stop glyph is a circle in a circle, twice round inside this round button.
						f.Column().Size(10, 10).Background(foreground).Radius(2)
					} else {
						arrow := lucideIcons[icon]
						arrow.Stroke = 3 // the glyph's 2 reads thin on a filled button
						f.Icon(arrow).Size(16, 16).Color(foreground)
					}
				})
			})
		})
	})
	f.End()
	if hasModel {
		panel.chip = chip.Rect()
		panel.scopeChip = gfx.Rect{}
		if ai.mode == agent.ModeAgent {
			panel.scopeChip = scopeChip.Rect()
		}
		if hasReasoning {
			panel.reasoningChip = reasoningChip.Rect()
		} else {
			panel.reasoningChip = gfx.Rect{}
		}
	} else {
		panel.reasoningChip = gfx.Rect{}
	}
}

// declareAIReasoningChip declares the pill in the message box that shows and controls
// the reasoning effort of the current model.
func (state *State) declareAIReasoningChip(f *ui.Frame) ui.Box {
	pal, m := state.pal, state.messages.AIPanel
	effort := state.aiEffectiveReasoningEffort()
	label := state.aiReasoningEffortLabel(effort)
	displayText := m.ReasoningEffort + ": " + label

	fill := pal.surfaceSoft
	if f.Canvas().Pressed() == hitAIReasoningChip || f.Canvas().Hovered() == hitAIReasoningChip || state.ai.panel.reasoningMenu {
		fill = pal.surfaceHot
	}
	return f.Row().Height(aiSendSize-4).PadX(9).Gap(5).Shrink(1).MinWidth(50).Items(ui.Center).Background(fill).Radius((aiSendSize-4)/2).
		HitWith(hitAIReasoningChip, displayText, gfx.HitOptions{Role: gfx.RoleButton, Expanded: state.ai.panel.reasoningMenu, Hint: m.ReasoningEffortHint}).Children(func() {
		f.Icon(lucideIcons[iconSliders]).Size(12, 12).Color(pal.muted).Shrink(0)
		f.Text(displayText).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.muted).Ellipsis().Shrink(1)
		f.Icon(lucideIcons[iconChevronDown]).Size(12, 12).Color(pal.muted).Shrink(0).MarginSides(0, 0, 0, 2)
	})
}

// declareAIModePill declares the switch between Ask and Agent in the message box:
// a pill naming the mode, which a click turns into the other. Agent, the mode that
// runs commands, is the one painted in the accent.
func (state *State) declareAIModePill(f *ui.Frame) {
	pal, m := state.pal, state.messages.AIPanel
	label, hint := m.ModeAsk, m.ModeHintAsk
	fill, foreground := pal.surfaceSoft, pal.muted
	if state.ai.mode != agent.ModeAsk {
		label, hint = m.ModeAgent, m.ModeHintAgent
		fill, foreground = pal.accentSoft, pal.accent
	}
	if f.Canvas().Pressed() == hitAIMode {
		fill = pal.surfaceHot
	} else if f.Canvas().Hovered() == hitAIMode && state.ai.mode == agent.ModeAsk {
		fill = pal.surfaceHot
	}
	f.Row().Height(aiSendSize-4).PadX(10).Shrink(0).Items(ui.Center).Background(fill).Radius((aiSendSize-4)/2).
		HitWith(hitAIMode, label, gfx.HitOptions{Role: gfx.RoleButton, Value: label, Hint: hint}).Children(func() {
		f.Text(label).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(foreground)
	})
}

// declareAIModelChip declares the pill in the message box that names the model and
// opens the list of the others.
func (state *State) declareAIModelChip(f *ui.Frame) ui.Box {
	pal := state.pal
	fill := pal.surfaceSoft
	if f.Canvas().Pressed() == hitAIModelChip || f.Canvas().Hovered() == hitAIModelChip || state.ai.panel.modelMenu {
		fill = pal.surfaceHot
	}
	label := state.ai.settings.defaultModel
	return f.Row().Height(aiSendSize-4).PadX(9).Gap(5).Shrink(1).MinWidth(56).Items(ui.Center).Background(fill).Radius((aiSendSize-4)/2).
		HitWith(hitAIModelChip, fmt.Sprintf(state.messages.AIPanel.ModelChooseFmt, label), gfx.HitOptions{Role: gfx.RoleButton, Expanded: state.ai.panel.modelMenu}).Children(func() {
		chipIcon, known := state.aiDefaultProviderIcon()
		if !known {
			chipIcon = lucideIcons[iconSliders]
		}
		f.Icon(chipIcon).Size(13, 13).Color(pal.muted).Shrink(0)
		f.Text(label).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.muted).Ellipsis().Shrink(1)
		f.Icon(lucideIcons[iconChevronDown]).Size(12, 12).Color(pal.muted).Shrink(0).MarginSides(0, 0, 0, 2)
	})
}

// declareAIAutoScopeChip declares the pill in the message box that names the
// approval scope of this conversation and opens the scopes to pick from. The chip
// carries the scope name alone: a prefix such as "Auto:" reads as a second,
// unexplained word, and the hint and the menu say what the name controls. Only
// Agent mode runs commands, so only it shows the chip; a request already running
// or waiting for an answer keeps the scope it started with, which is why locked
// disables the chip but still names it.
func (state *State) declareAIAutoScopeChip(f *ui.Frame, locked bool) ui.Box {
	pal, m := state.pal, state.messages.AIPanel
	level := state.aiAutoUpTo()
	label := state.aiScopeName(level)
	foreground, icon := pal.muted, iconCircleCheck
	if level >= risk.High {
		foreground, icon = pal.warning, iconAlert
	}
	fill := pal.surfaceSoft
	if f.Canvas().Pressed() == hitAIAutoScope || f.Canvas().Hovered() == hitAIAutoScope || state.ai.panel.autoMenu {
		fill = pal.surfaceHot
	}
	hint := m.AutoScopeHint
	if locked {
		hint = m.AutoScopeBusy
	}
	return f.Row().Height(aiSendSize-4).PadX(9).Gap(4).Shrink(0).Items(ui.Center).Background(fill).Radius((aiSendSize-4)/2).
		HitWith(hitAIAutoScope, label, gfx.HitOptions{Role: gfx.RoleButton, Hint: hint, Disabled: locked, Expanded: state.ai.panel.autoMenu}).Children(func() {
		f.Icon(lucideIcons[icon]).Size(12, 12).Color(foreground).Shrink(0)
		f.Text(label).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(foreground).Ellipsis()
		f.Icon(lucideIcons[iconChevronDown]).Size(12, 12).Color(pal.muted).Shrink(0).MarginSides(0, 0, 0, 2)
	})
}

// aiScope is one row of the approval chip's menu.
type aiScope struct {
	level       risk.Level
	name        string
	description string
}

// aiScopes lists what the chip offers, riskiest last, with what each scope lets
// run without asking. Critical is never among them: it always asks for the typed
// word, whatever the scope.
func (state *State) aiScopes() [3]aiScope {
	m := state.messages.AIPanel
	return [3]aiScope{
		{level: risk.Low, name: m.RiskLow, description: m.AutoScopeLowDesc},
		{level: risk.Medium, name: m.RiskMedium, description: m.AutoScopeMediumDesc},
		{level: risk.High, name: m.RiskHigh, description: m.AutoScopeHighDesc},
	}
}

// aiScopeName is the chip's word for a level, from the same list the menu shows.
func (state *State) aiScopeName(level risk.Level) string {
	scopes := state.aiScopes()
	for _, scope := range scopes {
		if scope.level == level {
			return scope.name
		}
	}
	return scopes[len(scopes)-1].name
}

// aiDefaultProviderIcon is the mark of the provider new questions go to.
func (state *State) aiDefaultProviderIcon() (gfx.Icon, bool) {
	for _, provider := range state.ai.providers {
		if provider.ID == state.ai.settings.defaultProvider {
			return aiProviderIcon(provider), true
		}
	}
	return gfx.Icon{}, false
}

// aiMenuProvider is a provider the chip's menu offers, with the models that were
// fetched or added for it.
type aiMenuProvider struct {
	id, name string
	icon     gfx.Icon
	models   []aiMenuModel
}

// aiMenuModel is a model the chip offers.
type aiMenuModel struct {
	name    string
	current bool
}

// aiMenuProviders lists the enabled providers that have models, each with them.
// A model that was fetched but not switched on yet is chosen from here too.
func (state *State) aiMenuProviders() []aiMenuProvider {
	ai := &state.ai
	var list []aiMenuProvider
	for _, provider := range ai.providers {
		if !provider.Enabled {
			continue
		}
		entry := aiMenuProvider{id: provider.ID, name: provider.Name, icon: aiProviderIcon(provider)}
		for _, model := range ai.models {
			if model.ProviderID == provider.ID && len(entry.models) < aiMenuModels {
				entry.models = append(entry.models, aiMenuModel{name: model.Name, current: state.aiIsDefault(provider.ID, model.Name)})
			}
		}
		if len(entry.models) > 0 {
			list = append(list, entry)
		}
	}
	return list
}

// aiMenuOpening is where the menu opens: the provider whose model answers now,
// with the list scrolled so that model is in view.
func (state *State) aiMenuOpening() (provider int, list components.MenuScroll) {
	list.BarID = hitAIModelBar
	provider, row := state.aiMenuOpeningRow()
	list.Offset = float32(row) * 32
	return provider, list
}

func (state *State) aiMenuOpeningRow() (provider, scroll int) {
	for index, entry := range state.aiMenuProviders() {
		if entry.id != state.ai.settings.defaultProvider {
			continue
		}
		for at, model := range entry.models {
			if model.current {
				return index, min(max(at-aiMenuRows/2, 0), max(len(entry.models)-aiMenuRows, 0))
			}
		}
		return index, 0
	}
	return 0, 0
}

// paintAIModelMenu is the chip's menu, above it: the providers, and beside them
// the models of the one that is open, a few rows at a time and scrolled with the
// wheel.
func (state *State) paintAIModelMenu(canvas *gfx.Canvas) {
	panel := &state.ai.panel
	providers := state.aiMenuProviders()
	if len(providers) == 0 {
		return
	}
	theme := state.Theme()
	size := theme.Metrics.FontSizeM
	panel.modelProvider = min(max(panel.modelProvider, 0), len(providers)-1)
	// Resting on a provider row opens its models after the same 100ms Base UI
	// uses, even while another provider's list is open.
	if index, ok := canvas.Hovered().In(hitAIModelProviders); ok && index < len(providers) && index != panel.modelProvider {
		if id, wait := panel.modelHover.Rest(canvas.Hovered(), time.Now(), 100*time.Millisecond); !id.IsZero() {
			panel.modelProvider = index
			panel.modelList = components.MenuScroll{BarID: hitAIModelBar}
			panel.modelSubmenu = panel.modelSubmenu.Open(canvas.Hovered())
			panel.modelTypeahead.Reset()
		} else if wait > 0 {
			canvas.RequestFrameIn(wait)
		}
	} else {
		panel.modelHover.Reset()
	}
	open := providers[panel.modelProvider]
	submenuOpen := panel.modelSubmenu.IsOpen(hitAIModelProviders.At(panel.modelProvider))
	options := components.DropdownMenuOptions{Layer: dialogLayer, Scope: dialogScope, DismissID: hitAIModelDismiss, Viewport: panel.window, Side: components.OverlayTop}
	menuWidth := func(width float32) float32 { return min(max(width+56, 140), 320) }

	// The providers first.
	var providersWidth float32
	for _, provider := range providers {
		providersWidth = max(providersWidth, canvas.TextWidthWeight(provider.name, size, gfx.WeightRegular))
	}
	providersWidth += 24 // the mark before each name
	providersHeight := float32(32*len(providers) + 8)
	first := state.aiMenuRect(canvas, menuWidth(providersWidth), providersHeight, func(popup ui.Box) { popup.Above(panel.chip, 6, ui.Start) })
	menu := components.BeginDropdownMenu(canvas, theme, first, options)
	var row gfx.Rect
	for index, provider := range providers {
		item := menu.AddItem(components.DropdownMenuItem{
			ID: hitAIModelProviders.At(index), Label: provider.name, Icon: provider.icon, Kind: components.DropdownMenuSubmenu,
			Expanded: panel.modelSubmenu.IsOpen(hitAIModelProviders.At(index)),
		})
		if index == panel.modelProvider {
			row = item
		}
	}
	menu.End()
	if !submenuOpen {
		panel.modelsRect = gfx.Rect{}
		return
	}

	// Beside the row that opened them: the models, a window of them.
	visible := min(len(open.models), aiMenuRows)
	var modelsWidth float32
	for _, model := range open.models {
		modelsWidth = max(modelsWidth, canvas.TextWidthWeight(model.name, size, gfx.WeightRegular))
	}
	modelsHeight := float32(32*visible + 8)
	width := menuWidth(modelsWidth)
	// Beside the row that opened them, top aligned, flipping left at the edge.
	models := components.SubmenuPlacement(row, gfx.Size{Width: width, Height: modelsHeight}, panel.window)
	panel.modelsRect = models
	// The model list owns Escape while it is open; its parent's backdrop still
	// dismisses both. It has no backdrop of its own.
	options.DismissID, options.Viewport = hitAIModelSubDismiss, gfx.Rect{}
	options.ID = hitAIModelList
	options.Scroll = &panel.modelList
	menu = components.BeginDropdownMenu(canvas, theme, models, options)
	for index, model := range open.models {
		menu.AddItem(components.DropdownMenuItem{ID: hitAIModels.At(index), Label: model.name, Kind: components.DropdownMenuRadio, Selected: model.current})
	}
	menu.End()
}

// paintAIAutoScopeMenu is the approval chip's menu, above it: the scopes and what
// each lets run without asking. It is a context menu because every row carries a
// description, which the animated dropdown menu has no room for; the cost is that
// it appears at once instead of growing out of the chip.
func (state *State) paintAIAutoScopeMenu(canvas *gfx.Canvas) {
	panel := &state.ai.panel
	if !panel.autoMenu || state.ai.mode == agent.ModeAsk {
		return
	}
	if _, hasModel := state.aiModelText(); !hasModel {
		return
	}
	theme := state.Theme()
	scopes := state.aiScopes()
	items := make([]components.ContextMenuItem, 0, len(scopes))
	var width, height float32
	for _, scope := range scopes {
		item := components.ContextMenuItem{
			ID: hitAIAutoScopeLevels.At(int(scope.level)), Label: scope.name, Description: scope.description,
			Kind: components.ContextMenuRadio, Selected: state.aiAutoUpTo() == scope.level,
			Value: scope.name, Hint: scope.description,
		}
		items = append(items, item)
		width = max(width, components.ContextMenuItemWidth(canvas, theme, item))
		height += components.ContextMenuItemHeight(theme, item, 0)
	}
	height += 8 // the surface's padding above and below the rows
	bounds := state.aiMenuRect(canvas, width, height, func(popup ui.Box) { popup.Above(panel.scopeChip, 6, ui.Start) })
	menu := components.BeginContextMenu(canvas, theme, bounds, components.ContextMenuOptions{
		Layer: dialogLayer, Scope: hitAIAutoScopeDismiss, DismissID: hitAIAutoScopeDismiss,
		Viewport: panel.window, Scroll: &panel.autoScroll,
	})
	for _, item := range items {
		menu.AddItem(item)
	}
	// A menu opened with the pointer has no keyboard focus of its own yet: give
	// the scope the first row, which is also where the arrow keys start. Once a
	// row holds focus, painting must not take it back.
	if focused := canvas.Focused(); focused.Scope() != hitAIAutoScopeLevels {
		canvas.SetInitialFocus(hitAIAutoScopeLevels.At(0))
	}
	menu.End()
}

// paintAIReasoningMenu is the reasoning chip's popup menu, above it: lists the supported levels.
func (state *State) paintAIReasoningMenu(canvas *gfx.Canvas) {
	panel := &state.ai.panel
	if !panel.reasoningMenu {
		return
	}
	_, levels, ok := state.aiCurrentModelReasoning()
	if !ok || len(levels) == 0 {
		return
	}
	theme := state.Theme()
	currentEffort := state.aiEffectiveReasoningEffort()
	items := make([]components.ContextMenuItem, 0, len(levels))
	var width, height float32
	for index, level := range levels {
		label := state.aiReasoningEffortLabel(level)
		item := components.ContextMenuItem{
			ID:       hitAIReasoningMenu.At(index),
			Label:    label,
			Kind:     components.ContextMenuRadio,
			Selected: currentEffort == level,
			Value:    level,
		}
		items = append(items, item)
		width = max(width, components.ContextMenuItemWidth(canvas, theme, item))
		height += components.ContextMenuItemHeight(theme, item, 0)
	}
	height += 8
	bounds := state.aiMenuRect(canvas, width, height, func(popup ui.Box) { popup.Above(panel.reasoningChip, 6, ui.Start) })
	menu := components.BeginContextMenu(canvas, theme, bounds, components.ContextMenuOptions{
		Layer:     dialogLayer,
		Scope:     hitAIReasoningDismiss,
		DismissID: hitAIReasoningDismiss,
		Viewport:  panel.window,
	})
	for _, item := range items {
		menu.AddItem(item)
	}
	if focused := canvas.Focused(); focused.Scope() != hitAIReasoningMenu {
		canvas.SetInitialFocus(hitAIReasoningMenu.At(0))
	}
	menu.End()
}

// aiMenuRect is where a menu width wide and height tall goes: place says where it
// hangs (above the chip, or at a point), and flex keeps it inside the window.
func (state *State) aiMenuRect(canvas *gfx.Canvas, width, height float32, place func(ui.Box)) gfx.Rect {
	f := state.aiFlex.menu.Begin(canvas, state.Theme(), state.ai.panel.window)
	popup := f.Popup().Size(width, height)
	place(popup)
	f.End()
	return popup.Rect()
}

// aiChooseModel makes the n-th model of the open provider the one that answers,
// switching it on if it was only fetched.
func (state *State) aiChooseModel(n int) {
	panel := &state.ai.panel
	panel.modelMenu = false
	providers := state.aiMenuProviders()
	if panel.modelProvider < 0 || panel.modelProvider >= len(providers) {
		return
	}
	provider := providers[panel.modelProvider]
	if n < 0 || n >= len(provider.models) {
		return
	}
	db, err := state.aiStore()
	if err == nil {
		err = db.SetModelEnabled(provider.id, provider.models[n].name, true)
	}
	if err == nil {
		err = db.SetDefaultModel(provider.id, provider.models[n].name)
	}
	if err != nil {
		state.ai.notice = state.aiScrub(err.Error(), "")
		return
	}
	state.aiWrote()
}
