package workbench

import (
	"fmt"
	"os"
	"path"
	"strconv"
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

// The docked files panel is part of the workbench page: its view is declared in
// a container the page names, and its editor replaces that view in the same
// container. The panel's hand-painted parts keep the identities they always had.

// sftpPanelScratch is the room the panel's page leaves for what is painted by
// hand after it has been laid out: the icon buttons, the filter, the path and
// the list. It is reused every paint.
type sftpPanelScratch struct {
	view    *sftpView
	session *Session
	// bounds is the panel's rectangle in the window. The view is declared in a
	// container of the workbench page, whose bounds are the window's, so the
	// sizes the layout needs are kept here.
	bounds gfx.Rect

	// headerTools is where orderToolbarTools builds the header's actions.
	headerTools [12]barTool
	// buttons are the icon buttons, in the order they were declared.
	buttons           [32]sftpIconButton
	buttonCount       int
	headerButtonCount int

	header, crumbs, filterSlot, listHeader, listSlot, spinnerSlot, moreButton ui.Box
	// How the List's rows were declared this paint: the column widths and the
	// width of a row, which the header shares.
	sizeWidth, modifiedWidth float32
	rowWidth                 float32
	// columnArea is where the header's columns lie, and columnWidths the three
	// widths painted there. The divider geometry the pointer reads and the
	// rectangles painted over the header are kept here so a repaint reuses them.
	columnArea     gfx.Rect
	columnWidths   [3]float32
	columnColumns  [3]gfx.Rect
	columnDividers [2]gfx.Rect
	columnHits     [2]gfx.Rect
}

// sftpEditorScratch is the room the editor page leaves for its text area and
// the spinner beside its status line.
type sftpEditorScratch struct {
	view     *sftpView
	textarea ui.Box
	spinner  ui.Box
}

// sftpEmptyScratch is the room the pre-list panel page leaves for its chrome.
type sftpEmptyScratch struct {
	session      *Session
	bounds       gfx.Rect
	canConnect   bool
	header       ui.Box
	closeButton  ui.Box
	sideButton   ui.Box
	sessionTitle ui.Box
	openButton   ui.Box
}

// sftpIconButton is an icon button the page declared, kept for the popup
// anchors painted after the layout.
type sftpIconButton struct {
	box ui.Box
	id  gfx.ID
}

// paintSFTPPanelDetails paints what the panel's view lays out but does not
// paint: the icon buttons, the filter, the path and the list's own parts. It
// runs after the view's children, inside the panel container.
func (state *State) paintSFTPPanelDetails(canvas *gfx.Canvas) {
	s := &state.chrome.sftpPanel
	view, session := s.view, s.session
	if view == nil {
		return
	}
	pal := state.pal
	folder, local := view.active(), view.local

	for index := range s.buttons[:s.buttonCount] {
		if s.buttons[index].id == hitSFTPHeaderMore {
			s.moreButton = s.buttons[index].box
			break
		}
	}
	headerBounds := s.header.Rect()
	canvas.Fill(gfx.Rect{X: headerBounds.X, Y: headerBounds.Y + headerBounds.Height - 1, Width: headerBounds.Width, Height: 1}, pal.line)
	if folder.headerMenuOpen && state.sftpHeaderMenuAvailable(view) {
		state.requestSFTPHeaderMenu(s.moreButton.Rect(), folder, view)
	}
	if folder.filterOpen {
		state.paintSFTPFilter(canvas, s.filterSlot.Rect(), folder)
	}
	if state.sftpPathVisible(folder) {
		state.paintSFTPPathInput(canvas, s.crumbs.Rect(), view)
	} else {
		state.paintSFTPBreadcrumbs(canvas, s.crumbs.Rect(), folder.crumbs)
	}
	state.paintSFTPList(canvas, s, session, view)
	state.paintSFTPPanelColumnDividers(canvas)
	view.places.anchors[sideIndex(local)] = s.crumbs.Rect()
	if view.bookmarksOpen {
		places := sftpPlaceBrowser{owner: state, session: session, view: view}
		places.paintDirectoryPlaces(canvas, state.contextRect)
	}
	if view.busy || local && view.localBusy {
		components.PaintSpinner(canvas, state.Theme(), s.spinnerSlot.Rect(), components.SpinnerPhase(canvas.Now()/(125*time.Millisecond)))
		canvas.RequestFrame()
	}
}

// declareSFTPPanel declares the files panel into its page: a title line with
// icon buttons, the path, the filter, the list and the status line. What is
// painted rather than laid out (the icon buttons, the path, the list) keeps its
// slot in the scratch.
func (state *State) declareSFTPPanel(c ui.C) {
	s := &state.chrome.sftpPanel
	view := s.view
	if view == nil {
		return
	}
	pal, messages := state.pal, state.messages
	folder, local := view.active(), view.local
	session := s.session

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

	headerTools := s.headerTools[:0]
	addHeader := func(id gfx.ID, glyph uint8, hint string, disabled bool, style ...iconButtonStyle) {
		toolStyle := iconButtonQuiet
		if len(style) > 0 {
			toolStyle = style[0]
		}
		headerTools = append(headerTools, barTool{id: id, icon: glyph, hint: hint, disabled: disabled, style: toolStyle})
	}
	if local {
		addHeader(hitSFTPNewDirectory, iconFolderPlus, messages.SFTPNewDirectory, view.localBusy)
	} else {
		addHeader(hitSFTPUpload, iconUpload, messages.SFTPUpload, view.busy)
		addHeader(hitSFTPUploadDirectory, iconFolderUp, messages.SFTPUploadDirectory, view.busy)
		addHeader(hitSFTPNewDirectory, iconFolderPlus, messages.SFTPNewDirectory, view.busy)
		addHeader(hitSFTPNewLink, iconLink, messages.Tools.TipNewLink, view.busy)
	}
	if !view.localOnly {
		addHeader(hitSFTPPaneToggle, paneIcon, paneHint, false)
	}
	addHeader(hitSFTPRefresh, iconRefresh, messages.SFTPRefresh, view.busy && !local || local && view.localBusy)
	followStyle := iconButtonQuiet
	if !state.sftpFollowOff {
		followStyle = iconButtonActive
	}
	addHeader(hitSFTPFollowToggle, iconFollowFolder, messages.Tools.FilesFollow, false, followStyle)
	moveIcon, moveHint := state.sftpPanelMoveAction()
	headerTools = state.orderToolbarTools(toolbarSFTP, headerTools, false)

	// icon declares an icon button where it goes; it is painted after Draw.
	icon := func(side float32, id gfx.ID, glyph uint8, hint string, disabled bool, style iconButtonStyle) {
		s.buttons[s.buttonCount] = sftpIconButton{box: state.declareIconButton(c, id, glyph, hint, disabled, style).Size(side, side), id: id}
		s.buttonCount++
	}
	ui.Column(c).Fill().PadSides(0, sftpPad, 6, sftpPad).Background(pal.workspace).Children(func() {
		// Keep the panel's title, docking and close buttons on their own row, with the
		// folder actions below so they do not compete with the title for space.
		s.header = ui.Row(c).FillWidth().Height(infoBarHeight).Shrink(0).MarginSides(0, -sftpPad, 0, -sftpPad).PadX(sftpPad).Background(pal.workspace).Items(ui.Center)
		s.header.Children(func() {
			ui.Text(c, title).FontSize(metrics.FontSizeM).Weight(gfx.WeightSemibold).Color(pal.text).Ellipsis().Grow(1).Height(20).MarginSides(0, 8, 0, 0)
			ui.Row(c).Gap(4).Shrink(0).Children(func() {
				if state.toolbarVisible(toolbarSFTP, hitSFTPPanelSide) {
					icon(sftpHeaderButton, hitSFTPPanelSide, moveIcon, moveHint, false, iconButtonQuiet)
				}
				if state.toolbarVisible(toolbarSFTP, hitSFTPClose) {
					icon(sftpHeaderButton, hitSFTPClose, iconClose, messages.TipSFTPClose, false, iconButtonQuiet)
				}
			})
		})
		ui.Row(c).FillWidth().Height(sftpButton).Shrink(0).Gap(4).Items(ui.Center).MarginSides(6, 0, 0, 0).Children(func() {
			for _, tool := range headerTools {
				icon(sftpHeaderButton, tool.id, tool.icon, tool.hint, tool.disabled, tool.style)
			}
			// Once hidden entries are enabled, the Eye control remains visible
			// as their status affordance beside the secondary actions menu.
			if folder.showHidden {
				icon(sftpHeaderButton, hitSFTPHiddenToggle, iconEye, messages.Tools.FilesHideHidden, false, iconButtonActive)
			}
			ui.Spacer(c)
			icon(sftpHeaderButton, hitSFTPHeaderMore, iconEllipsis, messages.Tools.FilesMore, false, iconButtonQuiet)
		})
		s.headerButtonCount = s.buttonCount

		// Where we are: one level up, then the tail of the path as many folders as
		// fit. The bookmark controls close the row on the right: the list of the
		// folders kept, and a star for this one.
		ui.Row(c).Height(sftpButton).Shrink(0).Gap(4).Items(ui.Center).MarginSides(6, 0, 0, 0).Children(func() {
			icon(sftpButton, hitSFTPUp, iconArrowUp, messages.SFTPUp, !local && view.busy || folder.path == "/" || folder.path == "", iconButtonQuiet)
			height := float32(24)
			if state.sftpPathVisible(folder) {
				height = sftpButton
			}
			s.crumbs = ui.Slot(c).Grow(1).Height(height)
			listID, starID, hint := hitSFTPBookmarkList, hitSFTPBookmarkToggle, messages.SFTPWindow.DirectoryFavorites
			if local {
				listID, starID, hint = hitSFTPLocalPlaces, hitSFTPLocalStar, messages.SFTPWindow.LocalPlaces
			}
			ui.Row(c).Gap(2).Shrink(0).Children(func() {
				icon(sftpButton, listID, iconChevronDown, hint, false, iconButtonQuiet)
				icon(sftpButton, starID, starIcon, starHint, folder.path == "" || !local && (view.busy || !places.connected() || session.host.CatalogID == ""), iconButtonQuiet)
			})
		})
		if folder.filterOpen {
			// The filter, and the switch that takes it into the subfolders.
			ui.Row(c).Height(32).Shrink(0).Gap(4).Items(ui.Center).MarginSides(5, 0, 4, 0).Children(func() {
				s.filterSlot = ui.Slot(c).Grow(1).Height(32)
				if sftpSearchAvailable(view, local) {
					style := iconButtonQuiet
					if folder.deep {
						style = iconButtonActive
					}
					icon(sftpButton, hitSFTPSearchToggle, iconFolderOpen, messages.Tools.FilesSearchSubfolders, false, style)
				}
			})
		}
		ui.Divider(c).Background(pal.line).MarginSides(2, 0, 3, 0)

		// The rows are one Viva List, under a header that sorts them by a
		// column. The List takes what the header leaves.
		searching, rows := folder.searching(), folder.shownRows()
		width := max(s.bounds.Width-2*sftpPad, 0)
		visible := 0
		if folder.listHeight > 0 {
			visible = int(folder.listHeight / sftpRowHeight)
		}
		scrolls := len(rows) > visible && visible > 0
		rowWidth := width
		if scrolls {
			rowWidth = max(rowWidth-10, 0)
		}
		s.rowWidth = rowWidth
		resizable := state.sftpColumnResizeAvailable()
		var columns sftpColumnMetrics
		if resizable {
			columnAreaWidth := max(rowWidth-16, 0)
			columns = state.pendingSFTPColumnMetrics(view, folder, c.Canvas(), columnAreaWidth, 0)
			s.sizeWidth, s.modifiedWidth = columns.size, columns.modified
			s.columnWidths = [3]float32{columns.name, columns.size, columns.modified}
		} else {
			s.sizeWidth, s.modifiedWidth = sftpColumnWidths(rowWidth)
		}
		if !searching {
			s.listHeader = ui.Row(c).Width(rowWidth).Height(sftpListHeaderHeight).Shrink(0).PadX(8).Items(ui.Center).Children(func() {
				if resizable {
					state.declareSFTPColumnHeader(c, folder, 0, state.messages.SFTPWindow.Name, columns.name, false)
				} else {
					state.declareSFTPColumnHeader(c, folder, 0, state.messages.SFTPWindow.Name, 0, true)
				}
				for _, entry := range state.sftpColumns.entries[1:] {
					if !entry.Enabled {
						continue
					}
					label, columnWidth := state.messages.SFTPWindow.Size, s.sizeWidth
					if entry.Key == sftpColumnModified {
						label, columnWidth = state.messages.SFTPWindow.Modified, s.modifiedWidth
					}
					state.declareSFTPColumnHeader(c, folder, int(entry.Key), label, columnWidth, false)
				}
			})
		}
		folder.prepareSFTPList()
		folder.seedSFTPListChoice()
		if folder.listPendingFirst >= 0 {
			folder.list.ScrollTo(folder.listPendingFirst, ui.ListStart)
			folder.listPendingFirst = -1
		}
		s.listSlot = ui.List(c, &folder.list, len(rows), func(row ui.ListRow) {
			state.declareSFTPListRow(row, folder, view, searching, s.sizeWidth, s.modifiedWidth, rowWidth, columns)
		}).Grow(1).HitWith(hitSFTPListBackground, messages.SFTPFiles, gfx.HitOptions{Role: gfx.RoleGroup, SkipFocus: true})
		folder.seedSFTPListCurrent()
		if folder.list.Changed() {
			folder.takeSFTPListChoice()
			state.afterSFTPListChoice(view, folder)
		}

		{
			// Reserve the same space for empty, single and multiple selections.
			ui.Column(c).Shrink(0).Children(func() {
				ui.Divider(c).Background(pal.line)
				ui.Row(c).Gap(2).Height(sftpButton).Shrink(0).MarginSides(5, 0, 0, 0).Children(func() {
					if selectedCount == 0 {
						return
					}
					opBusy := view.busy && !local || local && view.localBusy
					add := func(id gfx.ID, glyph uint8, label string, danger bool) {
						style := iconButtonQuiet
						if danger {
							style = iconButtonDanger
						}
						icon(sftpButton, id, glyph, label, opBusy, style)
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
						add(hitSFTPChmod, iconLock, messages.SFTPPermissions, false)
						if sftpOwnershipAvailable(session, view) {
							add(hitSFTPChown, iconWrench, messages.SFTPOwnership, false)
						}
						if _, archive := state.selectedArchive(session, view); archive {
							add(hitSFTPExtract, iconArchive, messages.Tools.Extract, false)
						}
					}
					if !local {
						add(hitSFTPMove, iconFolderOpen, messages.SFTPMove, false)
					}
					add(hitSFTPDelete, iconTrash, messages.SFTPDelete, true)
				})
				details := ""
				if one {
					details = state.sftpDetailsLine(view, selected)
				}
				ui.Text(c, details).FontSize(metrics.FontSizeS).Color(pal.muted).Ellipsis().Height(18).Shrink(0).MarginSides(2, 0, 2, 2)
			})
		}

		if local {
			state.declareLocalCoverage(c, folder, hitSFTPCoverage)
		}
		// What is happening.
		statusRow := ui.Row(c).Shrink(0).Gap(8).Items(ui.Center).MarginSides(6, 0, 0, 0)
		if folder.searching() {
			// What a search read, how much of it, and what it could not read are
			// too long for one line of a narrow panel, and the last of them is
			// the one not to lose.
			statusRow = statusRow.MinHeight(28)
		} else {
			statusRow = statusRow.Height(28)
		}
		statusRow.Children(func() {
			if view.busy || local && view.localBusy {
				s.spinnerSlot = ui.Slot(c).Size(16, 16)
			}
			if folder.searching() {
				ui.Paragraph(c, status, 3).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(statusColor).Grow(1).MarginSides(4, 0, 4, 0)
			} else {
				ui.Text(c, status).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(statusColor).Ellipsis().Grow(1)
			}
			if view.busy {
				cancel := ui.Button(c, messages.SFTPCancelTransfer).Small().Outline()
				if cancel.Clicked() {
					state.cancelSFTPOperation(view)
				}
			}
		})
	})
}

// paintSFTPList declares the visible rows of the list into the slot the page
// left for it, then paints what is drawn rather than laid out: the place
// dropped files would go and the scroll bar. Which rows are visible is
// arithmetic; where each goes and what is inside it is flex.
// paintSFTPList paints what the List does not: the rule under the column
// header, the scroll bar, the empty or loading message and the place dropped
// files would go. The rows themselves are declared as elements by
// declareSFTPListRow, and the header by declareSFTPColumnHeader.
// paintSFTPPanelColumnDividers draws the drag handles over the docked list's
// header and keeps the geometry the pointer reads.
func (state *State) paintSFTPPanelColumnDividers(canvas *gfx.Canvas) {
	if !state.sftpColumnResizeAvailable() {
		return
	}
	s := &state.chrome.sftpPanel
	if s.view == nil || !s.listHeader.Valid() {
		return
	}
	header := s.listHeader.Rect()
	if header.Width <= 16 || header.Height <= 0 {
		return
	}
	s.columnArea = gfx.Rect{X: header.X + 8, Y: header.Y, Width: header.Width - 16, Height: header.Height}
	ids := [2]gfx.ID{hitSFTPPanelColumnDivider.At(0), hitSFTPPanelColumnDivider.At(1)}
	paintSFTPColumnResize(canvas, state.Theme(), s.columnArea, s.columnWidths, 0, ids,
		state.messages.Tools.FilesResizeColumn, s.columnColumns[:], s.columnDividers[:], s.columnHits[:])
}

func (state *State) paintSFTPList(canvas *gfx.Canvas, s *sftpPanelScratch, session *Session, view *sftpView) {
	pal := state.pal
	folder, local := view.active(), view.local
	searching, rows := folder.searching(), folder.shownRows()
	body := s.listSlot.Rect()
	folder.listHeight = body.Height
	folder.visibleRows = max(0, int(body.Height/sftpRowHeight))
	if scroll := s.listSlot.ScrollState(); scroll != nil {
		folder.listScroll = scroll
		folder.scrollOffset = scroll.Offset
		folder.first = max(0, int(scroll.Offset/sftpRowHeight))
	}

	if !searching {
		if header := s.listHeader.Rect(); header.Height > 0 {
			canvas.Fill(gfx.Rect{X: header.X, Y: header.Y + header.Height - 1, Width: header.Width, Height: 1}, withAlpha(pal.muted, 0.18))
		}
	}

	// The List owns the offset; the bar is drawn from it with the same geometry
	// the rows were laid out under.
	area := components.BeginScrollArea(canvas, state.Theme(), body, gfx.Size{Width: body.Width, Height: float32(len(rows)) * sftpRowHeight}).
		Offset(0, folder.scrollOffset).
		VerticalScrollbar(hitSFTPListScrollbar, state.messages.SFTPFiles, "")
	area.BeginContent()
	folder.scroll = area.EndContent()

	empty := len(rows) == 0 && (!view.busy || local) && view.errorText == ""
	loading := len(rows) == 0 && view.busy && !local && view.operation == sftpclient.OperationList && view.errorText == ""
	reading := searching && len(rows) == 0 && folder.search.running && folder.search.failure == ""
	if reading {
		empty, loading = false, false
	}
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
		target := body
		if row := view.dropRow; row >= 0 && row < len(rows) {
			target = gfx.Rect{
				X: body.X, Y: body.Y + float32(row)*sftpRowHeight - folder.scrollOffset,
				Width: s.rowWidth, Height: sftpRowHeight,
			}
		}
		state.paintSFTPDropOverlay(canvas, target, state.messages.Tools.DropUploadHint)
	}
}

