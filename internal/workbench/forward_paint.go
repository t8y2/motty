package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/portforward"
)

func (state *State) paintForwardsWorkspace(canvas *gfx.Canvas, bounds gfx.Rect) {
	pal, messages := state.pal, state.messages
	canAdd := state.sshHostCount() > 0
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

	var content, remove, edit, action, add, empty ui.Box
	state.drawSectionBody(canvas, bounds, func(c ui.C) {
		ui.Column(c).Fill().PadSides(12, 18, 0, 18).Children(func() {
			ui.Row(c).MinHeight(58).Gap(gap).MarginSides(0, 0, 12, 0).Items(ui.Start).Children(func() {
				ui.Column(c).Grow(1).Gap(3).MarginSides(4, 0, 0, 0).Children(func() {
					ui.Text(c, messages.ForwardTitle).FontSize(18).Weight(gfx.WeightSemibold).Color(pal.text).Ellipsis()
					ui.Paragraph(c, messages.ForwardDescription, 2).FontSize(metrics.FontSizeS).Color(pal.muted)
				})
				remove = ui.Slot(c).Size(removeWidth, toolbarHeight)
				edit = ui.Slot(c).Size(editWidth, toolbarHeight)
				action = ui.Slot(c).Size(actionWidth, toolbarHeight)
				add = ui.Slot(c).Size(addWidth, toolbarHeight)
			})
			if len(state.forwards) == 0 {
				empty = state.reserveEmptyPage(c, bounds, messages.ForwardEmpty, messages.ForwardEmptySub, messages.ForwardAdd)
			} else {
				content = ui.Slot(c).Grow(1).MarginSides(0, 0, 9, 0)
			}
			ui.Divider(c).Background(pal.line)
			ui.Row(c).Height(50).PadSides(8, 0, 20, 0).Items(ui.Center).Children(func() {
				ui.Text(c, state.forwardCountText).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.faint).Width(124)
				if hasSelection && selected.detail != "" {
					ui.Text(c, selected.detail).FontSize(metrics.FontSizeS).Color(detailColor).Grow(1)
				}
			})
		})
	})

	state.paintToolbarButton(canvas, hitForwardRemove, remove.Rect().X, remove.Rect().Y, removeWidth, messages.ForwardRemove, removeIcon, false, hasSelection, condensed)
	state.paintToolbarButton(canvas, hitForwardEdit, edit.Rect().X, edit.Rect().Y, editWidth, messages.ForwardEdit, editIcon, false, hasSelection, condensed)
	state.paintToolbarButton(canvas, hitForwardStartStop, action.Rect().X, action.Rect().Y, actionWidth, actionLabel, realActionIcon, false, hasSelection, condensed)
	state.paintToolbarButton(canvas, hitForwardAdd, add.Rect().X, add.Rect().Y, addWidth, messages.ForwardAdd, addIcon, true, canAdd, condensed)

	if len(state.forwards) == 0 {
		state.drawReservedEmptyPage(canvas, empty, iconForward, hitForwardAdd, messages.ForwardAdd, canAdd)
		return
	}
	columns := [...]components.TableColumn{{Width: 180}, {Width: 160}, {Width: 170}, {Width: 210}, {Width: 0}}
	headings := [...]string{
		messages.ForwardColumnName,
		messages.ForwardColumnHost,
		messages.ForwardColumnListener,
		messages.ForwardColumnDestination,
		messages.ForwardColumnStatus,
	}
	components.FitTableHeadings(canvas, state.Theme(), columns[:], headings[:])
	table := components.BeginTable(canvas, state.Theme(), content.Rect(), columns[:])
	table.Header(headings[:])
	for index := range state.forwards {
		forward := &state.forwards[index]
		if table.RowAt(hitForwardRowList, int(forward.ID), forward.cells[:], forward.ID == state.selectedForwardID) && state.modal == modalNone {
			state.selectForward(forward.ID)
		}
	}
}

