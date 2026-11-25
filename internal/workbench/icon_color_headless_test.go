//go:build windows || darwin || linux

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

func TestLogoColorSelectionThroughDriver(t *testing.T) {
	for _, language := range []i18n.Language{i18n.English, i18n.Chinese, i18n.TraditionalChinese} {
		for _, width := range []int{900, 1400} {
			t.Run(fmt.Sprintf("%s-%d", language, width), func(t *testing.T) {
				state := newTestState(t, freshOptions(t))
				state.setLanguage(language)
				state.Click(hitSettings)
				state.selectSettingsCategory(settingsAppearance)
				view, err := headless.Open(state.WindowConfig(app.Config{
					Title: "Logo colors", Width: width, Height: 900, Tree: Tree(),
					Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnResize: state.Resize,
				}), nil, headless.Options{Scale: 1.5})
				if err != nil {
					t.Fatal(err)
				}
				defer view.Close()
				click := func(id gfx.ID) {
					t.Helper()
					hit, found := hitByIDSnapshot(view.Snapshot().Hits, id)
					if !found {
						t.Fatalf("missing control %v", id)
					}
					if err := view.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
						t.Fatal(err)
					}
				}
				shot := func(name string) {
					t.Helper()
					if dir := os.Getenv("MOTTY_ICON_COLOR_SCREENSHOTS"); dir != "" && language == i18n.Chinese {
						if err := view.Wait(250 * time.Millisecond); err != nil {
							t.Fatal(err)
						}
						if name == "settings-blue" || name == "settings-preview-narrow" {
							picture, err := view.Image()
							if err != nil {
								t.Fatal(err)
							}
							checked := 0
							for _, label := range view.Snapshot().Labels {
								for color := iconMint; color < iconColorCount; color++ {
									if label.BitmapID != color.previewBitmap().ID {
										continue
									}
									checked++
									// Both stems of the M must remain visible when the
									// dialog covers settings or the sidebar beneath it.
									for _, stem := range []float32{0.25, 0.75} {
										x := int((label.Bounds.X + label.Bounds.Width*stem) * 1.5)
										y := int((label.Bounds.Y + label.Bounds.Height*0.55) * 1.5)
										r, g, b, _ := picture.At(x, y).RGBA()
										if max(r, g, b) < 80*257 {
											t.Fatalf("%s preview is obscured at stem %g", color.saved(), stem)
										}
									}
								}
							}
							if checked != int(iconColorCount) {
								t.Fatalf("only %d effect previews were painted", checked)
							}
							if name == "settings-blue" {
								if err := view.Screenshot(filepath.Join(dir, "logo-color-picker.png")); err != nil {
									t.Fatal(err)
								}
							}
						}
						if err := view.Screenshot(filepath.Join(dir, name+".png")); err != nil {
							t.Fatal(err)
						}
					}
				}
				assertClosed := func() {
					t.Helper()
					if state.modal != modalNone {
						t.Fatal("the picker did not close")
					}
					if _, found := hitByIDSnapshot(view.Snapshot().Hits, hitSettingsIconColors.At(0)); found {
						t.Fatal("effect previews are still shown outside the picker")
					}
				}
				assertClosed()
				if width == 1400 {
					shot("settings-logo-color")
				}
				for color := iconMint; color < iconColorCount; color++ {
					previous := state.iconColor
					click(hitSettingsIconColorChoose)
					if state.modal != modalIconColorPicker || state.iconColorChoice != previous {
						t.Fatal("Choose did not open a picker with the current color selected")
					}
					selected, found := hitByIDSnapshot(view.Snapshot().Hits, hitSettingsIconColors.At(int(color)))
					if !found || selected.Bounds.X < 0 || selected.Bounds.X+selected.Bounds.Width > float32(width) || selected.Bounds.Height < 64 {
						t.Fatalf("%s has no complete effect preview: %v", color.saved(), selected.Bounds)
					}
					if err := view.ClickLabel(state.iconColorLabel(color)); err != nil {
						t.Fatal(err)
					}
					if state.iconColorChoice != color || state.iconColor != previous {
						t.Fatal("selecting a preview changed the applied color before confirmation")
					}
					chosen, found := hitByIDSnapshot(view.Snapshot().Hits, selected.ID)
					if !found || !chosen.Checked || chosen.SkipTab {
						t.Fatalf("%s is not selected and keyboard accessible", color.saved())
					}
					if width == 1400 {
						shot("settings-" + color.saved())
					}
					if width == 900 && color == iconBlue {
						shot("settings-preview-narrow")
					}
					click(hitIconColorConfirm)
					assertClosed()
					if state.iconColor != color {
						t.Fatalf("Confirm did not apply %s", color.saved())
					}
				}
				click(hitSettingsIconColorChoose)
				if err := view.Keys("Right"); err != nil {
					t.Fatal(err)
				}
				if state.iconColorChoice != iconMint || state.iconColor != iconGraphite {
					t.Fatal("Right did not move the preview without applying it")
				}
				if err := view.Keys("Left"); err != nil {
					t.Fatal(err)
				}
				if state.iconColorChoice != iconGraphite {
					t.Fatal("Left did not wrap to the last preview")
				}
				if err := view.Keys("Right"); err != nil {
					t.Fatal(err)
				}
				if err := view.Keys("Escape"); err != nil {
					t.Fatal(err)
				}
				assertClosed()
				if state.iconColor != iconGraphite {
					t.Fatal("Escape applied an unconfirmed preview")
				}
				click(hitSettingsIconColorChoose)
				if err := view.ClickLabel(state.iconColorLabel(iconBlue)); err != nil {
					t.Fatal(err)
				}
				click(hitIconColorCancel)
				assertClosed()
				if state.iconColor != iconGraphite {
					t.Fatal("Cancel applied an unconfirmed preview")
				}
				click(hitSettingsIconColorChoose)
				if state.iconColorChoice != iconGraphite {
					t.Fatal("reopening kept a cancelled preview")
				}
				if err := view.Click(16, 100); err != nil {
					t.Fatal(err)
				}
				assertClosed()
				click(hitSettingsIconColorChoose)
				if err := view.ClickLabel(state.iconColorLabel(iconOrange)); err != nil {
					t.Fatal(err)
				}
				if err := view.Keys("Enter"); err != nil {
					t.Fatal(err)
				}
				assertClosed()
				if state.iconColor != iconOrange {
					t.Fatal("Enter did not confirm the selected preview")
				}
				if width == 1400 {
					state.setAppearance(appearanceLight)
					if err := view.Resize(width, 900); err != nil {
						t.Fatal(err)
					}
					click(hitSettingsIconColorChoose)
					shot("logo-color-picker-light")
					click(hitIconColorClose)
					assertClosed()
				}
			})
		}
	}
}
