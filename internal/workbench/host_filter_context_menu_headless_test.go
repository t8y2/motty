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

func hostFilterContextView(t *testing.T, state *State, size gfx.Size) *headless.Session {
	t.Helper()
	view, err := headless.Open(app.Config{
		Width: int(size.Width), Height: int(size.Height), Tree: Tree(), Paint: state.Paint,
		OnKey: state.Key, OnPointer: state.Pointer, OnScroll: state.Scroll, OnResize: state.Resize,
		TextInput: func(c app.TextInputContext) *app.TextInput {
			if editor := state.EditorFor(c.ID); editor != nil {
				return &app.TextInput{Editor: editor}
			}
			return nil
		},
	}, nil, headless.Options{Scale: 1.25})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(view.Close)
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	return view
}

func hostFilterContextHit(t *testing.T, view *headless.Session, id gfx.ID) gfx.Hit {
	t.Helper()
	hit, ok := hitByIDSnapshot(view.Snapshot().Hits, id)
	if !ok {
		t.Fatalf("missing hit %s", tree.Text(id))
	}
	return hit
}

func hostFilterContextClick(t *testing.T, view *headless.Session, id gfx.ID, secondary bool) {
	t.Helper()
	hit := hostFilterContextHit(t, view, id)
	x, y := hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2
	var err error
	if secondary {
		err = view.RightClick(x, y)
	} else {
		err = view.Click(x, y)
	}
	if err != nil {
		t.Fatal(err)
	}
}

func hostFilterContextShot(t *testing.T, view *headless.Session, name string) {
	t.Helper()
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if dir := os.Getenv("MOTTY_FILTER_CONTEXT_SCREENSHOTS"); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := view.Screenshot(filepath.Join(dir, name+".png")); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHostFilterContextNativeSelectedAndUnselectedGroups(t *testing.T) {
	for _, mode := range []hostViewMode{hostViewList, hostViewGrid} {
		for _, light := range []bool{false, true} {
			t.Run(fmt.Sprintf("view=%d/light=%v", mode, light), func(t *testing.T) {
				state := newTestState(t, testOptions(t))
				state.hostView = mode
				state.setLanguage(i18n.Chinese)
				if light {
					state.setAppearance(appearanceLight)
				} else {
					state.setAppearance(appearanceDark)
				}
				state.hosts[0].Group, state.hosts[0].Label = "停车生产本地", "停车测试主机"
				state.hosts[1].Group, state.hosts[1].Label = "朗新内网", "内网测试主机 A"
				state.hosts[2].Group, state.hosts[2].Label = "朗新内网", "内网测试主机 B"
				state.hostDataChanged()
				view := hostFilterContextView(t, state, gfx.Size{Width: 1400, Height: 900})
				click := func(id gfx.ID) { hostFilterContextClick(t, view, id, false) }
				right := func(id gfx.ID) { hostFilterContextClick(t, view, id, true) }
				shot := func(name string) {
					if mode == hostViewList && light {
						hostFilterContextShot(t, view, name)
					}
				}
				selected := state.hostGroupIndex("停车生产本地")
				click(hitHostFilterList.At(selected))
				if _, ok := hitByIDSnapshot(view.Snapshot().Hits, hitHostGroupRowList.At(selected)); ok {
					t.Fatal("selected filter fixture still exposes a list group header")
				}
				right(hitHostFilterList.At(selected))
				if !state.groupActions.open || state.groupActions.path != "停车生产本地" || state.hostFilter != "停车生产本地" {
					t.Fatal("native selected chip right-click failed or changed the filter")
				}
				for _, id := range []gfx.ID{hitGroupNewHost, hitGroupNewChild, hitGroupRename, hitGroupMove, hitGroupDelete} {
					hostFilterContextHit(t, view, id)
				}
				shot("01-selected-group-menu")
				click(hitGroupRename)
				shot("02-rename-from-selected-group")
				for _, operation := range []func() error{
					func() error { return view.Keys("cmd+a") },
					func() error { return view.Type("停车生产环境") },
					func() error { return view.Keys("Enter") },
				} {
					if err := operation(); err != nil {
						t.Fatal(err)
					}
				}
				if state.modal != modalNone || state.hostFilter != "停车生产环境" || state.hosts[0].Group != state.hostFilter {
					t.Fatal("rename from selected filter did not update its hosts and filter")
				}
				right(hitHostFilterList.At(state.hostGroupIndex("朗新内网")))
				if state.hostFilter != "停车生产环境" || state.groupActions.path != "朗新内网" {
					t.Fatal("right-click on another chip selected it or used the wrong target")
				}
				click(hitGroupNewHost)
				if state.editor.value(fieldGroup) != "朗新内网" {
					t.Fatal("New Host used the active filter instead of the pointed chip")
				}
				shot("03-new-host-in-pointed-group")
				click(hitDialogCancel)
				right(hitHostFilterList.At(state.hostGroupIndex("朗新内网")))
				if err := view.Keys("Escape"); err != nil {
					t.Fatal(err)
				}
				if state.groupActions.open || state.hostFilter != "停车生产环境" {
					t.Fatal("Escape changed the filter or left the menu open")
				}
				click(hitHostFilterAll)
				right(hitHostFilterAll)
				if state.groupActions.open || state.hostFilterOn {
					t.Fatal("All opened group actions")
				}
				click(hitHostFilterList.At(state.hostGroupIndex("朗新内网")))
				if state.hostFilter != "朗新内网" {
					t.Fatal("left-click filter selection regressed")
				}
			})
		}
	}
}

func TestHostFilterContextNativeOverflowGroup(t *testing.T) {
	state := manyHostsState(t, 18)
	state.setLanguage(i18n.Chinese)
	state.setAppearance(appearanceDark)
	for index := range state.hosts {
		state.hosts[index].Group = fmt.Sprintf("机房分组 %02d", index)
	}
	state.hostDataChanged()
	view := hostFilterContextView(t, state, gfx.Size{Width: 960, Height: 600})
	hostFilterContextClick(t, view, hitHostFilterMore, true)
	if state.groupActions.open {
		t.Fatal("More itself opened a group menu")
	}
	hostFilterContextClick(t, view, hitHostFilterMore, false)
	index := state.hostFilterHidden
	target := state.hostGroups[index].Name
	hostFilterContextClick(t, view, hitHostFilterMenuList.At(index), true)
	if !state.groupActions.open || state.groupActions.path != target || state.hostFilterMenu || state.hostFilterOn {
		t.Fatal("native overflow right-click did not replace the picker without selecting")
	}
	hostFilterContextShot(t, view, "04-overflow-group-menu")
	if err := view.Click(25, 80); err != nil {
		t.Fatal(err)
	}
	if state.groupActions.open || state.hostFilterOn {
		t.Fatal("outside click did not dismiss only the group menu")
	}
}
