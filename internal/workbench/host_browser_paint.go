package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
)

func (state *State) paintHostBrowserPanel(canvas *gfx.Canvas, bounds gfx.Rect, session *Session) {
	if bounds.Width <= 0 || bounds.Height <= 0 || session == nil {
		return
	}
	state.contextRect = bounds
	state.paintHostBrowser(canvas, bounds, session)
}

func (state *State) getHostBrowserPage() *ui.Page {
	if state.hostBrowserPage == nil {
		state.hostBrowserPage = ui.NewPage(ui.PageConfig{
			Tree: &tree, Theme: &state.theme,
		})
	}
	return state.hostBrowserPage
}

func (state *State) getHostBrowserStatusPage() *ui.Page {
	if state.hostBrowserStatusPage == nil {
		state.hostBrowserStatusPage = ui.NewPage(ui.PageConfig{
			Tree: &tree, Theme: &state.theme,
		})
	}
	return state.hostBrowserStatusPage
}

func (state *State) paintHostBrowser(canvas *gfx.Canvas, bounds gfx.Rect, session *Session) {
	browser := state.hostBrowserFor(session)
	tab := browser.current()
	if tab == nil {
		return
	}
	m, pal := state.messages.HostBrowser, state.pal
	var address, back, forward, reload, goTo, external, closeButton, newTab, content ui.Box
	tabCloses := make([]ui.Box, len(browser.tabs))
	state.getHostBrowserPage().Draw(canvas, bounds, func(c ui.C) {
		ui.Column(c).Fill().Background(pal.workspace).Children(func() {
			ui.Row(c).Height(infoBarHeight).Shrink(0).PadSides(0, 8, 0, 8).Gap(4).Items(ui.Center).Children(func() {
				ui.Icon(c, lucideIcons[iconGlobe]).Size(16, 16).Color(pal.accent).Shrink(0).MarginSides(0, 3, 0, 0)
				ui.Row(c).Grow(1).Basis(0).MinWidth(0).Height(28).Gap(2).Items(ui.Center).Clip().Children(func() {
					for index, page := range browser.tabs {
						id := hostBrowserTabList.At(page.id)
						title := page.title(m.NewTab)
						active := index == browser.active
						fill, foreground := gfx.Color{}, pal.muted
						switch {
						case active:
							fill, foreground = pal.surface, pal.text
						case canvas.Hovered() == id || canvas.Hovered() == hostBrowserTabCloseList.At(page.id):
							fill, foreground = withAlpha(pal.surfaceHot, 0.7), pal.text
						}
						ui.Row(c).Height(26).Basis(160).MinWidth(64).Shrink(1).PadSides(0, 4, 0, 8).Gap(2).Items(ui.Center).
							Radius(radius.control).Background(fill).
							HitWith(id, title, gfx.HitOptions{Role: gfx.RoleTab, Selected: active}).Children(func() {
							ui.Text(c, title).FontSize(metrics.FontSizeS).Color(foreground).Grow(1).Basis(0).MinWidth(0).Ellipsis()
							tabCloses[index] = ui.Slot(c).Size(18, 18).Shrink(0)
						})
					}
				})
				newTab = ui.Slot(c).Size(24, 24).Shrink(0)
				closeButton = ui.Slot(c).Size(24, 24).Shrink(0)
			})
			ui.Row(c).Height(32).Shrink(0).PadSides(0, 6, 0, 6).Gap(3).Items(ui.Center).Children(func() {
				back = ui.Slot(c).Size(24, 24)
				forward = ui.Slot(c).Size(24, 24)
				reload = ui.Slot(c).Size(24, 24)
				external = ui.Slot(c).Size(24, 24)
			})
			ui.Row(c).Height(40).Shrink(0).PadSides(0, 6, 6, 6).Gap(5).Items(ui.Center).Children(func() {
				address = ui.Slot(c).Grow(1).MinWidth(80).Height(30)
				goTo = ui.Slot(c).Size(26, 26)
			})
			if problem := browser.problem(tab); problem != "" {
				ui.Paragraph(c, problem, 3).FontSize(metrics.FontSizeS).Color(pal.danger).PadSides(0, 8, 0, 8).Shrink(0)
			}
			content = ui.Slot(c).Grow(1).Basis(0).Background(pal.surface)
		})
	})

	for index, page := range browser.tabs {
		state.paintIconButton(canvas, hostBrowserTabCloseList.At(page.id), iconClose, tabCloses[index].Rect(), m.CloseTab, false, iconButtonQuiet, pal.workspace)
	}
	connected := session.phase == sshconn.PhaseConnected
	state.paintIconButton(canvas, hitHostBrowserNewTab, iconPlus, newTab.Rect(), m.NewTab, false, iconButtonQuiet, pal.workspace)
	state.paintIconButton(canvas, hitHostBrowserClose, iconClose, closeButton.Rect(), m.Close, false, iconButtonQuiet, pal.workspace)
	state.paintIconButton(canvas, hitHostBrowserBack, iconChevronLeft, back.Rect(), m.Back, tab.view == nil, iconButtonQuiet, pal.workspace)
	state.paintIconButton(canvas, hitHostBrowserForward, iconChevronRight, forward.Rect(), m.Forward, tab.view == nil, iconButtonQuiet, pal.workspace)
	state.paintIconButton(canvas, hitHostBrowserReload, iconRefresh, reload.Rect(), m.Reload, tab.view == nil, iconButtonQuiet, pal.workspace)
	state.paintIconButton(canvas, hitHostBrowserExternal, iconLink, external.Rect(), m.OpenExternal, !connected, iconButtonQuiet, pal.workspace)
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
	var actionBox ui.Box
	state.getHostBrowserStatusPage().Draw(canvas, content.Rect(), func(c ui.C) {
		ui.Column(c).Fill().Center().Pad(16).Gap(12).Children(func() {
			ui.Icon(c, lucideIcons[iconGlobe]).Size(28, 28).Color(pal.muted)
			ui.Text(c, status).FontSize(metrics.FontSizeS).Color(pal.muted).TextCenter()
			if failed {
				actionBox = ui.Slot(c).Width(200).Height(32)
			}
		})
	})
	if failed {
		actionRect := actionBox.Rect()
		if actionRect.Width == 0 {
			actionRect = gfx.Rect{
				X:      content.Rect().X + (content.Rect().Width-200)/2,
				Y:      content.Rect().Y + content.Rect().Height/2,
				Width:  200,
				Height: 32,
			}
		}
		if tab.err == m.Unsupported {
			components.BeginButton(canvas, state.Theme(), hitHostBrowserLaunchExternal, actionRect).Secondary(m.OpenInExternalBrowser)
		} else {
			components.BeginButton(canvas, state.Theme(), hitHostBrowserRetry, actionRect).Secondary(m.Retry)
		}
	}
}
