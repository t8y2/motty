//go:build darwin || windows || linux

package workbench

import (
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
)

func TestStartupUploadsSidebarLockupWithoutNavigation(test *testing.T) {
	for _, mode := range []string{"light", "dark"} {
		test.Run(mode, func(test *testing.T) {
			state := newTestState(test, testOptions(test))
			if mode == "light" {
				state.Click(hitTheme)
			}
			var canvas gfx.Canvas
			size := gfx.Size{Width: 1400, Height: 900}
			state.Paint(&canvas, size)
			paints := 0
			session, err := headless.Open(app.Config{
				Width: 1400, Height: 900, Tree: Tree(),
				Paint: func(canvas *gfx.Canvas, size gfx.Size) {
					paints++
					state.Paint(canvas, size)
				},
				OnPointer: state.Pointer, OnKey: state.Key, OnResize: state.Resize,
			}, &canvas, headless.Options{})
			if err != nil {
				test.Fatal(err)
			}
			defer session.Close()
			bitmap := lockupLight()
			if mode == "dark" {
				bitmap = lockupDark()
			}
			found := false
			for _, label := range session.Snapshot().Labels {
				if label.BitmapID == bitmap.ID {
					found = true
					if !label.BitmapUpload || len(label.Bitmap) == 0 || label.Bounds.Width <= 0 || label.Bounds.Height <= 0 {
						test.Fatal("the first frame cannot display the sidebar lockup")
					}
				}
			}
			if !found {
				test.Fatal("the first frame has no sidebar lockup")
			}
			before := paints
			if err := session.Driver().Advance(time.Minute); err != nil {
				test.Fatal(err)
			}
			if paints != before {
				test.Fatal("idle startup scheduled an extra repaint")
			}
		})
	}
}
