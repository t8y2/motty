package workbench

import (
	"fmt"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
)

// The native window owns its loading state, including retry and cancellation.
// Reuse the terminal connection ring and its reduced-motion behavior.

// paintConnectingDetails paints the ring around the badge the connecting view
// declared. The window's page declares the view, so the window calls it after
// the page has painted.
func (w *sftpWindow) paintConnectingDetails(canvas *gfx.Canvas) {
	state, s := w.owner, &w.session
	pal := state.pal
	failed := s.failed() || s.phase == sshconn.PhaseClosed
	waiting := isPromptPhase(s.phase)
	bounds := w.ui.connect.badge.Rect()
	cx, cy := bounds.X+bounds.Width/2, bounds.Y+bounds.Height/2
	tint := pal.accent
	icon := uint8(iconSFTPTransfer)
	if failed {
		tint = pal.danger
		icon = iconAlert
	}
	canvas.Rounded(gfx.Rect{X: cx - 25, Y: cy - 25, Width: 50, Height: 50}, 14, withAlpha(tint, .12))
	paintIcon(canvas, icon, gfx.Rect{X: cx - 12, Y: cy - 12, Width: 24, Height: 24}, tint)
	if !failed {
		paintConnectionRing(canvas, cx, cy, 32, tint)
		if !waiting && !canvas.ReduceMotion() {
			canvas.RequestFrameIn(time.Second / 30)
		}
	}
}

// declareConnecting declares the window's loading page: what it is connecting
// to, what happened, and the one action it can offer.
func (w *sftpWindow) declareConnecting(c ui.C) {
	state, s := w.owner, &w.session
	m, pal := state.messages, state.pal
	size := w.ui.size
	failed := s.failed() || s.phase == sshconn.PhaseClosed
	waiting := isPromptPhase(s.phase)
	title, detail := fmt.Sprintf(m.SFTPWindow.ConnectingFmt, s.Title), s.Address
	if waiting {
		detail = m.SFTPWindow.AuthenticationHint
	}
	if failed {
		title = m.HostStateFailed
		if s.err != nil {
			detail = s.err.Error()
		}
	}
	ui.Column(c).Fill().Center().Pad(20).Children(func() {
		ui.Column(c).Width(min(480, max(size.Width-40, 0))).Gap(12).Items(ui.Center).Children(func() {
			w.ui.connect.badge = ui.Slot(c).Size(72, 72)
			ui.Text(c, title).FontSize(18).Weight(gfx.WeightSemibold).Color(pal.text).FillWidth().Ellipsis().TextCenter().LineHeight(26)
			ui.Text(c, detail).FontSize(metrics.FontSizeS).Color(pal.muted).FillWidth().Ellipsis().TextCenter().LineHeight(22)
			ui.Row(c).Gap(8).MarginSides(12, 0, 0, 0).Children(func() {
				closeButton := ui.Button(c, m.ConnClose).Outline()
				if closeButton.Clicked() {
					w.closeHostNow()
				}
				if failed {
					retry := ui.Button(c, m.ConnRetry)
					if retry.Clicked() && (w.session.failed() || w.session.phase == sshconn.PhaseClosed) {
						w.connect()
					}
				} else if waiting {
					authenticate := ui.Button(c, m.SFTPWindow.Authenticate)
					if authenticate.Clicked() {
						w.owner.raiseSFTPAuthentication(&w.session)
					}
				}
			})
		})
	})
}
