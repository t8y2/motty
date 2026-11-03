package workbench

import (
	"fmt"

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
	// No scrim dismiss: a press beside the form must not discard its input.
	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope, CloseID: hitProxyDialogClose,
		InitialFocusID: hitProxyDialogTarget, ContentSize: ui.Size{Width: 500}, FitBody: true, BodyHeight: withError(state.proxyDialogBodyHeight(), state.proxyDialogError),
	})
	layout := dialog.Begin(messages.ProxyDialogTitle, "")
	const rowHeight = 64
	var target, jump, via, viaHelp, relay flex.Box
	viaShell := state.proxyEditor.viaShell
	f := state.proxyDialogLayout.Begin(canvas, theme, layout.Body)
	f.Column().Fill().PadSides(2, 4, 0, 4).Children(func() {
		target = f.Slot().Height(rowHeight)
		jump = f.Slot().Height(rowHeight)
		via, viaHelp = state.declareDialogCheckboxHelp(f, messages.ProxyFieldViaShell, 4, 6)
		if viaShell {
			relay = f.Slot().Height(rowHeight)
			f.Paragraph(state.proxyRelayNote(), 2).FontSize(metrics.FontSizeS).Color(state.pal.muted).MarginSides(0, 6, 0, 6)
		}
		if state.proxyDialogError != "" {
			f.Text(state.proxyDialogError).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(state.pal.danger).MarginSides(6, 0, 0, 0)
		}
	})
	f.End()
	state.paintProxyHostField(canvas, target.Rect(), true)
	state.paintProxyHostField(canvas, jump.Rect(), false)
	state.paintDialogCheckbox(canvas, hitProxyDialogViaShell, messages.ProxyFieldViaShell, viaShell, via)
	state.paintHelpIcon(canvas, hitProxyDialogViaHelp, messages.ProxyViaShellHelp, viaHelp.Rect())
	if viaShell {
		content := components.BeginField(canvas, theme, relay.Rect()).Paint(messages.ProxyFieldRelay, "", "")
		value := state.proxyEditor.relay.String()
		focused := canvas.Focused() == hitProxyDialogRelay
		components.PaintInput(canvas, theme, content, components.InputSnapshot{
			ID: hitProxyDialogRelay, Label: messages.ProxyFieldRelay, Value: value, Placeholder: messages.ProxyRelayPlaceholder,
			Focused: focused, PlatformEditing: focused && value != "",
		})
	}
	dialog.Actions(layout,
		components.ButtonAction{ID: hitProxyDialogCancel, Label: messages.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitProxyDialogSubmit, Label: messages.ProxyCreate},
	)
	dialog.End()
}

// proxyDialogBodyHeight is the height of the dialog's fields: the two hosts, the
// choice of relaying through the jump host's shell, and when it is on the relay
// command with the note under it.
func (state *State) proxyDialogBodyHeight() float32 {
	height := float32(130 + 30) // two fields, then the checkbox
	if state.proxyEditor.viaShell {
		height += 64 + 44 // the relay field, then a note of two lines
	}
	return height
}

// proxyRelayNote says what is typed on the jump host before the relay starts.
func (state *State) proxyRelayNote() string {
	if jump, ok := state.hostByID(state.proxyEditor.jumpID); ok && jump.LoginCommand != "" {
		return fmt.Sprintf(state.messages.ProxyRelayPreludeFmt, jump.LoginCommand)
	}
	return state.messages.ProxyRelayNoPrelude
}

func (state *State) paintProxyHostField(canvas *ui.Canvas, bounds ui.Rect, target bool) {
	theme := state.Theme()
	label := state.messages.ProxyFieldJump
	triggerID, dismissID, itemBase := hitProxyDialogJump, hitProxyJumpDismiss, hitProxyJumpItemList
	selectedID, open := state.proxyEditor.jumpID, state.proxyEditor.jumpOpen
	if target {
		label = state.messages.ProxyFieldTarget
		triggerID, dismissID, itemBase = hitProxyDialogTarget, hitProxyTargetDismiss, hitProxyTargetItemList
		selectedID, open = state.proxyEditor.targetID, state.proxyEditor.targetOpen
	}
	content := components.BeginField(canvas, theme, bounds).Paint(label, "", "")
	selected, selectedOK := state.hostByID(selectedID)
	value, hint := "", ""
	if selectedOK {
		value, hint = selected.Label, selected.target
	}
	count := len(state.hosts)
	visible := min(count, proxyHostVisible)
	popupHeight := float32(8 + visible*32)
	// Add every host and let MenuScroll own the wheel, same pattern as the
	// transfer-center host select. The old page window only painted six rows and
	// wired ScrollUp/ScrollDown buttons, so trackpad gestures moved MenuScroll
	// over a list that already fit and never changed which hosts were shown.
	opening := open && state.popups.owner != dismissID
	scroll := state.selectScroll(open, dismissID)
	if opening && scroll != nil {
		selectedIndex := 0
		for index, host := range state.hosts {
			if host.ID == selectedID {
				selectedIndex = index
				break
			}
		}
		scroll.Offset = max(0, min(float32(selectedIndex)*32, float32(count)*32+8-popupHeight))
		// RequestFocus, not SetInitialFocus: the dialog claims its trigger as the
		// initial focus after this field paints, at the same trap depth, so an initial
		// focus set here would be overridden. A request is applied once, and the list
		// follows the focused row from then on.
		canvas.RequestFocus(itemBase.At(int(selectedID)))
	}
	selectControl := components.BeginSelect(canvas, theme, content, components.SelectOptions{
		TriggerID: triggerID, Label: label, Value: value, Hint: hint, Open: open, Invalid: !selectedOK,
		Content: ui.Rect{X: content.X, Y: content.Y + content.Height + 4, Width: content.Width, Height: popupHeight},
		Layer:   dialogLayer + 1, Scope: dialogScope, DismissID: dismissID, Viewport: state.windowRect(),
		Scroll: scroll,
	})
	defer selectControl.End()
	if open {
		state.menuPlaced(selectControl.ContentBounds())
	}
	for _, host := range state.hosts {
		disabled := !host.isSSH()
		if target && state.proxyTargetConfigured(host.ID) {
			disabled = true
		}
		selectControl.AddItem(components.SelectItem{
			ID: itemBase.At(int(host.ID)), Label: host.Label, Value: host.target,
			Selected: host.ID == selectedID, Disabled: disabled,
		})
	}
}
