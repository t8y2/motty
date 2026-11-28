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
		return state.themeText("Built-in", "内置", "內建")
	case schemeFilterCommunityDark:
		return state.themeText("Community dark", "社区深色", "社群深色")
	case schemeFilterCommunityLight:
		return state.themeText("Community light", "社区浅色", "社群淺色")
	case schemeFilterMine:
		return state.themeText("Mine", "我的", "我的")
	default:
		return state.themeText("All", "全部", "全部")
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
