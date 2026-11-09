package workbench

import (
	"fmt"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

func (state *State) paintProxiesWorkspace(canvas *gfx.Canvas, bounds gfx.Rect) {
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
	labeled := toolbarButtonWidth(canvas, messages.ProxyRemove, false) + gap + toolbarButtonWidth(canvas, messages.ProxyEdit, false) +
		gap + toolbarButtonWidth(canvas, actionLabel, false) + gap + toolbarButtonWidth(canvas, messages.ProxyAdd, true) + gap + titleLeast
	condensed := labeled > bounds.Width-36
	removeWidth, removeIcon := toolbarButtonLayout(canvas, messages.ProxyRemove, 0, iconTrash, condensed)
	editWidth, editIcon := toolbarButtonLayout(canvas, messages.ProxyEdit, 0, iconPencil, condensed)
	actionWidth, realActionIcon := toolbarButtonLayout(canvas, actionLabel, 0, actionIcon, condensed)
	addWidth, addIcon := toolbarButtonLayout(canvas, messages.ProxyAdd, iconPlus, iconPlus, condensed)

	var content, emptyIcon, emptyAdd, remove, edit, action, add ui.Box
	f := state.proxyLayout.Begin(canvas, state.Theme(), bounds)
	f.Column().Fill().PadSides(12, 18, 0, 18).Children(func() {
		f.Row().MinHeight(58).Gap(gap).MarginSides(0, 0, 12, 0).Items(ui.Start).Children(func() {
			f.Column().Grow(1).Gap(3).MarginSides(4, 0, 0, 0).Children(func() {
				f.Text(messages.ProxyTitle).FontSize(18).Weight(gfx.WeightSemibold).Color(pal.text).Ellipsis()
				f.Paragraph(messages.ProxyDescription, 2).FontSize(metrics.FontSizeS).Color(pal.muted)
			})
			remove = f.Slot().Size(removeWidth, toolbarHeight)
			edit = f.Slot().Size(editWidth, toolbarHeight)
			action = f.Slot().Size(actionWidth, toolbarHeight)
			add = f.Slot().Size(addWidth, toolbarHeight)
		})
		if len(state.proxies) == 0 {
			emptyIcon, emptyAdd = state.declareEmptyPage(f, bounds, messages.ProxyEmpty, messages.ProxyEmptySub, messages.ProxyAdd)
		} else {
			content = f.Slot().Grow(1).MarginSides(0, 0, 9, 0)
		}
		f.Divider().Background(pal.line)
		f.Row().Height(50).PadSides(8, 0, 20, 0).Items(ui.Center).Children(func() {
			f.Text(state.proxyCountText).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.faint).Width(162)
			if hasSelection {
				f.Text(state.proxyLabel(selected)).FontSize(metrics.FontSizeS).Color(pal.muted).Grow(1)
			}
		})
	})
	f.End()

	state.paintToolbarButton(canvas, hitProxyRemove, remove.Rect().X, remove.Rect().Y, removeWidth, messages.ProxyRemove, removeIcon, false, hasSelection, condensed)
	state.paintToolbarButton(canvas, hitProxyEdit, edit.Rect().X, edit.Rect().Y, editWidth, messages.ProxyEdit, editIcon, false, hasSelection, condensed)
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

func (state *State) paintProxyDialog(canvas *gfx.Canvas, viewport gfx.Rect) {
	messages, theme := state.messages, state.Theme()
	// No scrim dismiss: a press beside the form must not discard its input.
	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope, CloseID: hitProxyDialogClose,
		InitialFocusID: hitProxyDialogTarget, ContentSize: gfx.Size{Width: 500}, FitBody: true, BodyHeight: withError(state.proxyDialogBodyHeight(), state.proxyDialogError),
	})
	dialogTitle := messages.ProxyDialogTitle
	submitLabel := messages.ProxyCreate
	if state.proxyEditor.editing {
		dialogTitle = messages.ProxyEditDialogTitle
		submitLabel = messages.ProxyRouteSave
	}
	layout := dialog.Begin(dialogTitle, "")
	const rowHeight = 64
	var target, jump, via, viaHelp, relay ui.Box
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
			f.Text(state.proxyDialogError).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(state.pal.danger).MarginSides(6, 0, 0, 0)
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
		components.ButtonAction{ID: hitProxyDialogSubmit, Label: submitLabel},
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

// paintProxyHostField paints one searchable host picker. Typing in the field
// filters the list by name, address, account and group, so a long catalog is
// still reachable without scrolling.
func (state *State) paintProxyHostField(canvas *gfx.Canvas, bounds gfx.Rect, target bool) {
	theme := state.Theme()
	label := state.messages.ProxyFieldJump
	inputID, triggerID, dismissID := hitProxyDialogJump, hitProxyJumpTrigger, hitProxyJumpDismiss
	scrollID, itemBase := hitProxyJumpScroll, hitProxyJumpItemList
	scope := hitProxyJumpPickerScope
	control := &state.proxyEditor.jumpSelect
	if target {
		label = state.messages.ProxyFieldTarget
		inputID, triggerID, dismissID = hitProxyDialogTarget, hitProxyTargetTrigger, hitProxyTargetDismiss
		scrollID, itemBase = hitProxyTargetScroll, hitProxyTargetItemList
		scope = hitProxyTargetPickerScope
		control = &state.proxyEditor.targetSelect
	}
	content := components.BeginField(canvas, theme, bounds).Paint(label, "", "")
	layout := components.PaintSingleSelect(canvas, theme, content, control, components.SingleSelectOptions{
		InputID: inputID, TriggerID: triggerID, DismissID: dismissID, Scope: scope,
		ScrollID: scrollID, OptionIDs: itemBase,
		Label: label, Placeholder: state.messages.HostPickerSearch,
		EmptyLabel: state.messages.HostPickerNoMatches,
		Viewport:   state.windowRect(), Layer: dialogLayer + 1,
	})
	if target {
		state.proxyEditor.targetPopup = layout.Popup
		return
	}
	state.proxyEditor.jumpPopup = layout.Popup
}
