package workbench

import (
	"fmt"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

const (
	commandPaletteVisibleRows = 10
	// commandPaletteRowPitch is a row and the gap below it.
	commandPaletteRowPitch = 34
	// commandPaletteBarGap is how far the scrollbar's strip sits beyond the
	// list: in the dialog's padding, so it never covers a row's text.
	commandPaletteBarGap = 8
	// commandPaletteBarWidth is the slim bar's strip, as the scroll area's
	// overlay style draws it; the thumb is a little narrower.
	commandPaletteBarWidth = 6
	// commandPaletteWidth is the dialog's content width, the palette's ever since
	// the new-tab picker shared it.
	commandPaletteWidth = 620
	// commandPaletteSortWidth is the default/workspaces/recent switch's width.
	commandPaletteSortWidth = 270
	// commandPaletteSortGap is the space between the switch and the close button.
	commandPaletteSortGap = 8
)

// declareCommandPalette declares the palette in the workbench page: the page's
// Dialog, with the search field, the sort switch and the list as page elements.
// The palette keeps its own keyboard and pointer gestures for the rows it names
// by hand, while the page routes the controls it declares.
func (state *State) declareCommandPalette(c ui.C) {
	if state.modal != modalCommandPalette {
		return
	}
	palette := &state.commandPalette
	if palette.connect {
		state.adoptWSLDistros()
	}
	state.refreshCommandPalette()
	title, placeholder, empty := state.commandPaletteTexts()
	palette.searchLabel, palette.searchPlace = title, placeholder
	palette.emptyText = empty
	if !palette.searchBound {
		palette.searchField.Bind(&palette.search)
		palette.searchBound = true
	}
	palette.ctx = c
	if palette.body == nil {
		palette.body = func() { state.declareCommandPaletteBody(palette.ctx) }
		palette.listOver = state.paintCommandPaletteListOverlay
		palette.chromeOver = state.paintCommandPaletteChrome
	}
	height := state.commandPaletteHeight()
	// The dialog's own layout says where its title row sits and how wide its body
	// is; the sort switch and the rows keep the rectangles they had when the
	// palette opened the dialog by hand.
	viewport := gfx.Rect{Width: c.Bounds().Width, Height: c.Bounds().Height}
	layout := components.BeginDialog(c.Canvas(), *c.Theme(), viewport, components.DialogOptions{
		ContentSize: gfx.Size{Width: commandPaletteWidth, Height: height},
	}).Layout("")
	palette.bodyWidth = layout.Body.Width
	palette.sortLeft = layout.Close.X - commandPaletteSortWidth - commandPaletteSortGap - layout.Body.X
	palette.sortTop = layout.Close.Y - layout.Body.Y
	palette.sortHeight = layout.Close.Height
	// The palette is a dialog's peer: on its own layer it paints over a terminal's
	// native view and over the page behind it.
	ui.PopupLayer(c, dialogLayer)
	// The palette's Dialog is declared in the workbench page: the page names its
	// controls, and the explicit layer keeps its paint above the assistant
	// drawer's.
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: title, Width: commandPaletteWidth, Height: height, Layer: dialogLayer,
		Scope: dialogScope, DismissID: hitCommandPaletteDismiss, CloseID: hitCommandPaletteClose,
		InitialFocusID: hitCommandPaletteSearch,
	})
	dialog.Body(palette.body)
	if dialog.Dismissed() {
		state.closeCommandPalette()
	}
}

// commandPaletteTexts is the dialog's title and the search field's label and
// placeholder, by which list the palette is showing.
func (state *State) commandPaletteTexts() (title, placeholder, empty string) {
	m := state.messages
	title, placeholder, empty = m.CommandPaletteTitle, m.CommandPalettePlaceholder, m.CommandPaletteEmpty
	switch {
	case state.commandPalette.connect:
		title, placeholder, empty = m.LabelNewTab, m.NewTabPickerPlaceholder, m.NewTabPickerEmpty
		if state.commandPalette.workspaces {
			placeholder, empty = m.NewTabWorkspacePlaceholder, m.WorkspaceLibraryEmpty
		}
	case state.commandPalette.snippets:
		title, placeholder, empty = m.Tools.SnippetsTitle, m.Tools.SnippetPickerPlaceholder, m.Tools.SnippetPickerEmpty
		if state.snippetSend.target == sendChosen {
			placeholder, empty = m.Tools.SendTargetsPlaceholder, m.Tools.SendTargetsEmpty
		}
	}
	return title, placeholder, empty
}

