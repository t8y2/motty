package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"
	"motty/internal/portforward"
)

const forwardHostPageSize = 6

func (state *State) paintForwardsWorkspace(canvas *ui.Canvas, bounds ui.Rect) {
	pal, messages := state.pal, state.messages
	selected, hasSelection := state.forwardByID(state.selectedForwardID)
	actionLabel := messages.ForwardStart
	if selected.runner != nil || selected.pending || selected.starting {
		actionLabel = messages.ForwardStop
	}
	addWidth := toolbarButtonWidth(canvas, messages.ForwardAdd, true)
	actionWidth := toolbarButtonWidth(canvas, actionLabel, false)
	removeWidth := toolbarButtonWidth(canvas, messages.ForwardRemove, false)
	detailColor := pal.muted
	if selected.lastError != "" {
		detailColor = pal.danger
	}

	var content, emptyIcon, emptyAdd, remove, action, add flex.Box
	f := state.forwardLayout.Begin(canvas, state.Theme(), bounds)
	f.Column().Fill().PadSides(12, 18, 0, 18).Children(func() {
		f.Row().MinHeight(58).Gap(10).MarginSides(0, 0, 12, 0).Items(flex.Start).Children(func() {
			f.Column().Grow(1).Gap(3).MarginSides(4, 0, 0, 0).Children(func() {
				f.Text(messages.ForwardTitle).FontSize(18).Weight(ui.WeightSemibold).Color(pal.text)
				f.Paragraph(messages.ForwardDescription, 2).FontSize(metrics.FontSizeS).Color(pal.muted)
			})
			remove = f.Slot().Size(removeWidth, toolbarHeight)
			action = f.Slot().Size(actionWidth, toolbarHeight)
			add = f.Slot().Size(addWidth, toolbarHeight)
		})
		if len(state.forwards) == 0 {
			emptyIcon, emptyAdd = state.declareEmptyPage(f, bounds, messages.ForwardEmpty, messages.ForwardEmptySub, messages.ForwardAdd)
		} else {
			content = f.Slot().Grow(1).MarginSides(0, 0, 9, 0)
		}
		f.Divider().Background(pal.line)
		f.Row().Height(50).PadSides(8, 0, 20, 0).Items(flex.Center).Children(func() {
			f.Text(state.forwardCountText).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(pal.faint).Width(124)
			if hasSelection && selected.detail != "" {
				f.Text(selected.detail).FontSize(metrics.FontSizeS).Color(detailColor).Grow(1)
			}
		})
	})
	f.End()

	state.paintToolbarButton(canvas, hitForwardRemove, remove.Rect().X, remove.Rect().Y, removeWidth, messages.ForwardRemove, 0, false, hasSelection)
	state.paintToolbarButton(canvas, hitForwardStartStop, action.Rect().X, action.Rect().Y, actionWidth, actionLabel, 0, false, hasSelection)
	state.paintToolbarButton(canvas, hitForwardAdd, add.Rect().X, add.Rect().Y, addWidth, messages.ForwardAdd, iconPlus, true, true)

	if len(state.forwards) == 0 {
		state.paintEmptyPage(canvas, emptyIcon, emptyAdd, iconForward, hitForwardAdd, messages.ForwardAdd, true)
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

// declareEmptyPage declares the centered empty state of a list page (an icon
// tile, a title, a sentence and an Add button, with the icon's top 40% of the
// way down the page's body) and returns the boxes the icon and the button are
// painted into; paintEmptyPage paints them.
func (state *State) declareEmptyPage(f *flex.Frame, bounds ui.Rect, title, sub, add string) (icon, button flex.Box) {
	pal := state.pal
	body := max(0, bounds.Height-142)
	f.Column().Grow(1).MarginSides(0, 0, 9, 0).PadSides(max(0, body*0.40-48), 0, 0, 0).Items(flex.Center).Children(func() {
		f.Column().Size(62, 62).Center().Background(pal.surface).Radius(15).Children(func() {
			icon = f.Slot().Size(24, 24)
		})
		f.Text(title).FontSize(17).Weight(ui.WeightSemibold).Color(pal.text).MarginSides(20, 0, 0, 0).TextCenter()
		f.Text(sub).FontSize(metrics.FontSizeM).Color(pal.muted).MarginSides(8, 0, 0, 0).TextCenter()
		button = f.Slot().Size(toolbarButtonWidth(f.Canvas(), add, true), toolbarHeight).MarginSides(20, 0, 0, 0)
	})
	return icon, button
}

// paintEmptyPage paints what declareEmptyPage reserved room for.
func (state *State) paintEmptyPage(canvas *ui.Canvas, iconBox, buttonBox flex.Box, icon uint8, id ui.ID, label string, enabled bool) {
	paintIcon(canvas, icon, iconBox.Rect(), state.pal.muted)
	button := buttonBox.Rect()
	state.paintToolbarButton(canvas, id, button.X, button.Y, button.Width, label, iconPlus, true, enabled)
}

func (state *State) paintForwardDialog(canvas *ui.Canvas, viewport ui.Rect) {
	messages, theme := state.messages, state.Theme()
	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{
		Layer:          dialogLayer,
		Scope:          dialogScope,
		DismissID:      hitForwardDialogDismiss,
		CloseID:        hitForwardDialogClose,
		InitialFocusID: hitForwardDialogLabel,
		ContentSize:    ui.Size{Width: 560}, FitBody: true, BodyHeight: withError(4*58+2, state.forwardDialogError), // four rows of fields
	})
	layout := dialog.Begin(messages.ForwardDialogTitle, messages.ForwardDialogDescription)

	// Four rows of fields; the last holds the port beside the destination,
	// 24% / 44% / 24% of the row with 4% between.
	const rowHeight = 58
	var name, host, mode, port, destination, destinationPort flex.Box
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
		if state.forwardDialogError != "" {
			f.Text(state.forwardDialogError).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(state.pal.danger).MarginSides(6, 0, 0, 0)
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

	dialog.Actions(layout,
		components.ButtonAction{ID: hitForwardDialogCancel, Label: messages.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitForwardDialogSubmit, Label: messages.ForwardCreate},
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

func (state *State) paintForwardModeField(canvas *ui.Canvas, bounds ui.Rect) {
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

func (state *State) paintForwardDialogField(canvas *ui.Canvas, id ui.ID, field forwardField, title, placeholder string, bounds ui.Rect) {
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

func (state *State) paintForwardHostField(canvas *ui.Canvas, bounds ui.Rect) {
	theme := state.Theme()
	content := components.BeginField(canvas, theme, bounds).Paint(state.messages.ForwardFieldHost, "", "")
	selected, selectedOK := state.hostByID(state.forwardEditor.hostID)
	value := ""
	hint := ""
	if selectedOK {
		value = selected.Label
		hint = selected.Address
	}

	count := len(state.hosts)
	visible := min(count, forwardHostPageSize)
	popupHeight := float32(8 + visible*32)
	if count > forwardHostPageSize {
		popupHeight += 48
	}
	selectControl := components.BeginSelect(canvas, theme, content, components.SelectOptions{
		TriggerID: hitForwardDialogHost,
		Label:     state.messages.ForwardFieldHost,
		Value:     value,
		Hint:      hint,
		Open:      state.forwardEditor.hostOpen,
		Invalid:   !selectedOK,
		Content: ui.Rect{
			X: content.X, Y: content.Y + content.Height + 4,
			Width: content.Width, Height: popupHeight,
		},
		Layer: dialogLayer + 1, Scope: dialogScope, DismissID: hitForwardHostDismiss, Viewport: state.windowRect(),
		Scroll: state.selectScroll(state.forwardEditor.hostOpen, hitForwardHostDismiss),
	})
	defer selectControl.End()
	if state.forwardEditor.hostOpen {
		state.menuPlaced(selectControl.ContentBounds())
	}
	if count > forwardHostPageSize {
		selectControl.AddScrollUp(hitForwardHostScrollUp, state.forwardEditor.hostOffset == 0)
	}
	end := min(state.forwardEditor.hostOffset+forwardHostPageSize, count)
	for index := state.forwardEditor.hostOffset; index < end; index++ {
		host := state.hosts[index]
		selectControl.AddItem(components.SelectItem{
			ID: hitForwardHostItemList.At(int(host.ID)), Label: host.Label, Value: host.Address,
			Selected: host.ID == state.forwardEditor.hostID, Disabled: !host.isSSH(),
		})
	}
	if count > forwardHostPageSize {
		selectControl.AddScrollDown(hitForwardHostScrollDown, end == count)
	}
}
