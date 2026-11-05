//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
	"motty/internal/completion"
	"motty/internal/i18n"
)

func TestCompletionDetailsShowsTheEntireSelectedDescription(t *testing.T) {
	w := newCompletionWorld(t)
	w.type_("cat -")
	w.state.openCompletions()
	if !w.state.completion.list.active {
		t.Fatal("the test list did not open")
	}
	w.state.completion.list.candidates[0].Desc = "Explain the selected option fully, including its original safe-tail-marker"
	var canvas ui.Canvas
	w.state.Paint(&canvas, ui.Size{Width: 900, Height: 600})
	var shown []string
	for _, text := range canvas.Texts() {
		shown = append(shown, text.Value)
	}
	if !strings.Contains(strings.Join(shown, " "), "safe-tail-marker") {
		t.Fatalf("selected description was clipped: %q", shown)
	}
}

func TestHeadlessCompletionLocalizedDetailsInNarrowAndWideLayouts(t *testing.T) {
	for _, language := range []i18n.Language{i18n.Chinese, i18n.TraditionalChinese} {
		for _, size := range []ui.Size{{Width: 320, Height: 240}, {Width: 900, Height: 600}} {
			t.Run(fmt.Sprintf("%s-%.0fx%.0f", language, size.Width, size.Height), func(t *testing.T) {
				w := newCompletionWorld(t)
				w.state.setLanguage(language)
				w.type_("cat -")
				w.state.openCompletions()
				view, err := headless.Open(app.Config{
					Width: int(size.Width), Height: int(size.Height), Tree: Tree(), Paint: w.state.Paint, OnResize: w.state.Resize,
				}, nil, headless.Options{})
				if err != nil {
					t.Fatal(err)
				}
				defer view.Close()
				if err := view.Rest(); err != nil {
					t.Fatal(err)
				}
				want := completion.LocalizedDescription(language.String(), "cat", "-b")
				if !strings.Contains(strings.Join(view.Texts(), " "), want) {
					t.Fatalf("localized description %q missing: %q", want, view.Texts())
				}
				for _, problem := range headless.CheckLayout(view.Snapshot()) {
					for _, hit := range view.Snapshot().Hits {
						if hit.ID == hitCompletionList.At(0) && intersectRect(problem.Bounds, hit.Bounds).Width > 0 {
							t.Errorf("localized completion overlaps: %+v", problem)
						}
					}
				}
				if directory := os.Getenv("MOTTY_COMPLETION_SCREENSHOTS"); directory != "" {
					if err := os.MkdirAll(directory, 0o700); err != nil {
						t.Fatal(err)
					}
					name := fmt.Sprintf("%s-localized-%.0fx%.0f.png", language, size.Width, size.Height)
					if err := view.Screenshot(filepath.Join(directory, name)); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}

func TestCompletionDetailsFollowHoverWithoutChangingSelection(t *testing.T) {
	w := newCompletionWorld(t)
	w.type_("cat -")
	w.state.openCompletions()
	w.state.completion.list.candidates[1].Desc = "hover-tail-marker"
	result := w.state.Pointer(ui.PointerEvent{Kind: ui.PointerMove, Target: hitCompletionList.At(1)})
	if !result.Repaint || w.state.completion.list.selected != 0 {
		t.Fatalf("hover changed selection or did not repaint: selected=%d result=%+v", w.state.completion.list.selected, result)
	}
	var canvas ui.Canvas
	w.state.Paint(&canvas, ui.Size{Width: 900, Height: 600})
	var visible []string
	for _, text := range canvas.Texts() {
		visible = append(visible, text.Value)
	}
	if !strings.Contains(strings.Join(visible, " "), "hover-tail-marker") {
		t.Fatalf("hover detail missing: %q", visible)
	}
}

func TestHeadlessCompletionDetailsInThreeLanguagesAndNarrowPane(t *testing.T) {
	for _, language := range []i18n.Language{i18n.English, i18n.Chinese, i18n.TraditionalChinese} {
		for _, size := range []ui.Size{{Width: 320, Height: 240}, {Width: 900, Height: 600}} {
			t.Run(fmt.Sprintf("%s-%.0fx%.0f", language, size.Width, size.Height), func(t *testing.T) {
				w := newCompletionWorld(t)
				w.state.setLanguage(language)
				w.type_("cat -")
				w.state.openCompletions()
				w.state.completion.list.candidates[0].Desc = "The complete safe-tail-marker description stays visible in this terminal pane"
				view, err := headless.Open(app.Config{
					Width: int(size.Width), Height: int(size.Height), Tree: Tree(),
					Paint: w.state.Paint, OnResize: w.state.Resize, OnKey: w.state.Key,
				}, nil, headless.Options{})
				if err != nil {
					t.Fatal(err)
				}
				defer view.Close()
				if err := view.Rest(); err != nil {
					t.Fatal(err)
				}
				snapshot := view.Snapshot()
				found := false
				for _, hit := range snapshot.Hits {
					if hit.ID == hitCompletionList.At(0) && hit.Bounds.Width > 0 {
						found = true
					}
				}
				if !found || !strings.Contains(strings.Join(view.Texts(), " "), "safe-tail-marker") {
					t.Fatalf("completion details not visible at %.0fx%.0f: %q", size.Width, size.Height, view.Texts())
				}
				if size.Width == 320 && !strings.Contains(strings.Join(view.Texts(), " "), "1/7") {
					t.Fatal("narrow list does not show the selected position")
				}
				tailVisible := false
				var tailBounds, tailClip ui.Rect
				for _, text := range snapshot.Texts {
					if strings.Contains(text.Value, "pane") {
						tailBounds, tailClip = text.Bounds, text.ClipOf
						visible := text.Bounds
						if text.HasClip {
							visible = intersectRect(visible, text.ClipOf)
						}
						tailVisible = visible.Height >= text.Bounds.Height-1 && visible.Y+visible.Height <= size.Height
					}
				}
				if !tailVisible {
					t.Fatalf("last description line is not fully visible at %.0fx%.0f: bounds=%+v clip=%+v", size.Width, size.Height, tailBounds, tailClip)
				}
				if directory := os.Getenv("MOTTY_COMPLETION_SCREENSHOTS"); directory != "" {
					if err := os.MkdirAll(directory, 0o700); err != nil {
						t.Fatal(err)
					}
					name := fmt.Sprintf("%s-%.0fx%.0f.png", language, size.Width, size.Height)
					if err := view.Screenshot(filepath.Join(directory, name)); err != nil {
						t.Fatal(err)
					}
				}
				for _, problem := range headless.CheckLayout(snapshot) {
					for _, hit := range snapshot.Hits {
						if hit.ID == hitCompletionList.At(0) && intersectRect(problem.Bounds, hit.Bounds).Width > 0 {
							t.Errorf("completion row layout: %+v", problem)
						}
					}
				}
			})
		}
	}
}

func TestHeadlessCompletionSettingsCanBeToggled(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsTerminal)
	state.Click(hitSettingsCompletionList)
	var invalidator app.Invalidator
	view, err := headless.Open(app.Config{
		Title: "completion settings", Width: 1400, Height: 900, Tree: Tree(),
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnResize: state.Resize, Invalidator: &invalidator,
	}, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	for _, control := range []struct {
		label string
		id    ui.ID
		on    func() bool
	}{
		{state.messages.Completion.AutoPopupRow, hitSettingsCompletionAuto, func() bool { return state.completion.autoPopup }},
		{state.messages.Completion.ProbeRow, hitSettingsCompletionProbe, func() bool { return state.completion.probeEnabled }},
		{state.messages.Completion.PreviewRow, hitSettingsCompletionPreview, func() bool { return state.completion.previewEnabled }},
	} {
		if err := view.ClickLabel(control.label); err != nil {
			t.Fatal(err)
		}
		if !control.on() {
			t.Fatalf("%q did not toggle through the headless pointer route", control.label)
		}
		invalidator.Invalidate()
		if err := view.Driver().Pump(); err != nil {
			t.Fatal(err)
		}
		painted := false
		for _, hit := range view.Snapshot().Hits {
			if hit.ID == control.id && hit.Checked {
				painted = true
			}
		}
		if !painted {
			t.Fatalf("%q changed state but did not repaint as checked", control.label)
		}
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if directory := os.Getenv("MOTTY_COMPLETION_SCREENSHOTS"); directory != "" {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := view.Screenshot(filepath.Join(directory, "settings-controls.png")); err != nil {
			t.Fatal(err)
		}
	}
}
