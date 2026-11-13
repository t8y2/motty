package workbench

import (
	"fmt"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// proxySectionUI is what the proxies page's declaration leaves for its paint.
type proxySectionUI struct {
	remove, edit, action, add, empty              ui.Box
	removeWidth, editWidth, actionWidth, addWidth float32
	removeIcon, editIcon, actionIcon, addIcon     uint8
	actionLabel                                   string
	condensed                                     bool
	// draw is the page hook that paints the empty state. It is kept so a paint
	// does not build a closure for the hook.
	draw func(*ui.DrawContext)
}

// declareProxiesWorkspace declares the proxies page in the workbench page: its
// header, its open/empty body and its footer. The buttons, the table and the
// empty state's icon are painted into the slots it leaves.
func (state *State) declareProxiesWorkspace(c ui.C) {
	canvas := c.Canvas()
	pal, messages := state.pal, state.messages
	state.syncProxyTable()
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
	bounds := state.catalogSectionBounds()
	labeled := toolbarButtonWidth(canvas, messages.ProxyRemove, false) + gap + toolbarButtonWidth(canvas, messages.ProxyEdit, false) +
		gap + toolbarButtonWidth(canvas, actionLabel, false) + gap + toolbarButtonWidth(canvas, messages.ProxyAdd, true) + gap + titleLeast
	condensed := labeled > bounds.Width-36
	removeWidth, removeIcon := toolbarButtonLayout(canvas, messages.ProxyRemove, 0, iconTrash, condensed)
	editWidth, editIcon := toolbarButtonLayout(canvas, messages.ProxyEdit, 0, iconPencil, condensed)
	actionWidth, realActionIcon := toolbarButtonLayout(canvas, actionLabel, 0, actionIcon, condensed)
	addWidth, addIcon := toolbarButtonLayout(canvas, messages.ProxyAdd, iconPlus, iconPlus, condensed)

	canAdd := state.canAddJumpRoute()
	view := &state.sectionBodies.proxies
	draw := view.draw
	*view = proxySectionUI{
		removeWidth: removeWidth, editWidth: editWidth, actionWidth: actionWidth, addWidth: addWidth,
		removeIcon: removeIcon, editIcon: editIcon, actionIcon: realActionIcon, addIcon: addIcon,
		actionLabel: actionLabel, condensed: condensed,
	}
	if draw == nil {
		draw = func(d *ui.DrawContext) { state.paintProxiesWorkspace(d.Canvas) }
	}
	view.draw = draw
	body := ui.Column(c).Fill().PadSides(12, 18, 0, 18)
	body.Children(func() {
		ui.Row(c).MinHeight(58).Gap(gap).MarginSides(0, 0, 12, 0).Items(ui.Start).Children(func() {
			ui.Column(c).Grow(1).Gap(3).MarginSides(4, 0, 0, 0).Children(func() {
				ui.Text(c, messages.ProxyTitle).FontSize(18).Weight(gfx.WeightSemibold).Color(pal.text).Ellipsis()
				ui.Paragraph(c, messages.ProxyDescription, 2).FontSize(metrics.FontSizeS).Color(pal.muted)
			})
			view.remove = state.declareButtonSized(c, hitProxyRemove, messages.ProxyRemove, removeIcon, false, false, hasSelection, toolbarFont, condensed).Size(removeWidth, toolbarHeight)
			view.edit = state.declareButtonSized(c, hitProxyEdit, messages.ProxyEdit, editIcon, false, false, hasSelection, toolbarFont, condensed).Size(editWidth, toolbarHeight)
			view.action = state.declareButtonSized(c, hitProxyStartStop, actionLabel, realActionIcon, false, false, hasSelection, toolbarFont, condensed).Size(actionWidth, toolbarHeight)
			view.add = state.declareButtonSized(c, hitProxyAdd, messages.ProxyAdd, addIcon, true, false, canAdd, toolbarFont, condensed).Size(addWidth, toolbarHeight)
		})
		if len(state.proxies) == 0 {
			view.empty = state.reserveEmptyPage(c, bounds, messages.ProxyEmpty, messages.ProxyEmptySub, messages.ProxyAdd, hitProxyAdd, iconGlobe, canAdd)
		} else {
			state.proxyColumns = [...]components.TableColumn{{Width: 210}, {Width: 210}, {Width: 120}, {Width: 0}}
			state.proxyHeadings = [...]string{messages.ProxyColumnTarget, messages.ProxyColumnJump, messages.ProxyColumnStatus, messages.ProxyColumnRoute}
			components.FitTableHeadings(canvas, state.Theme(), state.proxyColumns[:], state.proxyHeadings[:])
			table := ui.Table(c, &state.proxyTable, ui.TableSpec{
				Rows: hitProxyRowList, Columns: state.proxyColumns[:], Headers: state.proxyHeadings[:], Source: &state.proxyRows,
				Scrollbar: hitProxyScrollThumb, ScrollbarLabel: messages.ProxyListScroll,
				ScrollbarGutter: catalogBarGutter,
			}).Grow(1).Shrink(1)
			if row, ok := table.RowClicked(); ok && state.modal == modalNone && row >= 0 && row < len(state.proxies) {
				state.proxyTable.Select(row)
				state.selectProxy(state.proxies[row].ID)
			}
		}
		ui.Divider(c).Background(pal.line)
		ui.Row(c).Height(50).PadSides(8, 0, 20, 0).Items(ui.Center).Children(func() {
			ui.Text(c, state.proxyCountText).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.faint).Width(162)
			if hasSelection {
				ui.Text(c, state.proxyLabel(selected)).FontSize(metrics.FontSizeS).Color(pal.muted).Grow(1)
			}
		})
	})
	body.Clip().DrawOver(view.draw)
}

