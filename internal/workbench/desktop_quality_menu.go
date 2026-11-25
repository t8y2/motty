package workbench

import (
	"github.com/vivaui/viva/ui"

	"motty/internal/desktop"
)

// The quality menu of a remote desktop's bar: the same choices as the host
// editor's Quality and bandwidth section, for the session that is open. They
// are saved with the host, so the next connection starts the same way. A VNC
// session takes them at once; RDP fixes them in its handshake, so a change asks
// to connect again, as the tab's menu does.

// setDesktopQuality changes the quality choices of a session's host, saves them
// and gives them to the session: VNC applies them to the running connection,
// anything else asks to reconnect. The resolution menu changes a host the same
// way.
func (state *State) setDesktopQuality(session *Session, change func(*Host)) {
	host := session.host
	change(&host)
	if host.ColorDepth == session.host.ColorDepth && host.PreferredEncoding == session.host.PreferredEncoding &&
		host.desktopEffects() == session.host.desktopEffects() &&
		host.DesktopWidth == session.host.DesktopWidth && host.DesktopHeight == session.host.DesktopHeight {
		return
	}
	if index := state.hostIndex(session.HostID); index >= 0 {
		state.replaceHost(index, state.hosts[index], hostWithQuality(state.hosts[index], host))
		state.save()
	} else {
		session.host = host
	}
	session.reusePolicy = reusePolicyFor(session.host)
	if session.desk != nil {
		if adjuster, ok := session.desk.session.(desktop.QualityAdjuster); ok {
			if err := adjuster.SetQuality(session.host.ColorDepth, session.host.PreferredEncoding); err != nil {
				state.showActionFailure(state.messages.Desktop.Quality, err, err.Error())
			}
			return
		}
	}
	if state.canReconnectLive(session) {
		state.askSessionLink(session, false)
	}
}

// hostWithQuality is saved with the quality choices of chosen: nothing else of
// the saved host is taken from the session's copy.
func hostWithQuality(saved, chosen Host) Host {
	saved.ColorDepth, saved.PreferredEncoding, saved.Effects = chosen.ColorDepth, chosen.PreferredEncoding, chosen.Effects
	saved.DesktopWidth, saved.DesktopHeight = chosen.DesktopWidth, chosen.DesktopHeight
	return saved
}

// declareDesktopQualityMenu declares the menu under the bar's quality button.
func (state *State) declareDesktopQualityMenu(c ui.C, session *Session, view *desktopView, anchor ui.Box) {
	if !view.qualityMenu {
		return
	}
	m := state.messages.Desktop
	host := session.host
	rdp := host.Protocol == protocolRDP
	depths := vncColorDepths[:]
	if rdp {
		depths = rdpColorDepths[:]
	}
	ui.PopupLayer(c, dialogLayer)
	ui.Menu(c, anchor, &view.qualityMenu, func(menu *ui.MenuItems) {
		menu.WidthRange(240, 240)
		if rdp {
			menu.Label(m.QualityReconnectNote)
		}
		menu.Label(m.ColorDepthField)
		for _, depth := range depths {
			if menu.Item(state.colorDepthLabel(depth)).Chosen(colorDepthOrDefault(host.ColorDepth) == depth).Clicked() {
				chosen := savedColorDepth(depth, depths[1:])
				c.After(func() { state.setDesktopQuality(session, func(host *Host) { host.ColorDepth = chosen }) })
			}
		}
		menu.Separator()
		if !rdp {
			menu.Label(m.EncodingPreferenceField)
			for _, encoding := range vncEncodings {
				if menu.Item(state.desktopEncodingLabel(encoding)).Chosen(host.PreferredEncoding == encoding).Clicked() {
					c.After(func() { state.setDesktopQuality(session, func(host *Host) { host.PreferredEncoding = encoding }) })
				}
			}
			return
		}
		menu.Label(m.QualityHeading)
		effects := host.desktopEffects()
		for index, effect := range effectChoices(m, &effects) {
			if menu.Item(effect.label).Checked(*effect.setting).Clicked() {
				c.After(func() {
					state.setDesktopQuality(session, func(host *Host) {
						next := host.desktopEffects()
						flipped := effectChoices(m, &next)[index].setting
						*flipped = !*flipped
						if next == desktop.DefaultEffects() {
							host.Effects = nil
						} else {
							host.Effects = &next
						}
					})
				})
			}
		}
	})
}