// afterSFTPListChoice reports a choice the List delivered: the count, and the
// folder the first click of a double click would open.
func (state *State) afterSFTPListChoice(view *sftpView, folder *sftpFolder) {
	view.setItemCountStatus(state.messages.SFTPItemsFmt, selectedSFTPCount(view))
	if folder.searching() || view.local || view.busy || selectedSFTPCount(view) != 1 {
		return
	}
	for index := range folder.rows {
		if row := folder.rows[index]; row.selected && row.entry.IsDir() {
			// The first click of a double-click: start reading the folder now,
			// so the second finds the work done or under way.
			state.prefetchSFTP(view, path.Join(view.path, row.entry.Name))
			return
		}
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

// declareSFTPColumnHeader declares one column of the list's header: its label,
// which sorts the list by that column, and the arrow when it does.
func (state *State) declareSFTPColumnHeader(c ui.C, folder *sftpFolder, column int, label string, width float32, grow bool) {
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
	box := ui.Row(c).Height(sftpListHeaderHeight).Shrink(0)
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
		textBox := ui.Text(c, text).FontSize(metrics.FontSizeS).Color(color).LineHeight(sftpListHeaderHeight).Ellipsis()
		if grow {
			textBox = textBox.Grow(1)
		} else {
			// The name starts at the list's left edge, like the rows under it
			// and the detached window's header; the numeric columns end at
			// their right edge.
			textBox = textBox.Width(width - 4)
			if column > 0 {
				textBox = textBox.TextEnd()
			}
		}
	})
}

