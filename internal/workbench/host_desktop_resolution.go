package workbench

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"

	"motty/internal/desktop/rdp"
)

// An RDP host asks the server for a desktop the size of its pane unless it
// chose a resolution: then every connection asks for that size, and the
// picture is scaled to the pane like the picture of a server that cannot
// follow it. Zero by zero is the automatic size every host had before.

// desktopSize is a remote resolution choice. The zero value is automatic.
type desktopSize struct{ width, height int }

// customDesktopSize stands for the choice that reads the size from the field
// beside the select; no real size is negative.
var customDesktopSize = desktopSize{-1, -1}

// desktopSizeChoices are what the resolution select lists, first the automatic
// size and last the custom one.
var desktopSizeChoices = [...]desktopSize{
	{}, {1280, 720}, {1600, 900}, {1920, 1080}, {2560, 1440}, {3840, 2160}, customDesktopSize,
}

// fixedDesktopSize is the size a host asks for, and false when it follows its
// pane. A size the protocol cannot carry, such as one left by hand in the
// catalog, is automatic too.
func (host Host) fixedDesktopSize() (desktopSize, bool) {
	if host.Protocol != protocolRDP || !rdp.ValidSize(host.DesktopWidth, host.DesktopHeight) {
		return desktopSize{}, false
	}
	return desktopSize{host.DesktopWidth &^ 1, host.DesktopHeight}, true
}

// savedDesktopSize is the width and height written for a host: zero for an
// automatic size or one that is not valid.
func savedDesktopSize(width, height int) (int, int) {
	if !rdp.ValidSize(width, height) {
		return 0, 0
	}
	return width &^ 1, height
}

// parseDesktopSize reads "1920 × 1080": two whole numbers joined by ×, x or *.
func parseDesktopSize(text string) (desktopSize, bool) {
	text = strings.NewReplacer("×", "x", "X", "x", "*", "x").Replace(text)
	left, right, found := strings.Cut(text, "x")
	if !found {
		return desktopSize{}, false
	}
	width, err := strconv.Atoi(strings.TrimSpace(left))
	if err != nil {
		return desktopSize{}, false
	}
	height, err := strconv.Atoi(strings.TrimSpace(right))
	if err != nil || !rdp.ValidSize(width, height) {
		return desktopSize{}, false
	}
	return desktopSize{width &^ 1, height}, true
}

func (size desktopSize) String() string { return fmt.Sprintf("%d × %d", size.width, size.height) }

// desktopSizeChoice is the choice that shows a stored size: the preset it is
// one of, custom for any other size, automatic for none.
func desktopSizeChoice(size desktopSize) desktopSize {
	if size == (desktopSize{}) {
		return size
	}
	for _, preset := range desktopSizeChoices[1 : len(desktopSizeChoices)-1] {
		if preset == size {
			return preset
		}
	}
	return customDesktopSize
}

func (state *State) desktopSizeLabel(size desktopSize) string {
	switch size {
	case desktopSize{}:
		return state.messages.Desktop.ResolutionAuto
	case customDesktopSize:
		return state.messages.Desktop.ResolutionCustom
	}
	return size.String()
}

// resolutionSize is the size the form describes, and false when its custom
// field does not hold one.
func (editor *hostEditor) resolutionSize() (desktopSize, bool) {
	if editor.resolution != customDesktopSize {
		return editor.resolution, true
	}
	return parseDesktopSize(editor.value(fieldResolution))
}

// hostResolutionBody is where the resolution rows of the host form were laid out.
type hostResolutionBody struct {
	field  ui.FormFieldState
	box    ui.Box
	choice desktopSize
	custom ui.Box
}

// declareResolutionSelect lays out the resolution select of an RDP host, beside
// the color depth. A custom choice is filled with a common size, so its field
// never opens empty.
func (state *State) declareResolutionSelect(c ui.C, rowHeight float32) {
	editor, form, m := &state.editor, &state.editor.form, state.messages.Desktop
	b := &form.resolutionBody
	b.choice = editor.resolution
	ui.FormField(c, &b.field, m.ResolutionField).FillWidth().Height(rowHeight).Children(func() {
		b.box = ui.Select(c, &b.choice, desktopSizeChoices[:], state.desktopSizeLabel).Label(m.ResolutionField)
		if b.box.Changed() {
			editor.resolution = b.choice
			if editor.resolution == customDesktopSize && editor.value(fieldResolution) == "" {
				editor.field(fieldResolution).Insert(desktopSizeChoices[3].String())
			}
			state.dialogError = ""
		}
	})
}

// declareResolutionCustom lays out the size field under the selects when the
// choice is a custom one.
func (state *State) declareResolutionCustom(c ui.C, rowHeight float32) {
	editor, form := &state.editor, &state.editor.form
	form.resolutionBody.custom = ui.Slot(c).Height(rowHeight)
	state.declareHostTextField(c, form.resolutionBody.custom, &form.fieldResolution, hitDialogResolution, &editor.resolutionCustom,
		state.messages.Desktop.ResolutionCustomField, desktopSizeChoices[3].String(), false, rowHeight)
}

var (
	hitDesktopResolutionClose  = tree.ID(desktopHits, "resolution-close")
	hitDesktopResolutionCancel = tree.ID(desktopHits, "resolution-cancel")
	hitDesktopResolutionSubmit = tree.ID(desktopHits, "resolution-submit")
	hitDesktopResolutionField  = tree.ID(desktopHits, "resolution-field")
)

