//go:build darwin || windows || linux

package workbench

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
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
			var canvas ui.Canvas
			size := ui.Size{Width: 1400, Height: 900}
			state.Paint(&canvas, size)
			b.ReportAllocs()
			for b.Loop() {
				canvas.Reset()
				state.Paint(&canvas, size)
			}
		})
	}
}