// declareSFTPListRow declares one entry as a List row: its type icon, its name,
// which takes the room and ends with an ellipsis, and, when there is room, its
// modification time. A selected row is tinted; the pointer lightens one. A
// search result has its folder after the name, where the columns would be.
func (state *State) declareSFTPListRow(row ui.ListRow, folder *sftpFolder, view *sftpView, result bool, sizeWidth, modifiedWidth, rowWidth float32, columns sftpColumnMetrics) {
	rows := folder.shownRows()
	if row.Index < 0 || row.Index >= len(rows) {
		return
	}
	if folder.listRowsScope != row.ID.Scope() {
		folder.listRowsScope = row.ID.Scope()
	}
	entry := &rows[row.Index]
	c, canvas := row.C, row.C.Canvas()
	pal := state.pal
	name := entry.cells[1]
	if entry.entry.IsLink() && entry.entry.LinkTarget != "" {
		name = entry.cells[1] + " → " + entry.entry.LinkTarget
	}
	nameColor := pal.text
	if entry.entry.LinkBroken {
		nameColor = pal.danger
	}
	tint := gfx.Color{Alpha: 1} // a colored glyph keeps its own colors
	if entry.mono {
		tint = pal.muted
		if entry.entry.Mode&os.ModeSymlink != 0 {
			tint = pal.accent
		}
	}
	var background gfx.Color
	switch {
	case row.Selected():
		background = pal.accentSoft
	case canvas.Hovered() == row.ID:
		background = withAlpha(pal.surfaceHot, 0.7)
	}
	focused := row.Focused()
	contents := func() {
		declareSFTPRowIcon(c, entry, tint)
		label := ui.Row(c).Grow(1).Clip().Items(ui.Center).MarginSides(0, 0, 0, 8)
		label.Children(func() {
			if result {
				state.declareSFTPResultLabel(c, entry, nameColor, rowWidth)
				return
			}
			if entry.matchEnd > entry.matchStart && entry.matchEnd <= len(entry.entry.Name) {
				if prefix := entry.entry.Name[:entry.matchStart]; prefix != "" {
					ui.Text(c, prefix).FontSize(metrics.FontSizeM).Color(nameColor).Shrink(0)
				}
				state.declareSFTPMatch(c, entry.entry.Name[entry.matchStart:entry.matchEnd])
				suffix := entry.entry.Name[entry.matchEnd:]
				if entry.entry.IsLink() && entry.entry.LinkTarget != "" {
					suffix += " → " + entry.entry.LinkTarget
				}
				ui.Text(c, suffix).FontSize(metrics.FontSizeM).Color(nameColor).Ellipsis().Grow(1)
				return
			}
			nameText := ui.Text(c, name).FontSize(metrics.FontSizeM).Color(nameColor).Ellipsis().Grow(1)
			available := rowWidth - 16 - sftpColumnIconWidth - sftpColumnNameGap
			if state.sftpColumnResizeAvailable() {
				available = columns.name - sftpColumnIconWidth - sftpColumnNameGap
			} else {
				for _, column := range state.sftpColumns.entries[1:] {
					if !column.Enabled {
						continue
					}
					available -= sizeWidth + sftpColumnNameGap
					if column.Key == sftpColumnModified {
						available += sizeWidth - modifiedWidth
					}
				}
			}
			if available > 0 && canvas.TextWidth(name, metrics.FontSizeM) > available {
				nameText.HitWith(row.ID, name, gfx.HitOptions{Hint: name, SkipFocus: true})
			}
		})
		for _, column := range state.sftpColumns.entries[1:] {
			if !column.Enabled || result {
				continue
			}
			if column.Key == sftpColumnSize {
				ui.Text(c, entry.cells[2]).FontSize(metrics.FontSizeS).Color(pal.muted).Width(sizeWidth).TextEnd().Shrink(0)
				continue
			}
			if column.Key == sftpColumnModified {
				date := entry.cells[3]
				if modifiedWidth < 100 && len(date) > 5 {
					date = date[5:] // drop the year
				}
				ui.Text(c, date).FontSize(metrics.FontSizeS).Color(pal.muted).Width(modifiedWidth).TextEnd().Shrink(0)
			}
		}
	}
	box := row.Element.Height(sftpRowHeight).Shrink(0).Width(rowWidth).Items(ui.Center)
	if background.Alpha == 0 && !focused {
		// Most rows have nothing behind them: their parts go straight in the row.
		box = box.PadX(8)
		box.Children(contents)
		return
	}
	// A tinted or focused row is a rounded shape a pixel in from the top and
	// bottom of the row, so the parts go in a box of that shape.
	box = box.PadY(1)
	box.Children(func() {
		inner := ui.Row(c).Fill().Items(ui.Center).PadX(8).Background(background).Radius(radius.control)
		if focused {
			inner.Border(1.5, pal.accent)
		}
		inner.Children(contents)
	})
}

