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

// A user tag whose pill cannot fit the room the protocol tag leaves is left
// out, not painted over the status column or the card's checkbox. Measuring
// used to clamp an oversized pill to the room, so it passed the fit check and
// the painter still drew it whole.
func TestTooWideHostTagLeftOutBesideProtocolTag(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.hosts = []Host{
		{ID: 1, CatalogID: "wide-tag", Label: "alpha", Address: "a.io", User: "ops", Protocol: protocolRDP,
			Tags: "abcdefghijklmnopqrstuvwxyz012345"},
		{ID: 2, CatalogID: "fitting-tag", Label: "bravo", Address: "b.io", User: "ci", Protocol: protocolSSH,
			Tags: "web"},
	}
	state.hostDataChanged()

	for _, view := range []struct {
		name string
		mode hostViewMode
		// The widest tag a host can take (32 runes) stays over the tag room at
		// these widths while a one-word tag still fits whole: the list's tag
		// column collapses below about 1100 and its 18% share grows past such a
		// pill above about 1400, the grid's cards are narrowest around 960.
		width  float32
		height float32
	}{
		{name: "list", mode: hostViewList, width: 1180, height: 700},
		{name: "grid", mode: hostViewGrid, width: 960, height: 700},
	} {
		t.Run(view.name, func(t *testing.T) {
			state.hostView = view.mode
			state.hostsScroll = 0
			var canvas gfx.Canvas
			state.Paint(&canvas, gfx.Size{Width: view.width, Height: view.height})

			painted := make(map[string]bool)
			for _, text := range canvas.Texts() {
				painted[text.Value] = true
			}
			if painted[state.hosts[0].Tags] {
				t.Fatalf("tag wider than the room beside the protocol tag was painted")
			}
			if !painted["web"] {
				t.Fatalf("a tag that fits was left out")
			}
			if !painted["RDP"] || !painted["SSH"] {
				t.Fatalf("protocol tag missing beside user tags")
			}
		})
	}
}
