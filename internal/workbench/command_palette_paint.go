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
	value := state.commandPalette.search.String()
	focused := canvas.Focused() == hitCommandPaletteSearch
	palette := &state.commandPalette
	count := len(palette.visible)

	var search, list ui.Box
	var sendInput, sendButton, sendTargets, sendRun ui.Box
	sending := palette.snippets
	f := state.paletteLayout.Begin(canvas, state.Theme(), layout.Body)
	f.Column().Fill().Children(func() {
		search = f.Slot().Height(36).FillWidth().Shrink(0)
		if count == 0 {
			f.Text(empty).FontSize(metrics.FontSizeM).Color(state.pal.muted).LineHeight(24).TextCenter().MarginSides(36, 0, 0, 0)
		} else {
			list = f.Slot().Height(commandPaletteListHeight).FillWidth().Shrink(0).MarginSides(12, 0, 0, 0)
		}
		if !sending {
			return
		}
		// The send form sits under the list, whatever its length.
		f.Spacer().Grow(1)
		f.Row().Height(36).FillWidth().Shrink(0).Gap(8).MarginSides(12, 0, 0, 0).Children(func() {
			sendInput = f.Slot().Height(36).Grow(1)
			sendButton = f.Slot().Size(84, 36).Shrink(0)
		})
		f.Row().Height(32).FillWidth().Shrink(0).Gap(12).Items(ui.Center).MarginSides(8, 0, 0, 0).Children(func() {
			sendTargets = f.Slot().Size(270, 32).Shrink(0)
			if state.snippetSend.target == sendChosen {
				f.Text(fmt.Sprintf(state.messages.Tools.SendChosenFmt, len(state.snippetSend.chosen))).FontSize(metrics.FontSizeS).Color(state.pal.muted).LineHeight(20).Shrink(0)
			}
			f.Spacer().Grow(1)
			sendRun = state.declareDialogCheckbox(f, state.messages.Tools.SendRun, 0, 0)
		})
	})
	f.End()
	palette.listRect = gfx.Rect{}
	if count > 0 {
		palette.listRect = list.Rect()
		area := components.BeginScrollArea(canvas, state.Theme(), palette.listRect, gfx.Size{Width: palette.listRect.Width, Height: palette.contentHeight()}).Offset(0, palette.scroll).HoverScrollbars(false)
		geometry := area.BeginContent()
		palette.scroll = geometry.OffsetY
		// Lay out only the visible window, including the partially exposed last row.
		first := int(geometry.OffsetY / commandPaletteRowStep)
		last := min(count, first+commandPaletteVisibleRows+1)
		content := geometry.Content
		content.Y += float32(first) * commandPaletteRowStep
		content.Height = float32(last-first) * commandPaletteRowStep
		f := state.paletteListLayout.Begin(canvas, state.Theme(), content)
		f.Column().Gap(4).FillWidth().Children(func() {
			for visible := first; visible < last; visible++ {
				entry := palette.entries[palette.visible[visible]]
				if entry.folds() {
					state.paintFoldRow(f, entry, visible)
					continue
				}
				if entry.header {
					f.Row().Height(30).PadX(10).Gap(6).Items(ui.Center).Shrink(0).
						HitOutset(2, 0, 2, 0).
						HitWith(hitCommandPaletteHeaders.At(visible), entry.label, gfx.HitOptions{Role: gfx.RoleGroup, SkipFocus: true}).
						Children(func() {
							f.Text(entry.label).FontSize(metrics.FontSizeS).Weight(gfx.WeightSemibold).Color(state.pal.faint).LineHeight(20).Ellipsis().Shrink(1)
							if entry.detail != "" {
								f.Text(entry.detail).FontSize(metrics.FontSizeS).Color(state.pal.faint).LineHeight(20).Shrink(0)
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
				row := f.Row().Height(30).PadX(10).Gap(12).Items(ui.Center).Shrink(0)
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
						tick := f.Stack().Size(14, 14).Shrink(0)
						if entry.checked {
							tick.Children(func() { f.Icon(lucideIcons[iconCheck]).Size(14, 14).Color(color) })
						}
					}
					if entry.recency == "" {
						f.Text(entry.label).FontSize(metrics.FontSizeM).Weight(gfx.WeightMedium).Color(color).Ellipsis().Grow(1)
					} else {
						f.Row().Grow(1).Shrink(1).Gap(8).Items(ui.Center).Children(func() {
							f.Text(entry.label).FontSize(metrics.FontSizeM).Weight(gfx.WeightMedium).Color(color).Ellipsis().Shrink(1)
							f.Text(entry.recency).FontSize(metrics.FontSizeS).Color(withAlpha(color, 0.65)).Shrink(0)
						})
					}
					if entry.detail != "" {
						f.Text(entry.detail).FontSize(metrics.FontSizeS).Color(detailColor).Ellipsis().MaxWidth(layout.Body.Width * 0.45).Shrink(0)
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
						if canvas.Hovered() == hitConnectPickShellPicker {
							arrowWash = state.pal.line
						}
						f.Stack().Size(24, 24).Shrink(0).HitWith(hitConnectPickShellPicker, state.messages.LocalShellChoose, gfx.HitOptions{Role: gfx.RoleButton, SkipTab: true, SkipFocus: true, Hint: state.messages.LocalShellChoose, Expanded: expanded}).Children(func() {
							f.Row().Fill().Radius(radius.item).Background(arrowWash)
							f.Row().Fill().Items(ui.Center).Justify(ui.Center).Children(func() {
								f.Icon(lucideIcons[glyph]).Size(14, 14).Color(state.pal.muted).Shrink(0)
							})
						})
					}
					// A saved host can be opened for editing from its own row: the
					// pencil opens the host's dialog instead of connecting.
					if edit := connectPickEdit(entry.target); !edit.IsZero() {
						var editWash gfx.Color
						if canvas.Hovered() == edit {
							editWash = state.pal.line
						}
						f.Stack().Size(24, 24).Shrink(0).HitWith(edit, state.messages.EditHost, gfx.HitOptions{Role: gfx.RoleButton, SkipTab: true, Hint: state.messages.EditHost}).Children(func() {
							f.Row().Fill().Radius(radius.item).Background(editWash)
							f.Row().Fill().Items(ui.Center).Justify(ui.Center).Children(func() {
								f.Icon(lucideIcons[iconPencil]).Size(14, 14).Color(state.pal.muted).Shrink(0)
							})
						})
					}
				})
			}
		})
		f.End()
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

	components.PaintInput(canvas, state.Theme(), search.Rect(), components.InputSnapshot{
		ID: hitCommandPaletteSearch, Label: title, Value: value,
		Placeholder: placeholder, Focused: focused, PlatformEditing: focused && value != "",
		NavigationKeys: true,
	})
	if sending {
		state.paintSnippetSend(canvas, sendInput, sendButton, sendTargets, sendRun)
	}
	dialog.End()
}

// commandPaletteHeight is the dialog's height: the snippet picker adds the form
// that sends a typed command under its list.
func (state *State) commandPaletteHeight() float32 {
	if state.commandPalette.snippets {
		return 474 + 12 + 36 + 8 + 32
	}
	return 474
}

// paintFoldRow is the heading of a group the list can open and close: a choice
// like the rows under it, with a chevron for the way it will go.
func (state *State) paintFoldRow(f *ui.Frame, entry commandPaletteEntry, visible int) {
	selected := state.commandPalette.selected == visible
	color, detailColor := state.pal.muted, state.pal.faint
	row := f.Row().Height(30).PadX(10).Gap(8).Items(ui.Center).Shrink(0)
	if selected {
		color, detailColor = state.pal.onAccent, state.pal.onAccent
		row.Background(state.pal.accent).Radius(radius.item)
	}
	glyph := iconChevronRight
	if entry.expanded {
		glyph = iconChevronDown
	}
	row.HitOutset(2, 0, 2, 0).HitWith(hitCommandPaletteItems.At(visible), entry.label, gfx.HitOptions{Role: gfx.RoleButton, SkipTab: true, Expanded: entry.expanded}).Children(func() {
		f.Icon(lucideIcons[glyph]).Size(14, 14).Color(color).Shrink(0)
		f.Text(entry.label).FontSize(metrics.FontSizeM).Weight(gfx.WeightSemibold).Color(color).Ellipsis().Grow(1)
		if entry.detail != "" {
			f.Text(entry.detail).FontSize(metrics.FontSizeS).Color(detailColor).Shrink(0)
		}
	})
}
