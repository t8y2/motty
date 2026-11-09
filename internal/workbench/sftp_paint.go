package workbench

import (
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/sftpclient"
)

// The file panel is a column a few hundred pixels wide (a side panel or a
// drawer), so it is built like one: a title line with icon buttons, a toolbar
// of icon buttons, the path, a list of rows (type icon, name, date) and a
// status line. Nothing in it assumes the width of a page.
const (
	sftpPad              = float32(12)
	sftpRowHeight        = float32(30)
	sftpIconSize         = float32(16)
	sftpButton           = float32(28)
	sftpListHeaderHeight = float32(28)
	// The title row is as tall as the terminal's info bar and its buttons are that
	// bar's size, so the two rows share a center line across the window.
	sftpHeaderButton = float32(24)
)

// sftpIconButton is an icon button declared into a slot of the page and painted
// once the page has been laid out.
type sftpIconButton struct {
	box      ui.Box
	id       gfx.ID
	icon     uint8
	hint     string
	disabled bool
	style    iconButtonStyle
	behind   gfx.Color
}

func (state *State) paintSFTPIconButtons(canvas *gfx.Canvas, buttons []sftpIconButton) {
	for index := range buttons {
		button := &buttons[index]
		behind := button.behind
		if behind.Alpha == 0 {
			behind = state.pal.workspace
		}
		state.paintIconButton(canvas, button.id, button.icon, button.box.Rect(), button.hint, button.disabled, button.style, behind)
	}
}

