//go:build darwin || windows || linux

package workbench

import (
	"slices"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
)

func TestHostCatalogChineseNameOrderHeadless(t *testing.T) {
	state := chineseNameOrderState(t)
	state.setHostOrder(hostOrderManual)
	view, err := headless.Open(app.Config{
		Width: 1400, Height: 900, Tree: Tree(), Paint: state.Paint,
		OnKey: state.Key, OnPointer: state.Pointer, OnScroll: state.Scroll, OnResize: state.Resize,
	}, nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	hit := func(id gfx.ID) gfx.Hit {
		t.Helper()
		found, ok := hitByIDSnapshot(view.Snapshot().Hits, id)
		if !ok {
			t.Fatalf("missing native control %s", tree.Text(id))
		}
		return found
	}
	click := func(id gfx.ID) {
		t.Helper()
		control := hit(id)
		if control.Disabled {
			t.Fatalf("disabled native control %s", tree.Text(id))
		}
		if err := view.Click(control.Bounds.X+control.Bounds.Width/2, control.Bounds.Y+control.Bounds.Height/2); err != nil {
			t.Fatal(err)
		}
		if err := view.Rest(); err != nil {
			t.Fatal(err)
		}
	}
	click(hitHostOrder)
	click(hitHostOrderName)
	if state.hostOrder != hostOrderName {
		t.Fatal("native order menu did not select name order")
	}
	if got, want := displayHostIDs(state), []string{"host_bit", "host_bau", "host_ucas", "host_fu", "host_quan"}; !slices.Equal(got, want) {
		t.Fatalf("native pinyin order = %v, want %v", got, want)
	}
}
