//go:build darwin || windows || linux

package workbench

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
	"motty/internal/i18n"
)

func TestHostTagsNativeQueryKeepsFocusAndDoesNotSubmitTheHost(t *testing.T) {
	for _, scale := range []float64{1, 1.5, 2} {
		state := newTestState(t, freshOptions(t))
		state.setLanguage(i18n.Chinese)
		state.setAppearance(appearanceLight)
		state.Click(hitAddHost)
		state.editor.label.Insert("native tags")
		state.editor.address.Insert("192.0.2.42")
		view, err := headless.Open(app.Config{Width: 1100, Height: 820, Tree: Tree(), Paint: state.Paint, OnKey: state.Key, OnPointer: state.Pointer, OnScroll: state.Scroll, TextInput: func(c app.TextInputContext) *app.TextInput {
			if editor := state.EditorFor(c.ID); editor != nil {
				return &app.TextInput{Editor: editor}
			}
			return nil
		}}, nil, headless.Options{Scale: scale})
		if err != nil {
			t.Fatal(err)
		}
		if err := view.Rest(); err != nil {
			t.Fatal(err)
		}
		click := func(id ui.ID) {
			t.Helper()
			for _, hit := range view.Snapshot().Hits {
				if hit.ID == id {
					if err := view.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
						t.Fatal(err)
					}
					return
				}
			}
			t.Fatal("missing hit", id)
		}
		click(hitDialogTags)
		if dir := os.Getenv("MOTTY_TAGS_SCREENSHOTS"); dir != "" && scale == 1 {
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := view.Screenshot(filepath.Join(dir, "host-tags-options.png")); err != nil {
				t.Fatal(err)
			}
		}
		if err := view.Type("上海机房"); err != nil {
			t.Fatal(err)
		}
		if err := view.Keys("Enter"); err != nil {
			t.Fatal(err)
		}
		p := state.hostTagPicker()
		if state.modal != modalNewHost || len(state.hosts) != 0 || state.editor.value(fieldTags) != "上海机房" || !p.control.Open {
			t.Fatal("native Enter submitted host or dropped tag", state.editor.value(fieldTags))
		}
		if err := view.Type("生产"); err != nil {
			t.Fatal("query lost native editing after first selection", err)
		}
		if err := view.Keys("Enter"); err != nil {
			t.Fatal(err)
		}
		if state.editor.value(fieldTags) != "上海机房, 生产" {
			t.Fatal("second selection was not accepted", state.editor.value(fieldTags))
		}
		click(hitHostTagRemove.At(0))
		if err := view.Type("测试"); err != nil {
			t.Fatal("chip remove lost native input focus", err)
		}
		if err := view.Keys("Enter"); err != nil {
			t.Fatal(err)
		}
		if state.editor.value(fieldTags) != "生产, 测试" {
			t.Fatal("chip removal damaged following selection", state.editor.value(fieldTags))
		}
		if err := view.Keys("Shift+Tab"); err != nil {
			t.Fatal(err)
		}
		if p.control.Open {
			t.Fatal("Shift+Tab did not leave the popup")
		}
		for tries := 0; view.Snapshot().Focused != hitHostTagRemove.At(1) && tries < 24; tries++ {
			if err := view.Keys("Tab"); err != nil {
				t.Fatal(err)
			}
		}
		if view.Snapshot().Focused != hitHostTagRemove.At(1) {
			t.Fatal("chip remove cannot receive keyboard focus", tree.Text(view.Snapshot().Focused))
		}
		if err := view.Keys("Enter"); err != nil {
			t.Fatal(err)
		}
		if state.editor.value(fieldTags) != "生产" {
			t.Fatal("Enter on a remove button chose an option instead")
		}
		if err := view.Type("测试"); err != nil {
			t.Fatal(err)
		}
		if err := view.Keys("Enter"); err != nil {
			t.Fatal(err)
		}
		if dir := os.Getenv("MOTTY_TAGS_SCREENSHOTS"); dir != "" && scale == 1 {
			if err := view.Screenshot(filepath.Join(dir, "host-tags-selected.png")); err != nil {
				t.Fatal(err)
			}
		}
		if err := view.Click(20, 20); err != nil {
			t.Fatal(err)
		}
		if p.control.Open || state.modal != modalNewHost {
			t.Fatal("outside click closed host dialog instead of dropdown")
		}
		click(hitDialogSubmit)
		if len(state.hosts) != 1 || state.hosts[0].Tags != "生产, 测试" {
			t.Fatal("native tag save failed", state.dialogError)
		}
		view.Close()
	}
}
