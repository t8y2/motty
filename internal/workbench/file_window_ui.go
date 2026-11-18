package workbench

import (
	"encoding/binary"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/filecontent"
)

var documentHits = tree.Feature("document-window")
var (
	hitDocumentTabList       = tree.Sub(documentHits, tree.Name("tabs"))
	hitDocumentTabCloseList  = tree.Sub(documentHits, tree.Name("tab-close"))
	hitDocumentTabBar        = tree.ID(documentHits, "tabs-scroll")
	hitDocumentSave          = tree.ID(documentHits, "save")
	hitDocumentReload        = tree.ID(documentHits, "reload")
	hitDocumentInput         = tree.ID(documentHits, "editor")
	hitDocumentEncoding      = tree.ID(documentHits, "encoding")
	hitDocumentScroll        = tree.ID(documentHits, "editor-scroll")
	hitDocumentFit           = tree.ID(documentHits, "fit")
	hitDocumentActual        = tree.ID(documentHits, "actual")
	hitDocumentZoomIn        = tree.ID(documentHits, "zoom-in")
	hitDocumentZoomOut       = tree.ID(documentHits, "zoom-out")
	hitDocumentCancel        = tree.ID(documentHits, "cancel")
	hitDocumentDiscard       = tree.ID(documentHits, "discard")
	hitDocumentSaveClose     = tree.ID(documentHits, "save-close")
	hitDocumentSaveMenu      = tree.ID(documentHits, "save-menu")
	hitDocumentCompare       = tree.ID(documentHits, "compare")
	hitDocumentSaveDirect    = tree.ID(documentHits, "save-direct")
	hitDocumentReturnToEdit  = tree.ID(documentHits, "return-to-edit")
	hitDocumentCompareRetry  = tree.ID(documentHits, "compare-retry")
	hitDocumentConfirmUpload = tree.ID(documentHits, "confirm-upload")
	hitDocumentUploadConfirm = tree.ID(documentHits, "upload-confirm")
	hitDocumentLocation      = tree.ID(documentHits, "location")
	hitDocumentStatus        = tree.ID(documentHits, "status")
	hitDocumentDiffScroll    = tree.ID(documentHits, "diff-scroll-v")
	hitDocumentDiffScrollX   = tree.ID(documentHits, "diff-scroll-h")
	hitDocumentDiffViewport  = tree.ID(documentHits, "diff-viewport")
)

type fileDocumentUI struct {
	marks       []components.MarkdownCodeRun
	highlighter func(string, string, []components.CodeEditorRun) []components.CodeEditorRun
	// page is the window's page when this document is painted in a window of its
	// own (a paint not made by the shared window manager). compareView declares
	// the comparison body into it.
	page        *ui.Page
	compareView func(ui.C)
	// contentBox and compare keep the room the page laid out for the editor, the
	// image and the diff; what those paint by hand goes into them.
	contentBox ui.Box
	code       components.CodeEditor
	diff       components.CodeDiff
	input      app.TextInput
	// encodingChoice is the encoding select's value, empty for Automatic. It is
	// copied from the document's format as the page declares the control and
	// written back by it when the user picks another.
	encodingChoice  string
	encodingChoices []string
	editorBounds    gfx.Rect
	imageBounds     gfx.Rect
	size            gfx.Size
	// saveMenuTrigger is the chevron the save menu hangs under, saveMenuDeclared
	// whether the page declared it in this paint, and saveMenuOpen whether the
	// menu is open.
	saveMenuTrigger  ui.Box
	saveMenuDeclared bool
	saveMenuOpen     bool
	zoom             float32
	scale            float32
	pan              gfx.Point
	imageGeometry    components.ImageViewGeometry
	tooltip          components.HoverTooltip
	// caret is the phase of the caret both views draw: the one in the editor and
	// the one in the comparison, which never shows together.
	caret components.CaretBlink
}

