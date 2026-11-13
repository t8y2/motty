package workbench

import (
	"slices"
	"testing"

	"motty/internal/i18n"
)

func chineseNameOrderState(t *testing.T) *State {
	t.Helper()
	state := newTestState(t, freshOptions(t))
	state.setLanguage(i18n.Chinese)
	state.hosts = []Host{
		{ID: 1, CatalogID: "host_quan", Label: "泉州职业技术大学", Group: "高校/35-福建省"},
		{ID: 2, CatalogID: "host_fu", Label: "福州大学", Group: "高校/35-福建省"},
		{ID: 3, CatalogID: "host_bit", Label: "北京理工大学", Group: "高校/11-北京市"},
		{ID: 4, CatalogID: "host_bau", Label: "北京农学院", Group: "高校/11-北京市"},
		{ID: 5, CatalogID: "host_ucas", Label: "中国科学院大学", Group: "高校/11-北京市"},
	}
	state.nextHostID = 6
	for index := range state.hosts {
		state.hosts[index].finish()
	}
	state.hostDataChanged()
	chooseDisplayOrder(state, "name")
	return state
}

func TestHostDisplayOrderSortsChineseNamesByPinyin(t *testing.T) {
	state := chineseNameOrderState(t)
	if got, want := displayHostIDs(state), []string{"host_bit", "host_bau", "host_ucas", "host_fu", "host_quan"}; !slices.Equal(got, want) {
		t.Fatalf("pinyin host order = %v, want %v", got, want)
	}
	var groups []string
	for _, group := range state.hostGroups {
		groups = append(groups, group.Name)
	}
	if want := []string{"高校", "高校/11-北京市", "高校/35-福建省"}; !slices.Equal(groups, want) {
		t.Fatalf("pinyin group order = %v, want %v", groups, want)
	}
}

func TestHostDisplayOrderSortsTopLevelGroupsByPinyin(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.setLanguage(i18n.Chinese)
	for _, name := range []string{"个人", "临时", "公司", "合作", "托管", "高校"} {
		state.hostGroups = append(state.hostGroups, hostGroup{Name: name, Explicit: true, Expanded: true})
	}
	chooseDisplayOrder(state, "name")
	var names []string
	for _, group := range state.hostGroups {
		names = append(names, group.Name)
	}
	if want := []string{"高校", "个人", "公司", "合作", "临时", "托管"}; !slices.Equal(names, want) {
		t.Fatalf("top-level group order = %v, want %v", names, want)
	}
}

func TestHostDisplayOrderFollowsALanguageChange(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.hosts = []Host{
		{ID: 1, CatalogID: "host_quan", Label: "泉州职业技术大学", Group: "Servers"},
		{ID: 2, CatalogID: "host_fu", Label: "福州大学", Group: "Servers"},
	}
	state.nextHostID = 3
	for index := range state.hosts {
		state.hosts[index].finish()
	}
	state.hostDataChanged()
	chooseDisplayOrder(state, "name")
	before := displayHostIDs(state)
	if slices.Equal(before, []string{"host_fu", "host_quan"}) {
		t.Fatalf("English order already reads as pinyin: %v", before)
	}
	state.setLanguage(i18n.Chinese)
	if got, want := displayHostIDs(state), []string{"host_fu", "host_quan"}; !slices.Equal(got, want) {
		t.Fatalf("language change did not resort hosts: %v, want %v", got, want)
	}
}

func TestHostNameCollatorFollowsTheInterfaceLanguage(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.language = i18n.Chinese
	if order := state.hostNameCollator().Compare("福州", "泉州"); order >= 0 {
		t.Fatalf("Chinese does not sort by pinyin: %d", order)
	}
	state.language = i18n.TraditionalChinese
	if order := state.hostNameCollator().Compare("泉州", "福州"); order >= 0 {
		t.Fatalf("Traditional Chinese does not sort by stroke count: %d", order)
	}
	state.language = i18n.English
	if order := state.hostNameCollator().Compare("福州", "泉州"); order <= 0 {
		t.Fatalf("English no longer follows the root order: %d", order)
	}
}

func TestHostDisplayOrderIsDeterministicForCanonicallyEqualLabels(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.hosts = []Host{
		{ID: 1, CatalogID: "host_composed", Label: "é", Group: "Servers"},
		{ID: 2, CatalogID: "host_decomposed", Label: "é", Group: "Servers"},
	}
	state.nextHostID = 3
	for index := range state.hosts {
		state.hosts[index].finish()
	}
	state.hostDataChanged()
	chooseDisplayOrder(state, "name")
	first := displayHostIDs(state)
	slices.Reverse(state.hosts)
	state.hostDataChanged()
	if got := displayHostIDs(state); !slices.Equal(got, first) {
		t.Fatalf("equal labels depend on input order: %v, want %v", got, first)
	}
}
