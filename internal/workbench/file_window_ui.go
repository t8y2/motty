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
	hitDocumentTabList         = tree.Sub(documentHits, tree.Name("tabs"))
	hitDocumentTabCloseList    = tree.Sub(documentHits, tree.Name("tab-close"))
	hitDocumentTabBar          = tree.ID(documentHits, "tabs-scroll")
	hitDocumentSave            = tree.ID(documentHits, "save")
	hitDocumentReload          = tree.ID(documentHits, "reload")
	hitDocumentInput           = tree.ID(documentHits, "editor")
	hitDocumentEncoding        = tree.ID(documentHits, "encoding")
	hitDocumentEncodings       = tree.Sub(documentHits, tree.Name("encoding-items"))
	hitDocumentEncodingDismiss = tree.ID(documentHits, "encoding-dismiss")
	hitDocumentEncodingBar     = tree.ID(documentHits, "encoding-scroll")
	hitDocumentScroll          = tree.ID(documentHits, "editor-scroll")
	hitDocumentFit             = tree.ID(documentHits, "fit")
	hitDocumentActual          = tree.ID(documentHits, "actual")
	hitDocumentZoomIn          = tree.ID(documentHits, "zoom-in")
	hitDocumentZoomOut         = tree.ID(documentHits, "zoom-out")
	hitDocumentCancel          = tree.ID(documentHits, "cancel")
	hitDocumentDiscard         = tree.ID(documentHits, "discard")
	hitDocumentSaveClose       = tree.ID(documentHits, "save-close")
	hitDocumentSaveMenu        = tree.ID(documentHits, "save-menu")
	hitDocumentSaveMenuDismiss = tree.ID(documentHits, "save-menu-dismiss")
	hitDocumentCompare         = tree.ID(documentHits, "compare")
	hitDocumentSaveDirect      = tree.ID(documentHits, "save-direct")
	hitDocumentReturnToEdit    = tree.ID(documentHits, "return-to-edit")
	hitDocumentCompareRetry    = tree.ID(documentHits, "compare-retry")
	hitDocumentConfirmUpload   = tree.ID(documentHits, "confirm-upload")
	hitDocumentUploadConfirm   = tree.ID(documentHits, "upload-confirm")
	hitDocumentLocation        = tree.ID(documentHits, "location")
	hitDocumentStatus          = tree.ID(documentHits, "status")
	hitDocumentDiffScroll      = tree.ID(documentHits, "diff-scroll-v")
	hitDocumentDiffScrollX     = tree.ID(documentHits, "diff-scroll-h")
	hitDocumentDiffViewport    = tree.ID(documentHits, "diff-viewport")
)

