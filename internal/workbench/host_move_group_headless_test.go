//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/i18n"
)

func hostMoveGroupShot(t *testing.T, view *headless.Session, name string) {
	t.Helper()
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if dir := os.Getenv("MOTTY_MOVE_MENU_SCREENSHOTS"); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := view.Screenshot(filepath.Join(dir, name+".png")); err != nil {
			t.Fatal(err)
		}
	}
}

func hostMoveGroupNavigate(t *testing.T, state *State, view *headless.Session, value string) gfx.ID {
	t.Helper()
	hosts := append([]Host(nil), state.hosts...)
	targets := slices.Clone(state.hostActionIDs)
	parts := strings.Split(value, "/")
	for level := range parts {
		prefix := strings.Join(parts[:level+1], "/")
		var row, arrow gfx.Hit
		for _, hit := range view.Snapshot().Hits {
			if hit.ID.Scope() == hitHostGroupItemList && hit.Value == prefix {
				if hit.Role == gfx.RoleOption {
					row = hit
				} else if hit.Role == gfx.RoleButton && hit.Expandable {
					arrow = hit
				}
			}
		}
		if row.ID.IsZero() {
			t.Fatalf("no segment for %q at level %d", prefix, level)
		}
		if level == len(parts)-1 {
			return row.ID
		}
		if !arrow.ID.IsZero() {
			hostFilterContextClick(t, view, arrow.ID, false)
		} else {
			if err := view.Driver().Move(row.Bounds.X+row.Bounds.Width/2, row.Bounds.Y+row.Bounds.Height/2); err != nil {
				t.Fatal(err)
			}
			if err := view.Driver().Advance(70 * time.Millisecond); err != nil {
				t.Fatal(err)
			}
		}
		if err := view.Rest(); err != nil {
			t.Fatal(err)
		}
		if !state.hostGroupMenu || !reflect.DeepEqual(state.hosts, hosts) || !slices.Equal(state.hostActionIDs, targets) {
			t.Fatalf("navigation to %q changed hosts, targets or open state", prefix)
		}
	}
	t.Fatal("empty destination")
	return gfx.ID{}
}

// Moving a host should browse the same segment hierarchy as its group filter;
// a flat list of full paths obscures the differing last segment of long names.
func TestMoveHostGroupBrowsesSegmentsFromItsToolbar(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.setLanguage(i18n.Chinese)
	state.hosts[0].Group = ""
	state.hosts[1].Group = "高校/65/新疆维吾尔自治区/喀什大学/主校区/中心"
	state.hosts[2].Group = "高校/65/新疆维吾尔自治区/喀什大学/主校区/东区"
	state.hostDataChanged()
	view := hostFilterContextView(t, state, gfx.Size{Width: 1200, Height: 686})
	hostFilterContextClick(t, view, hitHostCheckList.At(int(state.hosts[0].ID)), false)
	hostFilterContextClick(t, view, hitMoveHostGroup, false)
	hostMoveGroupShot(t, view, "move-group-root")
	groups := 0
	for _, hit := range view.Snapshot().Hits {
		if hit.ID.Scope() != hitHostGroupItemList {
			continue
		}
		groups++
		if strings.Contains(hit.Label, "/") {
			t.Fatalf("move menu shows flattened path %q instead of a hierarchy segment", hit.Label)
		}
	}
	if groups == 0 {
		t.Fatal("move menu has no group choices")
	}
	const destination = "高校/65/新疆维吾尔自治区/喀什大学/主校区/中心"
	id := hostMoveGroupNavigate(t, state, view, destination)
	hostMoveGroupShot(t, view, "move-group-deep")
	hostFilterContextClick(t, view, id, false)
	if state.hostGroupMenu || state.hosts[0].Group != destination {
		t.Fatalf("leaf chose %q, open=%v", state.hosts[0].Group, state.hostGroupMenu)
	}
}

