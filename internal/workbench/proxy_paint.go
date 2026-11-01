package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"
)

func (state *State) paintProxiesWorkspace(canvas *ui.Canvas, bounds ui.Rect) {
	pal, messages := state.pal, state.messages
	canAdd := state.sshHostCount() >= 2
	selected, hasSelection := state.proxyByID(state.selectedProxyID)
	actionLabel := messages.ProxyStart
	actionIcon := uint8(iconPlay)
	if hasSelection && !selected.Stopped {
		actionLabel = messages.ProxyStop
		actionIcon = iconStop
	}
	// The title keeps a readable column beside the buttons; a card too narrow for
	// both shows the buttons as their icons, whose tooltips name them.
	const (
		titleLeast = float32(220)
		gap        = float32(10)
	)
	labeled := toolbarButtonWidth(canvas, messages.ProxyRemove, false) + gap + toolbarButtonWidth(canvas, actionLabel, false) +
		gap + toolbarButtonWidth(canvas, messages.ProxyAdd, true) + gap + titleLeast
	condensed := labeled > bounds.Width-36
	removeWidth, removeIcon := toolbarButtonLayout(canvas, messages.ProxyRemove, 0, iconTrash, condensed)
	actionWidth, realActionIcon := toolbarButtonLayout(canvas, actionLabel, 0, actionIcon, condensed)
	addWidth, addIcon := toolbarButtonLayout(canvas, messages.ProxyAdd, iconPlus, iconPlus, condensed)

	var content, emptyIcon, emptyAdd, remove, action, add flex.Box
	f := state.proxyLayout.Begin(canvas, state.Theme(), bounds)
	f.Column().Fill().PadSides(12, 18, 0, 18).Children(func() {
		f.Row().MinHeight(58).Gap(gap).MarginSides(0, 0, 12, 0).Items(flex.Start).Children(func() {
			f.Column().Grow(1).Gap(3).MarginSides(4, 0, 0, 0).Children(func() {
				f.Text(messages.ProxyTitle).FontSize(18).Weight(ui.WeightSemibold).Color(pal.text).Ellipsis()
				f.Paragraph(messages.ProxyDescription, 2).FontSize(metrics.FontSizeS).Color(pal.muted)
			})
			remove = f.Slot().Size(removeWidth, toolbarHeight)
			action = f.Slot().Size(actionWidth, toolbarHeight)
			add = f.Slot().Size(addWidth, toolbarHeight)
		})
		if len(state.proxies) == 0 {
			emptyIcon, emptyAdd = state.declareEmptyPage(f, bounds, messages.ProxyEmpty, messages.ProxyEmptySub, messages.ProxyAdd)
		} else {
			content = f.Slot().Grow(1).MarginSides(0, 0, 9, 0)
		}
		f.Divider().Background(pal.line)
		f.Row().Height(50).PadSides(8, 0, 20, 0).Items(flex.Center).Children(func() {
			f.Text(state.proxyCountText).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(pal.faint).Width(162)
			if hasSelection {
				f.Text(state.proxyLabel(selected)).FontSize(metrics.FontSizeS).Color(pal.muted).Grow(1)
			}
		})
	})
	f.End()

	state.paintToolbarButton(canvas, hitProxyRemove, remove.Rect().X, remove.Rect().Y, removeWidth, messages.ProxyRemove, removeIcon, false, hasSelection, condensed)
	state.paintToolbarButton(canvas, hitProxyStartStop, action.Rect().X, action.Rect().Y, actionWidth, actionLabel, realActionIcon, false, hasSelection, condensed)
	state.paintToolbarButton(canvas, hitProxyAdd, add.Rect().X, add.Rect().Y, addWidth, messages.ProxyAdd, addIcon, true, canAdd, condensed)

	if len(state.proxies) == 0 {
		state.paintEmptyPage(canvas, emptyIcon, emptyAdd, iconGlobe, hitProxyAdd, messages.ProxyAdd, canAdd)
		return
	}
	columns := [...]components.TableColumn{{Width: 210}, {Width: 210}, {Width: 120}, {Width: 0}}
	headings := [...]string{messages.ProxyColumnTarget, messages.ProxyColumnJump, messages.ProxyColumnStatus, messages.ProxyColumnRoute}
	components.FitTableHeadings(canvas, state.Theme(), columns[:], headings[:])
	table := components.BeginTable(canvas, state.Theme(), content.Rect(), columns[:])
	table.Header(headings[:])
	for _, route := range state.proxies {
		if table.RowAt(hitProxyRowList, int(route.ID), route.cells[:], route.ID == state.selectedProxyID) && state.modal == modalNone {
			state.selectProxy(route.ID)
		}
	}
}