// declareSFTPMatch declares the part of a name the query matched: heavier than
// the rest, and on a tint of the accent that shows in light and dark alike.
func (state *State) declareSFTPMatch(c ui.C, match string) {
	pal := state.pal
	ui.Text(c, match).FontSize(metrics.FontSizeM).Weight(gfx.WeightSemibold).Color(pal.text).Background(withAlpha(pal.accent, 0.3)).Radius(radius.control).Shrink(0)
}

// declareSFTPResultLabel declares what a search result says: its name with the
// match marked, then its folder, which gives way when room is short. A folder
// too long for the room loses its middle, as the first and the last folders say
// the most about where a file is.
func (state *State) declareSFTPResultLabel(c ui.C, row *sftpRow, nameColor gfx.Color, rowWidth float32) {
	pal, canvas := state.pal, c.Canvas()
	name := row.entry.Name
	start, end := row.matchStart, row.matchEnd
	if end <= start || end > len(name) {
		start, end = 0, 0
	}
	if prefix := name[:start]; prefix != "" {
		ui.Text(c, prefix).FontSize(metrics.FontSizeM).Color(nameColor).Shrink(0)
	}
	if end > start {
		state.declareSFTPMatch(c, name[start:end])
	}
	if suffix := name[end:]; suffix != "" {
		ui.Text(c, suffix).FontSize(metrics.FontSizeM).Color(nameColor).Shrink(0)
	}
	if row.where == "" {
		return
	}
	// Left for the folder: the row less its padding, the icon, the gaps and the
	// name, which is a little wider in its heavy match.
	room := rowWidth - 16 - 30 - 16 - canvas.TextWidth(name, metrics.FontSizeM) - 6
	where := elideSFTPFolder(row.where, func(text string) bool { return canvas.TextWidth(text, metrics.FontSizeS) <= room })
	ui.Text(c, where).FontSize(metrics.FontSizeS).Color(pal.muted).Ellipsis().Grow(1).MarginSides(0, 0, 0, 8)
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

// paintSFTPEditorDetails paints the editor's text area and spinner after its
// view has been laid out in the panel container.
func (state *State) paintSFTPEditorDetails(canvas *gfx.Canvas) {
	editor := &state.chrome.sftpEditor
	view := editor.view
	if view == nil {
		return
	}

	value := view.editEditor.String()
	focused := canvas.Focused() == hitSFTPEditor
	selection := view.editEditor.Selection()
	snapshot := components.TextareaSnapshot{
		ID: hitSFTPEditor, Label: state.messages.SFTPEditLabel, Value: value,
		Focused: focused, HideEditorText: true, ShowCaret: selection.Start == selection.End, ReadOnly: view.busy,
		Layout:         &view.editLayout,
		SelectionStart: aiByteOffset(value, selection.Start), SelectionEnd: aiByteOffset(value, selection.End),
		Caret: aiByteOffset(value, view.editEditor.Caret()),
	}
	if marked, composing := view.editEditor.Composition(); composing {
		snapshot.CompositionStart, snapshot.CompositionEnd = aiByteOffset(value, marked.Start), aiByteOffset(value, marked.End)
	}
	view.editRect = editor.textarea.Rect()
	components.PaintTextarea(canvas, state.Theme(), view.editRect, snapshot)
	if view.busy {
		components.PaintSpinner(canvas, state.Theme(), editor.spinner.Rect(), components.SpinnerPhase(canvas.Now()/(125*time.Millisecond)))
		canvas.RequestFrame()
	}
}

// declareSFTPEditor declares the editor page. It replaces the file list while a
// remote file is open for editing.
func (state *State) declareSFTPEditor(c ui.C) {
	editor := &state.chrome.sftpEditor
	view := editor.view
	if view == nil {
		return
	}
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

	ui.Column(c).Fill().PadSides(14, 18, 10, 18).Background(pal.workspace).Children(func() {
		// Back on the left, the file's name and place beside it, the actions from
		// the right.
		ui.Row(c).Height(36).Shrink(0).Children(func() {
			back := ui.Button(c, messages.SFTPEditBack).Small().Outline().Disabled(view.busy)
			if back.Clicked() && !view.busy {
				state.closeSFTPEditor(view)
			}
			ui.Spacer(c)
			ui.Row(c).Gap(8).Shrink(0).Children(func() {
				reload := ui.Button(c, messages.SFTPEditReload).Small().Secondary().Disabled(view.busy)
				if reload.Clicked() && !view.busy {
					state.reloadSFTPEditor(view)
				}
				save := ui.Button(c, messages.SFTPEditSave).Small().Disabled(view.busy || !dirty)
				if save.Clicked() && !view.busy {
					state.saveSFTPEditor(view)
				}
			})
		})
		// Under the actions, whole: beside them the name of a file had no room in a
		// panel this narrow.
		ui.Column(c).Shrink(0).MarginSides(8, 0, 0, 0).Children(func() {
			ui.Text(c, view.editName).FontSize(18).Weight(gfx.WeightSemibold).Color(pal.text).Ellipsis().LineHeight(26)
			ui.Text(c, view.editLocation).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.muted).Ellipsis().LineHeight(20)
		})
		ui.Text(c, messages.SFTPEditSafety).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.muted).LineHeight(22).MarginSides(10, 0, 6, 0)
		editor.textarea = ui.Slot(c).Grow(1).MinHeight(64)
		ui.Row(c).Height(28).Shrink(0).Gap(8).Items(ui.Center).MarginSides(14, 0, 0, 0).Children(func() {
			if view.busy {
				editor.spinner = ui.Slot(c).Size(16, 16)
			}
			ui.Text(c, status).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(statusColor).Ellipsis().Grow(1)
			if view.busy {
				// With the file list's own place for it: in the header it did not
				// fit beside the other three in a panel this narrow.
				cancel := ui.Button(c, messages.SFTPCancelTransfer).Small().Secondary()
				if cancel.Clicked() {
					state.cancelSFTPOperation(view)
				}
			}
		})
	})
}