func TestMoveHostGroupPreservesCapturedTargetsAndPersists(t *testing.T) {
	const destination = "高校/65/新疆维吾尔自治区/喀什大学/主校区/中心"
	for _, entry := range []string{"toolbar", "context-unchecked", "context-checked"} {
		t.Run(entry, func(t *testing.T) {
			state := newTestState(t, testOptions(t))
			for index := range state.hosts {
				state.hosts[index].Group = "Source"
			}
			state.hostGroups = append(state.hostGroups, hostGroup{Name: destination, Explicit: true, Expanded: true})
			state.hostDataChanged()
			before := append([]Host(nil), state.hosts...)
			view := hostFilterContextView(t, state, gfx.Size{Width: 650, Height: 800})
			hostFilterContextClick(t, view, hitHostCheckList.At(int(state.hosts[0].ID)), false)
			targets := []uint32{state.hosts[0].ID, state.hosts[1].ID}
			if entry != "context-unchecked" {
				hostFilterContextClick(t, view, hitHostCheckList.At(int(state.hosts[1].ID)), false)
			}
			if entry == "toolbar" {
				hostFilterContextClick(t, view, hitMoveHostGroup, false)
			} else {
				// The list names its rows through the page tree, not with the
				// workbench host hit, so the row is resolved from the fresh
				// frame before the secondary press.
				row, ok := hostListHostHitIn(view.Snapshot().Hits, state, state.hosts[1].ID)
				if !ok {
					t.Fatal("the second host's row is not painted")
				}
				x, y := row.Bounds.X+row.Bounds.Width/2, row.Bounds.Y+row.Bounds.Height/2
				if err := view.Driver().ClickButton(x, y, gfx.PointerButtonSecondary); err != nil {
					t.Fatal(err)
				}
				hostFilterContextClick(t, view, hitHostMenuMove, false)
				if entry == "context-unchecked" {
					targets = []uint32{state.hosts[1].ID}
				}
			}
			if !slices.Equal(state.hostActionIDs, targets) {
				t.Fatalf("captured %v, want %v", state.hostActionIDs, targets)
			}
			id := hostMoveGroupNavigate(t, state, view, destination)
			hostMoveGroupShot(t, view, "move-group-compact-"+entry)
			hostFilterContextClick(t, view, id, false)
			if state.hostGroupMenu {
				t.Fatal("choosing the destination left the picker open")
			}
			for index := range before {
				if slices.Contains(targets, before[index].ID) {
					before[index].Group = destination
				}
			}
			if !reflect.DeepEqual(state.hosts, before) {
				t.Fatal("moving captured hosts changed another host or a connection field")
			}
			restarted := newTestState(t, state.options)
			if !reflect.DeepEqual(restarted.hosts, before) {
				t.Fatal("the full destination did not survive reload")
			}
		})
	}
}

func TestMoveHostGroupPreservesEveryDestinationAndActionIdentity(t *testing.T) {
	for _, destination := range []string{"A/Prod", "B/Prod", "A", "Empty", "Remove from group", "New group", "one/two/three/four/five/six/seven/eight/nine"} {
		t.Run(destination, func(t *testing.T) {
			state := newTestState(t, testOptions(t))
			for index := range state.hosts {
				state.hosts[index].Group = ""
			}
			for _, path := range []string{"A/Prod", "B/Prod", "Empty", state.messages.MoveGroupNone, state.messages.MoveGroupNew} {
				state.hostGroups = append(state.hostGroups, hostGroup{Name: path, Explicit: true, Expanded: true})
			}
			if strings.Count(destination, "/") > 7 {
				state.hostGroups = append(state.hostGroups, hostGroup{Name: destination, Explicit: true, Expanded: true})
			}
			state.hostDataChanged()
			view := hostFilterContextView(t, state, gfx.Size{Width: 1200, Height: 900})
			hostFilterContextClick(t, view, hitHostCheckList.At(int(state.hosts[0].ID)), false)
			hostFilterContextClick(t, view, hitMoveHostGroup, false)
			var id gfx.ID
			if state.moveGroupTree() == nil {
				// A too-deep path keeps the flat fallback and its keyboard scroll.
				if err := view.Keys("End"); err != nil {
					t.Fatal(err)
				}
				for _, hit := range view.Snapshot().Hits {
					if hit.ID.Scope() == hitHostGroupItemList && hit.Value == destination {
						id = hit.ID
					}
				}
			} else {
				id = hostMoveGroupNavigate(t, state, view, destination)
			}
			if id.IsZero() || id == hitHostGroupNone || id == hitHostGroupNew {
				t.Fatal("a named destination was lost or collided with a fixed command")
			}
			hostFilterContextClick(t, view, id, false)
			if state.hosts[0].Group != destination || state.modal != modalNone || state.hostGroupMenu {
				t.Fatalf("destination %q chose %q (modal=%v open=%v)", destination, state.hosts[0].Group, state.modal, state.hostGroupMenu)
			}
		})
	}
}

