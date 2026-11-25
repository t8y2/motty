package workbench

import (
	"math"

	"github.com/vivaui/viva/base"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

// A form dialog is one modal with up to a few text fields, an error line and a
// submit button, for the actions that only need a password or an address. The
// caller says what the fields are and what submitting does; the dialog does the
// rest, so each such action is a few lines instead of another dialog.

// formDialogHits is the place of the controls this file paints.
var formDialogHits = tree.Feature("form-dialog")

var (
	hitFormFieldList = tree.Sub(formDialogHits, tree.Name("form-field"))
	hitFormSubmit    = tree.ID(formDialogHits, "form-submit")
	hitFormTest      = tree.ID(formDialogHits, "form-test")
	hitFormBack      = tree.ID(formDialogHits, "form-back")
	hitFormCancel    = tree.ID(formDialogHits, "form-cancel")
	hitFormClose     = tree.ID(formDialogHits, "form-close")
	hitFormCheckbox  = tree.ID(formDialogHits, "form-checkbox")
)

const maxFormFields = 5

// formTextareaHeight is the height of a multiline field's slot, its title
// included: about seven rows of text under the title.
const formTextareaHeight = 190

// formField is one input of a form. A multiline field is a box of several rows
// that takes Enter as a new line, for text that is pasted whole, a private key
// for one.
type formField struct {
	label, placeholder string
	secure             bool
	multiline          bool
	editor             text.Editor
	// pageField and pageBound are the page field declared over the editor; row is its
	// labelled row and shown reveals a secret one. A multiline field is a page text
	// area, area, bound to the same editor.
	pageField ui.Field
	pageBound bool
	row       ui.FormFieldState
	shown     bool
	area      dialogArea
}

// formDialog is the open form. run is called when the user submits: it returns
// the text of an error to show (the form stays open) or "" when the work is done
// or started, which closes the form.
type formDialog struct {
	title, description string
	submit             string
	submitLabel        func(*formDialog) string
	backLabel          string
	back               func(*State, *formDialog)
	danger             bool
	// test, if set, gives the form a button that tries what the fields say
	// without saving them. It starts its network work and returns "" at once;
	// its result comes back on the window thread.
	test        func(*State, *formDialog) string
	testRunning bool
	// testNote is what a test that went through says in the form; the status
	// line behind a dialog is not seen.
	testNote string
	// A form may ask for explicit consent after its fields. The caller owns
	// the label, explanation and saved value; other forms have no checkbox.
	checkLabel, checkDescription string
	checked                      bool
	fields                       [maxFormFields]formField
	count                        int
	dialogError                  string
	run                          func(*State, *formDialog) string
	// cancel, if set, runs instead of closing when the user cancels; a form that
	// stands between two screens uses it to go back.
	cancel func(*State)
}

// openForm shows a form. The fields are copied in; use the returned form to
// preset their text.
func (state *State) openForm(form formDialog, fields ...formField) *formDialog {
	state.formSerial++
	state.form = form
	state.form.count = copy(state.form.fields[:], fields)
	for index := range state.form.fields {
		// A field copied from another form is bound to that form's editor.
		state.form.fields[index].pageBound, state.form.fields[index].shown = false, false
	}
	state.form.dialogError = ""
	state.form.testRunning = false
	state.modal = modalForm
	return &state.form
}

// value is the trimmed-free text of field index.
func (form *formDialog) value(index int) string {
	if index < 0 || index >= form.count {
		return ""
	}
	return form.fields[index].editor.String()
}

func (form *formDialog) set(index int, value string) {
	form.fields[index].editor = text.Editor{}
	form.fields[index].editor.Insert(value)
}

// slotHeight is the room field index takes in the dialog, its title and the gap
// below it included.
func (form *formDialog) slotHeight(theme *base.Theme, index int) float32 {
	if form.fields[index].multiline {
		return formTextareaHeight + 4
	}
	return dialogRowHeight(theme)
}

// multilineField is the field that has the focus when it is a multiline one.
func (state *State) multilineField(hit gfx.ID) *formField {
	if state.modal != modalForm || hit.Scope() != hitFormFieldList {
		return nil
	}
	if field := &state.form.fields[uint32(item(hit))]; field.multiline {
		return field
	}
	return nil
}

func (state *State) formEditor(hit gfx.ID) *text.Editor {
	if hit.Scope() != hitFormFieldList {
		return nil
	}
	return &state.form.fields[uint32(item(hit))].editor
}

func (state *State) clickForm(id gfx.ID) {
	switch id {
	case hitFormCheckbox:
		if state.form.checkLabel != "" {
			state.form.checked = !state.form.checked
		}
	case hitFormSubmit:
		state.submitForm()
	case hitFormTest:
		if message := state.startFormTest(); message != "" {
			state.form.dialogError = message
		}
	case hitFormBack:
		if state.form.back != nil {
			form := state.form
			state.clearFormTestStatus()
			state.closeModal()
			form.back(state, &form)
		}
	case hitFormCancel, hitFormClose:
		cancel := state.form.cancel
		state.clearFormTestStatus()
		state.closeModal()
		if cancel != nil {
			cancel(state)
		}
	}
}

// startFormTest runs the form's optional test without saving. The form stays
// open; the test's asynchronous result also stays in this exact form.
func (state *State) startFormTest() string {
	if state.form.test == nil || state.form.testRunning {
		return ""
	}
	message := state.form.test(state, &state.form)
	if message != "" {
		return message
	}
	state.form.testRunning = true
	state.form.dialogError, state.form.testNote = "", ""
	state.status = state.messages.Tools.SyncTestRunning
	return ""
}

// clearFormTestStatus takes "testing" off the status line when no result is
// going to replace it.
func (state *State) clearFormTestStatus() {
	if state.status == state.messages.Tools.SyncTestRunning {
		state.status = ""
	}
}

// finishFormTest reports an asynchronous form test in the open form. A closed
// or replaced form drops the stale result.
func (state *State) finishFormTest(serial uint32, err error) {
	if state.modal != modalForm || state.formSerial != serial {
		state.clearFormTestStatus()
		return
	}
	state.form.testRunning = false
	if err != nil {
		state.clearFormTestStatus()
		state.form.dialogError = state.backupErrorText(err)
		return
	}
	state.form.dialogError, state.form.testNote = "", state.messages.Tools.SyncTestOK
	state.status = state.messages.Tools.SyncTestOK
}

func (state *State) submitForm() {
	form := &state.form
	if form.run == nil {
		state.closeModal()
		return
	}
	serial := state.formSerial
	message := form.run(state, form)
	if state.formSerial != serial || state.modal != modalForm {
		return // run opened another form or dialog; that one stands
	}
	if message != "" {
		form.dialogError = message
		return
	}
	state.closeModal()
}

// bodyHeight is the dialog body's height: its fields and consent box. The error
// under them, if any, adds its wrapped lines (formProblemHeight).
func (form *formDialog) bodyHeight(theme *base.Theme) float32 {
	height := float32(0)
	for index := 0; index < form.count; index++ {
		height += form.slotHeight(theme, index)
	}
	if form.checkLabel != "" {
		height += 72
	}
	return height
}

// The form dialog's width, the room its body keeps on each side of its fields,
// and the error line under them. The error's words can be a server's or the
// network stack's and are not known when the dialog is written, so the dialog is
// sized to the lines they wrap to instead of cutting them at its edge.
const (
	formWidth       = float32(540)
	formBodyPad     = float32(4)
	formProblemLine = float32(18)
	formProblemGap  = float32(12)
)

// formProblemHeight is the room the error or test note under the form's fields
// takes: its gap above and as many lines as its words need at the body's width.
func (state *State) formProblemHeight(canvas *gfx.Canvas, problem string) float32 {
	if problem == "" {
		return 0
	}
	metrics := state.Theme().Metrics
	width := formWidth - 2*(metrics.PadSurface+metrics.GapItem) - 2*formBodyPad
	// The scratch holds the lines of a reason of ordinary length; a longer one
	// allocates, which is rare enough not to keep room for.
	var scratch [8]string
	lines := canvas.WrapLines(scratch[:0], problem, metrics.FontSizeS, gfx.WeightMedium, max(width, 1), math.MaxInt32)
	return formProblemGap + float32(len(lines))*formProblemLine
}

// formProblem is what the form says under its fields: the error of the last
// submit, or the note of a test that went through.
func (form *formDialog) formProblem() string {
	if form.dialogError != "" {
		return form.dialogError
	}
	return form.testNote
}

// problemWrapC declares a form's error under its fields: its words are the
// server's or the network stack's, not the application's, so it takes as many
// lines as the room needs. The dialog is sized with the same lines
// (formProblemHeight).
func (state *State) problemWrapC(c ui.C, problem string) {
	if problem != "" {
		ui.Paragraph(c, problem, 0).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(state.pal.danger).LineHeight(formProblemLine)
	}
}

// declareFormDialog declares the open form as a dialog of the page.
func (state *State) declareFormDialog(c ui.C) {
	form := &state.form
	initial := hitFormSubmit
	if form.count > 0 {
		initial = hitFormFieldList.At(0)
	}
	viewport := dialogViewport(c)
	bodyHeight := form.bodyHeight(c.Theme()) + state.formProblemHeight(c.Canvas(), form.formProblem())
	height := dialogContentHeight(c.Canvas(), *c.Theme(), viewport, formWidth, form.description, bodyHeight)
	// No scrim dismiss: a press beside the form must not discard its input.
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: form.title, Description: form.description, Width: formWidth, Height: height, Layer: dialogLayer,
		Scope: dialogScope, CloseID: hitFormClose, InitialFocusID: initial,
	})
	dialog.Body(func() { state.declareFormDialogBody(c) })
	dialog.Footer(func() {
		actions := []components.ButtonAction{
			{ID: hitFormCancel, Label: state.messages.Cancel, Variant: components.ButtonSecondary},
		}
		if form.back != nil {
			actions = append(actions, components.ButtonAction{ID: hitFormBack, Label: form.backLabel, Variant: components.ButtonOutline})
		}
		if form.test != nil {
			actions = append(actions, components.ButtonAction{ID: hitFormTest, Label: state.messages.Tools.SyncTest, Variant: components.ButtonOutline, Disabled: form.testRunning})
		}
		variant := components.ButtonDefault
		if form.danger {
			variant = components.ButtonDestructive
		}
		label := form.submit
		if form.submitLabel != nil {
			label = form.submitLabel(form)
		}
		actions = append(actions, components.ButtonAction{ID: hitFormSubmit, Label: label, Variant: variant})
		state.dialogActionFooter(c, actions...)
	})
}

