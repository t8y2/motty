package workbench

import (
	"testing"

	"motty/internal/portforward"
)

func init() {
	extraPaintScenes = append(extraPaintScenes,
		plainScene("g2-forward-local", func(t *testing.T, s *State) { s.openForwardDialog() }),
		plainScene("g2-forward-local-relay-error", func(t *testing.T, s *State) {
			s.openForwardDialog()
			s.forwardEditor.viaShell = true
			s.forwardDialogError = "Enter a valid port."
		}),
		plainScene("g2-forward-socks5", func(t *testing.T, s *State) {
			s.openForwardDialog()
			s.forwardEditor.mode = portforward.ModeSOCKS5
		}),
		plainScene("g2-forward-remote", func(t *testing.T, s *State) {
			s.openForwardDialog()
			s.forwardEditor.mode = portforward.ModeRemote
		}),
		plainScene("g2-proxy-route", func(t *testing.T, s *State) { s.openProxyDialog() }),
		plainScene("g2-proxy-route-relay-error", func(t *testing.T, s *State) {
			s.openProxyDialog()
			s.proxyEditor.viaShell = true
			s.proxyDialogError = "Pick a jump host."
		}),
	)
}
