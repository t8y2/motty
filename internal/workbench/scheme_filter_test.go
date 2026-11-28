package workbench

import (
	"fmt"
	"reflect"
	"testing"
)

func TestSchemeCopiesStayInThirdRowNewestFirst(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	var ids []string
	for i := 0; i < 6; i++ {
		scheme := state.resolvedScheme("dracula")
		scheme.id, scheme.name = "", fmt.Sprintf("Copy %d", i)
		id, err := state.saveCustomScheme(scheme)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	state.setTerminalScheme(ids[0])
	picks := state.schemeQuickPicks()
	if len(picks) != 12 {
		t.Fatalf("got %d cards, want three rows", len(picks))
	}
	var third []string
	for _, index := range picks[8:] {
		id, _, _ := state.schemeAt(index)
		third = append(third, id)
	}
	want := []string{ids[5], ids[4], ids[3], ids[2]}
	if !reflect.DeepEqual(third, want) {
		t.Fatalf("third row %v, want %v", third, want)
	}
	if len(state.userSchemeIndices()) != 6 {
		t.Fatal("overflow copies missing from full collection")
	}
	state.scheme.category = schemeFilterMine
	state.openSchemeBrowser()
	if state.scheme.category != schemeFilterAll {
		t.Fatal("browser did not default to All")
	}
}

func TestSchemeCategoryAndSearchFilters(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	scheme := state.resolvedScheme("dracula")
	scheme.id, scheme.name = "", "My violet copy"
	id, err := state.saveCustomScheme(scheme)
	if err != nil {
		t.Fatal(err)
	}
	for category := schemeFilterAll; category <= schemeFilterMine; category++ {
		state.scheme.category = category
		indices := state.filteredSchemeIndices("")
		if len(indices) == 0 {
			t.Fatalf("category %d has no cards", category)
		}
		if category == schemeFilterAll || category == schemeFilterMine {
			first, _, _ := state.schemeAt(indices[0])
			if first != id {
				t.Fatal("my theme is not first")
			}
		}
		for _, index := range indices {
			_, _, s := state.schemeAt(index)
			switch category {
			case schemeFilterBuiltin:
				if index >= schemeCount {
					t.Fatal("non-builtin in builtins")
				}
			case schemeFilterCommunityDark, schemeFilterCommunityLight:
				if index < schemeCount || index >= schemeCount+len(communitySchemes) {
					t.Fatal("non-community card")
				}
				if (luminance(s.background) < .5) != (category == schemeFilterCommunityDark) {
					t.Fatal("wrong brightness category")
				}
			}
		}
	}
	state.scheme.category = schemeFilterMine
	matches := state.filteredSchemeIndices("violet copy")
	if len(matches) != 1 {
		t.Fatalf("combined filter returned %d cards", len(matches))
	}
}
