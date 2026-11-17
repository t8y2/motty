//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/i18n"
)

func TestHostGroupPickerNativeQuerySelectionAndDismissal(t *testing.T) {
	for _, scale := range []float64{1, 1.25, 1.5, 1.75, 2} {
		for _, light := range []bool{false, true} {
			t.Run(fmt.Sprintf("%.2f/light=%v", scale, light), func(t *testing.T) {
				state := newTestState(t, testOptions(t))
				state.setLanguage(i18n.Chinese)
				if light {
					state.setAppearance(appearanceLight)
				} else {
					state.setAppearance(appearanceDark)
				}
				state.hosts[0].Group = "已有分组"
				state.refreshHostGroups()
				state.Click(hitAddHost)
				view, err := headless.Open(app.Config{Width: 1100, Height: 820, Tree: Tree(), Paint: state.Paint, OnKey: state.Key, OnPointer: state.Pointer, OnScroll: state.Scroll, TextInput: func(c app.TextInputContext) *app.TextInput {
					if editor := state.EditorFor(c.ID); editor != nil {
						return &app.TextInput{Editor: editor}
					}
					return nil
				}}, nil, headless.Options{Scale: scale})
				if err != nil {
					t.Fatal(err)
				}
				defer view.Close()
				if err := view.Rest(); err != nil {
					t.Fatal(err)
				}
				click := func(id gfx.ID) {
					t.Helper()
					for _, hit := range view.Snapshot().Hits {
						if hit.ID == id {
							if err := view.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
								t.Fatal(err)
							}
							return
						}
					}
					t.Fatal("missing hit", tree.Text(id))
				}
				shot := func(name string) {
					t.Helper()
					if dir := os.Getenv("MOTTY_GROUP_SCREENSHOTS"); dir != "" && scale == 1 {
						if err := os.MkdirAll(dir, 0700); err != nil {
							t.Fatal(err)
						}
						if err := view.Screenshot(filepath.Join(dir, fmt.Sprintf("%s-light-%v.png", name, light))); err != nil {
							t.Fatal(err)
						}
					}
				}
				click(hitDialogGroup)
				shot("host-groups-existing")
				if err := view.Type("上海机房"); err != nil {
					t.Fatal(err)
				}
				shot("host-groups-create")
				if err := view.Keys("Enter"); err != nil {
					t.Fatal(err)
				}
				p := state.hostGroupPicker()
				if state.modal != modalNewHost || p.control.Open || state.editor.value(fieldGroup) != "上海机房" {
					t.Fatal("native Enter lost value or submitted form", state.dialogError)
				}
				shot("host-groups-selected")
				click(hitHostGroupTrigger)
				if err := view.Type("已有分组"); err != nil {
					t.Fatal(err)
				}
				if err := view.Keys("Enter"); err != nil {
					t.Fatal(err)
				}
				if state.editor.value(fieldGroup) != "已有分组" || p.control.Open {
					t.Fatal("native replacement failed")
				}
				click(hitHostGroupClear)
				if state.editor.value(fieldGroup) != "" {
					t.Fatal("native clear did not remove group")
				}
				click(hitHostGroupTrigger)
				if err := view.Keys("Escape"); err != nil {
					t.Fatal(err)
				}
				if state.modal != modalNewHost || p.control.Open {
					t.Fatal("Escape dismissed parent form")
				}
				click(hitHostGroupTrigger)
				if err := view.Keys("Tab"); err != nil {
					t.Fatal(err)
				}
				if p.control.Open {
					t.Fatal("Tab trapped focus")
				}
				click(hitHostGroupTrigger)
				if err := view.Click(12, 70); err != nil {
					t.Fatal(err)
				}
				if p.control.Open || state.modal != modalNewHost {
					t.Fatal("outside press did not dismiss only the picker")
				}
			})
		}
	}
}