// declareSFTPDialog declares the open file dialog in the page that hosts the
// window: the standalone window's page, or the workbench page's file tab. Its
// body and footer are page elements; the fields are painted into the slots it
// leaves.
func (state *State) declareSFTPDialog(c ui.C, viewport gfx.Rect, view *sftpView) {
	messages := state.messages
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
	case sftpDialogChown:
		title = messages.SFTPOwnershipTitle
		description = messages.SFTPOwnershipDescription
		fieldLabel = messages.SFTPPermissionsOwner
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
	fieldDialog := sftpDialogShowsField(view.dialog)
	showError := view.dialog == sftpDialogDelete && view.dialogError != ""
	body := float32(0) // a confirmation is only its text
	switch {
	case view.dialog == sftpDialogSymlink, view.dialog == sftpDialogChown:
		body = 6 + 64 + 8 + 64 // two fields: the link's name and target, or the owner and group
	case view.dialog == sftpDialogChmod:
		// The octal field, then the permission grid: a 16px header row and
		// three 24px rows, with a 4px gap between them.
		body = 6 + 64 + 8 + 16 + 3*24 + 3*4
	case fieldDialog:
		body = 6 + 64
	case showError:
		body = 20
	}
	view.dialogFieldLabel = fieldLabel
	view.dialogCancelLabel, view.dialogSubmitLabel, view.dialogSubmitVariant = cancelLabel, submitLabel, submitVariant
	view.dialogViewport = viewport
	// No scrim dismiss: a press beside the form must not discard its input.
	height := dialogContentHeight(c.Canvas(), *c.Theme(), viewport, 520, description, body)
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: title, Description: description, Width: 520, Height: height, Layer: dialogLayer,
		Scope: dialogScope, CloseID: hitSFTPDialogClose, InitialFocusID: initialFocus,
	})
	dialog.Body(func() { state.declareSFTPDialogBody(c, view) })
	dialog.Footer(func() { state.declareSFTPDialogFooter(c, view) })
}