func (d *fileDocument) paint(canvas *gfx.Canvas, size gfx.Size) {
	defer d.owner.paintNoteExit(canvas, gfx.Rect{Width: size.Width, Height: size.Height})
	if d.note != nil {
		d.paintNoteWindow(canvas, size)
		return
	}

	canvas.SetTree(&tree)
	if id, ok := canvas.ClickTarget(); ok {
		canvas.AfterPaint(func() { d.click(id) })
	}
	d.ui.size = size
	d.ui.scale = canvas.PixelScale()
	if d.ui.highlighter == nil {
		d.ui.highlighter = d.highlight
	}
	theme := d.owner.Theme()
	canvas.Fill(gfx.Rect{Width: size.Width, Height: size.Height}, theme.Background)
	d.windowPage().Draw(canvas, gfx.Rect{Width: size.Width, Height: size.Height}, d.declareEditWindow)
	if d.owner.fileWindows.painting {
		d.owner.fileWindows.paintTabsScrollbar(canvas)
	}
	if d.image != nil {
		d.ui.imageBounds = d.ui.contentBox.Rect()
		d.ui.imageGeometry = components.PaintImageView(canvas, theme, d.ui.contentBox.Rect(), components.ImageViewOptions{
			Bitmap: gfx.Bitmap{Pix: d.image.Pix, Width: d.image.Width, Height: d.image.Height, Stride: d.image.Width * 4, ID: binary.LittleEndian.Uint64(d.revision[:8]), Version: 1}, Zoom: d.ui.zoom, Pan: d.ui.pan,
		})
	} else {
		d.ui.editorBounds = d.ui.contentBox.Rect()
		d.paintCodeEditor(canvas, theme, d.ui.contentBox.Rect())
	}
	d.notices.paint(canvas, theme, gfx.Rect{Width: size.Width, Height: size.Height})
	d.ui.tooltip.Paint(canvas, theme, gfx.Rect{Width: size.Width, Height: size.Height}, components.TooltipOptions{Layer: tooltipLayer, SideOffset: tooltipGap}, components.HoverTooltipDelay)
}

// windowPage is the page this paint declares its window in: the shared window's
// one page while the manager paints, else the document's own.
func (d *fileDocument) windowPage() *ui.Page {
	if d.owner.fileWindows.painting {
		return d.owner.fileWindows.editPage(&d.owner.theme)
	}
	return d.documentPage()
}

// declareEditWindow declares the document window: the tab strip when it shares a
// window, the problem line, the room the editor takes, and the footer. What the
// editor, the image and the footer's menus paint themselves goes into the slots
// kept here.
func (d *fileDocument) declareEditWindow(c ui.C) {
	theme, m := d.owner.Theme(), d.owner.messages.FileWindow
	bounds := c.Bounds()
	shared := d.owner.fileWindows.painting
	d.ui.saveMenuTrigger, d.ui.saveMenuDeclared = ui.Box{}, false
	ui.Column(c).Fill().Children(func() {
		declareWindowTitlebar(c, d.owner.pal, d.name, d.source)
		ui.Column(c).Grow(1).Shrink(1).Pad(12).Gap(8).Children(func() {
			if shared {
				d.owner.fileWindows.declareTabsSection(c, bounds.Width)
				ui.Divider(c).Background(theme.Border)
			}
			if d.problem != "" {
				ui.Paragraph(c, d.problem, 4).FontSize(theme.Metrics.FontSizeS).Color(theme.Danger).LineHeight(18)
			}
			d.ui.contentBox = ui.Slot(c).Grow(1).MinHeight(64)
			ui.Row(c).Height(32).Gap(8).Items(ui.Center).Children(func() {
				if d.image == nil {
					d.declareEncoding(c, bounds)
				}
				status := d.footerStatus()
				ui.Text(c, status).FontSize(theme.Metrics.FontSizeS).Color(theme.MutedForeground).Ellipsis().LineHeight(22).Grow(1).
					HitWith(hitDocumentStatus, status, gfx.HitOptions{Role: gfx.RoleGroup, SkipFocus: true, Hint: status})
				if d.image != nil {
					d.pageButton(c, "fit", hitDocumentFit, m.Fit).Small().Outline()
					d.pageButton(c, "actual", hitDocumentActual, m.Actual).Small().Outline()
					d.pageButton(c, "zoom-out", hitDocumentZoomOut, "−").Small().Outline()
					d.pageButton(c, "zoom-in", hitDocumentZoomIn, "+").Small().Outline()
				}
				if d.image == nil && d.ready {
					wrap := d.owner.messages.CodeEditor.WrapOn
					if d.edit.noWrap {
						wrap = d.owner.messages.CodeEditor.WrapOff
					}
					d.pageButton(c, "wrap", hitDocumentWrap, wrap).Small().Outline()
				}
				d.pageButton(c, "reload", hitDocumentReload, m.Reload).Small().Outline().Disabled(d.busy || d.comparison.active)
				if d.image == nil {
					disabled := d.busy || !d.ready || !d.dirty() || d.comparison.active
					if d.local {
						d.pageButton(c, "save", hitDocumentSave, m.Save).Small().Disabled(disabled)
					} else {
						d.declareSaveSplit(c, disabled)
					}
				}
			})
		})
	})
	d.declareSaveMenu(c)
	if d.confirm != "" {
		d.declareConfirmation(c)
	}
}