func TestMoveHostGroupKeyboardActionsAndDismissal(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.hosts[0].Group = "Source"
	state.hostGroups = append(state.hostGroups, hostGroup{Name: "Z/B/C/D/E/F/G/H", Explicit: true, Expanded: true})
	state.hostDataChanged()
	view := hostFilterContextView(t, state, gfx.Size{Width: 650, Height: 800})
	hostFilterContextClick(t, view, hitHostCheckList.At(int(state.hosts[0].ID)), false)
	hostFilterContextClick(t, view, hitMoveHostGroup, false)
	key := func(keys string) {
		t.Helper()
		if err := view.Keys(keys); err != nil {
			t.Fatal(err)
		}
	}
	// Typeahead, Right and Escape reach and leave a child without committing.
	// The driver's bare text helper requires an editor; menu text is a raw
	// application key, as sent by the native backend.
	if result := state.Key(gfx.KeyEvent{Text: "z"}); !result.Handled {
		t.Fatal("menu typeahead did not handle the printable key")
	}
	key("Right")
	if state.moveGroups.tree.Depth() != 1 {
		t.Fatal("typeahead and Right did not enter Z")
	}
	key("Escape")
	if !state.hostGroupMenu || state.moveGroups.tree.Depth() != 0 || state.hosts[0].Group != "Source" {
		t.Fatal("Escape did not return to the unchanged root")
	}
	key("Tab")
	if state.hostGroupMenu || state.hosts[0].Group != "Source" {
		t.Fatal("Tab did not dismiss without moving")
	}
	hostFilterContextClick(t, view, hitMoveHostGroup, false)
	key("Home")
	key("Enter")
	if state.hostGroupMenu || state.hosts[0].Group != "" {
		t.Fatal("Home/Enter did not invoke Remove from group")
	}
	hostFilterContextClick(t, view, hitMoveHostGroup, false)
	key("End")
	key("Enter")
	if state.modal != modalMoveHostGroup || state.hostGroupMenu || state.hosts[0].Group != "" || state.hostGroupEditor.String() != "" {
		t.Fatal("End/Enter did not open a fresh New group form without moving")
	}
	if err := view.Type("New/Child"); err != nil {
		t.Fatal(err)
	}
	key("Enter")
	if state.modal != modalNone || state.hosts[0].Group != "New/Child" {
		t.Fatal("New group did not preserve full-path submission")
	}
}

