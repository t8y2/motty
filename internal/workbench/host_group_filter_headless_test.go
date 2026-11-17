//go:build darwin || windows || linux

package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
)

func TestFilteredHostGroupsNativeDisclosure(t *testing.T) {
	for _, mode := range []hostViewMode{hostViewList, hostViewGrid} {
		state := nestedHostGroupState(t)
		state.hostView = mode
		view, err := headless.Open(app.Config{Width: 1400, Height: 900, Tree: Tree(), Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnScroll: state.Scroll, OnResize: state.Resize}, nil, headless.Options{Scale: 1})
		if err != nil {
			t.Fatal(err)
		}
		func() {
			defer view.Close()
			if err := view.Rest(); err != nil {
				t.Fatal(err)
			}
			clickHit := func(hit gfx.Hit) {
				t.Helper()
				if err := view.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
					t.Fatal(err)
				}
			}
			click := func(id gfx.ID) {
				t.Helper()
				hit, ok := hitByIDSnapshot(view.Snapshot().Hits, id)
				if !ok {
					t.Fatalf("view %d missing %s", mode, tree.Text(id))
				}
				clickHit(hit)
			}
			hostPainted := func(hostID uint32) bool {
				hits := view.Snapshot().Hits
				if mode == hostViewGrid {
					_, ok := hitByIDSnapshot(hits, hitHostList.At(int(hostID)))
					return ok
				}
				_, ok := hostListHostHitIn(hits, state, hostID)
				return ok
			}
			child := func() gfx.Hit {
				t.Helper()
				hits := view.Snapshot().Hits
				if mode == hostViewGrid {
					hit, ok := hitByIDSnapshot(hits, hitHostGroupOpenList.At(state.hostGroupIndex("Team/Prod")))
					if !ok {
						t.Fatalf("view %d missing the group disclosure", mode)
					}
					return hit
				}
				hit, ok := hostListGroupChevronIn(hits, state, state.hostGroupIndex("Team/Prod"))
				if !ok {
					t.Fatalf("view %d missing the group disclosure", mode)
				}
				return hit
			}
			click(hitHostFilterList.At(state.hostGroupIndex("Team")))
			clickHit(child())
			if hostPainted(state.hosts[0].ID) {
				t.Fatalf("view %d did not hide collapsed descendant", mode)
			}
			clickHit(child())
			if !hostPainted(state.hosts[0].ID) {
				t.Fatalf("view %d did not restore expanded descendant", mode)
			}
		}()
	}
}
