package workbench

import (
	"fmt"
	"time"

	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
)

// The native window owns its loading state, including retry and cancellation.
// Reuse the terminal connection ring and its reduced-motion behavior.
func (w *sftpWindow) paintConnecting(canvas *ui.Canvas, size ui.Size) {
	state, s := w.owner, &w.session
	m, pal := state.messages, state.pal
	failed := s.failed() || s.phase == sshconn.PhaseClosed
	waiting := isPromptPhase(s.phase)
	title, detail := fmt.Sprintf(m.SFTPWindow.ConnectingFmt, s.Title), s.Address
	icon, tint := uint8(iconTransfer), pal.accent
	if waiting {
		detail = m.SFTPWindow.AuthenticationHint
	} else if s.phase == sshconn.PhaseAuthenticating {
		detail = m.ConnDetailAgent
	}
	if failed {
		title, icon, tint = m.HostStateFailed, iconAlert, pal.danger
		if s.err != nil {
			detail = s.err.Error()
		}
	}
	var badge flex.Box
	f := w.ui.frame.Begin(canvas, state.Theme(), ui.Rect{Width: size.Width, Height: size.Height})
	f.Column().Fill().Center().Pad(20).Children(func() {
		f.Column().Width(min(480, max(size.Width-40, 0))).Gap(12).Items(flex.Center).Children(func() {
			badge = f.Slot().Size(72, 72)
			f.Text(title).FontSize(18).Weight(ui.WeightSemibold).Color(pal.text).FillWidth().Ellipsis().LineHeight(26)
			f.Text(detail).FontSize(metrics.FontSizeS).Color(pal.muted).FillWidth().Ellipsis().LineHeight(22)
			f.Row().Gap(8).MarginSides(12, 0, 0, 0).Children(func() {
				f.Button(hitSFTPWindowConnectCancel, m.ConnClose).Outline()
				if failed {
					f.Button(hitSFTPWindowConnectRetry, m.ConnRetry)
				} else if waiting {
					f.Button(hitSFTPWindowAuthenticate, m.SFTPWindow.Authenticate)
				}
			})
		})
	})
	f.End()
	bounds := badge.Rect()
	cx, cy := bounds.X+bounds.Width/2, bounds.Y+bounds.Height/2
	canvas.Rounded(ui.Rect{X: cx - 25, Y: cy - 25, Width: 50, Height: 50}, 14, withAlpha(tint, .12))
	paintIcon(canvas, icon, ui.Rect{X: cx - 12, Y: cy - 12, Width: 24, Height: 24}, tint)
	if !failed {
		paintConnectionRing(canvas, cx, cy, 32, tint)
		if !waiting && !canvas.ReduceMotion() {
			canvas.RequestFrameIn(time.Second / 30)
		}
	}
}
