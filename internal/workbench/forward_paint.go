package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/portforward"
)

// forwardSectionUI is what the forwards page's declaration leaves for its paint.
type forwardSectionUI struct {
	remove, edit, action, add, empty              ui.Box
	removeWidth, editWidth, actionWidth, addWidth float32
	removeIcon, editIcon, actionIcon, addIcon     uint8
	actionLabel                                   string
	condensed                                     bool
	// draw is the page hook that paints the empty state. It is kept so a paint
	// does not build a closure for the hook.
	draw func(*ui.DrawContext)
}

// declareForwardsWorkspace declares the forwards page in the workbench page:
// its header, its open/empty body and its footer. The buttons, the table and the
// empty state's icon are painted into the slots it leaves.
func (state *State) declareForwardsWorkspace(c ui.C) {
	canvas := c.Canvas()
	pal, messages := state.pal, state.messages
	state.syncForwardTable()
	selected, hasSelection := state.forwardByID(state.selectedForwardID)
	actionLabel := messages.ForwardStart
	actionIcon := uint8(iconPlay)
	running := selected.runner != nil || selected.pending || selected.starting
	if running {
		actionLabel = messages.ForwardStop
		actionIcon = iconStop
	}
	// The title keeps a readable column beside the buttons; a card too narrow for
	// both shows the buttons as their icons, whose tooltips name them.
	const (
		titleLeast = float32(220)
		gap        = float32(10)
	)
	bounds := state.catalogSectionBounds()
	labeled := toolbarButtonWidth(canvas, messages.ForwardRemove, false) + gap + toolbarButtonWidth(canvas, messages.ForwardEdit, false) +
		gap + toolbarButtonWidth(canvas, actionLabel, false) + gap + toolbarButtonWidth(canvas, messages.ForwardAdd, true) + gap + titleLeast
	condensed := labeled > bounds.Width-36
	removeWidth, removeIcon := toolbarButtonLayout(canvas, messages.ForwardRemove, 0, iconTrash, condensed)
	editWidth, editIcon := toolbarButtonLayout(canvas, messages.ForwardEdit, 0, iconPencil, condensed)
	actionWidth, realActionIcon := toolbarButtonLayout(canvas, actionLabel, 0, actionIcon, condensed)
	addWidth, addIcon := toolbarButtonLayout(canvas, messages.ForwardAdd, iconPlus, iconPlus, condensed)
	detailColor := pal.muted
	if selected.lastError != "" {
		detailColor = pal.danger
	}
	canAdd := state.sshHostCount() > 0
	view := &state.sectionBodies.forwards
	draw := view.draw
	*view = forwardSectionUI{
		removeWidth: removeWidth, editWidth: editWidth, actionWidth: actionWidth, addWidth: addWidth,
		removeIcon: removeIcon, editIcon: editIcon, actionIcon: realActionIcon, addIcon: addIcon,
		actionLabel: actionLabel, condensed: condensed,
	}
	if draw == nil {
		draw = func(d *ui.DrawContext) { state.paintForwardsWorkspace(d.Canvas) }
	}
	view.draw = draw
	body := ui.Column(c).Fill().PadSides(12, 18, 0, 18)
	body.Children(func() {
		ui.Row(c).MinHeight(58).Gap(gap).MarginSides(0, 0, 12, 0).Items(ui.Start).Children(func() {
			ui.Column(c).Grow(1).Gap(3).MarginSides(4, 0, 0, 0).Children(func() {
				ui.Text(c, messages.ForwardTitle).FontSize(18).Weight(gfx.WeightSemibold).Color(pal.text).Ellipsis()
				ui.Paragraph(c, messages.ForwardDescription, 2).FontSize(metrics.FontSizeS).Color(pal.muted)
			})
			view.remove = state.declareButtonSized(c, hitForwardRemove, messages.ForwardRemove, removeIcon, false, false, hasSelection, toolbarFont, condensed).Size(removeWidth, toolbarHeight)
			view.edit = state.declareButtonSized(c, hitForwardEdit, messages.ForwardEdit, editIcon, false, false, hasSelection, toolbarFont, condensed).Size(editWidth, toolbarHeight)
			view.action = state.declareButtonSized(c, hitForwardStartStop, actionLabel, realActionIcon, false, false, hasSelection, toolbarFont, condensed).Size(actionWidth, toolbarHeight)
			view.add = state.declareButtonSized(c, hitForwardAdd, messages.ForwardAdd, addIcon, true, false, canAdd, toolbarFont, condensed).Size(addWidth, toolbarHeight)
		})
		if len(state.forwards) == 0 {
			view.empty = state.reserveEmptyPage(c, bounds, messages.ForwardEmpty, messages.ForwardEmptySub, messages.ForwardAdd, hitForwardAdd, iconForward, canAdd)
		} else {
			state.forwardColumns = [...]components.TableColumn{{Width: 180}, {Width: 160}, {Width: 170}, {Width: 210}, {Width: 0}}
			state.forwardHeadings = [...]string{
				messages.ForwardColumnName, messages.ForwardColumnHost, messages.ForwardColumnListener,
				messages.ForwardColumnDestination, messages.ForwardColumnStatus,
			}
			components.FitTableHeadings(canvas, state.Theme(), state.forwardColumns[:], state.forwardHeadings[:])
			table := ui.Table(c, &state.forwardTable, ui.TableSpec{
				Rows: hitForwardRowList, Columns: state.forwardColumns[:], Headers: state.forwardHeadings[:], Source: &state.forwardRows,
				Scrollbar: hitForwardScrollThumb, ScrollbarLabel: messages.ForwardListScroll,
				ScrollbarGutter: catalogBarGutter,
			}).Grow(1).Shrink(1)
			if row, ok := table.RowClicked(); ok && state.modal == modalNone && row >= 0 && row < len(state.forwards) {
				state.forwardTable.Select(row)
				state.selectForward(state.forwards[row].ID)
			}
		}
		ui.Divider(c).Background(pal.line)
		ui.Row(c).Height(50).PadSides(8, 0, 20, 0).Items(ui.Center).Children(func() {
			ui.Text(c, state.forwardCountText).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.faint).Width(124)
			if hasSelection && selected.detail != "" {
				ui.Text(c, selected.detail).FontSize(metrics.FontSizeS).Color(detailColor).Grow(1)
			}
		})
	})
	body.Clip().DrawOver(view.draw)
}

