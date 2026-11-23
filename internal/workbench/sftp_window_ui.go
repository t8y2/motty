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
	sftpWindowPathEdit           = [2]gfx.ID{tree.ID(sftpWindowLocal, "path-edit"), tree.ID(sftpWindowRemote, "path-edit")}
	sftpWindowBreadcrumbs        = [2]gfx.Scope{tree.Sub(sftpWindowLocal, tree.Name("breadcrumbs")), tree.Sub(sftpWindowRemote, tree.Name("breadcrumbs"))}
	sftpWindowList               = [2]gfx.ID{tree.ID(sftpWindowLocal, "list"), tree.ID(sftpWindowRemote, "list")}
	sftpWindowSortHits           = [2]gfx.Scope{tree.Sub(sftpWindowLocal, tree.Name("sort")), tree.Sub(sftpWindowRemote, tree.Name("sort"))}
	sftpWindowActions            = [2]gfx.Scope{tree.Sub(sftpWindowLocal, tree.Name("actions")), tree.Sub(sftpWindowRemote, tree.Name("actions"))}
	sftpWindowMenuActions        = tree.Sub(sftpWindowHits, tree.Name("menu-actions"))
	sftpWindowUp                 = [2]gfx.ID{tree.ID(sftpWindowLocal, "up"), tree.ID(sftpWindowRemote, "up")}
	sftpWindowRefresh            = [2]gfx.ID{tree.ID(sftpWindowLocal, "refresh"), tree.ID(sftpWindowRemote, "refresh")}
	sftpWindowNewFolder          = [2]gfx.ID{tree.ID(sftpWindowLocal, "new-folder"), tree.ID(sftpWindowRemote, "new-folder")}
	hitSFTPWindowSplit           = tree.ID(sftpWindowHits, "split")
	hitSFTPWindowUpload          = tree.ID(sftpWindowHits, "upload")
	hitSFTPWindowDownload        = tree.ID(sftpWindowHits, "download")
	hitSFTPWindowCloseCancel     = tree.ID(sftpWindowHits, "close-cancel")
	hitSFTPWindowCloseConfirm    = tree.ID(sftpWindowHits, "close-confirm")
	hitSFTPWindowExternalReplace = tree.ID(sftpWindowHits, "external-replace")
	hitSFTPWindowExternalKeep    = tree.ID(sftpWindowHits, "external-keep")
	hitSFTPWindowMenuDismiss     = tree.ID(sftpWindowHits, "menu-dismiss")
	hitSFTPWindowMenuCopy        = tree.ID(sftpWindowHits, "menu-copy")
	sftpWindowQueueActions       = tree.Sub(sftpWindowHits, tree.Name("queue-actions"))
	sftpWindowQueueRows          = tree.Sub(sftpWindowHits, tree.Name("queue-rows"))
	sftpWindowQueueStatus        = tree.Sub(sftpWindowHits, tree.Name("queue-status"))
	hitSFTPQueueDetails          = tree.ID(sftpWindowHits, "queue-details")
	hitSFTPQueueDetailsDismiss   = tree.ID(sftpWindowHits, "queue-details-dismiss")
	hitSFTPQueueReplaceAll       = tree.ID(sftpWindowHits, "queue-replace-all")
	hitSFTPQueueSkipAll          = tree.ID(sftpWindowHits, "queue-skip-all")
	hitSFTPQueueReplaceConfirm   = tree.ID(sftpWindowHits, "queue-replace-confirm")
	hitSFTPQueueReplaceCancel    = tree.ID(sftpWindowHits, "queue-replace-cancel")
)

