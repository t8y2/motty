package workbench

import (
	"fmt"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

const inputNoticeHeight = float32(36)

// declareTerminalInputNotice declares the locale notice in the terminal pane's
// row: the words that explain the problem, the fix button and the dismiss
// button. The page routes their presses; the notice is not painted by hand.
func (state *State) declareTerminalInputNotice(c ui.C, session *Session, width float32) {
	m := state.messages.TerminalInput
	label := m.LocaleWarning
	ready := state.inputLocaleRepairReady(session)
	if !ready {
		label = m.LocaleBusy
	}
	if session.term.inputGuard.repairing {
		label = m.LocaleRepairing
	}
	if session.term.inputGuard.environment.Locale == "" {
		label = m.LocaleUnavailable
	}
	pal := state.pal
	buttonWidth := min(float32(108), width/3)
	row := ui.Row(c).Height(inputNoticeHeight).FillWidth().Shrink(0).Background(pal.surface).Items(ui.Center)
	row.Children(func() {
		ui.Row(c).Grow(1).Height(inputNoticeHeight).MarginSides(0, 4, 0, 10).Items(ui.Start).
			HitWith(hitInputNotice, label, gfx.HitOptions{Role: gfx.RoleStatus, Hint: m.LocaleWarning, SkipTab: true}).
			Children(func() {
				ui.Text(c, label).FontSize(metrics.FontSizeS).Color(pal.warning).LineHeight(20).MarginTop(10).Ellipsis()
			})
		ui.Row(c).Width(buttonWidth).Height(inputNoticeHeight).Shrink(0).Items(ui.Center).Justify(ui.End).
			MarginSides(0, 5, 0, 0).Children(func() {
			ui.Button(c, m.LocaleFix).XSmall().Outline().Hit(hitInputLocaleFix, m.LocaleFix).Disabled(!ready)
		})
		state.declareIconButton(c, hitInputLocaleDismiss, iconClose, m.LocaleDismiss, false, iconButtonQuiet).
			Size(24, 24).MarginSides(0, 3, 0, 0)
	})
}

var hitInputNotice = tree.ID(inputGuardHits, "notice")

func (state *State) declareTerminalInputConfirm(c ui.C) {
	m, ask := state.messages.TerminalInput, state.inputConfirmation
	title, body, detail, confirm := m.Title, m.Description, ask.detail, m.Replace
	cancel := m.Cancel
	if ask.locale != "" {
		title, body, detail, confirm = m.LocaleTitle, fmt.Sprintf(m.LocaleBodyFmt, ask.locale), ask.locale, m.LocaleConfirm
		cancel = state.messages.Cancel
	}
	if ask.error != "" {
		body = ask.error
	}
	viewport := dialogViewport(c)
	width := min(float32(560), max(float32(260), viewport.Width-24))
	lines := max(2, wrappedLines(c.Canvas(), body, metrics.FontSizeM, width-64))
	height := dialogContentHeight(c.Canvas(), *c.Theme(), viewport, width, "", float32(lines)*22+52)
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: title, Width: width, Height: height, Layer: dialogLayer,
		Scope: dialogScope, DismissID: hitInputDismiss, CloseID: hitInputClose, InitialFocusID: hitInputCancel,
	})
	dialog.Body(func() {
		ui.Column(c).Fill().Gap(10).Children(func() {
			ui.Paragraph(c, body, lines).FontSize(metrics.FontSizeM).Color(state.pal.muted).LineHeight(22)
			ui.Row(c).Height(38).PadX(10).Items(ui.Center).Background(state.pal.surface).Border(1, state.pal.line).Radius(radius.control).Children(func() {
				ui.Text(c, detail).FontSize(metrics.FontSizeM).Color(state.pal.text).Ellipsis().Grow(1)
			})
		})
	})
	dialog.Footer(func() {
		actions := []components.ButtonAction{{ID: hitInputCancel, Label: cancel, Variant: components.ButtonSecondary}}
		if ask.locale == "" {
			actions = append(actions, components.ButtonAction{ID: hitInputKeep, Label: m.Keep, Variant: components.ButtonOutline})
		}
		actions = append(actions, components.ButtonAction{ID: hitInputReplace, Label: confirm})
		state.dialogActionFooter(c, actions...)
	})
}
