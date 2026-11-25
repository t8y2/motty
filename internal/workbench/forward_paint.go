package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/portforward"
)

// forwardSectionUI is what the forwards page's declaration leaves for its paint.
type forwardSectionUI struct {
	add, empty ui.Box
	addWidth   float32
	addIcon    uint8
	condensed  bool
	// listScroll is the page list's scroll area, so a long list can be moved
	// from elsewhere.
	listScroll *ui.ScrollState
	// draw is the page hook that paints the empty state. It is kept so a paint
	// does not build a closure for the hook.
	draw func(*ui.DrawContext)
}

// The forward list keeps the widths its rows read at: five data columns and the
// one its row actions take at the end.
const (
	forwardActionsWidth = float32(92)
	forwardStatusLeast  = float32(110)
	// forwardSidePad is the body's padding on each side, which the rows and
	// their headings sit inside.
	forwardSidePad = float32(18)
)

// declareForwardsWorkspace declares the forwards page in the workbench page:
// its header, its open/empty body and its footer. The button, the rows and the
// empty state's icon are painted into the slots it leaves.
func (state *State) declareForwardsWorkspace(c ui.C) {
	canvas := c.Canvas()
	pal, messages := state.pal, state.messages
	state.syncForwardList()
	selected, hasSelection := state.forwardByID(state.selectedForwardID)
	// The title keeps a readable column beside the button; a card too narrow for
	// both shows the button as its icon, whose tooltip names it.
	const (
		titleLeast = float32(220)
		gap        = float32(10)
	)
	bounds := state.catalogSectionBounds()
	condensed := toolbarButtonWidth(canvas, messages.ForwardAdd, true)+gap+titleLeast > bounds.Width-36
	addWidth, addIcon := toolbarButtonLayout(canvas, messages.ForwardAdd, iconPlus, iconPlus, condensed)
	detailColor := pal.muted
	if selected.lastError != "" {
		detailColor = pal.danger
	}
	canAdd := state.sshHostCount() > 0
	view := &state.sectionBodies.forwards
	draw := view.draw
	*view = forwardSectionUI{addWidth: addWidth, addIcon: addIcon, condensed: condensed}
	if draw == nil {
		draw = func(d *ui.DrawContext) { state.paintForwardsWorkspace(d.Canvas) }
	}
	view.draw = draw
	body := ui.Column(c).Fill().PadSides(12, forwardSidePad, 0, forwardSidePad)
	body.Children(func() {
		ui.Row(c).MinHeight(58).Gap(gap).MarginSides(0, 0, 12, 0).Items(ui.Start).Children(func() {
			ui.Column(c).Grow(1).Gap(3).MarginSides(4, 0, 0, 0).Children(func() {
				ui.Text(c, messages.ForwardTitle).FontSize(18).Weight(gfx.WeightSemibold).Color(pal.text).Ellipsis()
				ui.Paragraph(c, messages.ForwardDescription, 2).FontSize(metrics.FontSizeS).Color(pal.muted)
			})
			view.add = state.declareButtonSized(c, hitForwardAdd, messages.ForwardAdd, addIcon, true, false, canAdd, toolbarFont, condensed).Size(addWidth, toolbarHeight)
		})
		if len(state.forwards) == 0 {
			view.empty = state.reserveEmptyPage(c, bounds, messages.ForwardEmpty, messages.ForwardEmptySub, messages.ForwardAdd, hitForwardAdd, iconForward, canAdd)
		} else {
			state.declareForwardList(c, bounds)
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

// declareForwardList declares the forwards page's headings and rows. The rows are
// the page's list, so each one can carry the buttons that act on it; the page
// declares the headings above them.
func (state *State) declareForwardList(c ui.C, bounds gfx.Rect) {
	canvas, messages := c.Canvas(), state.messages
	state.forwardColumns = [...]components.TableColumn{
		{Width: 180}, {Width: 160}, {Width: 170}, {Width: 210}, {Width: 0}, {Width: forwardActionsWidth},
	}
	state.forwardHeadings = [...]string{
		messages.ForwardColumnName, messages.ForwardColumnHost, messages.ForwardColumnListener,
		messages.ForwardColumnDestination, messages.ForwardColumnStatus, "",
	}
	components.FitTableHeadings(canvas, state.Theme(), state.forwardColumns[:], state.forwardHeadings[:])
	state.fitForwardColumns(bounds.Width - 2*forwardSidePad)
	theme := state.Theme()
	headerHeight := components.TableHeaderHeight(theme)
	headerPad := theme.Metrics.GapItem
	ui.Column(c).Grow(1).Shrink(1).MinHeight(0).Children(func() {
		ui.Row(c).Height(headerHeight).Shrink(0).FillWidth().Gap(0).Items(ui.Center).Children(func() {
			for column := range 5 {
				width := state.forwardColumns[column].Width
				if width <= 0 {
					continue
				}
				ui.Text(c, state.forwardHeadings[column]).Weight(gfx.WeightMedium).PadX(headerPad).Width(width).Shrink(0).Ellipsis()
			}
			ui.Slot(c).Width(state.forwardColumns[5].Width).Height(headerHeight).Shrink(0)
			ui.Slot(c).Absolute().Left(0).Right(0).Bottom(0).Height(1).Background(theme.Border)
		})
		list := ui.List(c, &state.forwardList, len(state.forwards), state.forwardRowDecl).Grow(1).Shrink(1).MinHeight(0).FillWidth().
			Scrollbars().ScrollbarIDs(hitForwardScrollThumb, gfx.ID{}).ScrollbarSize(catalogBarGutter).
			ScrollbarLabels(messages.ForwardListScroll, "").Label(messages.ForwardTitle)
		state.sectionBodies.forwards.listScroll = list.ScrollState()
		if state.forwardList.Changed() {
			state.takeForwardListSelection()
		}
	})
}

// fitForwardColumns fits the list's columns to the room its card has: the
// actions keep the width their buttons need at the end, the status keeps a
// readable column, and the four data columns before them share what is left in
// proportion to the widths they read well at, so a narrow card ellipsizes its
// cells instead of cutting the status and the actions off the row.
func (state *State) fitForwardColumns(width float32) {
	width = max(width, 0)
	actions := min(forwardActionsWidth, width)
	state.forwardColumns[5].Width = actions
	content := width - actions
	fixed := float32(0)
	for column := range 4 {
		fixed += state.forwardColumns[column].Width
	}
	status := content - fixed
	if status < forwardStatusLeast {
		status = min(forwardStatusLeast, content)
		if scale := min(1, (content-status)/max(fixed, 1)); scale < 1 {
			for column := range 4 {
				state.forwardColumns[column].Width *= scale
			}
		}
	}
	state.forwardColumns[4].Width = max(status, 0)
}

// declareForwardRow declares one row of the forwards page: its data cells at the
// list's column widths, and the buttons that edit it, start or stop it and
// remove it at its end. Each button acts on this row, whatever row the page has
// selected.
func (state *State) declareForwardRow(row ui.ListRow) {
	if row.Index < 0 || row.Index >= len(state.forwards) {
		return
	}
	forward := &state.forwards[row.Index]
	theme, messages := state.Theme(), state.messages
	height := components.TableRowHeight(theme)
	pad := theme.Metrics.GapItem
	c := row.C
	element := row.Element.Height(height).Shrink(0).FillWidth().Gap(0).Items(ui.Center)
	if forward.ID == state.selectedForwardID {
		element = element.Background(theme.Muted)
	} else {
		element = element.Hover(ui.Look{}.Background(theme.Hover)).Pressed(ui.Look{}.Background(theme.Muted))
	}
	element.Children(func() {
		for column := range 5 {
			width := state.forwardColumns[column].Width
			if width <= 0 {
				continue
			}
			ui.Text(c, forward.cells[column]).PadX(pad).Width(width).Shrink(0).Ellipsis()
		}
		running := forward.runner != nil || forward.pending || forward.starting
		toggleIcon, toggleLabel := uint8(iconPlay), messages.ForwardStart
		if running {
			toggleIcon, toggleLabel = iconStop, messages.ForwardStop
		}
		// The actions keep clear of the hover scrollbar's lane at the row's end.
		ui.Row(c).Width(state.forwardColumns[5].Width).Height(height).Shrink(0).Justify(ui.End).Items(ui.Center).
			PadSides(0, 10, 0, 0).Gap(2).Children(func() {
			state.declareIconButton(c, hitForwardRowEditList.At(int(forward.ID)), iconPencil, messages.ForwardEdit, false, iconButtonQuiet).Size(24, 24)
			state.declareIconButton(c, hitForwardRowToggleList.At(int(forward.ID)), toggleIcon, toggleLabel, false, iconButtonQuiet).Size(24, 24)
			state.declareIconButton(c, hitForwardRowRemoveList.At(int(forward.ID)), iconTrash, messages.ForwardRemove, false, iconButtonQuiet).Size(24, 24)
		})
		ui.Slot(c).Absolute().Left(0).Right(0).Bottom(0).Height(1).Background(theme.Border)
	})
	element.Hit(hitForwardRowList.At(row.Index), forward.Label)
}

// paintForwardsWorkspace paints the forwards page's empty state into the slot
// its declaration left. The rows are the page's list.
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
	// Four rows of fields, the shell-relay box for a forward that dials through
	// the server with its relay command when it is on, and the two boxes under
	// them.
	bodyHeight := withError(4*dialogRowHeight(c.Theme())+2*dialogCheckRowHeight, state.forwardDialogError)
	if modeUsesServerDestination(state.forwardEditor.mode) {
		bodyHeight += dialogCheckRowHeight
		if state.forwardEditor.viaShell {
			bodyHeight += dialogRowHeight(c.Theme())
		}
	}
	if state.forwardEditor.bindExposed() {
		bodyHeight += 38
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
	rowHeight := dialogRowHeight(c.Theme())
	column := ui.Column(c).Fill().PadSides(2, 4, 0, 4)
	column.Children(func() {
		state.declareForwardDialogField(c, hitForwardDialogLabel, forwardFieldLabel, messages.ForwardFieldName, "").FillWidth()
		ui.FormField(c, &editor.hostRow, messages.ForwardFieldHost).Height(rowHeight).Children(func() {
			ui.Combobox(c, &editor.hostChoice, state.forwardHostChoices(), state.hostPickerName, messages.ForwardFieldHost).
				Placeholder(messages.HostPickerSearch).Empty(messages.HostPickerNoMatches).FillWidth()
		})
		if editor.hostChoice != editor.hostID {
			state.applyForwardHostChoice(editor.hostChoice)
			editor.hostChoice = editor.hostID
		}
		ui.Column(c).Height(rowHeight).Children(func() { state.declareForwardMode(c) })
		ui.Row(c).Height(rowHeight).Children(func() {
			portTitle := messages.ForwardFieldLocalPort
			if mode == portforward.ModeRemote {
				portTitle = messages.ForwardFieldRemotePort
			}
			state.declareForwardDialogField(c, hitForwardDialogBindAddress, forwardFieldBindAddress, messages.ForwardFieldBindAddress, messages.ForwardBindPlaceholder).Grow(30)
			ui.Spacer(c).Grow(3)
			state.declareForwardDialogField(c, hitForwardDialogLocalPort, forwardFieldLocalPort, portTitle, "").Grow(20)
			ui.Spacer(c).Grow(4)
			if !modeUsesClientDestination(mode) {
				placeholder := messages.ForwardDestinationPlaceholder
				if mode == portforward.ModeRemote {
					placeholder = messages.ForwardLocalDestinationHint
				}
				state.declareForwardDialogField(c, hitForwardDialogDestination, forwardFieldDestinationHost, messages.ForwardFieldDestination, placeholder).Grow(44)
				ui.Spacer(c).Grow(4)
				state.declareForwardDialogField(c, hitForwardDialogDestinationPort, forwardFieldDestinationPort, messages.ForwardFieldDestinationPort, "").Grow(24)
			} else {
				ui.Column(c).Grow(72).Basis(0).PadSides(22, 0, 0, 0).Children(func() {
					ui.Text(c, hint).FontSize(metrics.FontSizeS).Color(state.pal.muted)
				})
			}
		})
		if editor.bindExposed() {
			ui.Wrap(c, messages.ForwardBindWarning).Height(38).FontSize(c.Theme().Metrics.FontSizeS).
				Weight(gfx.WeightMedium).Color(state.pal.danger).MarginSides(6, 0, 0, 0)
		}
		if modeUsesServerDestination(mode) {
			_, b.viaShellHelp = state.declareDialogCheckHelp(c, hitForwardDialogViaShell, messages.ForwardFieldViaShell, state.forwardEditor.viaShell, dialogCheckGapAbove, dialogCheckGapBelow)
			if editor.viaShell {
				state.declareForwardDialogField(c, hitForwardDialogRelay, forwardFieldRelay, messages.ForwardFieldRelay, messages.ForwardRelayPlaceholder).FillWidth()
			}
		}
		_, b.autoStartHelp = state.declareDialogCheckHelp(c, hitForwardDialogAutoStart, messages.ForwardFieldAutoStart, state.forwardEditor.autoStart, dialogCheckGapAbove, dialogCheckGapBelow)
		_, b.resumeHelp = state.declareDialogCheckHelp(c, hitForwardDialogResume, messages.ForwardFieldResume, state.forwardEditor.resume, dialogCheckGapAbove, dialogCheckGapBelow)
		if state.forwardDialogError != "" {
			ui.Text(c, state.forwardDialogError).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(state.pal.danger).MarginSides(6, 0, 0, 0)
		}
	})
	column.DrawOver(func(d *ui.DrawContext) { state.paintForwardDialogFields(d.Canvas, mode) })
}

// paintForwardDialogFields paints the forward editor's help icons into the
// slots the body laid out.
func (state *State) paintForwardDialogFields(canvas *gfx.Canvas, mode portforward.Mode) {
	messages := state.messages
	b := &state.forwardEditor.dialogBody
	if modeUsesServerDestination(mode) {
		state.paintHelpIcon(canvas, hitForwardDialogViaShellHelp, messages.ForwardViaShellHelp, b.viaShellHelp.Rect())
	}
	state.paintHelpIcon(canvas, hitForwardDialogAutoStartHelp, messages.ForwardAutoStartHelp, b.autoStartHelp.Rect())
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

// declareForwardDialogField declares one of the editor's text fields: a labelled row around a page text
// bound to the field's editor. The caller sizes the row.
func (state *State) declareForwardDialogField(c ui.C, id gfx.ID, field forwardField, title, placeholder string) ui.Box {
	editor := &state.forwardEditor
	if !editor.bound {
		for index := range editor.texts {
			editor.texts[index].Bind(editor.field(forwardField(index)))
		}
		editor.bound = true
	}
	return ui.FormField(c, &editor.rows[field], title).Height(dialogRowHeight(c.Theme())).Children(func() {
		ui.TextField(c, &editor.texts[field], title, placeholder, ui.WithID(id))
	})
}