type sftpWindowUI struct {
	// root names this window's page in the identity tree: every window is a
	// page of its own, and no two windows may name their controls alike.
	root       string
	windowPage *ui.Page
	// panes keeps what each pane's declaration leaves for the details painted
	// after the page: the panes are keyed containers of the window page now, not
	// pages of their own. body and queue are the rectangles the page laid out
	// for the split and the transfer queue.
	panes   [2]sftpWindowScratch
	body    gfx.Rect
	queue   gfx.Rect
	connect sftpWindowConnectScratch
	size    gfx.Size
	// bounds is the rectangle the view occupies in its canvas: the whole window
	// for a native one, the tab's rectangle when hosted in the main window.
	bounds       gfx.Rect
	list         [2]gfx.Rect
	offset       [2]float32
	offsetSynced [2]float32
	typeahead    [2]components.MenuTypeahead
	treeBarDrag  int // 1: local tree, 2: remote tree; zero when idle
	treeBarGrab  float32
	// columnDragging, columnDrag and the fit fields are the file lists' column
	// dividers. The area and widths the panes last painted are kept for the
	// pointer, which has no canvas of its own.
	columnDragging   [2]bool
	columnDrag       [2]int
	columnFitPending [2]bool
	columnFitDivider [2]int
	columnArea       [2]gfx.Rect
	columnWidths     [2][3]float32
	// split is the divider between the panes. It holds the position in pixels and
	// keeps the share across a window resize; the page declares it as ui.Splitter.
	split             ui.SplitterState
	tooltip           components.HoverTooltip
	menuOpen          bool
	menuAt            gfx.Point
	queueBounds       gfx.Rect
	queueScroll       *ui.ScrollState
	queueEntries      []int
	queueRevision     uint64
	queueConflicts    int
	queueBatch        transferBatch
	queueDetails      transferDetails
	sort              [2]sftpWindowSort
	headerAnchor      [2]ui.Box
	selection         [2]sftpWindowSelection
	profileQuickOpen  bool
	profileQuickFocus bool
	profileQuickBox   ui.Box
}