func (state *State) paintSFTPPage(canvas *gfx.Canvas, size gfx.Size, session *Session) {
	view := session.files
	if view == nil {
		return
	}
	if view.editPath != "" {
		state.paintSFTPEditor(canvas, size, session, view)
		return
	}
	pal, messages := state.pal, state.messages
	headerBackground, headerBorder := pal.workspace, pal.line
	folder, local := view.active(), view.local
	if folder.pathEditing && !state.sftpPathInput && canvas.Focused() != hitSFTPPath {
		folder.pathEditing = false
		folder.resetPathInput()
	}

	// What the page shows decides its parts. The selection bar and the file
	// actions are as tall as what they hold; the list takes what they leave.
	selectedCount, selected := selectedSFTP(view)
	one := selectedCount == 1
	places := sftpPlaceBrowser{owner: state, session: session, view: view}
	starred := places.directoryFavorite(local)
	starIcon, starHint := uint8(iconStar), messages.SFTPWindow.FavoriteAdd
	if starred {
		starIcon, starHint = iconStarSolid, messages.SFTPWindow.FavoriteRemove
	}
	paneIcon, paneHint := uint8(iconHardDrive), messages.Tools.PaneLocal
	if local {
		paneIcon, paneHint = iconHosts, messages.Tools.PaneRemote
	}
	title := messages.SFTPFiles
	if local {
		title = messages.Tools.LocalFiles
	}
	status := view.status
	statusColor := pal.muted
	if view.errorText != "" {
		status = view.errorText
		statusColor = pal.danger
	}
	var buttons [24]sftpIconButton
	var headerList [8]barTool
	headerTools := headerList[:0]
	addHeader := func(id gfx.ID, glyph uint8, hint string, disabled bool) {
		headerTools = append(headerTools, barTool{id: id, icon: glyph, hint: hint, disabled: disabled})
	}
	if local {
		addHeader(hitSFTPNewDirectory, iconFolderPlus, messages.SFTPNewDirectory, false)
	} else {
		addHeader(hitSFTPUpload, iconUpload, messages.SFTPUpload, view.busy)
		addHeader(hitSFTPUploadDirectory, iconFolderUp, messages.SFTPUploadDirectory, view.busy)
		addHeader(hitSFTPNewDirectory, iconFolderPlus, messages.SFTPNewDirectory, view.busy)
		addHeader(hitSFTPNewLink, iconLink, messages.Tools.TipNewLink, view.busy)
	}
	if !view.localOnly {
		addHeader(hitSFTPPaneToggle, paneIcon, paneHint, false)
	}
	addHeader(hitSFTPRefresh, iconRefresh, messages.SFTPRefresh, view.busy && !local)
	moveIcon, moveHint := state.sftpPanelMoveAction()
	addHeader(hitSFTPPanelSide, moveIcon, moveHint, false)
	addHeader(hitSFTPClose, iconClose, messages.TipSFTPClose, false)
	headerTools = state.orderToolbarTools(toolbarSFTP, headerTools, false)
	buttonCount, headerButtonCount := 0, 0
	var header, crumbs, filterSlot, listSlot, spinnerSlot, moreButton ui.Box

	f := state.filesLayout.Begin(canvas, state.Theme(), gfx.Rect{Y: windowBarHeight, Width: size.Width, Height: max(0, size.Height-windowBarHeight)})
	// icon declares an icon button where it goes; it is painted after End.
	icon := func(side float32, id gfx.ID, glyph uint8, hint string, disabled bool, style iconButtonStyle) {
		buttons[buttonCount] = sftpIconButton{box: f.Slot().Size(side, side), id: id, icon: glyph, hint: hint, disabled: disabled, style: style}
		buttonCount++
	}
	f.Column().Fill().PadSides(0, sftpPad, 6, sftpPad).Background(pal.workspace).Children(func() {
		// One row for the panel: its name, then the actions on the folder and
		// refresh and close, from the right. The words "Files" and the address say
		// nothing the tab does not already; the room goes to the list.
		header = f.Row().FillWidth().Height(infoBarHeight).Shrink(0).MarginSides(0, -sftpPad, 0, -sftpPad).PadX(sftpPad).Background(headerBackground).Items(ui.Center).Children(func() {
			f.Text(title).FontSize(metrics.FontSizeM).Weight(gfx.WeightSemibold).Color(pal.text).Ellipsis().Grow(1).Height(20).MarginSides(0, 8, 0, 0)
			f.Row().Gap(4).Shrink(0).Children(func() {
				for _, tool := range headerTools {
					icon(sftpHeaderButton, tool.id, tool.icon, tool.hint, tool.disabled, iconButtonQuiet)
				}
				// Filtering and turning hidden entries on are secondary actions. Keep
				// the header stable at the narrow default width; once hidden entries are
				// enabled, the Eye control remains visible as their status affordance.
				if folder.showHidden {
					icon(sftpHeaderButton, hitSFTPHiddenToggle, iconEye, messages.Tools.FilesHideHidden, false, iconButtonActive)
				}
				icon(sftpHeaderButton, hitSFTPHeaderMore, iconEllipsis, messages.Tools.FilesMore, false, iconButtonQuiet)
			})
		})
		headerButtonCount = buttonCount

		// Where we are: one level up, then the tail of the path as many folders as
		// fit. The bookmark controls close the row on the right: the list of the
		// folders kept, and a star for this one.
		f.Row().Height(sftpButton).Shrink(0).Gap(4).Items(ui.Center).MarginSides(6, 0, 0, 0).Children(func() {
			icon(sftpButton, hitSFTPUp, iconArrowUp, messages.SFTPUp, !local && view.busy || folder.path == "/" || folder.path == "", iconButtonQuiet)
			height := float32(24)
			if state.sftpPathVisible(folder) {
				height = sftpButton
			}
			crumbs = f.Slot().Grow(1).Height(height)
			listID, starID, hint := hitSFTPBookmarkList, hitSFTPBookmarkToggle, messages.SFTPWindow.DirectoryFavorites
			if local {
				listID, starID, hint = hitSFTPLocalPlaces, hitSFTPLocalStar, messages.SFTPWindow.LocalPlaces
			}
			f.Row().Gap(2).Shrink(0).Children(func() {
				icon(sftpButton, listID, iconChevronDown, hint, false, iconButtonQuiet)
				icon(sftpButton, starID, starIcon, starHint, folder.path == "" || !local && (view.busy || !places.connected() || session.host.CatalogID == ""), iconButtonQuiet)
			})
		})
		if folder.filterOpen {
			// The filter, and the switch that takes it into the subfolders.
			f.Row().Height(32).Shrink(0).Gap(4).Items(ui.Center).MarginSides(5, 0, 4, 0).Children(func() {
				filterSlot = f.Slot().Grow(1).Height(32)
				if sftpSearchAvailable(view, local) {
					style := iconButtonQuiet
					if folder.deep {
						style = iconButtonActive
					}
					icon(sftpButton, hitSFTPSearchToggle, iconFolderOpen, messages.Tools.FilesSearchSubfolders, false, style)
				}
			})
		}
		f.Divider().Background(pal.line).MarginSides(2, 0, 3, 0)

		listSlot = f.Slot().Grow(1)

		if selectedCount > 0 {
			// What can be done with the selection, under the list so the rows above
			// do not move when one is picked.
			rowBottom := float32(2)
			if one {
				rowBottom = 0 // the details line keeps that room
			}
			f.Column().Shrink(0).Children(func() {
				f.Divider().Background(pal.line)
				f.Row().Gap(2).Height(sftpButton).Shrink(0).MarginSides(5, 0, rowBottom, 0).Children(func() {
					add := func(id gfx.ID, glyph uint8, label string, danger bool) {
						style := iconButtonQuiet
						if danger {
							style = iconButtonDanger
						}
						icon(sftpButton, id, glyph, label, view.busy, style)
					}
					if one {
						add(hitSFTPOpenEntry, iconFolderOpen, messages.SFTPOpen, false)
					}
					if view.localOnly {
						add(hitSFTPInsertPath, iconTerminal, messages.SFTPInsertPath, session.term == nil || session.term.shell == nil)
					} else if local {
						add(hitSFTPSend, iconUpload, messages.Tools.SendToServer, false)
					} else {
						add(hitSFTPDownload, iconDownload, messages.SFTPDownload, false)
						if view.lfolder.path != "" {
							add(hitSFTPDownloadHere, iconFileDown, messages.Tools.DownloadHere, false)
						}
					}
					if one && !local {
						if !selected.IsDir() {
							add(hitSFTPEditExternal, iconExternal, messages.Tools.ExtEditButton, false)
						}
						add(hitSFTPRename, iconPencil, messages.SFTPRename, false)
						add(hitSFTPMove, iconFolderOpen, messages.SFTPMove, false)
						add(hitSFTPChmod, iconLock, messages.SFTPPermissions, false)
						if _, archive := state.selectedArchive(session, view); archive {
							add(hitSFTPExtract, iconArchive, messages.Tools.Extract, false)
						}
					}
					if !local {
						add(hitSFTPDelete, iconTrash, messages.SFTPDelete, true)
					}
				})
				if one {
					f.Text(state.sftpDetailsLine(view, selected)).FontSize(metrics.FontSizeS).Color(pal.muted).Ellipsis().Height(18).Shrink(0).MarginSides(2, 0, 2, 2)
				}
			})
		}

		if local {
			state.declareLocalCoverage(f, folder, hitSFTPCoverage)
		}
		// What is happening.
		statusRow := f.Row().Shrink(0).Gap(8).Items(ui.Center).MarginSides(6, 0, 0, 0)
		if folder.searching() {
			// What a search read, how much of it, and what it could not read are
			// too long for one line of a narrow panel, and the last of them is
			// the one not to lose.
			statusRow = statusRow.MinHeight(28)
		} else {
			statusRow = statusRow.Height(28)
		}
		statusRow.Children(func() {
			if view.busy {
				spinnerSlot = f.Slot().Size(16, 16)
			}
			if folder.searching() {
				f.Paragraph(status, 3).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(statusColor).Grow(1).MarginSides(4, 0, 4, 0)
			} else {
				f.Text(status).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(statusColor).Ellipsis().Grow(1)
			}
			if view.busy {
				f.Button(hitSFTPCancel, messages.SFTPCancelTransfer).Small().Outline()
			}
		})
	})
	f.End()
	for index := range buttons[:buttonCount] {
		if buttons[index].id == hitSFTPHeaderMore {
			moreButton = buttons[index].box
			break
		}
	}

	headerBounds := header.Rect()
	canvas.Fill(gfx.Rect{X: headerBounds.X, Y: headerBounds.Y + headerBounds.Height - 1, Width: headerBounds.Width, Height: 1}, headerBorder)
	for i := 0; i < headerButtonCount; i++ {
		buttons[i].behind = headerBackground
	}
	state.paintSFTPIconButtons(canvas, buttons[:buttonCount])
	if folder.headerMenuOpen && state.sftpHeaderMenuAvailable(view) {
		state.requestSFTPHeaderMenu(moreButton.Rect(), folder, view)
	}
	if folder.filterOpen {
		state.paintSFTPFilter(canvas, filterSlot.Rect(), folder)
	}
	if state.sftpPathVisible(folder) {
		state.paintSFTPPathInput(canvas, crumbs.Rect(), view)
	} else {
		state.paintSFTPBreadcrumbs(canvas, crumbs.Rect(), folder.crumbs)
	}
	state.paintSFTPList(canvas, size, listSlot.Rect(), session, view)
	view.places.anchors[sideIndex(local)] = crumbs.Rect()
	if view.bookmarksOpen {
		places.paintDirectoryPlaces(canvas, gfx.Rect{Y: windowBarHeight, Width: size.Width, Height: max(0, size.Height-windowBarHeight)})
	}
	if view.busy {
		components.PaintSpinner(canvas, state.Theme(), spinnerSlot.Rect(), components.SpinnerPhase(canvas.Now()/(125*time.Millisecond)))
		canvas.RequestFrame()
	}
}

