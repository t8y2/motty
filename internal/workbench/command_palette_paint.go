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
)

// The two roots name the page's controls in the window's Tree. Both pages share
// the window's tree with the rest of the workbench.
const (
	commandPaletteBodyRoot = "command-palette-body"
	commandPaletteListRoot = "command-palette-list"
)

func (state *State) paintCommandPalette(canvas *gfx.Canvas, viewport gfx.Rect) {
	if state.commandPalette.connect {
		state.adoptWSLDistros()
	}
	state.refreshCommandPalette()
	dialog := components.BeginDialog(canvas, state.Theme(), viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitCommandPaletteDismiss, CloseID: hitCommandPaletteClose, InitialFocusID: hitCommandPaletteSearch,
		ContentSize: gfx.Size{Width: 620, Height: state.commandPaletteHeight()},
	})
	title, placeholder, empty := state.messages.CommandPaletteTitle, state.messages.CommandPalettePlaceholder, state.messages.CommandPaletteEmpty
	switch {
	case state.commandPalette.connect:
		title, placeholder, empty = state.messages.LabelNewTab, state.messages.NewTabPickerPlaceholder, state.messages.NewTabPickerEmpty
		if state.commandPalette.workspaces {
			placeholder, empty = state.messages.NewTabWorkspacePlaceholder, state.messages.WorkspaceLibraryEmpty
		}
	case state.commandPalette.snippets:
		title, placeholder, empty = state.messages.Tools.SnippetsTitle, state.messages.Tools.SnippetPickerPlaceholder, state.messages.Tools.SnippetPickerEmpty
		if state.snippetSend.target == sendChosen {
			placeholder, empty = state.messages.Tools.SendTargetsPlaceholder, state.messages.Tools.SendTargetsEmpty
		}
	}
	layout := dialog.Begin(title, "")
	if state.commandPalette.connect {
		state.paintConnectSort(canvas, layout)
	}
	palette := &state.commandPalette
	palette.searchLabel, palette.searchPlace = title, placeholder
	if !palette.searchBound {
		palette.searchField.Bind(&palette.search)
		palette.searchBound = true
	}
	count := len(palette.visible)
	sending := palette.snippets

	palette.emptyText = empty
	// The declarations lay out the search field, the list's room and the send
	// form; the fields and controls themselves are painted into their boxes
	// below, so they keep their own native editing and the palette's own
	// keyboard handling.
	palette.listBox = ui.Box{}
	palette.sendInputBox, palette.sendButtonBox = ui.Box{}, ui.Box{}
	palette.sendRunBox = ui.Box{}
	if palette.page == nil {
		palette.page = ui.NewPage(ui.PageConfig{Root: commandPaletteBodyRoot, Theme: &state.theme})
		palette.view = state.declareCommandPaletteBody
	}
	palette.page.Draw(canvas, layout.Body, palette.view)

	palette.listRect = gfx.Rect{}
	if count > 0 {
		palette.listRect = palette.listBox.Rect()
		area := components.BeginScrollArea(canvas, state.Theme(), palette.listRect, gfx.Size{Width: palette.listRect.Width, Height: palette.contentHeight()}).Offset(0, palette.scroll).HoverScrollbars(false)
		geometry := area.BeginContent()
		palette.scroll = geometry.OffsetY
		// Lay out only the visible window, including the partially exposed last row.
		first := int(geometry.OffsetY / commandPaletteRowStep)
		last := min(count, first+commandPaletteVisibleRows+1)
		content := geometry.Content
		content.Y += float32(first) * commandPaletteRowStep
		content.Height = float32(last-first) * commandPaletteRowStep
		palette.listFirst, palette.listLast = first, last
		palette.bodyWidth = layout.Body.Width
		if palette.listPage == nil {
			palette.listPage = ui.NewPage(ui.PageConfig{Root: commandPaletteListRoot, Theme: &state.theme})
			palette.listView = state.declareCommandPaletteList
		}
		palette.listPage.Draw(canvas, content, palette.listView)
		area.EndContent()
	}
	palette.bar = components.ScrollAreaGeometry{}
	if palette.maxScroll() > 0 {
		// Keep the upstream draggable bar beside, rather than over, the rows.
		bounds := palette.listRect
		bounds.Width += commandPaletteBarGap + commandPaletteBarWidth
		area := components.BeginScrollArea(canvas, state.Theme(), bounds, gfx.Size{Width: bounds.Width, Height: palette.contentHeight()}).Offset(0, palette.scroll).VerticalScrollbar(hitCommandPaletteBar, title, "").HoverScrollbars(true)
		palette.bar = area.EndContent()
	}

	if sending {
		state.paintSnippetSend(canvas, palette.sendInputBox, palette.sendButtonBox, palette.sendRunBox)
	}
	dialog.End()
}