// declareSFTPDialogFooter declares the dialog's buttons in its footer. They keep
// the application's identities, and the view keeps the draw so the footer is
// the same when an application modal stands beside the dialog.
func (state *State) declareSFTPDialogFooter(c ui.C, view *sftpView) {
	if view.dialogFooterDraw == nil {
		view.dialogFooterDraw = func(d *ui.DrawContext) {
			actions := [...]components.ButtonAction{
				{ID: hitSFTPDialogCancel, Label: view.dialogCancelLabel, Variant: components.ButtonSecondary},
				{ID: hitSFTPDialogSubmit, Label: view.dialogSubmitLabel, Variant: view.dialogSubmitVariant},
			}
			components.BeginButtonRow(d.Canvas, *d.Theme, d.Rect).Size(components.ButtonSizeDefault).Paint(actions[:]...)
		}
	}
	ui.Slot(c).Fill().DrawOver(view.dialogFooterDraw)
}

// paintSFTPDialogDetails paints the open dialog's fields into the slots the
// page laid out: one labelled input, two for a link, or the permission grid
// under a chmod field.
func (state *State) paintSFTPDialogDetails(canvas *gfx.Canvas, view *sftpView) {
	if !sftpDialogShowsField(view.dialog) {
		return
	}
	if view.dialog == sftpDialogChown {
		state.paintSFTPOwnership(canvas, view)
		return
	}
	messages, theme := state.messages, state.Theme()
	fieldLabel := view.dialogFieldLabel
	field := components.BeginField(canvas, theme, view.dialogFirst.Rect())
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
		state.paintSFTPChmodGrid(canvas, view, &view.dialogChmod)
	}
	if view.dialog == sftpDialogSymlink {
		second := components.BeginField(canvas, theme, view.dialogSecond.Rect()).Paint(messages.Tools.LinkTarget, "", "")
		secondValue := view.dialogEditor2.String()
		secondFocused := canvas.Focused() == hitSFTPDialogField2
		components.PaintInput(canvas, theme, second, components.InputSnapshot{
			ID: hitSFTPDialogField2, Label: messages.Tools.LinkTarget, Value: secondValue,
			Focused: secondFocused, PlatformEditing: secondFocused && secondValue != "",
		})
	}
}