// declareCommandPaletteBody declares the dialog body: the sort switch in the
// title row, the search field, the list's window (or the empty notice), and, for
// the snippet picker, the form that sends a typed command.
func (state *State) declareCommandPaletteBody(c ui.C) {
	palette := &state.commandPalette
	sending := palette.snippets
	palette.listBox = ui.Box{}
	palette.sendInputBox, palette.sendButtonBox, palette.sendRunBox = ui.Box{}, ui.Box{}, ui.Box{}
	column := ui.Column(c).Fill()
	if sending {
		column = column.DrawOver(palette.chromeOver)
	}
	column.Children(func() {
		if palette.connect {
			state.declareConnectSort(c)
		}
		// The sort switch's change rebuilds the list as it is declared, so the
		// count is read after it, not before.
		count := len(palette.visible)
		ui.TextFieldID(c, hitCommandPaletteSearch, &palette.searchField, palette.searchLabel, palette.searchPlace).
			Height(36).FillWidth().Shrink(0).NavigationKeys()
		if count == 0 {
			palette.listRect = gfx.Rect{}
			palette.bar = components.ScrollAreaGeometry{}
			ui.Text(c, palette.emptyText).FontSize(metrics.FontSizeM).Color(state.pal.muted).LineHeight(24).TextCenter().MarginSides(36, 0, 0, 0)
		} else {
			palette.listBox = ui.Row(c).Height(commandPaletteListHeight).FillWidth().Shrink(0).MarginSides(12, 0, 0, 0).DrawOver(palette.listOver)
			palette.listBox.Children(func() {
				// The rows are clipped to the list's window; the scrollbar beside it
				// is painted by the row's DrawOver, outside that clip.
				list := ui.Column(c).Fill().Clip()
				list.Children(func() {
					first := max(0, int(palette.scroll/commandPaletteRowStep))
					last := min(count, first+commandPaletteVisibleRows+1)
					inner := ui.Column(c).FillWidth().Gap(4).MarginTop(-palette.scroll)
					inner.Children(func() {
						if first > 0 {
							ui.Spacer(c).Height(float32(first)*commandPaletteRowStep - 4)
						}
						for visible := first; visible < last; visible++ {
							state.declareCommandPaletteRow(c, palette.entries[palette.visible[visible]], visible)
						}
					})
				})
			})
		}
		if !sending {
			return
		}
		// The send form sits under the list, whatever its length.
		ui.Spacer(c).Grow(1)
		ui.Row(c).Height(36).FillWidth().Shrink(0).Gap(8).MarginSides(12, 0, 0, 0).Children(func() {
			palette.sendInputBox = ui.Slot(c).Height(36).Grow(1)
			palette.sendButtonBox = ui.Slot(c).Size(84, 36).Shrink(0)
		})
		ui.Row(c).Height(32).FillWidth().Shrink(0).Gap(12).Items(ui.Center).MarginSides(8, 0, 0, 0).Children(func() {
			ui.Row(c).Size(270, 32).Shrink(0).Children(func() {
				if ui.Choice(c, &state.snippetSend.target, sendTargetChoices[:], state.sendTargetLabel).Fill().Changed() {
					state.applySendTarget()
				}
			})
			if state.snippetSend.target == sendChosen {
				ui.Text(c, fmt.Sprintf(state.messages.Tools.SendChosenFmt, len(state.snippetSend.chosen))).FontSize(metrics.FontSizeS).Color(state.pal.muted).LineHeight(20).Shrink(0)
			}
			ui.Spacer(c).Grow(1)
			side := components.CheckboxExtent(state.Theme())
			palette.sendRunBox = ui.Row(c).Height(20).Gap(24-side).Items(ui.Center).MarginSides(0, 0, 0, 2).Children(func() {
				ui.Slot(c).Size(side, side).Shrink(0)
				ui.Text(c, state.messages.Tools.SendRun).FontSize(metrics.FontSizeM).Weight(gfx.WeightMedium).Color(state.pal.text).LineHeight(20).Grow(1)
			})
		})
	})
}

