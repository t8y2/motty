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
	// root names this window's page in the identity tree: every window is a
	// page of its own, and no two windows may name their controls alike.
	root       string
	windowPage *ui.Page
	// bookmarkBodyDraw and bookmarkFooterDraw paint the bookmark editor's
	// hand-painted controls into the slots its dialog declares. They are made
	// once so a warm repaint allocates nothing.
	bookmarkBodyDraw   func(*ui.DrawContext)
	bookmarkFooterDraw func(*ui.DrawContext)
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
	bars         [2]components.ScrollAreaGeometry
	barDrag      int // 1: local, 2: remote, 3: queue; zero when idle
	barGrab      float32
	treeBarDrag  int // 1: local tree, 2: remote tree; zero when idle
	treeBarGrab  float32
	fraction     float32
	split        components.Resizable
	splitting    bool
	tooltip      components.HoverTooltip
	menuOpen     bool
	menuAt       gfx.Point
	queueBounds  gfx.Rect
	queueOffset  float32
	queueBar     components.ScrollAreaGeometry
	// The queue's cards are page elements now; these are the entries drawn and
	// the scratch their hand-painted parts go into.
	queueItems          int
	queueVisible        []*transferEntry
	queueCards          []transferCard
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
	// tabPanes, tabBody and tabQueue are what the main-window tab declared: the
	// page owns the tab's view, so its per-side room and the split are kept here
	// between the declaration and the details painted after it.
	tabPanes [2]sftpWindowScratch
	tabBody  gfx.Rect
	tabQueue gfx.Rect
}

// sftpWindowScratch is the room one pane's declaration leaves for what is
// painted by hand after the page has been laid out. Every pane keeps its own, so
// the details painted after the page read the slots of the side they belong to.
type sftpWindowScratch struct {
	// bounds is the pane's rectangle as the page sees it: a pane declared in a
	// page is not drawn in a page of its own, so it reads its size from here.
	bounds      gfx.Rect
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
	w.ui.bounds = gfx.Rect{Width: size.Width, Height: size.Height}
	theme, m := w.owner.Theme(), w.owner.messages
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
	w.ui.split = components.BeginResizable(canvas, theme, hitSFTPWindowSplit, w.ui.body, w.ui.fraction).LimitsPx(220, 220)
	w.ui.split.Paint(m.SFTPWindow.Title)
	firstLocal := w.owner.sftpWindowOrder == sftpLocalLeft
	if s := &w.ui.panes[sideIndex(firstLocal)]; s.list.Valid() {
		w.paintPaneDetails(canvas, s, firstLocal)
	}
	if s := &w.ui.panes[sideIndex(!firstLocal)]; s.list.Valid() {
		w.paintPaneDetails(canvas, s, !firstLocal)
	}
	if w.queueCount() > 0 {
		w.paintQueueDetails(canvas, w.ui.queue)
	} else {
		w.ui.queueBounds = gfx.Rect{}
		w.ui.queueOffset = 0
	}
	if w.view.bookmarksOpen {
		w.paintDirectoryPlaces(canvas)
	}
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
			w.paintHeaderMenu(canvas, side, gfx.Rect{Width: size.Width, Height: size.Height}, &w.ui.headerScroll)
		}
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

// The window's body is a padded column: the panes take what is left, the transfer
// queue keeps a share at the bottom when it has entries, and the status line
// closes it. The main-window tab lays the same parts out at absolute rectangles,
// so both read the same metrics.
const (
	sftpWindowPad          = float32(12)
	sftpWindowGap          = float32(8)
	sftpWindowStatusHeight = float32(20)
)

func sftpQueueHeight(size gfx.Size) float32 {
	return min(244, max(108, size.Height*.28))
}

// bookmarkDraws makes the handlers that paint the bookmark editor's fields,
// buttons and lists, once.
func (w *sftpWindow) bookmarkDraws() {
	if w.ui.bookmarkBodyDraw == nil {
		w.ui.bookmarkBodyDraw = w.paintBookmarkBody
		w.ui.bookmarkFooterDraw = w.paintBookmarkFooter
	}
}