// paintForwardsWorkspace paints the forwards page's empty state into the slot
// its declaration left. A table is painted by the page itself.
func (state *State) paintForwardsWorkspace(canvas *gfx.Canvas) {
	view := &state.sectionBodies.forwards
	if len(state.forwards) == 0 {
		state.drawReservedEmptyPage(canvas, view.empty, iconForward)
	}
}

// declareForwardDialog declares the forward editor as a dialog of the workbench
// page.
func (state *State) declareForwardDialog(c ui.C) {
	messages := state.messages
	dialogTitle := messages.ForwardDialogTitle
	submitLabel := messages.ForwardCreate
	if state.forwardEditor.editing {
		dialogTitle = messages.ForwardEditDialogTitle
		submitLabel = messages.ForwardSave
	}
	// Four rows of fields, the resume box, and for a forward that dials through
	// the server the shell-relay box with its relay command when it is on.
	bodyHeight := withError(4*58+2+30, state.forwardDialogError)
	if modeUsesServerDestination(state.forwardEditor.mode) {
		bodyHeight += 30
		if state.forwardEditor.viaShell {
			bodyHeight += 58
		}
	}
	viewport := dialogViewport(c)
	height := dialogContentHeight(c.Canvas(), *c.Theme(), viewport, 560, messages.ForwardDialogDescription, bodyHeight)
	// No scrim dismiss: a press beside the form must not discard its input.
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: dialogTitle, Description: messages.ForwardDialogDescription,
		Width: 560, Height: height, Layer: dialogLayer,
		Scope: dialogScope, CloseID: hitForwardDialogClose, InitialFocusID: hitForwardDialogLabel,
	})
	dialog.Body(func() { state.declareForwardDialogBody(c) })
	dialog.Footer(func() {
		if dialogButton(c, messages.Cancel, components.ButtonSecondary, false).Clicked() {
			c.After(func() { state.clickForwardDialog(hitForwardDialogCancel) })
		}
		if dialogButton(c, submitLabel, components.ButtonDefault, false).Clicked() {
			c.After(func() { state.clickForwardDialog(hitForwardDialogSubmit) })
		}
	})
}

