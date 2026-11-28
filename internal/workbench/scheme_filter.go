package workbench

import "github.com/vivaui/viva/ui"

type schemeCategory uint8

const (
	schemeFilterAll schemeCategory = iota
	schemeFilterBuiltin
	schemeFilterCommunityDark
	schemeFilterCommunityLight
	schemeFilterMine
)

type schemeDrawerGroup struct {
	category schemeCategory
	title    string
	indices  []int
}

func (state *State) schemeCategoryLabel(category schemeCategory) string {
	switch category {
	case schemeFilterBuiltin:
		return state.messages.Themes.BuiltIn
	case schemeFilterCommunityDark:
		return state.messages.Themes.CommunityDark
	case schemeFilterCommunityLight:
		return state.messages.Themes.CommunityLight
	case schemeFilterMine:
		return state.messages.Themes.Mine
	default:
		return state.messages.Themes.All
	}
}

func (state *State) declareSchemeCategoryFilter(c ui.C) {
	ui.Grid(c, 128).FillWidth().Gap(6).MarginBottom(10).Children(func() {
		for category := schemeFilterAll; category <= schemeFilterMine; category++ {
			button := ui.Button(c, state.schemeCategoryLabel(category)).FillWidth()
			if state.scheme.category == category {
				button.Secondary()
			} else {
				button.Outline()
			}
			if button.Clicked() {
				state.scheme.category = category
				state.scheme.reset = true
			}
		}
	})
}

func (state *State) schemeDrawerGroups() []schemeDrawerGroup {
	m := state.messages.Tools
	builtins := make([]int, schemeCount)
	for i := range builtins {
		builtins[i] = i
	}
	dark, light := state.communitySchemeIndices()
	return []schemeDrawerGroup{
		{schemeFilterMine, m.SchemeMine, state.userSchemeIndices()},
		{schemeFilterBuiltin, m.SchemeBuiltin, builtins},
		{schemeFilterCommunityDark, m.SchemeCommunity + " · " + m.SchemeDark, dark},
		{schemeFilterCommunityLight, m.SchemeCommunity + " · " + m.SchemeLight, light},
	}
}

// 分类与名称搜索组合生效，搜索结果也保持“我的”优先。
func (state *State) filteredSchemeIndices(filter string) []int {
	var matches []int
	for _, group := range state.schemeDrawerGroups() {
		if state.scheme.category != schemeFilterAll && state.scheme.category != group.category {
			continue
		}
		for _, index := range group.indices {
			_, name, _ := state.schemeAt(index)
			if schemeMatches(name, filter) {
				matches = append(matches, index)
			}
		}
	}
	return matches
}
