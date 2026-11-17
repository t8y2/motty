package workbench

import (
	"net/netip"
	"sort"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text/collation"
	"github.com/vivaui/viva/ui"
	textlanguage "golang.org/x/text/language"
	"motty/internal/i18n"
)

type hostOrderMode uint8

const (
	hostOrderManual hostOrderMode = iota
	hostOrderName
	hostOrderIP
)

var (
	hostOrderHits = tree.Feature("host-order")
	hitHostOrder  = tree.ID(hostOrderHits, "trigger")
)

func hostOrderFromString(value string) hostOrderMode {
	switch value {
	case "name":
		return hostOrderName
	case "ip":
		return hostOrderIP
	default:
		return hostOrderManual
	}
}

func (mode hostOrderMode) saved() string {
	switch mode {
	case hostOrderName:
		return "name"
	case hostOrderIP:
		return "ip"
	default:
		return ""
	}
}

func (state *State) setHostOrder(mode hostOrderMode) {
	state.hostOrderMenu = false
	if state.hostOrder == mode {
		return
	}
	state.hostOrder = mode
	state.endHostSort()
	state.hostsScroll = 0
	state.visible = nil
	state.refreshHostGroups()
	state.syncVisible()
	state.save()
}

// hostNameCollator reads names the way the interface's language does, so
// Chinese sorts by pinyin rather than by code point.
func (state *State) hostNameCollator() *collation.Collator {
	switch state.language {
	case i18n.Chinese:
		return collation.New(textlanguage.SimplifiedChinese)
	case i18n.TraditionalChinese:
		return collation.New(textlanguage.TraditionalChinese)
	default:
		return collation.New(textlanguage.English)
	}
}

func compareHostName(collator *collation.Collator, left, right Host) int {
	if order := collator.Compare(left.Label, right.Label); order != 0 {
		return order
	}
	// Labels that collate the same, such as two spellings of one normalized
	// name, still need one fixed order.
	if order := strings.Compare(left.Label, right.Label); order != 0 {
		return order
	}
	return strings.Compare(left.CatalogID, right.CatalogID)
}

func compareHostAddress(collator *collation.Collator, left, right Host) int {
	leftAddress, leftError := netip.ParseAddr(strings.Trim(left.Address, "[]"))
	rightAddress, rightError := netip.ParseAddr(strings.Trim(right.Address, "[]"))
	switch {
	case leftError == nil && rightError == nil:
		if order := leftAddress.Unmap().Compare(rightAddress.Unmap()); order != 0 {
			return order
		}
	case leftError == nil:
		return -1
	case rightError == nil:
		return 1
	default:
		if order := strings.Compare(strings.ToLower(left.Address), strings.ToLower(right.Address)); order != 0 {
			return order
		}
	}
	return compareHostName(collator, left, right)
}

func (state *State) sortVisibleHosts() { state.sortHostIndexes(state.visible) }

// sortHostIndexes puts the hosts at indexes in the order the user chose for the
// list; the manual order is the order of the catalog, which they already have.
func (state *State) sortHostIndexes(indexes []int) {
	if state.hostOrder == hostOrderManual {
		return
	}
	collator := state.hostNameCollator()
	sort.Slice(indexes, func(leftIndex, rightIndex int) bool {
		left, right := state.hosts[indexes[leftIndex]], state.hosts[indexes[rightIndex]]
		if state.hostOrder == hostOrderIP {
			return compareHostAddress(collator, left, right) < 0
		}
		return compareHostName(collator, left, right) < 0
	})
}

func (state *State) clickHostOrder(id gfx.ID) bool {
	if id != hitHostOrder {
		return false
	}
	if state.activeSection != sectionHosts || state.modal != modalNone {
		state.hostOrderMenu = false
		return false
	}
	state.hostOrderMenu = !state.hostOrderMenu
	state.hostPrivacy.menu = false
	state.hostMenu.open, state.groupActions.open = false, false
	state.hostFilterMenu, state.hostGroupMenu, state.importMenu = false, false, false
	state.chipMenu.open = false
	return true
}

// declareHostOrderMenu declares the display-order menu under the trigger the
// page declared: the three orders, the current one chosen.
func (state *State) declareHostOrderMenu(c ui.C, anchor ui.Box) {
	if !state.hostOrderMenu {
		return
	}
	if state.activeSection != sectionHosts || state.modal != modalNone {
		state.hostOrderMenu = false
		return
	}
	messages := state.messages.HostCatalog
	ui.PopupLayer(c, dialogLayer)
	ui.Menu(c, anchor, &state.hostOrderMenu, func(menu *ui.MenuItems) {
		menu.RowHeight(contextMenuRow)
		for _, option := range [...]struct {
			label string
			mode  hostOrderMode
		}{
			{messages.Manual, hostOrderManual},
			{messages.Name, hostOrderName},
			{messages.IP, hostOrderIP},
		} {
			if menu.Item(option.label).Chosen(state.hostOrder == option.mode).Clicked() {
				c.After(func() { state.setHostOrder(option.mode) })
			}
		}
	})
}