// paintSFTPList declares the visible rows of the list into the slot the page
// left for it, then paints what is drawn rather than laid out: the place
// dropped files would go and the scroll bar. Which rows are visible is
// arithmetic; where each goes and what is inside it is flex.
func (state *State) paintSFTPList(canvas *gfx.Canvas, size gfx.Size, list gfx.Rect, session *Session, view *sftpView) {
	pal := state.pal
	folder, local := view.active(), view.local
	// Search results are the same list without the columns that describe a
	// folder's entries: a result is where it is, and the panel is narrow.
	searching, rows := folder.searching(), folder.shownRows()
	headerHeight := min(sftpListHeaderHeight, max(list.Height, 0))
	if searching {
		headerHeight = 0
	}
	body := gfx.Rect{X: list.X, Y: list.Y + headerHeight, Width: list.Width, Height: max(list.Height-headerHeight, 0)}
	visibleRows := max(0, int(body.Height/sftpRowHeight))
	scrolls := len(rows) > visibleRows && visibleRows > 0
	rowWidth := body.Width
	if scrolls {
		rowWidth = max(rowWidth-10, 0)
	}
	sizeWidth, modifiedWidth := sftpColumnWidths(rowWidth)
	empty := len(rows) == 0 && (!view.busy || local) && view.errorText == ""
	loading := len(rows) == 0 && view.busy && !local && view.operation == sftpclient.OperationList && view.errorText == ""
	reading := searching && len(rows) == 0 && folder.search.running && folder.search.failure == ""
	if reading {
		empty, loading = false, false
	}
	boxes := state.filesRowBoxes[:0]
	var dropColumn ui.Box

	if !searching {
		state.paintSFTPColumnHeader(canvas, gfx.Rect{X: list.X, Y: list.Y, Width: list.Width, Height: headerHeight}, folder, sizeWidth, modifiedWidth, scrolls)
	}

	if !view.scrollDrag {
		if folder.scroll.MaxOffsetY <= 0 || folder.scrollOffset > folder.scroll.MaxOffsetY || int(folder.scrollOffset/sftpRowHeight) != folder.first {
			folder.scrollOffset = float32(folder.first) * sftpRowHeight
		}
	}
	area := components.BeginScrollArea(canvas, state.Theme(), body, gfx.Size{Width: body.Width, Height: float32(len(rows)) * sftpRowHeight}).
		Offset(0, folder.scrollOffset).
		VerticalScrollbar(hitSFTPListScrollbar, state.messages.SFTPFiles, "")
	geometry := area.BeginContent()
	folder.scroll = geometry
	folder.scrollOffset = geometry.OffsetY
	first := min(int(geometry.OffsetY/sftpRowHeight), max(0, len(rows)-max(visibleRows, 1)))
	folder.first = first
	last := min(len(rows), first+int(math.Ceil(float64(body.Height/sftpRowHeight)))+2)
	f := state.filesList.Begin(canvas, state.Theme(), gfx.Rect{
		X: body.X, Y: body.Y - geometry.OffsetY + float32(first)*sftpRowHeight,
		Width: rowWidth, Height: float32(last-first) * sftpRowHeight,
	})
	f.Column().FillWidth().Children(func() {
		dropColumn = f.Column().Grow(1)
		dropColumn.Children(func() {
			for index := first; index < last; index++ {
				boxes = append(boxes, state.declareSFTPRow(
					f, &rows[index], hitSFTPEntryList.At(index-first), sizeWidth, modifiedWidth, searching, rowWidth,
				))
			}
		})
	})
	f.End()
	area.EndContent()

	folder.visibleRows = visibleRows
	state.filesRowBoxes = boxes
	if loading || reading {
		spinnerSize := float32(16)
		spinner := gfx.Rect{
			X: body.X + (body.Width-spinnerSize)/2, Y: body.Y + (body.Height-spinnerSize)/2,
			Width: spinnerSize, Height: spinnerSize,
		}
		components.PaintSpinner(canvas, state.Theme(), spinner, components.SpinnerPhase(canvas.Now()/(125*time.Millisecond)))
		label := state.messages.SFTPLoading
		if reading {
			label = state.messages.Tools.FilesSearchSearching
		}
		canvas.LabelCenter(label, gfx.Rect{
			X: body.X, Y: body.Y + body.Height/2 + 14, Width: body.Width, Height: 22,
		}, gfx.TextStyle{Size: metrics.FontSizeM, Color: pal.muted})
	} else if empty {
		label := state.messages.SFTPEmpty
		switch {
		case searching && folder.search.failure != "":
			label = state.messages.Tools.FilesSearchUnavailable // the status line has the reason
		case searching:
			label = state.messages.Tools.FilesSearchNoMatches
		case local && folder.coverageEmpty != "":
			label = folder.coverageEmpty
		}
		canvas.LabelCenter(label, body, gfx.TextStyle{Size: metrics.FontSizeM, Color: pal.muted})
	}

	if view.dropping && !local {
		// Where dropped files would go: the folder row they are over, or the folder.
		target := dropColumn.Rect()
		if row := view.dropRow; row >= first && row < last {
			target = boxes[row-first].Rect()
		}
		state.paintSFTPDropOverlay(canvas, target, state.messages.Tools.DropUploadHint)
	}
}