// paintSFTPOwnership paints the ownership dialog's two editable suggestion
// fields. Each opens a filtered list of the accounts the server's files name;
// the text stays free, so an account those files do not list can still be
// typed as a name or a numeric id.
func (state *State) paintSFTPOwnership(canvas *gfx.Canvas, view *sftpView) {
	messages, theme := state.messages, state.Theme()
	labels := [2]string{messages.SFTPPermissionsOwner, messages.SFTPPermissionsGroup}
	boxes := [2]ui.Box{view.dialogFirst, view.dialogSecond}
	for field := range 2 {
		id := hitSFTPDialogField
		if field == 1 {
			id = hitSFTPDialogField2
		}
		editor := view.ownershipEditor(field)
		// A typed change opens the suggestions. The query is kept because the
		// docked window edits the field through EditorFor, which has no event
		// callback; the field comparison is what a text event would tell us.
		if canvas.Focused() == id && editor.String() != view.ownership.lastQuery[field] {
			view.ownership.open[field] = true
			view.ownership.highlighted[field] = 0
			view.ownership.lastQuery[field] = editor.String()
		}
		group := components.BeginField(canvas, theme, boxes[field].Rect())
		if view.dialogError != "" {
			group = group.Invalid()
		}
		errorText := ""
		if field == 0 {
			errorText = view.dialogError
		}
		input := group.Paint(labels[field], "", errorText)
		open := view.ownership.open[field]
		query := editor.String()
		options := components.ComboboxOptions{
			InputID: id, Label: labels[field], Value: query, Query: query,
			Placeholder: labels[field], Open: open, Invalid: view.dialogError != "",
			ShowTrigger: true, Layer: dialogLayer + 1, Scope: dialogScope,
			DismissID: hitSFTPChownDismiss, Viewport: view.dialogViewport,
		}
		var matches []sftpAccountOption
		if open {
			matches = view.ownership.matches(field, query)
			options.Content = sftpOwnershipPopupRect(input, len(matches), view.dialogViewport)
			options.MatchInputWidth = true
		}
		combo := components.BeginCombobox(canvas, theme, input, options)
		if open {
			if len(matches) == 0 {
				combo.AddEmpty(messages.SFTPOwnershipNoMatches)
			}
			for index, account := range matches {
				combo.AddItem(components.ComboboxItem{
					ID:          ownershipItemScope(field).At(index),
					Label:       account.name,
					Detail:      strconv.FormatUint(uint64(account.id), 10),
					Selected:    query == account.name,
					Highlighted: index == view.ownership.highlighted[field],
				})
			}
		}
		combo.End()
	}
}

