package workbench

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

// 关闭背景图保留图片，重启后无需重新选择即可恢复。
func TestBackgroundEnableSwitchKeepsSavedPicture(t *testing.T) {
	picked := filepath.Join(t.TempDir(), "wall.png")
	writePNG(t, picked, 64, 64)
	state, options := backgroundTestState(t, picked)
	picks := 0
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { picks++; return []string{picked}, nil }
	state.options.OpenFiles = options.OpenFiles
	state.Click(tree.ID(backgroundHits, "bg-enabled"))
	waitFor(t, state, state.backgroundOn)
	state.bg.visibility.Value = 65
	state.Click(tree.ID(backgroundHits, "bg-hide-application"))
	state.Click(tree.ID(backgroundHits, "bg-enabled"))
	if state.backgroundOn() {
		t.Fatal("disabled picture is still enabled")
	}
	if _, err := os.Stat(state.backgroundPath()); err != nil {
		t.Fatal(err)
	}
	state.Close()
	again := newTestState(t, options)
	waitFor(t, again, func() bool { return again.bg.pix != nil })
	if again.backgroundOn() {
		t.Fatal("disabled state was not restored")
	}
	again.Click(tree.ID(backgroundHits, "bg-enabled"))
	if !again.backgroundOn() || again.bg.visibility.Value != 65 || picks != 1 {
		t.Fatalf("picture not reused: enabled=%v visibility=%v picks=%d", again.backgroundOn(), again.bg.visibility.Value, picks)
	}
	saved, _, err := loadState(options.StatePath)
	if err != nil || saved.Background == nil || !saved.Background.HideApplicationBackground || !again.bg.hideApplicationBackground {
		t.Fatalf("saved background: %v", err)
	}
}

// 同一行不重新输出，也必须随开关即时恢复或隐藏底色。
func TestBackgroundHidesApplicationFillOnlyWithPicture(t *testing.T) {
	state, _, _, shell := liveSession(t)
	state.bg = backgroundState{set: true, pix: []byte{0, 0, 0, 255}, width: 1, height: 1}
	state.bg.visibility.Value = 20
	shell.say("\x1b[48;2;9;29;73mAPPLICATION\x1b[0m\r\n")
	state.Wake()
	paint := func(want bool) {
		t.Helper()
		var canvas gfx.Canvas
		state.Paint(&canvas, gfx.Size{Width: 1200, Height: 800})
		found, text := false, false
		for _, r := range canvas.Rectangles() {
			found = found || r.Color == gfx.RGB(9, 29, 73)
		}
		for _, label := range canvas.Texts() {
			text = text || label.Value == "APPLICATION"
		}
		if found != want || !text {
			t.Fatalf("application fill=%v want=%v text=%v", found, want, text)
		}
	}
	paint(true)
	state.Click(tree.ID(backgroundHits, "bg-hide-application"))
	paint(false)
	state.Click(tree.ID(backgroundHits, "bg-enabled"))
	paint(true)
	state.Click(tree.ID(backgroundHits, "bg-enabled"))
	paint(false)
	state.Click(tree.ID(backgroundHits, "bg-hide-application"))
	paint(true)
}