func sftpColumnWidths(width float32) (size, modified float32) {
	modified = 82
	if width >= 360 {
		modified = 108
	}
	size = 54
	if width >= 360 {
		size = 64
	}
	return
}

func (state *State) paintSFTPColumnHeader(canvas *gfx.Canvas, bounds gfx.Rect, folder *sftpFolder, sizeWidth, modifiedWidth float32, scrolls bool) {
	if bounds.Height <= 0 {
		return
	}
	width := bounds.Width
	if scrolls {
		width = max(width-10, 0)
	}
	f := state.filesHeader.Begin(canvas, state.Theme(), gfx.Rect{X: bounds.X, Y: bounds.Y, Width: width, Height: bounds.Height})
	f.Row().Fill().PadX(8).Items(ui.Center).Children(func() {
		state.declareSFTPColumnHeader(f, folder, 0, state.messages.SFTPWindow.Name, 0, true)
		for _, entry := range state.sftpColumns.entries[1:] {
			if !entry.Enabled {
				continue
			}
			label, width := state.messages.SFTPWindow.Size, sizeWidth
			if entry.Key == sftpColumnModified {
				label, width = state.messages.SFTPWindow.Modified, modifiedWidth
			}
			state.declareSFTPColumnHeader(f, folder, int(entry.Key), label, width, false)
		}
	})
	f.End()
	canvas.Fill(gfx.Rect{X: bounds.X, Y: bounds.Y + bounds.Height - 1, Width: width, Height: 1}, withAlpha(state.pal.muted, 0.18))
}

func (state *State) declareSFTPColumnHeader(f *ui.Frame, folder *sftpFolder, column int, label string, width float32, grow bool) {
	id := hitSFTPColumnList.At(column)
	active := folder.sortChosen && folder.sortOrder.column == column
	text := label
	if active {
		if folder.sortOrder.descending {
			text += " ↓"
		} else {
			text += " ↑"
		}
	}
	box := f.Row().Height(sftpListHeaderHeight).Shrink(0)
	if grow {
		box = box.Grow(1)
	} else {
		box = box.Width(width)
	}
	box = box.Items(ui.Center).HitWith(id, text, gfx.HitOptions{Role: gfx.RoleButton, Selected: active, Hint: label})
	box.Children(func() {
		color := state.pal.muted
		if active {
			color = state.pal.accent
		}
		textBox := f.Text(text).FontSize(metrics.FontSizeS).Color(color).LineHeight(sftpListHeaderHeight).Ellipsis()
		if grow {
			textBox = textBox.Grow(1)
		} else {
			textBox = textBox.Width(width - 4).TextEnd()
		}
	})
}

