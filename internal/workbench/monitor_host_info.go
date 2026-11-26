package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"motty/internal/sshconn"
)

func (state *State) monitorCurrentSSHUser(session *Session) string {
	if session == nil || session.isLocal() || !session.host.isSSH() || session.phase != sshconn.PhaseConnected || session.role != sshconn.EndpointTarget || state.hostPrivacy.userHidden() {
		return ""
	}
	if session.stats != nil {
		panel := &session.stats.panel
		if panel.haveDetails && !panel.detailsFailed && panel.data.LoginsAvailable {
			for _, login := range panel.data.Logins {
				if login.User == session.endpoint.User {
					return ""
				}
			}
		}
	}
	return session.endpoint.User
}

// Use the configured name and address, and the account of the target connection.
// While connecting through a jump host, its endpoint must not name this host.
func (state *State) monitorHostInfo(session *Session) [3]string {
	user := state.login(session.host)
	if !session.isLocal() && session.host.isSSH() && session.role == sshconn.EndpointTarget && session.endpoint.User != "" {
		user = session.endpoint.User
	}
	address := session.host.Address
	if distro := session.wslDistro(); distro != "" && session.HostID != localHostID {
		address = distro
	}
	fields := [3]string{session.host.Label, address, user}
	for i := range fields {
		if fields[i] == "" {
			fields[i] = "—"
		}
	}
	return fields
}

func (state *State) paintHostInfoText(canvas *gfx.Canvas, b gfx.Rect, session *Session) {
	if b.Width <= 0 || b.Height <= 0 || session == nil {
		return
	}
	colors := state.monitorColors()
	fields := state.monitorHostInfo(session)
	ids := [3]gfx.ID{hitMonitorHostName, hitMonitorHostAddress, hitMonitorHostUser}
	hidden := [3]bool{state.hostPrivacy.nameHidden(), state.hostPrivacy.addressHidden(), state.hostPrivacy.userHidden()}
	m := state.messages.Tools
	labels := [3]string{m.HostPrivacyName, m.HostPrivacyAddress, m.HostPrivacyUser}
	style := gfx.TextStyle{Size: 11, Color: colors.muted}
	const separator = " · "
	separatorWidth := canvas.TextWidthStyle(separator, style)
	available := max(b.Width-2*separatorWidth, 0)
	var widths [3]float32
	for i, value := range fields {
		widths[i] = canvas.TextWidthStyle(value, style) + 1
		if hidden[i] {
			widths[i] += hostPrivacyBlurPadding
		} else if i == 1 && session.host.Address != "" {
			widths[i] += 8 // horizontal padding for the copy button
		}
		if widths[i] > b.Width {
			widths[i] = b.Width
		}
	}
	if available <= 0 {
		widths = [3]float32{}
	} else if widths[0]+widths[1]+widths[2] > available {
		// Keep the address and account readable before assigning space to the name.
		address := min(widths[1], available*.5)
		user := min(widths[2], available*.25)
		name := min(widths[0], max(available-address-user, 0))
		spare := max(available-name-address-user, 0)
		address += min(max(widths[1]-address, 0), spare)
		user += min(max(widths[2]-user, 0), max(available-name-address-user, 0))
		widths = [3]float32{name, address, user}
		if name <= 0 {
			widths = [3]float32{}
		}
	}
	for i, value := range fields {
		hitLabel := value
		bounds := gfx.Rect{X: b.X, Y: b.Y, Width: widths[i], Height: b.Height}
		if bounds.Width > 0 {
			if hidden[i] {
				state.paintHostPrivacyMask(canvas, bounds, elide(canvas, value, style.Size, style.Weight, max(bounds.Width-hostPrivacyBlurPadding, 0)), style)
				canvas.HitWithOptions(ids[i], bounds, labels[i], gfx.HitOptions{Role: gfx.RoleStatus, SkipFocus: true})
			} else {
				textBounds, color := bounds, style.Color
				hit := gfx.HitOptions{Role: gfx.RoleStatus, SkipFocus: true}
				if i == 1 && session.host.Address != "" {
					background := colors.surface
					if canvas.Hovered() == ids[i] || canvas.Pressed() == ids[i] {
						background = state.pal.surfaceSoft
						canvas.Rounded(bounds, cornerPx(4), background)
					}
					if canvas.FocusVisible() == ids[i] {
						canvas.RoundedBorder(bounds, cornerPx(4), 1, colors.cpu, background)
					}
					textBounds = bounds.Inset(gfx.Insets{Left: 4, Right: 4})
					color = colors.cpu
					hit = gfx.HitOptions{Role: gfx.RoleButton, Cursor: gfx.PointerCursorHand, Value: value}
					hitLabel = state.messages.Monitor.AddressCopy
				}
				monitorLabel(canvas, value, textBounds, style.Size, color, style.Weight)
				canvas.HitWithOptions(ids[i], bounds, hitLabel, hit)
			}
		}
		b.X += widths[i]
		if i < len(fields)-1 && widths[i] > 0 && widths[i+1] > 0 {
			monitorLabel(canvas, separator, gfx.Rect{X: b.X, Y: b.Y, Width: separatorWidth, Height: b.Height}, style.Size, style.Color, style.Weight)
			b.X += separatorWidth
		}
	}
}

func (state *State) copyMonitorHostAddress(session *Session) {
	if !state.monitorShown(state.deck.ActiveWorkspace()) || state.hostPrivacy.addressHidden() || session.host.Address == "" {
		return
	}
	m := state.messages.Monitor
	if state.options.SetClipboardText == nil {
		state.fileNotices.show(m.AddressCopyFailed, "", components.ToastError)
		return
	}
	if err := state.options.SetClipboardText(session.host.Address); err != nil {
		state.fileNotices.show(m.AddressCopyFailed, err.Error(), components.ToastError)
		return
	}
	state.fileNotices.show(m.AddressCopied, "", components.ToastSuccess)
}
