package workbench

import (
	"fmt"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

const inputNoticeHeight = float32(36)

func (state *State) paintTerminalInputNotice(canvas *gfx.Canvas, session *Session, bounds gfx.Rect) {
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
	canvas.Fill(bounds, state.pal.surface)
	buttonWidth := min(float32(108), bounds.Width/3)
	textBounds := gfx.Rect{X: bounds.X + 10, Y: bounds.Y, Width: max(0, bounds.Width-buttonWidth-46), Height: bounds.Height}
	canvas.HitWithOptions(hitInputNotice, textBounds, label, gfx.HitOptions{Role: gfx.RoleStatus, Hint: m.LocaleWarning, SkipTab: true})
	canvas.Clip(textBounds)
	canvas.Label(label, gfx.Rect{X: textBounds.X, Y: textBounds.Y + 10, Width: textBounds.Width, Height: 20}, gfx.TextStyle{Size: metrics.FontSizeS, Color: state.pal.warning})
	canvas.Unclip()
	components.BeginButtonRow(canvas, state.Theme(), gfx.Rect{X: bounds.X + bounds.Width - buttonWidth - 32, Y: bounds.Y + 5, Width: buttonWidth, Height: 26}).Size(components.ButtonSizeXSmall).Paint(
		components.ButtonAction{ID: hitInputLocaleFix, Label: m.LocaleFix, Variant: components.ButtonOutline, Disabled: !ready},
	)
	state.paintIconButton(canvas, hitInputLocaleDismiss, iconClose, gfx.Rect{X: bounds.X + bounds.Width - 27, Y: bounds.Y + 6, Width: 24, Height: 24}, m.LocaleDismiss, false, iconButtonQuiet, state.pal.surface)
}

var hitInputNotice = tree.ID(inputGuardHits, "notice")

func (state *State) paintTerminalInputConfirm(canvas *gfx.Canvas, viewport gfx.Rect) {
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
	width := min(float32(560), max(float32(260), viewport.Width-24))
	lines := max(2, wrappedLines(canvas, body, metrics.FontSizeM, width-64))
	dialog := components.BeginDialog(canvas, state.Theme(), viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope, DismissID: hitInputDismiss, CloseID: hitInputClose, InitialFocusID: hitInputCancel,
		ContentSize: gfx.Size{Width: width}, FitBody: true, BodyHeight: float32(lines)*22 + 52,
	})
	layout := dialog.Begin(title, "")
	f := state.partLayout.Begin(canvas, state.Theme(), layout.Body)
	f.Column().Fill().Gap(10).Children(func() {
		f.Paragraph(body, lines).FontSize(metrics.FontSizeM).Color(state.pal.muted).LineHeight(22)
		f.Row().Height(38).PadX(10).Items(ui.Center).Background(state.pal.surface).Border(1, state.pal.line).Radius(radius.control).Children(func() {
			f.Text(detail).FontSize(metrics.FontSizeM).Color(state.pal.text).Ellipsis().Grow(1)
		})
	})
	f.End()
	actions := []components.ButtonAction{{ID: hitInputCancel, Label: cancel, Variant: components.ButtonSecondary}}
	if ask.locale == "" {
		actions = append(actions, components.ButtonAction{ID: hitInputKeep, Label: m.Keep, Variant: components.ButtonOutline})
	}
	actions = append(actions, components.ButtonAction{ID: hitInputReplace, Label: confirm})
	dialog.Actions(layout, actions...)
	dialog.End()
}
