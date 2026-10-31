//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
	"motty/internal/serial"
)

// paintHostCardTexts paints one card on a canvas of its own and returns what it
// wrote, so a test can check the words a user reads rather than the fields behind
// them.
func paintHostCardTexts(t *testing.T, state *State, host Host, width float32) []ui.Text {
	t.Helper()
	card := ui.Rect{X: 20, Y: 20, Width: width, Height: hostCardHeight}
	var canvas ui.Canvas
	view, err := headless.Open(app.Config{
		Width: int(width + 40), Height: 104, Tree: Tree(),
		Paint: func(canvas *ui.Canvas, _ ui.Size) {
			canvas.Fill(ui.Rect{Width: width + 40, Height: 104}, state.pal.workspace)
			state.paintHostCard(canvas, card, host)
		},
	}, &canvas, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	return canvas.Texts()
}

// A card names the account and the destination once, whatever the protocol: a
// tunneled desktop already carries the account inside its target, a desktop
// target spells its own protocol, and a serial line has no account to show.
func TestHostCardConnectionLineNamesEachPartOnce(t *testing.T) {
	state := manyHostsState(t, 1)
	cases := []struct {
		name string
		host Host
		want string
	}{
		{"ssh", Host{Label: "prod", Address: "10.0.0.1", User: "ops", Port: 2222}, "ops · 10.0.0.1:2222"},
		{"vnc-over-ssh", Host{Label: "desk", Protocol: protocolVNC, ViaSSH: true, Address: "10.0.0.9", User: "ops", Port: 5901}, "vnc+ssh://ops@10.0.0.9:5901"},
		{"vnc", Host{Label: "desk", Protocol: protocolVNC, Address: "10.0.0.9", Port: 5901}, "vnc://10.0.0.9:5901"},
		{"rdp-user", Host{Label: "win", Protocol: protocolRDP, Address: "win.example", User: "admin", Port: 3389}, "admin · rdp://win.example"},
		{"rdp", Host{Label: "win", Protocol: protocolRDP, Address: "win.example", Port: 3389}, "rdp://win.example"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			host := test.host
			host.finish()
			for _, text := range paintHostCardTexts(t, state, host, 420) {
				if strings.Contains(text.Value, host.Address) {
					if text.Value != test.want {
						t.Fatalf("the card's connection line = %q, want %q", text.Value, test.want)
					}
					return
				}
			}
			t.Fatalf("the card has no line with %q", host.Address)
		})
	}

	// A serial line has no login: the card says where the line is and how it is
	// set, never the local account that owns the app.
	line := Host{Protocol: protocolSerial, Address: "/dev/tty.usbserial", Line: serial.Default}
	line.finish()
	if got := state.hostCardConnection(line); got != line.target {
		t.Fatalf("the serial card's connection line = %q, want its target %q", got, line.target)
	}
}

// A destination too long for the card ends in an ellipsis instead of running
// under the checkbox.
func TestHostCardTruncatesALongConnectionLine(t *testing.T) {
	state := manyHostsState(t, 1)
	host := state.hosts[0]
	host.Address = "a-very-long-host-name-that-does-not-fit-any-narrow-card.example.internal"
	host.Port = 2222
	host.finish()
	var line ui.Text
	found := false
	for _, text := range paintHostCardTexts(t, state, host, hostCardMinWidth) {
		if strings.Contains(text.Value, host.User) && strings.Contains(text.Value, " · ") {
			line, found = text, true
		}
	}
	if !found {
		t.Fatal("the card has no connection line")
	}
	if !strings.HasSuffix(line.Value, "…") {
		t.Fatalf("the connection line %q does not end in an ellipsis", line.Value)
	}
	if line.HasClip {
		t.Fatalf("the ellipsized connection line is clipped too: %+v", line.ClipOf)
	}
}

func TestHostCardShowsConnectionAndCentersCheckbox(t *testing.T) {
	for _, scale := range []float64{1, 1.5, 2} {
		for _, test := range []struct {
			name  string
			width float32
			label string
		}{
			{"narrow", 280, "Host 00"},
			{"wide", 420, "Host 00"},
			{"long-name", 280, "生产环境 Production host with a very long descriptive name"},
		} {
			t.Run(fmt.Sprintf("%s-scale-%g", test.name, scale), func(t *testing.T) {
				state := manyHostsState(t, 1)
				host := state.hosts[0]
				host.Label = test.label
				card := ui.Rect{X: 20, Y: 20, Width: test.width, Height: hostCardHeight}
				var canvas ui.Canvas
				view, err := headless.Open(app.Config{
					Width: int(test.width + 40), Height: 104, Tree: Tree(),
					Paint: func(canvas *ui.Canvas, _ ui.Size) {
						canvas.Fill(ui.Rect{Width: test.width + 40, Height: 104}, state.pal.workspace)
						state.paintHostCard(canvas, card, host)
					},
				}, &canvas, headless.Options{Scale: scale})
				if err != nil {
					t.Fatal(err)
				}
				defer view.Close()
				if dir := os.Getenv("MOTTY_HOST_CARD_SCREENSHOTS"); dir != "" {
					if err := view.Screenshot(filepath.Join(dir, t.Name()[strings.LastIndex(t.Name(), "/")+1:]+".png")); err != nil {
						t.Fatal(err)
					}
				}
				found := false
				for _, text := range canvas.Texts() {
					if strings.Contains(text.Value, host.User) && strings.Contains(text.Value, host.Address) {
						found = true
						if text.Bounds.Y < card.Y || text.Bounds.Y+text.Bounds.Height > card.Y+card.Height {
							t.Errorf("connection details are outside the card: %+v", text.Bounds)
						}
						if text.HasClip && (text.ClipOf.Height < text.Bounds.Height || text.ClipOf.Width < text.Bounds.Width) {
							t.Errorf("connection details are clipped: bounds=%+v clip=%+v", text.Bounds, text.ClipOf)
						}
					}
				}
				if !found {
					t.Error("card is missing the username and host address")
				}
				check, ok := hitByID(&canvas, hitHostCheckList.At(int(host.ID)))
				if !ok {
					t.Fatal("card is missing its checkbox")
				}
				for _, text := range canvas.Texts() {
					if text.Bounds.X+text.Bounds.Width > check.Bounds.X+0.01 {
						t.Errorf("card text overlaps the checkbox: text=%+v checkbox=%+v", text.Bounds, check.Bounds)
					}
				}
				if delta := check.Bounds.Y + check.Bounds.Height/2 - (card.Y + card.Height/2); math.Abs(float64(delta)) > 0.01 {
					t.Errorf("checkbox is %g pixels from the card's vertical center", delta)
				}
			})
		}
	}
}