func (state *State) paintForwardDialog(canvas *gfx.Canvas, viewport gfx.Rect) {
	messages, theme := state.messages, state.Theme()
	// No scrim dismiss: a press beside the form must not discard its input.
	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{
		Layer:          dialogLayer,
		Scope:          dialogScope,
		CloseID:        hitForwardDialogClose,
		InitialFocusID: hitForwardDialogLabel,
		ContentSize:    gfx.Size{Width: 560}, FitBody: true, BodyHeight: withError(4*58+2+30, state.forwardDialogError), // four rows of fields, then the checkbox
	})
	dialogTitle := messages.ForwardDialogTitle
	submitLabel := messages.ForwardCreate
	if state.forwardEditor.editing {
		dialogTitle = messages.ForwardEditDialogTitle
		submitLabel = messages.ForwardSave
	}
	layout := dialog.Begin(dialogTitle, messages.ForwardDialogDescription)

	// The mode switch is a page Choice, which writes the new mode during this
	// paint. The rows were laid out from the mode at the start of the paint, so
	// the rest of the paint must read that same mode; the next paint shows the
	// new one.
	mode := state.forwardEditor.mode

	// Four rows of fields; the last holds the port beside the destination,
	// 24% / 44% / 24% of the row with 4% between.
	const rowHeight = 58
	state.forwardDialogPage().Draw(canvas, layout.Body, state.forwardEditor.dialogView)
	body := &state.forwardEditor.dialogBody

	state.paintForwardDialogField(canvas, hitForwardDialogLabel, forwardFieldLabel, messages.ForwardFieldName, "", body.name.Rect())
	state.paintForwardHostField(canvas, body.host.Rect())
	state.paintForwardModeField(canvas, body.mode.Rect())
	portTitle := messages.ForwardFieldLocalPort
	if mode == portforward.ModeRemote {
		portTitle = messages.ForwardFieldRemotePort
	}
	state.paintForwardDialogField(canvas, hitForwardDialogLocalPort, forwardFieldLocalPort, portTitle, "", body.port.Rect())
	if !modeUsesClientDestination(mode) {
		placeholder := messages.ForwardDestinationPlaceholder
		if mode == portforward.ModeRemote {
			placeholder = messages.ForwardLocalDestinationHint
		}
		state.paintForwardDialogField(canvas, hitForwardDialogDestination, forwardFieldDestinationHost, messages.ForwardFieldDestination, placeholder, body.destination.Rect())
		state.paintForwardDialogField(canvas, hitForwardDialogDestinationPort, forwardFieldDestinationPort, messages.ForwardFieldDestinationPort, "", body.destinationPort.Rect())
	}
	state.paintDialogCheckbox(canvas, hitForwardDialogResume, messages.ForwardFieldResume, state.forwardEditor.resume, body.resume)
	state.paintHelpIcon(canvas, hitForwardDialogResumeHelp, messages.ForwardResumeHelp, body.resumeHelp.Rect())

	dialog.Actions(layout,
		components.ButtonAction{ID: hitForwardDialogCancel, Label: messages.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitForwardDialogSubmit, Label: submitLabel},
	)
	dialog.End()
}

// forwardDialogBodyRoot names the page the forward editor's body is declared in.
const forwardDialogBodyRoot = "forward-dialog-body"

// forwardDialogPage makes the page the forward editor's body is declared in.
func (state *State) forwardDialogPage() *ui.Page {
	editor := &state.forwardEditor
	if editor.dialogPage == nil {
		editor.dialogPage = ui.NewPage(ui.PageConfig{Root: forwardDialogBodyRoot, Theme: &state.theme})
		editor.dialogView = state.declareForwardDialogBody
	}
	return editor.dialogPage
}

// declareForwardDialogBody lays out the forward editor: four rows of fields, the
// resume box and the error line. The fields themselves are painted into their
// slots by paintForwardDialog.
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
	ui.Column(c).Fill().PadSides(2, 4, 0, 4).Children(func() {
		b.name = ui.Slot(c).Height(rowHeight)
		b.host = ui.Slot(c).Height(rowHeight)
		b.mode = ui.Slot(c).Height(rowHeight)
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
		b.resume, b.resumeHelp = state.declareDialogCheckboxHelpC(c, messages.ForwardFieldResume, 4, 6)
		if state.forwardDialogError != "" {
			ui.Text(c, state.forwardDialogError).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(state.pal.danger).MarginSides(6, 0, 0, 0)
		}
	})
}

func (state *State) paintForwardModeField(canvas *gfx.Canvas, bounds gfx.Rect) {
	content := components.BeginField(canvas, state.Theme(), bounds).Paint(state.messages.ForwardFieldType, "", "")
	state.drawForwardMode(canvas, content)
}

// forwardModeRoot names the forward type switch in the window's Tree.
const forwardModeRoot = "forward-mode"

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

// drawForwardMode paints the forward type switch in its slot. It is a page
// Choice, so its arrows move between the options.
func (state *State) drawForwardMode(canvas *gfx.Canvas, bounds gfx.Rect) {
	editor := &state.forwardEditor
	if editor.modePage == nil {
		editor.modePage = ui.NewPage(ui.PageConfig{Root: forwardModeRoot, Theme: &state.theme})
		editor.modeView = state.declareForwardMode
	}
	editor.modePage.Draw(canvas, bounds, editor.modeView)
}

func (state *State) declareForwardMode(c ui.C) {
	choice := ui.Choice(c, &state.forwardEditor.mode, forwardModeChoices[:], state.forwardModeLabel).Fill()
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
