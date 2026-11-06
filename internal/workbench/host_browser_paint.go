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
	tab := browser.current()
	if tab == nil {
		return
	}
	m, pal := state.messages.HostBrowser, state.pal
	var address, back, forward, reload, goTo, external, closeButton, newTab, content flex.Box
	tabCloses := make([]flex.Box, len(browser.tabs))
	f := state.hostBrowserLayout.Begin(canvas, state.Theme(), bounds)
	f.Column().Fill().Background(pal.workspace).Children(func() {
		f.Row().Height(infoBarHeight).Shrink(0).PadSides(0, 8, 0, 8).Gap(4).Items(flex.Center).Children(func() {
			f.Icon(lucideIcons[iconGlobe]).Size(16, 16).Color(pal.accent).Shrink(0).MarginSides(0, 3, 0, 0)
			f.Row().Grow(1).Basis(0).MinWidth(0).Height(28).Gap(2).Items(flex.Center).Clip().Children(func() {
				for index, page := range browser.tabs {
					id := hostBrowserTabList.At(page.id)
					title := page.title(m.NewTab)
					active := index == browser.active
					fill, foreground := ui.Color{}, pal.muted
					switch {
					case active:
						fill, foreground = pal.surface, pal.text
					case canvas.Hovered() == id || canvas.Hovered() == hostBrowserTabCloseList.At(page.id):
						fill, foreground = withAlpha(pal.surfaceHot, 0.7), pal.text
					}
					f.Row().Height(26).Basis(160).MinWidth(64).Shrink(1).PadSides(0, 4, 0, 8).Gap(2).Items(flex.Center).
						Radius(radius.control).Background(fill).
						HitWith(id, title, ui.HitOptions{Role: ui.RoleTab, Selected: active}).Children(func() {
						f.Text(title).FontSize(metrics.FontSizeS).Color(foreground).Grow(1).Basis(0).MinWidth(0).Ellipsis()
						tabCloses[index] = f.Slot().Size(18, 18).Shrink(0)
					})
				}
			})
			newTab = f.Slot().Size(24, 24).Shrink(0)
			closeButton = f.Slot().Size(24, 24).Shrink(0)
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
		if problem := browser.problem(tab); problem != "" {
			f.Paragraph(problem, 3).FontSize(metrics.FontSizeS).Color(pal.danger).PadSides(0, 8, 0, 8).Shrink(0)
		}
		content = f.Slot().Grow(1).Basis(0).Background(pal.surface)
	})
	f.End()

	for index, page := range browser.tabs {
		state.paintIconButton(canvas, hostBrowserTabCloseList.At(page.id), iconClose, tabCloses[index].Rect(), m.CloseTab, false, iconButtonQuiet, pal.workspace)
	}
	state.paintIconButton(canvas, hitHostBrowserNewTab, iconPlus, newTab.Rect(), m.NewTab, false, iconButtonQuiet, pal.workspace)
	state.paintIconButton(canvas, hitHostBrowserClose, iconClose, closeButton.Rect(), m.Close, false, iconButtonQuiet, pal.workspace)
	state.paintIconButton(canvas, hitHostBrowserBack, iconChevronLeft, back.Rect(), m.Back, tab.view == nil, iconButtonQuiet, pal.workspace)
	state.paintIconButton(canvas, hitHostBrowserForward, iconChevronRight, forward.Rect(), m.Forward, tab.view == nil, iconButtonQuiet, pal.workspace)
	state.paintIconButton(canvas, hitHostBrowserReload, iconRefresh, reload.Rect(), m.Reload, tab.view == nil, iconButtonQuiet, pal.workspace)
	state.paintIconButton(canvas, hitHostBrowserExternal, iconLink, external.Rect(), m.OpenExternal, tab.url == "", iconButtonQuiet, pal.workspace)
	state.paintIconButton(canvas, hitHostBrowserGo, iconChevronRight, goTo.Rect(), m.Go, false, iconButtonActive, pal.workspace)
	value := tab.address.String()
	components.PaintInputGroup(canvas, state.Theme(), address.Rect(), components.InputGroupSnapshot{
		ID: hitHostBrowserAddress, Label: m.Address, Value: value, Placeholder: m.AddressPlaceholder,
		NativeEditing: canvas.Focused() == hitHostBrowserAddress && value != "",
		InlineStart:   components.InputGroupSlot{Kind: components.InputGroupSlotIcon, Icon: lucideIcons[iconGlobe]},
	})
	if browser.focusAddress && canvas.Focused() != hitHostBrowserAddress {
		canvas.RequestFocus(hitHostBrowserAddress)
		browser.focusAddress = false
	}
	connected := session.phase == sshconn.PhaseConnected
	if tab.view != nil && tab.err == "" && browser.err == "" && connected {
		canvas.NativeView(tab.view.ID(), content.Rect())
		return
	}
	status, failed := m.BlankPage, false
	switch {
	case !connected:
		status = m.Disconnected
	case browser.err != "" || (tab.err != "" && tab.url != ""):
		status, failed = m.Unavailable, true
	case browser.tunnel == nil || tab.url != "":
		status = m.Connecting
	}
	f = state.hostBrowserStatusLayout.Begin(canvas, state.Theme(), content.Rect())
	f.Column().Fill().Center().Pad(16).Gap(12).Children(func() {
		f.Icon(lucideIcons[iconGlobe]).Size(28, 28).Color(pal.muted)
		f.Text(status).FontSize(metrics.FontSizeS).Color(pal.muted).TextCenter()
		if failed {
			f.Button(hitHostBrowserRetry, m.Retry)
		}
	})
	f.End()
}
