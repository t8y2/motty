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
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"
	"motty/internal/filecontent"
)

var documentHits = tree.Feature("document-window")
var (
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
	hitDocumentLocation        = tree.ID(documentHits, "location")
	hitDocumentStatus          = tree.ID(documentHits, "status")
)

type fileDocumentUI struct {
	marks                                     []components.MarkdownCodeRun
	highlighter                               func(string, string, []components.CodeEditorRun) []components.CodeEditorRun
	frame                                     flex.View
	code                                      components.CodeEditor
	input                                     app.TextInput
	editorBounds, encodingBounds, imageBounds ui.Rect
	size                                      ui.Size
	menu                                      components.MenuScroll
	zoom                                      float32
	scale                                     float32
	pan                                       ui.Point
	imageGeometry                             components.ImageViewGeometry
	tooltip                                   components.HoverTooltip
}

func (d *fileDocument) paint(canvas *ui.Canvas, size ui.Size) {
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
	canvas.Fill(ui.Rect{Width: size.Width, Height: size.Height}, theme.Background)
	var content, encoding flex.Box
	f := d.ui.frame.Begin(canvas, theme, ui.Rect{Width: size.Width, Height: size.Height})
	f.Column().Fill().Pad(12).Gap(8).Children(func() {
		f.Text(d.name).FontSize(18).Bold().Color(theme.Foreground).Ellipsis().LineHeight(32).
			HitWith(hitDocumentLocation, d.name, ui.HitOptions{Role: ui.RoleGroup, SkipFocus: true, Hint: d.source + " · " + d.location})
		if d.problem != "" {
			f.Paragraph(d.problem, 4).FontSize(theme.Metrics.FontSizeS).Color(theme.Danger).LineHeight(18)
		}
		content = f.Slot().Grow(1).MinHeight(64)
		f.Row().Height(32).Gap(8).Items(flex.Center).Children(func() {
			if d.image == nil {
				encoding = f.Slot().Width(min(220, size.Width*.4)).Height(32)
			}
			status := d.footerStatus()
			f.Text(status).FontSize(theme.Metrics.FontSizeS).Color(theme.MutedForeground).Ellipsis().LineHeight(22).Grow(1).
				HitWith(hitDocumentStatus, status, ui.HitOptions{Role: ui.RoleGroup, SkipFocus: true, Hint: status})
			if d.image != nil {
				f.Button(hitDocumentFit, m.Fit).Small().Outline()
				f.Button(hitDocumentActual, m.Actual).Small().Outline()
				f.Button(hitDocumentZoomOut, "−").Small().Outline()
				f.Button(hitDocumentZoomIn, "+").Small().Outline()
			}
			f.Button(hitDocumentReload, m.Reload).Small().Outline().Disabled(d.busy)
			if d.image == nil {
				f.Button(hitDocumentSave, m.Save).Small().Disabled(d.busy || !d.ready || !d.dirty())
			}
		})
	})
	f.End()
	if d.image != nil {
		d.ui.imageBounds = content.Rect()
		d.ui.imageGeometry = components.PaintImageView(canvas, theme, content.Rect(), components.ImageViewOptions{
			Bitmap: ui.Bitmap{Pix: d.image.Pix, Width: d.image.Width, Height: d.image.Height, Stride: d.image.Width * 4, ID: binary.LittleEndian.Uint64(d.revision[:8]), Version: 1}, Zoom: d.ui.zoom, Pan: d.ui.pan,
		})
	} else {
		d.ui.encodingBounds = encoding.Rect()
		d.ui.editorBounds = content.Rect()
		value := d.editor.String()
		selection := d.editor.Selection()
		focused := canvas.Focused() == hitDocumentInput
		snapshot := components.CodeEditorSnapshot{TextareaSnapshot: components.TextareaSnapshot{
			ID: hitDocumentInput, Label: m.Title, Value: value, Placeholder: m.ChooseEncoding, ReadOnly: d.busy || !d.ready, Focused: focused,
			HideEditorText: true, ShowCaret: selection.Start == selection.End, Layout: &d.ui.code.Layout,
			SelectionStart: aiByteOffset(value, selection.Start), SelectionEnd: aiByteOffset(value, selection.End), Caret: aiByteOffset(value, d.editor.Caret()),
		}, Language: documentLanguage(d.name), Highlight: d.ui.highlighter, ScrollbarID: hitDocumentScroll}
		if marked, ok := d.editor.Composition(); ok {
			snapshot.CompositionStart = aiByteOffset(value, marked.Start)
			snapshot.CompositionEnd = aiByteOffset(value, marked.End)
		}
		d.ui.code.Paint(canvas, theme, content.Rect(), snapshot)
		d.paintEncoding(canvas)
	}
	if d.confirm != "" {
		d.paintConfirmation(canvas)
	}
	d.notices.paint(canvas, theme, ui.Rect{Width: size.Width, Height: size.Height})
	d.ui.tooltip.Paint(canvas, theme, ui.Rect{Width: size.Width, Height: size.Height}, components.TooltipOptions{Layer: tooltipLayer, SideOffset: tooltipGap}, components.HoverTooltipDelay)
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
	if status == "" {
		return ending
	}
	return ending + " · " + status
}

