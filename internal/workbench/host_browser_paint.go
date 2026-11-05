package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
)

func (state *State) paintHostBrowserPanel(canvas *ui.Canvas, bounds ui.Rect, session *Session) {
	if bounds.Width <= 0 || bounds.Height <= 0 || session == nil {
		return
	}
	state.contextRect = bounds
	state.hostBrowserCanvas.BeginEmbedded(canvas, contextMount)
	state.paintHostBrowser(&state.hostBrowserCanvas, ui.Rect{Width: bounds.Width, Height: bounds.Height}, session)
	state.hostBrowserCanvas.EndFrame()
	canvas.Clip(bounds)
	canvas.Embed(&state.hostBrowserCanvas, ui.Point{X: bounds.X, Y: bounds.Y}, contextMount)
	canvas.Unclip()
	adoptEmbeddedFocus(canvas, &state.hostBrowserCanvas, contextMount)
}

func (state *State) paintHostBrowser(canvas *ui.Canvas, bounds ui.Rect, session *Session) {
	browser := state.hostBrowserFor(session)
	m, pal := state.messages.HostBrowser, state.pal
	var address, back, forward, reload, goTo, external, closeButton, content flex.Box
	f := state.hostBrowserLayout.Begin(canvas, state.Theme(), bounds)
	f.Column().Fill().Background(pal.workspace).Children(func() {
		f.Row().Height(infoBarHeight).Shrink(0).PadSides(0, 8, 0, 8).Gap(7).Items(flex.Center).Children(func() {
			f.Icon(lucideIcons[iconGlobe]).Size(16, 16).Color(pal.accent)
			f.Text(m.Title).FontSize(metrics.FontSizeM).Weight(ui.WeightSemibold).Color(pal.text).Grow(1).Ellipsis()
			closeButton = f.Slot().Size(24, 24)
		})
		f.Row().Height(32).Shrink(0).PadSides(0, 6, 0, 6).Gap(3).Items(flex.Center).Children(func() {
			back = f.Slot().Size(24, 24)
			forward = f.Slot().Size(24, 24)
			reload = f.Slot().Size(24, 24)
			external = f.Slot().Size(24, 24)
		})
		f.Row().Height(40).Shrink(0).PadSides(0, 6, 6, 6).Gap(5).Items(flex.Center).Children(func() {
			address = f.Slot().Grow(1).MinWidth(80).Height(30)
			goTo = f.Slot().Size(26, 26)
		})
		if browser.err != "" {
			f.Paragraph(browser.err, 3).FontSize(metrics.FontSizeS).Color(pal.danger).PadSides(0, 8, 0, 8).Shrink(0)
		}
		content = f.Slot().Grow(1).Basis(0).Background(pal.surface)
	})
	f.End()

	state.paintIconButton(canvas, hitHostBrowserClose, iconClose, closeButton.Rect(), m.Close, false, iconButtonQuiet, pal.workspace)
	state.paintIconButton(canvas, hitHostBrowserBack, iconChevronLeft, back.Rect(), m.Back, browser.view == nil, iconButtonQuiet, pal.workspace)
	state.paintIconButton(canvas, hitHostBrowserForward, iconChevronRight, forward.Rect(), m.Forward, browser.view == nil, iconButtonQuiet, pal.workspace)
	state.paintIconButton(canvas, hitHostBrowserReload, iconRefresh, reload.Rect(), m.Reload, browser.view == nil, iconButtonQuiet, pal.workspace)
	state.paintIconButton(canvas, hitHostBrowserExternal, iconLink, external.Rect(), m.OpenExternal, false, iconButtonQuiet, pal.workspace)
	state.paintIconButton(canvas, hitHostBrowserGo, iconChevronRight, goTo.Rect(), m.Go, false, iconButtonActive, pal.workspace)
	value := browser.address.String()
	components.PaintInputGroup(canvas, state.Theme(), address.Rect(), components.InputGroupSnapshot{
		ID: hitHostBrowserAddress, Label: m.Address, Value: value, Placeholder: m.AddressPlaceholder,
		NativeEditing: canvas.Focused() == hitHostBrowserAddress && value != "",
		InlineStart:   components.InputGroupSlot{Kind: components.InputGroupSlotIcon, Icon: lucideIcons[iconGlobe]},
	})
	if browser.focusAddress && canvas.Focused() != hitHostBrowserAddress {
		canvas.RequestFocus(hitHostBrowserAddress)
		browser.focusAddress = false
	}
	if browser.view != nil && browser.err == "" && session.phase == sshconn.PhaseConnected {
		canvas.NativeView(browser.view.ID(), content.Rect())
		return
	}
	status := m.Connecting
	if session.phase != sshconn.PhaseConnected {
		status = m.Disconnected
	} else if browser.err != "" {
		status = m.Unavailable
	}
	f = state.hostBrowserStatusLayout.Begin(canvas, state.Theme(), content.Rect())
	f.Column().Fill().Center().Pad(16).Gap(12).Children(func() {
		f.Icon(lucideIcons[iconGlobe]).Size(28, 28).Color(pal.muted)
		f.Text(status).FontSize(metrics.FontSizeS).Color(pal.muted).TextCenter()
		if browser.err != "" {
			f.Button(hitHostBrowserRetry, m.Retry)
		}
	})
	f.End()
}
