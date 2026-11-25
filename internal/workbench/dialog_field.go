package workbench

import (
	"github.com/vivaui/viva/base"
	"github.com/vivaui/viva/field"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

// dialogRowGap is the air a dialog's form leaves under a field's control,
// between the control and the title of the row below it, and dialogCheckHeight
// is the room a checkbox row and its margins take. They are the dialog rhythm
// rather than a spacing token: every dialog shares it, so a form that sums its
// body's height can use the same numbers the rows are declared with.
const (
	dialogRowGap         = 10
	dialogCheckHeight    = 20
	dialogCheckGapAbove  = 4
	dialogCheckGapBelow  = 6
	dialogCheckRowHeight = dialogCheckHeight + dialogCheckGapAbove + dialogCheckGapBelow
)

// dialogRowHeight is the height of one labelled row of a dialog's form: the
// field's title, the space under it, the control, and the dialog's gap under the
// control.
func dialogRowHeight(theme *base.Theme) float32 {
	metrics := theme.Metrics
	return field.TitleHeight(metrics) + field.Gap(metrics) + metrics.ControlHeight + dialogRowGap
}

// dialogField is a labelled single-line field of a dialog the page declares: its
// row, its text bound to an editor the application owns, and, for a secret, whether
// the text shows.
type dialogField struct {
	row   ui.FormFieldState
	input ui.Field
	box   ui.Box
	bound bool
	shown bool
}

// dialogFieldSpec says what a dialogField shows: Description is the line under
// it, Error replaces that line and paints the field as wrong, and Invalid paints
// it as wrong with no line of its own. A Secret field has the button that shows
// its text.
type dialogFieldSpec struct {
	ID                                     gfx.ID
	Label, Description, Placeholder, Error string
	Secret, Invalid, Disabled, Multiline   bool
	Height                                 float32
}

// declare declares the field over editor, the one the application reads, and
// returns the text's box.
func (f *dialogField) declare(c ui.C, state *State, editor *text.Editor, spec dialogFieldSpec) {
	if !f.bound {
		f.input.Bind(editor)
		f.bound = true
	}
	f.input.Secure = spec.Secret && !f.shown
	f.input.Invalid = spec.Invalid || spec.Error != ""
	f.input.Disabled = spec.Disabled
	f.row.For, f.row.Description, f.row.Error = spec.ID, spec.Description, spec.Error
	f.row.Invalid = spec.Invalid || spec.Error != ""
	if spec.Multiline {
		theme := c.Theme()
		metrics := theme.Metrics
		ui.Column(c).FillWidth().Height(spec.Height).Shrink(0).PadSides(0, 0, dialogRowGap, 0).Gap(metrics.GapItem / 2).Children(func() {
			ui.Text(c, spec.Label).FontSize(metrics.FontSizeM).Weight(gfx.WeightMedium).Color(theme.Foreground).LineHeight(metrics.FontSizeM + 4).Ellipsis().LabelFor(spec.ID)
			f.box = ui.TextArea(c, &f.input, spec.Label, spec.Placeholder, ui.WithID(spec.ID)).FillWidth().Grow(1)
		})
		return
	}
	ui.FormField(c, &f.row, spec.Label).FillWidth().Height(spec.Height).Children(func() {
		box := ui.TextField(c, &f.input, spec.Label, spec.Placeholder, ui.WithID(spec.ID))
		f.box = box
		if spec.Secret {
			icon, name := lucideIcons[iconEye], state.messages.ShowSecret
			if f.shown {
				icon, name = lucideIcons[iconEyeOff], state.messages.HideSecret
			}
			box.Action(icon, name)
			if box.ActionClicked() {
				f.shown = !f.shown
			}
		}
	})
}

// pageFlags is the arena of values the page's checkboxes and switches are declared
// over: the application keeps the setting and the click is the central handler's, so
// the control only shows it. Each paint starts again at the first.
type pageFlags struct {
	values [256]bool
	used   int
	// stops are the checkboxes declared this paint, for the host form to keep in
	// its tab ring while they lie outside its clipped viewport.
	stops     [32]checkStop
	stopCount int
}

// checkStop is a checkbox row declared by a dialog: where its control is, once laid out.
type checkStop struct {
	id  gfx.ID
	row ui.Box
}

// flag returns a value the control is declared over, holding on.
func (state *State) pageFlag(on bool) *bool {
	flags := &state.chrome.flags
	if flags.used == len(flags.values) {
		spare := on
		return &spare
	}
	flag := &flags.values[flags.used]
	flags.used++
	*flag = on
	return flag
}

// declareDialogCheck declares a checkbox with its words inside a dialog's body, the
// given room above and below it. A press, or Space on it, is the central click
// handler's under id.
func (state *State) declareDialogCheck(c ui.C, id gfx.ID, label string, checked bool, above, below float32) ui.Box {
	row := ui.Row(c).Height(dialogCheckHeight).Items(ui.Center).MarginSides(above, 0, below, 2).Children(func() {
		ui.Checkbox(c, state.pageFlag(checked), label).Hit(id, label).Grow(1)
	})
	state.noteCheck(id, row)
	return row
}

// declareDialogCheckDisabled is declareDialogCheck dimmed and inert: it is not a tab
// stop, and its words do not toggle it or reach the click handler.
func (state *State) declareDialogCheckDisabled(c ui.C, id gfx.ID, label string, checked bool, above, below float32) ui.Box {
	return ui.Row(c).Height(dialogCheckHeight).Items(ui.Center).MarginSides(above, 0, below, 2).Children(func() {
		ui.Checkbox(c, state.pageFlag(checked), label).Hit(id, label).Disabled(true).Grow(1)
	})
}

// noteCheck records a checkbox row of this paint.
func (state *State) noteCheck(id gfx.ID, row ui.Box) {
	flags := &state.chrome.flags
	if flags.stopCount < len(flags.stops) {
		flags.stops[flags.stopCount] = checkStop{id, row}
		flags.stopCount++
	}
}

// rememberDialogChecks keeps the checkboxes this paint declared in the host form's tab
// ring, where they lie outside its clipped viewport. Call it once the page is laid out.
func (state *State) rememberDialogChecks() {
	flags := &state.chrome.flags
	for _, stop := range flags.stops[:flags.stopCount] {
		state.rememberHostControl(stop.id, stop.row.Rect())
	}
}

// declareDialogCheckHelp is declareDialogCheck with a help icon right after the label,
// which paintHelpIcon fills in.
func (state *State) declareDialogCheckHelp(c ui.C, id gfx.ID, label string, checked bool, above, below float32) (row, help ui.Box) {
	row = ui.Row(c).Height(dialogCheckHeight).Gap(8).Items(ui.Center).MarginSides(above, 0, below, 2).Children(func() {
		ui.Checkbox(c, state.pageFlag(checked), label).Hit(id, label).Shrink(0)
		help = ui.Slot(c).Size(16, 16).Shrink(0)
		ui.Spacer(c).Grow(1)
	})
	state.noteCheck(id, row)
	return row, help
}

// dialogArea is a labelled text area of a dialog the page declares: its title, and a
// page text area over an editor the application owns.
type dialogArea struct {
	input ui.Field
	bound bool
}

// declare declares the area, height tall in all: the title over the rows.
func (a *dialogArea) declare(c ui.C, id gfx.ID, editor *text.Editor, label, placeholder string, height float32) {
	if !a.bound {
		a.input.Bind(editor)
		a.bound = true
	}
	theme := c.Theme()
	metrics := theme.Metrics
	ui.Column(c).FillWidth().Height(height).Shrink(0).Gap(metrics.GapItem / 2).Children(func() {
		ui.Text(c, label).FontSize(metrics.FontSizeM).Weight(gfx.WeightMedium).Color(theme.Foreground).LineHeight(metrics.FontSizeM + 4).Ellipsis().LabelFor(id)
		ui.TextArea(c, &a.input, label, placeholder, ui.WithID(id)).FillWidth().Grow(1)
	})
}