// sftpWindowScratch is the room one pane's declaration leaves for what is
// painted by hand after the page has been laid out. Every pane keeps its own, so
// the details painted after the page read the slots of the side they belong to.
type sftpWindowScratch struct {
	// bounds is the pane's rectangle as the page sees it: a pane declared in a
	// page is not drawn in a page of its own, so it reads its size from here.
	bounds  gfx.Rect
	local   bool
	header  ui.Box
	address ui.Box
	// addressWidth is the width the path got in the last paint: the path folds
	// against the room it actually has, as the painted slot was measured.
	addressWidth float32
	list         ui.Box
	tree         ui.Box
	treeSet      bool
	filter       ui.Box
	sudoMode     ui.Box
	buttons      [20]sftpIconButton
	buttonCount  int
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

// windowPage is the page that declares the window's own chrome, its connecting
// state and its two panes. Config takes it as the standalone window's page.
func (w *sftpWindow) windowPage() *ui.Page {
	if w.ui.windowPage == nil {
		w.ui.windowPage = ui.NewPage(ui.PageConfig{Tree: &tree, Root: w.pageRoot(), Theme: &w.owner.theme, View: w.declareWindow})
	}
	return w.ui.windowPage
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
	w.ui.bounds = w.windowBodyBounds()
	theme := w.owner.Theme()
	canvas.Fill(gfx.Rect{Width: size.Width, Height: size.Height}, theme.Background)
	// The event pass a connecting control delivers may start the browser while
	// the page declares; this paint still shows what it declared.
	connecting := w.view.worker == nil
	// The page declares the panes and keeps their slots here; the details painted
	// after it read the side they belong to.
	w.ui.panes = [2]sftpWindowScratch{}
	w.ui.body, w.ui.queue = gfx.Rect{}, gfx.Rect{}
	w.windowPage().Draw(canvas, gfx.Rect{Width: size.Width, Height: size.Height}, w.declareWindow)
	if connecting {
		w.paintConnectingDetails(canvas)
		return
	}
	firstLocal := w.owner.sftpWindowOrder == sftpLocalLeft
	if s := &w.ui.panes[sideIndex(firstLocal)]; s.list.Valid() {
		w.paintPaneDetails(canvas, s, firstLocal)
	}
	if s := &w.ui.panes[sideIndex(!firstLocal)]; s.list.Valid() {
		w.paintPaneDetails(canvas, s, !firstLocal)
	}
	for side := range w.ui.panes {
		header := w.ui.panes[side].header
		if !header.Valid() {
			continue
		}
		rect := header.Rect()
		if rect.Width <= 16 || rect.Height <= 0 {
			continue
		}
		w.ui.columnArea[side] = gfx.Rect{X: rect.X + 8, Y: rect.Y, Width: rect.Width - 16, Height: rect.Height}
		if w.owner.sftpColumnResizeAvailable() {
			w.paintColumnDividers(canvas, side)
		}
	}
	w.paintQueue(canvas, w.ui.queue, w.ui.bounds)
	if w.drag.active {
		w.paintDrag(canvas)
	} else if w.foreign.active {
		w.paintForeignDrag(canvas)
	}
	w.notices.paint(canvas, theme, gfx.Rect{Width: size.Width, Height: size.Height})
	w.ui.tooltip.Paint(canvas, theme, gfx.Rect{Width: size.Width, Height: size.Height}, components.TooltipOptions{Layer: tooltipLayer, SideOffset: tooltipGap}, components.HoverTooltipDelay)
	if !w.pendingFocus.IsZero() {
		canvas.RequestFocus(w.pendingFocus)
		w.pendingFocus = gfx.ID{}
	}
}

// The window's body is a padded column: the panes take what is left, the transfer
// queue keeps a share at the bottom when it has entries, and the status line
// closes it. The main-window tab lays the same parts out at absolute rectangles,
// so both read the same metrics.
const (
	sftpWindowPad          = float32(12)
	sftpWindowGap          = float32(8)
	sftpWindowStatusHeight = float32(20)
)

// windowBodyBounds is the room below the window's titlebar strip: the bar takes
// TitlebarHeight, and everything the window shows is laid out inside it.
func (w *sftpWindow) windowBodyBounds() gfx.Rect {
	size := w.ui.size
	return gfx.Rect{Y: TitlebarHeight, Width: size.Width, Height: max(size.Height-TitlebarHeight, 0)}
}

// declareWindow declares the window's own chrome: its titlebar and, under it,
// the connecting state while there is no browser or else the two panes, the
// transfer queue and the status line.
func (w *sftpWindow) declareWindow(c ui.C) {
	ui.Column(c).Fill().Children(func() {
		declareWindowTitlebar(c, w.owner.pal, w.owner.messages.SFTPWindow.Title, w.session.Title+" · "+w.session.Address)
		w.declareWindowBody(c)
	})
}

// declareWindowBody declares what the window shows under its bar. The panes are
// keyed containers of this page, laid out at the split's panel rectangles below
// the bar exactly as the main-window tab lays them out in its own rectangle.
func (w *sftpWindow) declareWindowBody(c ui.C) {
	if w.view.worker == nil {
		w.ui.connect = sftpWindowConnectScratch{}
		w.declareConnecting(c)
		return
	}
	size := w.ui.size
	bounds := w.windowBodyBounds()
	queue := w.queueCount()
	body, queueRect, status := sftpTabPaneRects(bounds, queue, w.ui.queueConflicts)
	w.ui.body, w.ui.queue = body, queueRect
	firstLocal := w.owner.sftpWindowOrder == sftpLocalLeft
	ui.Column(c).Grow(1).Shrink(1).Children(func() {
		w.declarePanes(c, body, firstLocal, false)
		ui.Column(c).Absolute().
			Left(status.X - bounds.X).Top(status.Y - bounds.Y).Width(status.Width).Height(status.Height).
			Children(func() { w.declareStatusLine(c) })
	})
	if queue > 0 {
		w.declareQueue(c, queueRect, gfx.Rect{})
	}
	if w.bookmark.open {
		w.declareBookmarkDialog(c, gfx.Rect{Width: size.Width, Height: size.Height})
	}
	if w.view.dialog != sftpDialogNone {
		w.owner.declareSFTPDialog(c, gfx.Rect{Width: size.Width, Height: size.Height}, w.view)
	}
	w.owner.declareSFTPFileProperties(c, gfx.Rect{Width: size.Width, Height: size.Height}, &w.fileProperties)
	w.declareConfirmations(c)
	w.declarePlacesMenu(c)
	w.declareWindowMenu(c)
	w.declareHeaderMenus(c, &w.ui.headerAnchor)
	w.declareProfileQuickMenu(c)
}

// sftpWindowSplitExtent is the room ui.Splitter's divider takes between the panes.
// The page declares it one pixel thick, as the painted separator was; the panes
// are worked out before the layout so a first paint knows the room it has.
const sftpWindowSplitExtent = float32(1)

// paneSplitWidth is the width the first pane takes in a body of the given width.
// The share the divider keeps is used while it has one, so a window resize keeps
// the proportion; otherwise the position in pixels is kept inside the limits.
func (w *sftpWindow) paneSplitWidth(bodyWidth float32) float32 {
	least := float32(220)
	most := max(bodyWidth-sftpWindowSplitExtent-least, least)
	width := w.ui.split.Pos
	if w.ui.split.Ratio > 0 {
		width = w.ui.split.Ratio * (bodyWidth - sftpWindowSplitExtent)
	}
	if width <= 0 {
		width = max(min((bodyWidth-sftpWindowSplitExtent)/2, most), least)
	}
	return min(max(width, least), most)
}

// declarePanes declares the two panes and the splitter between them as a row.
func (w *sftpWindow) declarePanes(c ui.C, body gfx.Rect, firstLocal bool, parts bool) {
	width := w.paneSplitWidth(body.Width)
	w.ui.split.Pos = width
	secondWidth := max(body.Width-width-sftpWindowSplitExtent, 0)
	ui.Column(c).Height(body.Height + 2*sftpWindowPad).Shrink(0).Pad(sftpWindowPad).Children(func() {
		ui.Row(c).Grow(1).Shrink(1).Children(func() {
			w.declarePaneSide(c, gfx.Rect{Width: width, Height: body.Height}, firstLocal, parts)
			ui.Splitter(c, &w.ui.split).SplitterThickness(sftpWindowSplitExtent).Mins(220, 220).Label(w.owner.messages.SFTPWindow.Title).
				Hit(hitSFTPWindowSplit, w.owner.messages.SFTPWindow.Title)
			w.declarePaneSide(c, gfx.Rect{Width: secondWidth, Height: body.Height}, !firstLocal, parts)
		})
	})
}

// declareConfirmations declares the window's alert dialogs: the question before a
// close with transfers running, and the choice an external edit of an open file
// raises. They are page dialogs, so their buttons are the application's identities.
func (w *sftpWindow) declareConfirmations(c ui.C) {
	m := w.owner.messages
	if w.closeConfirm {
		dialog := ui.Dialog(c, ui.DialogSpec{
			Title: m.SFTPWindow.CloseTitle, Description: w.closeDescription(), Width: 512,
			Layer: dialogLayer + 1, Scope: dialogScope, CloseID: hitSFTPWindowCloseCancel,
			InitialFocusID: hitSFTPWindowCloseCancel, NoClose: true,
		})
		dialog.Footer(func() {
			w.owner.dialogActionFooter(c,
				components.ButtonAction{ID: hitSFTPWindowCloseCancel, Label: m.Cancel, Variant: components.ButtonOutline},
				components.ButtonAction{ID: hitSFTPWindowCloseConfirm, Label: m.SFTPWindow.CloseTransfers, Variant: components.ButtonDestructive},
			)
		})
	}
	if w.externalConflict != 0 && !w.closeConfirm && w.view.dialog == sftpDialogNone {
		if edit := w.owner.editByID(w.externalConflict); edit != nil {
			tools := m.Tools
			dialog := ui.Dialog(c, ui.DialogSpec{
				Title: tools.ExtEditConflictTitle, Description: fmt.Sprintf(tools.ExtEditConflictDescriptionFmt, edit.name), Width: 512,
				Layer: dialogLayer + 1, Scope: dialogScope, CloseID: hitSFTPWindowExternalKeep,
				InitialFocusID: hitSFTPWindowExternalKeep, NoClose: true,
			})
			dialog.Footer(func() {
				w.owner.dialogActionFooter(c,
					components.ButtonAction{ID: hitSFTPWindowExternalKeep, Label: m.Cancel, Variant: components.ButtonOutline},
					components.ButtonAction{ID: hitSFTPWindowExternalReplace, Label: tools.ExtEditReplace, Variant: components.ButtonDestructive},
				)
			})
		}
	}
}

// declarePaneSide declares one pane as a keyed container of the window page and
// keeps the room its declaration leaves for the details the page does not paint.
// The pane is laid out by the row at the width it was given, so its declaration
// reads the room from there.
func (w *sftpWindow) declarePaneSide(c ui.C, bounds gfx.Rect, local bool, part bool) {
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return
	}
	side := sideIndex(local)
	s := &w.ui.panes[side]
	*s = sftpWindowScratch{local: local, bounds: bounds}
	if part {
		name := fileTabRemotePart
		if local {
			name = fileTabLocalPart
		}
		c.Part(name)
	}
	ui.Column(c.KeyInt(side)).Width(bounds.Width).Height(bounds.Height).Shrink(0).
		Children(func() { w.declarePane(c, s) })
}

