package workbench

import (
	"slices"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

// revealName is the name every show/hide button's place has: a password field's
// button sits in a scope of its own beneath the field's scope, named like the
// field, so no two fields share one and a click finds its field again.
var revealName = tree.Name("reveal")

// secretReveal remembers which password fields show their text. A field that
// was not painted in a frame is forgotten by the next one, so closing a dialog
// by any route hides what was in it, and a secret is never left in the open for
// the next time it is shown.
type secretReveal struct {
	shown []revealedButton
	// refocus is the field to hand the focus back to after its button was
	// pressed: the user is partway through typing, not moving on.
	refocus gfx.ID
}

// revealedButton is the show/hide button of a field that shows its text, and
// whether the current frame has painted it yet.
type revealedButton struct {
	button gfx.ID
	seen   bool
}

func (reveal *secretReveal) reset() {
	reveal.shown = reveal.shown[:0]
	reveal.refocus = gfx.ID{}
}

// beginPaint drops the fields the previous frame did not paint.
func (reveal *secretReveal) beginPaint() {
	kept := reveal.shown[:0]
	for _, entry := range reveal.shown {
		if entry.seen {
			kept = append(kept, revealedButton{button: entry.button})
		}
	}
	reveal.shown = kept
}

// index is where a button is in shown, or -1.
func (reveal *secretReveal) index(button gfx.ID) int {
	return slices.IndexFunc(reveal.shown, func(entry revealedButton) bool { return entry.button == button })
}

// revealButton is the identity of the show/hide button of a field.
func revealButton(field gfx.ID) gfx.ID {
	return tree.Sub(field.Scope(), revealName).ID(field.Name())
}

// revealedField is the field a show/hide button belongs to, and false for any
// other identity.
func revealedField(button gfx.ID) (gfx.ID, bool) {
	scope := button.Scope()
	if scope.IsZero() || tree.ScopeName(scope) != revealName {
		return gfx.ID{}, false
	}
	return tree.Parent(scope).ID(button.Name()), true
}

// clickSecretReveal toggles a field between bullets and its text.
func (state *State) clickSecretReveal(id gfx.ID) bool {
	field, ok := revealedField(id)
	if !ok {
		return false
	}
	reveal := &state.reveal
	if index := reveal.index(id); index >= 0 {
		reveal.shown = slices.Delete(reveal.shown, index, index+1)
	} else {
		reveal.shown = append(reveal.shown, revealedButton{button: id, seen: true})
	}
	reveal.refocus = field
	return true
}

// paintSecretInput paints a text field. A secret one gets a button at its end
// that swaps the bullets for the text, as a password field of a web form does;
// any other field is painted as it is.
func (state *State) paintSecretInput(canvas *gfx.Canvas, bounds gfx.Rect, snapshot components.InputSnapshot) {
	state.paintSecretInputStart(canvas, bounds, snapshot, components.InputGroupSlot{})
}

// paintSecretInputStart is paintSecretInput with an addon at the start of a
// secret field, such as the button that chooses a shared password.
func (state *State) paintSecretInputStart(canvas *gfx.Canvas, bounds gfx.Rect, snapshot components.InputSnapshot, start components.InputGroupSlot) {
	theme := state.Theme()
	if !snapshot.Secure || snapshot.ID.IsZero() {
		components.PaintInput(canvas, theme, bounds, snapshot)
		return
	}
	button := revealButton(snapshot.ID)
	index := state.reveal.index(button)
	revealed := index >= 0
	if revealed {
		state.reveal.shown[index].seen = true
	}
	if state.reveal.refocus == snapshot.ID {
		state.reveal.refocus = gfx.ID{}
		canvas.RequestFocus(snapshot.ID)
		// The runtime moves the focus after this frame; paint the field as the
		// one that has it, or its ring is missing until something repaints.
		snapshot.Focused = true
	}
	icon, label := lucideIcons[iconEye], state.messages.ShowSecret
	if revealed {
		icon, label = lucideIcons[iconEyeOff], state.messages.HideSecret
	}
	components.PaintInputGroup(canvas, theme, bounds, components.InputGroupSnapshot{
		ID: snapshot.ID, Label: snapshot.Label, Value: snapshot.Value, DisplayValue: snapshot.DisplayValue,
		Placeholder: snapshot.Placeholder, Hint: snapshot.Hint,
		Disabled: snapshot.Disabled, ReadOnly: snapshot.ReadOnly, Invalid: snapshot.Invalid,
		Secure: !revealed, Focused: snapshot.Focused, NativeEditing: snapshot.PlatformEditing,
		NavigationKeys: snapshot.NavigationKeys, ShowCaret: snapshot.ShowCaret, Caret: snapshot.Caret,
		SelectionStart: snapshot.SelectionStart, SelectionEnd: snapshot.SelectionEnd,
		CompositionStart: snapshot.CompositionStart, CompositionEnd: snapshot.CompositionEnd,
		InlineStart: start,
		InlineEnd: components.InputGroupSlot{
			Kind: components.InputGroupSlotButton, ID: button, Icon: icon, Label: label,
			ButtonSize: components.InputGroupButtonIconXS, Disabled: snapshot.Disabled,
		},
	})
}