// declareCommandPaletteBody declares the dialog body: the search field, the
// list's room (or the empty notice), and, for the snippet picker, the form that
// sends a typed command. The fields and controls are painted into the boxes it
// keeps.
func (state *State) declareCommandPaletteBody(c ui.C) {
	palette := &state.commandPalette
	count := len(palette.visible)
	sending := palette.snippets
	ui.Column(c).Fill().Children(func() {
		ui.TextFieldID(c, hitCommandPaletteSearch, &palette.searchField, palette.searchLabel, palette.searchPlace).
			Height(36).FillWidth().Shrink(0).NavigationKeys()
		if count == 0 {
			ui.Text(c, palette.emptyText).FontSize(metrics.FontSizeM).Color(state.pal.muted).LineHeight(24).TextCenter().MarginSides(36, 0, 0, 0)
		} else {
			palette.listBox = ui.Slot(c).Height(commandPaletteListHeight).FillWidth().Shrink(0).MarginSides(12, 0, 0, 0)
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

// declareCommandPaletteList declares the rows the scroll area shows. The page's
// bounds are the window of visible rows the scroll area scrolled to, so the
// rows are laid out only there.
func (state *State) declareCommandPaletteList(c ui.C) {
	palette := &state.commandPalette
	canvas := c.Frame().Canvas()
	width := palette.bodyWidth
	// The send-target choice can rebuild the list during the event-pass paint,
	// after the body declared the window this list was sized for; keep the rows
	// inside what the rebuilt list actually holds.
	last := min(palette.listLast, len(palette.visible))
	ui.Column(c).Gap(4).FillWidth().Children(func() {
		for visible := palette.listFirst; visible < last; visible++ {
			entry := palette.entries[palette.visible[visible]]
			if entry.folds() {
				state.declareFoldRow(c, entry, visible)
				continue
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
				continue
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
			// The rows touch through the gap between them, so crossing it
			// never takes the pointer off the list and flashes the first row.
			// Rows are not tab stops: the pointer and the arrows are the one
			// way the selection moves, so Tab keeps its normal traversal.
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
				// The Local Terminal row keeps one plain choice and opens the
				// shell list from its arrow without leaving this dialog.
				if entry.target == hitConnectPickLocal && localShellSettingVisible() && len(state.localShellMenu.choices) > 1 {
					expanded := state.commandPalette.localShells
					glyph := iconChevronRight
					if expanded {
						glyph = iconChevronDown
					}
					var arrowWash gfx.Color
					if canvas != nil && canvas.Hovered() == hitConnectPickShellPicker {
						arrowWash = state.pal.line
					}
					ui.Stack(c).Size(24, 24).Shrink(0).HitWith(hitConnectPickShellPicker, state.messages.LocalShellChoose, gfx.HitOptions{Role: gfx.RoleButton, SkipTab: true, SkipFocus: true, Hint: state.messages.LocalShellChoose, Expanded: expanded}).Children(func() {
						ui.Row(c).Fill().Radius(radius.item).Background(arrowWash)
						ui.Row(c).Fill().Items(ui.Center).Justify(ui.Center).Children(func() {
							ui.Icon(c, lucideIcons[glyph]).Size(14, 14).Color(state.pal.muted).Shrink(0)
						})
					})
				}
				// A saved host can be opened for editing from its own row: the
				// pencil opens the host's dialog instead of connecting.
				if edit := connectPickEdit(entry.target); !edit.IsZero() {
					var editWash gfx.Color
					if canvas != nil && canvas.Hovered() == edit {
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
	})
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