// declareFormDialogBody lays out a form: its fields, its consent box and the
// error or test note under them, and paints the fields into their slots.
func (state *State) declareFormDialogBody(c ui.C) {
	form := &state.form
	column := ui.Column(c).Fill().PadSides(4, 4, 0, 4)
	column.Children(func() {
		for index := 0; index < form.count; index++ {
			field := &form.fields[index]
			if field.multiline {
				height := form.slotHeight(c.Theme(), index) - 4
				ui.Column(c).Height(height).Shrink(0).MarginSides(0, 0, 4, 0).Children(func() {
					field.area.declare(c, hitFormFieldList.At(index), &field.editor, field.label, field.placeholder, height)
				})
			} else {
				state.declareFormField(c, index)
			}
		}
		if form.checkLabel != "" {
			ui.Column(c).Height(72).Gap(2).PadSides(8, 0, 4, 0).Children(func() {
				ui.Row(c).Height(24).Items(ui.Center).Children(func() {
					ui.Checkbox(c, state.pageFlag(form.checked), form.checkLabel).Hit(hitFormCheckbox, form.checkLabel)
				})
				ui.Paragraph(c, form.checkDescription, 2).FontSize(metrics.FontSizeS).LineHeight(16).Color(state.pal.muted).MarginSides(0, 0, 0, 28)
			})
		}
		state.problemWrapC(c, form.dialogError)
		if form.dialogError == "" && form.testNote != "" {
			ui.Paragraph(c, form.testNote, 0).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(state.pal.success).LineHeight(formProblemLine)
		}
	})
}

// declareFormField declares a single-line field i of the form: a labelled row around a page
// text bound to the field's editor. A secret one has a button that reveals it.
func (state *State) declareFormField(c ui.C, i int) {
	field, m := &state.form.fields[i], state.messages
	if !field.pageBound {
		field.pageField.Bind(&field.editor)
		field.pageBound = true
	}
	field.pageField.Secure = field.secure && !field.shown
	ui.FormField(c, &field.row, field.label).FillWidth().Height(dialogRowHeight(c.Theme())).Children(func() {
		box := ui.TextField(c, &field.pageField, field.label, field.placeholder, ui.WithID(hitFormFieldList.At(i)))
		if field.secure {
			icon, name := lucideIcons[iconEye], m.ShowSecret
			if field.shown {
				icon, name = lucideIcons[iconEyeOff], m.HideSecret
			}
			box.Action(icon, name)
			if box.ActionClicked() {
				field.shown = !field.shown
			}
		}
	})
}
