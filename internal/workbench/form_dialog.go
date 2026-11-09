package workbench

import (
	"unicode/utf8"

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
	// layout and rect are the rows and place of a multiline field as last painted.
	layout components.TextareaLayout
	rect   gfx.Rect
}

// formDialog is the open form. run is called when the user submits: it returns
// the text of an error to show (the form stays open) or "" when the work is done
// or started, which closes the form.
type formDialog struct {
	title, description string
	submit             string
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
func (form *formDialog) slotHeight(index int) float32 {
	if form.fields[index].multiline {
		return formTextareaHeight + 4
	}
	return 66
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

// formKey moves the caret of a multiline field by the rows it wrapped to, which
// the native editor does not know. Enter in such a field is a new line, which the
// native editor makes; every other form field submits.
func (state *State) formKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	field := state.multilineField(event.Target)
	if field == nil {
		return gfx.EventResult{}, false
	}
	if _, composing := field.editor.Composition(); composing {
		return gfx.EventResult{}, false
	}
	movement, extend := components.TextareaMovementForKey(event.Key, event.Modifiers)
	if movement == components.TextareaMoveNone {
		return gfx.EventResult{}, false
	}
	value := field.editor.String()
	to := field.layout.Move(value, aiByteOffset(value, field.editor.Caret()), movement)
	field.editor.MoveTo(utf8.RuneCountInString(value[:to]), extend)
	return gfx.EventResult{Handled: true, Repaint: true}, true
}

// scrollForm scrolls the multiline field under the pointer. The wheel never
// reaches what is behind a dialog.
func (state *State) scrollForm(event gfx.ScrollEvent) gfx.EventResult {
	for index := 0; index < state.form.count; index++ {
		field := &state.form.fields[index]
		if field.multiline && pointInRect(event.X, event.Y, field.rect) {
			field.layout.ScrollBy(event.DeltaY)
			return gfx.EventResult{Handled: true, Repaint: true}
		}
	}
	return gfx.EventResult{Handled: true}
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

// bodyHeight is the dialog body's height: its fields, and the error under them
// when there is one.
func (form *formDialog) bodyHeight() float32 {
	height := float32(0)
	for index := 0; index < form.count; index++ {
		height += form.slotHeight(index)
	}
	if form.checkLabel != "" {
		height += 72
	}
	return withError(height, form.dialogError+form.testNote)
}

func (state *State) paintFormDialog(canvas *gfx.Canvas, viewport gfx.Rect) {
	theme := state.Theme()
	form := &state.form
	initial := hitFormSubmit
	if form.count > 0 {
		initial = hitFormFieldList.At(0)
	}
	// No scrim dismiss: a press beside the form must not discard its input.
	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		CloseID: hitFormClose, InitialFocusID: initial,
		ContentSize: gfx.Size{Width: 540}, FitBody: true, BodyHeight: form.bodyHeight(),
	})
	layout := dialog.Begin(form.title, form.description)
	var slots [maxFormFields]ui.Box
	var checkbox ui.Box
	f := state.settingsRow.Begin(canvas, theme, layout.Body)
	f.Column().Fill().PadSides(4, 4, 0, 4).Children(func() {
		for index := 0; index < form.count; index++ {
			slots[index] = f.Slot().Height(form.slotHeight(index)-4).MarginSides(0, 0, 4, 0)
		}
		if form.checkLabel != "" {
			f.Column().Height(72).Gap(2).PadSides(8, 0, 4, 0).Children(func() {
				checkbox = f.Row().Height(24).Items(ui.Center)
				checkbox.Children(func() {
					f.Slot().Width(28)
					f.Text(form.checkLabel).FontSize(metrics.FontSizeM).Color(state.pal.text)
				})
				f.Paragraph(form.checkDescription, 2).FontSize(metrics.FontSizeS).LineHeight(16).Color(state.pal.muted).MarginSides(0, 0, 0, 28)
			})
		}
		state.problemLine(f, form.dialogError)
		if form.dialogError == "" && form.testNote != "" {
			f.Text(form.testNote).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(state.pal.success).LineHeight(18)
		}
	})
	f.End()
	for index := 0; index < form.count; index++ {
		field := &form.fields[index]
		id := hitFormFieldList.At(index)
		slot := slots[index].Rect()
		content := components.BeginField(canvas, theme, slot).Paint(field.label, "", "")
		value := field.editor.String()
		focused := canvas.Focused() == id
		if field.multiline {
			state.paintFormTextarea(canvas, field, gfx.Rect{X: content.X, Y: content.Y, Width: content.Width, Height: slot.Y + slot.Height - content.Y}, id, focused)
			continue
		}
		state.paintSecretInput(canvas, content, components.InputSnapshot{
			ID: id, Label: field.label, Value: value, Placeholder: field.placeholder, Secure: field.secure,
			Focused: focused, PlatformEditing: focused && value != "",
		})
	}
	if form.checkLabel != "" {
		rect := checkbox.Rect()
		side := components.CheckboxExtent(theme)
		components.BeginCheckbox(canvas, theme, hitFormCheckbox, gfx.Rect{X: rect.X, Y: rect.Y + (rect.Height-side)/2}, form.checked).Label(form.checkLabel).Paint()
		canvas.HitLabel(hitFormCheckbox, gfx.Rect{X: rect.X + 28, Y: rect.Y, Width: max(rect.Width-28, 0), Height: rect.Height}, form.checkLabel, false)
	}
	variant := components.ButtonDefault
	if form.danger {
		variant = components.ButtonDestructive
	}
	actions := []components.ButtonAction{
		{ID: hitFormCancel, Label: state.messages.Cancel, Variant: components.ButtonSecondary},
	}
	if form.test != nil {
		actions = append(actions, components.ButtonAction{ID: hitFormTest, Label: state.messages.Tools.SyncTest, Variant: components.ButtonOutline, Disabled: form.testRunning})
	}
	actions = append(actions, components.ButtonAction{ID: hitFormSubmit, Label: form.submit, Variant: variant})
	dialog.Actions(layout, actions...)
	dialog.End()
}

func (state *State) paintFormTextarea(canvas *gfx.Canvas, field *formField, bounds gfx.Rect, id gfx.ID, focused bool) {
	value := field.editor.String()
	selection := field.editor.Selection()
	snapshot := components.TextareaSnapshot{
		ID: id, Label: field.label, Value: value, Placeholder: field.placeholder,
		Focused: focused, HideEditorText: true, ShowCaret: selection.Start == selection.End,
		Layout:         &field.layout,
		SelectionStart: aiByteOffset(value, selection.Start), SelectionEnd: aiByteOffset(value, selection.End),
		Caret: aiByteOffset(value, field.editor.Caret()),
	}
	if marked, composing := field.editor.Composition(); composing {
		snapshot.CompositionStart, snapshot.CompositionEnd = aiByteOffset(value, marked.Start), aiByteOffset(value, marked.End)
	}
	field.rect = bounds
	components.PaintTextarea(canvas, state.Theme(), bounds, snapshot)
}