// declareSFTPRow declares one entry: its type icon, its name, which takes the
// room and ends with an ellipsis, and, when there is room, its modification
// time. A selected row is tinted; the pointer lightens one. It returns the row.
// A search result has its folder after the name, where the columns would be.
func (state *State) declareSFTPRow(f *ui.Frame, row *sftpRow, id gfx.ID, sizeWidth, modifiedWidth float32, result bool, rowWidth float32) ui.Box {
	pal, canvas := state.pal, f.Canvas()
	name := row.cells[1]
	if row.entry.IsLink() && row.entry.LinkTarget != "" {
		name = row.cells[1] + " → " + row.entry.LinkTarget
	}
	nameColor := pal.text
	if row.entry.LinkBroken {
		nameColor = pal.danger
	}
	tint := gfx.Color{Alpha: 1} // a colored glyph keeps its own colors
	if row.mono {
		tint = pal.muted
		if row.entry.Mode&os.ModeSymlink != 0 {
			tint = pal.accent
		}
	}
	var background gfx.Color
	switch {
	case row.selected:
		background = pal.accentSoft
	case canvas.Hovered() == id:
		background = withAlpha(pal.surfaceHot, 0.7)
	}
	options := gfx.HitOptions{Role: gfx.RoleButton, Selected: row.selected}
	if result {
		options.Hint = row.where // the whole folder, when the row has room for part of it
	}
	box := f.Row().Height(sftpRowHeight).Shrink(0).Items(ui.Center).HitWith(id, row.cells[1], options)
	contents := func() {
		declareSFTPRowIcon(f, row, tint)
		label := f.Row().Grow(1).Clip().Items(ui.Center).MarginSides(0, 0, 0, 8)
		label.Children(func() {
			if result {
				state.declareSFTPResultLabel(f, row, nameColor, rowWidth)
				return
			}
			if row.matchEnd > row.matchStart && row.matchEnd <= len(row.entry.Name) {
				if prefix := row.entry.Name[:row.matchStart]; prefix != "" {
					f.Text(prefix).FontSize(metrics.FontSizeM).Color(nameColor).Shrink(0)
				}
				state.declareSFTPMatch(f, row.entry.Name[row.matchStart:row.matchEnd])
				suffix := row.entry.Name[row.matchEnd:]
				if row.entry.IsLink() && row.entry.LinkTarget != "" {
					suffix += " → " + row.entry.LinkTarget
				}
				f.Text(suffix).FontSize(metrics.FontSizeM).Color(nameColor).Ellipsis().Grow(1)
				return
			}
			f.Text(name).FontSize(metrics.FontSizeM).Color(nameColor).Ellipsis().Grow(1)
		})
		for _, entry := range state.sftpColumns.entries[1:] {
			if !entry.Enabled || result {
				continue
			}
			if entry.Key == sftpColumnSize {
				f.Text(row.cells[2]).FontSize(metrics.FontSizeS).Color(pal.muted).Width(sizeWidth).TextEnd().Shrink(0)
				continue
			}
			if entry.Key == sftpColumnModified {
				date := row.cells[3]
				if modifiedWidth < 100 && len(date) > 5 {
					date = date[5:] // drop the year
				}
				f.Text(date).FontSize(metrics.FontSizeS).Color(pal.muted).Width(modifiedWidth).TextEnd().Shrink(0)
			}
		}
	}
	if background.Alpha == 0 && canvas.FocusVisible() != id {
		// Most rows have nothing behind them: their parts go straight in the row.
		return box.PadX(8).Children(contents)
	}
	// A tinted or focused row is a rounded shape a pixel in from the top and
	// bottom of the row, so the parts go in a box of that shape.
	return box.PadY(1).Children(func() {
		inner := f.Row().Grow(1).Self(ui.Stretch).Items(ui.Center).PadX(8).Background(background).Radius(radius.control)
		if canvas.FocusVisible() == id {
			inner.Border(1.5, pal.accent)
		}
		inner.Children(contents)
	})
}

// declareSFTPMatch declares the part of a name the query matched: heavier than
// the rest, and on a tint of the accent that shows in light and dark alike.
func (state *State) declareSFTPMatch(f *ui.Frame, match string) {
	pal := state.pal
	f.Text(match).FontSize(metrics.FontSizeM).Weight(gfx.WeightSemibold).Color(pal.text).Background(withAlpha(pal.accent, 0.3)).Radius(radius.control).Shrink(0)
}

// declareSFTPResultLabel declares what a search result says: its name with the
// match marked, then its folder, which gives way when room is short. A folder
// too long for the room loses its middle, as the first and the last folders say
// the most about where a file is.
func (state *State) declareSFTPResultLabel(f *ui.Frame, row *sftpRow, nameColor gfx.Color, rowWidth float32) {
	pal, canvas := state.pal, f.Canvas()
	name := row.entry.Name
	start, end := row.matchStart, row.matchEnd
	if end <= start || end > len(name) {
		start, end = 0, 0
	}
	if prefix := name[:start]; prefix != "" {
		f.Text(prefix).FontSize(metrics.FontSizeM).Color(nameColor).Shrink(0)
	}
	if end > start {
		state.declareSFTPMatch(f, name[start:end])
	}
	if suffix := name[end:]; suffix != "" {
		f.Text(suffix).FontSize(metrics.FontSizeM).Color(nameColor).Shrink(0)
	}
	if row.where == "" {
		return
	}
	// Left for the folder: the row less its padding, the icon, the gaps and the
	// name, which is a little wider in its heavy match.
	room := rowWidth - 16 - 30 - 16 - canvas.TextWidth(name, metrics.FontSizeM) - 6
	where := elideSFTPFolder(row.where, func(text string) bool { return canvas.TextWidth(text, metrics.FontSizeS) <= room })
	f.Text(where).FontSize(metrics.FontSizeS).Color(pal.muted).Ellipsis().Grow(1).MarginSides(0, 0, 0, 8)
}

