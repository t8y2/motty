//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"motty/internal/i18n"
)

func TestChineseCompletionDescriptionsThroughNativeDriver(t *testing.T) {
	for _, width := range []int{960, 1400} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			world := newCompletionWorld(t)
			world.state.setLanguage(i18n.Chinese)
			world.type_("ta")
			world.state.openCompletions()
			pumpUntil(t, world.state, "Chinese completion descriptions", func() bool {
				return world.state.completion.descriptions != nil
			})
			view, err := headless.Open(app.Config{
				Title: "Completion descriptions", Width: width, Height: 700, Tree: Tree(),
				Paint: world.state.Paint, OnResize: world.state.Resize, OnKey: world.state.Key,
				OnPointer: world.state.Pointer, OnScroll: world.state.Scroll,
			}, nil, headless.Options{Scale: 1.25})
			if err != nil {
				t.Fatal(err)
			}
			defer view.Close()
			for _, language := range []i18n.Language{i18n.Chinese, i18n.English, i18n.Chinese} {
				world.state.setLanguage(language)
				if err := view.Frame(); err != nil {
					t.Fatal(err)
				}
				if err := view.Rest(); err != nil {
					t.Fatal(err)
				}
				want := "output the last part of files"
				if language == i18n.Chinese {
					want = "输出文件末尾的内容"
				}
				found := false
				for _, text := range view.Snapshot().Texts {
					if text.Value == want {
						found = true
						if text.Bounds.X < 0 || text.Bounds.X+text.Bounds.Width > float32(width) {
							t.Fatalf("localized description escapes the window: %+v", text.Bounds)
						}
					}
				}
				if directory := os.Getenv("MOTTY_COMPLETION_LOCALE_SCREENSHOTS"); directory != "" {
					if err := os.MkdirAll(directory, 0o700); err != nil {
						t.Fatal(err)
					}
					if err := view.Screenshot(filepath.Join(directory, fmt.Sprintf("completion-%s-%d.png", language, width))); err != nil {
						t.Fatal(err)
					}
				}
				if !found {
					t.Fatalf("native frame did not paint %q", want)
				}
			}
			before := world.shell.typed()
			if err := view.ClickLabel("tail"); err != nil {
				t.Fatal(err)
			}
			if got := world.shell.typed(); got != before+"il" {
				t.Fatalf("native acceptance typed %q, want %q", got, before+"il")
			}
		})
	}
}