type fileDocumentUI struct {
	marks                                     []components.MarkdownCodeRun
	highlighter                               func(string, string, []components.CodeEditorRun) []components.CodeEditorRun
	frame                                     ui.View
	code                                      components.CodeEditor
	diff                                      components.CodeDiff
	input                                     app.TextInput
	editorBounds, encodingBounds, imageBounds gfx.Rect
	size                                      gfx.Size
	menu                                      components.MenuScroll
	saveMenu                                  components.MenuScroll
	saveMenuOpen                              bool
	saveAnchor                                gfx.Rect
	zoom                                      float32
	scale                                     float32
	pan                                       gfx.Point
	imageGeometry                             components.ImageViewGeometry
	tooltip                                   components.HoverTooltip
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
	theme, m := d.owner.Theme(), d.owner.messages.FileWindow
	canvas.Fill(gfx.Rect{Width: size.Width, Height: size.Height}, theme.Background)
	var content, encoding, save ui.Box
	f := d.ui.frame.Begin(canvas, theme, gfx.Rect{Width: size.Width, Height: size.Height})
	f.Column().Fill().Pad(12).Gap(8).Children(func() {
		if d.owner.fileWindows.painting {
			d.owner.fileWindows.paintTabs(f, size)
			f.Divider().Background(theme.Border)
		}
		if d.problem != "" {
			f.Paragraph(d.problem, 4).FontSize(theme.Metrics.FontSizeS).Color(theme.Danger).LineHeight(18)
		}
		content = f.Slot().Grow(1).MinHeight(64)
		f.Row().Height(32).Gap(8).Items(ui.Center).Children(func() {
			if d.image == nil {
				encoding = f.Slot().Width(min(220, size.Width*.4)).Height(32)
			}
			status := d.footerStatus()
			f.Text(status).FontSize(theme.Metrics.FontSizeS).Color(theme.MutedForeground).Ellipsis().LineHeight(22).Grow(1).
				HitWith(hitDocumentStatus, status, gfx.HitOptions{Role: gfx.RoleGroup, SkipFocus: true, Hint: status})
			if d.image != nil {
				f.Button(hitDocumentFit, m.Fit).Small().Outline()
				f.Button(hitDocumentActual, m.Actual).Small().Outline()
				f.Button(hitDocumentZoomOut, "−").Small().Outline()
				f.Button(hitDocumentZoomIn, "+").Small().Outline()
			}
			if d.image == nil && d.ready {
				wrap := d.owner.messages.CodeEditor.WrapOn
				if d.edit.noWrap {
					wrap = d.owner.messages.CodeEditor.WrapOff
				}
				f.Button(hitDocumentWrap, wrap).Small().Outline()
			}
			f.Button(hitDocumentReload, m.Reload).Small().Outline().Disabled(d.busy || d.comparison.active)
			if d.image == nil {
				disabled := d.busy || !d.ready || !d.dirty() || d.comparison.active
				if d.local {
					f.Button(hitDocumentSave, m.Save).Small().Disabled(disabled)
				} else {
					save = f.Slot().Width(112).Height(32)
				}
			}
		})
	})
	f.End()
	if d.owner.fileWindows.painting {
		d.owner.fileWindows.paintTabsScrollbar(canvas)
	}
	d.ui.saveAnchor = gfx.Rect{}
	if d.image == nil && !d.local {
		d.ui.saveAnchor = save.Rect()
	}
	if d.image != nil {
		d.ui.imageBounds = content.Rect()
		d.ui.imageGeometry = components.PaintImageView(canvas, theme, content.Rect(), components.ImageViewOptions{
			Bitmap: gfx.Bitmap{Pix: d.image.Pix, Width: d.image.Width, Height: d.image.Height, Stride: d.image.Width * 4, ID: binary.LittleEndian.Uint64(d.revision[:8]), Version: 1}, Zoom: d.ui.zoom, Pan: d.ui.pan,
		})
	} else {
		d.ui.encodingBounds = encoding.Rect()
		d.ui.editorBounds = content.Rect()
		d.paintCodeEditor(canvas, theme, content.Rect())
		d.paintEncoding(canvas)
	}
	if d.image == nil && !d.local {
		d.paintDocumentSave(canvas)
		d.paintSaveMenu(canvas)
	}
	if d.confirm != "" {
		d.paintConfirmation(canvas)
	}
	d.notices.paint(canvas, theme, gfx.Rect{Width: size.Width, Height: size.Height})
	d.ui.tooltip.Paint(canvas, theme, gfx.Rect{Width: size.Width, Height: size.Height}, components.TooltipOptions{Layer: tooltipLayer, SideOffset: tooltipGap}, components.HoverTooltipDelay)
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
	var content ui.Box
	f := uiState.frame.Begin(canvas, theme, gfx.Rect{Width: size.Width, Height: size.Height})
	f.Column().Fill().Pad(12).Gap(8).Children(func() {
		f.Text(d.name).FontSize(18).Bold().Color(theme.Foreground).Ellipsis().LineHeight(32).
			HitWith(hitDocumentLocation, d.name, gfx.HitOptions{Role: gfx.RoleGroup, SkipFocus: true, Hint: d.source + " · " + d.location})
		if d.problem != "" {
			f.Paragraph(d.problem, 4).FontSize(theme.Metrics.FontSizeS).Color(theme.Danger).LineHeight(18)
		}
		f.Row().Height(28).Gap(10).Items(ui.Center).Children(func() {
			f.Text(m.ServerVersion).Grow(1).FontSize(theme.Metrics.FontSizeS).Weight(gfx.WeightMedium).Color(theme.MutedForeground).Ellipsis()
			f.Text(fmt.Sprintf(m.CompareAddedRemoved, d.comparison.result.Stats.Added, d.comparison.result.Stats.Removed)).
				FontSize(theme.Metrics.FontSizeS).Color(theme.MutedForeground).Ellipsis()
			f.Text(m.LocalVersion).Grow(1).TextEnd().FontSize(theme.Metrics.FontSizeS).Weight(gfx.WeightMedium).Color(theme.MutedForeground).Ellipsis()
		})
		if d.comparison.result.Stats.Coarse {
			f.Text(m.CompareCoarse).FontSize(theme.Metrics.FontSizeS).Color(d.owner.pal.warning).Ellipsis()
		}
		content = f.Slot().Grow(1).MinHeight(64)
		f.Row().Height(32).Gap(8).Items(ui.Center).Children(func() {
			status := d.comparisonStatus()
			f.Text(status).FontSize(theme.Metrics.FontSizeS).Color(theme.MutedForeground).Ellipsis().LineHeight(22).Grow(1).
				HitWith(hitDocumentStatus, status, gfx.HitOptions{Role: gfx.RoleGroup, SkipFocus: true, Hint: status})
			if d.comparison.conflict {
				f.Button(hitDocumentCompareRetry, m.CompareRetry).Small().Outline().Disabled(d.busy)
			}
			f.Button(hitDocumentReturnToEdit, m.ReturnToEdit).Small().Outline().Disabled(d.busy || d.comparison.saving)
			f.Button(hitDocumentConfirmUpload, m.Save).Small().Disabled(!d.canUploadComparison())
		})
	})
	f.End()

	uiState.editorBounds = content.Rect()
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
	if content.Rect().Width < 2*(40*cellWidth+72) {
		snapshot.Layout = components.CodeDiffStacked
	}
	if marked, ok := d.editor.Composition(); ok {
		snapshot.CompositionStart = d.editor.ByteOffset(marked.Start)
		snapshot.CompositionEnd = d.editor.ByteOffset(marked.End)
	}
	uiState.diff.Paint(canvas, theme, content.Rect(), snapshot)
	if d.comparison.confirm {
		d.paintComparisonConfirmation(canvas)
	}
	uiState.tooltip.Paint(canvas, theme, gfx.Rect{Width: size.Width, Height: size.Height}, components.TooltipOptions{Layer: tooltipLayer, SideOffset: tooltipGap}, components.HoverTooltipDelay)
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

func (d *fileDocument) paintDocumentSave(canvas *gfx.Canvas) {
	area := d.ui.saveAnchor
	if area.Width < 64 || area.Height <= 0 {
		return
	}
	theme, m := d.owner.Theme(), d.owner.messages.FileWindow
	disabled := d.busy || !d.ready || !d.dirty() || d.comparison.active
	components.BeginSplitButton(canvas, theme, area, components.SplitButtonOptions{
		PrimaryID: hitDocumentSave, MenuID: hitDocumentSaveMenu,
		Label: m.Save, MenuLabel: m.SaveOptions, MenuHint: m.CompareSave,
		Icon: lucideIcons[iconChevronDown], Variant: components.ButtonDefault,
		Size: components.ButtonSizeSmall, TriggerWidth: 32,
		Open: d.ui.saveMenuOpen, Disabled: disabled,
	}).Paint()
	d.ui.saveAnchor = area
}

func (d *fileDocument) paintSaveMenu(canvas *gfx.Canvas) {
	if !d.ui.saveMenuOpen || d.ui.saveAnchor.Width <= 0 {
		return
	}
	theme, m := d.owner.Theme(), d.owner.messages.FileWindow
	width := float32(184)
	height := float32(40)
	anchor := d.ui.saveAnchor
	viewport := gfx.Rect{Width: d.ui.size.Width, Height: d.ui.size.Height}
	bounds, side := components.PlaceOverlay(anchor, gfx.Size{Width: width, Height: height}, viewport, components.OverlayPlacement{
		Side: components.OverlayBottom, Align: components.OverlayAlignEnd, SideOffset: 4, Margin: 8,
	})
	menu := components.BeginDropdownMenu(canvas, theme, bounds, components.DropdownMenuOptions{
		Layer: popupLayer, Scope: hitDocumentSaveMenuDismiss, DismissID: hitDocumentSaveMenuDismiss,
		ID: hitDocumentSaveMenu, Side: side, Viewport: viewport, Scroll: &d.ui.saveMenu,
	})
	menu.AddItem(components.DropdownMenuItem{ID: hitDocumentCompare, Label: m.CompareSave, Icon: lucideIcons[iconColumns2]})
	menu.End()
}

func (d *fileDocument) paintComparisonConfirmation(canvas *gfx.Canvas) {
	m, theme := d.owner.messages.FileWindow, d.owner.Theme()
	viewport := gfx.Rect{Width: d.comparison.ui.size.Width, Height: d.comparison.ui.size.Height}
	components.BeginAlertDialog(canvas, theme, viewport, components.AlertDialogOptions{
		Layer: dialogLayer, Scope: dialogScope, ActionID: hitDocumentUploadConfirm, CancelID: hitDocumentCancel, InitialFocusID: hitDocumentCancel,
	}).Paint(m.SaveConfirmFinalTitle, m.SaveConfirmFinalDescription, m.Save, m.Cancel)
}

func (d *fileDocument) paintEncoding(canvas *gfx.Canvas) {
	m := d.owner.messages.FileWindow
	value := d.format.Encoding
	if value == "" {
		value = m.Auto
	}
	bounds := d.ui.encodingBounds
	height := min(float32(8+32*(len(filecontent.Encodings)+1)), max(bounds.Y-8, d.ui.size.Height-bounds.Y-bounds.Height-8))
	content := gfx.Rect{X: bounds.X, Y: bounds.Y + bounds.Height + 4, Width: bounds.Width, Height: height}
	if content.Y+height > d.ui.size.Height-4 {
		content.Y = bounds.Y - height - 4
	}
	var scroll *components.MenuScroll
	if d.encodingOpen {
		d.ui.menu.BarID = hitDocumentEncodingBar
		scroll = &d.ui.menu
	}
	control := components.BeginSelect(canvas, d.owner.Theme(), bounds, components.SelectOptions{
		TriggerID: hitDocumentEncoding, Label: m.Encoding, Value: m.Encoding + ": " + value, Open: d.encodingOpen, Disabled: d.busy || d.comparison.active,
		Content: content, Layer: popupLayer, Scope: hitDocumentEncoding, DismissID: hitDocumentEncodingDismiss, Viewport: gfx.Rect{Width: d.ui.size.Width, Height: d.ui.size.Height}, Scroll: scroll,
	})
	defer control.End()
	control.AddItem(components.SelectItem{ID: hitDocumentEncodings.At(0), Label: m.Auto, Selected: d.format.Encoding == ""})
	for index, name := range filecontent.Encodings {
		control.AddItem(components.SelectItem{ID: hitDocumentEncodings.At(index + 1), Label: name, Selected: d.format.Encoding == name})
	}
}

func (d *fileDocument) paintConfirmation(canvas *gfx.Canvas) {
	m, theme := d.owner.messages.FileWindow, d.owner.Theme()
	viewport := gfx.Rect{Width: d.ui.size.Width, Height: d.ui.size.Height}
	if d.confirm == "reload" {
		components.BeginAlertDialog(canvas, theme, viewport, components.AlertDialogOptions{Layer: dialogLayer, Scope: dialogScope, ActionID: hitDocumentDiscard, CancelID: hitDocumentCancel, ActionVariant: components.ButtonDestructive, InitialFocusID: hitDocumentCancel}).Paint(m.ConfirmTitle, m.ConfirmReload, m.Discard, m.Cancel)
		return
	}
	if d.confirm == "save" {
		dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{Layer: dialogLayer, Scope: dialogScope, CloseID: hitDocumentCancel, InitialFocusID: hitDocumentCancel, ContentSize: gfx.Size{Width: 500}, FitBody: true}).WithoutCloseButton()
		layout := dialog.Begin(m.SaveConfirmTitle, m.SaveConfirmDescription)
		dialog.Actions(layout,
			components.ButtonAction{ID: hitDocumentCancel, Label: m.Cancel, Variant: components.ButtonOutline},
			components.ButtonAction{ID: hitDocumentSaveDirect, Label: m.SaveDirect},
		)
		dialog.End()
		return
	}
	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{Layer: dialogLayer, Scope: dialogScope, CloseID: hitDocumentCancel, InitialFocusID: hitDocumentCancel, ContentSize: gfx.Size{Width: 460}, FitBody: true}).WithoutCloseButton()
	layout := dialog.Begin(m.ConfirmTitle, m.ConfirmClose)
	dialog.Actions(layout,
		components.ButtonAction{ID: hitDocumentDiscard, Label: m.Discard, Variant: components.ButtonDestructive},
		components.ButtonAction{ID: hitDocumentCancel, Label: m.Cancel, Variant: components.ButtonOutline},
		components.ButtonAction{ID: hitDocumentSaveClose, Label: m.SaveClose},
	)
	dialog.End()
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
	if d.ui.saveMenuOpen {
		switch id {
		case hitDocumentSaveMenuDismiss:
			d.ui.saveMenuOpen = false
		case hitDocumentCompare:
			d.ui.saveMenuOpen = false
			d.beginComparison()
		}
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
			d.ui.saveMenu = components.MenuScroll{}
			d.encodingOpen = false
		}
	case id == hitDocumentReload:
		if d.comparison.active {
			return
		}
		if d.dirty() {
			d.confirm = "reload"
		} else {
			d.reload()
		}
	case id == hitDocumentEncoding:
		if !d.busy && !d.comparison.active {
			d.encodingOpen = !d.encodingOpen
			d.ui.menu = components.MenuScroll{}
		}
	case id == hitDocumentEncodingDismiss:
		d.encodingOpen = false
	case id.Scope() == hitDocumentEncodings:
		index, ok := id.In(hitDocumentEncodings)
		if !ok {
			return
		}
		name := "Auto"
		if index > 0 && index <= len(filecontent.Encodings) {
			name = filecontent.Encodings[index-1]
		}
		d.chooseEncoding(name)
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
	if d.ui.saveMenuOpen {
		if event.Key == gfx.KeyEscape {
			d.ui.saveMenuOpen = false
			handled.Focus = hitDocumentSaveMenu
			return handled
		}
		if event.Key == gfx.KeyTab {
			d.ui.saveMenuOpen = false
			handled.ContinueTraversal = true
			return handled
		}
		if id, ok := d.ui.saveMenu.Navigate(event.Key, event.Target, true); ok {
			handled.Focus = id
			handled.FocusVisible = true
			return handled
		}
	}
	if d.encodingOpen {
		if event.Key == gfx.KeyEscape {
			d.encodingOpen = false
			handled.Focus = hitDocumentEncoding
			return handled
		}
		if event.Key == gfx.KeyTab {
			d.encodingOpen = false
			handled.ContinueTraversal = true
			return handled
		}
		if id, ok := d.ui.menu.Navigate(event.Key, event.Target, true); ok {
			handled.Focus = id
			handled.FocusVisible = true
			return handled
		}
	}
	if event.Target == hitDocumentEncoding && !d.comparison.active && (event.Key == gfx.KeyDown || event.Key == gfx.KeyUp) {
		d.encodingOpen = true
		return handled
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
	if d.encodingOpen {
		d.ui.menu.Wheel(event)
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	if d.ui.saveMenuOpen {
		d.ui.saveMenu.Wheel(event)
		return gfx.EventResult{Handled: true, Repaint: true}
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
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	if d.note != nil {
		return d.noteWindowPointer(event)
	}

	if d.confirm != "" {
		return gfx.EventResult{}
	}
	if d.encodingOpen && (event.Target == hitDocumentEncodingBar || d.ui.menu.Dragging()) {
		d.ui.menu.Drag(event)
		return gfx.EventResult{Handled: true, Repaint: true}
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
