package workbench

import (
	"slices"
	"testing"
)

func chipNames(state *State) []string {
	state.fitChipGroups()
	names := make([]string, 0, len(state.hostChips))
	for _, index := range state.hostChips {
		names = append(names, state.hostGroups[index].Name)
	}
	return names
}

// Nested groups do not all string along one row: the row is the path to the
// level in view followed by that level's groups, and a chosen group with none of
// its own leaves its siblings in view.
func TestHostFilterChipsFollowTheChosenLevel(t *testing.T) {
	state := &State{hostGroups: []hostGroup{
		{Name: "School"}, {Name: "School/Gansu"}, {Name: "School/Gansu/Lanzhou"},
		{Name: "School/Gansu/Tianshui"}, {Name: "School/Xinjiang"}, {Name: "Office"},
	}}
	for _, test := range []struct {
		name   string
		on     bool
		filter string
		want   []string
	}{
		{"nothing chosen shows the top level", false, "", []string{"School", "Office"}},
		{"a top-level group with groups of its own opens it", true, "School", []string{"School", "School/Gansu", "School/Xinjiang"}},
		{"a deeper group opens with its path", true, "School/Gansu", []string{"School", "School/Gansu", "School/Gansu/Lanzhou", "School/Gansu/Tianshui"}},
		{"a group with none stays on its siblings", true, "School/Gansu/Tianshui", []string{"School", "School/Gansu", "School/Gansu/Lanzhou", "School/Gansu/Tianshui"}},
		{"a top-level group with none stays on the top level", true, "Office", []string{"School", "Office"}},
		{"a group that is gone shows the top level", true, "Gone", []string{"School", "Office"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			state.hostFilterOn, state.hostFilter = test.on, test.filter
			if got := chipNames(state); !slices.Equal(got, test.want) {
				t.Fatalf("chips %q, want %q", got, test.want)
			}
		})
	}
}

// Every group stays reachable by clicking chips: from the top level down, each
// group is a chip once a chip for its parent has been chosen.
func TestHostFilterChipsReachEveryGroup(t *testing.T) {
	state := &State{hostGroups: []hostGroup{
		{Name: "A"}, {Name: "A/B"}, {Name: "A/B/C"}, {Name: "A/D"}, {Name: "E"}, {Name: "E/F"}, {Name: "orphan/leaf"},
	}}
	reached := map[string]bool{}
	var visit func(filter string, on bool)
	visit = func(filter string, on bool) {
		state.hostFilterOn, state.hostFilter = on, filter
		for _, name := range chipNames(state) {
			if !reached[name] {
				reached[name] = true
				visit(name, true)
			}
		}
	}
	visit("", false)
	for _, group := range state.hostGroups {
		if !reached[group.Name] {
			t.Errorf("group %q has no chip on any path from the top level", group.Name)
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
