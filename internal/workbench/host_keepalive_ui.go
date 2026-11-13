package workbench

import (
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// The idle keepalive rows of the host form's Advanced section: the interval it
// runs at, the custom interval beside it, and what it sends.
var hostIdleKeepAliveHits = tree.Feature("host-idle-keepalive")

var (
	hitDialogIdleKeepAlive         = tree.ID(hostIdleKeepAliveHits, "dialog-idle-keepalive")
	hitDialogIdleKeepAliveDismiss  = tree.ID(hostIdleKeepAliveHits, "dialog-idle-keepalive-dismiss")
	hitDialogIdleKeepAliveItemList = tree.Sub(hostIdleKeepAliveHits, tree.Name("dialog-idle-keepalive-item"))
	hitDialogIdleUnit              = tree.ID(hostIdleKeepAliveHits, "dialog-idle-unit")
	hitDialogIdleUnitDismiss       = tree.ID(hostIdleKeepAliveHits, "dialog-idle-unit-dismiss")
	hitDialogIdleUnitItemList      = tree.Sub(hostIdleKeepAliveHits, tree.Name("dialog-idle-unit-item"))
	hitDialogIdleInput             = tree.ID(hostIdleKeepAliveHits, "dialog-idle-input")
	hitDialogIdleInputDismiss      = tree.ID(hostIdleKeepAliveHits, "dialog-idle-input-dismiss")
	hitDialogIdleInputItemList     = tree.Sub(hostIdleKeepAliveHits, tree.Name("dialog-idle-input-item"))
	hitDialogIdleInterval          = tree.ID(hostIdleKeepAliveHits, "dialog-idle-interval")
	hitDialogIdleCustomInput       = tree.ID(hostIdleKeepAliveHits, "dialog-idle-custom-input")
)

var (
	idleKeepAliveChoices = [...]idleKeepAlivePreset{
		idleKeepAliveOff, idleKeepAliveOneMinute, idleKeepAliveFiveMinutes, idleKeepAliveTenMinutes, idleKeepAliveCustom,
	}
	idleUnitChoices  = [...]idleIntervalUnit{idleIntervalSeconds, idleIntervalMinutes}
	idleInputChoices = [...]idleInputMode{idleInputCR, idleInputNUL, idleInputCustom}
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

func (state *State) idleKeepAliveOptions() []components.SelectItem {
	editor := &state.editor
	options := make([]components.SelectItem, 0, len(idleKeepAliveChoices))
	for index, preset := range idleKeepAliveChoices {
		options = append(options, components.SelectItem{
			ID: hitDialogIdleKeepAliveItemList.At(index), Label: state.idleKeepAlivePresetLabel(preset),
			Selected: editor.idleKeepAlive == preset,
		})
	}
	return options
}

func (state *State) idleUnitOptions() []components.SelectItem {
	editor := &state.editor
	options := make([]components.SelectItem, 0, len(idleUnitChoices))
	for index, unit := range idleUnitChoices {
		options = append(options, components.SelectItem{
			ID: hitDialogIdleUnitItemList.At(index), Label: state.idleIntervalUnitLabel(unit),
			Selected: editor.idleUnit == unit,
		})
	}
	return options
}

func (state *State) idleInputOptions() []components.SelectItem {
	editor := &state.editor
	current := editor.idleInputValue()
	options := make([]components.SelectItem, 0, len(idleInputChoices))
	for index, mode := range idleInputChoices {
		options = append(options, components.SelectItem{
			ID: hitDialogIdleInputItemList.At(index), Label: state.idleInputLabel(mode),
			Selected: current == mode,
		})
	}
	return options
}

// declareHostIdleKeepAliveRows lays out the probe's rows. It is called inside
// the Advanced section, after the SSH-only rows when there are any.
func (state *State) declareHostIdleKeepAliveRows(c ui.C) {
	editor, form := &state.editor, &state.editor.form
	b := &form.idleBody
	*b = hostIdleKeepAliveRowsBody{}
	// The frame that delivers a click paints the rows it laid out, while the
	// click has already written the value the next frame will show.
	form.idleLayout, form.idleInputLayout = editor.idleKeepAlive, editor.idleInputValue()
	rowHeight := float32(hostFormRowHeight)
	m := state.messages.Tools
	ui.Column(c).FillWidth().Children(func() {
		ui.Column(c).Height(groupHeadingHeight).Children(func() {
			ui.Column(c).Height(1).MarginSides(12, 0, 0, 0).Background(withAlpha(state.pal.line, 0.7))
			ui.Text(c, m.IdleKeepAliveHeading).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(state.pal.muted).MarginSides(8, 0, 0, 2)
		})
		b.interval = ui.Slot(c).Height(rowHeight)
		if form.idleLayout == idleKeepAliveCustom {
			ui.Row(c).Height(rowHeight).Gap(8).Children(func() {
				b.customInterval = ui.Slot(c).Grow(1)
				b.unit = ui.Slot(c).Width(120).Shrink(0)
			})
		}
		if form.idleLayout != idleKeepAliveOff {
			b.input = ui.Slot(c).Height(rowHeight)
			if form.idleInputLayout == idleInputCustom {
				b.customInput = ui.Slot(c).Height(rowHeight)
			}
		}
		ui.Slot(c).Height(16)
	})
}

func (state *State) paintHostIdleKeepAliveRows(canvas *gfx.Canvas) {
	editor, form := &state.editor, &state.editor.form
	b := &form.idleBody
	m := state.messages.Tools
	state.paintHostIdleSelect(canvas, hitDialogIdleKeepAlive, hitDialogIdleKeepAliveDismiss, b.interval.Rect(),
		m.IdleKeepAliveField, m.IdleKeepAliveField, state.idleKeepAlivePresetLabel(form.idleLayout), editor.idleKeepAliveOpen, state.idleKeepAliveOptions())
	if form.idleLayout == idleKeepAliveCustom {
		state.paintDialogField(canvas, hitDialogIdleInterval, fieldIdleInterval,
			m.IdleKeepAliveCustomInterval, "90", b.customInterval.Rect())
		state.paintHostIdleSelect(canvas, hitDialogIdleUnit, hitDialogIdleUnitDismiss, b.unit.Rect(),
			"", m.IdleKeepAliveCustomInterval, state.idleIntervalUnitLabel(editor.idleUnit), editor.idleUnitOpen, state.idleUnitOptions())
	}
	if form.idleLayout != idleKeepAliveOff {
		state.paintHostIdleSelect(canvas, hitDialogIdleInput, hitDialogIdleInputDismiss, b.input.Rect(),
			m.IdleKeepAliveInputField, m.IdleKeepAliveInputField, state.idleInputLabel(form.idleInputLayout), editor.idleInputOpen, state.idleInputOptions())
		if form.idleInputLayout == idleInputCustom {
			state.paintDialogField(canvas, hitDialogIdleCustomInput, fieldIdleInputCustom,
				m.IdleKeepAliveCustomInput, m.IdleKeepAliveCustomPlaceholder, b.customInput.Rect())
		}
	}
}

// paintHostIdleSelect paints one select of the probe rows: its field, the
// popup it opens and the items the click handler reads. title is painted above
// the control; accessibleLabel names it when there is no room for a title, as
// for the unit beside the custom interval.
func (state *State) paintHostIdleSelect(canvas *gfx.Canvas, trigger, dismiss gfx.ID, bounds gfx.Rect, title, accessibleLabel, value string, open bool, options []components.SelectItem) {
	theme := state.Theme()
	content := components.BeginField(canvas, theme, bounds).Paint(title, "", "")
	state.rememberHostControl(trigger, content)
	portal := state.beginHostFormPopup(canvas, open)
	defer state.endHostFormPopup(canvas, portal)
	list := state.hostAuthListBounds(content, len(options))
	selectControl := components.BeginSelect(canvas, theme, content, components.SelectOptions{
		TriggerID: trigger, Label: accessibleLabel, Value: value,
		Open: open, Content: list,
		Layer: dialogLayer + 1, Scope: dialogScope, DismissID: dismiss, Viewport: state.windowRect(),
	})
	defer selectControl.End()
	if open {
		state.menuPlaced(list)
	}
	for _, option := range options {
		selectControl.AddItem(option)
	}
}

// clickHostIdleKeepAlive handles the probe's three selects. It reports false
// for a hit it does not own.
func (state *State) clickHostIdleKeepAlive(id gfx.ID) bool {
	editor := &state.editor
	switch {
	case id == hitDialogIdleKeepAlive:
		editor.idleKeepAliveOpen = !editor.idleKeepAliveOpen
		editor.idleUnitOpen, editor.idleInputOpen = false, false
	case id == hitDialogIdleKeepAliveDismiss:
		editor.idleKeepAliveOpen = false
	case id.Scope() == hitDialogIdleKeepAliveItemList:
		index := item(id)
		if index < 0 || index >= len(idleKeepAliveChoices) {
			return false
		}
		editor.idleKeepAlive = idleKeepAliveChoices[index]
		editor.idleKeepAliveOpen = false
		if editor.idleKeepAlive == idleKeepAliveCustom && strings.TrimSpace(editor.idleInterval.String()) == "" {
			editor.idleInterval.Insert("1")
			editor.idleUnit = idleIntervalMinutes
		}
	case id == hitDialogIdleUnit:
		editor.idleUnitOpen = !editor.idleUnitOpen
		editor.idleKeepAliveOpen, editor.idleInputOpen = false, false
	case id == hitDialogIdleUnitDismiss:
		editor.idleUnitOpen = false
	case id.Scope() == hitDialogIdleUnitItemList:
		index := item(id)
		if index < 0 || index >= len(idleUnitChoices) {
			return false
		}
		editor.idleUnit = idleUnitChoices[index]
		editor.idleUnitOpen = false
	case id == hitDialogIdleInput:
		editor.idleInputOpen = !editor.idleInputOpen
		editor.idleKeepAliveOpen, editor.idleUnitOpen = false, false
	case id == hitDialogIdleInputDismiss:
		editor.idleInputOpen = false
	case id.Scope() == hitDialogIdleInputItemList:
		index := item(id)
		if index < 0 || index >= len(idleInputChoices) {
			return false
		}
		mode := idleInputChoices[index]
		editor.idleInput = mode
		// Clicking the option already shown as this protocol's default does not
		// pin it: a later protocol change still follows that protocol.
		editor.idleInputChosen = mode != idleInputForProtocol(editor.protocol)
		editor.idleInputOpen = false
	default:
		return false
	}
	state.dialogError = ""
	return true
}