func (state *State) paintProxyDialog(canvas *ui.Canvas, viewport ui.Rect) {
	messages, theme := state.messages, state.Theme()
	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope, DismissID: hitProxyDialogDismiss, CloseID: hitProxyDialogClose,
		InitialFocusID: hitProxyDialogTarget, ContentSize: ui.Size{Width: 500}, FitBody: true, BodyHeight: withError(130, state.proxyDialogError), // two fields
	})
	layout := dialog.Begin(messages.ProxyDialogTitle, "")
	const rowHeight = 64
	var target, jump flex.Box
	f := state.proxyDialogLayout.Begin(canvas, theme, layout.Body)
	f.Column().Fill().PadSides(2, 4, 0, 4).Children(func() {
		target = f.Slot().Height(rowHeight)
		jump = f.Slot().Height(rowHeight)
		if state.proxyDialogError != "" {
			f.Text(state.proxyDialogError).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(state.pal.danger).MarginSides(6, 0, 0, 0)
		}
	})
	f.End()
	state.paintProxyHostField(canvas, target.Rect(), true)
	state.paintProxyHostField(canvas, jump.Rect(), false)
	dialog.Actions(layout,
		components.ButtonAction{ID: hitProxyDialogCancel, Label: messages.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitProxyDialogSubmit, Label: messages.ProxyCreate},
	)
	dialog.End()
}

func (state *State) paintProxyHostField(canvas *ui.Canvas, bounds ui.Rect, target bool) {
	theme := state.Theme()
	label := state.messages.ProxyFieldJump
	triggerID, dismissID, itemBase := hitProxyDialogJump, hitProxyJumpDismiss, hitProxyJumpItemList
	selectedID, open, offset := state.proxyEditor.jumpID, state.proxyEditor.jumpOpen, state.proxyEditor.jumpOffset
	if target {
		label = state.messages.ProxyFieldTarget
		triggerID, dismissID, itemBase = hitProxyDialogTarget, hitProxyTargetDismiss, hitProxyTargetItemList
		selectedID, open, offset = state.proxyEditor.targetID, state.proxyEditor.targetOpen, state.proxyEditor.targetOffset
	}
	content := components.BeginField(canvas, theme, bounds).Paint(label, "", "")
	selected, selectedOK := state.hostByID(selectedID)
	value, hint := "", ""
	if selectedOK {
		value, hint = selected.Label, selected.target
	}
	count := len(state.hosts)
	visible := min(count, proxyHostPageSize)
	popupHeight := float32(8 + visible*32)
	if count > proxyHostPageSize {
		popupHeight += 48
	}
	selectControl := components.BeginSelect(canvas, theme, content, components.SelectOptions{
		TriggerID: triggerID, Label: label, Value: value, Hint: hint, Open: open, Invalid: !selectedOK,
		Content: ui.Rect{X: content.X, Y: content.Y + content.Height + 4, Width: content.Width, Height: popupHeight},
		Layer:   dialogLayer + 1, Scope: dialogScope, DismissID: dismissID, Viewport: state.windowRect(),
		Scroll: state.selectScroll(open, dismissID),
	})
	defer selectControl.End()
	if open {
		state.menuPlaced(selectControl.ContentBounds())
	}
	if count > proxyHostPageSize {
		upID := hitProxyJumpScrollUp
		if target {
			upID = hitProxyTargetScrollUp
		}
		selectControl.AddScrollUp(upID, offset == 0)
	}
	end := min(offset+proxyHostPageSize, count)
	for index := offset; index < end; index++ {
		host := state.hosts[index]
		disabled := !host.isSSH()
		if target && state.proxyTargetConfigured(host.ID) {
			disabled = true
		}
		selectControl.AddItem(components.SelectItem{
			ID: itemBase.At(int(host.ID)), Label: host.Label, Value: host.target,
			Selected: host.ID == selectedID, Disabled: disabled,
		})
	}
	if count > proxyHostPageSize {
		downID := hitProxyJumpScrollDown
		if target {
			downID = hitProxyTargetScrollDown
		}
		selectControl.AddScrollDown(downID, end == count)
	}
}