// declareEncoding declares the footer's encoding select: a label and the page's
// drop-down, whose value is the document's encoding, empty for Automatic.
func (d *fileDocument) declareEncoding(c ui.C, bounds gfx.Rect) {
	theme, m := d.owner.Theme(), d.owner.messages.FileWindow
	if d.ui.encodingChoice != d.format.Encoding {
		d.ui.encodingChoice = d.format.Encoding
	}
	if d.ui.encodingChoices == nil {
		d.ui.encodingChoices = append([]string{""}, filecontent.Encodings...)
	}
	ui.Row(c).Width(min(220, bounds.Width*.4)).Height(32).Gap(4).Items(ui.Center).Shrink(0).Children(func() {
		ui.Text(c, m.Encoding+":").FontSize(theme.Metrics.FontSizeS).Color(theme.MutedForeground).LineHeight(22).Shrink(0)
		selector := ui.Select(c, &d.ui.encodingChoice, d.ui.encodingChoices, d.encodingLabel).
			Label(m.Encoding).Hit(hitDocumentEncoding, m.Encoding).Height(32).Grow(1).
			Disabled(d.busy || d.comparison.active)
		if selector.Changed() {
			name := d.ui.encodingChoice
			c.After(func() { d.chooseEncoding(name) })
		}
	})
}

// encodingLabel names one choice of the encoding select.
func (d *fileDocument) encodingLabel(name string) string {
	if name == "" {
		return d.owner.messages.FileWindow.Auto
	}
	return name
}

// declareSaveSplit declares the remote save button and the chevron that opens
// the actions beside it, as page buttons under one row. Both keep the
// application's own identities, so the central dispatcher handles their clicks.
func (d *fileDocument) declareSaveSplit(c ui.C, disabled bool) {
	m := d.owner.messages.FileWindow
	theme := d.owner.Theme()
	ui.Row(c).Gap(0).Shrink(0).Items(ui.Center).Children(func() {
		// The split button the page replaces kept its halves at the size it was
		// given: an 80px primary and a 32px trigger, the label in the small type
		// the size names.
		ui.Button(c, m.Save).Small().Width(80).Height(32).Hit(hitDocumentSave, m.Save).Disabled(disabled)
		d.ui.saveMenuTrigger = ui.Button(c, m.SaveOptions).Icon(lucideIcons[iconChevronDown]).IconOnly().
			Size(32, 32).Hit(hitDocumentSaveMenu, m.SaveOptions).Disabled(disabled).
			Draw(func(part *ui.DrawContext) {
				// The hairline the split button drew between its halves.
				part.Canvas.Fill(gfx.Rect{X: part.Rect.X - 0.5, Y: part.Rect.Y, Width: 1, Height: part.Rect.Height}, theme.Input)
			})
		d.ui.saveMenuDeclared = true
	})
}

// declareSaveMenu declares the open save menu under the chevron: the actions
// that work on the server's copy of the document.
func (d *fileDocument) declareSaveMenu(c ui.C) {
	if !d.ui.saveMenuOpen || !d.ui.saveMenuDeclared {
		return
	}
	m := d.owner.messages.FileWindow
	var rows menuRows
	rows.add(components.ContextMenuItem{ID: hitDocumentCompare, Label: m.CompareSave, Icon: lucideIcons[iconColumns2]})
	ui.Menu(c, d.ui.saveMenuTrigger, &d.ui.saveMenuOpen, func(menu *ui.MenuItems) {
		menu.WidthRange(184, 184)
		rows.declare(c, menu, func(id gfx.ID) { d.click(id) })
	})
}

// pageButton is a footer button of the page, named by key so its identity does
// not move when a neighbouring control comes or goes. Its click is the page's,
// handled after the paint that delivered it.
func (d *fileDocument) pageButton(c ui.C, key string, id gfx.ID, label string) ui.Box {
	c.Key(key)
	button := ui.Button(c, label)
	if button.Clicked() {
		c.After(func() { d.click(id) })
	}
	return button
}