// declareForwardDialogBody lays out the forward editor: four rows of fields, the
// resume box and the error line, and paints the fields into their slots.
func (state *State) declareForwardDialogBody(c ui.C) {
	editor, messages := &state.forwardEditor, state.messages
	mode := editor.mode
	b := &editor.dialogBody
	*b = forwardDialogBody{}
	hint := messages.ForwardSOCKS5Hint
	if mode == portforward.ModeHTTPConnect {
		hint = messages.ForwardHTTPConnectHint
	}
	const rowHeight = 58
	column := ui.Column(c).Fill().PadSides(2, 4, 0, 4)
	column.Children(func() {
		b.name = ui.Slot(c).Height(rowHeight)
		b.host = ui.Slot(c).Height(rowHeight)
		ui.Column(c).Height(rowHeight).Children(func() { state.declareForwardMode(c) })
		ui.Row(c).Height(rowHeight).Children(func() {
			b.port = ui.Slot(c).Grow(24)
			ui.Spacer(c).Grow(4)
			if !modeUsesClientDestination(mode) {
				b.destination = ui.Slot(c).Grow(44)
				ui.Spacer(c).Grow(4)
				b.destinationPort = ui.Slot(c).Grow(24)
			} else {
				ui.Column(c).Grow(72).Basis(0).PadSides(22, 0, 0, 0).Children(func() {
					ui.Text(c, hint).FontSize(metrics.FontSizeS).Color(state.pal.muted)
				})
			}
		})
		if modeUsesServerDestination(mode) {
			b.viaShell, b.viaShellHelp = state.declareDialogCheckboxHelpC(c, messages.ForwardFieldViaShell, 4, 6)
			if editor.viaShell {
				b.relay = ui.Slot(c).Height(rowHeight)
			}
		}
		b.resume, b.resumeHelp = state.declareDialogCheckboxHelpC(c, messages.ForwardFieldResume, 4, 6)
		if state.forwardDialogError != "" {
			ui.Text(c, state.forwardDialogError).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(state.pal.danger).MarginSides(6, 0, 0, 0)
		}
	})
	column.DrawOver(func(d *ui.DrawContext) { state.paintForwardDialogFields(d.Canvas, mode) })
}

// paintForwardDialogFields paints the forward editor's fields into the slots the
// body laid out.
func (state *State) paintForwardDialogFields(canvas *gfx.Canvas, mode portforward.Mode) {
	messages := state.messages
	b := &state.forwardEditor.dialogBody
	state.paintForwardDialogField(canvas, hitForwardDialogLabel, forwardFieldLabel, messages.ForwardFieldName, "", b.name.Rect())
	state.paintForwardHostField(canvas, b.host.Rect())
	portTitle := messages.ForwardFieldLocalPort
	if mode == portforward.ModeRemote {
		portTitle = messages.ForwardFieldRemotePort
	}
	state.paintForwardDialogField(canvas, hitForwardDialogLocalPort, forwardFieldLocalPort, portTitle, "", b.port.Rect())
	if !modeUsesClientDestination(mode) {
		placeholder := messages.ForwardDestinationPlaceholder
		if mode == portforward.ModeRemote {
			placeholder = messages.ForwardLocalDestinationHint
		}
		state.paintForwardDialogField(canvas, hitForwardDialogDestination, forwardFieldDestinationHost, messages.ForwardFieldDestination, placeholder, b.destination.Rect())
		state.paintForwardDialogField(canvas, hitForwardDialogDestinationPort, forwardFieldDestinationPort, messages.ForwardFieldDestinationPort, "", b.destinationPort.Rect())
	}
	if modeUsesServerDestination(mode) {
		state.paintDialogCheckbox(canvas, hitForwardDialogViaShell, messages.ForwardFieldViaShell, state.forwardEditor.viaShell, b.viaShell)
		state.paintHelpIcon(canvas, hitForwardDialogViaShellHelp, messages.ForwardViaShellHelp, b.viaShellHelp.Rect())
		if state.forwardEditor.viaShell {
			state.paintForwardDialogField(canvas, hitForwardDialogRelay, forwardFieldRelay, messages.ForwardFieldRelay, messages.ForwardRelayPlaceholder, b.relay.Rect())
		}
	}
	state.paintDialogCheckbox(canvas, hitForwardDialogResume, messages.ForwardFieldResume, state.forwardEditor.resume, b.resume)
	state.paintHelpIcon(canvas, hitForwardDialogResumeHelp, messages.ForwardResumeHelp, b.resumeHelp.Rect())
}