// declareStatusLine declares what the window says under the panes: the roots a
// browse-synchronized pair shows, else its status, error or hint.
func (w *sftpWindow) declareStatusLine(c ui.C) {
	m := w.owner.messages
	theme := w.owner.Theme()
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
		ui.Row(c).Height(sftpWindowStatusHeight).Gap(6).Items(ui.Center).Children(func() {
			if w.view.busy || w.view.localBusy {
				ui.Spinner(c).Size(16, 16).Shrink(0)
			}
			ui.Icon(c, lucideIcons[iconSFTPBrowseSync]).Size(14, 14).Color(w.owner.pal.success)
			ui.Text(c, m.SFTPWindow.BrowseRootLabel).FontSize(theme.Metrics.FontSizeS).Color(theme.Foreground)
			ui.Text(c, w.browse.roots[0]).FontSize(theme.Metrics.FontSizeS).Color(theme.MutedForeground).Ellipsis().Grow(1)
			ui.Icon(c, lucideIcons[iconForward]).Size(12, 12).Color(theme.MutedForeground)
			ui.Text(c, w.browse.roots[1]).FontSize(theme.Metrics.FontSizeS).Color(theme.MutedForeground).Ellipsis().Grow(1)
			if status != "" && status != w.browseDescription() {
				ui.Text(c, status).FontSize(theme.Metrics.FontSizeS).Color(theme.MutedForeground).Ellipsis().Grow(1)
			}
		})
		return
	}
	ui.Row(c).Height(sftpWindowStatusHeight).Gap(6).Items(ui.Center).Children(func() {
		if w.view.busy || w.view.localBusy {
			ui.Spinner(c).Size(16, 16).Shrink(0)
		}
		ui.Text(c, status).FontSize(theme.Metrics.FontSizeS).Color(theme.MutedForeground).Ellipsis().Grow(1).LineHeight(sftpWindowStatusHeight)
	})
}