// declareCommandPaletteRow declares one choice of the list.
func (state *State) declareCommandPaletteRow(c ui.C, entry commandPaletteEntry, visible int) {
	palette := &state.commandPalette
	width := palette.bodyWidth
	if entry.folds() {
		state.declareFoldRow(c, entry, visible)
		return
	}
	if entry.header {
		ui.Row(c).Height(30).PadX(10).Gap(6).Items(ui.Center).Shrink(0).
			HitOutset(2, 0, 2, 0).
			HitWith(hitCommandPaletteHeaders.At(visible), entry.label, gfx.HitOptions{Role: gfx.RoleGroup, SkipFocus: true}).
			Children(func() {
				ui.Text(c, entry.label).FontSize(metrics.FontSizeS).Weight(gfx.WeightSemibold).Color(state.pal.faint).LineHeight(20).Ellipsis().Shrink(1)
				if entry.detail != "" {
					ui.Text(c, entry.detail).FontSize(metrics.FontSizeS).Color(state.pal.faint).LineHeight(20).Shrink(0)
				}
			})
		return
	}
	id := hitCommandPaletteItems.At(visible)
	selected := palette.selected == visible
	color, detailColor := state.pal.muted, state.pal.faint
	if selected {
		color, detailColor = state.pal.onAccent, state.pal.onAccent
	}
	row := ui.Row(c).Height(30).PadX(10).Gap(12).Items(ui.Center).Shrink(0)
	if selected {
		row.Background(state.pal.accent).Radius(radius.item)
	}
	// The rows touch through the gap between them, so crossing it never takes
	// the pointer off the list and flashes the first row. Rows are not tab stops:
	// the pointer and the arrows are the one way the selection moves, so Tab
	// keeps its normal traversal.
	row.HitOutset(2, 0, 2, 0).HitWith(id, entry.label, gfx.HitOptions{Role: gfx.RoleButton, SkipTab: true, Hint: entry.recencyHint, Selected: entry.checked}).Children(func() {
		if entry.checkable {
			// The tick keeps its place when unticked, so the names line up.
			tick := ui.Stack(c).Size(14, 14).Shrink(0)
			if entry.checked {
				tick.Children(func() { ui.Icon(c, lucideIcons[iconCheck]).Size(14, 14).Color(color) })
			}
		}
		if entry.recency == "" {
			ui.Text(c, entry.label).FontSize(metrics.FontSizeM).Weight(gfx.WeightMedium).Color(color).Ellipsis().Grow(1)
		} else {
			ui.Row(c).Grow(1).Shrink(1).Gap(8).Items(ui.Center).Children(func() {
				ui.Text(c, entry.label).FontSize(metrics.FontSizeM).Weight(gfx.WeightMedium).Color(color).Ellipsis().Shrink(1)
				ui.Text(c, entry.recency).FontSize(metrics.FontSizeS).Color(withAlpha(color, 0.65)).Shrink(0)
			})
		}
		if entry.detail != "" {
			ui.Text(c, entry.detail).FontSize(metrics.FontSizeS).Color(detailColor).Ellipsis().MaxWidth(width * 0.45).Shrink(0)
		}
		// The Local Terminal row keeps one plain choice and opens the shell list
		// from its arrow without leaving this dialog.
		if entry.target == hitConnectPickLocal && localShellSettingVisible() && state.visibleShellCount() > 1 {
			expanded := state.commandPalette.localShells
			glyph := iconChevronRight
			if expanded {
				glyph = iconChevronDown
			}
			var arrowWash gfx.Color
			if canvas := c.Canvas(); canvas != nil && canvas.Hovered() == hitConnectPickShellPicker {
				arrowWash = state.pal.line
			}
			ui.Stack(c).Size(24, 24).Shrink(0).HitWith(hitConnectPickShellPicker, state.messages.LocalShellChoose, gfx.HitOptions{Role: gfx.RoleButton, SkipTab: true, SkipFocus: true, Hint: state.messages.LocalShellChoose, Expanded: expanded}).Children(func() {
				ui.Row(c).Fill().Radius(radius.item).Background(arrowWash)
				ui.Row(c).Fill().Items(ui.Center).Justify(ui.Center).Children(func() {
					ui.Icon(c, lucideIcons[glyph]).Size(14, 14).Color(state.pal.muted).Shrink(0)
				})
			})
		}
		// A saved host can be opened for editing from its own row: the pencil
		// opens the host's dialog instead of connecting.
		if edit := connectPickEdit(entry.target); !edit.IsZero() {
			var editWash gfx.Color
			if canvas := c.Canvas(); canvas != nil && canvas.Hovered() == edit {
				editWash = state.pal.line
			}
			ui.Stack(c).Size(24, 24).Shrink(0).HitWith(edit, state.messages.EditHost, gfx.HitOptions{Role: gfx.RoleButton, SkipTab: true, Hint: state.messages.EditHost}).Children(func() {
				ui.Row(c).Fill().Radius(radius.item).Background(editWash)
				ui.Row(c).Fill().Items(ui.Center).Justify(ui.Center).Children(func() {
					ui.Icon(c, lucideIcons[iconPencil]).Size(14, 14).Color(state.pal.muted).Shrink(0)
				})
			})
		}
	})
}

