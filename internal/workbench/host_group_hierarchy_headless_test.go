//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/i18n"
)

func TestHostGroupHierarchyNativeMenusAndDialogs(t *testing.T) {
	for _, size := range []gfx.Size{{Width: 1400, Height: 900}, {Width: 900, Height: 600}} {
		for _, mode := range []hostViewMode{hostViewList, hostViewGrid} {
			for _, light := range []bool{false, true} {
				t.Run(fmt.Sprintf("%dx%d/view=%d/light=%v", int(size.Width), int(size.Height), mode, light), func(t *testing.T) {
					state := nestedHostGroupState(t)
					state.hostView = mode
					state.setLanguage(i18n.Chinese)
					if light {
						state.setAppearance(appearanceLight)
					} else {
						state.setAppearance(appearanceDark)
					}
					view, err := headless.Open(app.Config{
						Width: int(size.Width), Height: int(size.Height), Tree: Tree(), Paint: state.Paint,
						OnKey: state.Key, OnPointer: state.Pointer, OnScroll: state.Scroll, OnResize: state.Resize,
						TextInput: func(c app.TextInputContext) *app.TextInput {
							if editor := state.EditorFor(c.ID); editor != nil {
								return &app.TextInput{Editor: editor}
							}
							return nil
						},
					}, nil, headless.Options{Scale: 1})
					if err != nil {
						t.Fatal(err)
					}
					defer view.Close()
					must := func(err error) {
						t.Helper()
						if err != nil {
							t.Fatal(err)
						}
					}
					must(view.Rest())
					hit := func(id gfx.ID) gfx.Hit {
						t.Helper()
						h, ok := hitByIDSnapshot(view.Snapshot().Hits, id)
						if !ok {
							t.Fatalf("missing hit %s", tree.Text(id))
						}
						return h
					}
					click := func(id gfx.ID) {
						t.Helper()
						h := hit(id)
						must(view.Click(h.Bounds.X+h.Bounds.Width/2, h.Bounds.Y+h.Bounds.Height/2))
					}
					shot := func(name string) {
						t.Helper()
						must(view.Rest())
						if dir := os.Getenv("MOTTY_HIERARCHY_SCREENSHOTS"); dir != "" {
							must(os.MkdirAll(dir, 0700))
							must(view.Screenshot(filepath.Join(dir, fmt.Sprintf("%s-%dx%d-view%d-light%v.png", name, int(size.Width), int(size.Height), mode, light))))
						}
					}
					group := func(path string) gfx.Hit {
						id := hitHostGroupRowList.At(state.hostGroupIndex(path))
						if mode == hostViewGrid {
							id = hitHostGroupOpenList.At(state.hostGroupIndex(path))
						}
						return hit(id)
					}
					right := func(path string) {
						t.Helper()
						h := group(path)
						must(view.RightClick(h.Bounds.X+60, h.Bounds.Y+h.Bounds.Height/2))
					}
					shot("tree")
					right("Team/Prod")
					for _, id := range []gfx.ID{hitGroupNewHost, hitGroupNewChild, hitGroupRename, hitGroupMove, hitGroupPin, hitGroupDelete} {
						hit(id)
					}
					shot("menu")
					click(hitGroupRename)
					must(view.Keys("cmd+a"))
					must(view.Type("上海生产"))
					shot("rename")
					must(view.Keys("Enter"))
					if state.hosts[0].Group != "Team/上海生产/EU" {
						t.Fatal("native rename failed", state.dialogError)
					}
					right("Team/上海生产")
					click(hitGroupMove)
					must(view.Keys("Enter"))
					if !state.groupActions.parentOpen {
						t.Fatal("Enter did not open the focused parent selector")
					}
					shot("parent-picker")
					click(hitGroupParentItems.At(state.hostGroupIndex("Team2")))
					click(hitMoveGroupSubmit)
					if state.hosts[0].Group != "Team2/上海生产/EU" {
						t.Fatal("native parent selection failed", state.dialogError)
					}
					right("Team2/上海生产")
					click(hitGroupNewHost)
					if state.editor.value(fieldGroup) != "Team2/上海生产" {
						t.Fatal("native new host omitted clicked group")
					}
					click(hitDialogCancel)
					right("Team2/上海生产")
					click(hitGroupDelete)
					shot("delete")
					click(hitGroupDeleteUngroup)
					click(hitMoveGroupSubmit)
					if state.hosts[0].Group != "" || len(state.hosts) != 3 {
						t.Fatal("native deletion lost a host or destination")
					}
				})
			}
		}
	}
}

func TestHostGroupLongPathsNativeFitDeleteDialog(t *testing.T) {
	state := nestedHostGroupState(t)
	long := strings.Repeat("Production-", 20)
	state.hosts[0].Group = long + "/Child"
	state.hostDataChanged()
	chooseHostGroupAction(t, state, long+"/Child", "delete")
	var painted *gfx.Canvas
	view, err := headless.Open(app.Config{Width: 900, Height: 600, Tree: Tree(), Paint: func(c *gfx.Canvas, size gfx.Size) { state.Paint(c, size); painted = c }}, nil, headless.Options{Scale: 1.5})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	for _, text := range view.Snapshot().Texts {
		if text.Layer >= dialogLayer && strings.Contains(text.Value, long) && painted.TextWidthWeight(text.Value, text.Style.Size, text.Style.Weight) > text.Bounds.Width+1 {
			t.Fatalf("long path escapes dialog: width %.0f > %.0f", painted.TextWidthWeight(text.Value, text.Style.Size, text.Style.Weight), text.Bounds.Width)
		}
	}
}
