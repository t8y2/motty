//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/i18n"
)

func TestConnectionColorSelectionThroughDriver(t *testing.T) {
	state, _, _, _ := liveSession(t)
	state.setLanguage(i18n.Chinese)
	state.openEditHostID(state.sessions[0].HostID)
	preview, err := headless.Open(app.Config{Title: "Connection colors", Width: 1100, Height: 760, Tree: Tree(), Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut, OnResize: state.Resize}, nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer preview.Close()
	if err = preview.ClickLabel(state.messages.ColorBlue); err != nil {
		t.Fatal(err)
	}
	if state.editor.value(fieldColor) != "#3b82f6" {
		t.Fatal("click did not select color")
	}
	if err = preview.Wait(300 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	shots := os.Getenv("MOTTY_COLOR_SCREENSHOTS")
	if shots != "" {
		if err = preview.Screenshot(filepath.Join(shots, "connection-color-dialog.png")); err != nil {
			t.Fatal(err)
		}
	}
	if err = preview.ClickLabel(state.messages.SaveHost); err != nil {
		t.Fatal(err)
	}
	if state.sessions[0].host.Color != "#3b82f6" {
		t.Fatal("save did not update live terminal")
	}
	if err = preview.Wait(300 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if shots != "" {
		if err = preview.Screenshot(filepath.Join(shots, "connection-color-terminal.png")); err != nil {
			t.Fatal(err)
		}
		state.setAppearance(appearanceLight)
		if err = preview.Resize(1100, 760); err != nil {
			t.Fatal(err)
		}
		if err = preview.Wait(300 * time.Millisecond); err != nil {
			t.Fatal(err)
		}
		if err = preview.Screenshot(filepath.Join(shots, "connection-color-terminal-light.png")); err != nil {
			t.Fatal(err)
		}
	}
}

func BenchmarkPaintConnectionHeader(b *testing.B) {
	for _, color := range []string{"", "#3b82f6"} {
		b.Run("color="+color, func(b *testing.B) {
			state, _, _, _ := liveSession(b)
			state.sessions[0].host.Color = color
			var canvas gfx.Canvas
			size := gfx.Size{Width: 1400, Height: 900}
			state.Paint(&canvas, size)
			b.ReportAllocs()
			for b.Loop() {
				canvas.Reset()
				state.Paint(&canvas, size)
			}
		})
	}
}

func TestConnectionColorChromeAtDifferentScales(t *testing.T) {
	for _, scale := range []float64{1, 1.25, 1.5, 1.75, 2} {
		t.Run(fmt.Sprint(scale), func(t *testing.T) {
			state, session, _, _ := openTestSFTP(t)
			state.setLanguage(i18n.Chinese)
			session.host.Color = "#3b82f6"
			state.hosts[0].Color = "#3b82f6"
			state.rebuildTabOptions()
			view, err := headless.Open(app.Config{Width: 1400, Height: 900, Tree: Tree(), Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnResize: state.Resize}, nil, headless.Options{Scale: scale})
			if err != nil {
				t.Fatal(err)
			}
			defer view.Close()
			for _, light := range []bool{false, true} {
				if light {
					state.setAppearance(appearanceLight)
				}
				state.activeSection = sectionTerminal
				if err := view.Resize(1400, 900); err != nil {
					t.Fatal(err)
				}
				refresh := dualHit(t, view, contextID(hitSFTPRefresh))
				close := dualHit(t, view, contextID(hitSFTPClose))
				if refresh.Y != close.Y || close.X+close.Width > 1400 || close.Y < 0 {
					t.Fatal("file header actions are clipped or misaligned")
				}
				for move := 0; move < 2; move++ {
					button := dualHit(t, view, contextID(hitSFTPPanelSide))
					if err := view.Click(button.X+button.Width/2, button.Y+button.Height/2); err != nil {
						t.Fatal(err)
					}
					current := dualHit(t, view, contextID(hitSFTPRefresh))
					if current.X < 0 || current.X+current.Width > 1400 || current.Y != refresh.Y || move == 0 && current.X <= refresh.X {
						t.Fatal("docking changed the file header geometry or clipped its controls")
					}
				}
				for _, page := range []string{"terminal", "list", "cards"} {
					if page != "terminal" {
						state.activeSection = sectionHosts
						state.hostView = hostViewList
						if page == "cards" {
							state.hostView = hostViewGrid
						}
						if err := view.Resize(1400, 900); err != nil {
							t.Fatal(err)
						}
					}
					if err := view.Driver().Advance(300 * time.Millisecond); err != nil {
						t.Fatal(err)
					}
					if dir := os.Getenv("MOTTY_COLOR_CHROME_SCREENSHOTS"); dir != "" {
						if err := view.Screenshot(filepath.Join(dir, fmt.Sprintf("connection-%s-light-%v-scale-%g.png", page, light, scale))); err != nil {
							t.Fatal(err)
						}
					}
				}
			}
		})
	}
}
