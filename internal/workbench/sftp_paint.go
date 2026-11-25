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
	headerTools   [12]barTool
	overflowTools []barTool
	// overflow is the storage overflowTools reads. It is a copy because the eye
	// appended to the visible roster would otherwise overwrite the first action
	// that the same backing array made overflow.
	overflow [maxBarTools]barTool
	// buttons are the icon buttons, in the order they were declared.
	buttons           [32]sftpIconButton
	buttonCount       int
	headerButtonCount int

	header, crumbs, listHeader, listSlot, moreButton ui.Box
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
	local := view.local

	headerBounds := s.header.Rect()
	canvas.Fill(gfx.Rect{X: headerBounds.X, Y: headerBounds.Y + headerBounds.Height - 1, Width: headerBounds.Width, Height: 1}, pal.line)
	state.paintSFTPList(canvas, s, session, view)
	state.paintSFTPPanelColumnDividers(canvas)
	view.places.anchors[sideIndex(local)] = s.crumbs.Rect()
	state.paintSFTPPanelDrag(canvas, s, view)
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
	view.reconcileSFTPRename(c.Canvas(), !local && state.modal == modalNone)

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
		addHeader(hitSFTPNewFile, iconSFTPNewFile, messages.SFTPNewFile, view.localBusy)
		addHeader(hitSFTPNewDirectory, iconSFTPNewDirectory, messages.SFTPNewDirectory, view.localBusy)
	} else {
		addHeader(hitSFTPUpload, iconSFTPUpload, messages.SFTPUpload, view.busy)
		addHeader(hitSFTPUploadDirectory, iconSFTPUploadDirectory, messages.SFTPUploadDirectory, view.busy)
		addHeader(hitSFTPNewFile, iconSFTPNewFile, messages.SFTPNewFile, view.busy)
		addHeader(hitSFTPNewDirectory, iconSFTPNewDirectory, messages.SFTPNewDirectory, view.busy)
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
	if !local && !view.localOnly {
		addHeader(hitSFTPLocateTerminal, iconTerminal, messages.Tools.FilesLocateTerminal, !state.canLocateSFTPTerminal(view))
	}
	moveIcon, moveHint := state.sftpPanelMoveAction()
	headerTools = state.orderToolbarTools(toolbarSFTP, headerTools, false)
	// Reserve More and the gaps on either side of the flexible spacer before
	// fitting actions. The eye that reports hidden entries on screen keeps its
	// slot too: it is their status affordance, not just another action.
	const gap = float32(4)
	hidden := folder.showHidden
	width := max(s.bounds.Width-2*sftpPad, 0)
	capacity := max(int((width-sftpHeaderButton-gap)/(sftpHeaderButton+gap)), 0)
	if hidden {
		capacity = max(capacity-1, 0)
	}
	visible := min(len(headerTools), capacity)
	count := len(headerTools) - visible
	for i := 0; i < count; i++ {
		s.overflow[i] = headerTools[visible+i]
	}
	s.overflowTools = s.overflow[:count]
	headerTools = headerTools[:visible]
	if hidden {
		headerTools = append(headerTools, barTool{id: hitSFTPHiddenToggle, icon: iconEye, hint: messages.Tools.FilesHideHidden, style: iconButtonActive})
	}

	// icon declares an icon button where it goes; it is painted after Draw.
	icon := func(side float32, id gfx.ID, glyph uint8, hint string, disabled bool, style iconButtonStyle) {
		s.buttons[s.buttonCount] = sftpIconButton{box: state.declareIconButton(c, id, glyph, hint, disabled, style).Size(side, side), id: id}
		s.buttonCount++
	}
	// A header button stays the target of a secondary press while disabled, so its
	// visibility menu still opens; the other icons keep ordinary disabled hits.
	headerIcon := func(id gfx.ID, glyph uint8, hint string, disabled bool, style iconButtonStyle) {
		box := state.declareIconButtonWithOptions(c, id, glyph, hint, disabled, style, 0, gfx.HitOptions{SecondaryWhenDisabled: true})
		s.buttons[s.buttonCount] = sftpIconButton{box: box.Size(sftpHeaderButton, sftpHeaderButton), id: id}
		s.buttonCount++
	}
	ui.Column(c).Fill().PadSides(0, sftpPad, 6, sftpPad).Background(pal.workspace).Children(func() {
		// Keep the panel's title, docking and close buttons on their own row, with the
		// folder actions below so they do not compete with the title for space.
		s.header = ui.Row(c).FillWidth().Height(infoBarHeight).Shrink(0).MarginSides(0, -sftpPad, 0, -sftpPad).PadX(sftpPad).Background(pal.workspace).Items(ui.Center)
		s.header.Children(func() {
			ui.Text(c, title).FontSize(metrics.FontSizeM).Weight(gfx.WeightSemibold).Color(pal.text).Ellipsis().Grow(1).Height(20).LineHeight(20).MarginSides(0, 8, 0, 0)
			ui.Row(c).Gap(4).Shrink(0).Children(func() {
				headerIcon(hitSFTPPanelSide, moveIcon, moveHint, false, iconButtonQuiet)
				headerIcon(hitSFTPClose, iconClose, messages.TipSFTPClose, false, iconButtonQuiet)
			})
		})
		ui.Row(c).FillWidth().Height(sftpButton).Shrink(0).Gap(gap).Items(ui.Center).MarginSides(6, 0, 0, 0).Children(func() {
			for _, tool := range headerTools {
				headerIcon(tool.id, tool.icon, tool.hint, tool.disabled, tool.style)
			}
			ui.Spacer(c)
			headerIcon(hitSFTPHeaderMore, iconEllipsis, messages.Tools.FilesMore, false, iconButtonQuiet)
		})
		s.headerButtonCount = s.buttonCount
		s.moreButton = ui.Box{}
		for index := range s.buttons[:s.buttonCount] {
			if s.buttons[index].id == hitSFTPHeaderMore {
				s.moreButton = s.buttons[index].box
				break
			}
		}

		// Where we are: one level up, then the tail of the path as many folders as
		// fit. The bookmark controls close the row on the right: the list of the
		// folders kept, and a star for this one.
		ui.Row(c).Height(sftpButton).Shrink(0).Gap(4).Items(ui.Center).MarginSides(6, 0, 0, 0).Children(func() {
			icon(sftpButton, hitSFTPUp, iconSFTPUp, messages.SFTPUp, !local && view.busy || folder.path == "/" || folder.path == "", iconButtonQuiet)
			height := float32(24)
			if state.sftpPathVisible(folder) {
				height = sftpButton
			}
			// The path collapses against the room the crumbs actually get: the
			// panel's padding, the up button, the two side buttons and their
			// gaps, as the painted slot was measured.
			// The painted row folded against the panel less its header buttons,
			// as the hand-painted path did; the up button and the side buttons
			// sit inside that room and the overspill is where the last crumb
			// goes.
			addressWidth := max(s.bounds.Width-3*sftpButton-6, 80)
			var panelDrag *sftpPanelDrag
			if state.sftpPanelCanDrag(view, folder) {
				panelDrag = state.ensureSFTPPanelDrag(view)
			}
			s.crumbs = state.declareSFTPAddressWithDrag(c, folder, hitSFTPPath, hitSFTPPathEdit, hitSFTPBreadcrumbList, addressWidth, !local && view.busy, messages.Tools.FilesPathPlaceholder, panelDrag).Height(height).Grow(1)
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
				filter := state.declareSFTPFilterField(c, folder, hitSFTPFilter, func() {
					state.clickSFTPFilter(view, hitSFTPFilterClear)
				})
				filter.Grow(1).Height(32)
				if filter.Changed() {
					folder.rememberSelection()
					state.applySFTPFilter(view, folder, false)
				}
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
		}).Grow(1).HitWith(hitSFTPListBackground, messages.SFTPFiles, gfx.HitOptions{Role: gfx.RoleGroup}).AlwaysScrollbars().
			ScrollbarLabels(messages.SFTPFiles, "")
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
						add(hitSFTPSend, iconSFTPUpload, messages.Tools.SendToServer, false)
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
				ui.Text(c, details).FontSize(metrics.FontSizeS).Color(pal.muted).Ellipsis().Height(18).LineHeight(18).Shrink(0).MarginSides(2, 0, 2, 2)
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
				ui.Spinner(c).Size(16, 16)
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
	if folder.headerMenuOpen {
		// The menu hangs off the More button's own box: the page places it from
		// the rectangle the control is laid out at, not the one the declaration
		// can read before layout. A rect captured here is empty on the paint
		// that opens the menu, which put the menu at the panel's corner.
		if !state.sftpHeaderMenuAvailable(view) || !s.moreButton.Valid() {
			folder.headerMenuOpen = false
		} else {
			rows := state.panelHeaderMenuRows(folder, view)
			state.declareMenuOn(c, &folder.headerMenuOpen, s.moreButton, &rows, func(id gfx.ID) {
				state.clickSFTPPanelHeaderAction(session, view, id)
			})
		}
	}
	if view.bookmarksOpen {
		places := sftpPlaceBrowser{owner: state, session: session, view: view}
		places.declareDirectoryPlaces(c, s.crumbs)
	}
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

	// The List owns the offset; it draws its own scrollbar, as every page
	// ScrollArea does.
	empty := len(rows) == 0 && (!view.busy || local) && view.errorText == ""
	loading := len(rows) == 0 && view.busy && !local && view.operation == sftpclient.OperationList && view.errorText == ""
	reading := searching && len(rows) == 0 && folder.search.running && folder.search.failure == ""
	if reading {
		empty, loading = false, false
	}
	if loading || reading {
		// The list shows its own spinner in the middle while the folder is read;
		// the label under it says what is being read.
		const spinnerSize = float32(16)
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
		state.declareSFTPRowIcon(c, entry, tint)
		label := ui.Row(c).Grow(1).Clip().Items(ui.Center).MarginSides(0, 0, 0, 8)
		label.Children(func() {
			if !view.local && !result && state.declareSFTPRename(c, view, row.ID, entry.entry) {
				return
			}
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
	if view.rename == nil && !result && state.sftpPanelCanDrag(view, folder) {
		drag := state.ensureSFTPPanelDrag(view)
		box = box.DragSource(&drag.state)
		if entry.entry.IsDir() && !entry.entry.LinkBroken {
			box = box.DropTarget(&drag.state, drag.accept, drag.drop)
		}
	}
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

// declareSFTPAddress declares where the panel or a pane is: the page path field
// while the path is being edited, else the folders of the path as links. width is
// the room the row has, for folding the leading folders away.
func (state *State) declareSFTPAddress(c ui.C, folder *sftpFolder, fieldID, editID gfx.ID, links gfx.Scope, width float32, readOnly bool, placeholder string) ui.Box {
	return state.declareSFTPAddressWithDrag(c, folder, fieldID, editID, links, width, readOnly, placeholder, nil)
}

func (state *State) declareSFTPAddressWithDrag(c ui.C, folder *sftpFolder, fieldID, editID gfx.ID, links gfx.Scope, width float32, readOnly bool, placeholder string, drag *sftpPanelDrag) ui.Box {
	if state.sftpPathVisible(folder) {
		folder.crumbBoxes = folder.crumbBoxes[:0]
		editor := folder.pathInput()
		label := state.messages.Tools.FilesPathLabel
		if readOnly {
			// The server owns the path while it answers: show the draft, keep the
			// keyboard in it, but offer no text input, so typing is refused as it
			// was while the path was painted read-only.
			theme := state.Theme()
			return ui.Text(c, editor.String()).FontSize(theme.Metrics.FontSizeM).Color(theme.Foreground).
				Ellipsis().PadX(10).Height(32).Grow(1).Background(theme.Background).
				Border(1, theme.Input).Radius(theme.Metrics.RadiusControl).
				HitWith(fieldID, label, gfx.HitOptions{Role: gfx.RoleTextField})
		}
		// Paint with PlatformEditing so long directory paths scroll horizontally
		// with the caret and native pointer hit-testing stays aligned with the
		// visible text.
		value := editor.String()
		theme := state.Theme()
		return ui.Slot(c).Height(32).Grow(1).Draw(func(d *ui.DrawContext) {
			focused := d.Canvas.Focused() == fieldID
			components.PaintInput(d.Canvas, theme, d.Rect, components.InputSnapshot{
				ID:              fieldID,
				Label:           label,
				Value:           value,
				Placeholder:     placeholder,
				Hint:            placeholder,
				Focused:         focused,
				PlatformEditing: focused && value != "",
			})
		})
	}
	return state.declareSFTPBreadcrumbs(c, folder, editID, links, width, placeholder, drag)
}

// declareSFTPBreadcrumbs declares the path as links, keeping the folders nearest
// the current one and folding the rest into an ellipsis when the path is wider
// than width.
func (state *State) declareSFTPBreadcrumbs(c ui.C, folder *sftpFolder, edit gfx.ID, links gfx.Scope, width float32, placeholder string, drag *sftpPanelDrag) ui.Box {
	crumbs := folder.crumbs
	folder.crumbBoxes = folder.crumbBoxes[:0]
	// The path reads at the size of the list under it, not the theme's caption.
	// A separator is a chevron the size of the text with gap-2.5 (10) on each side.
	const size, separator = float32(13), float32(33)
	canvas, theme := c.Canvas(), state.Theme()
	first := 0
	total := float32(0)
	for index := len(crumbs) - 1; index >= 0; index-- {
		next := total + canvas.TextWidth(crumbs[index].label, size) + separator
		if index < len(crumbs)-1 && next > width-separator*2 {
			first = index + 1
			break
		}
		total = next
	}
	row := ui.Row(c).Height(32).Grow(1).Clip().Gap(6).Items(ui.Center).
		HitWith(edit, state.messages.Tools.FilesPathLabel, gfx.HitOptions{Role: gfx.RoleButton, Hint: placeholder})
	row.Children(func() {
		if first > 0 {
			// The painted ellipsis was a three-dot icon in a twenty-pixel box.
			ui.Icon(c, lucideIcons[iconEllipsis]).Size(16, 16).MarginSides(0, 2, 0, 2).
				Color(theme.MutedForeground).Shrink(0)
		}
		for index := first; index < len(crumbs); index++ {
			if index > first || first > 0 {
				ui.Icon(c, lucideIcons[iconChevronRight]).Size(size, size).Color(theme.MutedForeground).Shrink(0)
			}
			label := crumbs[index].label
			if index == len(crumbs)-1 {
				ui.Text(c, label).FontSize(size).Color(theme.Foreground).Ellipsis()
				continue
			}
			crumbID := links.At(index)
			box := ui.Text(c, label).FontSize(size).Color(theme.MutedForeground).Ellipsis().
				HitWith(crumbID, label, gfx.HitOptions{Role: gfx.RoleLink}).
				Hover(ui.Look{}.Color(theme.Foreground)).
				Pressed(ui.Look{}.Color(theme.Foreground))
			if drag != nil {
				box = box.HitOutset(3, 3, 3, 3).DropTarget(&drag.state, drag.accept, drag.drop)
			}
			folder.crumbBoxes = append(folder.crumbBoxes, sftpCrumbBox{path: crumbs[index].path, box: box})
		}
	})
	return row
}

// paintSFTPBreadcrumbs draws the path as links, keeping the folders nearest the
// current one and folding the rest into an ellipsis when the path is wider than
// the panel.
func (state *State) paintSFTPBreadcrumbs(canvas *gfx.Canvas, bounds gfx.Rect, crumbList []sftpCrumb) {
	state.paintSFTPFolderBreadcrumbs(canvas, bounds, crumbList, hitSFTPPathEdit, hitSFTPBreadcrumbList)
}

func (state *State) paintSFTPFolderBreadcrumbs(canvas *gfx.Canvas, bounds gfx.Rect, crumbList []sftpCrumb, edit gfx.ID, links gfx.Scope) {
}

// paintSFTPEditorDetails keeps the editor's rectangle for the wheel and the
// legacy scroll handlers. The text area and its spinner are page controls now,
// declared by declareSFTPEditor.
func (state *State) paintSFTPEditorDetails(canvas *gfx.Canvas) {
	editor := &state.chrome.sftpEditor
	view := editor.view
	if view == nil {
		return
	}
	view.editRect = editor.textarea.Rect()
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
		view.editField.Bind(&view.editEditor)
		view.editField.Disabled = view.busy
		editor.textarea = ui.TextAreaID(c, hitSFTPEditor, &view.editField, messages.SFTPEditLabel, "").Grow(1).MinHeight(64)
		ui.Row(c).Height(28).Shrink(0).Gap(8).Items(ui.Center).MarginSides(14, 0, 0, 0).Children(func() {
			if view.busy {
				ui.Spinner(c).Size(16, 16)
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
	case sftpDialogCreateFile:
		title = messages.SFTPNewFileTitle
		fieldLabel = messages.SFTPFileName
		submitLabel = messages.SFTPCreateFile
	case sftpDialogCreateDirectory:
		title = messages.SFTPNewDirectoryTitle
		fieldLabel = messages.SFTPDirectoryName
		submitLabel = messages.SFTPCreateDirectory
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
	case sftpDialogDeleteForce:
		title = messages.SFTPDeleteForceTitle
		description = messages.SFTPDeleteForceDescription
		submitLabel = messages.SFTPDeleteForceConfirm
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
	case sftpDialogDownloadConflict:
		title = messages.SFTPDownloadConflictTitle
		if view.downloadConflictCount > 1 {
			description = fmt.Sprintf(messages.SFTPDownloadConflictCountFmt, view.downloadConflictCount)
		} else {
			description = fmt.Sprintf(messages.SFTPDownloadConflictDescriptionFmt, view.downloadConflictName)
		}
		submitLabel = messages.SFTPDownloadKeepBoth
		initialFocus = hitSFTPDialogSubmit
	}
	fieldDialog := sftpDialogShowsField(view.dialog)
	showError := (view.dialog == sftpDialogDelete || view.dialog == sftpDialogDeleteForce) && view.dialogError != ""
	body := float32(0) // a confirmation is only its text
	switch {
	case view.dialog == sftpDialogDelete:
		// The method label, its one-line hint and the choice control; the
		// dialog's error, when one is left from a failed queue, follows them.
		body = 6 + 18 + 4 + 36 + 4 + c.Theme().Metrics.ControlHeight
		if showError {
			body += 4 + 20
		}
	case view.dialog == sftpDialogDeleteForce:
		if showError {
			body = 20
		}
	case view.dialog == sftpDialogSymlink, view.dialog == sftpDialogChown:
		body = 6 + 2*dialogRowHeight(c.Theme()) // two fields: the link's name and target, or the owner and group
	case view.dialog == sftpDialogChmod:
		// The octal field, then the permission grid: a 16px header row and
		// three 24px rows, with a 4px gap between them.
		body = 6 + dialogRowHeight(c.Theme()) + 16 + 3*24 + 3*4
	case fieldDialog:
		body = 6 + dialogRowHeight(c.Theme())
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
	if view.dialog == sftpDialogDownloadConflict {
		state.dialogActionFooter(c,
			components.ButtonAction{ID: hitSFTPDialogCancel, Label: view.dialogCancelLabel, Variant: components.ButtonSecondary},
			components.ButtonAction{ID: hitSFTPDialogReplace, Label: state.messages.SFTPDownloadReplace, Variant: components.ButtonDestructive},
			components.ButtonAction{ID: hitSFTPDialogSubmit, Label: view.dialogSubmitLabel, Variant: components.ButtonDefault},
		)
		return
	}
	state.dialogActionFooter(c,
		components.ButtonAction{ID: hitSFTPDialogCancel, Label: view.dialogCancelLabel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitSFTPDialogSubmit, Label: view.dialogSubmitLabel, Variant: view.dialogSubmitVariant},
	)
}

// declareSFTPChmodBit declares one checkbox of the permission grid as a compact
// 20px cell: a page slot whose hit is the application's bit identity, drawn with
// the theme's checkbox look. A checkbox control keeps a 40x32 pointer target,
// which would reach the next cell of this grid, so the grid paints its own.
func (state *State) declareSFTPChmodBit(c ui.C, id gfx.ID, label string, checked, enabled bool) ui.Box {
	options := gfx.HitOptions{Role: gfx.RoleCheckbox, Checked: checked, Disabled: !enabled}
	// The painted cell was a 20x20 slot with a 24x24 target starting four pixels
	// up and left of the slot, and the 16px square at its top left.
	cell := ui.Slot(c).Size(20, 20).Shrink(0).HitWith(id, label, options).HitOutset(4, 0, 0, 4)
	cell.Draw(func(d *ui.DrawContext) {
		theme := d.Theme
		side := float32(16)
		box := gfx.Rect{
			X: d.Rect.X, Y: d.Rect.Y,
			Width: side, Height: side,
		}
		fill, border, ink := theme.Background, theme.Input, theme.PrimaryForeground
		if checked {
			fill, border = theme.Primary, theme.Primary
		}
		if !enabled {
			fill.Alpha *= .4
			border.Alpha *= .4
			ink.Alpha *= .4
		}
		d.Canvas.Rounded(box, cornerPx(3), fill)
		d.Canvas.RoundedOutline(box, 3, 1, border)
		if checked {
			d.Canvas.DrawIcon(lucideIcons[iconCheck], gfx.Rect{X: box.X + 2, Y: box.Y + 2, Width: 12, Height: 12}, ink)
		}
	})
	return cell
}

// declareSFTPDialogField declares field i (0 the first, 1 a link's target) of a dialog as a
// page field: a labelled row around a text bound to the dialog's editor. The error of the
// dialog is the first field's.
func (state *State) declareSFTPDialogField(c ui.C, view *sftpView, i int, label string) {
	if !view.dialogBound {
		view.dialogTexts[0].Bind(&view.dialogEditor)
		view.dialogTexts[1].Bind(&view.dialogEditor2)
		view.dialogBound = true
	}
	id := hitSFTPDialogField
	if i == 1 {
		id = hitSFTPDialogField2
	}
	invalid := i == 0 && view.dialogError != ""
	row := &view.dialogRows[i]
	row.Invalid = invalid
	row.Error = ""
	if i == 0 {
		row.Error = view.dialogError
	}
	ui.FormField(c, row, label).FillWidth().Height(dialogRowHeight(c.Theme())).Shrink(0).Children(func() {
		ui.TextFieldID(c, id, &view.dialogTexts[i], label, "")
	})
}

// declareSFTPOwnershipFields declares the ownership dialog's two editable
// suggestion fields. Each opens a filtered list of the accounts the server's
// files name; the text stays free, so an account those files do not list can
// still be typed as a name or a numeric id.
func (state *State) declareSFTPOwnershipFields(c ui.C, view *sftpView) {
	messages := state.messages
	labels := [2]string{messages.SFTPPermissionsOwner, messages.SFTPPermissionsGroup}
	for field := range 2 {
		id := hitSFTPDialogField
		if field == 1 {
			id = hitSFTPDialogField2
		}
		editor := view.ownershipEditor(field)
		if !view.chownBound[field] {
			view.chownTexts[field].Bind(editor)
			view.chownBound[field] = true
		}
		row := &view.chownRows[field]
		row.For = id
		row.Invalid = view.dialogError != ""
		row.Error = ""
		if field == 0 {
			row.Error = view.dialogError
		}
		var input ui.Box
		ui.FormField(c, row, labels[field]).FillWidth().Height(dialogRowHeight(c.Theme())).Shrink(0).Children(func() {
			input = ui.TextFieldID(c, id, &view.chownTexts[field], labels[field], "").Height(32)
			// A typed change opens the suggestions; the field reports an edit,
			// and a suggestion the program wrote is not one.
			if input.Changed() && editor.String() != view.ownership.lastQuery[field] {
				view.ownership.open[field] = true
				view.ownership.highlighted[field] = 0
				view.ownership.lastQuery[field] = editor.String()
			}
		})
		if field == 0 {
			view.dialogFirst = input
		} else {
			view.dialogSecond = input
		}
		if view.ownership.open[field] {
			state.declareSFTPOwnershipPopup(c, view, field, input)
		}
	}
}

// declareSFTPOwnershipPopup declares one field's suggestions in a page popover,
// under the field: a list of the accounts the server's files name, filtered by
// what is typed. A chosen row keeps the identity the dialog's handler knows.
func (state *State) declareSFTPOwnershipPopup(c ui.C, view *sftpView, field int, anchor ui.Box) {
	messages := state.messages
	query := view.ownershipEditor(field).String()
	matches := view.ownership.matches(field, query)
	// The suggestions are as wide as the field they hang from, as the painted
	// list was; a first paint without a placed field falls back to a panel.
	width := view.chownFieldWidth
	if width <= 0 {
		width = 320
	}
	spec := ui.PopoverSpec{
		Scope: dialogScope, DismissID: hitSFTPChownDismiss,
		Side: components.PopoverBottom, Align: ui.Start, Offset: 8, Width: width,
	}
	ui.PopoverWith(c, anchor, &view.ownership.open[field], spec, func() {
		ui.Column(c).Gap(2).Children(func() {
			if len(matches) == 0 {
				ui.Text(c, messages.SFTPOwnershipNoMatches).FontSize(metrics.FontSizeS).
					Color(state.pal.muted).PadSides(8, 0, 6, 0).Height(36)
			}
			for index, account := range matches {
				row := ui.Row(c.KeyInt(index)).Height(32).PadX(8).Gap(8).Items(ui.Center).
					HitWith(ownershipItemScope(field).At(index), account.name, gfx.HitOptions{Role: gfx.RoleOption})
				if index == view.ownership.highlighted[field] {
					row = row.Background(state.pal.accentSoft)
				}
				row.Children(func() {
					ui.Text(c, account.name).FontSize(metrics.FontSizeM).Color(state.pal.text).Ellipsis().Grow(1)
					ui.Text(c, strconv.FormatUint(uint64(account.id), 10)).FontSize(metrics.FontSizeS).Color(state.pal.muted).Shrink(0)
				})
			}
		})
	})
}

// sftpDeleteMethodLabel is one segment of the delete confirmation's method
// choice.
func (state *State) sftpDeleteMethodLabel(method sftpDeleteMethod) string {
	if method == sftpDeleteForce {
		return state.messages.SFTPDeleteMethodForce
	}
	return state.messages.SFTPDeleteMethodNormal
}

// declareSFTPDialogBody lays out the dialog's body: the delete method choice,
// one labelled input, two for a link, the permission grid under a chmod field,
// or the error of a failed queue when that is what the dialog says. The parts
// painted by hand are kept in view so the painter can fill the boxes the page
// laid out.
func (state *State) declareSFTPDialogBody(c ui.C, view *sftpView) {
	showError := (view.dialog == sftpDialogDelete || view.dialog == sftpDialogDeleteForce) && view.dialogError != ""
	view.dialogFirst, view.dialogSecond = ui.Box{}, ui.Box{}
	view.dialogChmod = [9]ui.Box{}
	column := ui.Column(c).Fill()
	if showError {
		column.Justify(ui.Center) // as a label in the whole body
	}
	column.Children(func() {
		switch {
		case view.dialog == sftpDialogDelete:
			inner := ui.Column(c).Shrink(0).Gap(4).PadSides(6, 4, 0, 4)
			inner.Children(func() {
				theme := c.Theme()
				ui.Text(c, state.messages.SFTPDeleteMethodLabel).FontSize(theme.Metrics.FontSizeM).
					Weight(gfx.WeightMedium).Color(state.pal.text).Ellipsis()
				ui.Text(c, state.messages.SFTPDeleteMethodHint).FontSize(theme.Metrics.FontSizeS).
					Color(state.pal.muted).LineHeight(18).Height(36)
				ui.Choice(c, &view.deleteMethod, sftpDeleteMethods[:], state.sftpDeleteMethodLabel).
					Height(theme.Metrics.ControlHeight)
				if showError {
					ui.Text(c, view.dialogError).FontSize(theme.Metrics.FontSizeS).
						Weight(gfx.WeightMedium).Color(state.pal.danger).Ellipsis()
				}
			})
		case view.dialog == sftpDialogDeleteForce && showError:
			ui.Text(c, view.dialogError).FontSize(c.Theme().Metrics.FontSizeS).
				Weight(gfx.WeightMedium).Color(state.pal.danger).Ellipsis()
		case sftpDialogShowsField(view.dialog):
			inner := ui.Column(c).Shrink(0).Gap(8).PadSides(6, 4, 0, 4)
			inner.Children(func() {
				if view.dialog == sftpDialogChown {
					state.declareSFTPOwnershipFields(c, view)
				} else {
					state.declareSFTPDialogField(c, view, 0, view.dialogFieldLabel)
				}
				if view.dialog == sftpDialogSymlink {
					state.declareSFTPDialogField(c, view, 1, state.messages.Tools.LinkTarget)
				}
				if view.dialog == sftpDialogChmod {
					view.dialogChmod = state.declareSFTPChmodGrid(c, view)
				}
			})
			if view.dialog == sftpDialogChown {
				// The field's width is only known once the body is laid out; the
				// hook that reads it runs after that, in the same paint.
				if view.fieldWidthDraw == nil {
					view.fieldWidthDraw = func(*ui.DrawContext) {
						if width := view.dialogFirst.Rect().Width; width > 0 {
							view.chownFieldWidth = width
						}
					}
				}
				inner.DrawOver(view.fieldWidthDraw)
			}
		}
	})
}

// declareSFTPChmodGrid lays out the three permission rows (owner, group,
// others × read, write, execute) under the octal field. Each box mirrors one bit
// of the octal field: tapping it rewrites the field and typing moves the boxes
// (issue #392). While the field holds no usable mode the boxes stand down.
func (state *State) declareSFTPChmodGrid(c ui.C, view *sftpView) [9]ui.Box {
	m := state.messages
	words := [3]string{m.SFTPPermissionsRead, m.SFTPPermissionsWrite, m.SFTPPermissionsExecute}
	groups := [3]string{m.SFTPPermissionsOwner, m.SFTPPermissionsGroup, m.SFTPPermissionsOthers}
	mode, ok := sftpChmodFromDialog(view)
	var boxes [9]ui.Box
	grid := ui.Column(c).Shrink(0).Gap(4)
	grid.Children(func() {
		ui.Row(c).Height(16).Shrink(0).Gap(8).Items(ui.Center).Children(func() {
			ui.Slot(c).Width(72).Shrink(0)
			for _, letter := range []string{"R", "W", "X"} {
				ui.Text(c, letter).FontSize(metrics.FontSizeS).Color(state.pal.muted).Width(20).TextCenter().Shrink(0)
			}
			ui.Spacer(c).Grow(1)
		})
		for group := range groups {
			ui.Row(c).Height(24).Shrink(0).Gap(8).Items(ui.Center).Children(func() {
				ui.Text(c, groups[group]).FontSize(metrics.FontSizeS).Color(state.pal.text).Width(72).Shrink(0)
				for perms := range words {
					index := group*3 + perms
					checked := ok && mode&(1<<uint(8-index)) != 0
					label := groups[group] + " " + words[perms]
					// The grid is tighter than a Checkbox's 40x32 target, whose
					// neighbours would overlap; the cell draws its own box and
					// takes a 20px target, as the permission grid always did.
					boxes[index] = state.declareSFTPChmodBit(c, hitSFTPChmodBits.At(index), label, checked, ok)
				}
				ui.Spacer(c).Grow(1)
			})
		}
	})
	return boxes
}

// panelHeaderMenuRows is the docked panel's More menu: the actions its narrow
// header could not fit, the history, the filters, follow and the editor, and the
// folder-size action. The rows keep the identities its dispatcher knows.
func (state *State) panelHeaderMenuRows(folder *sftpFolder, view *sftpView) menuRows {
	m := state.messages
	tools := m.Tools
	var rows menuRows
	// Follow has a permanent entry below; the overflow row would repeat it.
	overflowed := false
	for _, tool := range state.chrome.sftpPanel.overflowTools {
		if tool.id == hitSFTPFollowToggle {
			continue
		}
		rows.add(components.ContextMenuItem{ID: tool.id, Label: tool.hint, Icon: lucideIcons[tool.icon], Disabled: tool.disabled})
		overflowed = true
	}
	if overflowed {
		rows.separate()
	}
	ready := state.sftpNavigationReady(view, view.local)
	_, canBack := folder.historyTarget(-1)
	_, canForward := folder.historyTarget(1)
	rows.add(components.ContextMenuItem{ID: hitSFTPHistoryBack, Label: tools.FilesBack, Shortcut: "Alt+Left", Disabled: !ready || !canBack})
	rows.add(components.ContextMenuItem{ID: hitSFTPHistoryForward, Label: tools.FilesForward, Shortcut: "Alt+Right", Disabled: !ready || !canForward})
	if len(folder.history.recent) > 0 {
		rows.add(components.ContextMenuItem{Label: tools.FilesRecent, Disabled: true})
		for index, directory := range folder.history.recent {
			rows.add(components.ContextMenuItem{ID: sftpRecentHits.At(index), Label: directory, Hint: directory, Disabled: !ready, Selected: directory == folder.path})
		}
	}
	rows.add(components.ContextMenuItem{ID: hitSFTPHeaderFilter, Label: tools.FilesFilterLabel, Icon: lucideIcons[iconSearch], Kind: components.ContextMenuCheckbox, Checked: folder.filterOpen || folder.filterInputEditor().String() != ""})
	hiddenIcon, hiddenLabel := uint8(iconEyeOff), tools.FilesShowHidden
	if folder.showHidden {
		hiddenIcon, hiddenLabel = iconEye, tools.FilesHideHidden
	}
	rows.add(components.ContextMenuItem{ID: hitSFTPHeaderHidden, Label: hiddenLabel, Icon: lucideIcons[hiddenIcon], Kind: components.ContextMenuCheckbox, Checked: folder.showHidden})
	rows.add(components.ContextMenuItem{ID: hitSFTPHeaderFollow, Label: tools.FilesFollow, Icon: lucideIcons[iconFollowFolder], Kind: components.ContextMenuCheckbox, Checked: !state.sftpFollowOff})
	if label, disabled := state.sftpEditorItem(view, folder); label != "" {
		rows.add(components.ContextMenuItem{ID: hitSFTPHeaderEditor, Label: label, Icon: lucideIcons[iconExternal], Disabled: disabled})
	}
	rows.separate()
	rows.add(components.ContextMenuItem{
		ID: hitSFTPCalculateDirectorySize, Label: m.SFTPWindow.CalculateDirectorySize, Icon: lucideIcons[iconInfo],
		Disabled: folder.path == "" || !view.local && view.openFiles == nil,
	})
	return rows
}