func TestMoveHostGroupScrollStaysModalAndCanReachLastAction(t *testing.T) {
	state := newTestState(t, testOptions(t))
	for index := range state.hosts {
		state.hosts[index].Group = ""
	}
	for index := 0; index < 50; index++ {
		state.hostGroups = append(state.hostGroups, hostGroup{Name: fmt.Sprintf("Group %02d/Child", index), Explicit: true})
	}
	state.hostDataChanged()
	view := hostFilterContextView(t, state, gfx.Size{Width: 650, Height: 500})
	hostFilterContextClick(t, view, hitHostCheckList.At(int(state.hosts[0].ID)), false)
	hostFilterContextClick(t, view, hitMoveHostGroup, false)
	before := state.hostsScroll
	if err := view.Driver().Scroll(gfx.ScrollEvent{X: 640, Y: 490, DeltaY: 1000}); err != nil {
		t.Fatal(err)
	}
	if state.hostsScroll != before || !state.hostGroupMenu {
		t.Fatal("outside wheel scrolled the host page or dismissed the picker")
	}
	bar := hostFilterContextHit(t, view, hitHostMoveGroupScroll)
	x, y := bar.Bounds.X+bar.Bounds.Width/2, bar.Bounds.Y+2
	if err := view.Driver().Press(x, y, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if err := view.Driver().Move(x, bar.Bounds.Y+bar.Bounds.Height-2); err != nil {
		t.Fatal(err)
	}
	if err := view.Driver().Release(x, bar.Bounds.Y+bar.Bounds.Height-2, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if !state.hostGroupMenu || state.hosts[0].Group != "" {
		t.Fatal("scrollbar drag dismissed the picker or chose a group")
	}
	if err := view.Keys("End"); err != nil {
		t.Fatal(err)
	}
	hostFilterContextClick(t, view, hitHostGroupNew, false)
	if state.modal != modalMoveHostGroup {
		t.Fatal("the last fixed action was not reachable after scrolling")
	}
}

func TestMoveHostGroupWheelOwnsWindowScroll(t *testing.T) {
	state := newTestState(t, testOptions(t))
	for index := range state.hosts {
		state.hosts[index].Group = ""
	}
	for index := range 50 {
		state.hostGroups = append(state.hostGroups, hostGroup{Name: fmt.Sprintf("Group %02d", index), Explicit: true})
	}
	state.hostDataChanged()
	view := openCatalogWindow(t, state)
	hostFilterContextClick(t, view, hitHostCheckList.At(int(state.hosts[0].ID)), false)
	hostFilterContextClick(t, view, hitMoveHostGroup, false)

	list := state.hostList.scroll
	if list == nil || state.hostsContent <= state.hostList.viewport.Height {
		t.Fatal("the host list must overflow behind the picker")
	}
	before := list.Offset
	first := hostFilterContextHit(t, view, hitHostGroupNone)
	x, y := first.Bounds.X+first.Bounds.Width/2, first.Bounds.Y+first.Bounds.Height/2
	if !pointInRect(x, y, state.hostList.viewport) {
		t.Fatal("the picker must overlap the scrollable host list")
	}
	if _, ok := findSnapshotHit(view.Snapshot(), hitHostGroupNew); ok {
		t.Fatal("the last action should require scrolling")
	}
	wheel := func(x, y, delta float32) {
		t.Helper()
		if err := view.Scroll(gfx.ScrollEvent{X: x, Y: y, DeltaY: delta}); err != nil {
			t.Fatal(err)
		}
		if list.Offset != before || !state.hostGroupMenu {
			t.Fatal("the wheel scrolled the host list or dismissed the picker")
		}
	}
	wheel(x, y, 2000)
	hostFilterContextHit(t, view, hitHostGroupNew)
	wheel(x, y, 2000) // At the end, the wheel still belongs to the picker.
	wheel(x, y, -2000)
	hostFilterContextHit(t, view, hitHostGroupNone)

	backdropX := state.hostList.viewport.X + state.hostList.viewport.Width - 8
	hit, ok := view.Driver().HitTest(backdropX, y)
	if !ok || hit != hitHostGroupDismiss {
		t.Fatal("the test point must hit the backdrop over the host list")
	}
	wheel(backdropX, y, 2000)
	hostFilterContextHit(t, view, hitHostGroupNone)
}