func (d *fileDocument) paintComparison(canvas *gfx.Canvas, size gfx.Size) {
	canvas.SetTree(&tree)
	if id, ok := canvas.ClickTarget(); ok {
		canvas.AfterPaint(func() { d.clickComparison(id) })
	}
	uiState := &d.comparison.ui
	uiState.size = size
	uiState.scale = canvas.PixelScale()
	if uiState.highlighter == nil {
		uiState.highlighter = d.highlight
	}
	theme, m := d.owner.Theme(), d.owner.messages.FileWindow
	canvas.Fill(gfx.Rect{Width: size.Width, Height: size.Height}, theme.Background)
	d.comparisonPage().Draw(canvas, gfx.Rect{Width: size.Width, Height: size.Height}, uiState.compareView)

	uiState.editorBounds = uiState.contentBox.Rect()
	value := d.editor.String()
	selection := d.editor.Selection()
	caret := d.editor.ByteOffset(d.editor.Caret())
	focused := canvas.Focused() == hitDocumentInput
	showCaret := false
	if focused && selection.Start == selection.End {
		showCaret = uiState.caret.Lit(canvas, caret)
	} else {
		uiState.caret.Rest()
	}
	snapshot := components.CodeDiffSnapshot{
		Result: d.comparison.result, ID: hitDocumentInput, Label: m.DiffLabel,
		Value: value, Language: documentLanguage(d.name), Highlight: uiState.highlighter,
		AddedColor: theme.Success, RemovedColor: theme.Danger,
		FontSize: d.owner.terminalFontSize, MonoFace: d.owner.terminalFontFace, TabWidth: d.edit.options.TabWidth,
		Disabled: d.busy || d.comparison.loading || d.comparison.confirm, Focused: focused,
		ShowCaret:      showCaret,
		Caret:          caret,
		SelectionStart: d.editor.ByteOffset(selection.Start), SelectionEnd: d.editor.ByteOffset(selection.End),
		CaretColor:          d.owner.pal.cursor,
		VerticalScrollbarID: hitDocumentDiffScroll, HorizontalScrollbarID: hitDocumentDiffScrollX,
		ViewportID: hitDocumentDiffViewport,
	}
	fontSize := d.owner.terminalFontSize
	if fontSize <= 0 {
		fontSize = theme.Metrics.FontSizeM
	}
	cellWidth, _ := canvas.MonoCellSize(fontSize, d.owner.terminalFontFace)
	if uiState.contentBox.Rect().Width < 2*(40*cellWidth+72) {
		snapshot.Layout = components.CodeDiffStacked
	}
	if marked, ok := d.editor.Composition(); ok {
		snapshot.CompositionStart = d.editor.ByteOffset(marked.Start)
		snapshot.CompositionEnd = d.editor.ByteOffset(marked.End)
	}
	uiState.diff.Paint(canvas, theme, uiState.contentBox.Rect(), snapshot)
	uiState.tooltip.Paint(canvas, theme, gfx.Rect{Width: size.Width, Height: size.Height}, components.TooltipOptions{Layer: tooltipLayer, SideOffset: tooltipGap}, components.HoverTooltipDelay)
}

// comparisonPage is the page that lays out the comparison window's body. It
// lives with the comparison's UI so a window opened for one document keeps its
// own page.
func (d *fileDocument) comparisonPage() *ui.Page {
	uiState := &d.comparison.ui
	if uiState.page == nil {
		uiState.compareView = d.declareComparisonWindow
		uiState.page = ui.NewPage(ui.PageConfig{
			Tree: &tree, Root: fmt.Sprintf("file-comparison-%d", fileWindowPageSeq.Add(1)), Theme: &d.owner.theme, View: uiState.compareView,
		})
	}
	return uiState.page
}

// declareComparisonWindow declares the comparison window: the file's name, the
// version counts and the room the diff takes, with the actions under it. The
// buttons keep their application identities and are painted after the page.
func (d *fileDocument) declareComparisonWindow(c ui.C) {
	uiState := &d.comparison.ui
	theme, m := *c.Theme(), d.owner.messages.FileWindow
	uiState.contentBox = ui.Box{}
	ui.Column(c).Fill().Children(func() {
		declareWindowTitlebar(c, d.owner.pal, m.CompareSave, d.name)
		ui.Column(c).Grow(1).Shrink(1).Pad(12).Gap(8).Children(func() {
			ui.Text(c, d.name).FontSize(18).Bold().Color(theme.Foreground).Ellipsis().LineHeight(32).
				HitWith(hitDocumentLocation, d.name, gfx.HitOptions{Role: gfx.RoleGroup, SkipFocus: true, Hint: d.source + " · " + d.location})
			if d.problem != "" {
				ui.Paragraph(c, d.problem, 4).FontSize(theme.Metrics.FontSizeS).Color(theme.Danger).LineHeight(18)
			}
			ui.Row(c).Height(28).Gap(10).Items(ui.Center).Children(func() {
				ui.Text(c, m.ServerVersion).Grow(1).FontSize(theme.Metrics.FontSizeS).Weight(gfx.WeightMedium).Color(theme.MutedForeground).Ellipsis()
				ui.Text(c, fmt.Sprintf(m.CompareAddedRemoved, d.comparison.result.Stats.Added, d.comparison.result.Stats.Removed)).
					FontSize(theme.Metrics.FontSizeS).Color(theme.MutedForeground).Ellipsis()
				ui.Text(c, m.LocalVersion).Grow(1).TextEnd().FontSize(theme.Metrics.FontSizeS).Weight(gfx.WeightMedium).Color(theme.MutedForeground).Ellipsis()
			})
			if d.comparison.result.Stats.Coarse {
				ui.Text(c, m.CompareCoarse).FontSize(theme.Metrics.FontSizeS).Color(d.owner.pal.warning).Ellipsis()
			}
			uiState.contentBox = ui.Slot(c).Grow(1).MinHeight(64)
			ui.Row(c).Height(32).Gap(8).Items(ui.Center).Children(func() {
				status := d.comparisonStatus()
				ui.Text(c, status).FontSize(theme.Metrics.FontSizeS).Color(theme.MutedForeground).Ellipsis().LineHeight(22).Grow(1).
					HitWith(hitDocumentStatus, status, gfx.HitOptions{Role: gfx.RoleGroup, SkipFocus: true, Hint: status})
				if d.comparison.conflict {
					ui.Button(c, m.CompareRetry).Small().Outline().Hit(hitDocumentCompareRetry, m.CompareRetry).Disabled(d.busy)
				}
				ui.Button(c, m.ReturnToEdit).Small().Outline().Hit(hitDocumentReturnToEdit, m.ReturnToEdit).Disabled(d.busy || d.comparison.saving)
				ui.Button(c, m.Save).Small().Hit(hitDocumentConfirmUpload, m.Save).Disabled(!d.canUploadComparison())
			})
		})
	})
	if d.comparison.confirm {
		d.declareComparisonConfirmation(c)
	}
}

