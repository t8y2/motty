package workbench

import (
	"fmt"
	"math"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

var sftpWindowHits = tree.Feature("sftp-window")
var (
	hitSFTPDetach                = tree.ID(sftpHits, "detach")
	sftpWindowSides              = tree.Sub(sftpWindowHits, tree.Name("sides"))
	sftpWindowLocal              = tree.Sub(sftpWindowSides, tree.Name("local"))
	sftpWindowRemote             = tree.Sub(sftpWindowSides, tree.Name("remote"))
	sftpWindowRows               = [2]gfx.Scope{tree.Sub(sftpWindowLocal, tree.Name("rows")), tree.Sub(sftpWindowRemote, tree.Name("rows"))}
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
	hitSFTPWindowConnectCancel   = tree.ID(sftpWindowHits, "connect-cancel")
	hitSFTPWindowConnectRetry    = tree.ID(sftpWindowHits, "connect-retry")
	hitSFTPWindowAuthenticate    = tree.ID(sftpWindowHits, "authenticate")
	sftpWindowQueueRows          = tree.Sub(sftpWindowHits, tree.Name("queue-rows"))
)

type sftpWindowUI struct {
	frame               ui.View
	panes               [2]ui.View
	rows                [2]ui.View
	size                gfx.Size
	list                [2]gfx.Rect
	offset              [2]float32
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
	var body, queue ui.Box
	f := w.ui.frame.Begin(canvas, theme, gfx.Rect{Width: size.Width, Height: size.Height})
	f.Column().Fill().Pad(12).Gap(8).Children(func() {
		body = f.Slot().Grow(1)
		if w.queueCount() > 0 {
			queue = f.Slot().Height(min(244, max(108, size.Height*.28))).Shrink(0)
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
			f.Row().Height(20).Gap(6).Items(ui.Center).Children(func() {
				f.Icon(lucideIcons[iconSFTPBrowseSync]).Size(14, 14).Color(w.owner.pal.success)
				f.Text(m.SFTPWindow.BrowseRootLabel).FontSize(theme.Metrics.FontSizeS).Color(theme.Foreground)
				f.Text(w.browse.roots[0]).FontSize(theme.Metrics.FontSizeS).Color(theme.MutedForeground).Ellipsis().Grow(1)
				f.Icon(lucideIcons[iconForward]).Size(12, 12).Color(theme.MutedForeground)
				f.Text(w.browse.roots[1]).FontSize(theme.Metrics.FontSizeS).Color(theme.MutedForeground).Ellipsis().Grow(1)
				if status != "" && status != w.browseDescription() {
					f.Text(status).FontSize(theme.Metrics.FontSizeS).Color(theme.MutedForeground).Ellipsis().Grow(1)
				}
			})
		} else {
			f.Text(status).FontSize(theme.Metrics.FontSizeS).Color(theme.MutedForeground).Ellipsis().LineHeight(20)
		}
	})
	f.End()
	w.ui.split = components.BeginResizable(canvas, theme, hitSFTPWindowSplit, body.Rect(), w.ui.fraction).LimitsPx(220, 220)
	panels := w.ui.split.Paint(m.SFTPWindow.Title)
	firstLocal := w.owner.sftpWindowOrder == sftpLocalLeft
	w.paintPane(canvas, panels.First, firstLocal)
	w.paintPane(canvas, panels.Second, !firstLocal)
	if w.queueCount() > 0 {
		w.paintQueue(canvas, queue.Rect())
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
	var address, list, filter ui.Box
	var sudoMode ui.Box
	compact := bounds.Height < 230
	var buttons [20]sftpIconButton
	buttonCount := 0
	f := w.ui.panes[side].Begin(canvas, theme, bounds)
	icon := func(id gfx.ID, glyph uint8, hint string, disabled bool) {
		buttons[buttonCount] = sftpIconButton{box: f.Slot().Size(28, 28), id: id, icon: glyph, hint: hint, disabled: disabled, style: iconButtonQuiet}
		buttonCount++
	}
	f.Column().Fill().Pad(8).Gap(6).Children(func() {
		f.Row().Height(28).Gap(4).Items(ui.Center).Children(func() {
			title := m.SFTPWindow.Remote
			if local {
				title = m.SFTPWindow.Local
			}
			f.Text(title).Bold().Color(theme.Foreground).Ellipsis().Grow(1)
			if !local && sudoSFTPAvailable(&w.session, w.view) {
				sudoMode = f.Slot().Width(64).Height(28).Shrink(0)
			}
			treeHint := m.SFTPWindow.ShowDirectoryTree
			if w.directoryTree[side].enabled {
				treeHint = m.SFTPWindow.HideDirectoryTree
			}
			treeDisabled := directoryTreeToggleDisabled(w.directoryTree[side].enabled, max(bounds.Width-16, 0))
			if treeDisabled {
				treeHint = m.SFTPWindow.DirectoryTreeNeedsWidth
			}
			icon(sftpWindowTreeToggle[side], iconDirectoryTree, treeHint, treeDisabled)
			if w.directoryTree[side].enabled {
				buttons[buttonCount-1].style = iconButtonActive
			}
			if !local && bounds.Width >= 420 {
				icon(hitSFTPWindowUpload, iconUpload, m.SFTPWindow.Upload, !w.canTransfer(true))
				icon(hitSFTPWindowDownload, iconDownload, m.SFTPWindow.Download, !w.canTransfer(false))
			}
			if local || bounds.Width >= 340 {
				icon(sftpWindowUp[side], iconArrowUp, m.SFTPUp, folder.path == "" || !local && (w.view.busy || !w.connected()))
				icon(sftpWindowRefresh[side], iconRefresh, m.SFTPRefresh, !local && (w.view.busy || !w.connected()))
			}
			if folder.showHidden {
				icon(sftpWindowHidden[side], iconEye, m.Tools.FilesHideHidden, false)
				buttons[buttonCount-1].style = iconButtonActive
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
					buttons[buttonCount-1].style = iconButtonActive
				}
			}
			icon(sftpWindowMore[side], iconEllipsis, m.Tools.FilesMore, false)
		})
		f.Row().Height(32).Gap(2).Items(ui.Center).Children(func() {
			address = f.Slot().Height(32).Grow(1)
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
			filter = f.Slot().Height(32).Shrink(0)
		}
		if local {
			w.owner.declareLocalCoverage(f, folder, hitSFTPWindowCoverage)
		}
		{
			f.Row().Height(20).Gap(8).PadX(8).Items(ui.Center).Children(func() {
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
					if w.ui.sort[side].column == column {
						if w.ui.sort[side].descending {
							label += " ↓"
						} else {
							label += " ↑"
						}
					}
					box := f.Text(label).FontSize(theme.Metrics.FontSizeS).Color(theme.MutedForeground).Ellipsis().HitWith(sftpWindowSortHits[side].At(column), label, gfx.HitOptions{Role: gfx.RoleButton})
					if column == 0 {
						box.Grow(1)
					} else if column == 1 {
						box.Width(62).TextEnd()
					} else {
						box.Width(w.dateWidth(bounds.Width))
					}
				}
			})
		}
		list = f.Slot().Grow(1)
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
			f.Text(status).FontSize(theme.Metrics.FontSizeS).Color(theme.MutedForeground).Ellipsis().LineHeight(20)
		}
		if !compact {
			f.Row().Height(28).Gap(2).Items(ui.Center).Children(func() {
				if count == 0 {
					return
				}
				for _, action := range w.selectionActions(local) {
					icon(sftpWindowActions[side].At(action.index), action.glyph, action.label, w.browse.pending != nil || w.view.busy && !local || !local && !w.connected())
					if sftpWindowActionIDs[action.index] == hitSFTPDelete {
						buttons[buttonCount-1].style = iconButtonDanger
					}
				}
			})
			if bounds.Height >= 320 {
				if count == 1 {
					f.Text(w.owner.sftpDetailsLine(w.view, folder.rows[selection.index].entry)).FontSize(metrics.FontSizeS).Color(theme.MutedForeground).Ellipsis().LineHeight(18)
				} else {
					f.Slot().Height(18)
				}
			}
		}
	})
	f.End()
	if !local && sudoSFTPAvailable(&w.session, w.view) {
		components.BeginToggle(canvas, theme, hitSFTPWindowSudoMode, sudoMode.Rect(), w.view.sudo).Small().Outline(m.SFTPModeToggle)
	}
	w.owner.paintSFTPIconButtons(canvas, buttons[:buttonCount])
	for _, button := range buttons[:buttonCount] {
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
		w.owner.paintSFTPFilterInput(canvas, filter.Rect(), folder, sftpWindowFilter[side], sftpWindowFilterClear[side])
	}
	if !local {
		w.view.places.anchors[1] = address.Rect()
	} else {
		w.view.places.anchors[0] = address.Rect()
	}
	value := folder.pathInput().String()
	components.PaintInput(canvas, theme, address.Rect(), components.InputSnapshot{ID: sftpWindowPath[side], Label: m.SFTPWindow.Path, Value: value, Hint: folder.path, Focused: canvas.Focused() == sftpWindowPath[side], PlatformEditing: canvas.Focused() == sftpWindowPath[side]})
	paneContent := list.Rect()
	frameBounds := paneContent
	if treeWidth, show := directoryTreePaneWidths(paneContent.Width); w.directoryTree[side].enabled && show {
		treeBounds := gfx.Rect{X: paneContent.X, Y: paneContent.Y, Width: treeWidth, Height: paneContent.Height}
		w.paintDirectoryTree(canvas, treeBounds, local)
		frameBounds = gfx.Rect{
			X:      treeBounds.X + treeBounds.Width + directoryTreeGap,
			Y:      paneContent.Y,
			Width:  max(paneContent.Width-treeBounds.Width-directoryTreeGap, 0),
			Height: paneContent.Height,
		}
	} else {
		w.directoryTree[side].bounds = gfx.Rect{}
	}
	canvas.RoundedBorder(frameBounds, 6, 1, theme.Border.Mix(theme.MutedForeground, .4), theme.Background)
	// Keep scrolling rows and scrollbars inside the frame, so neither can
	// paint over its top or bottom edge.
	rowsBounds := frameBounds.Inset(gfx.All(2))
	w.ui.list[side] = rowsBounds
	folder.visibleRows = max(1, int(rowsBounds.Height/sftpRowHeight))
	area := components.BeginScrollArea(canvas, theme, rowsBounds, gfx.Size{Width: rowsBounds.Width, Height: float32(len(folder.rows)) * sftpRowHeight}).Offset(0, w.ui.offset[side]).Viewport(sftpWindowList[side], m.SFTPFiles).VerticalScrollbar(sftpWindowBar[side], m.SFTPFiles, "")
	geometry := area.BeginContent()
	w.ui.bars[side] = geometry
	w.ui.offset[side] = geometry.OffsetY
	folder.first = int(geometry.OffsetY / sftpRowHeight)
	last := min(len(folder.rows), folder.first+int(math.Ceil(float64(rowsBounds.Height/sftpRowHeight)))+1)
	r := w.ui.rows[side].Begin(canvas, theme, gfx.Rect{X: rowsBounds.X, Y: rowsBounds.Y - float32(math.Mod(float64(geometry.OffsetY), float64(sftpRowHeight))), Width: max(rowsBounds.Width-10, 0), Height: float32(last-folder.first) * sftpRowHeight})
	r.Column().FillWidth().Children(func() {
		for index := folder.first; index < last; index++ {
			w.paintRow(r, &folder.rows[index], sftpWindowRows[side].At(index), bounds.Width)
		}
	})
	r.End()
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
		canvas.LabelCenter(label, rowsBounds, gfx.TextStyle{Size: theme.Metrics.FontSizeM, Color: theme.MutedForeground})
	}
	area.EndContent()
}

