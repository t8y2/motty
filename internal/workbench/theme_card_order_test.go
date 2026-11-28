package workbench

import (
	"reflect"
	"testing"
)

func TestCustomThemeCardsKeepOrderWhenSelected(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.setTerminalScheme("dracula")
	var ids []string
	for _, name := range []string{"Violet copy", "Blue copy", "Green copy"} {
		scheme := state.resolvedScheme("dracula")
		scheme.id, scheme.name = "", name
		id, err := state.saveCustomScheme(scheme)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	cards := func() []string {
		var result []string
		for _, index := range state.schemeQuickPicks() {
			id, _, _ := state.schemeAt(index)
			result = append(result, id)
		}
		return result
	}
	before := cards()
	for _, id := range []string{ids[2], ids[0], "nord", ids[1]} {
		state.setTerminalScheme(id)
		if got := cards(); !reflect.DeepEqual(got, before) {
			t.Fatalf("selecting %q reordered cards: %v; want %v", id, got, before)
		}
	}
}
