package workbench

import (
	"net/netip"
	"sort"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

type hostOrderMode uint8

const (
	hostOrderManual hostOrderMode = iota
	hostOrderName
	hostOrderIP
)

var (
	hostOrderHits       = tree.Feature("host-order")
	hitHostOrder        = tree.ID(hostOrderHits, "trigger")
	hitHostOrderDismiss = tree.ID(hostOrderHits, "dismiss")
	hitHostOrderManual  = tree.ID(hostOrderHits, "manual")
	hitHostOrderName    = tree.ID(hostOrderHits, "name")
	hitHostOrderIP      = tree.ID(hostOrderHits, "ip")
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
	state.hostSort.enabled = false
	state.hostSort.grid.Cancel()
	state.hostsScroll = 0
	state.visible = nil
	state.refreshHostGroups()
	state.syncVisible()
	state.save()
}

func compareHostName(left, right Host) int {
	if order := strings.Compare(strings.ToLower(left.Label), strings.ToLower(right.Label)); order != 0 {
		return order
	}
	if order := strings.Compare(left.Label, right.Label); order != 0 {
		return order
	}
	return strings.Compare(left.CatalogID, right.CatalogID)
}

func compareHostAddress(left, right Host) int {
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
	return compareHostName(left, right)
}

func (state *State) sortVisibleHosts() {
	if state.hostOrder == hostOrderManual {
		return
	}
	sort.Slice(state.visible, func(leftIndex, rightIndex int) bool {
		left, right := state.hosts[state.visible[leftIndex]], state.hosts[state.visible[rightIndex]]
		if state.hostOrder == hostOrderIP {
			return compareHostAddress(left, right) < 0
		}
		return compareHostName(left, right) < 0
	})
}

func (state *State) clickHostOrder(id gfx.ID) bool {
	if !state.hostOrderMenu && id != hitHostOrder {
		return false
	}
	if state.activeSection != sectionHosts || state.modal != modalNone {
		state.hostOrderMenu = false
		return false
	}
	if id == hitHostOrder {
		state.hostOrderMenu = !state.hostOrderMenu
		state.hostMenu.open, state.groupActions.open = false, false
		state.hostFilterMenu, state.hostGroupMenu, state.importMenu = false, false, false
		state.chipMenu.open = false
		return true
	}
	state.hostOrderMenu = false
	switch id {
	case hitHostOrderManual:
		state.setHostOrder(hostOrderManual)
	case hitHostOrderName:
		state.setHostOrder(hostOrderName)
	case hitHostOrderIP:
		state.setHostOrder(hostOrderIP)
	}
	return true
}

func (state *State) paintHostOrder(canvas *gfx.Canvas, viewport gfx.Rect) {
	if !state.hostOrderMenu || state.activeSection != sectionHosts || state.modal != modalNone {
		return
	}
	messages := state.messages.HostCatalog
	var rows menuRows
	for _, option := range [...]struct {
		id    gfx.ID
		label string
		mode  hostOrderMode
	}{
		{hitHostOrderManual, messages.Manual, hostOrderManual},
		{hitHostOrderName, messages.Name, hostOrderName},
		{hitHostOrderIP, messages.IP, hostOrderIP},
	} {
		rows.add(components.ContextMenuItem{ID: option.id, Label: option.label, Kind: components.ContextMenuRadio, Selected: state.hostOrder == option.mode})
	}
	width, height := rows.size(canvas, state.contextMenuTheme())
	menu := components.BeginContextMenu(canvas, state.contextMenuTheme(), state.popupBelow(canvas, state.hostOrderButton, width, height, 0), components.ContextMenuOptions{
		Layer: dialogLayer, Scope: dialogScope, DismissID: hitHostOrderDismiss, Viewport: viewport,
		Scroll: state.menuScrollFor(hitHostOrderDismiss), RowHeight: contextMenuRow,
	})
	state.menuPlaced(menu.Bounds())
	rows.paint(&menu, nil)
	menu.End()
}

func (state *State) paintHostOrderButton(canvas *gfx.Canvas, bounds gfx.Rect) {
	state.hostOrderButton = bounds
	style := iconButtonQuiet
	if state.hostOrder != hostOrderManual {
		style = iconButtonActive
	}
	state.paintIconButton(canvas, hitHostOrder, iconSliders, bounds, state.messages.HostCatalog.Order, false, style, state.pal.workspace)
}