// sftpOwnershipPopupRect places the suggestion list under its field, or above
// it when the bottom of the window is closer.
func sftpOwnershipPopupRect(input gfx.Rect, rows int, viewport gfx.Rect) gfx.Rect {
	height := float32(8 + 36) // the empty result
	if rows > 0 {
		height = float32(8 + rows*32)
	}
	x := input.X
	if right := viewport.X + viewport.Width; x+input.Width > right {
		x = right - input.Width
	}
	if x < viewport.X {
		x = viewport.X
	}
	y := input.Y + input.Height + 8
	if bottom := viewport.Y + viewport.Height - 8; y+height > bottom {
		y = input.Y - 8 - height
	}
	if y < viewport.Y+8 {
		y = viewport.Y + 8
	}
	return gfx.Rect{X: x, Y: y, Width: input.Width, Height: height}
}

// declareSFTPDialogBody lays out the dialog's body: one labelled input, two for
// a link, the permission grid under a chmod field, or the error of a failed
// move when that is what the dialog says. The parts painted by hand are kept in
// view so the painter can fill the boxes the page laid out.
func (state *State) declareSFTPDialogBody(c ui.C, view *sftpView) {
	showError := view.dialog == sftpDialogDelete && view.dialogError != ""
	view.dialogFirst, view.dialogSecond = ui.Box{}, ui.Box{}
	view.dialogChmod = [9]ui.Box{}
	if view.dialogDraw == nil {
		view.dialogDraw = func(d *ui.DrawContext) { state.paintSFTPDialogDetails(d.Canvas, view) }
	}
	column := ui.Column(c).Fill()
	if showError {
		column.Justify(ui.Center) // as a label in the whole body
	}
	column.Children(func() {
		switch {
		case sftpDialogShowsField(view.dialog):
			ui.Column(c).Shrink(0).Gap(8).PadSides(6, 4, 0, 4).Children(func() {
				view.dialogFirst = ui.Slot(c).Height(64).Shrink(0)
				if view.dialog == sftpDialogSymlink || view.dialog == sftpDialogChown {
					view.dialogSecond = ui.Slot(c).Height(64).Shrink(0)
				}
				if view.dialog == sftpDialogChmod {
					view.dialogChmod = state.declareSFTPChmodGrid(c)
				}
			})
		case showError:
			ui.Text(c, view.dialogError).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(state.pal.danger).Ellipsis()
		}
	})
	column.DrawOver(view.dialogDraw)
}

// declareSFTPChmodGrid lays out the three permission rows (owner, group,
// others × read, write, execute) under the octal field and captures the nine
// checkbox slots; paintSFTPChmodGrid fills them in.
func (state *State) declareSFTPChmodGrid(c ui.C) [9]ui.Box {
	m := state.messages
	groups := [3]string{m.SFTPPermissionsOwner, m.SFTPPermissionsGroup, m.SFTPPermissionsOthers}
	letters := [3]string{"R", "W", "X"}
	var boxes [9]ui.Box
	grid := ui.Column(c).Shrink(0).Gap(4)
	grid.Children(func() {
		ui.Row(c).Height(16).Shrink(0).Gap(8).Items(ui.Center).Children(func() {
			ui.Slot(c).Width(72).Shrink(0)
			for _, letter := range letters {
				ui.Text(c, letter).FontSize(metrics.FontSizeS).Color(state.pal.muted).Width(20).TextCenter().Shrink(0)
			}
			ui.Spacer(c).Grow(1)
		})
		for group := range groups {
			ui.Row(c).Height(24).Shrink(0).Gap(8).Items(ui.Center).Children(func() {
				ui.Text(c, groups[group]).FontSize(metrics.FontSizeS).Color(state.pal.text).Width(72).Shrink(0)
				for perms := range letters {
					boxes[group*3+perms] = ui.Slot(c).Size(20, 20).Shrink(0)
				}
				ui.Spacer(c).Grow(1)
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