// declareWindow declares the window's own chrome: the connecting state while
// there is no browser, else the two panes, the transfer queue and the status
// line. The panes are keyed containers of this page, laid out at the split's
// panel rectangles exactly as the main-window tab lays them out.
func (w *sftpWindow) declareWindow(c ui.C) {
	if w.view.worker == nil {
		w.ui.connect = sftpWindowConnectScratch{}
		w.declareConnecting(c)
		return
	}
	size := w.ui.size
	bounds := gfx.Rect{Width: size.Width, Height: size.Height}
	queue := w.queueCount() > 0
	body, queueRect, status := sftpTabPaneRects(bounds, queue)
	w.ui.body, w.ui.queue = body, queueRect
	w.ui.split = components.BeginResizable(nil, w.owner.Theme(), hitSFTPWindowSplit, body, w.ui.fraction).LimitsPx(220, 220)
	panels := w.ui.split.Paint("")
	firstLocal := w.owner.sftpWindowOrder == sftpLocalLeft
	ui.Column(c).Fill().Children(func() {
		w.declarePaneSide(c, panels.First, firstLocal)
		w.declarePaneSide(c, panels.Second, !firstLocal)
		ui.Column(c).Absolute().
			Left(status.X).Top(status.Y).Width(status.Width).Height(status.Height).
			Children(func() { w.declareStatusLine(c) })
	})
	if queue {
		w.declareQueue(c, queueRect, gfx.Rect{})
	}
	if w.bookmark.open {
		w.declareBookmarkDialog(c, gfx.Rect{Width: w.ui.size.Width, Height: w.ui.size.Height})
	}
	if w.view.dialog != sftpDialogNone {
		w.owner.declareSFTPDialog(c, bounds, w.view)
	}
	w.owner.declareSFTPFileProperties(c, gfx.Rect{Width: size.Width, Height: size.Height}, &w.fileProperties)
}

