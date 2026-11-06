package workbench

import (
	"slices"
	"strings"
	"testing"

	"github.com/vivaui/viva/ui"
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
		state.hostChipMenuKey(ui.KeyEvent{Key: ui.KeyEnter})
		if state.chipMenu.open || !state.hostFilterOn || state.hostFilter != name {
			t.Fatalf("choosing %q left the filter at %q (on %v, menu open %v)", name, state.hostFilter, state.hostFilterOn, state.chipMenu.open)
		}
	}
}

func TestHostGroupChipLabelIsTheLastSegment(t *testing.T) {
	state := &State{}
	for name, want := range map[string]string{"School": "School", "School/Gansu": "Gansu", "A / B": "B", "trailing/": "trailing/"} {
		if got := state.hostGroupChipLabel(hostGroup{Name: name}); got != want {
			t.Errorf("%q labelled %q, want %q", name, got, want)
		}
	}
}
