package workbench

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

type networkRuleListIDs struct {
	search, viewport, thumb, all gfx.ID
	rows, checks                 gfx.Scope
}

var (
	forwardListIDs          = networkRuleIDs(forwardsHits, hitForwardRowList)
	proxyListIDs            = networkRuleIDs(proxiesHits, hitProxyRowList)
	hitNetworkRemoveClose   = tree.ID(shellHits, "network-remove-close")
	hitNetworkRemoveCancel  = tree.ID(shellHits, "network-remove-cancel")
	hitNetworkRemoveConfirm = tree.ID(shellHits, "network-remove-confirm")
)

func networkRuleIDs(scope, rows gfx.Scope) networkRuleListIDs {
	return networkRuleListIDs{
		search: tree.ID(scope, "rule-search"), viewport: tree.ID(scope, "rule-scroll"),
		thumb: tree.ID(scope, "rule-scroll-thumb"), all: tree.ID(scope, "rule-check-all"),
		rows: rows, checks: tree.Sub(scope, tree.Name("rule-check")),
	}
}

type networkRuleRow struct {
	id          uint32
	cells       [6]string
	selectLabel string
}

type networkRuleList struct {
	search   text.Editor
	query    string
	rows     []networkRuleRow
	checked  map[uint32]bool
	scroll   float32
	geometry components.ScrollAreaGeometry
	dragging bool
	grab     float32
}

func (list *networkRuleList) beginRows() {
	query := strings.ToLower(strings.TrimSpace(list.search.String()))
	if query != list.query {
		list.query = query
		list.scroll = 0
		clear(list.checked)
	}
	for id := range list.checked {
		list.checked[id] = false
	}
	clear(list.rows)
	list.rows = list.rows[:0]
	list.geometry = components.ScrollAreaGeometry{}
}

func (list *networkRuleList) addRow(id uint32, cells []string, extra, selectLabel string) {
	if list.query == "" || strings.Contains(strings.ToLower(extra), list.query) ||
		slices.ContainsFunc(cells, func(cell string) bool {
			return strings.Contains(strings.ToLower(cell), list.query)
		}) {
		row := networkRuleRow{id: id, selectLabel: selectLabel}
		copy(row.cells[1:], cells)
		list.rows = append(list.rows, row)
		if _, checked := list.checked[id]; checked {
			list.checked[id] = true
		}
	}
}

func (list *networkRuleList) finishRows() {
	for id, visible := range list.checked {
		if !visible {
			delete(list.checked, id)
		}
	}
}

func (list *networkRuleList) contains(id uint32) bool {
	return slices.ContainsFunc(list.rows, func(row networkRuleRow) bool { return row.id == id })
}

func (list *networkRuleList) selection(single uint32) (uint32, bool) {
	switch len(list.checked) {
	case 0:
		return single, single != 0 && (list.query == "" || list.contains(single))
	case 1:
		for id := range list.checked {
			return id, true
		}
	}
	return 0, false
}

func (list *networkRuleList) toggle(id uint32) {
	if list.checked[id] {
		delete(list.checked, id)
	} else if list.contains(id) {
		if list.checked == nil {
			list.checked = make(map[uint32]bool)
		}
		list.checked[id] = true
	}
}

func (list *networkRuleList) toggleAll() {
	if len(list.checked) == len(list.rows) {
		clear(list.checked)
		return
	}
	if list.checked == nil {
		list.checked = make(map[uint32]bool, len(list.rows))
	}
	for _, row := range list.rows {
		list.checked[row.id] = true
	}
}

func (state *State) activeNetworkRuleList() (*networkRuleList, networkRuleListIDs) {
	switch state.activeSection {
	case sectionForwards:
		return &state.forwardList, forwardListIDs
	case sectionProxies:
		return &state.proxyList, proxyListIDs
	}
	return nil, networkRuleListIDs{}
}

func (state *State) networkRuleListKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if event.Release {
		return gfx.EventResult{}, false
	}
	if state.modal == modalRemoveNetworkRules && event.Key == gfx.KeyEnter &&
		event.Target == hitNetworkRemoveConfirm && !event.Repeat {
		state.clickNetworkRemove(hitNetworkRemoveConfirm)
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	list, ids := state.activeNetworkRuleList()
	if list == nil || state.modal != modalNone {
		return gfx.EventResult{}, false
	}
	switch {
	case event.Target == ids.search && event.Key == gfx.KeyEscape && list.search.RuneCount() > 0:
		list.search = text.Editor{}
	case event.Target == ids.viewport || event.Target == ids.thumb:
		step := components.TableRowHeight(state.Theme())
		area := components.BeginScrollArea(nil, state.Theme(), list.geometry.Bounds,
			gfx.Size{Height: float32(len(list.rows)) * step})
		_, offset, ok := area.ScrollForKey(0, list.scroll, event.Key, step)
		if !ok {
			return gfx.EventResult{}, false
		}
		list.scroll = offset
	case !event.Repeat && (event.Key == gfx.KeySpace || event.Key == gfx.KeyEnter):
		switch {
		case event.Target == ids.all:
			list.toggleAll()
		case event.Target.Scope() == ids.checks:
			list.toggle(uint32(item(event.Target)))
		case event.Target.Scope() == ids.rows:
			state.selectNetworkRule(list, uint32(item(event.Target)))
		default:
			return gfx.EventResult{}, false
		}
	default:
		return gfx.EventResult{}, false
	}
	return gfx.EventResult{Handled: true, Repaint: true}, true
}