// forwardModeChoices are the four kinds of forward the dialog offers.
var forwardModeChoices = [...]portforward.Mode{
	portforward.ModeLocal, portforward.ModeRemote, portforward.ModeSOCKS5, portforward.ModeHTTPConnect,
}

func (state *State) forwardModeLabel(mode portforward.Mode) string {
	m := state.messages
	switch mode {
	case portforward.ModeRemote:
		return m.ForwardModeRemote
	case portforward.ModeSOCKS5:
		return m.ForwardModeSOCKS5
	case portforward.ModeHTTPConnect:
		return m.ForwardModeHTTPConnect
	}
	return m.ForwardModeLocal
}

// declareForwardMode declares the forward type switch in the dialog body. It is
// a page Choice, so its arrows move between the options.
func (state *State) declareForwardMode(c ui.C) {
	metrics := c.Theme().Metrics
	gap := metrics.GapItem / 2
	ui.Text(c, state.messages.ForwardFieldType).FontSize(metrics.FontSizeM).Weight(gfx.WeightMedium).Color(state.pal.text).LineHeight(metrics.FontSizeM + 4).Ellipsis()
	choice := ui.Choice(c, &state.forwardEditor.mode, forwardModeChoices[:], state.forwardModeLabel).Height(metrics.ControlHeight).MarginSides(gap, 0, 0, 0)
	// Any press on the switch clears a validation error, including a press on the
	// mode that is already chosen.
	if _, clicked := choice.ChosenIndex(); clicked {
		state.forwardDialogError = ""
	}
}

func (state *State) paintForwardDialogField(canvas *gfx.Canvas, id gfx.ID, field forwardField, title, placeholder string, bounds gfx.Rect) {
	theme := state.Theme()
	content := components.BeginField(canvas, theme, bounds).Paint(title, "", "")
	value := state.forwardEditor.field(field).String()
	focused := canvas.Focused() == id
	components.PaintInput(canvas, theme, content, components.InputSnapshot{
		ID:              id,
		Label:           title,
		Value:           value,
		Placeholder:     placeholder,
		Focused:         focused,
		PlatformEditing: focused && value != "",
	})
}

// paintForwardHostField paints the searchable host picker. Typing filters by
// name, address, account and group, which is what makes a long host list
// navigable without paging.
func (state *State) paintForwardHostField(canvas *gfx.Canvas, bounds gfx.Rect) {
	theme := state.Theme()
	content := components.BeginField(canvas, theme, bounds).Paint(state.messages.ForwardFieldHost, "", "")
	layout := components.PaintSingleSelect(canvas, theme, content, &state.forwardEditor.hostSelect, components.SingleSelectOptions{
		InputID: hitForwardDialogHost, TriggerID: hitForwardHostTrigger, DismissID: hitForwardHostDismiss,
		Scope: hitForwardHostPickerScope, ScrollID: hitForwardHostScroll, OptionIDs: hitForwardHostItemList,
		Label: state.messages.ForwardFieldHost, Placeholder: state.messages.HostPickerSearch,
		EmptyLabel: state.messages.HostPickerNoMatches,
		Viewport:   state.windowRect(), Layer: dialogLayer + 1,
	})
	state.forwardEditor.hostPopup = layout.Popup
}
