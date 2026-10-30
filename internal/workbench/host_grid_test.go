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
)

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
