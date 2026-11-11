package workbench

import (
	"fmt"
	"strings"

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
// done: the controls and fields that are not flex elements. The composer chips
// and the message box are page elements; their boxes are kept so the menus can
// hang above the chips and the native editor can paint into the box.
type aiBody struct {
	close, history, newChat                  ui.Box
	setup, list                              ui.Box
	confirm, decline, approve                ui.Box
	box, modelChip, scopeChip, reasoningChip ui.Box
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
//
// The box is a stack of the native editor and the row under it, so the editor
// keeps its surface and text input while the chips, the attach and upload
// buttons and Send are ordinary page elements painted over it.
func (state *State) declareAIBottom(c ui.C, body *aiBody, boxHeight float32) {
	ai, panel, pal := &state.ai, &state.ai.panel, state.pal
	ui.Column(c).Children(func() {
		ui.Column(c).PadSides(7, aiPad, 0, aiPad).Gap(8).Children(func() {
			if text := state.aiNoticeText(); text != "" {
				ui.Column(c).PadSides(6, 10, 6, 10).Background(withAlpha(pal.danger, 0.12)).Radius(radius.control).Children(func() {
					ui.Paragraph(c, text, 6).FontSize(aiSmallSize).Weight(gfx.WeightRegular).Color(pal.danger).LineHeight(aiSmallLine)
				})
			}
			if len(ai.approvals) > 0 {
				state.declareAIApproval(c, body)
			}
			if len(panel.mentions) > 0 {
				ui.Column(c).Pad(3).Border(1, pal.line).Background(pal.surface).Radius(radius.popup).Children(func() {
					for index, name := range panel.mentions {
						state.declareAIMention(c, index, name)
					}
				})
			}
			if panel.chipRows > 0 {
				state.declareAIChips(c)
			}
		})
		ui.Stack(c).Height(boxHeight).MarginSides(aiComposerMargin, aiComposerMargin, aiComposerMargin, aiComposerMargin).Children(func() {
			// The native editor is painted by its Draw, under the row declared
			// after it: the text is the editor's, the controls over it are the
			// page's.
			body.box = ui.Slot(c).Fill().Draw(panel.composerDraw)
			ui.Column(c).Fill().Justify(ui.End).Children(func() {
				state.declareAIComposerRow(c, body)
			})
		})
	})
}

// declareAIComposerRow declares the row along the bottom of the message box:
// the mode, the model and the other chips at the left, attach, upload and Send
// at the right.
func (state *State) declareAIComposerRow(c ui.C, body *aiBody) {
	ai := &state.ai
	running := ai.run != nil
	_, hasModel := state.aiModelText()
	disabled := !running && (!state.aiCanSend() || !hasModel)
	_, _, hasReasoning := state.aiCurrentModelReasoning()
	ui.Row(c).Height(aiSendSize+8).PadSides(0, 12, 6, 12).Gap(5).Items(ui.Center).Children(func() {
		state.declareAIModePill(c)
		if hasModel {
			body.modelChip = state.declareAIModelChip(c)
			if hasReasoning {
				body.reasoningChip = state.declareAIReasoningChip(c)
			}
			if ai.mode == agent.ModeAgent {
				body.scopeChip = state.declareAIAutoScopeChip(c, running || len(ai.approvals) > 0)
			}
		}
		ui.Spacer(c)
		state.declareAIAttachButton(c)
		if ai.mode == agent.ModeAgent {
			state.declareAIUploadButton(c)
		}
		state.declareAISendButton(c, running, disabled)
	})
}

// declareAISendButton declares Send, or Stop while a request is running, as the
// round button in the corner of the box. The focus ring is two pixels outside
// the button.
func (state *State) declareAISendButton(c ui.C, running, disabled bool) {
	pal, m := state.pal, state.messages.AIPanel
	canvas := c.Canvas()
	fill, foreground := pal.accent, pal.onAccent
	icon, label := iconArrowUp, m.Send
	if running {
		icon, label = iconRecordStop, m.Stop
	}
	switch {
	case disabled:
		fill, foreground = pal.surfaceHot, pal.faint
	case canvas != nil && (canvas.Pressed() == hitAISend || canvas.Hovered() == hitAISend):
		fill = pal.accentHot
	}
	ui.Stack(c).Size(aiSendSize, aiSendSize).Shrink(0).Children(func() {
		if !disabled && canvas != nil && canvas.FocusVisible() == hitAISend {
			ui.Column(c).Fill().Margin(-2).Background(withAlpha(pal.accent, 0.5)).Radius(aiSendSize/2 + 2)
		}
		ui.Column(c).Fill().Center().Background(fill).Radius(aiSendSize/2).
			HitWith(hitAISend, label, gfx.HitOptions{Role: gfx.RoleButton, Disabled: disabled, Hint: label}).Children(func() {
			if running {
				// Stop is a solid square; the lucide stop glyph is a circle in a
				// circle, twice round inside this round button.
				ui.Column(c).Size(10, 10).Background(foreground).Radius(2)
			} else {
				arrow := lucideIcons[icon]
				arrow.Stroke = 3 // the glyph's 2 reads thin on a filled button
				ui.Icon(c, arrow).Size(16, 16).Color(foreground)
			}
		})
	})
}

// paintAIComposerTextarea paints the message box's surface, text and text hit
// into the page element that reserves it. It runs while the page is painted,
// before the chips declared after it, so the native editing surface stays under
// the row and keeps the whole box as its pointer target.
func (state *State) paintAIComposerTextarea(d *ui.DrawContext) {
	ai, panel, m := &state.ai, &state.ai.panel, state.messages.AIPanel
	theme := state.Theme()
	value := ai.input.String()
	focused := d.Canvas.Focused() == hitAIInput
	selection, composition := ai.input.Selection(), ai.input.Composition
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
	components.PaintTextarea(d.Canvas, theme, d.Rect, snapshot)
}

// declareAIMention declares one session the @ before the caret can name.
func (state *State) declareAIMention(c ui.C, index int, name mentionName) {
	panel, pal := &state.ai.panel, state.pal
	id := hitAIMentions.At(index)
	var background gfx.Color
	if canvas := c.Canvas(); canvas != nil && (panel.mentionIndex == index || canvas.FocusVisible() == id) {
		background = pal.surfaceHot
	}
	label := "@" + name.name
	ui.Row(c).Height(aiMentionRow).FillWidth().PadX(8).Gap(10).Items(ui.Center).Background(background).Radius(radius.item).
		HitWith(id, label, gfx.HitOptions{Role: gfx.RoleButton, Hint: name.detail, Selected: panel.mentionIndex == index, SkipTab: true}).Children(func() {
		ui.Text(c, label).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.text).LineHeight(18).Ellipsis().Grow(1)
		ui.Text(c, name.detail).FontSize(metrics.FontSizeS).Color(pal.muted).LineHeight(16).MaxWidth(120).Ellipsis()
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
func (state *State) declareAIApproval(c ui.C, body *aiBody) {
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

	ui.Column(c).PadSides(0, aiApprovalPad-1, aiApprovalPad-1, aiApprovalPad-1).Background(pal.surface).Border(1, withAlpha(foreground, 0.7)).Radius(radius.popup).Children(func() {
		ui.Row(c).Height(aiApprovalPad + 24).Items(ui.Start).Children(func() {
			ui.Text(c, title).FontSize(metrics.FontSizeM).Bold().Color(pal.text).LineHeight(20).Grow(1).MarginSides(8, 0, 0, 0)
			if !approval.escalation {
				ui.Row(c).Height(16).PadX(6).Center().Shrink(0).MarginSides(10, 0, 0, 0).Background(fill).Radius(radius.control).Children(func() {
					ui.Text(c, state.aiRiskText(assessment.Level)).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(foreground).LineHeight(14)
				})
			}
		})
		ui.Column(c).Height(panel.approvalLay.summaryH).MarginSides(0, 0, 8, 0).Pad(aiBoxPad).Background(pal.terminal).Radius(radius.item).Children(func() {
			for _, line := range panel.approvalLay.summary {
				ui.Text(c, line).FontSize(aiMonoSize).MonoFace(state.terminalFontFace).Color(pal.text)
			}
		})
		if request.SessionLabel != "" {
			ui.Text(c, m.Target+": "+request.SessionLabel).MaxLines(0).FontSize(aiSmallSize).Color(pal.text).LineHeight(aiSmallLine).MarginSides(0, 0, 8, 0)
		}
		if len(assessment.Reasons) > 0 {
			ui.Column(c).MarginSides(0, 0, 4, 0).Children(func() {
				for index, reason := range assessment.Reasons {
					if index == aiMaxReasons {
						break
					}
					ui.Row(c).Items(ui.Start).Children(func() {
						ui.Text(c, "•").FontSize(aiApprovalSize).Color(pal.muted).LineHeight(aiApprovalLine).Width(12)
						ui.Paragraph(c, reason, 2).FontSize(aiApprovalSize).Weight(gfx.WeightRegular).Color(pal.muted).LineHeight(aiApprovalLine).Grow(1)
					})
				}
			})
		}
		if assessment.Safer != "" {
			ui.Column(c).MarginSides(0, 0, 4, 0).Children(func() {
				ui.Text(c, m.ApprovalSafer).FontSize(aiApprovalSize).Weight(gfx.WeightMedium).Color(pal.text).LineHeight(aiApprovalLine)
				ui.Paragraph(c, assessment.Safer, 2).FontSize(aiApprovalSize).Weight(gfx.WeightRegular).Color(pal.muted).LineHeight(aiApprovalLine)
			})
		}
		if request.NeedsText {
			if panel.confirmFor != approval || panel.confirmFormat != m.ConfirmFmt {
				panel.confirmFor, panel.confirmFormat = approval, m.ConfirmFmt
				panel.confirmText = fmt.Sprintf(m.ConfirmFmt, request.Confirm)
			}
			ui.Column(c).MarginSides(0, 0, 8, 0).Children(func() {
				ui.Text(c, panel.confirmText).FontSize(aiSmallSize).Weight(gfx.WeightMedium).Color(pal.text).LineHeight(18).MarginSides(0, 0, 2, 0)
				body.confirm = ui.Slot(c).Height(32)
			})
		}
		ui.Row(c).Height(aiButtonHeight).Gap(8).Items(ui.Center).Children(func() {
			// More questions behind this one.
			if more := len(ai.approvals) - 1; more > 0 {
				if panel.moreCount != more || panel.moreFormat != m.ApprovalMoreFmt {
					panel.moreCount, panel.moreFormat = more, m.ApprovalMoreFmt
					panel.moreText = fmt.Sprintf(m.ApprovalMoreFmt, more)
				}
				ui.Text(c, panel.moreText).FontSize(metrics.FontSizeS).Color(pal.muted).Grow(1)
			} else {
				ui.Spacer(c)
			}
			body.decline = ui.Slot(c).Size(92, aiButtonHeight)
			body.approve = ui.Slot(c).Size(92, aiButtonHeight)
		})
	})
}

// paintAIBottom paints into the slots of the bottom the parts that are not page
// elements, in the order the keyboard visits them: the pinned question's field
// and buttons. The message box, the chips and Send are declared by the page; this
// only keeps the geometry the wheel, the target selector and the menus read.
func (state *State) paintAIBottom(canvas *gfx.Canvas, body *aiBody) {
	ai, panel := &state.ai, &state.ai.panel
	m := state.messages.AIPanel
	theme := state.Theme()

	if len(ai.approvals) > 0 {
		approval := ai.approvals[0]
		request := &approval.request
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

	// The message box, with the model chip and Send inside it.
	box := body.box.Rect()
	panel.targetBounds = gfx.Rect{X: box.X + 8, Y: box.Y + 6, Width: min(200, max(box.Width-16, 0)), Height: 24}
	panel.composer = box
	panel.boxHeight = box.Height
}

// declareAIReasoningChip declares the pill in the message box that shows and controls
// the reasoning effort of the current model.
func (state *State) declareAIReasoningChip(c ui.C) ui.Box {
	pal, m := state.pal, state.messages.AIPanel
	effort := state.aiEffectiveReasoningEffort()
	label := state.aiReasoningEffortLabel(effort)
	displayText := m.ReasoningEffort + ": " + label

	fill := pal.surfaceSoft
	if canvas := c.Canvas(); canvas != nil && (canvas.Pressed() == hitAIReasoningChip || canvas.Hovered() == hitAIReasoningChip) || state.ai.panel.reasoningMenu {
		fill = pal.surfaceHot
	}
	return ui.Row(c).Height(aiSendSize-4).PadX(9).Gap(5).Shrink(1).MinWidth(50).Items(ui.Center).Background(fill).Radius((aiSendSize-4)/2).
		HitWith(hitAIReasoningChip, displayText, gfx.HitOptions{Role: gfx.RoleButton, Expanded: state.ai.panel.reasoningMenu, Hint: m.ReasoningEffortHint}).Children(func() {
		ui.Icon(c, lucideIcons[iconSliders]).Size(12, 12).Color(pal.muted).Shrink(0)
		ui.Text(c, displayText).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.muted).Ellipsis().Shrink(1)
		ui.Icon(c, lucideIcons[iconChevronDown]).Size(12, 12).Color(pal.muted).Shrink(0).MarginSides(0, 0, 0, 2)
		if state.ai.panel.reasoningMenu {
			state.declareAIReasoningMenu(c, aiChipMenuAnchor(c, hitAIReasoningChip, displayText))
		}
	})
}

// declareAIModePill declares the switch between Ask and Agent in the message box:
// a pill naming the mode, which a click turns into the other. Agent, the mode that
// runs commands, is the one painted in the accent.
func (state *State) declareAIModePill(c ui.C) {
	pal, m := state.pal, state.messages.AIPanel
	label, hint := m.ModeAsk, m.ModeHintAsk
	fill, foreground := pal.surfaceSoft, pal.muted
	if state.ai.mode != agent.ModeAsk {
		label, hint = m.ModeAgent, m.ModeHintAgent
		fill, foreground = pal.accentSoft, pal.accent
	}
	if canvas := c.Canvas(); canvas != nil {
		if canvas.Pressed() == hitAIMode {
			fill = pal.surfaceHot
		} else if canvas.Hovered() == hitAIMode && state.ai.mode == agent.ModeAsk {
			fill = pal.surfaceHot
		}
	}
	ui.Row(c).Height(aiSendSize-4).PadX(10).Shrink(0).Items(ui.Center).Background(fill).Radius((aiSendSize-4)/2).
		HitWith(hitAIMode, label, gfx.HitOptions{Role: gfx.RoleButton, Value: label, Hint: hint}).Children(func() {
		ui.Text(c, label).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(foreground)
	})
}

// declareAIModelChip declares the pill in the message box that names the model and
// opens the list of the others.
func (state *State) declareAIModelChip(c ui.C) ui.Box {
	pal := state.pal
	fill := pal.surfaceSoft
	if canvas := c.Canvas(); canvas != nil && (canvas.Pressed() == hitAIModelChip || canvas.Hovered() == hitAIModelChip) || state.ai.panel.modelMenu {
		fill = pal.surfaceHot
	}
	label := state.ai.settings.defaultModel
	accessible := fmt.Sprintf(state.messages.AIPanel.ModelChooseFmt, label)
	return ui.Row(c).Height(aiSendSize-4).PadX(9).Gap(5).Shrink(1).MinWidth(56).Items(ui.Center).Background(fill).Radius((aiSendSize-4)/2).
		HitWith(hitAIModelChip, accessible, gfx.HitOptions{Role: gfx.RoleButton, Expanded: state.ai.panel.modelMenu}).Children(func() {
		chipIcon, known := state.aiDefaultProviderIcon()
		if !known {
			chipIcon = lucideIcons[iconSliders]
		}
		ui.Icon(c, chipIcon).Size(13, 13).Color(pal.muted).Shrink(0)
		ui.Text(c, label).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.muted).Ellipsis().Shrink(1)
		ui.Icon(c, lucideIcons[iconChevronDown]).Size(12, 12).Color(pal.muted).Shrink(0).MarginSides(0, 0, 0, 2)
		if state.ai.panel.modelMenu {
			state.declareAIModelMenu(c, aiChipMenuAnchor(c, hitAIModelChip, accessible))
		}
	})
}

// declareAIAutoScopeChip declares the pill in the message box that names the
// approval scope of this conversation and opens the scopes to pick from. The chip
// carries the scope name alone: a prefix such as "Auto:" reads as a second,
// unexplained word, and the hint and the menu say what the name controls. Only
// Agent mode runs commands, so only it shows the chip; a request already running
// or waiting for an answer keeps the scope it started with, which is why locked
// disables the chip but still names it.
func (state *State) declareAIAutoScopeChip(c ui.C, locked bool) ui.Box {
	pal, m := state.pal, state.messages.AIPanel
	level := state.aiAutoUpTo()
	label := state.aiScopeName(level)
	foreground, icon := pal.muted, iconCircleCheck
	if level >= risk.High {
		foreground, icon = pal.warning, iconAlert
	}
	fill := pal.surfaceSoft
	if canvas := c.Canvas(); canvas != nil && (canvas.Pressed() == hitAIAutoScope || canvas.Hovered() == hitAIAutoScope) || state.ai.panel.autoMenu {
		fill = pal.surfaceHot
	}
	hint := m.AutoScopeHint
	if locked {
		hint = m.AutoScopeBusy
	}
	return ui.Row(c).Height(aiSendSize-4).PadX(9).Gap(4).Shrink(0).Items(ui.Center).Background(fill).Radius((aiSendSize-4)/2).
		HitWith(hitAIAutoScope, label, gfx.HitOptions{Role: gfx.RoleButton, Hint: hint, Disabled: locked, Expanded: state.ai.panel.autoMenu}).Children(func() {
		ui.Icon(c, lucideIcons[icon]).Size(12, 12).Color(foreground).Shrink(0)
		ui.Text(c, label).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(foreground).Ellipsis()
		ui.Icon(c, lucideIcons[iconChevronDown]).Size(12, 12).Color(pal.muted).Shrink(0).MarginSides(0, 0, 0, 2)
		if state.ai.panel.autoMenu {
			state.declareAIAutoScopeMenu(c, aiChipMenuAnchor(c, hitAIAutoScope, label))
		}
	})
}

// aiChipMenuAnchor is where a chip's menu hangs: an invisible, zero-size element
// at the chip's top left, two pixels up, carrying the chip's identity so the
// menu's keys reach it and closing gives the focus back to it. ui.Menu leaves
// four pixels between a menu and its anchor, so the menu opens six above the
// chip.
func aiChipMenuAnchor(c ui.C, id gfx.ID, label string) ui.Box {
	return ui.Slot(c).Absolute().Left(0).Top(-2).Size(0, 0).
		HitWith(id, label, gfx.HitOptions{Role: gfx.RoleButton, SkipTab: true})
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

// declareAIModelMenu declares the chip's menu, above it: the providers, and
// beside them the models of the one that is open. anchor is the chip itself, so
// the popup is placed after the layout of the box that opened it.
func (state *State) declareAIModelMenu(c ui.C, anchor ui.Box) {
	panel := &state.ai.panel
	providers := state.aiMenuProviders()
	if len(providers) == 0 {
		return
	}
	canvas, size := c.Canvas(), c.Theme().Metrics.FontSizeM
	textWidth := func(value string) float32 {
		if canvas == nil {
			return 0
		}
		return canvas.TextWidthWeight(value, size, gfx.WeightRegular)
	}
	var providersWidth float32
	for _, provider := range providers {
		providersWidth = max(providersWidth, textWidth(provider.name))
	}
	providersWidth += 24 // the mark before each name
	width := aiMenuWidth(providersWidth)
	// The menus are dialogs' peers: they must paint over a drawer's content.
	ui.PopupLayer(c, dialogLayer)
	panel.menusShown = true
	ui.Menu(c, anchor, &panel.modelMenu, func(menu *ui.MenuItems) {
		menu.Above()
		menu.WidthRange(width, width)
		for index := range providers {
			provider := &providers[index]
			var modelsWidth float32
			for _, model := range provider.models {
				modelsWidth = max(modelsWidth, textWidth(model.name))
			}
			modelsMenuWidth := aiMenuWidth(modelsWidth)
			menu.Sub(provider.name, func(to *ui.MenuItems) {
				to.WidthRange(modelsMenuWidth, modelsMenuWidth)
				for row := range provider.models {
					model := &provider.models[row]
					if to.Item(model.name).Chosen(model.current).Clicked() {
						state.aiChooseModel(index, row)
					}
				}
			}).Icon(provider.icon)
		}
	})
}

// declareAIAutoScopeMenu declares the approval chip's menu, above it: the scopes
// and what each lets run without asking. An item's second line carries that.
func (state *State) declareAIAutoScopeMenu(c ui.C, anchor ui.Box) {
	panel := &state.ai.panel
	if state.ai.mode == agent.ModeAsk {
		return
	}
	if _, hasModel := state.aiModelText(); !hasModel {
		return
	}
	canvas, theme := c.Canvas(), c.Theme()
	scopes := state.aiScopes()
	var width float32
	for _, scope := range scopes {
		item := components.ContextMenuItem{Label: scope.name, Description: scope.description, Kind: components.ContextMenuRadio}
		if canvas != nil {
			width = max(width, components.ContextMenuItemWidth(canvas, *theme, item))
		}
	}
	width = max(width, aiMenuContextWidth)
	level := state.aiAutoUpTo()
	// The menus are dialogs' peers: they must paint over a drawer's content.
	ui.PopupLayer(c, dialogLayer)
	panel.menusShown = true
	ui.Menu(c, anchor, &panel.autoMenu, func(menu *ui.MenuItems) {
		menu.Above()
		menu.WidthRange(width, width)
		for _, scope := range scopes {
			if menu.Item(scope.name).Description(scope.description).Chosen(level == scope.level).Clicked() {
				state.aiSetAutoUpTo(scope.level)
			}
		}
	})
}

// declareAIReasoningMenu declares the reasoning chip's menu, above it: the
// levels the current model supports.
func (state *State) declareAIReasoningMenu(c ui.C, anchor ui.Box) {
	panel := &state.ai.panel
	_, levels, ok := state.aiCurrentModelReasoning()
	if !ok || len(levels) == 0 {
		return
	}
	canvas, theme := c.Canvas(), c.Theme()
	var width float32
	for _, level := range levels {
		item := components.ContextMenuItem{Label: state.aiReasoningEffortLabel(level), Kind: components.ContextMenuRadio}
		if canvas != nil {
			width = max(width, components.ContextMenuItemWidth(canvas, *theme, item))
		}
	}
	width = max(width, aiMenuContextWidth)
	current := state.aiEffectiveReasoningEffort()
	// The menus are dialogs' peers: they must paint over a drawer's content.
	ui.PopupLayer(c, dialogLayer)
	panel.menusShown = true
	ui.Menu(c, anchor, &panel.reasoningMenu, func(menu *ui.MenuItems) {
		menu.Above()
		menu.WidthRange(width, width)
		for _, level := range levels {
			if menu.Item(state.aiReasoningEffortLabel(level)).Chosen(current == level).Clicked() {
				panel.reasoningEffort = level
			}
		}
	})
}

// aiChooseModel makes the n-th model of provider providerIndex the one that
// answers, switching it on if it was only fetched.
func (state *State) aiChooseModel(providerIndex, n int) {
	panel := &state.ai.panel
	panel.modelMenu = false
	providers := state.aiMenuProviders()
	if providerIndex < 0 || providerIndex >= len(providers) {
		return
	}
	provider := providers[providerIndex]
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
