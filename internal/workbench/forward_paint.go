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

	var content, emptyIcon, emptyAdd, remove, edit, action, add ui.Box
	f := state.forwardLayout.Begin(canvas, state.Theme(), bounds)
	f.Column().Fill().PadSides(12, 18, 0, 18).Children(func() {
		f.Row().MinHeight(58).Gap(gap).MarginSides(0, 0, 12, 0).Items(ui.Start).Children(func() {
			f.Column().Grow(1).Gap(3).MarginSides(4, 0, 0, 0).Children(func() {
				f.Text(messages.ForwardTitle).FontSize(18).Weight(gfx.WeightSemibold).Color(pal.text).Ellipsis()
				f.Paragraph(messages.ForwardDescription, 2).FontSize(metrics.FontSizeS).Color(pal.muted)
			})
			remove = f.Slot().Size(removeWidth, toolbarHeight)
			edit = f.Slot().Size(editWidth, toolbarHeight)
			action = f.Slot().Size(actionWidth, toolbarHeight)
			add = f.Slot().Size(addWidth, toolbarHeight)
		})
		if len(state.forwards) == 0 {
			emptyIcon, emptyAdd = state.declareEmptyPage(f, bounds, messages.ForwardEmpty, messages.ForwardEmptySub, messages.ForwardAdd)
		} else {
			content = f.Slot().Grow(1).MarginSides(0, 0, 9, 0)
		}
		f.Divider().Background(pal.line)
		f.Row().Height(50).PadSides(8, 0, 20, 0).Items(ui.Center).Children(func() {
			f.Text(state.forwardCountText).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.faint).Width(124)
			if hasSelection && selected.detail != "" {
				f.Text(selected.detail).FontSize(metrics.FontSizeS).Color(detailColor).Grow(1)
			}
		})
	})
	f.End()

	state.paintToolbarButton(canvas, hitForwardRemove, remove.Rect().X, remove.Rect().Y, removeWidth, messages.ForwardRemove, removeIcon, false, hasSelection, condensed)
	state.paintToolbarButton(canvas, hitForwardEdit, edit.Rect().X, edit.Rect().Y, editWidth, messages.ForwardEdit, editIcon, false, hasSelection, condensed)
	state.paintToolbarButton(canvas, hitForwardStartStop, action.Rect().X, action.Rect().Y, actionWidth, actionLabel, realActionIcon, false, hasSelection, condensed)
	state.paintToolbarButton(canvas, hitForwardAdd, add.Rect().X, add.Rect().Y, addWidth, messages.ForwardAdd, addIcon, true, canAdd, condensed)

	if len(state.forwards) == 0 {
		state.paintEmptyPage(canvas, emptyIcon, emptyAdd, iconForward, hitForwardAdd, messages.ForwardAdd, canAdd)
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

	// Four rows of fields; the last holds the port beside the destination,
	// 24% / 44% / 24% of the row with 4% between.
	const rowHeight = 58
	var name, host, mode, port, destination, destinationPort, resume, resumeHelp ui.Box
	hint := messages.ForwardSOCKS5Hint
	if state.forwardEditor.mode == portforward.ModeHTTPConnect {
		hint = messages.ForwardHTTPConnectHint
	}
	f := state.forwardDialogLayout.Begin(canvas, theme, layout.Body)
	f.Column().Fill().PadSides(2, 4, 0, 4).Children(func() {
		name = f.Slot().Height(rowHeight)
		host = f.Slot().Height(rowHeight)
		mode = f.Slot().Height(rowHeight)
		f.Row().Height(rowHeight).Children(func() {
			port = f.Slot().Grow(24)
			f.Spacer().Grow(4)
			if !state.forwardEditor.usesClientDestination() {
				destination = f.Slot().Grow(44)
				f.Spacer().Grow(4)
				destinationPort = f.Slot().Grow(24)
			} else {
				f.Column().Grow(72).Basis(0).PadSides(22, 0, 0, 0).Children(func() {
					f.Text(hint).FontSize(metrics.FontSizeS).Color(state.pal.muted)
				})
			}
		})
		resume, resumeHelp = state.declareDialogCheckboxHelp(f, messages.ForwardFieldResume, 4, 6)
		if state.forwardDialogError != "" {
			f.Text(state.forwardDialogError).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(state.pal.danger).MarginSides(6, 0, 0, 0)
		}
	})
	f.End()

	state.paintForwardDialogField(canvas, hitForwardDialogLabel, forwardFieldLabel, messages.ForwardFieldName, "", name.Rect())
	state.paintForwardHostField(canvas, host.Rect())
	state.paintForwardModeField(canvas, mode.Rect())
	portTitle := messages.ForwardFieldLocalPort
	if state.forwardEditor.mode == portforward.ModeRemote {
		portTitle = messages.ForwardFieldRemotePort
	}
	state.paintForwardDialogField(canvas, hitForwardDialogLocalPort, forwardFieldLocalPort, portTitle, "", port.Rect())
	if !state.forwardEditor.usesClientDestination() {
		placeholder := messages.ForwardDestinationPlaceholder
		if state.forwardEditor.mode == portforward.ModeRemote {
			placeholder = messages.ForwardLocalDestinationHint
		}
		state.paintForwardDialogField(canvas, hitForwardDialogDestination, forwardFieldDestinationHost, messages.ForwardFieldDestination, placeholder, destination.Rect())
		state.paintForwardDialogField(canvas, hitForwardDialogDestinationPort, forwardFieldDestinationPort, messages.ForwardFieldDestinationPort, "", destinationPort.Rect())
	}
	state.paintDialogCheckbox(canvas, hitForwardDialogResume, messages.ForwardFieldResume, state.forwardEditor.resume, resume)
	state.paintHelpIcon(canvas, hitForwardDialogResumeHelp, messages.ForwardResumeHelp, resumeHelp.Rect())

	dialog.Actions(layout,
		components.ButtonAction{ID: hitForwardDialogCancel, Label: messages.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitForwardDialogSubmit, Label: submitLabel},
	)
	dialog.End()
}

func (state *State) forwardModeOptions() [4]components.SegmentOption {
	return [4]components.SegmentOption{
		{ID: hitForwardModeLocal, Label: state.messages.ForwardModeLocal},
		{ID: hitForwardModeRemote, Label: state.messages.ForwardModeRemote},
		{ID: hitForwardModeSOCKS5, Label: state.messages.ForwardModeSOCKS5},
		{ID: hitForwardModeHTTPConnect, Label: state.messages.ForwardModeHTTPConnect},
	}
}

func (state *State) paintForwardModeField(canvas *gfx.Canvas, bounds gfx.Rect) {
	content := components.BeginField(canvas, state.Theme(), bounds).Paint(state.messages.ForwardFieldType, "", "")
	selected := hitForwardModeLocal
	switch state.forwardEditor.mode {
	case portforward.ModeRemote:
		selected = hitForwardModeRemote
	case portforward.ModeSOCKS5:
		selected = hitForwardModeSOCKS5
	case portforward.ModeHTTPConnect:
		selected = hitForwardModeHTTPConnect
	}
	options := state.forwardModeOptions()
	components.BeginSegmentedControl(canvas, state.Theme(), content, options[:], selected).Paint()
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