// elideSFTPFolder shortens a folder path that does not fit by dropping folders
// from its middle: the first and as many of the last as fit, with an ellipsis
// between. fits says whether a text fits the room.
func elideSFTPFolder(dir string, fits func(string) bool) string {
	if fits(dir) {
		return dir
	}
	parts := strings.Split(dir, "/")
	if len(parts) < 3 {
		return dir
	}
	for keep := len(parts) - 2; keep >= 1; keep-- {
		if candidate := parts[0] + "/…/" + strings.Join(parts[len(parts)-keep:], "/"); fits(candidate) {
			return candidate
		}
	}
	return parts[0] + "/…/" + parts[len(parts)-1]
}

// paintSFTPBreadcrumbs draws the path as links, keeping the folders nearest the
// current one and folding the rest into an ellipsis when the path is wider than
// the panel.
func (state *State) paintSFTPBreadcrumbs(canvas *gfx.Canvas, bounds gfx.Rect, crumbList []sftpCrumb) {
	// The path reads at the size of the list under it, not the theme's caption.
	// A separator is a chevron the size of the text with gap-2.5 (10) on each side.
	const size, separator = float32(13), float32(33)
	first := 0
	total := float32(0)
	for index := len(crumbList) - 1; index >= 0; index-- {
		next := total + canvas.TextWidth(crumbList[index].label, size) + separator
		if index < len(crumbList)-1 && next > bounds.Width-separator*2 {
			first = index + 1
			break
		}
		total = next
	}
	canvas.HitWithOptions(hitSFTPPathEdit, bounds, state.messages.Tools.FilesPathLabel, gfx.HitOptions{Role: gfx.RoleButton, Hint: state.messages.Tools.FilesPathPlaceholder})
	breadcrumb := components.BeginBreadcrumb(canvas, state.Theme(), bounds).FontSize(size)
	if first > 0 {
		breadcrumb.AddEllipsis(gfx.ID{})
	}
	for index := first; index < len(crumbList); index++ {
		if index == len(crumbList)-1 {
			breadcrumb.AddCurrent(crumbList[index].label)
		} else {
			breadcrumb.AddLink(hitSFTPBreadcrumbList.At(index), crumbList[index].label)
		}
	}
}

func (state *State) paintSFTPEditor(canvas *gfx.Canvas, size gfx.Size, session *Session, view *sftpView) {
	pal, messages := state.pal, state.messages
	dirty := sftpEditorDirty(view)
	status := view.status
	statusColor := pal.muted
	if dirty && !view.busy {
		status = messages.SFTPEditDirty
	}
	if view.errorText != "" {
		status = view.errorText
		statusColor = pal.danger
	}

	var textarea, spinner ui.Box
	f := state.filesLayout.Begin(canvas, state.Theme(), gfx.Rect{Y: windowBarHeight, Width: size.Width, Height: max(0, size.Height-windowBarHeight)})
	f.Column().Fill().PadSides(14, 18, 10, 18).Background(pal.workspace).Children(func() {
		// Back on the left, the file's name and place beside it, the actions from
		// the right.
		f.Row().Height(36).Shrink(0).Children(func() {
			f.Button(hitSFTPEditBack, messages.SFTPEditBack).Small().Outline().Disabled(view.busy)
			f.Spacer()
			f.Row().Gap(8).Shrink(0).Children(func() {
				f.Button(hitSFTPEditReload, messages.SFTPEditReload).Small().Secondary().Disabled(view.busy)
				f.Button(hitSFTPEditSave, messages.SFTPEditSave).Small().Disabled(view.busy || !dirty)
			})
		})
		// Under the actions, whole: beside them the name of a file had no room in a
		// panel this narrow.
		f.Column().Shrink(0).MarginSides(8, 0, 0, 0).Children(func() {
			f.Text(view.editName).FontSize(18).Weight(gfx.WeightSemibold).Color(pal.text).Ellipsis().LineHeight(26)
			f.Text(view.editLocation).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.muted).Ellipsis().LineHeight(20)
		})
		f.Text(messages.SFTPEditSafety).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.muted).LineHeight(22).MarginSides(10, 0, 6, 0)
		textarea = f.Slot().Grow(1).MinHeight(64)
		f.Row().Height(28).Shrink(0).Gap(8).Items(ui.Center).MarginSides(14, 0, 0, 0).Children(func() {
			if view.busy {
				spinner = f.Slot().Size(16, 16)
			}
			f.Text(status).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(statusColor).Ellipsis().Grow(1)
			if view.busy {
				// With the file list's own place for it: in the header it did not
				// fit beside the other three in a panel this narrow.
				f.Button(hitSFTPCancel, messages.SFTPCancelTransfer).Small().Secondary()
			}
		})
	})
	f.End()

	value := view.editEditor.String()
	focused := canvas.Focused() == hitSFTPEditor
	selection := view.editEditor.Selection()
	snapshot := components.TextareaSnapshot{
		ID: hitSFTPEditor, Label: messages.SFTPEditLabel, Value: value,
		Focused: focused, HideEditorText: true, ShowCaret: selection.Start == selection.End, ReadOnly: view.busy,
		Layout:         &view.editLayout,
		SelectionStart: aiByteOffset(value, selection.Start), SelectionEnd: aiByteOffset(value, selection.End),
		Caret: aiByteOffset(value, view.editEditor.Caret()),
	}
	if marked, composing := view.editEditor.Composition(); composing {
		snapshot.CompositionStart, snapshot.CompositionEnd = aiByteOffset(value, marked.Start), aiByteOffset(value, marked.End)
	}
	view.editRect = textarea.Rect()
	components.PaintTextarea(canvas, state.Theme(), view.editRect, snapshot)
	if view.busy {
		components.PaintSpinner(canvas, state.Theme(), spinner.Rect(), components.SpinnerPhase(canvas.Now()/(125*time.Millisecond)))
		canvas.RequestFrame()
	}
}

