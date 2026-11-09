package workbench

import (
	"fmt"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

var sftpWindowHits = tree.Feature("sftp-window")
var (
	hitSFTPDetach    = tree.ID(sftpHits, "detach")
	sftpWindowSides  = tree.Sub(sftpWindowHits, tree.Name("sides"))
	sftpWindowLocal  = tree.Sub(sftpWindowSides, tree.Name("local"))
	sftpWindowRemote = tree.Sub(sftpWindowSides, tree.Name("remote"))
	sftpWindowRows   = [2]gfx.Scope{tree.Sub(sftpWindowLocal, tree.Name("rows")), tree.Sub(sftpWindowRemote, tree.Name("rows"))}
	// sftpWindowListRows records the scopes the two Lists name their rows in as
	// they declare them. The window addresses rows through them, and the test
	// helpers that still speak in sftpWindowRows resolve through them too.
	sftpWindowListRows           = [2]gfx.Scope{}
	sftpWindowFilter             = [2]gfx.ID{tree.ID(sftpWindowLocal, "filter"), tree.ID(sftpWindowRemote, "filter")}
	sftpWindowFilterClear        = [2]gfx.ID{tree.ID(sftpWindowLocal, "filter-clear"), tree.ID(sftpWindowRemote, "filter-clear")}
	sftpWindowTreeToggle         = [2]gfx.ID{tree.ID(sftpWindowLocal, "tree-toggle"), tree.ID(sftpWindowRemote, "tree-toggle")}
	sftpWindowTreeRows           = [2]gfx.Scope{tree.Sub(sftpWindowLocal, tree.Name("tree-rows")), tree.Sub(sftpWindowRemote, tree.Name("tree-rows"))}
	sftpWindowTreeDisclosures    = [2]gfx.Scope{tree.Sub(sftpWindowLocal, tree.Name("tree-disclosures")), tree.Sub(sftpWindowRemote, tree.Name("tree-disclosures"))}
	sftpWindowTreeViewport       = [2]gfx.ID{tree.ID(sftpWindowLocal, "tree"), tree.ID(sftpWindowRemote, "tree")}
	sftpWindowTreeBar            = [2]gfx.ID{tree.ID(sftpWindowLocal, "tree-scroll"), tree.ID(sftpWindowRemote, "tree-scroll")}
	hitSFTPWindowCoverage        = tree.ID(sftpWindowLocal, "coverage")
	sftpWindowHeaderHidden       = [2]gfx.ID{tree.ID(sftpWindowLocal, "header-hidden"), tree.ID(sftpWindowRemote, "header-hidden")}
	sftpWindowHeaderEditor       = [2]gfx.ID{tree.ID(sftpWindowLocal, "header-editor"), tree.ID(sftpWindowRemote, "header-editor")}
	sftpWindowHidden             = [2]gfx.ID{tree.ID(sftpWindowLocal, "hidden"), tree.ID(sftpWindowRemote, "hidden")}
	sftpWindowMore               = [2]gfx.ID{tree.ID(sftpWindowLocal, "more"), tree.ID(sftpWindowRemote, "more")}
	sftpWindowFilterToggle       = [2]gfx.ID{tree.ID(sftpWindowLocal, "filter-toggle"), tree.ID(sftpWindowRemote, "filter-toggle")}
	hitSFTPWindowSudoMode        = tree.ID(sftpWindowRemote, "sudo-mode")
	hitSFTPWindowHeaderDismiss   = tree.ID(sftpWindowHits, "header-dismiss")
	sftpWindowPath               = [2]gfx.ID{tree.ID(sftpWindowLocal, "path"), tree.ID(sftpWindowRemote, "path")}
	sftpWindowList               = [2]gfx.ID{tree.ID(sftpWindowLocal, "list"), tree.ID(sftpWindowRemote, "list")}
	sftpWindowSortHits           = [2]gfx.Scope{tree.Sub(sftpWindowLocal, tree.Name("sort")), tree.Sub(sftpWindowRemote, tree.Name("sort"))}
	sftpWindowActions            = [2]gfx.Scope{tree.Sub(sftpWindowLocal, tree.Name("actions")), tree.Sub(sftpWindowRemote, tree.Name("actions"))}
	sftpWindowMenuActions        = tree.Sub(sftpWindowHits, tree.Name("menu-actions"))
	sftpWindowUp                 = [2]gfx.ID{tree.ID(sftpWindowLocal, "up"), tree.ID(sftpWindowRemote, "up")}
	sftpWindowRefresh            = [2]gfx.ID{tree.ID(sftpWindowLocal, "refresh"), tree.ID(sftpWindowRemote, "refresh")}
	sftpWindowNewFolder          = [2]gfx.ID{tree.ID(sftpWindowLocal, "new-folder"), tree.ID(sftpWindowRemote, "new-folder")}
	sftpWindowBar                = [2]gfx.ID{tree.ID(sftpWindowLocal, "scroll"), tree.ID(sftpWindowRemote, "scroll")}
	hitSFTPWindowSplit           = tree.ID(sftpWindowHits, "split")
	hitSFTPWindowUpload          = tree.ID(sftpWindowHits, "upload")
	hitSFTPWindowDownload        = tree.ID(sftpWindowHits, "download")
	hitSFTPWindowCloseCancel     = tree.ID(sftpWindowHits, "close-cancel")
	hitSFTPWindowCloseConfirm    = tree.ID(sftpWindowHits, "close-confirm")
	hitSFTPWindowExternalReplace = tree.ID(sftpWindowHits, "external-replace")
	hitSFTPWindowExternalKeep    = tree.ID(sftpWindowHits, "external-keep")
	hitSFTPWindowMenuDismiss     = tree.ID(sftpWindowHits, "menu-dismiss")
	hitSFTPWindowMenuCopy        = tree.ID(sftpWindowHits, "menu-copy")
	hitSFTPWindowQueueBar        = tree.ID(sftpWindowHits, "queue-scroll")
	sftpWindowQueueRows          = tree.Sub(sftpWindowHits, tree.Name("queue-rows"))
)

type sftpWindowUI struct {
	// root names this window's pages in the identity tree: every window is a
	// page of its own, and no two windows may name their controls alike.
	root                string
	windowPage          *ui.Page
	windowView          func(ui.C)
	connectPage         *ui.Page
	connectView         func(ui.C)
	panePages           [2]*ui.Page
	paneView            func(ui.C)
	scratch             sftpWindowScratch
	connect             sftpWindowConnectScratch
	size                gfx.Size
	list                [2]gfx.Rect
	offset              [2]float32
	offsetSynced        [2]float32
	typeahead           [2]components.MenuTypeahead
	bars                [2]components.ScrollAreaGeometry
	barDrag             int // 1: local, 2: remote, 3: queue; zero when idle
	barGrab             float32
	treeBarDrag         int // 1: local tree, 2: remote tree; zero when idle
	treeBarGrab         float32
	fraction            float32
	split               components.Resizable
	splitting           bool
	tooltip             components.HoverTooltip
	menuOpen            bool
	menuAt              gfx.Point
	queueBounds         gfx.Rect
	queueOffset         float32
	queueBar            components.ScrollAreaGeometry
	sort                [2]sftpWindowSort
	menuScroll          components.MenuScroll
	headerAnchor        [2]gfx.Rect
	headerScroll        components.MenuScroll
	selection           [2]sftpWindowSelection
	profileQuickOpen    bool
	profileQuickFocus   bool
	profileQuickScroll  components.MenuScroll
	profileQuickAnchor  gfx.Rect
	profileHoverOpen    bool
	profileHoverAnchor  gfx.Rect
	profileHoverTrigger gfx.ID
}

// sftpWindowScratch is the room the window's page and each pane's page leave for
// what is painted by hand after they have been laid out.
type sftpWindowScratch struct {
	body, queue ui.Box
	local       bool
	address     ui.Box
	list        ui.Box
	tree        ui.Box
	treeSet     bool
	filter      ui.Box
	sudoMode    ui.Box
	buttons     [20]sftpIconButton
	buttonCount int
}

// sftpWindowConnectScratch is the room the connecting page leaves for its icon.
type sftpWindowConnectScratch struct{ badge ui.Box }

// pageRoot is this window's page root: unique per window, since every window
// names its controls in the window's one identity tree.
func (w *sftpWindow) pageRoot() string {
	if w.ui.root == "" {
		w.ui.root = fmt.Sprintf("sftp-window-%d", w.session.ID)
	}
	return w.ui.root
}

func sideIndex(local bool) int {
	if local {
		return 0
	}
	return 1
}

func (w *sftpWindow) paint(canvas *gfx.Canvas, size gfx.Size) {
	canvas.SetTree(&tree)
	if id, ok := canvas.ClickTarget(); ok {
		canvas.AfterPaint(func() { w.click(id) })
	}
	w.ui.size = size
	theme, m := w.owner.Theme(), w.owner.messages
	canvas.Fill(gfx.Rect{Width: size.Width, Height: size.Height}, theme.Background)
	if w.view.worker == nil {
		w.paintConnecting(canvas, size)
		return
	}
	if w.ui.windowPage == nil {
		w.ui.windowPage = ui.NewPage(ui.PageConfig{Tree: &tree, Root: w.pageRoot(), Theme: &w.owner.theme})
		w.ui.windowView = w.declareWindow
	}
	w.ui.scratch = sftpWindowScratch{}
	w.ui.windowPage.Draw(canvas, gfx.Rect{Width: size.Width, Height: size.Height}, w.ui.windowView)
	body := w.ui.scratch.body.Rect()
	var queue gfx.Rect
	if w.queueCount() > 0 {
		queue = w.ui.scratch.queue.Rect()
	}
	w.ui.split = components.BeginResizable(canvas, theme, hitSFTPWindowSplit, body, w.ui.fraction).LimitsPx(220, 220)
	panels := w.ui.split.Paint(m.SFTPWindow.Title)
	firstLocal := w.owner.sftpWindowOrder == sftpLocalLeft
	w.paintPane(canvas, panels.First, firstLocal)
	w.paintPane(canvas, panels.Second, !firstLocal)
	if w.queueCount() > 0 {
		w.paintQueue(canvas, queue)
	} else {
		w.ui.queueBounds = gfx.Rect{}
		w.ui.queueOffset = 0
	}
	if w.view.bookmarksOpen {
		w.paintDirectoryPlaces(canvas)
	}
	if w.view.dialog != sftpDialogNone {
		w.owner.paintSFTPDialog(canvas, gfx.Rect{Width: size.Width, Height: size.Height}, w.view)
	}
	w.owner.paintSFTPFileProperties(canvas, gfx.Rect{Width: size.Width, Height: size.Height}, &w.fileProperties)
	if w.closeConfirm {
		components.BeginAlertDialog(canvas, theme, gfx.Rect{Width: size.Width, Height: size.Height}, components.AlertDialogOptions{Layer: dialogLayer + 1, Scope: dialogScope, ActionID: hitSFTPWindowCloseConfirm, CancelID: hitSFTPWindowCloseCancel, InitialFocusID: hitSFTPWindowCloseCancel, ActionVariant: components.ButtonDestructive}).Paint(m.SFTPWindow.CloseTitle, w.closeDescription(), m.SFTPWindow.CloseTransfers, m.Cancel)
	}
	if w.externalConflict != 0 && !w.closeConfirm && w.view.dialog == sftpDialogNone {
		if edit := w.owner.editByID(w.externalConflict); edit != nil {
			m := w.owner.messages.Tools
			components.BeginAlertDialog(canvas, theme, gfx.Rect{Width: size.Width, Height: size.Height}, components.AlertDialogOptions{Layer: dialogLayer + 1, Scope: dialogScope, ActionID: hitSFTPWindowExternalReplace, CancelID: hitSFTPWindowExternalKeep, InitialFocusID: hitSFTPWindowExternalKeep, ActionVariant: components.ButtonDestructive}).Paint(m.ExtEditConflictTitle, fmt.Sprintf(m.ExtEditConflictDescriptionFmt, edit.name), m.ExtEditReplace, w.owner.messages.Cancel)
		}
	}
	if w.ui.menuOpen {
		w.paintMenu(canvas)
	}
	if side, ok := w.headerMenu(); ok {
		if w.window == nil {
			// Hosted by a main-window tab: the page paints the menu, so its
			// backdrop covers the window bar instead of only the tab.
			w.owner.requestSFTPWindowHeaderMenu(w, side, w.ui.headerAnchor[side])
		} else {
			// A standalone window is its own canvas, so its own viewport is the
			// whole window.
			w.paintHeaderMenu(canvas, side, gfx.Point{}, 0, gfx.Rect{Width: size.Width, Height: size.Height}, &w.ui.headerScroll)
		}
	}
	if w.bookmark.open {
		w.paintBookmarkDialog(canvas, gfx.Rect{Width: size.Width, Height: size.Height})
	}
	if w.ui.profileQuickOpen && !w.bookmark.open {
		w.paintProfileQuickMenu(canvas)
	}
	if !w.bookmark.open && !w.ui.profileQuickOpen {
		w.paintProfileQuickPreview(canvas, gfx.Rect{Width: size.Width, Height: size.Height})
	}
	if w.drag.active {
		w.paintDrag(canvas)
	}
	w.notices.paint(canvas, theme, gfx.Rect{Width: size.Width, Height: size.Height})
	if !w.ui.profileHoverOpen {
		w.ui.tooltip.Paint(canvas, theme, gfx.Rect{Width: size.Width, Height: size.Height}, components.TooltipOptions{Layer: tooltipLayer, SideOffset: tooltipGap}, components.HoverTooltipDelay)
	}
}

// declareWindow declares the window's own chrome: the status line, and the room
// the panes and the transfer queue take.
func (w *sftpWindow) declareWindow(c ui.C) {
	s := &w.ui.scratch
	size := w.ui.size
	theme, m := w.owner.Theme(), w.owner.messages
	ui.Column(c).Fill().Pad(12).Gap(8).Children(func() {
		s.body = ui.Slot(c).Grow(1)
		if w.queueCount() > 0 {
			s.queue = ui.Slot(c).Height(min(244, max(108, size.Height*.28))).Shrink(0)
		}
		status := w.view.status
		if w.view.errorText != "" {
			status = w.view.errorText
		}
		if !w.connected() {
			status = m.SFTPWindow.Disconnected
		}
		if status == "" {
			if w.browse.enabled {
				status = w.browseDescription()
			} else {
				status = m.SFTPWindow.DragHint
			}
		}
		if w.browse.enabled {
			ui.Row(c).Height(20).Gap(6).Items(ui.Center).Children(func() {
				ui.Icon(c, lucideIcons[iconSFTPBrowseSync]).Size(14, 14).Color(w.owner.pal.success)
				ui.Text(c, m.SFTPWindow.BrowseRootLabel).FontSize(theme.Metrics.FontSizeS).Color(theme.Foreground)
				ui.Text(c, w.browse.roots[0]).FontSize(theme.Metrics.FontSizeS).Color(theme.MutedForeground).Ellipsis().Grow(1)
				ui.Icon(c, lucideIcons[iconForward]).Size(12, 12).Color(theme.MutedForeground)
				ui.Text(c, w.browse.roots[1]).FontSize(theme.Metrics.FontSizeS).Color(theme.MutedForeground).Ellipsis().Grow(1)
				if status != "" && status != w.browseDescription() {
					ui.Text(c, status).FontSize(theme.Metrics.FontSizeS).Color(theme.MutedForeground).Ellipsis().Grow(1)
				}
			})
		} else {
			ui.Text(c, status).FontSize(theme.Metrics.FontSizeS).Color(theme.MutedForeground).Ellipsis().LineHeight(20)
		}
	})
}

func (w *sftpWindow) canTransfer(local bool) bool {
	if !w.connected() || w.view.worker == nil || w.navigationBusy() || w.bookmark.open || w.fileProperties.open || w.view.dialog != sftpDialogNone || w.closeConfirm || w.externalConflict != 0 || w.view.path == "" || w.view.lfolder.path == "" {
		return false
	}
	return w.selection(local).count > 0
}

func (w *sftpWindow) paintPane(canvas *gfx.Canvas, bounds gfx.Rect, local bool) {
	side := sideIndex(local)
	folder := w.folder(local)
	theme, m := w.owner.Theme(), w.owner.messages
	if w.ui.panePages[side] == nil {
		name := "remote"
		if local {
			name = "local"
		}
		w.ui.panePages[side] = ui.NewPage(ui.PageConfig{Tree: &tree, Root: w.pageRoot() + "-" + name, Theme: &w.owner.theme})
		w.ui.paneView = w.declarePane
	}
	w.ui.scratch = sftpWindowScratch{local: local}
	w.ui.panePages[side].Draw(canvas, bounds, w.ui.paneView)
	s := &w.ui.scratch
	if !local && sudoSFTPAvailable(&w.session, w.view) {
		components.BeginToggle(canvas, theme, hitSFTPWindowSudoMode, s.sudoMode.Rect(), w.view.sudo).Small().Outline(m.SFTPModeToggle)
	}
	w.owner.paintSFTPIconButtons(canvas, s.buttons[:s.buttonCount])
	for _, button := range s.buttons[:s.buttonCount] {
		if button.id == sftpWindowMore[side] {
			w.ui.headerAnchor[side] = button.box.Rect()
		}
		if button.id == hitSFTPLocationProfiles {
			w.ui.profileHoverAnchor = button.box.Rect()
		}
		if button.id == hitSFTPProfileQuick {
			w.ui.profileQuickAnchor = button.box.Rect()
		}
	}
	if folder.filterOpen {
		w.owner.paintSFTPFilterInput(canvas, s.filter.Rect(), folder, sftpWindowFilter[side], sftpWindowFilterClear[side])
	}
	if !local {
		w.view.places.anchors[1] = s.address.Rect()
	} else {
		w.view.places.anchors[0] = s.address.Rect()
	}
	value := folder.pathInput().String()
	components.PaintInput(canvas, theme, s.address.Rect(), components.InputSnapshot{ID: sftpWindowPath[side], Label: m.SFTPWindow.Path, Value: value, Hint: folder.path, Focused: canvas.Focused() == sftpWindowPath[side], PlatformEditing: canvas.Focused() == sftpWindowPath[side]})
	listBounds := s.list.Rect()
	if s.treeSet {
		treeBounds := s.tree.Rect()
		w.paintDirectoryTree(canvas, treeBounds, local)
	} else {
		w.directoryTree[side].bounds = gfx.Rect{}
	}
	// The List owns the rows, their identity, the choice and the offset; the
	// window keeps what the List cannot paint (the custom scrollbar, and the
	// empty or loading message) and the fields the drop target and the model
	// read between paints.
	w.ui.list[side] = listBounds
	folder.visibleRows = max(1, int(listBounds.Height/sftpRowHeight))
	if scroll := s.list.ScrollState(); scroll != nil {
		if w.ui.offset[side] != w.ui.offsetSynced[side] {
			// The model (history restore, a filter, a sort, a test) asked for an
			// offset by writing it; hand it to the List.
			scroll.ScrollTo(w.ui.offset[side])
		}
		folder.listScroll = scroll
		folder.scrollOffset = scroll.Offset
		w.ui.offset[side] = scroll.Offset
		w.ui.offsetSynced[side] = scroll.Offset
		folder.first = max(0, int(scroll.Offset/sftpRowHeight))
	}
	area := components.BeginScrollArea(canvas, theme, listBounds, gfx.Size{Width: listBounds.Width, Height: float32(len(folder.rows)) * sftpRowHeight}).Offset(0, w.ui.offset[side]).VerticalScrollbar(sftpWindowBar[side], m.SFTPFiles, "")
	w.ui.bars[side] = area.Geometry()
	if len(folder.rows) == 0 {
		label := m.SFTPEmpty
		if folder.filterInputEditor().String() != "" {
			label = fmt.Sprintf(m.Tools.FilesFilterCountFmt, 0, sftpVisibleRowCount(folder))
		}
		if local && folder.coverageEmpty != "" {
			label = folder.coverageEmpty
		}
		if !local && w.view.busy {
			label = m.SFTPLoading
		}
		canvas.LabelCenter(label, listBounds, gfx.TextStyle{Size: theme.Metrics.FontSizeM, Color: theme.MutedForeground})
	}
	area.EndContent()
}

// declarePane declares one side of the window: its title and actions, the path,
// the filter, the column headers and the room the list takes.
func (w *sftpWindow) declarePane(c ui.C) {
	s := &w.ui.scratch
	local := s.local
	folder := w.folder(local)
	theme, m := w.owner.Theme(), w.owner.messages
	bounds := c.Frame().Bounds()
	compact := bounds.Height < 230
	icon := func(id gfx.ID, glyph uint8, hint string, disabled bool) {
		s.buttons[s.buttonCount] = sftpIconButton{box: ui.Slot(c).Size(28, 28), id: id, icon: glyph, hint: hint, disabled: disabled, style: iconButtonQuiet}
		s.buttonCount++
	}
	ui.Column(c).Fill().Pad(8).Gap(6).Children(func() {
		ui.Row(c).Height(28).Gap(4).Items(ui.Center).Children(func() {
			title := m.SFTPWindow.Remote
			if local {
				title = m.SFTPWindow.Local
			}
			ui.Text(c, title).Bold().Color(theme.Foreground).Ellipsis().Grow(1)
			if !local && sudoSFTPAvailable(&w.session, w.view) {
				s.sudoMode = ui.Slot(c).Width(64).Height(28).Shrink(0)
			}
			treeHint := m.SFTPWindow.ShowDirectoryTree
			if w.directoryTree[sideIndex(local)].enabled {
				treeHint = m.SFTPWindow.HideDirectoryTree
			}
			treeDisabled := directoryTreeToggleDisabled(w.directoryTree[sideIndex(local)].enabled, max(bounds.Width-16, 0))
			if treeDisabled {
				treeHint = m.SFTPWindow.DirectoryTreeNeedsWidth
			}
			icon(sftpWindowTreeToggle[sideIndex(local)], iconDirectoryTree, treeHint, treeDisabled)
			if w.directoryTree[sideIndex(local)].enabled {
				s.buttons[s.buttonCount-1].style = iconButtonActive
			}
			if !local && bounds.Width >= 420 {
				icon(hitSFTPWindowUpload, iconUpload, m.SFTPWindow.Upload, !w.canTransfer(true))
				icon(hitSFTPWindowDownload, iconDownload, m.SFTPWindow.Download, !w.canTransfer(false))
			}
			if local || bounds.Width >= 340 {
				icon(sftpWindowUp[sideIndex(local)], iconArrowUp, m.SFTPUp, folder.path == "" || !local && (w.view.busy || !w.connected()))
				icon(sftpWindowRefresh[sideIndex(local)], iconRefresh, m.SFTPRefresh, !local && (w.view.busy || !w.connected()))
			}
			if folder.showHidden {
				icon(sftpWindowHidden[sideIndex(local)], iconEye, m.Tools.FilesHideHidden, false)
				s.buttons[s.buttonCount-1].style = iconButtonActive
			}
			if local {
				icon(hitSFTPLocationProfiles, iconSFTPProfiles, m.SFTPWindow.BookmarkTitle, w.browse.pending != nil)
				icon(hitSFTPProfileQuick, iconChevronDown, m.SFTPWindow.ProfileChoose, w.browse.pending != nil || w.visibleProfileCount() == 0)
				hint := m.SFTPWindow.BrowseSync
				if w.browse.enabled {
					hint = w.browseDescription()
				}
				icon(hitSFTPBrowseSync, iconSFTPBrowseSync, hint, !w.browse.enabled && (w.navigationBusy() || !w.connected()))
				if w.browse.enabled {
					s.buttons[s.buttonCount-1].style = iconButtonActive
				}
			}
			icon(sftpWindowMore[sideIndex(local)], iconEllipsis, m.Tools.FilesMore, false)
		})
		ui.Row(c).Height(32).Gap(2).Items(ui.Center).Children(func() {
			s.address = ui.Slot(c).Height(32).Grow(1)
			listID, starID, listHint := hitSFTPBookmarkList, hitSFTPBookmarkToggle, m.SFTPWindow.DirectoryFavorites
			if local {
				listID, starID, listHint = hitSFTPLocalPlaces, hitSFTPLocalStar, m.SFTPWindow.LocalPlaces
			}
			icon(listID, iconChevronDown, listHint, false)
			star, hint := uint8(iconStar), m.SFTPWindow.FavoriteAdd
			if w.directoryFavorite(local) {
				star, hint = iconStarSolid, m.SFTPWindow.FavoriteRemove
			}
			icon(starID, star, hint, folder.path == "" || local && w.pendingLocal != "" || !local && (w.view.busy || !w.connected() || w.session.host.CatalogID == ""))
		})
		if folder.filterOpen {
			s.filter = ui.Slot(c).Height(32).Shrink(0)
		}
		if local {
			w.owner.declareLocalCoverage(c, folder, hitSFTPWindowCoverage)
		}
		ui.Row(c).Height(20).Gap(8).PadX(8).Items(ui.Center).Children(func() {
			for _, entry := range w.owner.sftpColumns.entries {
				if !entry.Enabled {
					continue
				}
				column, label := int(entry.Key), m.SFTPWindow.Name
				switch entry.Key {
				case sftpColumnSize:
					label = m.SFTPWindow.Size
				case sftpColumnModified:
					label = m.SFTPWindow.Modified
				}
				if w.ui.sort[sideIndex(local)].column == column {
					if w.ui.sort[sideIndex(local)].descending {
						label += " ↓"
					} else {
						label += " ↑"
					}
				}
				box := ui.Text(c, label).FontSize(theme.Metrics.FontSizeS).Color(theme.MutedForeground).Ellipsis().HitWith(sftpWindowSortHits[sideIndex(local)].At(column), label, gfx.HitOptions{Role: gfx.RoleButton})
				if column == 0 {
					box.Grow(1)
				} else if column == 1 {
					box.Width(62).TextEnd()
				} else {
					box.Width(w.dateWidth(bounds.Width))
				}
			}
		})
		contentWidth := max(bounds.Width-16, 0)
		treeWidth, showTree := directoryTreePaneWidths(contentWidth)
		showTree = showTree && w.directoryTree[sideIndex(local)].enabled
		listWidth := contentWidth - 4
		if showTree {
			listWidth = max(listWidth-treeWidth-directoryTreeGap, 0)
		}
		rowWidth := max(listWidth-10, 0)
		ui.Row(c).Grow(1).HitWith(sftpWindowList[sideIndex(local)], m.SFTPFiles, gfx.HitOptions{Role: gfx.RoleGroup}).Children(func() {
			if showTree {
				s.tree = ui.Slot(c).Width(treeWidth)
				s.treeSet = true
				ui.Slot(c).Width(directoryTreeGap)
			}
			// The frame around the rows, as the panel has always drawn it: the
			// List sits two pixels in so its rows and scrollbar stay clear of
			// the rounded edge.
			frame := ui.Column(c).Grow(1).Pad(1).Background(theme.Background).Radius(6).Border(1, theme.Border.Mix(theme.MutedForeground, .4))
			frame.Children(func() {
				folder.prepareSFTPList()
				folder.seedSFTPListChoice()
				s.list = ui.List(c, &folder.list, len(folder.rows), func(row ui.ListRow) {
					w.declareListRow(row, sideIndex(local), bounds.Width, rowWidth)
				}).Grow(1).Label(m.SFTPFiles)
				folder.seedSFTPListCurrent()
				if folder.list.Changed() {
					folder.takeSFTPListChoice()
					w.selectionChanged(local)
				}
			})
		})
		selection := w.selection(local)
		count := selection.count
		// An `ls`-derived listing counts approximately, so it says so.
		format := m.SFTPItemsFmt
		if !local && w.view.approximate {
			format = m.SCPItemsFmt
		}
		status := fmt.Sprintf(format, len(folder.rows))
		if folder.filterInputEditor().String() != "" {
			status = fmt.Sprintf(m.Tools.FilesFilterCountFmt, len(folder.rows), sftpVisibleRowCount(folder))
			if !local && w.view.approximate {
				status += " · " + fmt.Sprintf(m.SCPItemsFmt, len(folder.rows))
			}
		}
		if count > 0 {
			status += " · " + fmt.Sprintf(m.SFTPWindow.SelectedFmt, count)
		}
		if !compact {
			ui.Text(c, status).FontSize(theme.Metrics.FontSizeS).Color(theme.MutedForeground).Ellipsis().LineHeight(20)
		}
		if !compact {
			ui.Row(c).Height(28).Gap(2).Items(ui.Center).Children(func() {
				if count == 0 {
					return
				}
				for _, action := range w.selectionActions(local) {
					icon(sftpWindowActions[sideIndex(local)].At(action.index), action.glyph, action.label, w.browse.pending != nil || w.view.busy && !local || !local && !w.connected())
					if sftpWindowActionIDs[action.index] == hitSFTPDelete {
						s.buttons[s.buttonCount-1].style = iconButtonDanger
					}
				}
			})
			if bounds.Height >= 320 {
				if count == 1 {
					ui.Text(c, w.owner.sftpDetailsLine(w.view, folder.rows[selection.index].entry)).FontSize(metrics.FontSizeS).Color(theme.MutedForeground).Ellipsis().LineHeight(18)
				} else {
					ui.Slot(c).Height(18)
				}
			}
		}
	})
}

// declareListRow declares one entry as a List row: its type icon, its name,
// and, when the columns are on, its size and modification time. A selected row
// is tinted, the pointer lightens one, and the keyboard's row is ringed. The
// List owns the choice and the hover identity; the window only reads them.
func (w *sftpWindow) declareListRow(row ui.ListRow, side int, paneWidth, rowWidth float32) {
	folder := w.folder(side == 0)
	if row.Index < 0 || row.Index >= len(folder.rows) {
		return
	}
	if folder.listRowsScope != row.ID.Scope() {
		folder.listRowsScope = row.ID.Scope()
	}
	sftpWindowListRows[side] = row.ID.Scope()
	entry := &folder.rows[row.Index]
	pal := w.owner.pal
	background := pal.workspace
	if row.Selected() {
		background = pal.accentSoft
	} else if row.C.Frame().Canvas().Hovered() == row.ID {
		background = pal.surfaceHot
	}
	color := pal.text
	if entry.entry.LinkBroken {
		color = pal.danger
	}
	tint := gfx.Color{Alpha: 1}
	if entry.mono {
		tint = pal.muted
	}
	box := row.Element.Height(sftpRowHeight).Shrink(0).Width(rowWidth).Gap(8).PadX(8).Items(ui.Center).Background(background).Radius(4)
	if row.Focused() {
		box = box.Border(1, pal.accent)
	}
	box.Children(func() {
		f := row.C.Frame()
		declareSFTPRowIcon(f, entry, tint)
		f.Text(entry.entry.Name).Color(color).Ellipsis().Grow(1)
		for _, column := range w.owner.sftpColumns.entries[1:] {
			if !column.Enabled {
				continue
			}
			if column.Key == sftpColumnSize {
				f.Text(entry.cells[2]).FontSize(metrics.FontSizeS).Color(pal.muted).Width(62).TextEnd()
			}
			if column.Key == sftpColumnModified {
				date := entry.cells[3]
				if paneWidth < 420 && len(date) >= 16 {
					date = date[5:10]
				}
				f.Text(date).FontSize(metrics.FontSizeS).Color(pal.muted).Width(w.dateWidth(paneWidth))
			}
		}
	})
}

func (w *sftpWindow) dateWidth(width float32) float32 {
	if width < 420 {
		return 76
	}
	return 118
}

func (w *sftpWindow) queueCount() int {
	count := 0
	for i := range w.owner.transfers.entries {
		e := &w.owner.transfers.entries[i]
		if e.owner == w.view && !e.removed {
			count++
		}
	}
	return count
}

func (w *sftpWindow) paintQueue(canvas *gfx.Canvas, bounds gfx.Rect) {
	theme, m := w.owner.Theme(), w.owner.messages
	canvas.Label(m.SFTPTransferQueue, gfx.Rect{X: bounds.X + 4, Y: bounds.Y, Width: bounds.Width - 8, Height: 20}, gfx.TextStyle{Size: theme.Metrics.FontSizeS, Weight: gfx.WeightSemibold, Color: theme.Foreground})
	w.ui.queueBounds = gfx.Rect{X: bounds.X, Y: bounds.Y + 24, Width: bounds.Width, Height: max(bounds.Height-24, 0)}
	area := components.BeginScrollArea(canvas, theme, w.ui.queueBounds, gfx.Size{Height: float32(w.queueCount()) * transferCardHeight}).Offset(0, w.ui.queueOffset).VerticalScrollbar(hitSFTPWindowQueueBar, m.SFTPTransferQueue, "")
	w.ui.queueBar = area.BeginContent()
	w.ui.queueOffset = w.ui.queueBar.OffsetY
	row := 0
	for pass := 0; pass < 2; pass++ {
		for i := len(w.owner.transfers.entries) - 1; i >= 0; i-- {
			e := &w.owner.transfers.entries[i]
			if e.owner != w.view || e.removed || (pass == 0) != transferActive(e.snapshot) {
				continue
			}
			box := gfx.Rect{X: bounds.X, Y: w.ui.queueBounds.Y + float32(row)*transferCardHeight - w.ui.queueOffset, Width: bounds.Width - 12, Height: transferCardHeight - 8}
			index := row
			row++
			if box.Y+box.Height < w.ui.queueBounds.Y || box.Y > w.ui.queueBounds.Y+w.ui.queueBounds.Height {
				continue
			}
			canvas.HitWithOptions(sftpWindowQueueRows.At(index), box, e.title, gfx.HitOptions{Role: gfx.RoleGroup, SkipFocus: true, Hint: e.paths})
			w.owner.paintTransferCard(canvas, box, e)
		}
	}
	area.EndContent()
}

func (w *sftpWindow) paintMenu(canvas *gfx.Canvas) {
	rows := w.owner.sftpMenuRows(&w.session, w.view)
	if selectedSFTPCount(w.view) == 0 {
		rows = menuRows{}
	}
	for i := range rows.entries[:rows.count] {
		entry := &rows.entries[i]
		if entry.separator {
			continue
		}
		item := &entry.item
		action := item.ID
		if mapped, ok := sftpMenuAction(action); ok {
			action = mapped
		}
		if action == hitSFTPCopyPath {
			item.ID = hitSFTPWindowMenuCopy
			continue
		}
		if index := sftpWindowActionIndex(action); index >= 0 {
			item.ID = sftpWindowMenuActions.At(index)
			if action != hitSFTPProperties {
				item.Disabled = w.browse.pending != nil || w.pendingLocal != "" || !w.activeLocal && (!w.connected() || w.view.busy)
			}
		}
	}
	theme := w.owner.contextMenuTheme()
	width, height := rows.size(canvas, theme)
	width = min(width, 480)
	menu := components.BeginContextMenu(canvas, theme, gfx.Rect{X: w.ui.menuAt.X, Y: w.ui.menuAt.Y, Width: width, Height: height}, components.ContextMenuOptions{Layer: popupLayer, Scope: hitSFTPWindowMenuDismiss, DismissID: hitSFTPWindowMenuDismiss, Viewport: gfx.Rect{Width: w.ui.size.Width, Height: w.ui.size.Height}, Scroll: &w.ui.menuScroll, RowHeight: contextMenuRow})
	rows.paint(&menu, nil)
	menu.End()
}

func (w *sftpWindow) paintDrag(canvas *gfx.Canvas) {
	local, target, row, valid := w.dropTarget(w.drag.at.X, w.drag.at.Y, w.drag.fromLocal)
	if !valid {
		return
	}
	bounds := w.ui.list[sideIndex(local)]
	if row >= 0 {
		bounds.Y += float32(row)*sftpRowHeight - w.ui.offset[sideIndex(local)]
		bounds.Height = sftpRowHeight
	}
	canvas.Clip(w.ui.list[sideIndex(local)])
	defer canvas.Unclip()
	label, glyph := w.owner.messages.SFTPWindow.DropDownloadHint, uint8(iconDownload)
	if w.drag.fromLocal {
		label, glyph = w.owner.messages.SFTPWindow.DropUploadHint, iconUpload
	}
	w.owner.paintSFTPTransferDrop(canvas, bounds, label, target, glyph)
}
