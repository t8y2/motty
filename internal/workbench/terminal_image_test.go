package workbench

import (
	"bytes"
	"encoding/base64"
	"image"
	stdcolor "image/color"
	"image/png"
	"testing"

	"github.com/vivaui/viva/gfx"
)

// settleImages waits for the background image decode and applies it, then
// wakes the window so the next paint sees the placement.
func settleImages(t testing.TB, state *State) {
	t.Helper()
	for index := range state.sessions {
		if screen := state.sessions[index].term; screen != nil {
			screen.screen.WaitImages()
		}
	}
	state.Wake()
}

func TestTerminalImagesReachVifBitmapCanvasAndSteadyPaintAllocatesNothing(t *testing.T) {
	if raceDetector {
		t.Skip("allocation counts are not comparable under the race detector")
	}
	state, _, _, shell := liveSession(t)
	picture := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	picture.SetNRGBA(0, 0, stdcolor.NRGBA{R: 255, A: 255})
	picture.SetNRGBA(1, 0, stdcolor.NRGBA{B: 255, A: 255})
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, picture); err != nil {
		t.Fatal(err)
	}
	encoded := base64.StdEncoding.EncodeToString(pngData.Bytes())
	shell.say("\x1b]1337;File=inline=1;width=40px;height=20px:" + encoded + "\x07")
	state.Wake()
	settleImages(t, state)

	var canvas gfx.Canvas
	size := gfx.Size{Width: 1400, Height: 900}
	paint := func() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}
	paint()
	images := canvas.Images()
	if len(images) != 1 || images[0].Bounds.Width != 40 || images[0].Bounds.Height != 20 || images[0].Bitmap.Width != 2 {
		t.Fatalf("canvas images = %+v", images)
	}
	if got := images[0].Bitmap.Pix; len(got) != 8 || got[2] != 255 || got[4] != 255 {
		t.Fatalf("canvas BGRA pixels = %v", got)
	}
	paint()
	if allocs := testing.AllocsPerRun(50, paint); allocs != 0 {
		t.Fatalf("unchanged terminal image paint allocates %v times", allocs)
	}
}

func BenchmarkPaintTerminalImageUnchanged(b *testing.B) {
	state, _, _, shell := liveSession(&testing.T{})
	raw := base64.StdEncoding.EncodeToString([]byte{255, 0, 0, 255})
	shell.say("\x1b_Ga=T,f=32,s=1,v=1,c=20,r=10;" + raw + "\x1b\\")
	state.Wake()
	settleImages(&testing.T{}, state)
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}
}