// desktopResolutionDialog types the custom remote resolution of an open RDP
// session, from its quality menu.
type desktopResolutionDialog struct {
	session uint32
	editor  text.Editor
	form    ui.FormFieldState
	input   ui.Field
	bound   bool
}

// openDesktopResolutionDialog asks for a resolution, starting from the one the
// host has.
func (state *State) openDesktopResolutionDialog(session *Session) {
	if state.modal != modalNone {
		return
	}
	state.desktopResolution = desktopResolutionDialog{session: session.ID}
	size, fixed := session.host.fixedDesktopSize()
	if !fixed {
		size = desktopSizeChoices[3]
	}
	state.desktopResolution.editor.Insert(size.String())
	state.desktopResolution.editor.SetSelection(text.Range{End: state.desktopResolution.editor.RuneCount()})
	state.dialogError = ""
	state.modal = modalDesktopResolution
}

func (state *State) clickDesktopResolutionDialog(id gfx.ID) {
	switch id {
	case hitDesktopResolutionClose, hitDesktopResolutionCancel:
		state.modal = modalNone
		state.dialogError = ""
	case hitDesktopResolutionSubmit:
		size, ok := parseDesktopSize(state.desktopResolution.editor.String())
		if !ok {
			state.dialogError = state.messages.Desktop.ErrResolution
			return
		}
		state.modal = modalNone
		state.dialogError = ""
		if session, found := state.sessionByIDPointer(state.desktopResolution.session); found {
			state.setDesktopQuality(session, func(host *Host) { host.DesktopWidth, host.DesktopHeight = size.width, size.height })
		}
	}
}

// desktopResolutionKey makes Enter in the field, or on the button, apply.
func (state *State) desktopResolutionKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if state.modal != modalDesktopResolution || event.Key != gfx.KeyEnter || event.Repeat {
		return gfx.EventResult{}, false
	}
	if event.Target != hitDesktopResolutionField && event.Target != hitDesktopResolutionSubmit {
		return gfx.EventResult{}, false
	}
	state.clickDesktopResolutionDialog(hitDesktopResolutionSubmit)
	return gfx.EventResult{Handled: true, Repaint: true}, true
}

func (state *State) declareDesktopResolutionDialog(c ui.C) {
	m := state.messages.Desktop
	dialog := &state.desktopResolution
	// No scrim dismiss: a press beside the form must not discard its input.
	viewport := dialogViewport(c)
	height := dialogContentHeight(c.Canvas(), *c.Theme(), viewport, 460, m.QualityReconnectNote, dialogRowHeight(c.Theme()))
	box := ui.Dialog(c, ui.DialogSpec{
		Title: m.ResolutionField, Description: m.QualityReconnectNote, Width: 460, Height: height, Layer: dialogLayer,
		Scope: dialogScope, CloseID: hitDesktopResolutionClose, InitialFocusID: hitDesktopResolutionField,
	})
	box.Body(func() {
		if !dialog.bound {
			dialog.input.Bind(&dialog.editor)
			dialog.bound = true
		}
		dialog.form.For, dialog.form.Error, dialog.form.Invalid = hitDesktopResolutionField, state.dialogError, state.dialogError != ""
		ui.FormField(c, &dialog.form, m.ResolutionCustomField).FillWidth().Height(dialogRowHeight(c.Theme())).Children(func() {
			ui.TextField(c, &dialog.input, m.ResolutionCustomField, desktopSizeChoices[3].String(), ui.WithID(hitDesktopResolutionField))
		})
	})
	box.Footer(func() {
		state.dialogActionFooter(c,
			components.ButtonAction{ID: hitDesktopResolutionCancel, Label: state.messages.Cancel, Variant: components.ButtonSecondary},
			components.ButtonAction{ID: hitDesktopResolutionSubmit, Label: m.ResolutionApply, Variant: components.ButtonDefault},
		)
	})
}

// declareDesktopResolutionMenu declares the menu under the bar's resolution
// button of an RDP session: the sizes the host editor offers. A choice is saved
// with the host and asks to connect again.
func (state *State) declareDesktopResolutionMenu(c ui.C, session *Session, view *desktopView, anchor ui.Box) {
	if !view.resolutionMenu {
		return
	}
	m := state.messages.Desktop
	current := desktopSize{}
	if size, fixed := session.host.fixedDesktopSize(); fixed {
		current = desktopSizeChoice(size)
	}
	ui.PopupLayer(c, dialogLayer)
	ui.Menu(c, anchor, &view.resolutionMenu, func(menu *ui.MenuItems) {
		menu.WidthRange(240, 240)
		menu.Label(m.QualityReconnectNote)
		for _, choice := range desktopSizeChoices {
			if !menu.Item(state.desktopSizeLabel(choice)).Chosen(current == choice).Clicked() {
				continue
			}
			if choice == customDesktopSize {
				c.After(func() { state.openDesktopResolutionDialog(session) })
				continue
			}
			c.After(func() {
				state.setDesktopQuality(session, func(host *Host) { host.DesktopWidth, host.DesktopHeight = choice.width, choice.height })
			})
		}
	})
}