// declarePaneSide declares one pane as a keyed container of the window page and
// keeps the room its declaration leaves for the details the page does not paint.
func (w *sftpWindow) declarePaneSide(c ui.C, bounds gfx.Rect, local bool) {
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return
	}
	side := sideIndex(local)
	s := &w.ui.panes[side]
	*s = sftpWindowScratch{local: local, bounds: bounds}
	ui.Column(c.KeyInt(side)).Absolute().
		Left(bounds.X).Top(bounds.Y).Width(bounds.Width).Height(bounds.Height).
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
	ui.Text(c, status).FontSize(theme.Metrics.FontSizeS).Color(theme.MutedForeground).Ellipsis().LineHeight(sftpWindowStatusHeight)
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
	if !local && sudoSFTPAvailable(&w.session, w.view) {
		components.BeginToggle(canvas, theme, hitSFTPWindowSudoMode, s.sudoMode.Rect(), w.view.sudo).Small().Outline(m.SFTPModeToggle)
	}
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
// the filter, the column headers and the room the list takes. Its slots go into
// the side's own scratch, so the details painted after the page read the right
// side even when the panes swap places.
func (w *sftpWindow) declarePane(c ui.C, s *sftpWindowScratch) {
	local := s.local
	folder := w.folder(local)
	theme, m := w.owner.Theme(), w.owner.messages
	bounds := s.bounds
	compact := bounds.Height < 230
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
			s.address = ui.Slot(c).Height(32).Grow(1)
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
					icon(sftpWindowActions[sideIndex(local)].At(action.index), action.glyph, action.label, w.browse.pending != nil || w.view.busy && !local || !local && !w.connected(), style)
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
	box.Children(func() {
		declareSFTPRowIcon(c, entry, tint)
		ui.Text(c, entry.entry.Name).Color(color).Ellipsis().Grow(1)
		for _, column := range w.owner.sftpColumns.entries[1:] {
			if !column.Enabled {
				continue
			}
			if column.Key == sftpColumnSize {
				ui.Text(c, entry.cells[2]).FontSize(metrics.FontSizeS).Color(pal.muted).Width(62).TextEnd()
			}
			if column.Key == sftpColumnModified {
				date := entry.cells[3]
				if paneWidth < 420 && len(date) >= 16 {
					date = date[5:10]
				}
				ui.Text(c, date).FontSize(metrics.FontSizeS).Color(pal.muted).Width(w.dateWidth(paneWidth))
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

// declareQueue declares the transfer queue's cards in the window page. The
// queue is scrolled by the window's own offset, so only the cards on screen are
// declared.
func (w *sftpWindow) declareQueue(c ui.C, bounds, origin gfx.Rect) {
	state := w.owner
	w.ui.queueBounds = gfx.Rect{X: bounds.X, Y: bounds.Y + 24, Width: bounds.Width, Height: max(bounds.Height-24, 0)}
	w.ui.queueVisible = w.ui.queueVisible[:0]
	for pass := 0; pass < 2; pass++ {
		for i := len(state.transfers.entries) - 1; i >= 0; i-- {
			e := &state.transfers.entries[i]
			if e.owner != w.view || e.removed || (pass == 0) != transferActive(e.snapshot) {
				continue
			}
			w.ui.queueVisible = append(w.ui.queueVisible, e)
		}
	}
	w.ui.queueItems = len(w.ui.queueVisible)
	if cap(w.ui.queueCards) < w.ui.queueItems {
		w.ui.queueCards = make([]transferCard, w.ui.queueItems)
	} else {
		w.ui.queueCards = w.ui.queueCards[:w.ui.queueItems]
		for index := range w.ui.queueCards {
			w.ui.queueCards[index] = transferCard{}
		}
	}
	maxOffset := max(float32(w.ui.queueItems)*transferCardHeight-w.ui.queueBounds.Height, 0)
	w.ui.queueOffset = min(max(w.ui.queueOffset, 0), maxOffset)
	row := 0
	for _, e := range w.ui.queueVisible {
		box := gfx.Rect{X: bounds.X, Y: w.ui.queueBounds.Y + float32(row)*transferCardHeight - w.ui.queueOffset, Width: bounds.Width - 12, Height: transferCardHeight - 8}
		index := row
		row++
		if box.Y+box.Height < w.ui.queueBounds.Y || box.Y > w.ui.queueBounds.Y+w.ui.queueBounds.Height {
			continue
		}
		card := &w.ui.queueCards[index]
		*card = transferCard{entry: e}
		container := ui.Column(c.KeyInt(e.id)).Absolute().
			Left(box.X - origin.X).Top(box.Y - origin.Y).Width(box.Width).Height(box.Height)
		container.HitWith(sftpWindowQueueRows.At(index), e.title, gfx.HitOptions{Role: gfx.RoleGroup, SkipFocus: true, Hint: e.paths})
		state.declareTransferCard(c, container, e, card)
	}
}

// paintQueueDetails paints the queue's header, scrollbar and the hand-painted
// parts of its cards. The cards themselves are the page's.
func (w *sftpWindow) paintQueueDetails(canvas *gfx.Canvas, bounds gfx.Rect) {
	theme, m := w.owner.Theme(), w.owner.messages
	canvas.Label(m.SFTPTransferQueue, gfx.Rect{X: bounds.X + 4, Y: bounds.Y, Width: bounds.Width - 8, Height: 20}, gfx.TextStyle{Size: theme.Metrics.FontSizeS, Weight: gfx.WeightSemibold, Color: theme.Foreground})
	area := components.BeginScrollArea(canvas, theme, w.ui.queueBounds, gfx.Size{Height: float32(w.ui.queueItems) * transferCardHeight}).Offset(0, w.ui.queueOffset).VerticalScrollbar(hitSFTPWindowQueueBar, m.SFTPTransferQueue, "")
	w.owner.paintTransferCardsFor(canvas, theme, w.ui.queueCards)
	w.ui.queueBar = area.EndContent()
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
	menu := components.BeginContextMenu(canvas, theme, gfx.Rect{X: w.ui.menuAt.X, Y: w.ui.menuAt.Y, Width: width, Height: height}, components.ContextMenuOptions{Layer: popupLayer, Scope: hitSFTPWindowMenuDismiss, DismissID: hitSFTPWindowMenuDismiss, Viewport: w.ui.bounds, Scroll: &w.ui.menuScroll, RowHeight: contextMenuRow})
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
