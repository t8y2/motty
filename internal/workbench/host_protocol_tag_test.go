package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

// The hosts page marks every saved connection with the protocol its host form
// was set to, in both the list and the card grid, and the marks are told apart
// by colour without using the palette's red.
func TestHostProtocolTagsAppearInListAndGrid(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.hosts = []Host{
		{ID: 1, CatalogID: "protocol-ssh", Label: "alpha", Address: "alpha.example.com", User: "deploy", Protocol: protocolSSH},
		{ID: 2, CatalogID: "protocol-telnet", Label: "bravo", Address: "bravo.example.com", User: "deploy", Protocol: protocolTelnet},
		{ID: 3, CatalogID: "protocol-serial", Label: "charlie", Address: "/dev/ttyUSB0", Protocol: protocolSerial},
		{ID: 4, CatalogID: "protocol-vnc", Label: "delta", Address: "delta.example.com", User: "admin", Protocol: protocolVNC},
		{ID: 5, CatalogID: "protocol-rdp", Label: "echo", Address: "echo.example.com", User: "admin", Protocol: protocolRDP},
	}
	state.hostDataChanged()

	ansi := func(index int) gfx.Color {
		c := state.pal.ansi[index]
		return gfx.RGB(c[0], c[1], c[2])
	}
	want := map[string]gfx.Color{
		"SSH":    ansi(4),
		"Telnet": ansi(6),
		"Serial": ansi(2),
		"VNC":    ansi(5),
		"RDP":    ansi(3),
	}
	red := ansi(1)

	for _, view := range []struct {
		name string
		mode hostViewMode
	}{
		{name: "list", mode: hostViewList},
		{name: "grid", mode: hostViewGrid},
	} {
		t.Run(view.name, func(t *testing.T) {
			state.hostView = view.mode
			state.hostsScroll = 0
			var canvas gfx.Canvas
			state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})

			found := make(map[string][]gfx.Color)
			for _, text := range canvas.Texts() {
				if _, ok := want[text.Value]; ok {
					found[text.Value] = append(found[text.Value], text.Style.Color)
				}
			}
			for label, color := range want {
				colors := found[label]
				if len(colors) != 1 {
					t.Fatalf("%s protocol tag painted %d times, want once", label, len(colors))
				}
				if colors[0] != color {
					t.Fatalf("%s protocol tag color = %v, want %v", label, colors[0], color)
				}
				if color == red {
					t.Fatalf("%s protocol tag uses the palette's red", label)
				}
			}
		})
	}
}