func (state *State) networkRuleListPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	list, ids := state.activeNetworkRuleList()
	if list == nil || state.modal != modalNone {
		return gfx.EventResult{}, false
	}
	if list.dragging {
		switch event.Kind {
		case gfx.PointerMove, gfx.PointerUp:
			list.scroll = list.geometry.VerticalOffsetForThumb(event.Y - list.grab)
			list.dragging = event.Kind != gfx.PointerUp
			return gfx.EventResult{Handled: true, Repaint: true}, true
		case gfx.PointerExit:
			list.dragging = false
		}
		return gfx.EventResult{Handled: true}, true
	}
	if event.Kind == gfx.PointerDown && event.Button == gfx.PointerButtonPrimary && event.Target == ids.thumb {
		list.dragging = true
		list.grab = event.Y - list.geometry.VerticalThumb.Y
		if !pointInRect(event.X, event.Y, list.geometry.VerticalThumb) {
			list.grab = list.geometry.VerticalThumb.Height / 2
			list.scroll = list.geometry.VerticalOffsetForThumb(event.Y - list.grab)
		}
		return gfx.EventResult{Handled: true, Repaint: true, Focus: ids.viewport}, true
	}
	return gfx.EventResult{}, false
}

func (list *networkRuleList) wheel(event gfx.ScrollEvent) gfx.EventResult {
	if !pointInRect(event.X, event.Y, list.geometry.Bounds) {
		return gfx.EventResult{}
	}
	before := list.scroll
	list.scroll = min(max(0, before+event.StepY(36)), list.geometry.MaxOffsetY)
	return gfx.EventResult{Handled: true, Repaint: list.scroll != before}
}

func (state *State) selectNetworkRule(list *networkRuleList, id uint32) {
	if !list.contains(id) {
		return
	}
	clear(list.checked)
	if list == &state.forwardList {
		state.selectForward(id)
	} else {
		state.selectProxy(id)
	}
}

type networkRuleRemoval struct {
	forwards bool
	ids      map[uint32]bool
}

func (state *State) askRemoveNetworkRules(list *networkRuleList) {
	state.networkRuleRemoval = networkRuleRemoval{forwards: list == &state.forwardList, ids: maps.Clone(list.checked)}
	state.modal = modalRemoveNetworkRules
}

func (state *State) declareNetworkRuleRemoval(c ui.C) {
	m := state.messages
	title, descriptionFmt := m.ProxyRemove, m.ProxyRemoveSelectionFmt
	if state.networkRuleRemoval.forwards {
		title, descriptionFmt = m.ForwardRemove, m.ForwardRemoveSelectionFmt
	}
	description := fmt.Sprintf(descriptionFmt, len(state.networkRuleRemoval.ids))
	viewport := dialogViewport(c)
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: title, Description: description, Width: 470,
		Height: dialogContentHeight(c.Canvas(), *c.Theme(), viewport, 470, description, 0),
		Layer:  dialogLayer, Scope: dialogScope, CloseID: hitNetworkRemoveClose, InitialFocusID: hitNetworkRemoveCancel,
	})
	dialog.Footer(func() {
		state.dialogActionFooter(c,
			components.ButtonAction{ID: hitNetworkRemoveCancel, Label: m.Cancel, Variant: components.ButtonSecondary},
			components.ButtonAction{ID: hitNetworkRemoveConfirm, Label: m.DeleteHostConfirm, Variant: components.ButtonDestructive},
		)
	})
}

