package workbench

import (
	"strings"
	"unicode"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

var (
	hostGroupPickerHits       = tree.Feature("host-group-picker")
	hitHostGroupTrigger       = tree.ID(hostGroupPickerHits, "trigger")
	hitHostGroupClear         = tree.ID(hostGroupPickerHits, "clear")
	hitHostGroupPickerDismiss = tree.ID(hostGroupPickerHits, "dismiss")
	hitHostGroupPickerScroll  = tree.ID(hostGroupPickerHits, "scroll")
	hostGroupPickerScope      = tree.ID(hostGroupPickerHits, "scope")
	hitHostGroupChoices       = tree.Sub(hostGroupPickerHits, tree.Name("option"))
)

type hostGroupPicker struct {
	control    components.SingleSelectState
	serialized string
	ready      bool
	popup      gfx.Rect
}

func validHostGroup(value string) bool {
	return strings.TrimSpace(value) != "" && !strings.ContainsFunc(value, unicode.IsControl)
}

func (state *State) hostGroupPicker() *hostGroupPicker {
	p := &state.editor.groupPicker
	if !p.ready {
		p.control.AllowCreate = true
		// A group has one name: typing "servers" over "Servers" must select the
		// existing group rather than create a case-different twin.
		p.control.MatchCaseInsensitive = true
		// Groups nest by "/": browse one level at a time, with typing as the
		// flat search over full paths.
		p.control.Separator = "/"
		p.control.SetValidateCreate(validHostGroup)
		options := make([]components.SingleSelectOption, 0, len(state.hostGroups))
		for _, group := range state.hostGroups {
			if group.Name != "" {
				options = append(options, components.SingleSelectOption{Value: group.Name})
			}
		}
		p.control.SetOptions(options)
		p.ready, p.serialized = true, "\x00"
	}
	if value := state.editor.value(fieldGroup); value != p.serialized {
		p.control.SetValue(value)
		p.serialized = value
	}
	return p
}

func (state *State) syncHostGroup() {
	p := &state.editor.groupPicker
	if value := p.control.Value(); value != p.serialized {
		state.editor.group = text.Editor{}
		state.editor.group.Insert(value)
		p.serialized = value
	}
	if p.control.Query.String() == "" && state.dialogError == state.messages.ErrHostGroupInvalid {
		state.dialogError = ""
	}
}

func (state *State) commitHostGroupQuery() bool {
	p := state.hostGroupPicker()
	if _, composing := p.control.Query.Composition(); composing {
		// The input method is still composing: its own confirm decides when the
		// text is a name, and a form save must not report it as invalid.
		return true
	}
	if !p.control.CommitQuery() {
		state.dialogError = state.messages.ErrHostGroupInvalid
		return false
	}
	state.syncHostGroup()
	return true
}

func (state *State) clickHostGroupPicker(id gfx.ID) bool {
	p := state.hostGroupPicker()
	switch {
	case id == hitDialogGroup:
		p.control.Open = true
		state.editor.tagPicker.control.Open = false
	case id == hitHostGroupTrigger:
		p.control.Open = !p.control.Open
		state.editor.tagPicker.control.Open = false
	case id == hitHostGroupClear:
		p.control.Clear()
		state.syncHostGroup()
	case id == hitHostGroupPickerDismiss:
		p.control.Open = false
	case id.Scope() == hitHostGroupChoices:
		p.control.Choose(item(id))
		state.syncHostGroup()
	default:
		return false
	}
	return true
}

func (state *State) hostGroupPickerKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if state.modal != modalNewHost {
		return gfx.EventResult{}, false
	}
	p := state.hostGroupPicker()
	if event.Target != hitDialogGroup && (!p.control.Open || event.Key != gfx.KeyEscape && event.Key != gfx.KeyTab) {
		return gfx.EventResult{}, false
	}
	if event.Target == hitDialogGroup && event.Key == gfx.KeyEnter && event.Modifiers == 0 && !event.Release {
		// Enter means "take what is typed", and with nothing typed it means the
		// form's Enter: save. The picker must not consume it for a group it did
		// not choose.
		if _, composing := p.control.Query.Composition(); composing || event.Repeat {
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
		if strings.TrimSpace(p.control.Query.String()) == "" {
			if p.control.Open {
				p.control.Dismiss()
				return gfx.EventResult{Handled: true, Repaint: true}, true
			}
			return gfx.EventResult{}, false
		}
		if !state.commitHostGroupQuery() {
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	result := p.control.Key(event)
	state.syncHostGroup()
	return result, result.Handled || result.Repaint
}

func isHostGroupPickerTarget(id gfx.ID) bool {
	return id == hitDialogGroup || id == hitHostGroupTrigger || id == hitHostGroupClear || id == hitHostGroupPickerScroll || id.Scope() == hitHostGroupChoices
}

func (state *State) paintHostGroupPicker(canvas *gfx.Canvas, bounds gfx.Rect) {
	p := state.hostGroupPicker()
	input := components.BeginField(canvas, state.Theme(), bounds).For(hitDialogGroup).Paint(state.messages.FieldGroup, "", "")
	state.revealHostField(canvas, hitDialogGroup, input)
	query := strings.TrimSpace(p.control.Query.String())
	viewport := gfx.Rect{Width: state.viewport.Width, Height: state.viewport.Height}
	if state.editor.form.clipped {
		// Keep every option and the scrollbar inside the form's actual clip.
		viewport = state.editor.form.geometry.Viewport
	}
	layout := components.PaintSingleSelect(canvas, state.Theme(), input, &p.control, components.SingleSelectOptions{
		InputID: hitDialogGroup, TriggerID: hitHostGroupTrigger, ClearID: hitHostGroupClear, DismissID: hitHostGroupPickerDismiss, Scope: hostGroupPickerScope, ScrollID: hitHostGroupPickerScroll, OptionIDs: hitHostGroupChoices,
		Label: state.messages.FieldGroup, Placeholder: state.messages.FieldGroupPlaceholder, EmptyLabel: state.messages.HostGroupsEmpty, CreatePrefix: state.messages.HostGroupCreatePrefix, ClearLabel: state.messages.MoveGroupNone,
		Viewport: viewport, Layer: dialogLayer + 2, Invalid: query != "" && !validHostGroup(query),
	})
	p.popup = layout.Popup
	if bounds, ok := canvas.HitBounds(hitHostGroupClear); ok {
		state.rememberHostControl(hitHostGroupClear, bounds)
	}
}
