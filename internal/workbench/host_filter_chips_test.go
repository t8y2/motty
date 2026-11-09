package workbench

import (
	"slices"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
)

func chipTestState() *State {
	return &State{hostGroups: []hostGroup{
		{Name: "School", Digits: "3"}, {Name: "School/Gansu", Digits: "2"}, {Name: "School/Gansu/Lanzhou", Digits: "1"},
		{Name: "School/Gansu/Tianshui", Digits: "1"}, {Name: "School/Xinjiang", Digits: "1"}, {Name: "Office", Digits: "1"},
		{Name: "orphan/leaf", Digits: "1"},
	}}
}

func chipNames(state *State) []string {
	state.fitChipGroups()
	names := make([]string, 0, len(state.hostChips))
	for _, index := range state.hostChips {
		names = append(names, state.hostGroups[index].Name)
	}
	return names
}

// The row keeps its shape whichever group is chosen: only top-level groups have
// a chip, and a group whose parent is missing counts as top level.
func TestHostFilterChipsAreAlwaysTheTopLevel(t *testing.T) {
	state := chipTestState()
	want := []string{"School", "Office", "orphan/leaf"}
	for _, filter := range []struct {
		on   bool
		name string
	}{{false, ""}, {true, "School"}, {true, "School/Gansu/Tianshui"}, {true, "Office"}} {
		state.hostFilterOn, state.hostFilter = filter.on, filter.name
		if got := chipNames(state); !slices.Equal(got, want) {
			t.Fatalf("with filter %+v chips %q, want %q", filter, got, want)
		}
	}
}

// A chip is chosen by its group or any group below it, and then names the nested
// group after its own, with that group's count.
func TestHostChipViewNamesTheChosenNestedGroup(t *testing.T) {
	state := chipTestState()
	school, office := state.hostGroupIndex("School"), state.hostGroupIndex("Office")
	for _, test := range []struct {
		name         string
		on           bool
		filter       string
		label        string
		digits       string
		chosen       bool
		officeChosen bool
	}{
		{"nothing chosen", false, "", "School", "3", false, false},
		{"the group itself", true, "School", "School", "3", true, false},
		{"a nested group", true, "School/Gansu", "School › Gansu", "2", true, false},
		{"a deeply nested group", true, "School/Gansu/Tianshui", "School › Tianshui", "1", true, false},
		{"another top-level group", true, "Office", "School", "3", false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			state.hostFilterOn, state.hostFilter = test.on, test.filter
			label, digits, chosen := state.hostChipView(school)
			if label != test.label || digits != test.digits || chosen != test.chosen {
				t.Fatalf("School chip %q %q chosen %v, want %q %q %v", label, digits, chosen, test.label, test.digits, test.chosen)
			}
			if _, _, chosen := state.hostChipView(office); chosen != test.officeChosen {
				t.Fatalf("Office chip chosen %v, want %v", chosen, test.officeChosen)
			}
		})
	}
}

// Every nested group is reachable from its top-level chip's menu, and choosing it
// there filters to the full path.
func TestHostChipMenuReachesEveryNestedGroup(t *testing.T) {
	state := chipTestState()
	if state.hostChipHasMenu(state.hostGroupIndex("Office")) {
		t.Fatal("a group with nothing nested has a menu")
	}
	root := state.hostGroupIndex("School")
	if !state.hostChipHasMenu(root) {
		t.Fatal("School has nested groups but no menu")
	}
	for _, name := range []string{"School/Gansu", "School/Gansu/Lanzhou", "School/Gansu/Tianshui", "School/Xinjiang"} {
		state.chipMenu.open = false
		state.openHostChipMenu(root)
		if !state.chipMenu.open {
			t.Fatal("the menu did not open")
		}
		state.chipMenu.tree.Seek(strings.TrimPrefix(name, "School/"))
		state.hostChipMenuKey(gfx.KeyEvent{Key: gfx.KeyEnter})
		if state.chipMenu.open || !state.hostFilterOn || state.hostFilter != name {
			t.Fatalf("choosing %q left the filter at %q (on %v, menu open %v)", name, state.hostFilter, state.hostFilterOn, state.chipMenu.open)
		}
	}
}

// An open chip menu is modal: the wheel must not reach the hosts list behind it,
// wherever the pointer is. The menu itself decides which of its panels scroll.
func TestAnOpenChipMenuKeepsTheWheelOffTheHostsList(t *testing.T) {
	state := manyHostsState(t, 40)
	state.activeSection = sectionHosts
	size := gfx.Size{Width: 1400, Height: 900}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	if state.hostsContent <= state.chrome.hostList.Height {
		t.Fatalf("the hosts list does not scroll at all (content %v, view %v)", state.hostsContent, state.chrome.hostList.Height)
	}
	state.chipMenu.open = true
	before := state.hostsScroll
	result := state.Scroll(gfx.ScrollEvent{DeltaY: 120, Precise: true, X: 600, Y: 400, Viewport: size})
	if !result.Handled {
		t.Fatal("the open chip menu left the wheel unhandled")
	}
	if state.hostsScroll != before {
		t.Fatalf("the hosts list scrolled behind the open menu: %v -> %v", before, state.hostsScroll)
	}
}

// A pinned chip makes room for its star instead of taking it out of the name:
// the label keeps its natural width and stays inside the chip. A marker the
// width forgot would squeeze the text, which is what this guards.
func TestPinnedHostChipLeavesRoomForItsMarker(t *testing.T) {
	state := nestedHostGroupState(t)
	index := state.hostGroupIndex("Team")
	state.hostGroups[index].Pinned = true
	canvas, _ := paintedTexts(state)
	chip, ok := hitByID(canvas, hitHostFilterList.At(index))
	if !ok {
		t.Fatal("the pinned group has no chip")
	}
	label := state.hostGroupChipLabel(state.hostGroups[index])
	painted, ok := textByValue(canvas.Texts(), label, chip.Bounds)
	if !ok {
		t.Fatalf("the pinned chip painted no %q", label)
	}
	natural := canvas.TextWidthWeight(label, metrics.FontSizeS, gfx.WeightSemibold)
	if painted.Bounds.Width < natural-0.5 {
		t.Fatalf("the pinned chip squeezed %q to %.1f of its %.1f natural width", label, painted.Bounds.Width, natural)
	}
	if painted.Bounds.X < chip.Bounds.X || painted.Bounds.X+painted.Bounds.Width > chip.Bounds.X+chip.Bounds.Width {
		t.Fatalf("%q at %+v spills out of its chip %+v", label, painted.Bounds, chip.Bounds)
	}
}

// textByValue is the painted text with that value on the row the bounds name, so
// a label shared by several controls is matched to the one asked about.
func textByValue(texts []gfx.Text, value string, row gfx.Rect) (gfx.Text, bool) {
	for _, text := range texts {
		if text.Value == value && text.Bounds.Y >= row.Y && text.Bounds.Y+text.Bounds.Height <= row.Y+row.Height {
			return text, true
		}
	}
	return gfx.Text{}, false
}

func TestHostGroupChipLabelIsTheLastSegment(t *testing.T) {
	state := &State{}
	for name, want := range map[string]string{"School": "School", "School/Gansu": "Gansu", "A / B": "B", "trailing/": "trailing/"} {
		if got := state.hostGroupChipLabel(hostGroup{Name: name}); got != want {
			t.Errorf("%q labelled %q, want %q", name, got, want)
		}
	}
}
