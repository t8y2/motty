//go:build windows || darwin || linux

package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
	"motty/internal/i18n"
)

func TestLanguageFlagsRetainTheFiveStars(t *testing.T) {
	view, err := headless.Open(app.Config{Title: "Language flags", Width: 640, Height: 620, Paint: func(canvas *ui.Canvas, size ui.Size) {
		theme := ui.LightTheme()
		canvas.Fill(ui.Rect{Width: size.Width, Height: size.Height}, theme.Background)
		for index, locale := range i18n.Supported() {
			x, y := float32(10+index%4*156), float32(10+index/4*150)
			icon := languageFlag(locale.Language)
			if text := languageIconText(locale.Language); text != "" {
				canvas.LabelCenter(text, ui.Rect{X: x, Y: y, Width: 128, Height: 96}, ui.TextStyle{Size: 64, Color: theme.Foreground})
			} else {
				canvas.DrawBitmap(icon, ui.Rect{X: x, Y: y, Width: 128, Height: 96})
			}
			canvas.Label(locale.Label, ui.Rect{X: x, Y: y + 100, Width: 145, Height: 30}, ui.TextStyle{Size: 13, Color: theme.Foreground})
		}
	}}, nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	bitmap, err := view.Image()
	if err != nil {
		t.Fatal(err)
	}
	for _, center := range [][2]int{{34, 34}, {58, 20}, {68, 29}, {68, 44}, {58, 53}} {
		foundYellow := false
		for y := center[1] - 1; y <= center[1]+1; y++ {
			for x := center[0] - 1; x <= center[0]+1; x++ {
				red, green, blue, _ := bitmap.At(x, y).RGBA()
				foundYellow = foundYellow || red > 45000 && green > 40000 && blue < 15000
			}
		}
		if !foundYellow {
			t.Errorf("Chinese flag lost the yellow star near %v", center)
		}
	}
	if directory := os.Getenv("MOTTY_LANGUAGE_SCREENSHOTS"); directory != "" {
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
		if err := view.Screenshot(filepath.Join(directory, "language-flags-details.png")); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLanguageSelectWithFlagsThroughNativeInput(t *testing.T) {
	for _, scale := range []float64{1, 1.5, 2} {
		t.Run(fmt.Sprintf("scale-%.1f", scale), func(t *testing.T) {
			state := newTestState(t, withSystemLanguages(freshOptions(t), "en-US"))
			state.Click(hitSettings)
			state.setAppearance(appearanceLight)
			view, err := headless.Open(app.Config{Title: "Motty languages", Width: 1280, Height: 900, Tree: Tree(), Paint: state.Paint, OnKey: state.Key, OnPointer: state.Pointer, OnScroll: state.Scroll, OnResize: state.Resize}, nil, headless.Options{Scale: scale})
			if err != nil {
				t.Fatal(err)
			}
			defer view.Close()
			click := func(id ui.ID) {
				t.Helper()
				for _, hit := range view.Snapshot().Hits {
					if hit.ID == id && !hit.Block && !hit.IsLabel {
						if err := view.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
							t.Fatal(err)
						}
						return
					}
				}
				t.Fatal("missing language control", id)
			}
			shot := func(name string) {
				t.Helper()
				if directory := os.Getenv("MOTTY_LANGUAGE_SCREENSHOTS"); directory != "" && scale == 1 {
					if err := os.MkdirAll(directory, 0700); err != nil {
						t.Fatal(err)
					}
					if err := view.Screenshot(filepath.Join(directory, name+".png")); err != nil {
						t.Fatal(err)
					}
				}
			}
			click(hitSettingsLanguageSelect)
			view.Rest()
			shot("language-options-light")
			if err := view.Keys("esc"); err != nil {
				t.Fatal(err)
			}
			if state.languagePicker.open || state.language != i18n.English {
				t.Fatal("Escape changed the language")
			}
			for _, locale := range i18n.Supported() {
				click(hitSettingsLanguageSelect)
				click(languageOptionID(locale.Language))
				if state.languagePicker.open || state.language != locale.Language || !state.languageChosen {
					t.Fatalf("failed to choose %s", locale.Tag)
				}
				view.Rest()
				if locale.Language == i18n.Japanese || locale.Language == i18n.Thai || locale.Language == i18n.TraditionalChinese {
					shot("settings-" + locale.Tag)
				}
			}
			click(hitSettingsLanguageSelect)
			click(languageOptionID(i18n.Turkish))
			state.setAppearance(appearanceDark)
			if err := view.Resize(960, 600); err != nil {
				t.Fatal(err)
			}
			click(hitSettingsLanguageSelect)
			view.Rest()
			shot("language-options-dark")
			if view.Snapshot().Focused != languageOptionID(i18n.Turkish) {
				t.Fatal("opening did not focus the current language")
			}
			for _, key := range []string{"home", "enter"} {
				if err := view.Keys(key); err != nil {
					t.Fatal(err)
				}
			}
			if state.language != i18n.Chinese || state.languagePicker.open {
				t.Fatalf("keyboard selection did not commit: language=%s open=%v focus=%s", state.language, state.languagePicker.open, tree.Text(view.Snapshot().Focused))
			}
			click(hitSettingsLanguageSelect)
			if err := view.Click(20, 100); err != nil {
				t.Fatal(err)
			}
			if state.languagePicker.open || state.activeSection != sectionSettings || state.language != i18n.Chinese {
				t.Fatal("outside press changed the underlying page")
			}
		})
	}
}
