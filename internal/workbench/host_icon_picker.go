package workbench

import (
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"

	"motty/internal/sshconn"
)

var (
	hostIconPickerHits  = tree.Feature("host-icon-picker")
	hitHostIconTrigger  = tree.ID(hostIconPickerHits, "trigger")
	hitHostIconDismiss  = tree.ID(hostIconPickerHits, "dismiss")
	hitHostIconScroll   = tree.ID(hostIconPickerHits, "scroll")
	hostIconPickerScope = tree.ID(hostIconPickerHits, "scope")
	hitHostIconChoices  = tree.Sub(hostIconPickerHits, tree.Name("option"))
)

// hostIconArtKey is what an option's art depends on: the system the automatic
// icon shows, whether the host is a remote desktop, and the theme. The picker
// rebuilds its options when it changes.
type hostIconArtKey struct {
	system  sshconn.RemoteSystem
	desktop bool
	dark    bool
}

// hostIconPicker is the searchable field that chooses a host's icon. The set is
// fixed: a query filters it and never creates an icon.
type hostIconPicker struct {
	control    components.SingleSelectState
	serialized string
	ready      bool
	art        hostIconArtKey
	popup      gfx.Rect
}

func (state *State) hostIconPicker() *hostIconPicker {
	p := &state.editor.iconPicker
	art := hostIconArtKey{
		system:  state.editor.system,
		desktop: state.editor.protocol == protocolVNC || state.editor.protocol == protocolRDP,
		dark:    state.pal.dark,
	}
	if !p.ready || p.art != art {
		p.control.MatchCaseInsensitive = true
		choices := hostIconChoices()
		options := make([]components.SingleSelectOption, 0, len(choices))
		for _, choice := range choices {
			options = append(options, components.SingleSelectOption{
				Value: choice.String(),
				Label: state.hostIconLabel(choice),
				Icon:  state.hostIconArt(choice, art.system, art.desktop),
			})
		}
		p.control.SetOptions(options)
		// Rebuilding (a protocol switch, a detection or a theme change) must not
		// reset the value or the query, so keep the control's value as the
		// serialization baseline instead of a sentinel.
		p.ready, p.art, p.serialized = true, art, p.control.Value()
	}
	if value := state.editor.icon.String(); value != p.serialized {
		p.control.SetValue(value)
		p.serialized = value
	}
	return p
}

// syncHostIcon takes the value the user chose. Only a change is a choice: a key
// that merely moves the highlight must not mark the icon as chosen.
func (state *State) syncHostIcon() {
	p := &state.editor.iconPicker
	value := p.control.Value()
	if value == p.serialized {
		return
	}
	icon, ok := parseHostIcon(value)
	if !ok {
		return
	}
	state.editor.icon, state.editor.iconChosen = icon, true
	p.serialized = value
}

// resetHostIconPicker asks the picker to rebuild its options, whose generic
// icons carry localized labels, when the language changes. The control keeps
// its value and query; only the option list is stale.
func (state *State) resetHostIconPicker() {
	state.editor.iconPicker.ready = false
}

func (state *State) clickHostIconPicker(id gfx.ID) bool {
	p := state.hostIconPicker()
	switch {
	case id == hitDialogIcon:
		state.closeOtherHostPickers()
		p.control.Open = true
	case id == hitHostIconTrigger:
		open := !p.control.Open
		state.closeOtherHostPickers()
		p.control.Open = open
	case id == hitHostIconDismiss:
		p.control.Open = false
	case id.Scope() == hitHostIconChoices:
		p.control.Choose(item(id))
		state.syncHostIcon()
	default:
		return false
	}
	return true
}

func (state *State) hostIconPickerKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if state.modal != modalNewHost {
		return gfx.EventResult{}, false
	}
	p := state.hostIconPicker()
	if event.Target != hitDialogIcon && (!p.control.Open || event.Key != gfx.KeyEscape && event.Key != gfx.KeyTab) {
		return gfx.EventResult{}, false
	}
	if event.Target == hitDialogIcon && event.Key == gfx.KeyEnter && event.Modifiers == 0 && !event.Release {
		if _, composing := p.control.Query.Composition(); composing || event.Repeat {
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
		// With the list closed and nothing typed, Enter belongs to the form's
		// save action. Otherwise the control chooses the row it highlights, so
		// Enter takes the icon a search narrowed the list to.
		if !p.control.Open && strings.TrimSpace(p.control.Query.String()) == "" {
			return gfx.EventResult{}, false
		}
	}
	result := p.control.Key(event)
	state.syncHostIcon()
	return result, result.Handled || result.Repaint
}

func (state *State) paintHostIconPicker(canvas *gfx.Canvas, bounds gfx.Rect) {
	p := state.hostIconPicker()
	input := components.BeginField(canvas, state.Theme(), bounds).For(hitDialogIcon).Paint(state.messages.FieldIcon, "", "")
	state.revealHostField(canvas, hitDialogIcon, input)
	// The list hangs from the field into the window, as the icon choice always
	// has: the form's clip is where its fields scroll, not where the list ends.
	portal := state.beginHostFormPopup(canvas, p.control.Open)
	defer state.endHostFormPopup(canvas, portal)
	layout := components.PaintSingleSelect(canvas, state.Theme(), input, &p.control, components.SingleSelectOptions{
		InputID: hitDialogIcon, TriggerID: hitHostIconTrigger, DismissID: hitHostIconDismiss, Scope: hostIconPickerScope, ScrollID: hitHostIconScroll, OptionIDs: hitHostIconChoices,
		Label: state.messages.FieldIcon, Placeholder: state.messages.FieldIconPlaceholder, EmptyLabel: state.messages.IconsEmpty,
		Viewport: state.windowRect(), Layer: dialogLayer + 2,
	})
	p.popup = layout.Popup
}