func (state *State) clickNetworkRemove(id gfx.ID) {
	switch id {
	case hitNetworkRemoveClose, hitNetworkRemoveCancel:
		state.closeModal()
	case hitNetworkRemoveConfirm:
		removal := state.networkRuleRemoval
		state.closeModal()
		count := 0
		if removal.forwards {
			for i := range state.forwards {
				if removal.ids[state.forwards[i].ID] {
					state.forwards[i].stop()
				}
			}
			count = len(state.forwards)
			state.forwards = slices.DeleteFunc(state.forwards, func(rule forwardRecord) bool { return removal.ids[rule.ID] })
			count -= len(state.forwards)
			clear(state.forwardList.checked)
			if removal.ids[state.selectedForwardID] {
				state.selectedForwardID = 0
			}
			state.refreshForwardCount()
		} else {
			count = len(state.proxies)
			state.proxies = slices.DeleteFunc(state.proxies, func(rule proxyRecord) bool { return removal.ids[rule.ID] })
			count -= len(state.proxies)
			clear(state.proxyList.checked)
			if removal.ids[state.selectedProxyID] {
				state.selectedProxyID = 0
			}
			state.sshSyncMark()
			state.refreshProxies()
		}
		state.status = fmt.Sprintf(state.messages.NetworkRulesRemovedFmt, count)
		state.save()
	}
}

func (state *State) paintNetworkRuleSearch(canvas *gfx.Canvas, bounds gfx.Rect, list *networkRuleList, id gfx.ID, placeholder string) {
	value := list.search.String()
	components.PaintInputGroup(canvas, state.Theme(), bounds, components.InputGroupSnapshot{
		ID: id, Label: state.messages.NavSearch, Value: value, Placeholder: placeholder,
		NativeEditing: canvas.Focused() == id && value != "",
		InlineStart:   components.InputGroupSlot{Kind: components.InputGroupSlotIcon, Icon: lucideIcons[iconSearch]},
	})
}

func (state *State) paintNetworkRuleTable(canvas *gfx.Canvas, bounds gfx.Rect, list *networkRuleList, ids networkRuleListIDs,
	label string, columns []components.TableColumn, headings []string, selected uint32) {
	theme := state.Theme()
	rowHeight, headerHeight := components.TableRowHeight(theme), components.TableHeaderHeight(theme)
	body := gfx.Rect{X: bounds.X, Y: bounds.Y + headerHeight, Width: bounds.Width, Height: bounds.Height - headerHeight}
	if body.Width <= 0 || body.Height <= 0 {
		return
	}
	height := float32(len(list.rows)) * rowHeight
	area := components.BeginScrollArea(canvas, theme, body, gfx.Size{Height: height}).
		Offset(0, list.scroll).Viewport(ids.viewport, label).VerticalScrollbar(ids.thumb, label, "")
	geometry := area.Geometry()
	width := body.Width - geometry.VerticalTrack.Width
	checkWidth, _ := components.CheckboxHitExtent(theme)
	columns[0].Width = checkWidth
	components.FitTableHeadings(canvas, theme, columns, headings)
	// Share the remaining width across the text columns so every column has
	// room at narrow window sizes, including the rightmost status or route.
	fixed := float32(0)
	for _, column := range columns[1 : len(columns)-1] {
		fixed += column.Width
	}
	available := max(0, width-checkWidth-96)
	if fixed > available {
		for i := 1; i < len(columns)-1; i++ {
			columns[i].Width = max(1, columns[i].Width*available/fixed)
		}
	}
	header := components.BeginTable(canvas, theme,
		gfx.Rect{X: bounds.X, Y: bounds.Y, Width: width, Height: headerHeight}, columns)
	header.Header(headings)
	checkSize := components.CheckboxExtent(theme)
	checkX := bounds.X + (checkWidth-checkSize)/2
	all := components.BeginCheckbox(canvas, theme, ids.all,
		gfx.Rect{X: checkX, Y: bounds.Y + (headerHeight-checkSize)/2}, len(list.checked) == len(list.rows)).
		Label(state.messages.NetworkRulesSelectAll)
	if len(list.checked) > 0 && len(list.checked) < len(list.rows) {
		all = all.Indeterminate()
	}
	all.Paint()

	geometry = area.BeginContent()
	list.geometry, list.scroll = geometry, geometry.OffsetY
	start := int(geometry.OffsetY / rowHeight)
	table := components.BeginTable(canvas, theme, gfx.Rect{
		X: body.X, Y: geometry.Content.Y + float32(start)*rowHeight,
		Width: width, Height: float32(len(list.rows)-start) * rowHeight,
	}, columns)
	for index := start; index < len(list.rows); index++ {
		y := geometry.Content.Y + float32(index)*rowHeight
		if y >= body.Y+body.Height {
			break
		}
		row := list.rows[index]
		checked := list.checked[row.id]
		table.Row(ids.rows.At(int(row.id)), row.cells[:len(columns)], checked || row.id == selected)
		components.BeginCheckbox(canvas, theme, ids.checks.At(int(row.id)),
			gfx.Rect{X: checkX, Y: y + (rowHeight-checkSize)/2}, checked).
			Label(row.selectLabel).Paint()
	}
	area.EndContent()
}