// paintCommandPaletteListOverlay paints the list's scrollbar after the page laid
// the rows out, as the palette always has: beside the rows, not over them.
func (state *State) paintCommandPaletteListOverlay(d *ui.DrawContext) {
	palette := &state.commandPalette
	palette.listRect = d.Rect
	if palette.maxScroll() <= 0 {
		palette.bar = components.ScrollAreaGeometry{}
		return
	}
	bounds := d.Rect
	bounds.Width += commandPaletteBarGap + commandPaletteBarWidth
	area := components.BeginScrollArea(d.Canvas, *d.Theme, bounds, gfx.Size{Width: bounds.Width, Height: palette.contentHeight()}).
		Offset(0, palette.scroll).VerticalScrollbar(hitCommandPaletteBar, palette.searchLabel, "").HoverScrollbars(true)
	palette.bar = area.EndContent()
}

// paintCommandPaletteChrome paints the parts the dialog body left for the page's
// last pass: the snippet picker's send form.
func (state *State) paintCommandPaletteChrome(d *ui.DrawContext) {
	palette := &state.commandPalette
	if !palette.snippets {
		return
	}
	state.paintSnippetSend(d.Canvas, palette.sendInputBox, palette.sendButtonBox, palette.sendRunBox)
}

// commandPaletteHeight is the dialog's height: the snippet picker adds the form
// that sends a typed command under its list.
func (state *State) commandPaletteHeight() float32 {
	if state.commandPalette.snippets {
		return 474 + 12 + 36 + 8 + 32
	}
	return 474
}

// declareFoldRow is the heading of a group the list can open and close: a choice
// like the rows under it, with a chevron for the way it will go.
func (state *State) declareFoldRow(c ui.C, entry commandPaletteEntry, visible int) {
	selected := state.commandPalette.selected == visible
	color, detailColor := state.pal.muted, state.pal.faint
	row := ui.Row(c).Height(30).PadX(10).Gap(8).Items(ui.Center).Shrink(0)
	if selected {
		color, detailColor = state.pal.onAccent, state.pal.onAccent
		row.Background(state.pal.accent).Radius(radius.item)
	}
	glyph := iconChevronRight
	if entry.expanded {
		glyph = iconChevronDown
	}
	row.HitOutset(2, 0, 2, 0).HitWith(hitCommandPaletteItems.At(visible), entry.label, gfx.HitOptions{Role: gfx.RoleButton, SkipTab: true, Expanded: entry.expanded}).Children(func() {
		ui.Icon(c, lucideIcons[glyph]).Size(14, 14).Color(color).Shrink(0)
		ui.Text(c, entry.label).FontSize(metrics.FontSizeM).Weight(gfx.WeightSemibold).Color(color).Ellipsis().Grow(1)
		if entry.detail != "" {
			ui.Text(c, entry.detail).FontSize(metrics.FontSizeS).Color(detailColor).Shrink(0)
		}
	})
}
