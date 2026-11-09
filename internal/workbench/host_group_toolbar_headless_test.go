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

func TestHostGroupToolbarNativeSelectionAndExpansion(t *testing.T) {
	for _, size := range []gfx.Size{{Width: 1400, Height: 900}, {Width: 960, Height: 600}} {
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
					view, err := headless.Open(app.Config{Width: int(size.Width), Height: int(size.Height), Tree: Tree(), Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnScroll: state.Scroll, OnResize: state.Resize}, nil, headless.Options{Scale: 1.25})
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
							t.Fatalf("missing %s", tree.Text(id))
						}
						return h
					}
					click := func(id gfx.ID) {
						t.Helper()
						h := hit(id)
						if h.Disabled {
							t.Fatalf("disabled %s", tree.Text(id))
						}
						must(view.Click(h.Bounds.X+h.Bounds.Width/2, h.Bounds.Y+h.Bounds.Height/2))
					}
					shot := func(name string) {
						t.Helper()
						must(view.Rest())
						if dir := os.Getenv("MOTTY_GROUP_TOOLBAR_SCREENSHOTS"); dir != "" && light && mode == hostViewList {
							must(os.MkdirAll(dir, 0700))
							must(view.Screenshot(filepath.Join(dir, fmt.Sprintf("433-%s-%dx%d.png", name, int(size.Width), int(size.Height)))))
						}
					}
					if !hit(hitHostGroupsExpandAll).Disabled || hit(hitHostGroupsCollapseAll).Disabled {
						t.Fatal("initial group control enablement is wrong")
					}
					shot("expanded")
					click(hitHostGroupsCollapseAll)
					if len(state.hostTreeRows) != 2 || hit(hitHostGroupsExpandAll).Disabled || !hit(hitHostGroupsCollapseAll).Disabled {
						t.Fatal("native collapse did not hide complete subtrees")
					}
					shot("collapsed")
					click(hitHostGroupsExpandAll)
					if len(state.hostTreeRows) != 8 {
						t.Fatal("native expand did not restore nested hosts")
					}
					first, second := state.hosts[1].ID, state.hosts[0].ID
					click(hitHostCheckList.At(int(first)))
					filter := hit(hitHostFilterAll)
					for _, id := range []gfx.ID{hitEditHost, hitCopyHost, hitMoveHostGroup, hitDashboardAddHosts, hitDeleteHost} {
						h := hit(id)
						if h.Bounds.Y < filter.Bounds.Y+filter.Bounds.Height {
							t.Fatal("selection toolbar overlaps filters")
						}
					}
					shot("single-selected")
					click(hitHostCheckList.At(int(second)))
					if len(state.hostSelections) != 2 || !hit(hitEditHost).Disabled || !hit(hitCopyHost).Disabled {
						t.Fatal("multiple selection enablement is wrong")
					}
					shot("multiple-selected")
					// Header controls must remain distinct and within the viewport.
					ids := []gfx.ID{hitHostFilterAll, hitHostViewList, hitHostViewGrid, hitHostSort, hitHostGroupsExpandAll, hitHostGroupsCollapseAll, hitExportHosts, hitEditHost, hitCopyHost, hitMoveHostGroup, hitDashboardAddHosts, hitDeleteHost}
					for i, id := range ids {
						h := hit(id)
						if h.Bounds.X < 0 || h.Bounds.Y < 0 || h.Bounds.X+h.Bounds.Width > size.Width || h.Bounds.Y+h.Bounds.Height > state.chrome.hostList.Y {
							t.Fatalf("header control escapes: %s %+v", tree.Text(id), h.Bounds)
						}
						for _, other := range ids[i+1:] {
							r := hit(other).Bounds
							if h.Bounds.X < r.X+r.Width && r.X < h.Bounds.X+h.Bounds.Width && h.Bounds.Y < r.Y+r.Height && r.Y < h.Bounds.Y+h.Bounds.Height {
								t.Fatalf("header controls overlap: %s and %s", tree.Text(id), tree.Text(other))
							}
						}
					}
					click(hitHostCheckList.At(int(first)))
					click(hitHostCheckList.At(int(second)))
					if _, ok := hitByIDSnapshot(view.Snapshot().Hits, hitDeleteHost); ok {
						t.Fatal("batch bar remained after clearing checks")
					}
					click(hitHostCheckList.At(int(first)))
					index := state.hostGroupIndex("Team2")
					if _, ok := hitByIDSnapshot(view.Snapshot().Hits, hitHostFilterList.At(index)); ok {
						click(hitHostFilterList.At(index))
					} else {
						click(hitHostFilterMore)
						click(hitHostFilterMenuList.At(index))
					}
					if !state.hostFilterOn || state.hostFilter != "Team2" || len(state.hostSelections) != 0 {
						t.Fatal("filter switching with checks did not select the group and reset checks")
					}
					if _, ok := hitByIDSnapshot(view.Snapshot().Hits, hitHostGroupsCollapseAll); ok {
						t.Fatal("flat group view still exposes bulk tree controls")
					}
				})
			}
		}
	}
}