func (state *State) paintSFTPDialog(canvas *gfx.Canvas, viewport gfx.Rect, view *sftpView) {
	messages, theme := state.messages, state.Theme()
	title := ""
	description := ""
	fieldLabel := ""
	submitLabel := ""
	cancelLabel := messages.Cancel
	initialFocus := hitSFTPDialogField
	submitVariant := components.ButtonDefault
	switch view.dialog {
	case sftpDialogBrowseCreateDirectory:
		m := messages.SFTPWindow
		title, submitLabel, cancelLabel = m.BrowseCreateTitle, m.BrowseCreateConfirm, m.BrowsePause
		initialFocus = hitSFTPDialogCancel
		if w := view.detached; w != nil && w.browse.pending != nil {
			request := w.browse.pending
			side := m.Remote
			if request.side == 0 {
				side = m.Local
			}
			description = fmt.Sprintf(m.BrowseCreateDescriptionFmt, side, request.paths[request.side])
		}
	case sftpDialogCreateDirectory:
		title = messages.SFTPNewDirectoryTitle
		fieldLabel = messages.SFTPDirectoryName
		submitLabel = messages.SFTPCreateDirectory
	case sftpDialogRename:
		title = messages.SFTPRenameTitle
		description = messages.SFTPRenameDescription
		fieldLabel = messages.SFTPNewName
		submitLabel = messages.SFTPRenameSubmit
	case sftpDialogMove:
		title = messages.SFTPMoveTitle
		description = messages.SFTPMoveDescription
		fieldLabel = messages.SFTPMoveDestination
		submitLabel = messages.SFTPMoveSubmit
	case sftpDialogDelete:
		title = messages.SFTPDeleteFileTitle
		description = fmt.Sprintf(messages.SFTPDeleteDescriptionFmt, view.dialogEntry.Name)
		if len(view.dialogEntries) > 1 {
			title = messages.SFTPDeleteSelectionTitle
			description = fmt.Sprintf(messages.SFTPDeleteSelectionFmt, len(view.dialogEntries))
		} else if view.dialogEntry.IsDir() && !view.dialogEntry.IsLink() {
			// A folder goes with everything in it; the confirmation says so.
			title = messages.SFTPDeleteDirectoryTitle
			description = fmt.Sprintf(messages.SFTPDeleteFolderContentsFmt, view.dialogEntry.Name)
		}
		submitLabel = messages.SFTPDeleteConfirm
		initialFocus = hitSFTPDialogCancel
		submitVariant = components.ButtonDestructive
	case sftpDialogChmod:
		title = messages.SFTPPermissionsTitle
		description = messages.SFTPPermissionsDescription
		fieldLabel = messages.SFTPPermissionsMode
		submitLabel = messages.SFTPPermissionsSubmit
	case sftpDialogSymlink:
		title = messages.Tools.LinkTitle
		fieldLabel = messages.Tools.LinkName
		submitLabel = messages.Tools.LinkCreate
	case sftpDialogExtract:
		title = messages.Tools.ExtractTitle
		description = fmt.Sprintf(messages.Tools.ExtractDescriptionFmt, view.dialogEntry.Name)
		fieldLabel = messages.Tools.ExtractFolder
		submitLabel = messages.Tools.ExtractConfirm
	case sftpDialogCompress:
		title = messages.Tools.CompressTitle
		description = fmt.Sprintf(messages.Tools.CompressDescriptionFmt, view.dialogEntry.Name)
		fieldLabel = messages.Tools.CompressName
		submitLabel = messages.Tools.CompressConfirm
	case sftpDialogDiscardEdit:
		title = messages.SFTPEditDiscardTitle
		description = messages.SFTPEditDiscardDescription
		submitLabel = messages.SFTPEditDiscardConfirm
		initialFocus = hitSFTPDialogCancel
		submitVariant = components.ButtonDestructive
	case sftpDialogReloadEdit:
		title = messages.SFTPEditReloadTitle
		description = messages.SFTPEditReloadDescription
		submitLabel = messages.SFTPEditReloadConfirm
		initialFocus = hitSFTPDialogCancel
		submitVariant = components.ButtonDestructive
	case sftpDialogRemoteChanged:
		title = messages.SFTPEditConflictTitle
		description = messages.SFTPEditConflictDescription
		submitLabel = messages.SFTPEditReloadConfirm
		initialFocus = hitSFTPDialogCancel
	}
	fieldDialog := view.dialog == sftpDialogCreateDirectory || view.dialog == sftpDialogRename || view.dialog == sftpDialogMove || view.dialog == sftpDialogChmod ||
		view.dialog == sftpDialogSymlink || view.dialog == sftpDialogExtract || view.dialog == sftpDialogCompress
	showError := view.dialog == sftpDialogDelete && view.dialogError != ""
	body := float32(0) // a confirmation is only its text
	switch {
	case view.dialog == sftpDialogSymlink:
		body = 6 + 64 + 8 + 64 // the link's name and where it points
	case view.dialog == sftpDialogChmod:
		// The octal field, then the permission grid: a 16px header row and
		// three 24px rows, with a 4px gap between them.
		body = 6 + 64 + 8 + 16 + 3*24 + 3*4
	case fieldDialog:
		body = 6 + 64
	case showError:
		body = 20
	}
	// No scrim dismiss: a press beside the form must not discard its input.
	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{
		Layer:          dialogLayer,
		Scope:          dialogScope,
		CloseID:        hitSFTPDialogClose,
		InitialFocusID: initialFocus,
		ContentSize:    gfx.Size{Width: 520}, FitBody: true, BodyHeight: body,
	})
	layout := dialog.Begin(title, description)
	var first, second ui.Box
	var chmodBoxes [9]ui.Box
	f := state.partLayout.Begin(canvas, theme, layout.Body)
	column := f.Column().Fill()
	if showError {
		column.Justify(ui.Center) // as a label in the whole body
	}
	column.Children(func() {
		switch {
		case fieldDialog:
			// One labelled input, two for a link: its name and where it points.
			f.Column().Shrink(0).Gap(8).PadSides(6, 4, 0, 4).Children(func() {
				first = f.Slot().Height(64).Shrink(0)
				if view.dialog == sftpDialogSymlink {
					second = f.Slot().Height(64).Shrink(0)
				}
				if view.dialog == sftpDialogChmod {
					chmodBoxes = state.declareSFTPChmodGrid(f)
				}
			})
		case showError:
			f.Text(view.dialogError).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(state.pal.danger).Ellipsis()
		}
	})
	f.End()
	if fieldDialog {
		field := components.BeginField(canvas, theme, first.Rect())
		if view.dialogError != "" {
			field = field.Invalid()
		}
		content := field.Paint(fieldLabel, "", view.dialogError)
		value := view.dialogEditor.String()
		focused := canvas.Focused() == hitSFTPDialogField
		components.PaintInput(canvas, theme, content, components.InputSnapshot{
			ID:              hitSFTPDialogField,
			Label:           fieldLabel,
			Value:           value,
			Focused:         focused,
			PlatformEditing: focused && value != "",
		})
		if view.dialog == sftpDialogChmod {
			state.paintSFTPChmodGrid(canvas, view, &chmodBoxes)
		}
		if view.dialog == sftpDialogSymlink {
			second := components.BeginField(canvas, theme, second.Rect()).Paint(messages.Tools.LinkTarget, "", "")
			secondValue := view.dialogEditor2.String()
			secondFocused := canvas.Focused() == hitSFTPDialogField2
			components.PaintInput(canvas, theme, second, components.InputSnapshot{
				ID: hitSFTPDialogField2, Label: messages.Tools.LinkTarget, Value: secondValue,
				Focused: secondFocused, PlatformEditing: secondFocused && secondValue != "",
			})
		}
	}
	dialog.Actions(layout,
		components.ButtonAction{ID: hitSFTPDialogCancel, Label: cancelLabel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitSFTPDialogSubmit, Label: submitLabel, Variant: submitVariant},
	)
	dialog.End()
}