func (d *fileDocument) paintEncoding(canvas *ui.Canvas) {
	m := d.owner.messages.FileWindow
	value := d.format.Encoding
	if value == "" {
		value = m.Auto
	}
	bounds := d.ui.encodingBounds
	height := min(float32(8+32*(len(filecontent.Encodings)+1)), max(bounds.Y-8, d.ui.size.Height-bounds.Y-bounds.Height-8))
	content := ui.Rect{X: bounds.X, Y: bounds.Y + bounds.Height + 4, Width: bounds.Width, Height: height}
	if content.Y+height > d.ui.size.Height-4 {
		content.Y = bounds.Y - height - 4
	}
	var scroll *components.MenuScroll
	if d.encodingOpen {
		d.ui.menu.BarID = hitDocumentEncodingBar
		scroll = &d.ui.menu
	}
	control := components.BeginSelect(canvas, d.owner.Theme(), bounds, components.SelectOptions{
		TriggerID: hitDocumentEncoding, Label: m.Encoding, Value: m.Encoding + ": " + value, Open: d.encodingOpen, Disabled: d.busy,
		Content: content, Layer: popupLayer, Scope: hitDocumentEncoding, DismissID: hitDocumentEncodingDismiss, Viewport: ui.Rect{Width: d.ui.size.Width, Height: d.ui.size.Height}, Scroll: scroll,
	})
	defer control.End()
	control.AddItem(components.SelectItem{ID: hitDocumentEncodings.At(0), Label: m.Auto, Selected: d.format.Encoding == ""})
	for index, name := range filecontent.Encodings {
		control.AddItem(components.SelectItem{ID: hitDocumentEncodings.At(index + 1), Label: name, Selected: d.format.Encoding == name})
	}
}