func (w *sftpWindow) canTransfer(local bool) bool {
	if !w.connected() || w.view.worker == nil || w.navigationBusy() || w.bookmark.open || w.fileProperties.open || w.view.dialog != sftpDialogNone || w.closeConfirm || w.externalConflict != 0 || w.view.path == "" || w.view.lfolder.path == "" {
		return false
	}
	return w.selection(local).count > 0
}

// paintPaneDetails paints what a pane's view lays out but does not paint itself:
// the icon buttons, the path and filter, the directory tree, the list's scroll
// bar and the empty message. It runs after the view's elements, with the room the
// declaration kept in s.
func (w *sftpWindow) paintPaneDetails(canvas *gfx.Canvas, s *sftpWindowScratch, local bool) {
	side := sideIndex(local)
	folder := w.folder(local)
	theme, m := w.owner.Theme(), w.owner.messages
	if !local {
		w.view.places.anchors[1] = s.address.Rect()
	} else {
		w.view.places.anchors[0] = s.address.Rect()
	}
	if path := s.address.Rect(); path.Width > 0 {
		s.addressWidth = path.Width
	}
	listBounds := s.list.Rect()
	w.ui.list[side] = listBounds
	if !s.treeSet {
		w.directoryTree[side].visible = w.directoryTree[side].visible[:0]
	}
	area := s.list.ScrollState()
	if area != nil {
		if w.ui.offset[side] != w.ui.offsetSynced[side] {
			// The model (history restore, a filter, a sort, a test) asked for an
			// offset by writing it; hand it to the List.
			area.ScrollTo(w.ui.offset[side])
		}
		folder.listScroll = area
		folder.scrollOffset = area.Offset
		w.ui.offset[side] = area.Offset
		w.ui.offsetSynced[side] = area.Offset
		folder.first = max(0, int(area.Offset/sftpRowHeight))
	}
	folder.visibleRows = max(1, int(listBounds.Height/sftpRowHeight))
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
}

// declareSudoToggle declares the remote pane's sudo toggle as a page control. It is
// a two-state button: the window's click handler flips the browser's sudo mode under
// the identity it has always had.
func (w *sftpWindow) declareSudoToggle(c ui.C) ui.Box {
	theme, m := w.owner.Theme(), w.owner.messages
	label := m.SFTPModeToggle
	toggle := ui.ToggleBase(c, w.owner.pageFlag(w.view.sudo), label).
		Hit(hitSFTPWindowSudoMode, label).Width(64).Height(28).Shrink(0).
		PadX(8).Gap(4).Items(ui.Center).Radius(theme.Metrics.RadiusControl).Border(1, theme.Input)
	if w.view.sudo {
		toggle = toggle.Background(theme.Accent).Border(1, theme.Primary)
	}
	toggle.Children(func() {
		ui.Text(c, label).FontSize(theme.Metrics.FontSizeS).Color(theme.Foreground).Ellipsis()
	})
	return toggle
}