func (d *fileDocument) footerStatus() string {
	m := d.owner.messages.FileWindow
	if d.image != nil {
		return fmt.Sprintf("%s · %d × %d · %s", strings.ToUpper(d.image.Format), d.image.Width, d.image.Height, m.ReadOnly)
	}
	status := d.status
	if status == m.Ready {
		status = ""
	}
	if d.dirty() && !d.busy {
		status = m.Dirty
	}
	if !d.ready {
		return status
	}
	ending := "LF"
	if d.format.LineEnding == "\r\n" {
		ending = "CRLF"
	}
	if d.format.LineEnding == "\r" {
		ending = "CR"
	}
	position := d.positionStatus() + " · " + ending
	if status == "" {
		return position
	}
	return position + " · " + status
}

func (d *fileDocument) comparisonStatus() string {
	m := d.owner.messages.FileWindow
	switch {
	case d.comparison.saving:
		return m.SavingChanges
	case d.comparison.loading:
		return m.Comparing
	case d.comparison.conflict:
		return m.CompareConflict
	case d.comparison.oversized:
		return m.CompareLimit
	case d.comparison.pending:
		return m.CompareUpdating
	case d.comparison.result.Stats.Added+d.comparison.result.Stats.Removed == 0:
		return m.CompareEqual
	default:
		return m.CompareReady
	}
}

// declareComparisonConfirmation declares the comparison window's confirmation
// before its changes are written to the server.
func (d *fileDocument) declareComparisonConfirmation(c ui.C) {
	m := d.owner.messages.FileWindow
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: m.SaveConfirmFinalTitle, Description: m.SaveConfirmFinalDescription, Width: 512, Layer: dialogLayer,
		Scope: dialogScope, CloseID: hitDocumentCancel, InitialFocusID: hitDocumentCancel, NoClose: true,
	})
	dialog.Footer(func() {
		// The alert dialog these replace gave both actions a 72px least width.
		ui.Button(c, m.Cancel).Outline().MinWidth(72).Hit(hitDocumentCancel, m.Cancel)
		ui.Button(c, m.Save).MinWidth(72).Hit(hitDocumentUploadConfirm, m.Save)
	})
}