func (d *fileDocument) paintConfirmation(canvas *ui.Canvas) {
	m, theme := d.owner.messages.FileWindow, d.owner.Theme()
	viewport := ui.Rect{Width: d.ui.size.Width, Height: d.ui.size.Height}
	if d.confirm == "reload" {
		components.BeginAlertDialog(canvas, theme, viewport, components.AlertDialogOptions{Layer: dialogLayer, Scope: dialogScope, ActionID: hitDocumentDiscard, CancelID: hitDocumentCancel, ActionVariant: components.ButtonDestructive, InitialFocusID: hitDocumentCancel}).Paint(m.ConfirmTitle, m.ConfirmReload, m.Discard, m.Cancel)
		return
	}
	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{Layer: dialogLayer, Scope: dialogScope, CloseID: hitDocumentCancel, InitialFocusID: hitDocumentCancel, ContentSize: ui.Size{Width: 460}, FitBody: true}).WithoutCloseButton()
	layout := dialog.Begin(m.ConfirmTitle, m.ConfirmClose)
	dialog.Actions(layout,
		components.ButtonAction{ID: hitDocumentDiscard, Label: m.Discard, Variant: components.ButtonDestructive},
		components.ButtonAction{ID: hitDocumentCancel, Label: m.Cancel, Variant: components.ButtonOutline},
		components.ButtonAction{ID: hitDocumentSaveClose, Label: m.SaveClose},
	)
	dialog.End()
}
func (d *fileDocument) click(id ui.ID) {
	if d.notices.click(id) {
		return
	}
	if d.confirm != "" {
		switch id {
		case hitDocumentCancel:
			d.confirm = ""
			d.allowClose = nil
			d.afterClosed = nil
			d.closeAfter = false
		case hitDocumentSaveClose:
			d.save(true)
		case hitDocumentDiscard:
			if d.confirm == "reload" {
				d.reload()
			} else {
				d.closeAllowed()
			}
		}
		return
	}
	switch {
	case id == hitDocumentSave:
		d.save(false)
	case id == hitDocumentReload:
		if d.dirty() {
			d.confirm = "reload"
		} else {
			d.reload()
		}
	case id == hitDocumentEncoding:
		if !d.busy {
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
		d.ui.pan = ui.Point{}
	case id == hitDocumentActual:
		d.ui.zoom = 1
		d.ui.pan = ui.Point{}
	case id == hitDocumentZoomIn:
		d.zoom(1.25)
	case id == hitDocumentZoomOut:
		d.zoom(.8)
	}
}

func (d *fileDocument) zoom(factor float32) {
	zoom := d.ui.zoom
	if zoom == 0 {
		zoom = d.ui.imageGeometry.Scale * d.ui.pixelScale()
	}
	d.ui.zoom = max(min(zoom*factor, 32), .01)
	d.ui.pan = ui.Point{}
}
func (view *fileDocumentUI) pixelScale() float32 {
	if view.imageGeometry.Scale <= 0 {
		return 1
	}
	return view.scale
}

func (d *fileDocument) textInput(context app.TextInputContext) *app.TextInput {
	if context.ID != hitDocumentInput || !d.ready || d.busy || d.confirm != "" {
		return nil
	}
	d.ui.input.Editor = nil
	d.ui.input.State = app.TextStateOf(&d.editor)
	d.ui.input.OnEvent = func(event app.TextEvent) (app.TextState, error) {
		if d.ready && !d.busy && d.confirm == "" {
			if event.Kind == app.TextInsert || event.Kind == app.TextPaste {
				event.Text = strings.ReplaceAll(strings.ReplaceAll(event.Text, "\r\n", "\n"), "\r", "\n")
			}
			event.Apply(&d.editor)
		}
		return app.TextStateOf(&d.editor), nil
	}
	return &d.ui.input
}

func (d *fileDocument) key(event ui.KeyEvent) ui.EventResult {
	if event.Release {
		return ui.EventResult{}
	}
	handled := ui.EventResult{Handled: true, Repaint: true}
	if d.confirm != "" {
		if event.Key == ui.KeyEscape {
			d.click(hitDocumentCancel)
			return handled
		}
		return ui.EventResult{}
	}
	if d.encodingOpen {
		if event.Key == ui.KeyEscape {
			d.encodingOpen = false
			handled.Focus = hitDocumentEncoding
			return handled
		}
		if event.Key == ui.KeyTab {
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
	if event.Target == hitDocumentEncoding && (event.Key == ui.KeyDown || event.Key == ui.KeyUp) {
		d.encodingOpen = true
		return handled
	}
	if event.Target == hitDocumentInput {
		if movement, extend := components.TextareaMovementForKey(event.Key, event.Modifiers); movement != components.TextareaMoveNone {
			value := d.editor.String()
			offset := d.ui.code.Layout.Move(value, aiByteOffset(value, d.editor.Caret()), movement)
			d.editor.MoveTo(utf8.RuneCountInString(value[:offset]), extend)
			return handled
		}
	}
	return ui.EventResult{}
}

func (d *fileDocument) shortcut(event ui.KeyEvent) ui.EventResult {
	if event.Modifiers&(ui.ModifierControl|ui.ModifierMeta) == 0 || event.Release {
		return ui.EventResult{}
	}
	switch strings.ToLower(event.Text) {
	case "s":
		if d.confirm == "" {
			d.save(false)
		}
		return ui.EventResult{Handled: true, Repaint: true}
	case "w":
		d.window.RequestClose()
		return ui.EventResult{Handled: true, Repaint: true}
	}
	return ui.EventResult{}
}

func (d *fileDocument) scroll(event ui.ScrollEvent) ui.EventResult {
	if d.confirm != "" {
		return ui.EventResult{Handled: true}
	}
	if d.encodingOpen {
		d.ui.menu.Wheel(event)
		return ui.EventResult{Handled: true, Repaint: true}
	}
	if d.image != nil {
		if event.Modifiers&(ui.ModifierControl|ui.ModifierMeta) != 0 {
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
		d.ui.code.Layout.ScrollBy(event.DeltaY)
	}
	return ui.EventResult{Handled: true, Repaint: true}
}

func (d *fileDocument) pointer(event ui.PointerEvent) ui.EventResult {
	if d.confirm != "" {
		return ui.EventResult{}
	}
	if d.encodingOpen && (event.Target == hitDocumentEncodingBar || d.ui.menu.Dragging()) {
		d.ui.menu.Drag(event)
		return ui.EventResult{Handled: true, Repaint: true}
	}
	return d.ui.code.Pointer(event)
}

func (d *fileDocument) codeColors() [components.MarkdownCodeClasses]ui.Color {
	pal := d.owner.pal
	ansi := func(index int) ui.Color { return ui.RGB(pal.ansi[index][0], pal.ansi[index][1], pal.ansi[index][2]) }
	return [components.MarkdownCodeClasses]ui.Color{components.CodeComment: pal.faint, components.CodeString: ansi(2), components.CodeNumber: ansi(3), components.CodeKeyword: ansi(5), components.CodeFunction: ansi(4), components.CodeVariable: ansi(6), components.CodeFlag: ansi(6), components.CodeKey: ansi(4), components.CodeAdded: ansi(2), components.CodeRemoved: ansi(1)}
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