// declarePane declares one side of the window: its title and actions, the path,
// the filter, the column headers and the room the list takes. Its slots go into
// the side's own scratch, so the details painted after the page read the right
// side even when the panes swap places.
func (w *sftpWindow) declarePane(c ui.C, s *sftpWindowScratch) {
	local := s.local
	side := sideIndex(local)
	folder := w.folder(local)
	theme, m := w.owner.Theme(), w.owner.messages
	bounds := s.bounds
	compact := bounds.Height < 230
	resizable := w.owner.sftpColumnResizeAvailable()
	var columns sftpColumnMetrics
	if resizable {
		columnAreaWidth := max(bounds.Width-32, 0)
		columns = w.owner.sftpColumnMetrics(columnAreaWidth, sftpColumnNameGap)
		if w.ui.columnFitPending[side] {
			fit := w.owner.sftpColumnFitWidth(folder, c.Canvas(), w.ui.columnFitDivider[side])
			columns = w.owner.applySFTPColumnFit(columns, w.ui.columnFitDivider[side], fit)
			w.ui.columnFitPending[side] = false
		}
		w.ui.columnWidths[side] = [3]float32{columns.name, columns.size, columns.modified}
	}
	icon := func(id gfx.ID, glyph uint8, hint string, disabled bool, style iconButtonStyle) {
		s.buttons[s.buttonCount] = sftpIconButton{box: w.owner.declareIconButton(c, id, glyph, hint, disabled, style).Size(28, 28), id: id}
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
				s.sudoMode = w.declareSudoToggle(c)
			}
			treeHint := m.SFTPWindow.ShowDirectoryTree
			if w.directoryTree[sideIndex(local)].enabled {
				treeHint = m.SFTPWindow.HideDirectoryTree
			}
			treeDisabled := directoryTreeToggleDisabled(w.directoryTree[sideIndex(local)].enabled, max(bounds.Width-16, 0))
			if treeDisabled {
				treeHint = m.SFTPWindow.DirectoryTreeNeedsWidth
			}
			treeStyle := iconButtonQuiet
			if w.directoryTree[sideIndex(local)].enabled {
				treeStyle = iconButtonActive
			}
			icon(sftpWindowTreeToggle[sideIndex(local)], iconDirectoryTree, treeHint, treeDisabled, treeStyle)
			if !local && bounds.Width >= 420 {
				icon(hitSFTPWindowUpload, iconUpload, m.SFTPWindow.Upload, !w.canTransfer(true), iconButtonQuiet)
				icon(hitSFTPWindowDownload, iconDownload, m.SFTPWindow.Download, !w.canTransfer(false), iconButtonQuiet)
			}
			if local || bounds.Width >= 340 {
				icon(sftpWindowUp[sideIndex(local)], iconArrowUp, m.SFTPUp, folder.path == "" || !local && (w.view.busy || !w.connected()), iconButtonQuiet)
				icon(sftpWindowRefresh[sideIndex(local)], iconRefresh, m.SFTPRefresh, !local && (w.view.busy || !w.connected()), iconButtonQuiet)
			}
			if folder.showHidden {
				icon(sftpWindowHidden[sideIndex(local)], iconEye, m.Tools.FilesHideHidden, false, iconButtonActive)
			}
			if local {
				icon(hitSFTPLocationProfiles, iconSFTPProfiles, m.SFTPWindow.BookmarkTitle, w.browse.pending != nil, iconButtonQuiet)
				icon(hitSFTPProfileQuick, iconChevronDown, m.SFTPWindow.ProfileChoose, w.browse.pending != nil || w.visibleProfileCount() == 0, iconButtonQuiet)
				hint := m.SFTPWindow.BrowseSync
				if w.browse.enabled {
					hint = w.browseDescription()
				}
				browseStyle := iconButtonQuiet
				if w.browse.enabled {
					browseStyle = iconButtonActive
				}
				icon(hitSFTPBrowseSync, iconSFTPBrowseSync, hint, !w.browse.enabled && (w.navigationBusy() || !w.connected()), browseStyle)
			}
			icon(sftpWindowMore[sideIndex(local)], iconEllipsis, m.Tools.FilesMore, false, iconButtonQuiet)
		})
		ui.Row(c).Height(32).Gap(2).Items(ui.Center).Children(func() {
			if folder.pathEditing && !w.owner.sftpPathInput && c.Canvas().Focused() != sftpWindowPath[side] {
				folder.pathEditing = false
				folder.resetPathInput()
			}
			// The path collapses against the room the crumbs actually get: the
			// pane's padding, the two side buttons and the row's gaps, as the
			// painted slot was measured.
			addressWidth := max(bounds.Width-2*8-2*sftpButton-2*2, 80)
			if s.addressWidth > 0 {
				addressWidth = s.addressWidth
			}
			s.address = w.owner.declareSFTPAddress(c, folder, sftpWindowPath[side], sftpWindowPathEdit[side], sftpWindowBreadcrumbs[side], addressWidth, !local && w.view.busy, folder.path).Height(32).Grow(1)
			listID, starID, listHint := hitSFTPBookmarkList, hitSFTPBookmarkToggle, m.SFTPWindow.DirectoryFavorites
			if local {
				listID, starID, listHint = hitSFTPLocalPlaces, hitSFTPLocalStar, m.SFTPWindow.LocalPlaces
			}
			icon(listID, iconChevronDown, listHint, false, iconButtonQuiet)
			star, hint := uint8(iconStar), m.SFTPWindow.FavoriteAdd
			if w.directoryFavorite(local) {
				star, hint = iconStarSolid, m.SFTPWindow.FavoriteRemove
			}
			icon(starID, star, hint, folder.path == "" || local && w.pendingLocal != "" || !local && (w.view.busy || !w.connected() || w.session.host.CatalogID == ""), iconButtonQuiet)
		})
		if folder.filterOpen {
			filter := w.owner.declareSFTPFilterField(c, folder, sftpWindowFilter[side], func() {
				w.clickFilterControl(sftpWindowFilterClear[side])
			})
			s.filter = filter.Height(32).Shrink(0)
			if filter.Changed() {
				w.applyFolderFilter(local)
			}
		}
		if local {
			w.owner.declareLocalCoverage(c, folder, hitSFTPWindowCoverage)
		}
		s.header = ui.Row(c).Height(20).Gap(8).PadX(8).Items(ui.Center)
		s.header.Children(func() {
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
				box := ui.Text(c, label).FontSize(theme.Metrics.FontSizeS).Color(theme.MutedForeground).Ellipsis().HitWith(sftpWindowSortHits[side].At(column), label, gfx.HitOptions{Role: gfx.RoleButton})
				if column == 0 {
					if resizable {
						box.Width(columns.name)
					} else {
						box.Grow(1)
					}
				} else if column == 1 {
					if resizable {
						box.Width(columns.size).TextEnd()
					} else {
						box.Width(62).TextEnd()
					}
				} else {
					if resizable {
						box.Width(columns.modified)
					} else {
						box.Width(w.dateWidth(bounds.Width))
					}
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
				s.tree = w.declareDirectoryTreePane(c, side, treeWidth)
				s.treeSet = s.tree.Valid()
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
					w.declareListRow(row, side, bounds.Width, rowWidth, columns)
				}).Grow(1).Label(m.SFTPFiles).AlwaysScrollbars().ScrollbarLabels(m.SFTPFiles, "")
				folder.seedSFTPListCurrent()
				if folder.list.Changed() {
					folder.takeSFTPListChoice()
					w.selectionChanged(local)
				}
				// Enter on a row opens it. The window page's List owns the
				// key now, so the submit is where the window learns of it; the
				// window's own key handler still takes Enter first only when
				// the List has not been painted yet.
				if row, submitted := folder.list.Submitted(); submitted && !w.listBlocked() {
					w.openEntry(local, row)
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
					style := iconButtonQuiet
					if sftpWindowActionIDs[action.index] == hitSFTPDelete {
						style = iconButtonDanger
					}
					icon(sftpWindowActions[sideIndex(local)].At(action.index), action.glyph, action.label, w.browse.pending != nil || w.view.busy && !local || local && w.view.localBusy || !local && !w.connected(), style)
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
	// The page needs the popup anchors before it paints, so they are taken here
	// rather than after the page has been laid out.
	for _, button := range s.buttons[:s.buttonCount] {
		switch button.id {
		case sftpWindowMore[side]:
			w.ui.headerAnchor[side] = button.box
		case hitSFTPProfileQuick:
			w.ui.profileQuickBox = button.box
		}
	}
}

// declareListRow declares one entry as a List row: its type icon, its name,
// and, when the columns are on, its size and modification time. A selected row
// is tinted, the pointer lightens one, and the keyboard's row is ringed. The
// List owns the choice and the hover identity; the window only reads them.
func (w *sftpWindow) declareListRow(row ui.ListRow, side int, paneWidth, rowWidth float32, columns sftpColumnMetrics) {
	folder := w.folder(side == 0)
	if row.Index < 0 || row.Index >= len(folder.rows) {
		return
	}
	if folder.listRowsScope != row.ID.Scope() {
		folder.listRowsScope = row.ID.Scope()
	}
	sftpWindowListRows[side] = row.ID.Scope()
	entry := &folder.rows[row.Index]
	c := row.C
	pal := w.owner.pal
	background := pal.workspace
	if row.Selected() {
		background = pal.accentSoft
	} else if c.Canvas().Hovered() == row.ID {
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
	sizeWidth, modifiedWidth := float32(62), w.dateWidth(paneWidth)
	resizable := w.owner.sftpColumnResizeAvailable()
	if resizable {
		sizeWidth, modifiedWidth = columns.size, columns.modified
	}
	box.Children(func() {
		w.owner.declareSFTPRowIcon(c, entry, tint)
		name := ui.Text(c, entry.entry.Name).Color(color).Ellipsis().Grow(1)
		// The row spends its padding, the icon and the gaps before the name text;
		// a name wider than what is left is the one the tooltip is for.
		available := rowWidth - 16 - sftpColumnIconWidth - 3*sftpColumnNameGap - sizeWidth - modifiedWidth
		if !resizable {
			available = rowWidth - 16 - sftpColumnIconWidth - sftpColumnNameGap
			for _, column := range w.owner.sftpColumns.entries[1:] {
				if column.Enabled {
					available -= sftpColumnNameGap
					if column.Key == sftpColumnSize {
						available -= sizeWidth
					} else if column.Key == sftpColumnModified {
						available -= modifiedWidth
					}
				}
			}
		}
		if available > 0 && c.Canvas().TextWidth(entry.entry.Name, metrics.FontSizeM) > available {
			name = name.HitWith(row.ID, entry.entry.Name, gfx.HitOptions{Hint: entry.entry.Name, SkipFocus: true})
		}
		for _, column := range w.owner.sftpColumns.entries[1:] {
			if !column.Enabled {
				continue
			}
			if column.Key == sftpColumnSize {
				ui.Text(c, entry.cells[2]).FontSize(metrics.FontSizeS).Color(pal.muted).Width(sizeWidth).TextEnd()
			}
			if column.Key == sftpColumnModified {
				date := entry.cells[3]
				if paneWidth < 420 && len(date) >= 16 {
					date = date[5:10]
				}
				ui.Text(c, date).FontSize(metrics.FontSizeS).Color(pal.muted).Width(modifiedWidth)
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

// windowMenuRows is the file list's right-click menu as page menu rows. The
// actions keep the identities the window's click handler dispatches.
func (w *sftpWindow) windowMenuRows() menuRows {
	rows := w.owner.sftpMenuRows(&w.session, w.view)
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
				item.Disabled = w.browse.pending != nil || w.pendingLocal != "" || w.activeLocal && w.view.localBusy || !w.activeLocal && (!w.connected() || w.view.busy)
			}
		}
	}
	return rows
}

// declarePlacesMenu declares the places menu under the address bar of the side
// that opened it.
func (w *sftpWindow) declarePlacesMenu(c ui.C) {
	if !w.view.bookmarksOpen {
		return
	}
	s := &w.ui.panes[sideIndex(w.view.local)]
	if !s.address.Valid() {
		return
	}
	w.placeBrowser().declareDirectoryPlaces(c, s.address)
}

// declareWindowMenu declares the file list's right-click menu, open at the point
// the press named.
func (w *sftpWindow) declareWindowMenu(c ui.C) {
	if !w.ui.menuOpen {
		return
	}
	rows := w.windowMenuRows()
	w.owner.declareRowsMenu(c, &w.ui.menuOpen, w.ui.menuAt, hitSFTPWindowMenuDismiss, &rows, func(id gfx.ID) { w.clickFileMenuAction(id) })
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

// paintForeignDrag paints the pane another window's file drag would land in.
func (w *sftpWindow) paintForeignDrag(canvas *gfx.Canvas) {
	side := sideIndex(w.foreign.local)
	if w.ui.list[side].Width <= 0 {
		return
	}
	bounds := w.ui.list[side]
	if w.foreign.row >= 0 {
		bounds.Y += float32(w.foreign.row)*sftpRowHeight - w.ui.offset[side]
		bounds.Height = sftpRowHeight
	}
	label, glyph := w.owner.messages.SFTPWindow.DropUploadHint, uint8(iconUpload)
	switch {
	case !w.foreign.fromLocal && w.foreign.local:
		label, glyph = w.owner.messages.SFTPWindow.DropDownloadHint, iconDownload
	case !w.foreign.fromLocal && !w.foreign.local:
		label, glyph = w.owner.messages.SFTPWindow.DropCopyHint, iconCopy
	}
	canvas.Clip(w.ui.list[side])
	defer canvas.Unclip()
	w.owner.paintSFTPTransferDrop(canvas, bounds, label, w.foreign.target, glyph)
}