func (w *sftpWindow) paintRow(f *ui.Frame, row *sftpRow, id gfx.ID, paneWidth float32) {
	pal, canvas := w.owner.pal, f.Canvas()
	background := pal.workspace
	if row.selected {
		background = pal.accentSoft
	} else if canvas.Hovered() == id {
		background = pal.surfaceHot
	}
	color := pal.text
	if row.entry.LinkBroken {
		color = pal.danger
	}
	tint := gfx.Color{Alpha: 1}
	if row.mono {
		tint = pal.muted
	}
	box := f.Row().Height(sftpRowHeight).Shrink(0).Gap(8).PadX(8).Items(ui.Center).Background(background).Radius(4).HitWith(id, row.entry.Name, gfx.HitOptions{Role: gfx.RoleButton, Selected: row.selected, Hint: row.entry.Name})
	if canvas.FocusVisible() == id {
		box.Border(1, pal.accent)
	}
	box.Children(func() {
		declareSFTPRowIcon(f, row, tint)
		f.Text(row.entry.Name).Color(color).Ellipsis().Grow(1)
		for _, entry := range w.owner.sftpColumns.entries[1:] {
			if !entry.Enabled {
				continue
			}
			if entry.Key == sftpColumnSize {
				f.Text(row.cells[2]).FontSize(metrics.FontSizeS).Color(pal.muted).Width(62).TextEnd()
			}
			if entry.Key == sftpColumnModified {
				date := row.cells[3]
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