// declareConfirmation declares the question the document asks before it is
// reloaded, closed or saved over the server's copy.
func (d *fileDocument) declareConfirmation(c ui.C) {
	m := d.owner.messages.FileWindow
	if d.confirm == "reload" {
		dialog := ui.Dialog(c, ui.DialogSpec{
			Title: m.ConfirmTitle, Description: m.ConfirmReload, Width: 512, Layer: dialogLayer,
			Scope: dialogScope, CloseID: hitDocumentCancel, InitialFocusID: hitDocumentCancel, NoClose: true,
		})
		dialog.Footer(func() {
			// The alert dialog this replaces gave both actions a 72px least width.
			ui.Button(c, m.Cancel).Outline().MinWidth(72).Hit(hitDocumentCancel, m.Cancel)
			ui.Button(c, m.Discard).Destructive().MinWidth(72).Hit(hitDocumentDiscard, m.Discard)
		})
		return
	}
	if d.confirm == "save" {
		dialog := ui.Dialog(c, ui.DialogSpec{
			Title: m.SaveConfirmTitle, Description: m.SaveConfirmDescription, Width: 500, Layer: dialogLayer,
			Scope: dialogScope, CloseID: hitDocumentCancel, InitialFocusID: hitDocumentCancel, NoClose: true,
		})
		dialog.Footer(func() {
			d.owner.dialogActionFooter(c,
				components.ButtonAction{ID: hitDocumentCancel, Label: m.Cancel, Variant: components.ButtonOutline},
				components.ButtonAction{ID: hitDocumentSaveDirect, Label: m.SaveDirect},
			)
		})
		return
	}
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: m.ConfirmTitle, Description: m.ConfirmClose, Width: 460, Layer: dialogLayer,
		Scope: dialogScope, CloseID: hitDocumentCancel, InitialFocusID: hitDocumentCancel, NoClose: true,
	})
	dialog.Footer(func() {
		d.owner.dialogActionFooter(c,
			components.ButtonAction{ID: hitDocumentDiscard, Label: m.Discard, Variant: components.ButtonDestructive},
			components.ButtonAction{ID: hitDocumentCancel, Label: m.Cancel, Variant: components.ButtonOutline},
			components.ButtonAction{ID: hitDocumentSaveClose, Label: m.SaveClose},
		)
	})
}
func (d *fileDocument) click(id gfx.ID) {
	if d.owner.clickNoteExit(id) {
		return
	}
	if d.owner.fileWindows.clickTab(id) {
		return
	}
	if d.note != nil {
		d.clickNoteWindow(id)
		return
	}
	if d.notices.click(id) {
		return
	}
	if d.confirm != "" {
		switch id {
		case hitDocumentCancel:
			cancelClose := d.closeCanceled
			if d.confirm == "close" {
				d.closeCanceled = nil
			}
			d.confirm = ""
			d.confirmCloseAfter = false
			d.allowClose = nil
			d.afterClosed = nil
			d.closeAfter = false
			if cancelClose != nil {
				cancelClose()
			}
		case hitDocumentSaveClose:
			d.saveDirect(true)
		case hitDocumentSaveDirect:
			closeAfter := d.confirmCloseAfter
			d.confirm = ""
			d.confirmCloseAfter = false
			d.saveDirect(closeAfter)
		case hitDocumentDiscard:
			if d.confirm == "reload" {
				d.reload()
			} else {
				d.closeAllowed()
			}
		}
		return
	}
	if d.clickCode(id) {
		return
	}
	switch {
	case id == hitDocumentSave:
		d.requestSave(false)
	case id == hitDocumentSaveMenu:
		if !d.busy && d.ready && d.dirty() && !d.comparison.active {
			d.ui.saveMenuOpen = !d.ui.saveMenuOpen
		}
	case id == hitDocumentCompare:
		d.ui.saveMenuOpen = false
		d.beginComparison()
	case id == hitDocumentReload:
		if d.comparison.active {
			return
		}
		if d.dirty() {
			d.confirm = "reload"
		} else {
			d.reload()
		}
	case id == hitDocumentFit:
		d.ui.zoom = 0
		d.ui.pan = gfx.Point{}
	case id == hitDocumentActual:
		d.ui.zoom = 1
		d.ui.pan = gfx.Point{}
	case id == hitDocumentZoomIn:
		d.zoom(1.25)
	case id == hitDocumentZoomOut:
		d.zoom(.8)
	}
}

func (d *fileDocument) clickComparison(id gfx.ID) {
	if d.comparison.confirm {
		switch id {
		case hitDocumentCancel:
			d.comparison.confirm = false
			d.comparison.ui.tooltip = components.HoverTooltip{}
		case hitDocumentUploadConfirm:
			d.uploadComparison()
		}
		return
	}
	switch id {
	case hitDocumentReturnToEdit:
		d.cancelComparison()
	case hitDocumentCompareRetry:
		d.refreshComparison()
	case hitDocumentConfirmUpload:
		d.confirmComparisonUpload()
	}
}

func (d *fileDocument) refreshComparison() {
	if d.busy || !d.comparison.active || d.worker == nil {
		return
	}
	if !fileDiffWithinLimit(d.editor.String()) {
		d.comparison.oversized = true
		d.problem = d.owner.messages.FileWindow.CompareLimit
		d.notices.show(d.name, d.problem, components.ToastWarning)
		d.invalidateWindows()
		return
	}
	c := &d.comparison
	c.loading = true
	c.pending = false
	c.conflict = false
	c.oversized = false
	c.saving = false
	c.confirm = false
	c.generation++
	d.busy = true
	d.problem = ""
	d.status = d.owner.messages.FileWindow.Comparing
	if !d.worker.ReadDocument(d.location) {
		d.busy = false
		c.loading = false
		c.conflict = true
		d.status = d.owner.messages.SFTPUnavailable
		d.problem = d.status
		d.notices.show(d.name, d.problem, components.ToastError)
	}
	d.invalidateWindows()
}

func (d *fileDocument) zoom(factor float32) {
	zoom := d.ui.zoom
	if zoom == 0 {
		zoom = d.ui.imageGeometry.Scale * d.ui.pixelScale()
	}
	d.ui.zoom = max(min(zoom*factor, 32), .01)
	d.ui.pan = gfx.Point{}
}
func (view *fileDocumentUI) pixelScale() float32 {
	if view.imageGeometry.Scale <= 0 {
		return 1
	}
	return view.scale
}

