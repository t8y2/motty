package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/term"
)

// 隐藏索引色和真彩色底色时，仍保留光标、反显和文字。
func TestBackgroundApplicationFillKeepsCursorAndInverse(t *testing.T) {
	screen := term.New(20, 2, 0)
	screen.Write([]byte("\x1b[48;2;9;29;73mRGB\x1b[48;5;234mINDEX\x1b[0;7mI\x1b[0m"))
	look := darkPalette
	var cache rowCache
	cache.begin()
	original := cache.row(screen.Row(0), 20, 0, cellSpan{}, &look, false)
	originalText, originalSpans := original.text, len(original.spans)
	look.hideApplicationBackground = true
	hidden := cache.row(screen.Row(0), 20, 0, cellSpan{}, &look, false)
	if hidden.text != originalText || len(hidden.spans) != 2 {
		t.Fatalf("hidden row: text=%q spans=%+v", hidden.text, hidden.spans)
	}
	if hidden.spans[0].color != look.cursor || hidden.spans[0].from != 0 || hidden.spans[0].to != 1 {
		t.Fatalf("cursor changed: %+v", hidden.spans[0])
	}
	if hidden.spans[1].from != 8 || hidden.spans[1].to != 9 {
		t.Fatalf("inverse changed: %+v", hidden.spans[1])
	}
	look.hideApplicationBackground = false
	restored := cache.row(screen.Row(0), 20, 0, cellSpan{}, &look, false)
	if restored.text != originalText || len(restored.spans) != originalSpans {
		t.Fatalf("original row not restored: %+v", restored.spans)
	}
}

// 设置搜索定位后，开关和下方说明均出现在实际设置页面中。
func TestBackgroundSettingsExposeSwitchAndDescription(t *testing.T) {
	state, _ := backgroundTestState(t, "")
	m := state.messages.Tools
	for _, label := range []string{m.BgEnabled, m.BgHideApplication} {
		searchSettings(t, state, label)
		found := false
		for index, row := range state.settings.results[:state.settings.count] {
			if row.label == label {
				state.Click(hitSettingsResultList.At(index))
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("setting not searchable: %s", label)
		}
	}
	driver := paneDriver(t, state, gfx.Size{Width: 1400, Height: 900})
	if err := driver.Paint(); err != nil {
		t.Fatal(err)
	}
	hitOf(t, driver, hitBgHideApplication)
	clickPaintedHit(t, driver, hitBgHideApplication)
	if !state.bg.hideApplicationBackground {
		t.Fatal("switch did not enable the setting")
	}
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	for _, label := range canvas.Texts() {
		if label.Value == m.BgHideApplicationDescription {
			return
		}
	}
	t.Fatal("application background explanation was not painted")
}
