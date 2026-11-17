//go:build darwin || windows || linux

package workbench

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/i18n"
)

// Opening a selected deep host filter uses the same hierarchy menu as the
// group's chevron. Every visible row must stay independently clickable.
func TestDeepHostFilterMenuKeepsAncestorRowsReachable(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.setLanguage(i18n.Chinese)
	const selected = "高校/65/新疆维吾尔自治区/喀什大学/主校区/中心"
	state.hosts[0].Group = selected
	state.hosts[1].Group = "高校/65/新疆维吾尔自治区/喀什大学/主校区/东区"
	state.hosts[2].Group = "高校/65/新疆维吾尔自治区/喀什大学/分校区"
	state.hostDataChanged()
	state.setHostFilter(true, selected)
	view := hostFilterContextView(t, state, gfx.Size{Width: 900, Height: 686})
	root := state.hostGroupIndex("高校")
	hostFilterContextClick(t, view, hitHostFilterChevronList.At(root), false)
	if !state.chipMenu.open || state.chipMenu.tree.Depth() != 4 {
		t.Fatalf("selected group did not open its five menu panels: open=%v depth=%d", state.chipMenu.open, state.chipMenu.tree.Depth())
	}
	if dir := os.Getenv("MOTTY_CHIP_MENU_SCREENSHOTS"); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := view.Screenshot(filepath.Join(dir, "deep-host-filter.png")); err != nil {
			t.Fatal(err)
		}
	}
	var rows []gfx.Hit
	for _, hit := range view.Snapshot().Hits {
		if _, ok := hit.ID.In(hitHostChipMenuList); ok && hit.Role == gfx.RoleOption {
			rows = append(rows, hit)
		}
	}
	for a, first := range rows {
		x, y := first.Bounds.X+first.Bounds.Width/2, first.Bounds.Y+first.Bounds.Height/2
		if target, ok := view.Driver().HitTest(x, y); !ok || target != first.ID {
			t.Fatalf("group row %q cannot be reached at its center", first.Label)
		}
		for _, second := range rows[a+1:] {
			width := min(first.Bounds.X+first.Bounds.Width, second.Bounds.X+second.Bounds.Width) - max(first.Bounds.X, second.Bounds.X)
			height := min(first.Bounds.Y+first.Bounds.Height, second.Bounds.Y+second.Bounds.Height) - max(first.Bounds.Y, second.Bounds.Y)
			if width > 0 && height > 0 {
				t.Fatalf("group row %q at %+v covers %q at %+v by %.0fx%.0f", second.Label, second.Bounds, first.Label, first.Bounds, width, height)
			}
		}
	}
	var leaf gfx.ID
	for _, row := range rows {
		if row.Value == "65/新疆维吾尔自治区/喀什大学/主校区/中心" {
			leaf = row.ID
		}
	}
	if leaf.IsZero() {
		t.Fatal("the selected leaf has no visible menu row")
	}
	hostFilterContextClick(t, view, leaf, false)
	if state.chipMenu.open || !state.hostFilterOn || state.hostFilter != selected {
		t.Fatalf("clicking the leaf selected %q (open=%v)", state.hostFilter, state.chipMenu.open)
	}
}