func (d *fileDocument) textInput(context app.TextInputContext) *app.TextInput {
	if d.owner.noteExit != nil {
		return nil
	}
	if d.note != nil {
		return d.noteWindowTextInput(context)
	}

	if input := d.barTextInput(context.ID); input != nil {
		return input
	}
	if context.ID != hitDocumentInput || !d.ready || d.busy || d.confirm != "" || d.comparison.confirm {
		return nil
	}
	d.ui.input.Editor = nil
	d.ui.input.State = app.TextStateOf(&d.editor)
	d.ui.input.OnEvent = func(event app.TextEvent) (app.TextState, error) {
		if d.ready && !d.busy && d.confirm == "" {
			before := d.editor.String()
			if event.Kind == app.TextInsert || event.Kind == app.TextPaste {
				event.Text = strings.ReplaceAll(strings.ReplaceAll(event.Text, "\r\n", "\n"), "\r", "\n")
			}
			event.Apply(&d.editor)
			if d.comparison.active && d.editor.String() != before {
				d.scheduleDiff()
			}
		}
		return app.TextStateOf(&d.editor), nil
	}
	return &d.ui.input
}

func (d *fileDocument) comparisonTextInput(context app.TextInputContext) *app.TextInput {
	if context.ID != hitDocumentInput || !d.comparison.active || !d.ready || d.busy || d.comparison.confirm {
		return nil
	}
	input := &d.comparison.ui.input
	input.Editor = nil
	input.State = app.TextStateOf(&d.editor)
	input.OnEvent = func(event app.TextEvent) (app.TextState, error) {
		if d.comparison.active && d.ready && !d.busy && !d.comparison.confirm {
			before := d.editor.String()
			if event.Kind == app.TextInsert || event.Kind == app.TextPaste {
				event.Text = strings.ReplaceAll(strings.ReplaceAll(event.Text, "\r\n", "\n"), "\r", "\n")
			}
			event.Apply(&d.editor)
			if d.editor.String() != before {
				d.scheduleDiff()
			}
		}
		return app.TextStateOf(&d.editor), nil
	}
	return input
}

func (d *fileDocument) key(event gfx.KeyEvent) gfx.EventResult {
	if d.owner.noteExit != nil {
		return d.owner.noteExitKey(event)
	}
	if d.note != nil {
		return d.noteWindowKey(event)
	}

	if event.Release {
		return gfx.EventResult{}
	}
	handled := gfx.EventResult{Handled: true, Repaint: true}
	if d.confirm != "" {
		if event.Key == gfx.KeyEscape {
			d.click(hitDocumentCancel)
			return handled
		}
		return gfx.EventResult{}
	}
	if result, ok := d.codeKey(event); ok {
		return result
	}
	return gfx.EventResult{}
}

func (d *fileDocument) comparisonKey(event gfx.KeyEvent) gfx.EventResult {
	if event.Release || !d.comparison.active {
		return gfx.EventResult{}
	}
	handled := gfx.EventResult{Handled: true, Repaint: true}
	if d.comparison.confirm {
		if event.Key == gfx.KeyEscape {
			d.comparison.confirm = false
			return handled
		}
		return gfx.EventResult{}
	}
	if event.Target == hitDocumentInput {
		if command, ok := components.CodeEditorCommandForKey(event); ok && d.ready && !d.busy && command.Apply(&d.editor) {
			d.scheduleDiff()
			d.comparison.ui.diff.RevealCaret(d.editor.String(), d.editor.ByteOffset(d.editor.Caret()))
			return handled
		}
		if movement, extend := components.TextareaMovementForKey(event.Key, event.Modifiers); movement != components.TextareaMoveNone {
			value := d.editor.String()
			offset := d.comparison.ui.diff.Move(value, d.editor.ByteOffset(d.editor.Caret()), movement)
			d.editor.MoveTo(utf8.RuneCountInString(value[:offset]), extend)
			d.comparison.ui.diff.RevealCaret(value, offset)
			return handled
		}
	}
	if event.Target == hitDocumentDiffViewport || event.Target == hitDocumentDiffScroll || event.Target == hitDocumentDiffScrollX {
		if result, ok := d.comparison.ui.diff.ScrollForKey(event.Key, event.Modifiers&gfx.ModifierShift != 0); ok {
			return result
		}
	}
	return gfx.EventResult{}
}