// declareSFTPChmodGrid lays out the three permission rows (owner, group,
// others × read, write, execute) under the octal field and captures the nine
// checkbox slots; paintSFTPChmodGrid fills them in.
func (state *State) declareSFTPChmodGrid(f *ui.Frame) [9]ui.Box {
	m := state.messages
	groups := [3]string{m.SFTPPermissionsOwner, m.SFTPPermissionsGroup, m.SFTPPermissionsOthers}
	letters := [3]string{"R", "W", "X"}
	var boxes [9]ui.Box
	grid := f.Column().Shrink(0).Gap(4)
	grid.Children(func() {
		f.Row().Height(16).Shrink(0).Gap(8).Items(ui.Center).Children(func() {
			f.Slot().Width(72).Shrink(0)
			for _, letter := range letters {
				f.Text(letter).FontSize(metrics.FontSizeS).Color(state.pal.muted).Width(20).TextCenter().Shrink(0)
			}
			f.Spacer().Grow(1)
		})
		for group := range groups {
			f.Row().Height(24).Shrink(0).Gap(8).Items(ui.Center).Children(func() {
				f.Text(groups[group]).FontSize(metrics.FontSizeS).Color(state.pal.text).Width(72).Shrink(0)
				for column := range letters {
					boxes[group*3+column] = f.Slot().Size(20, 20).Shrink(0)
				}
				f.Spacer().Grow(1)
			})
		}
	})
	return boxes
}

// paintSFTPChmodGrid fills the declared permission rows: each box mirrors one
// bit of the octal field, so tapping it rewrites the field and typing in the
// field moves the boxes (issue #392). While the field holds no usable mode the
// boxes stand down.
func (state *State) paintSFTPChmodGrid(canvas *gfx.Canvas, view *sftpView, boxes *[9]ui.Box) {
	m := state.messages
	words := [3]string{m.SFTPPermissionsRead, m.SFTPPermissionsWrite, m.SFTPPermissionsExecute}
	groups := [3]string{m.SFTPPermissionsOwner, m.SFTPPermissionsGroup, m.SFTPPermissionsOthers}
	mode, ok := sftpChmodFromDialog(view)
	for index := range boxes {
		rect := boxes[index].Rect()
		checked := ok && mode&(1<<uint(8-index)) != 0
		checkbox := components.BeginCheckbox(canvas, state.Theme(), hitSFTPChmodBits.At(index), rect, checked)
		// The grid is tighter than the checkbox's 40x32 default target, whose
		// neighbours would overlap; keep a 24px target on the 28px pitch.
		checkbox = checkbox.HitBounds(gfx.Rect{X: rect.X - 4, Y: rect.Y - 4, Width: 24, Height: 24})
		if !ok {
			checkbox = checkbox.Disabled()
		}
		checkbox.Label(groups[index/3] + " " + words[index%3]).Paint()
	}
}
