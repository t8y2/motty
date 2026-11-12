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
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/serial"
)

// paintHostCardTexts paints one card on a canvas of its own and returns what it
// wrote, so a test can check the words a user reads rather than the fields behind
// them.
func paintHostCardTexts(t *testing.T, state *State, host Host, width float32) []gfx.Text {
	t.Helper()
	card := gfx.Rect{X: 20, Y: 20, Width: width, Height: hostCardHeight}
	var canvas gfx.Canvas
	view, err := headless.Open(app.Config{
		Width: int(width + 40), Height: 104, Tree: Tree(),
		Paint: func(canvas *gfx.Canvas, _ gfx.Size) {
			canvas.Fill(gfx.Rect{Width: width + 40, Height: 104}, state.pal.workspace)
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
	var line gfx.Text
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
				card := gfx.Rect{X: 20, Y: 20, Width: test.width, Height: hostCardHeight}
				var canvas gfx.Canvas
				view, err := headless.Open(app.Config{
					Width: int(test.width + 40), Height: 104, Tree: Tree(),
					Paint: func(canvas *gfx.Canvas, _ gfx.Size) {
						canvas.Fill(gfx.Rect{Width: test.width + 40, Height: 104}, state.pal.workspace)
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

// A card grows with the window while it has the row to itself. The grid must not
// keep a column no host fills: widening the window past the point where one more
// column would fit would shrink the only card, and widening again would grow it
// back, which reads as the card changing length for no reason.
func TestHostGridDoesNotReserveAColumnNoHostFills(t *testing.T) {
	state := manyHostsState(t, 1)
	state.hostView = hostViewGrid
	last := float32(0)
	paint := func(width float32) {
		t.Helper()
		var canvas gfx.Canvas
		state.Paint(&canvas, gfx.Size{Width: width, Height: 700})
		card, ok := hitByID(&canvas, hitHostList.At(int(state.hosts[0].ID)))
		if !ok {
			t.Fatalf("the only host has no card at width %.0f", width)
		}
		if card.Bounds.Width+0.01 < last {
			t.Fatalf("the only host's card shrank from %.2f to %.2f as the window widened to %.0f", last, card.Bounds.Width, width)
		}
		last = card.Bounds.Width
	}
	// The card can only shrink where another column starts to fit, so painting
	// the range ends and each of those widths covers every constant-column run.
	// A full-window paint at all 761 widths cost seventeen seconds.
	paint(640)
	for width := float32(640); width < 1400; {
		// The viewport width at which the next column would fit if the grid did
		// not cap its columns at the widest run. The viewport widens with the
		// window by the same amount, so it is also the window's next step.
		columns := hostGridColumns(state.hostsGridViewport.Width)
		next := float32(columns+1)*(hostCardMinWidth+hostCardGap) - hostCardGap
		width = min(width+max(next-state.hostsGridViewport.Width, 1), 1400)
		paint(width)
	}
}

// The cap is the widest run of cards between group headers: a grid with more
// hosts than that still takes every column its width allows.
func TestHostGridColumnsStopAtTheWidestRun(t *testing.T) {
	few := manyHostsState(t, 3)
	if got := few.hostGridColumnCount(4000); got != 3 {
		t.Fatalf("a three-host grid asks for %d columns, want 3", got)
	}
	many := manyHostsState(t, 10)
	if got, want := many.hostGridColumnCount(1000), hostGridColumns(1000); got != want {
		t.Fatalf("a ten-host grid asks for %d columns at 1000px, want the %d its width allows", got, want)
	}
}

// The reveal offset a session log entry computes must place the host exactly
// where the grid paints it, so the two must count columns the same way.
func TestSessionLogRevealUsesTheGridColumnCount(t *testing.T) {
	state := manyHostsState(t, 2)
	state.hostView = hostViewGrid
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 700})
	second := state.hosts[1]
	row, ok := hitByID(&canvas, hitHostList.At(int(second.ID)))
	if !ok {
		t.Fatal("the second host has no card")
	}
	offset := state.sessionLogHostOffset(second.ID, state.hostsGridViewport.Width)
	if want := row.Bounds.Y - state.hostsGridViewport.Y; math.Abs(float64(offset-want)) > 0.01 {
		t.Fatalf("the reveal offset is %.2f, but the card is painted %.2f below the list top", offset, want)
	}
}