func (d *fileDocument) shortcut(event gfx.KeyEvent) gfx.EventResult {
	if d.owner.noteExit != nil {
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	if d.note != nil {
		return d.noteWindowShortcut(event)
	}

	if event.Modifiers&(gfx.ModifierControl|gfx.ModifierMeta) == 0 || event.Release {
		return gfx.EventResult{}
	}
	if result, ok := d.codeShortcut(event); ok {
		return result
	}
	switch strings.ToLower(event.Text) {
	case "s":
		if d.confirm == "" {
			d.requestSave(false)
		}
		return gfx.EventResult{Handled: true, Repaint: true}
	case "w":
		d.window.RequestClose()
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	return gfx.EventResult{}
}

func (d *fileDocument) comparisonShortcut(event gfx.KeyEvent) gfx.EventResult {
	if event.Modifiers&(gfx.ModifierControl|gfx.ModifierMeta) == 0 || event.Release || !d.comparison.active {
		return gfx.EventResult{}
	}
	if command, ok := components.CodeEditorCommandForKey(event); ok && event.Target == hitDocumentInput && d.ready && !d.busy && !d.comparison.confirm && command.Apply(&d.editor) {
		d.scheduleDiff()
		d.comparison.ui.diff.RevealCaret(d.editor.String(), d.editor.ByteOffset(d.editor.Caret()))
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	switch strings.ToLower(event.Text) {
	case "s":
		if !d.comparison.confirm {
			d.confirmComparisonUpload()
		}
	case "w":
		if d.comparison.window != nil {
			d.comparison.window.RequestClose()
		}
	default:
		return gfx.EventResult{}
	}
	return gfx.EventResult{Handled: true, Repaint: true}
}

func (d *fileDocument) scroll(event gfx.ScrollEvent) gfx.EventResult {
	if d.owner.noteExit != nil {
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	if d.note != nil {
		return d.noteWindowScroll(event)
	}

	if d.confirm != "" {
		return gfx.EventResult{Handled: true}
	}
	if d.image != nil {
		if event.Modifiers&(gfx.ModifierControl|gfx.ModifierMeta) != 0 {
			d.zoom(float32(math.Pow(1.0015, float64(-event.DeltaY))))
		} else {
			d.ui.pan.X -= event.DeltaX
			d.ui.pan.Y -= event.DeltaY
			width := float32(d.image.Width) * d.ui.imageGeometry.Scale
			height := float32(d.image.Height) * d.ui.imageGeometry.Scale
			x, y := max((width-d.ui.imageBounds.Width)/2, 0), max((height-d.ui.imageBounds.Height)/2, 0)
			d.ui.pan.X = max(-x, min(x, d.ui.pan.X))
			d.ui.pan.Y = max(-y, min(y, d.ui.pan.Y))
		}
	} else {
		return d.ui.code.Scroll(event)
	}
	return gfx.EventResult{Handled: true, Repaint: true}
}

func (d *fileDocument) pointer(event gfx.PointerEvent) gfx.EventResult {
	if d.owner.noteExit != nil {
		if isNoteExitHit(event.Target) {
			return gfx.EventResult{}
		}
		return gfx.EventResult{Handled: true}
	}
	if d.note != nil {
		return d.noteWindowPointer(event)
	}

	if d.confirm != "" {
		return gfx.EventResult{}
	}
	return d.ui.code.Pointer(event)
}

func (d *fileDocument) comparisonScroll(event gfx.ScrollEvent) gfx.EventResult {
	if !d.comparison.active || d.comparison.confirm {
		return gfx.EventResult{Handled: true}
	}
	return d.comparison.ui.diff.Scroll(event)
}

func (d *fileDocument) comparisonPointer(event gfx.PointerEvent) gfx.EventResult {
	if !d.comparison.active || d.comparison.confirm {
		return gfx.EventResult{}
	}
	return d.comparison.ui.diff.Pointer(event)
}

func (d *fileDocument) codeColors() [components.MarkdownCodeClasses]gfx.Color {
	return d.owner.codeColors()
}

func (s *State) codeColors() [components.MarkdownCodeClasses]gfx.Color {
	pal := s.pal
	ansi := func(index int) gfx.Color { return gfx.RGB(pal.ansi[index][0], pal.ansi[index][1], pal.ansi[index][2]) }
	return [components.MarkdownCodeClasses]gfx.Color{components.CodeComment: pal.faint, components.CodeString: ansi(2), components.CodeNumber: ansi(3), components.CodeKeyword: ansi(5), components.CodeFunction: ansi(4), components.CodeVariable: ansi(6), components.CodeFlag: ansi(6), components.CodeKey: ansi(4), components.CodeAdded: ansi(2), components.CodeRemoved: ansi(1)}
}

func documentLanguage(name string) string {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
	if strings.EqualFold(name, "Dockerfile") {
		return "sh"
	}
	if ext == "conf" || ext == "config" || ext == "env" || ext == "properties" {
		return "ini"
	}
	if ext == "xml" || ext == "html" || ext == "svg" {
		return "xml"
	}
	return ext
}