// paintProxiesWorkspace paints the proxies page's buttons, table and empty state
// into the slots its declaration left.
func (state *State) paintProxiesWorkspace(canvas *gfx.Canvas) {
	view := &state.sectionBodies.proxies
	if len(state.proxies) == 0 {
		state.drawReservedEmptyPage(canvas, view.empty, iconGlobe)
	}
}

// declareProxyDialog declares the route editor as a dialog of the workbench
// page.
func (state *State) declareProxyDialog(c ui.C) {
	messages := state.messages
	dialogTitle := messages.ProxyDialogTitle
	submitLabel := messages.ProxyCreate
	if state.proxyEditor.editing {
		dialogTitle = messages.ProxyEditDialogTitle
		submitLabel = messages.ProxyRouteSave
	}
	bodyHeight := withError(state.proxyDialogBodyHeight(), state.proxyDialogError)
	viewport := dialogViewport(c)
	height := dialogContentHeight(c.Canvas(), *c.Theme(), viewport, 500, "", bodyHeight)
	// No scrim dismiss: a press beside the form must not discard its input.
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: dialogTitle, Width: 500, Height: height, Layer: dialogLayer,
		Scope: dialogScope, CloseID: hitProxyDialogClose, InitialFocusID: hitProxyDialogTarget,
	})
	dialog.Body(func() { state.declareProxyDialogBody(c) })
	dialog.Footer(func() {
		if dialogButton(c, messages.Cancel, components.ButtonSecondary, false).Clicked() {
			c.After(func() { state.clickProxyDialog(hitProxyDialogCancel) })
		}
		if dialogButton(c, submitLabel, components.ButtonDefault, false).Clicked() {
			c.After(func() { state.clickProxyDialog(hitProxyDialogSubmit) })
		}
	})
}

// declareProxyDialogBody lays out the route dialog: the target and jump hosts,
// the shell-relay box and, when it is on, the relay command and its note.
func (state *State) declareProxyDialogBody(c ui.C) {
	editor, messages := &state.proxyEditor, state.messages
	viaShell := editor.viaShell
	b := &editor.dialogBody
	*b = proxyDialogBody{}
	const rowHeight = 64
	column := ui.Column(c).Fill().PadSides(2, 4, 0, 4)
	column.Children(func() {
		b.target = ui.Slot(c).Height(rowHeight)
		b.jump = ui.Slot(c).Height(rowHeight)
		b.via, b.viaHelp = state.declareDialogCheckboxHelpC(c, messages.ProxyFieldViaShell, 4, 6)
		if viaShell {
			b.relay = ui.Slot(c).Height(rowHeight)
			ui.Paragraph(c, state.proxyRelayNote(), 2).FontSize(metrics.FontSizeS).Color(state.pal.muted).MarginSides(0, 6, 0, 6)
		}
		if state.proxyDialogError != "" {
			ui.Text(c, state.proxyDialogError).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(state.pal.danger).MarginSides(6, 0, 0, 0)
		}
	})
	column.DrawOver(func(d *ui.DrawContext) { state.paintProxyDialogFields(d.Canvas) })
}

// paintProxyDialogFields paints the route editor's controls into the slots the
// body laid out.
func (state *State) paintProxyDialogFields(canvas *gfx.Canvas) {
	messages, theme := state.messages, state.Theme()
	b := &state.proxyEditor.dialogBody
	state.paintProxyHostField(canvas, b.target.Rect(), true)
	state.paintProxyHostField(canvas, b.jump.Rect(), false)
	state.paintDialogCheckbox(canvas, hitProxyDialogViaShell, messages.ProxyFieldViaShell, state.proxyEditor.viaShell, b.via)
	state.paintHelpIcon(canvas, hitProxyDialogViaHelp, messages.ProxyViaShellHelp, b.viaHelp.Rect())
	if state.proxyEditor.viaShell {
		content := components.BeginField(canvas, theme, b.relay.Rect()).Paint(messages.ProxyFieldRelay, "", "")
		value := state.proxyEditor.relay.String()
		focused := canvas.Focused() == hitProxyDialogRelay
		components.PaintInput(canvas, theme, content, components.InputSnapshot{
			ID: hitProxyDialogRelay, Label: messages.ProxyFieldRelay, Value: value, Placeholder: messages.ProxyRelayPlaceholder,
			Focused: focused, PlatformEditing: focused && value != "",
		})
	}
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
