package workbench

import (
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// The idle keepalive rows of the host form's Advanced section: the interval it
// runs at, the custom interval beside it, and what it sends.
var hostIdleKeepAliveHits = tree.Feature("host-idle-keepalive")

var (
	hitDialogIdleKeepAlive   = tree.ID(hostIdleKeepAliveHits, "dialog-idle-keepalive")
	hitDialogIdleUnit        = tree.ID(hostIdleKeepAliveHits, "dialog-idle-unit")
	hitDialogIdleInput       = tree.ID(hostIdleKeepAliveHits, "dialog-idle-input")
	hitDialogIdleInterval    = tree.ID(hostIdleKeepAliveHits, "dialog-idle-interval")
	hitDialogIdleCustomInput = tree.ID(hostIdleKeepAliveHits, "dialog-idle-custom-input")
	idleKeepAliveChoices     = [...]idleKeepAlivePreset{idleKeepAliveOff, idleKeepAliveOneMinute, idleKeepAliveFiveMinutes, idleKeepAliveTenMinutes, idleKeepAliveCustom}
	idleUnitChoices          = [...]idleIntervalUnit{idleIntervalSeconds, idleIntervalMinutes}
	idleInputChoices         = [...]idleInputMode{idleInputCR, idleInputNUL, idleInputCustom}
)

// hostIdleKeepAliveRowsBody is where the idle rows put each control.
type hostIdleKeepAliveRowsBody struct {
	interval, customInterval, unit, input, customInput ui.Box
}

func (state *State) idleKeepAlivePresetLabel(preset idleKeepAlivePreset) string {
	m := state.messages.Tools
	switch preset {
	case idleKeepAliveOneMinute:
		return m.IdleKeepAliveOneMinute
	case idleKeepAliveFiveMinutes:
		return m.IdleKeepAliveFiveMinutes
	case idleKeepAliveTenMinutes:
		return m.IdleKeepAliveTenMinutes
	case idleKeepAliveCustom:
		return m.IdleKeepAliveCustom
	default:
		return m.IdleKeepAliveOff
	}
}

func (state *State) idleIntervalUnitLabel(unit idleIntervalUnit) string {
	if unit == idleIntervalMinutes {
		return state.messages.Tools.IdleKeepAliveUnitMinutes
	}
	return state.messages.Tools.IdleKeepAliveUnitSeconds
}

func (state *State) idleInputLabel(mode idleInputMode) string {
	m := state.messages.Tools
	switch mode {
	case idleInputCR:
		return m.IdleKeepAliveInputCR
	case idleInputCustom:
		return m.IdleKeepAliveInputCustom
	default:
		return m.IdleKeepAliveInputNUL
	}
}

// declareHostIdleKeepAliveRows lays out the probe's rows as page controls. It is
// called inside the Advanced section, after the SSH-only rows when there are
// any. The values a select writes are applied after the paint that delivered a
// choice: the rows this frame laid out are the ones before the click.
func (state *State) declareHostIdleKeepAliveRows(c ui.C) {
	editor, form := &state.editor, &state.editor.form
	b := &form.idleBody
	*b = hostIdleKeepAliveRowsBody{}
	form.idleLayout, form.idleInputLayout = editor.idleKeepAlive, editor.idleInputValue()
	rowHeight := float32(hostFormRowHeight)
	m := state.messages.Tools
	ui.Column(c).FillWidth().Children(func() {
		ui.Column(c).Height(groupHeadingHeight).Children(func() {
			ui.Column(c).Height(1).MarginSides(12, 0, 0, 0).Background(withAlpha(state.pal.line, 0.7))
			ui.Text(c, m.IdleKeepAliveHeading).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(state.pal.muted).MarginSides(8, 0, 0, 2)
		})
		b.interval = ui.Slot(c).Height(rowHeight)
		form.idleIntervalChoice = editor.idleKeepAlive
		b.interval.Children(func() {
			ui.FormField(c, &form.idleIntervalField, m.IdleKeepAliveField).FillWidth().Height(rowHeight).Children(func() {
				form.idleIntervalBox = ui.Select(c, &form.idleIntervalChoice, idleKeepAliveChoices[:], state.idleKeepAlivePresetLabel).
					Label(m.IdleKeepAliveField).Hit(hitDialogIdleKeepAlive, m.IdleKeepAliveField)
				if form.idleIntervalBox.Changed() {
					editor.idleKeepAlive = form.idleIntervalChoice
					if editor.idleKeepAlive == idleKeepAliveCustom && strings.TrimSpace(editor.idleInterval.String()) == "" {
						editor.idleInterval.Insert("1")
						editor.idleUnit = idleIntervalMinutes
					}
					state.dialogError = ""
				}
			})
		})
		if form.idleLayout == idleKeepAliveCustom {
			ui.Row(c).Height(rowHeight).Gap(8).Children(func() {
				b.customInterval = ui.Slot(c).Grow(1)
				state.declareHostTextField(c, b.customInterval, &form.fieldIdleInterval, hitDialogIdleInterval, &editor.idleInterval, m.IdleKeepAliveCustomInterval, "90", false, rowHeight)
				b.unit = ui.Slot(c).Width(120).Shrink(0)
				form.idleUnitChoice = editor.idleUnit
				b.unit.Children(func() {
					ui.FormField(c, &form.idleUnitField, "").FillWidth().Height(rowHeight).Children(func() {
						form.idleUnitBox = ui.Select(c, &form.idleUnitChoice, idleUnitChoices[:], state.idleIntervalUnitLabel).
							Label(m.IdleKeepAliveCustomInterval).Hit(hitDialogIdleUnit, m.IdleKeepAliveCustomInterval)
						if form.idleUnitBox.Changed() {
							editor.idleUnit = form.idleUnitChoice
							state.dialogError = ""
						}
					})
				})
			})
		}
		if form.idleLayout != idleKeepAliveOff {
			b.input = ui.Slot(c).Height(rowHeight)
			form.idleInputChoice = editor.idleInputValue()
			b.input.Children(func() {
				ui.FormField(c, &form.idleInputField, m.IdleKeepAliveInputField).FillWidth().Height(rowHeight).Children(func() {
					form.idleInputBox = ui.Select(c, &form.idleInputChoice, idleInputChoices[:], state.idleInputLabel).
						Label(m.IdleKeepAliveInputField).Hit(hitDialogIdleInput, m.IdleKeepAliveInputField)
					if index, picked := form.idleInputBox.Picked(); picked && index >= 0 && index < len(idleInputChoices) {
						// A click on the option already shown is a choice too: it pins
						// the mode only when it is not this protocol's default.
						editor.chooseIdleInput(idleInputChoices[index])
						state.dialogError = ""
					}
				})
			})
			if form.idleInputLayout == idleInputCustom {
				b.customInput = ui.Slot(c).Height(rowHeight)
				state.declareHostTextField(c, b.customInput, &form.fieldIdleCustomInput, hitDialogIdleCustomInput, &editor.idleInputCustom,
					m.IdleKeepAliveCustomInput, m.IdleKeepAliveCustomPlaceholder, false, rowHeight)
			}
		}
		ui.Slot(c).Height(16)
	})
}

// chooseIdleInput takes the send mode a select chose. Choosing the option
// already shown as this protocol's default does not pin it: a later protocol
// change still follows that protocol.
func (editor *hostEditor) chooseIdleInput(mode idleInputMode) {
	editor.idleInput = mode
	editor.idleInputChosen = mode != idleInputForProtocol(editor.protocol)
}

// rememberHostIdleSelects keeps the probe's page selects in the form's tab ring.
func (state *State) rememberHostIdleSelects(canvas *gfx.Canvas) {
	form := &state.editor.form
	state.rememberHostPageSelect(canvas, form.idleIntervalBox, hitDialogIdleKeepAlive)
	state.rememberHostPageSelect(canvas, form.idleUnitBox, hitDialogIdleUnit)
	state.rememberHostPageSelect(canvas, form.idleInputBox, hitDialogIdleInput)
}
